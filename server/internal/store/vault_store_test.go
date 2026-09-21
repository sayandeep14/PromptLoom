package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/sayandeep14/PromptLoom/server/internal/db"
	"github.com/sayandeep14/PromptLoom/server/internal/models"
	"github.com/sayandeep14/PromptLoom/server/internal/store"
)

// Integration tests: they need a real PostgreSQL and are skipped unless
// TEST_DATABASE_URL is set. The database name must contain "test" because the
// tests recreate the schema and wipe every table.
//
//	docker run --rm -d -p 55432:5432 -e POSTGRES_PASSWORD=pw -e POSTGRES_DB=loom_test postgres:16
//	TEST_DATABASE_URL=postgres://postgres:pw@localhost:55432/loom_test go test ./internal/store/...
func setup(t *testing.T) context.Context {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration tests")
	}
	ctx := context.Background()
	t.Setenv("DATABASE_URL", url)
	if err := db.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)

	var name string
	if err := db.Pool.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(name, "test") {
		t.Fatalf("refusing to run destructive tests against database %q (name must contain \"test\")", name)
	}

	// Applying the schema twice also proves Migrate is idempotent.
	for i := 0; i < 2; i++ {
		if err := db.Migrate(ctx); err != nil {
			t.Fatalf("migrate (run %d): %v", i+1, err)
		}
	}
	if _, err := db.Pool.Exec(ctx, `TRUNCATE vault_files, vaults CASCADE`); err != nil {
		t.Fatal(err)
	}
	return ctx
}

var admin = models.Identity{Name: "admin", Admin: true}

func upsert(ctx context.Context, b *models.Bundle) error { return store.UpsertVault(ctx, b, admin) }

func del(ctx context.Context, slug string) error {
	_, err := store.DeleteVault(ctx, slug, admin)
	return err
}

func pack(slug string, files ...models.BundleFile) *models.Bundle {
	if len(files) == 0 {
		files = []models.BundleFile{{Path: "prompts/A.prompt.loom", FileType: "prompt", Content: "prompt A {}"}}
	}
	return &models.Bundle{
		Name: "Pack " + slug, Slug: slug, Version: "1.0.0", Description: "d", Author: "me",
		Tags: []string{"go", "backend"}, Files: files,
	}
}

func TestUpsertAndGetRoundTrip(t *testing.T) {
	ctx := setup(t)
	b := pack("go-backend",
		models.BundleFile{Path: "prompts/A.prompt.loom", FileType: "prompt", Content: "prompt A {}"},
		models.BundleFile{Path: ".dependency.loom", FileType: "meta", Content: "python>=1.0.0"},
		models.BundleFile{Path: "blocks/B.block.loom", FileType: "block", Content: "block B {}"},
	)
	b.PackID = "550e8400-e29b-41d4-a716-446655440000"
	b.RelatedLibraries = []models.RelatedLibrary{{Name: "gin", Version: "1.9.0", ID: "x"}}

	if err := upsert(ctx, b); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetBundle(ctx, "go-backend")
	if err != nil || got == nil {
		t.Fatalf("GetBundle: %v %v", got, err)
	}
	if got.Name != b.Name || got.Version != "1.0.0" || got.PackID != b.PackID || got.Author != "me" {
		t.Errorf("metadata mismatch: %+v", got)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "go" {
		t.Errorf("tags: %v", got.Tags)
	}
	if len(got.RelatedLibraries) != 1 || got.RelatedLibraries[0].Name != "gin" {
		t.Errorf("related libraries: %+v", got.RelatedLibraries)
	}
	// Files come back ordered by path.
	want := []string{".dependency.loom", "blocks/B.block.loom", "prompts/A.prompt.loom"}
	if len(got.Files) != 3 {
		t.Fatalf("files: %+v", got.Files)
	}
	for i, p := range want {
		if got.Files[i].Path != p {
			t.Errorf("file %d: %q, want %q", i, got.Files[i].Path, p)
		}
	}
	if got.Files[0].Content != "python>=1.0.0" || got.Files[0].FileType != "meta" {
		t.Errorf("file content/type lost: %+v", got.Files[0])
	}
}

func TestNilTagsAndEmptyPackID(t *testing.T) {
	ctx := setup(t)
	b := pack("bare")
	b.Tags = nil // client omitted tags entirely; column is NOT NULL
	b.PackID = ""
	if err := upsert(ctx, b); err != nil {
		t.Fatalf("upload without tags/pack_id must work: %v", err)
	}
	v, err := store.GetVault(ctx, "bare")
	if err != nil || v == nil {
		t.Fatal(v, err)
	}
	if v.PackID != "" || len(v.Tags) != 0 {
		t.Errorf("got pack_id=%q tags=%v", v.PackID, v.Tags)
	}
}

func TestUpsertReplacesFilesAndKeepsIdentity(t *testing.T) {
	ctx := setup(t)
	if err := upsert(ctx, pack("p",
		models.BundleFile{Path: "prompts/Old.prompt.loom", FileType: "prompt", Content: "old"},
		models.BundleFile{Path: "prompts/Keep.prompt.loom", FileType: "prompt", Content: "v1"})); err != nil {
		t.Fatal(err)
	}
	first, _ := store.GetVault(ctx, "p")

	v2 := pack("p", models.BundleFile{Path: "prompts/Keep.prompt.loom", FileType: "prompt", Content: "v2"})
	v2.Version = "2.0.0"
	if err := upsert(ctx, v2); err != nil {
		t.Fatal(err)
	}

	second, _ := store.GetVault(ctx, "p")
	if second.ID != first.ID {
		t.Error("re-publishing must update in place, not create a new row")
	}
	if second.Version != "2.0.0" || second.FileCount != 1 {
		t.Errorf("after replace: version=%s files=%d", second.Version, second.FileCount)
	}
	b, _ := store.GetBundle(ctx, "p")
	if len(b.Files) != 1 || b.Files[0].Content != "v2" {
		t.Errorf("stale files remain: %+v", b.Files)
	}
}

func TestUpsertIsAtomic(t *testing.T) {
	ctx := setup(t)
	if err := upsert(ctx, pack("p")); err != nil {
		t.Fatal(err)
	}
	// Duplicate paths violate UNIQUE(vault_id, path) midway through the file inserts.
	bad := pack("p",
		models.BundleFile{Path: "prompts/X.prompt.loom", FileType: "prompt", Content: "x"},
		models.BundleFile{Path: "prompts/X.prompt.loom", FileType: "prompt", Content: "y"})
	bad.Version = "9.9.9"
	if err := upsert(ctx, bad); err == nil {
		t.Fatal("expected the duplicate-path upload to fail")
	}
	got, _ := store.GetBundle(ctx, "p")
	if got.Version != "1.0.0" || len(got.Files) != 1 || got.Files[0].Path != "prompts/A.prompt.loom" {
		t.Errorf("failed upload must roll back completely, got version=%s files=%+v", got.Version, got.Files)
	}
}

func TestUniquenessConstraints(t *testing.T) {
	ctx := setup(t)
	a := pack("a")
	a.PackID = "550e8400-e29b-41d4-a716-446655440000"
	if err := upsert(ctx, a); err != nil {
		t.Fatal(err)
	}

	sameName := pack("b")
	sameName.Name = a.Name
	if err := upsert(ctx, sameName); err == nil {
		t.Error("two packs with the same name must conflict")
	}
	samePackID := pack("c")
	samePackID.PackID = a.PackID
	if err := upsert(ctx, samePackID); err == nil {
		t.Error("two packs with the same pack_id must conflict")
	}
	// A failed insert must not leave a half-created pack behind.
	if v, _ := store.GetVault(ctx, "b"); v != nil {
		t.Error("pack b should not exist")
	}
}

func TestSchemaRejectsBadFileType(t *testing.T) {
	ctx := setup(t)
	bad := pack("p", models.BundleFile{Path: "x.loom", FileType: "script", Content: "x"})
	if err := upsert(ctx, bad); err == nil {
		t.Error("file_type outside prompt|block|overlay|meta must be rejected by the schema")
	}
}

func TestListOrderingAndCounts(t *testing.T) {
	ctx := setup(t)
	items, err := store.ListVaults(ctx)
	if err != nil || len(items) != 0 {
		t.Fatalf("empty list: %v %v", items, err)
	}
	two := pack("zeta", models.BundleFile{Path: "a.prompt.loom", FileType: "prompt", Content: "1"},
		models.BundleFile{Path: "b.prompt.loom", FileType: "prompt", Content: "2"})
	two.Name = "Alpha"
	one := pack("alpha")
	one.Name = "Zulu"
	for _, b := range []*models.Bundle{one, two} {
		if err := upsert(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	items, err = store.ListVaults(ctx)
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	if items[0].Name != "Alpha" || items[0].FileCount != 2 || items[1].Name != "Zulu" || items[1].FileCount != 1 {
		t.Errorf("ordering/counts wrong: %+v", items)
	}
}

func TestMissingPackReturnsNil(t *testing.T) {
	ctx := setup(t)
	if v, err := store.GetVault(ctx, "nope"); v != nil || err != nil {
		t.Errorf("GetVault: %v %v", v, err)
	}
	if b, err := store.GetBundle(ctx, "nope"); b != nil || err != nil {
		t.Errorf("GetBundle: %v %v", b, err)
	}
}

func TestDeleteCascadesFiles(t *testing.T) {
	ctx := setup(t)
	if err := upsert(ctx, pack("p")); err != nil {
		t.Fatal(err)
	}
	if err := del(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if v, _ := store.GetVault(ctx, "p"); v != nil {
		t.Error("pack still exists")
	}
	var n int
	if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM vault_files`).Scan(&n); err != nil || n != 0 {
		t.Errorf("orphaned files: %d (%v)", n, err)
	}
	if err := del(ctx, "p"); err != nil {
		t.Errorf("deleting a missing pack should not error: %v", err)
	}
}

func TestSQLInjectionInSlugIsHarmless(t *testing.T) {
	ctx := setup(t)
	if err := upsert(ctx, pack("victim")); err != nil {
		t.Fatal(err)
	}
	// Handlers reject such slugs, but the store must be safe on its own too.
	if _, err := store.GetVault(ctx, "x' OR '1'='1"); err != nil {
		t.Fatal(err)
	}
	if err := del(ctx, "x'; DROP TABLE vaults; --"); err != nil {
		t.Fatal(err)
	}
	if v, _ := store.GetVault(ctx, "victim"); v == nil {
		t.Error("data was affected by an injection attempt")
	}
}

// Ownership is enforced inside the SQL statement itself.
func TestOwnership(t *testing.T) {
	ctx := setup(t)
	alice := models.Identity{Name: "alice"}
	bob := models.Identity{Name: "bob"}

	if err := store.UpsertVault(ctx, pack("kit"), alice); err != nil {
		t.Fatal(err)
	}
	v, _ := store.GetVault(ctx, "kit")
	if v.Owner != "alice" {
		t.Errorf("owner = %q", v.Owner)
	}
	if items, _ := store.ListVaults(ctx); len(items) != 1 || items[0].Owner != "alice" {
		t.Errorf("list owner: %+v", items)
	}

	changed := pack("kit")
	changed.Version = "2.0.0"
	if err := store.UpsertVault(ctx, changed, bob); !errors.Is(err, models.ErrNotOwner) {
		t.Errorf("bob replacing alice's pack: %v", err)
	}
	if v, _ := store.GetVault(ctx, "kit"); v.Version != "1.0.0" {
		t.Error("the pack was changed by a non-owner")
	}
	if deleted, err := store.DeleteVault(ctx, "kit", bob); deleted || !errors.Is(err, models.ErrNotOwner) {
		t.Errorf("bob deleting: %v %v", deleted, err)
	}

	if err := store.UpsertVault(ctx, changed, alice); err != nil {
		t.Errorf("owner update: %v", err)
	}
	if err := store.UpsertVault(ctx, pack("kit"), admin); err != nil {
		t.Errorf("admin update: %v", err)
	}
	if v, _ := store.GetVault(ctx, "kit"); v.Owner != "alice" {
		t.Errorf("an admin update must not take the pack over, owner = %q", v.Owner)
	}

	// packs that predate ownership (empty owner) belong to admins only
	if _, err := db.Pool.Exec(ctx, `UPDATE vaults SET owner = '' WHERE slug = 'kit'`); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertVault(ctx, changed, alice); !errors.Is(err, models.ErrNotOwner) {
		t.Errorf("legacy pack changed by a publisher: %v", err)
	}
	if err := store.UpsertVault(ctx, changed, admin); err != nil {
		t.Errorf("legacy pack changed by an admin: %v", err)
	}

	// an owner can delete; a missing pack is (false, nil)
	if err := store.UpsertVault(ctx, pack("mine"), bob); err != nil {
		t.Fatal(err)
	}
	if deleted, err := store.DeleteVault(ctx, "mine", bob); !deleted || err != nil {
		t.Errorf("owner delete: %v %v", deleted, err)
	}
	if deleted, err := store.DeleteVault(ctx, "mine", bob); deleted || err != nil {
		t.Errorf("deleting a missing pack: %v %v", deleted, err)
	}
}

// Two publishers racing for the same NEW slug: exactly one wins, the other is refused.
func TestOwnershipRace(t *testing.T) {
	ctx := setup(t)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b := pack("contested")
			b.Version = fmt.Sprintf("1.0.%d", i)
			results <- store.UpsertVault(ctx, b, models.Identity{Name: fmt.Sprintf("p%d", i)})
		}(i)
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, models.ErrNotOwner) {
			t.Errorf("unexpected error: %v", err)
		}
	}
	// the first to insert owns it; publishers that lost only succeed if they are that owner
	v, _ := store.GetVault(ctx, "contested")
	if v == nil || v.Owner == "" || wins != 1 {
		t.Errorf("owner=%v wins=%d, want exactly one winner", v, wins)
	}
}
