package packmetadata

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewIDIsAUUIDv4AndUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := NewID()
		if !uuidV4.MatchString(id) {
			t.Fatalf("%q is not a UUID v4", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id after %d draws", i)
		}
		seen[id] = true
	}
}

func TestWriteReadRoundTripAndIDIsKept(t *testing.T) {
	dir := t.TempDir()
	m := &Metadata{Slug: "go-backend", Version: "1.2.3", Name: "Go Backend", Author: "me",
		Tags: []string{"go"}, RelatedLibraries: []RelatedLibrary{{Name: "gin", Version: "1.9.0", ID: "x"}}}
	if err := Write(dir, m); err != nil {
		t.Fatal(err)
	}
	if !uuidV4.MatchString(m.ID) {
		t.Errorf("Write must assign an id when missing: %q", m.ID)
	}
	got, err := Read(dir)
	if err != nil || got.Slug != "go-backend" || got.Version != "1.2.3" || got.ID != m.ID || len(got.RelatedLibraries) != 1 {
		t.Errorf("%+v %v", got, err)
	}
	// re-writing keeps the same id (an id belongs to a slug+version)
	if err := Write(dir, got); err != nil {
		t.Fatal(err)
	}
	again, _ := Read(dir)
	if again.ID != m.ID {
		t.Error("the id must be preserved across re-writes")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, Filename))
	if !strings.HasSuffix(string(raw), "}\n") || !strings.Contains(string(raw), "\n  \"slug\"") {
		t.Errorf("expected indented JSON with a trailing newline:\n%s", raw)
	}
}

func TestReadErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Read(dir); err == nil {
		t.Error("missing file")
	}
	os.WriteFile(filepath.Join(dir, Filename), []byte("{ not json"), 0o644)
	if _, err := Read(dir); err == nil || !strings.Contains(err.Error(), Filename) {
		t.Errorf("bad JSON should name the file: %v", err)
	}
}

func TestValidate(t *testing.T) {
	ok := Metadata{Slug: "a", Version: "1", Name: "A"}
	if err := ok.Validate(); err != nil {
		t.Error(err)
	}
	for field, m := range map[string]Metadata{
		"slug": {Version: "1", Name: "A"}, "name": {Slug: "a", Version: "1"}, "version": {Slug: "a", Name: "A"},
	} {
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("missing %s: %v", field, err)
		}
	}
}
