package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildLoom compiles the real binary once per test that needs it.
func buildLoom(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "loom")
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/loom")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build loom: %v\n%s", err, out)
	}
	return bin
}

func runLoom(t *testing.T, bin, dir string, args ...string) (stdout string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code = 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run loom %v: %v", args, err)
	}
	return out.String() + errb.String(), code
}

func TestCLI(t *testing.T) {
	bin := buildLoom(t)

	t.Run("inspect passes on a valid project", func(t *testing.T) {
		dir := stage(t, filepath.Join(testdataDir, "valid", "01-minimal"))
		out, code := runLoom(t, bin, dir, "inspect")
		if code != 0 || !strings.Contains(out, "Library is clean") {
			t.Errorf("exit %d\n%s", code, out)
		}
	})

	t.Run("inspect fails with a helpful message on an invalid project", func(t *testing.T) {
		dir := stage(t, filepath.Join(testdataDir, "invalid", "unknown-parent"))
		out, code := runLoom(t, bin, dir, "inspect")
		if code != 1 {
			t.Errorf("exit %d, want 1", code)
		}
		for _, want := range []string{"inherits unknown prompt", "P.prompt.loom", "Did you mean"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("inspect reports parse errors with a position", func(t *testing.T) {
		dir := stage(t, filepath.Join(testdataDir, "invalid", "parse-unterminated"))
		out, code := runLoom(t, bin, dir, "inspect")
		if code != 1 || !strings.Contains(out, "closing '}'") || !strings.Contains(out, ".prompt.loom:") {
			t.Errorf("exit %d\n%s", code, out)
		}
	})

	t.Run("weave --stdout matches the golden file", func(t *testing.T) {
		src := filepath.Join(testdataDir, "valid", "02-inheritance-single")
		dir := stage(t, src)
		out, code := runLoom(t, bin, dir, "weave", "CodeReviewer", "--stdout")
		if code != 0 {
			t.Fatalf("exit %d\n%s", code, out)
		}
		want, err := os.ReadFile(filepath.Join(src, "golden", "CodeReviewer.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, strings.TrimSpace(string(want))) {
			t.Errorf("stdout does not contain the golden output\n--- want\n%s\n--- got\n%s", want, out)
		}
	})

	t.Run("weave --all writes one file per prompt", func(t *testing.T) {
		dir := stage(t, filepath.Join(testdataDir, "valid", "03-multi-parent-from-all"))
		if out, code := runLoom(t, bin, dir, "weave", "--all"); code != 0 {
			t.Fatalf("exit %d\n%s", code, out)
		}
		for _, name := range []string{"Backend", "Frontend", "FullStack"} {
			if _, err := os.Stat(filepath.Join(dir, "dist", name+".md")); err != nil {
				// the output directory comes from loom.toml [paths] out
				matches, _ := filepath.Glob(filepath.Join(dir, "dist", "*", name+".md"))
				if len(matches) == 0 {
					t.Errorf("no rendered file for %s under dist/", name)
				}
			}
		}
	})

	t.Run("weave of a missing prompt fails", func(t *testing.T) {
		dir := stage(t, filepath.Join(testdataDir, "valid", "01-minimal"))
		out, code := runLoom(t, bin, dir, "weave", "Nope", "--stdout")
		if code == 0 {
			t.Errorf("expected a non-zero exit\n%s", out)
		}
	})

	t.Run("fmt then inspect stays clean", func(t *testing.T) {
		dir := stage(t, filepath.Join(testdataDir, "valid", "07-variants"))
		if out, code := runLoom(t, bin, dir, "fmt"); code != 0 {
			t.Fatalf("fmt exit %d\n%s", code, out)
		}
		if out, code := runLoom(t, bin, dir, "inspect"); code != 0 {
			t.Errorf("inspect after fmt exit %d\n%s", code, out)
		}
	})
}
