package lock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/parser"
	"github.com/sayandeep14/PromptLoom/internal/registry"
)

// project writes files (relative path -> content) into a temp dir and registers
// every .loom file, so block source hashing has real files to read.
func project(t *testing.T, files map[string]string) (*registry.Registry, string) {
	t.Helper()
	dir := t.TempDir()
	reg := registry.New()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		nodes, err := parser.Parse(p, content)
		if err != nil {
			t.Fatal(err)
		}
		if err := reg.Register(nodes); err != nil {
			t.Fatal(err)
		}
	}
	return reg, dir
}

var base = map[string]string{
	"blocks/Rules.block.loom":   "block Rules {\n  constraints :=\n    - be safe\n}\n",
	"prompts/Alpha.prompt.loom": "prompt Alpha {\n  use Rules\n  persona :=\n    a.\n}\n",
	"prompts/Beta.prompt.loom":  "prompt Beta {\n  persona :=\n    b.\n}\n",
}

func TestGenerateIsSortedAndDeterministic(t *testing.T) {
	reg, dir := project(t, base)
	a, err := Generate(reg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Prompts) != 2 || a.Prompts[0].Name != "Alpha" || a.Prompts[1].Name != "Beta" {
		t.Errorf("prompts not sorted: %+v", a.Prompts)
	}
	if len(a.Blocks) != 1 || !strings.HasPrefix(a.Blocks[0].Hash, "sha256:") {
		t.Errorf("block hash: %+v", a.Blocks)
	}
	if got := a.Prompts[0].Blocks; len(got) != 1 || got[0] != "Rules" {
		t.Errorf("Alpha should record its block: %v", got)
	}
	if a.Prompts[1].Blocks != nil {
		t.Errorf("Beta uses no blocks: %v", a.Prompts[1].Blocks)
	}

	b, _ := Generate(reg, dir)
	if a.Prompts[0].Hash != b.Prompts[0].Hash || a.Blocks[0].Hash != b.Blocks[0].Hash {
		t.Error("Generate must be deterministic")
	}
}

func TestWriteReadRoundTripAndStableBytes(t *testing.T) {
	reg, dir := project(t, base)
	lf, _ := Generate(reg, dir)
	if err := Write(lf, dir); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(Path(dir))
	if err := Write(lf, dir); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(Path(dir))
	if string(first) != string(second) {
		t.Error("writing the same lockfile twice must give identical bytes (clean git diffs)")
	}

	got, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Prompts) != 2 || got.Prompts[0].Hash != lf.Prompts[0].Hash || got.Blocks[0].Name != "Rules" {
		t.Errorf("round trip lost data: %+v", got)
	}
}

func TestReadMissingAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	if _, err := Read(dir); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing file: %v", err)
	}

	if err := os.WriteFile(Path(dir), []byte("this is [not valid toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Read2(dir)
	if err == nil || strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "unreadable") {
		t.Errorf("a corrupt lockfile must not be reported as missing, got: %v", err)
	}
}

// Read2 is a tiny helper so the corrupt-file assertion reads clearly.
func Read2(dir string) error { _, err := Read(dir); return err }

func TestCheckCleanThenDetectsEachKindOfDrift(t *testing.T) {
	reg, dir := project(t, base)
	lf, _ := Generate(reg, dir)
	if err := Write(lf, dir); err != nil {
		t.Fatal(err)
	}

	if m, err := Check(reg, dir); err != nil || len(m) != 0 {
		t.Fatalf("fresh lock should be clean: %v %v", m, err)
	}

	// 1. edit a prompt's content
	edited := map[string]string{}
	for k, v := range base {
		edited[k] = v
	}
	edited["prompts/Beta.prompt.loom"] = "prompt Beta {\n  persona :=\n    CHANGED.\n}\n"
	reg2, dir2 := project(t, edited)
	if err := Write(lf, dir2); err != nil { // lock from the original state
		t.Fatal(err)
	}
	m, err := Check(reg2, dir2)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m[0].Name != "Beta" || m[0].Kind != "prompt" || m[0].Locked == m[0].Current {
		t.Errorf("edited prompt: %+v", m)
	}

	// 2. edit a block: the block itself AND the prompts are reported per their own hashes
	edited2 := map[string]string{}
	for k, v := range base {
		edited2[k] = v
	}
	edited2["blocks/Rules.block.loom"] = "block Rules {\n  constraints :=\n    - be VERY safe\n}\n"
	reg3, dir3 := project(t, edited2)
	if err := Write(lf, dir3); err != nil {
		t.Fatal(err)
	}
	m, _ = Check(reg3, dir3)
	kinds := map[string]string{}
	for _, x := range m {
		kinds[x.Name] = x.Kind
	}
	if kinds["Rules"] != "block" || kinds["Alpha"] != "prompt" {
		t.Errorf("editing a block must flag the block and the prompt using it: %+v", m)
	}
	if _, flagged := kinds["Beta"]; flagged {
		t.Error("Beta does not use the block and must not be flagged")
	}
}

func TestCheckNewAndRemovedEntries(t *testing.T) {
	reg, dir := project(t, base)
	lf, _ := Generate(reg, dir)
	if err := Write(lf, dir); err != nil {
		t.Fatal(err)
	}

	added := map[string]string{}
	for k, v := range base {
		added[k] = v
	}
	added["prompts/Gamma.prompt.loom"] = "prompt Gamma {\n  persona :=\n    g.\n}\n"
	delete(added, "prompts/Beta.prompt.loom")
	reg2, dir2 := project(t, added)
	if err := Write(lf, dir2); err != nil {
		t.Fatal(err)
	}
	m, err := Check(reg2, dir2)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, x := range m {
		if x.Locked == "(new)" {
			got[x.Name] = "new"
		}
		if x.Current == "(removed)" {
			got[x.Name] = "removed"
		}
	}
	if got["Gamma"] != "new" || got["Beta"] != "removed" {
		t.Errorf("got %v from %+v", got, m)
	}
}

func TestCheckWithoutLockfile(t *testing.T) {
	reg, dir := project(t, base)
	if _, err := Check(reg, dir); err == nil {
		t.Error("Check must fail when loom.lock is missing")
	}
}

func TestGenerateFailsClearlyOnUnresolvablePrompt(t *testing.T) {
	reg, dir := project(t, map[string]string{
		"prompts/Bad.prompt.loom": "prompt Bad inherits Ghost {\n  persona :=\n    x.\n}\n",
	})
	_, err := Generate(reg, dir)
	if err == nil || !strings.Contains(err.Error(), "Bad") {
		t.Errorf("error should name the prompt: %v", err)
	}
}
