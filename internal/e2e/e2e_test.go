package e2e

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/loader"
	"github.com/sayandeep14/PromptLoom/internal/registry"
	"github.com/sayandeep14/PromptLoom/internal/render"
	"github.com/sayandeep14/PromptLoom/internal/resolve"
	"github.com/sayandeep14/PromptLoom/internal/validate"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/valid/*/golden")

const testdataDir = "../../testdata"

// quietConfig is used when a fixture has no loom.toml of its own: it turns the
// "missing objective/format" warnings off so fixtures only mention what they test.
const quietConfig = `[project]
name = "fixture"
version = "0.0.0"

[paths]
prompts  = "prompts"
blocks   = "blocks"
overlays = "overlays"
out      = "dist"

[validation]
require_objective        = false
require_format           = false
require_contract         = false
warn_on_empty_context    = true
warn_on_deep_inheritance = true
max_inheritance_depth    = 3
`

// allRules is the catalogue of diagnostics the toolchain can produce. Every
// entry must be demonstrated by at least one testdata/invalid fixture
// (`rule: <id>` in its expect.txt). When you add a check to internal/validate,
// the parser, or the registry, add its id here and a fixture for it.
var allRules = []string{
	// load-time (lexer, parser, registry)
	"parse-missing-brace", "parse-unknown-toplevel", "parse-bad-declaration", "parse-bad-body-token",
	"parse-bad-from-expr", "parse-keyword-in-block", "parse-bad-var", "parse-unterminated",
	"duplicate-prompt", "duplicate-block", "duplicate-overlay",
	// validate: errors
	"unknown-parent", "unknown-block", "inheritance-cycle", "unknown-field", "duplicate-var",
	"duplicate-variant", "variant-unknown-field", "remove-on-scalar", "from-all-on-scalar",
	"from-index-out-of-range", "from-range-out-of-range", "from-not-a-parent", "from-unknown-field",
	"block-from-expr", "block-unknown-field", "overlay-unknown-field", "undeclared-variable",
	// validate: warnings
	"error-append-removed", "error-append-no-parent", "error-append-scalar", "error-append-in-block",
	"error-append-in-variant", "error-append-in-env", "error-remove-removed", "warn-bare-colon",
	"parse-extends", "from-variant-out-of-range", "warn-tags-operator", "warn-missing-objective",
	"warn-missing-format", "warn-empty-context", "warn-deep-inheritance", "warn-redefine-inherited",
	"warn-kind-mismatch", "warn-block-overridden", "warn-required-runtime-var", "warn-block-variable", "warn-overlay-variable",
	// weave-time
	"weave-unknown-variant", "weave-unknown-env", "weave-unknown-overlay", "weave-unknown-prompt",
	"weave-from-index-out-of-range",
}

// ---------- fixture plumbing ----------

func cases(t *testing.T, kind string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(testdataDir, kind))
	if err != nil {
		t.Fatalf("read testdata/%s: %v", kind, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// stage copies a fixture into a fresh temp dir so runs never touch testdata.
func stage(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return nil
		}
		// Expectations and goldens are test metadata, not part of the project.
		if rel == "expect.txt" || rel == "weave.txt" || rel == "golden" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "loom.toml")); os.IsNotExist(err) {
		if err := os.WriteFile(filepath.Join(dst, "loom.toml"), []byte(quietConfig), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

type expectation struct {
	kind, substr string
	matched      bool
}

type fixture struct {
	rules   []string
	expects []*expectation
	also    []string // substrings that must appear somewhere in the combined output
	output  []string // every diagnostic / error text produced, for `also:`
}

func (f *fixture) record(msg string) { f.output = append(f.output, msg) }

func readExpect(t *testing.T, dir string) fixture {
	t.Helper()
	var f fixture
	data, err := os.ReadFile(filepath.Join(dir, "expect.txt"))
	if os.IsNotExist(err) {
		return f
	}
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			t.Fatalf("%s/expect.txt:%d: want `kind: text`, got %q", dir, i+1, line)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "rule":
			f.rules = append(f.rules, v)
		case "also":
			f.also = append(f.also, v)
		case "error", "warning", "load-error", "weave-error":
			f.expects = append(f.expects, &expectation{kind: k, substr: v})
		default:
			t.Fatalf("%s/expect.txt:%d: unknown kind %q", dir, i+1, k)
		}
	}
	return f
}

// consume marks the first unmatched expectation of kind whose text is in msg.
func (f *fixture) consume(kind, msg string) bool {
	for _, e := range f.expects {
		if !e.matched && e.kind == kind && strings.Contains(msg, e.substr) {
			e.matched = true
			return true
		}
	}
	return false
}

func (f *fixture) unmet(t *testing.T) {
	t.Helper()
	all := strings.Join(f.output, "\n")
	for _, a := range f.also {
		if !strings.Contains(all, a) {
			t.Errorf("expected the output to also contain %q (e.g. a fix hint), got:\n%s", a, all)
		}
	}
	for _, e := range f.expects {
		if !e.matched {
			t.Errorf("expected %s containing %q, but none was produced", e.kind, e.substr)
		}
	}
}

// load runs the same steps as `loom inspect`.
func load(t *testing.T, dir string) (*registry.Registry, *config.Config, []validate.Diagnostic, error) {
	t.Helper()
	reg, cfg, err := loader.Load(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	return reg, cfg, validate.Validate(reg, cfg), nil
}

type weaveSpec struct {
	name, prompt, format string
	opts                 resolve.Options
}

func readWeave(t *testing.T, dir string) []weaveSpec {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "weave.txt"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []weaveSpec
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			t.Fatalf("weave.txt:%d: want `name: Prompt [flags]`", i+1)
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			t.Fatalf("weave.txt:%d: missing prompt name", i+1)
		}
		w := weaveSpec{name: strings.TrimSpace(name), prompt: fields[0], opts: resolve.Options{Variables: map[string]string{}}}
		for j := 1; j < len(fields); j++ {
			flagName := fields[j]
			if j+1 >= len(fields) {
				t.Fatalf("weave.txt:%d: flag %s needs a value", i+1, flagName)
			}
			val := fields[j+1]
			j++
			switch flagName {
			case "--variant":
				w.opts.Variant = val
			case "--env":
				w.opts.Env = val
			case "--overlay":
				w.opts.Overlays = append(w.opts.Overlays, val)
			case "--format":
				w.format = val
			case "--set":
				k, v, ok := strings.Cut(val, "=")
				if !ok {
					t.Fatalf("weave.txt:%d: --set wants key=value", i+1)
				}
				w.opts.Variables[k] = v
			default:
				t.Fatalf("weave.txt:%d: unknown flag %s", i+1, flagName)
			}
		}
		out = append(out, w)
	}
	return out
}

func weave(reg *registry.Registry, cfg *config.Config, w weaveSpec) (string, *ast.ResolvedPrompt, error) {
	rp, err := resolve.ResolveWithOptions(w.prompt, reg, w.opts)
	if err != nil {
		return "", nil, err
	}
	if w.format == "" {
		return render.Render(rp, cfg), rp, nil
	}
	out, _, err := render.RenderFormat(rp, cfg, w.format)
	return out, rp, err
}

// ---------- tests ----------

func TestValidFixtures(t *testing.T) {
	for _, name := range cases(t, "valid") {
		name := name
		t.Run(name, func(t *testing.T) {
			src := filepath.Join(testdataDir, "valid", name)
			dir := stage(t, src)
			fx := readExpect(t, src)

			reg, cfg, diags, err := load(t, dir)
			if err != nil {
				t.Fatalf("a valid fixture must load: %v", err)
			}
			for _, d := range diags {
				sev := "warning"
				if d.Sev == validate.Error {
					sev = "error"
				}
				if !fx.consume(sev, d.Message) {
					t.Errorf("unexpected %s: %s", sev, d)
				}
			}
			fx.unmet(t)

			specs := readWeave(t, src)
			if len(specs) == 0 { // default: weave every prompt to Markdown
				var names []string
				for _, n := range reg.Prompts() {
					names = append(names, n.Name)
				}
				sort.Strings(names)
				for _, n := range names {
					specs = append(specs, weaveSpec{name: n, prompt: n, opts: resolve.Options{Variables: map[string]string{}}})
				}
			}
			if len(specs) == 0 {
				t.Fatal("fixture defines no prompts")
			}

			for _, w := range specs {
				out, rp, err := weave(reg, cfg, w)
				if err != nil {
					t.Errorf("weave %s: %v", w.name, err)
					continue
				}
				checkInvariants(t, w.name, rp, out)

				// Weaving twice must give identical bytes and fingerprint.
				out2, rp2, err := weave(reg, cfg, w)
				if err != nil || out2 != out || rp2.Fingerprint != rp.Fingerprint {
					t.Errorf("weave %s is not deterministic", w.name)
				}

				golden := filepath.Join(src, "golden", w.name)
				if filepath.Ext(w.name) == "" {
					golden += ".md"
				}
				if *update {
					if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(golden, []byte(out), 0o644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Errorf("missing golden file %s (run: go test ./internal/e2e -update)", golden)
					continue
				}
				if string(want) != out {
					t.Errorf("weave %s differs from %s\n--- want\n%s\n--- got\n%s", w.name, golden, want, out)
				}
			}
		})
	}
}

// checkInvariants asserts properties every successfully woven prompt must have.
func checkInvariants(t *testing.T, name string, rp *ast.ResolvedPrompt, out string) {
	t.Helper()
	if strings.TrimSpace(out) == "" {
		t.Errorf("%s: empty output", name)
	}
	if strings.Contains(out, "{{") {
		t.Errorf("%s: unresolved {{ }} placeholder in output:\n%s", name, out)
	}
	for field, items := range map[string][]string{
		"instructions": rp.Instructions, "constraints": rp.Constraints,
		"examples": rp.Examples, "format": rp.Format,
	} {
		seen := map[string]bool{}
		for _, it := range items {
			if seen[it] {
				t.Errorf("%s: duplicate item %q in %s (lists must be de-duplicated)", name, it, field)
			}
			seen[it] = true
		}
	}
	if rp.Fingerprint == "" {
		t.Errorf("%s: missing fingerprint", name)
	}
}

var hasLine = regexp.MustCompile(`:\d+`)

func TestInvalidFixtures(t *testing.T) {
	for _, name := range cases(t, "invalid") {
		name := name
		t.Run(name, func(t *testing.T) {
			src := filepath.Join(testdataDir, "invalid", name)
			dir := stage(t, src)
			fx := readExpect(t, src)
			if len(fx.rules) == 0 || len(fx.expects) == 0 {
				t.Fatal("an invalid fixture needs `rule:` and at least one expectation in expect.txt")
			}

			reg, cfg, diags, err := load(t, dir)
			if err != nil {
				msg := err.Error()
				fx.record(msg)
				if !fx.consume("load-error", msg) {
					t.Errorf("unexpected load error: %s", msg)
				}
				// Errors must say where: file and line.
				if !hasLine.MatchString(msg) {
					t.Errorf("load error has no file:line position: %s", msg)
				}
				fx.unmet(t)
				return
			}

			errorCount := 0
			for _, d := range diags {
				sev := "warning"
				if d.Sev == validate.Error {
					sev = "error"
					errorCount++
				}
				fx.record(d.Message)
				if !fx.consume(sev, d.Message) {
					t.Errorf("unexpected %s: %s", sev, d)
				}
				if d.Pos.File == "" || d.Pos.Line == 0 {
					t.Errorf("%s has no source position: %s", sev, d.Message)
				}
			}

			// Weave-time failures for things validation cannot see.
			for _, w := range readWeave(t, src) {
				if _, _, err := weave(reg, cfg, w); err != nil {
					fx.record(err.Error())
					if !fx.consume("weave-error", err.Error()) {
						t.Errorf("weave %s: unexpected error: %v", w.name, err)
					}
				} else {
					t.Errorf("weave %s: expected an error, got success", w.name)
				}
			}
			fx.unmet(t)

			wantsErrors := false
			for _, e := range fx.expects {
				if e.kind == "error" {
					wantsErrors = true
				}
			}
			if wantsErrors && errorCount == 0 {
				t.Error("fixture expects validation errors but produced none")
			}
		})
	}
}

func TestEveryRuleHasAFixture(t *testing.T) {
	known := map[string]bool{}
	for _, r := range allRules {
		known[r] = true
	}
	covered := map[string]int{}
	for _, name := range cases(t, "invalid") {
		fx := readExpect(t, filepath.Join(testdataDir, "invalid", name))
		for _, r := range fx.rules {
			if !known[r] {
				t.Errorf("fixture %s declares unknown rule %q (typo, or add it to allRules)", name, r)
			}
			covered[r]++
		}
	}
	for _, r := range allRules {
		if covered[r] == 0 {
			t.Errorf("rule %q has no failing fixture in testdata/invalid", r)
		}
	}
}

func TestValidFixtureCoverage(t *testing.T) {
	// Feature areas that must each have a passing fixture (matched by directory-name prefix).
	for _, area := range []string{
		"minimal", "inheritance", "multi-parent", "from-", "blocks", "overlays",
		"variants", "env", "vars", "contract", "formats", "mixed-file", "namespace",
	} {
		found := false
		for _, name := range cases(t, "valid") {
			if strings.Contains(name, area) {
				found = true
			}
		}
		if !found {
			t.Errorf("no valid fixture covers %q", area)
		}
	}
}
