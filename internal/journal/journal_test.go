package journal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAddAndList(t *testing.T) {
	dir := t.TempDir()
	path, err := Add(dir, "Tightened the reviewer", "CodeReviewer", "ana", "Details about why.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "_tightened-the-reviewer.md") {
		t.Errorf("file name: %s", path)
	}
	entries, err := List(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("%v %v", entries, err)
	}
	e := entries[0]
	if e.Message != "Tightened the reviewer" || e.Prompt != "CodeReviewer" || e.Author != "ana" || e.Body == "" ||
		!strings.Contains(e.Body, "Details about why.") || e.Date.IsZero() || e.Slug != "tightened-the-reviewer" {
		t.Errorf("%+v", e)
	}
	if time.Since(e.Date) > time.Minute {
		t.Errorf("date should be now: %v", e.Date)
	}
}

// Two notes with the same title on the same day used to overwrite each other.
func TestSameTitleSameDayDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for _, body := range []string{"first", "second", "third"} {
		p, err := Add(dir, "Weekly tweak", "", "", body)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	if paths[0] == paths[1] || paths[1] == paths[2] || paths[0] == paths[2] {
		t.Fatalf("paths collide: %v", paths)
	}
	entries, _ := List(dir)
	if len(entries) != 3 {
		t.Fatalf("history was lost: %d entries", len(entries))
	}
	all := ""
	for _, e := range entries {
		all += e.Body
	}
	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(all, want) {
			t.Errorf("entry %q was overwritten", want)
		}
	}
}

// A newline in a field must not be able to add front-matter fields of its own.
func TestNewlinesCannotInjectFrontMatter(t *testing.T) {
	dir := t.TempDir()
	if _, err := Add(dir, "Title\nprompt: Injected", "Real\nauthor: mallory", "ana\ndate: 1999-01-01T00:00:00Z", ""); err != nil {
		t.Fatal(err)
	}
	entries, _ := List(dir)
	e := entries[0]
	if e.Prompt != "Real author: mallory" || e.Author != "ana date: 1999-01-01T00:00:00Z" {
		t.Errorf("fields must stay on one line: prompt=%q author=%q", e.Prompt, e.Author)
	}
	if e.Date.Year() == 1999 {
		t.Error("the date was forged")
	}
	if e.Message != "Title prompt: Injected" {
		t.Errorf("message: %q", e.Message)
	}
}

func TestListOrderIsNewestFirstAndStable(t *testing.T) {
	dir := t.TempDir()
	jd := filepath.Join(dir, journalDir)
	os.MkdirAll(jd, 0o755)
	for name, date := range map[string]string{
		"2026-01-01_old.md": "2026-01-01T10:00:00Z", "2026-03-01_new.md": "2026-03-01T10:00:00Z", "2026-02-01_mid.md": "2026-02-01T10:00:00Z",
	} {
		os.WriteFile(filepath.Join(jd, name), []byte("---\ndate: "+date+"\n---\n\n# "+name+"\n"), 0o644)
	}
	entries, _ := List(dir)
	var got []string
	for _, e := range entries {
		got = append(got, e.Slug)
	}
	if strings.Join(got, ",") != "new,mid,old" {
		t.Errorf("%v", got)
	}
}

func TestListSkipsNonEntriesAndHandlesAMissingDirectory(t *testing.T) {
	dir := t.TempDir()
	if got, err := List(dir); err != nil || got != nil {
		t.Errorf("no journal yet: %v %v", got, err)
	}
	jd := filepath.Join(dir, journalDir)
	os.MkdirAll(filepath.Join(jd, "subdir.md"), 0o755)
	os.WriteFile(filepath.Join(jd, "notes.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(jd, "2026-01-01_ok.md"), []byte("# ok\n"), 0o644)
	if got, _ := List(dir); len(got) != 1 {
		t.Errorf("only .md files count: %+v", got)
	}
}

func TestParseEntryWithoutFrontMatter(t *testing.T) {
	e := parseEntry("just a body, no heading", "/j/2026-05-06_fallback-title.md")
	if e.Message != "fallback title" || e.Slug != "fallback-title" || !e.Date.IsZero() {
		t.Errorf("%+v", e)
	}
	e = parseEntry("---\nprompt: P\n---\n\n# Heading\n\nbody", "/j/2026-05-06_x.md")
	if e.Prompt != "P" || e.Message != "Heading" {
		t.Errorf("%+v", e)
	}
	// a broken date does not lose the entry
	e = parseEntry("---\ndate: not-a-date\n---\n# t\n", "/j/2026-05-06_x.md")
	if !e.Date.IsZero() || e.Message != "t" {
		t.Errorf("%+v", e)
	}
}

func TestForPromptIsCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	Add(dir, "one", "CodeReviewer", "", "")
	Add(dir, "two", "Other", "", "")
	got, err := ForPrompt(dir, "codereviewer")
	if err != nil || len(got) != 1 || got[0].Message != "one" {
		t.Errorf("%+v %v", got, err)
	}
}

func TestMakeSlug(t *testing.T) {
	cases := map[string]string{
		"Hello, World!": "hello-world", "  spaced   out  ": "spaced-out", "": "entry", "!!!": "entry",
		"日本語": "entry", "UPPER lower 123": "upper-lower-123",
		"a-very-long-title-that-goes-on-and-on-and-on-forever": "a-very-long-title-that-goes-on-and-on-an",
		"exactly forty characters here-and-x-y-z ok":           "exactly-forty-characters-here-and-x-y-z",
	}
	for in, want := range cases {
		got := makeSlug(in)
		if got != want {
			t.Errorf("makeSlug(%q) = %q, want %q", in, got, want)
		}
		if len(got) > 40 || strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
			t.Errorf("makeSlug(%q) = %q is not a clean slug", in, got)
		}
	}
}
