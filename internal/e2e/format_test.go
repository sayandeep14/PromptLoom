package e2e

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/format"
	"github.com/sayandeep14/PromptLoom/internal/loader"
)

// `loom fmt` must never change what a project means or lose anything the author
// wrote. For every valid fixture: format every source file, then weave again and
// compare with the SAME golden files the unformatted project produced.

func sourceFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && loader.IsLoomSource(p) && !strings.HasSuffix(p, ".vars.loom") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func commentLines(src string) []string {
	var out []string
	for _, l := range strings.Split(src, "\n") {
		if tr := strings.TrimSpace(l); strings.HasPrefix(tr, "//") {
			out = append(out, tr)
		}
	}
	sort.Strings(out)
	return out
}

func TestFormatPreservesMeaningAndComments(t *testing.T) {
	for _, name := range cases(t, "valid") {
		name := name
		t.Run(name, func(t *testing.T) {
			src := filepath.Join(testdataDir, "valid", name)
			dir := stage(t, src)

			for _, f := range sourceFiles(t, dir) {
				before, err := os.ReadFile(f)
				if err != nil {
					t.Fatal(err)
				}
				after, err := format.Source(f, string(before))
				if err != nil {
					t.Fatalf("format %s: %v", f, err)
				}
				// nothing the author wrote may disappear
				if got, want := strings.Join(commentLines(after), "\n"), strings.Join(commentLines(string(before)), "\n"); got != want {
					t.Errorf("%s: comments changed\n--- before\n%s\n--- after\n%s", filepath.Base(f), want, got)
				}
				// idempotent
				again, err := format.Source(f, after)
				if err != nil || again != after {
					t.Errorf("%s: formatting is not idempotent (err=%v)\n--- once\n%s\n--- twice\n%s", filepath.Base(f), err, after, again)
				}
				if err := os.WriteFile(f, []byte(after), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			reg, cfg, _, err := load(t, dir)
			if err != nil {
				t.Fatalf("formatted project no longer loads: %v", err)
			}
			specs := readWeave(t, src)
			if len(specs) == 0 {
				for _, n := range reg.Prompts() {
					specs = append(specs, weaveSpec{name: n.Name, prompt: n.Name})
				}
			}
			for _, w := range specs {
				if w.opts.Variables == nil {
					w.opts.Variables = map[string]string{}
				}
				out, _, err := weave(reg, cfg, w)
				if err != nil {
					t.Errorf("weave %s after fmt: %v", w.name, err)
					continue
				}
				golden := filepath.Join(src, "golden", w.name)
				if filepath.Ext(w.name) == "" {
					golden += ".md"
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Errorf("missing golden %s", golden)
					continue
				}
				if string(want) != out {
					t.Errorf("formatting changed the output of %s\n--- want\n%s\n--- got\n%s", w.name, want, out)
				}
			}
		})
	}
}
