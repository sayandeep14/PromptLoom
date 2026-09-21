package validate

import (
	"strings"
	"testing"

	"github.com/sayandeepgiri/promptloom/server/internal/models"
)

func TestSlug(t *testing.T) {
	for _, s := range []string{"go-backend", "a", "python_backend2", "x1"} {
		if err := Slug(s); err != nil {
			t.Errorf("%q should be valid: %v", s, err)
		}
	}
	for _, s := range []string{"", "Go-Backend", "-lead", "has space", "a/b", "..", "a.b", "é", strings.Repeat("a", 65), "a;drop"} {
		if err := Slug(s); err == nil {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestFilePath(t *testing.T) {
	for _, p := range []string{"prompts/A.prompt.loom", ".dependency.loom", "loom.toml", "blocks/x.block.loom", "a/b/c.txt"} {
		if err := FilePath(p); err != nil {
			t.Errorf("%q should be valid: %v", p, err)
		}
	}
	bad := []string{
		"", "/etc/passwd", "../evil", "a/../../evil", "a/..", "./a", "a//b", "a/./b",
		`a\b`, `..\evil`, "C:/x", "a b", "a\x00b", "trailing/", strings.Repeat("a", 300),
	}
	for _, p := range bad {
		if err := FilePath(p); err == nil {
			t.Errorf("%q should be rejected", p)
		}
	}
}

func validBundle() *models.Bundle {
	return &models.Bundle{
		Name: "Go Backend", Slug: "go-backend", Version: "1.0.0",
		Files: []models.BundleFile{{Path: "prompts/A.prompt.loom", FileType: "prompt", Content: "prompt A {}"}},
	}
}

func TestBundleValid(t *testing.T) {
	b := validBundle()
	b.PackID = "550e8400-e29b-41d4-a716-446655440000"
	b.Version = "1.2.3-beta.1+build5"
	if errs := Bundle(b); len(errs) != 0 {
		t.Fatalf("expected valid, got %v", errs)
	}
}

func TestBundleRejects(t *testing.T) {
	cases := map[string]func(*models.Bundle){
		"bad slug":        func(b *models.Bundle) { b.Slug = "../x" },
		"bad version":     func(b *models.Bundle) { b.Version = "v1" },
		"empty version":   func(b *models.Bundle) { b.Version = "" },
		"bad pack id":     func(b *models.Bundle) { b.PackID = "not-a-uuid" },
		"no name":         func(b *models.Bundle) { b.Name = "  " },
		"long name":       func(b *models.Bundle) { b.Name = strings.Repeat("n", 101) },
		"control chars":   func(b *models.Bundle) { b.Author = "a\x07b" },
		"no files":        func(b *models.Bundle) { b.Files = nil },
		"traversal path":  func(b *models.Bundle) { b.Files[0].Path = "../../.ssh/authorized_keys" },
		"absolute path":   func(b *models.Bundle) { b.Files[0].Path = "/etc/cron.d/x" },
		"bad file type":   func(b *models.Bundle) { b.Files[0].FileType = "script" },
		"nul in content":  func(b *models.Bundle) { b.Files[0].Content = "a\x00b" },
		"oversize file":   func(b *models.Bundle) { b.Files[0].Content = strings.Repeat("x", MaxFileBytes+1) },
		"invalid utf8":    func(b *models.Bundle) { b.Files[0].Content = "\xff\xfe" },
		"duplicate paths": func(b *models.Bundle) { b.Files = append(b.Files, b.Files[0]) },
		"too many tags":   func(b *models.Bundle) { b.Tags = make([]string, MaxTags+1) },
		"empty tag":       func(b *models.Bundle) { b.Tags = []string{""} },
		"too many files":  func(b *models.Bundle) { b.Files = manyFiles(MaxFiles + 1) },
	}
	for name, mutate := range cases {
		b := validBundle()
		mutate(b)
		if errs := Bundle(b); len(errs) == 0 {
			t.Errorf("%s: expected validation errors, got none", name)
		}
	}
}

func TestBundleErrorListIsCapped(t *testing.T) {
	b := validBundle()
	b.Files = nil
	for i := 0; i < 100; i++ {
		b.Files = append(b.Files, models.BundleFile{Path: "../x", FileType: "prompt"})
	}
	if errs := Bundle(b); len(errs) > 21 {
		t.Errorf("error list not capped: %d entries", len(errs))
	}
}

func manyFiles(n int) []models.BundleFile {
	out := make([]models.BundleFile, n)
	for i := range out {
		out[i] = models.BundleFile{Path: "prompts/p" + strings.Repeat("a", i%50) + string(rune('a'+i%26)) + ".prompt.loom", FileType: "prompt"}
	}
	return out
}
