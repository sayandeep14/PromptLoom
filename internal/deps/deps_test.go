package deps_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/deps"
)

// ---- ParseContent ----

func TestParseContentBasic(t *testing.T) {
	content := `
# comment
go-backend==1.0.0
python>=2.0.0
webdev
`
	d, err := deps.ParseContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(d) != 3 {
		t.Fatalf("expected 3 deps, got %d: %v", len(d), d)
	}
	assertDep(t, d[0], "go-backend", "==", "1.0.0", "")
	assertDep(t, d[1], "python", ">=", "2.0.0", "")
	assertDep(t, d[2], "webdev", "", "", "")
}

func TestParseContentAlias(t *testing.T) {
	content := `webdev==0.0.1 as dev`
	d, err := deps.ParseContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(d) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(d))
	}
	assertDep(t, d[0], "webdev", "==", "0.0.1", "dev")
}

func TestParseContentNameOnlyAlias(t *testing.T) {
	content := `mypack as mp`
	d, err := deps.ParseContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(d) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(d))
	}
	if d[0].Alias != "mp" {
		t.Errorf("expected alias 'mp', got %q", d[0].Alias)
	}
}

func TestParseContentInvalidLine(t *testing.T) {
	_, err := deps.ParseContent(`@invalid`)
	if err == nil {
		t.Fatal("expected error for invalid line")
	}
}

// ---- ParseFile ----

func TestParseFileRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, deps.Filename)
	content := "backend>=1.0.0\nfrontend==2.3.0 as fe\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := deps.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(d) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(d))
	}
	assertDep(t, d[1], "frontend", "==", "2.3.0", "fe")
}

func assertDep(t *testing.T, d deps.Dependency, name, op, ver, alias string) {
	t.Helper()
	if d.Name != name {
		t.Errorf("name: expected %q got %q", name, d.Name)
	}
	if d.Op != op {
		t.Errorf("op: expected %q got %q", op, d.Op)
	}
	if d.Version != ver {
		t.Errorf("version: expected %q got %q", ver, d.Version)
	}
	if d.Alias != alias {
		t.Errorf("alias: expected %q got %q", alias, d.Alias)
	}
}

// ---- version constraint ----

func TestSatisfiesExact(t *testing.T) {
	assertSatisfies(t, "1.0.0", "==", "1.0.0", true)
	assertSatisfies(t, "1.0.1", "==", "1.0.0", false)
}

func TestSatisfiesGTE(t *testing.T) {
	assertSatisfies(t, "2.0.0", ">=", "1.0.0", true)
	assertSatisfies(t, "1.0.0", ">=", "1.0.0", true)
	assertSatisfies(t, "0.9.0", ">=", "1.0.0", false)
}

func TestSatisfiesGT(t *testing.T) {
	assertSatisfies(t, "1.0.1", ">", "1.0.0", true)
	assertSatisfies(t, "1.0.0", ">", "1.0.0", false)
}

func TestSatisfiesLTE(t *testing.T) {
	assertSatisfies(t, "1.0.0", "<=", "2.0.0", true)
	assertSatisfies(t, "2.0.0", "<=", "2.0.0", true)
	assertSatisfies(t, "2.0.1", "<=", "2.0.0", false)
}

func TestSatisfiesLT(t *testing.T) {
	assertSatisfies(t, "1.9.9", "<", "2.0.0", true)
	assertSatisfies(t, "2.0.0", "<", "2.0.0", false)
}

func TestSatisfiesCompatible(t *testing.T) {
	// ~=1.4.2: installed must be >=1.4.2 and same major.minor (1.4.x)
	assertSatisfies(t, "1.4.2", "~=", "1.4.2", true)
	assertSatisfies(t, "1.4.9", "~=", "1.4.2", true)
	assertSatisfies(t, "1.5.0", "~=", "1.4.2", false) // minor bump
	assertSatisfies(t, "2.0.0", "~=", "1.4.2", false) // major bump
	assertSatisfies(t, "1.4.1", "~=", "1.4.2", false) // older patch
}

func TestSatisfiesAnyVersion(t *testing.T) {
	assertSatisfies(t, "99.0.0", "", "", true)
}

func assertSatisfies(t *testing.T, installed, op, required string, want bool) {
	t.Helper()
	got := deps.Satisfies(installed, op, required)
	if got != want {
		t.Errorf("Satisfies(%q, %q, %q) = %v, want %v", installed, op, required, got, want)
	}
}

// ---- PackLock read/write ----

func TestPackLockRoundtrip(t *testing.T) {
	dir := t.TempDir()
	pl := &deps.PackLock{}
	pl.Upsert(deps.PackLockEntry{Slug: "go-backend", Version: "1.0.0"})
	pl.Upsert(deps.PackLockEntry{Slug: "python", Version: "2.1.0", RequiredBy: []string{"go-backend"}})

	if err := deps.WritePackLock(pl, dir); err != nil {
		t.Fatalf("WritePackLock: %v", err)
	}

	pl2, err := deps.ReadPackLock(dir)
	if err != nil {
		t.Fatalf("ReadPackLock: %v", err)
	}
	if len(pl2.Packs) != 2 {
		t.Fatalf("expected 2 packs, got %d", len(pl2.Packs))
	}

	goEntry := pl2.Find("go-backend")
	if goEntry == nil || goEntry.Version != "1.0.0" {
		t.Errorf("go-backend entry: %+v", goEntry)
	}
	pyEntry := pl2.Find("python")
	if pyEntry == nil || len(pyEntry.RequiredBy) != 1 || pyEntry.RequiredBy[0] != "go-backend" {
		t.Errorf("python entry: %+v", pyEntry)
	}
}

func TestPackLockUpsertUpdates(t *testing.T) {
	pl := &deps.PackLock{}
	pl.Upsert(deps.PackLockEntry{Slug: "alpha", Version: "1.0.0"})
	pl.Upsert(deps.PackLockEntry{Slug: "alpha", Version: "2.0.0"})
	if len(pl.Packs) != 1 {
		t.Fatalf("expected 1 pack after upsert, got %d", len(pl.Packs))
	}
	if pl.Packs[0].Version != "2.0.0" {
		t.Errorf("expected updated version 2.0.0, got %q", pl.Packs[0].Version)
	}
}

func TestPackLockMissingFile(t *testing.T) {
	dir := t.TempDir()
	pl, err := deps.ReadPackLock(dir)
	if err != nil {
		t.Fatalf("ReadPackLock on missing file: %v", err)
	}
	if len(pl.Packs) != 0 {
		t.Errorf("expected empty PackLock, got %v", pl.Packs)
	}
}
