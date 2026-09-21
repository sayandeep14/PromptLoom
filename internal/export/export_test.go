package export

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func parse(t *testing.T, src string) ([]Rule, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), Filename)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return ParseFile(p)
}

func TestParseRules(t *testing.T) {
	rules, err := parse(t, "# header\n\nexport `prompts`\nexport `pkg.sub` match `*-final.prompt.loom`\n"+
		"export `a` except match `*-temp.prompt.loom`\nexport `b` match `*.prompt.loom` except `skip.prompt.loom`\n"+
		"export `c` match `x*` except match `*y`\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Rule{
		{Package: "prompts", Line: 3},
		{Package: "pkg.sub", MatchGlob: "*-final.prompt.loom", Line: 4},
		{Package: "a", ExceptGlob: "*-temp.prompt.loom", Line: 5},
		{Package: "b", MatchGlob: "*.prompt.loom", ExceptFile: "skip.prompt.loom", Line: 6},
		{Package: "c", MatchGlob: "x*", ExceptGlob: "*y", Line: 7},
	}
	if len(rules) != len(want) {
		t.Fatalf("got %d rules: %+v", len(rules), rules)
	}
	for i := range want {
		if rules[i] != want[i] {
			t.Errorf("rule %d = %+v, want %+v", i, rules[i], want[i])
		}
	}
}

func TestParseErrorsNameTheLine(t *testing.T) {
	cases := map[string]string{
		"nonsense":                   "expected 'export'",
		"export":                     "backtick",
		"export pkg":                 "backtick",
		"export `p` mach `*.loom`":   "unexpected word \"mach\"", // a typo must not silently export everything
		"export `p` excpet `x.loom`": "unexpected word",
		"export `p` and also `x`":    "unexpected word",
	}
	for line, want := range cases {
		_, err := parse(t, "# c\n"+line+"\n")
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), ".export.loom:2") {
			t.Errorf("%q: want an error containing %q with a line number, got %v", line, want, err)
		}
	}
	if _, err := ParseFile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a missing file is an error")
	}
}

func tree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range []string{
		"a.prompt.loom", "b-final.prompt.loom", "c-temp.prompt.loom", "notes.txt", "helper.block.loom", "x.loom",
		"sub/d.prompt.loom", "sub/deep/e-final.prompt.loom", "other/f.prompt.loom",
	} {
		p := filepath.Join(dir, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	return dir
}

func names(t *testing.T, base string, paths []string) string {
	t.Helper()
	var out []string
	for _, p := range paths {
		rel, _ := filepath.Rel(base, p)
		out = append(out, filepath.ToSlash(rel))
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func TestMatch(t *testing.T) {
	base := tree(t)
	cases := []struct {
		rule Rule
		want string
	}{
		{Rule{Package: "sub"}, "sub/d.prompt.loom,sub/deep/e-final.prompt.loom"},
		{Rule{Package: "sub", MatchGlob: "*-final.prompt.loom"}, "sub/deep/e-final.prompt.loom"},
		{Rule{Package: "sub.deep"}, "sub/deep/e-final.prompt.loom"},
		{Rule{Package: "other"}, "other/f.prompt.loom"},
		{Rule{Package: "sub", ExceptGlob: "*-final.prompt.loom"}, "sub/d.prompt.loom"},
		{Rule{Package: "sub", ExceptFile: "d.prompt.loom"}, "sub/deep/e-final.prompt.loom"},
		{Rule{Package: "does.not.exist"}, ""},
	}
	for _, c := range cases {
		got, err := c.rule.Match(base)
		if err != nil {
			t.Fatal(err)
		}
		if names(t, base, got) != c.want {
			t.Errorf("%+v:\n got  %s\n want %s", c.rule, names(t, base, got), c.want)
		}
	}
}

// A package path is a directory path made of dot-separated names; it can never reach
// outside the base directory.
func TestMatchStaysInsideTheBaseDirectory(t *testing.T) {
	outer := t.TempDir()
	os.WriteFile(filepath.Join(outer, "secret.prompt.loom"), []byte("x"), 0o644)
	base := filepath.Join(outer, "base")
	os.MkdirAll(base, 0o755)
	os.WriteFile(filepath.Join(base, "ok.prompt.loom"), []byte("x"), 0o644)
	for _, pkg := range []string{"..", "...", "a..b", "../..", "/etc"} {
		r := Rule{Package: pkg}
		got, _ := r.Match(base)
		for _, p := range got {
			if rel, _ := filepath.Rel(base, p); strings.HasPrefix(rel, "..") {
				t.Errorf("package %q matched a file outside the base: %s", pkg, p)
			}
		}
	}
}

func TestWriteDefaultIsParseable(t *testing.T) {
	p := filepath.Join(t.TempDir(), Filename)
	if err := WriteDefault(p); err != nil {
		t.Fatal(err)
	}
	rules, err := ParseFile(p)
	if err != nil || len(rules) != 1 || rules[0].Package != "prompts" {
		t.Errorf("%+v %v", rules, err)
	}
}

func TestMalformedClauses(t *testing.T) {
	for _, line := range []string{
		"export `p` match", "export `p` match except", "export `p` except", "export `p` except match",
		"export `p` match `a` match `b`", "export `p` except `a` except `b`", "export `p` except match `a` except match `b`",
		"export `p` `q`", "export match `x`",
	} {
		if _, err := parse(t, line+"\n"); err == nil {
			t.Errorf("%q must be rejected", line)
		}
	}
	// keywords are case-insensitive, like the rest of the syntax
	if r, err := parse(t, "export `p` MATCH `a` EXCEPT `b`\n"); err != nil || r[0].MatchGlob != "a" || r[0].ExceptFile != "b" {
		t.Errorf("%+v %v", r, err)
	}
}
