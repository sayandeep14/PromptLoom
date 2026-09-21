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

func TestCLIFmt(t *testing.T) {
	bin := buildLoom(t)
	src := filepath.Join(testdataDir, "valid", "18-comments-env-secret")

	t.Run("keeps comments, env blocks and secret slots", func(t *testing.T) {
		dir := stage(t, src)
		file := filepath.Join(dir, "prompts", "Annotated.prompt.loom")
		before, _ := os.ReadFile(file)

		if out, code := runLoom(t, bin, dir, "fmt"); code != 0 {
			t.Fatalf("fmt exit %d\n%s", code, out)
		}
		after, _ := os.ReadFile(file)
		for _, want := range []string{
			"// ── Annotated reviewer", "// only in strict mode", "// never log secrets",
			"env prod {", "Use timeouts on every external call.", "slot api_key { required: true, secret: true }",
			"slot team { required: false }", "// trailing note: revisit after the Q3 review", "// end of file",
		} {
			if !strings.Contains(string(after), want) {
				t.Errorf("formatting lost %q:\n%s", want, after)
			}
		}
		if strings.Count(string(before), "//") != strings.Count(string(after), "//") {
			t.Errorf("comment count changed: %d -> %d", strings.Count(string(before), "//"), strings.Count(string(after), "//"))
		}
		// the project still inspects...
		if out, code := runLoom(t, bin, dir, "inspect"); code != 0 {
			t.Errorf("inspect after fmt: exit %d\n%s", code, out)
		}
		// ...and the secret slot is STILL secret: the CLI refuses to take its value on the
		// command line. (Before the formatter kept `secret: true`, this succeeded.)
		out, code := runLoom(t, bin, dir, "weave", "Annotated", "--set", "api_key=hunter2", "--set", "team=x", "--stdout")
		if code == 0 || !strings.Contains(out, "marked secret") || strings.Contains(out, "hunter2") {
			t.Errorf("secret slot lost its protection after fmt (exit %d):\n%s", code, out)
		}
	})

	t.Run("env blocks still apply after fmt", func(t *testing.T) {
		dir := stage(t, filepath.Join(testdataDir, "valid", "08-env"))
		if out, code := runLoom(t, bin, dir, "fmt"); code != 0 {
			t.Fatalf("fmt exit %d\n%s", code, out)
		}
		out, code := runLoom(t, bin, dir, "weave", "Pipeline", "--env", "prod", "--stdout")
		if code != 0 || !strings.Contains(out, "All external calls must use timeouts.") {
			t.Errorf("env block lost or broken after fmt (exit %d):\n%s", code, out)
		}
	})

	t.Run("--check exits 1 until the project is formatted", func(t *testing.T) {
		dir := stage(t, filepath.Join(testdataDir, "valid", "02-inheritance-single"))
		file := filepath.Join(dir, "prompts", "Base.prompt.loom")
		messy := "prompt   BaseEngineer   {\n\n\n  persona :=\n    You are a senior engineer.\n}\n"
		if err := os.WriteFile(file, []byte(messy), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, code := runLoom(t, bin, dir, "fmt", "--check"); code != 1 || !strings.Contains(out, "needs formatting") {
			t.Fatalf("--check on an unformatted project: exit %d\n%s", code, out)
		}
		if untouched, _ := os.ReadFile(file); string(untouched) != messy {
			t.Error("--check must not modify files")
		}
		if out, code := runLoom(t, bin, dir, "fmt"); code != 0 {
			t.Fatalf("fmt: exit %d\n%s", code, out)
		}
		if out, code := runLoom(t, bin, dir, "fmt", "--check"); code != 0 {
			t.Errorf("--check after fmt: exit %d\n%s", code, out)
		}
	})
}

// Commands that create prompt files must honour [paths] in loom.toml. Right after `loom init`
// the prompts live under loom/src/prompts, and generated files used to land in ./prompts,
// where `loom inspect` never looked.
func TestGeneratedFilesLandWhereTheProjectLoadsThem(t *testing.T) {
	bin := buildLoom(t)
	dir := t.TempDir()
	if out, code := runLoom(t, bin, dir, "init"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}

	if out, code := runLoom(t, bin, dir, "recipe", "apply", "reviewer", "--language", "Go"); code != 0 {
		t.Fatalf("recipe apply: %d\n%s", code, out)
	}
	os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# My Helper\n\n## Persona\nYou help.\n\n## Instructions\n- Be kind\n"), 0o644)
	if out, code := runLoom(t, bin, dir, "import", "notes.md"); code != 0 {
		t.Fatalf("import: %d\n%s", code, out)
	}

	if _, err := os.Stat(filepath.Join(dir, "prompts")); err == nil {
		t.Error("nothing may be written to ./prompts in a project that keeps its prompts under loom/src")
	}
	for _, want := range []string{"loom/src/prompts/CodeReviewer.prompt.loom", "loom/src/prompts/Notes.prompt.loom", "loom/src/blocks/SecurityChecklist.block.loom"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("missing %s", want)
		}
	}
	if out, code := runLoom(t, bin, dir, "trace", "CodeReviewer"); code != 0 {
		t.Errorf("the recipe's prompts must be visible to the project: %d\n%s", code, out)
	}
	if out, code := runLoom(t, bin, dir, "list"); code != 0 || !strings.Contains(out, "Notes") {
		t.Errorf("imported prompt must be listed: %d\n%s", code, out)
	}
}
