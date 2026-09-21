package installer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sayandeepgiri/promptloom/internal/deps"
)

// fakeRegistry serves bundles from memory and counts bundle downloads per slug.
type fakeRegistry struct {
	mu      sync.Mutex
	bundles map[string]Bundle
	hits    map[string]int
	srv     *httptest.Server
}

func newRegistry(t *testing.T, packs ...Bundle) *fakeRegistry {
	t.Helper()
	f := &fakeRegistry{bundles: map[string]Bundle{}, hits: map[string]int{}}
	for _, p := range packs {
		f.bundles[p.Slug] = p
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		// /api/v1/vaults/{slug}/bundle
		if len(parts) != 5 || parts[4] != "bundle" {
			http.NotFound(w, r)
			return
		}
		f.mu.Lock()
		f.hits[parts[3]]++
		b, ok := f.bundles[parts[3]]
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(b)
	}))
	t.Cleanup(f.srv.Close)
	t.Setenv("LOOM_REGISTRY_URL", f.srv.URL)
	return f
}

// pack builds a bundle with one prompt and optional .dependency.loom content.
func pack(slug, version, depsFile string) Bundle {
	files := []BundleFile{{
		Path: "prompts/" + strings.Title(slug) + ".prompt.loom", FileType: "prompt",
		Content: "prompt " + strings.Title(slug) + " {\n  persona :=\n    I am " + slug + ".\n}\n",
	}}
	if depsFile != "" {
		files = append(files, BundleFile{Path: ".dependency.loom", FileType: "meta", Content: depsFile})
	}
	return Bundle{Name: slug, Slug: slug, Version: version, Files: files}
}

func slugs(rs []*DepResult) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Meta.Slug)
	}
	return out
}

func TestInstallWritesSourceCompiledAndMetadata(t *testing.T) {
	newRegistry(t, Bundle{
		Name: "Kit", Slug: "kit", Version: "1.2.3", Author: "me", Tags: []string{"go"},
		Files: []BundleFile{
			{Path: "prompts/Base.prompt.loom", FileType: "prompt", Content: "prompt Base {\n  persona :=\n    hello.\n}\n"},
			{Path: "blocks/R.block.loom", FileType: "block", Content: "block R {\n  constraints :=\n    - x\n}\n"},
			{Path: "prompts/Broken.prompt.loom", FileType: "prompt", Content: "prompt Broken {{{"},
			{Path: ".metadata.loom", FileType: "meta", Content: "{}"},
		},
	})
	cwd := t.TempDir()
	res, err := Install("kit", cwd)
	if err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(cwd, "loompack", "kit", "source")
	for _, rel := range []string{"prompts/Base.prompt.loom", "blocks/R.block.loom", "prompts/Broken.prompt.loom", ".metadata.loom"} {
		if _, err := os.Stat(filepath.Join(src, rel)); err != nil {
			t.Errorf("source file %s not written: %v", rel, err)
		}
	}
	// Compiled output exists for the good prompt; the broken file is skipped, not fatal.
	md, err := os.ReadFile(filepath.Join(cwd, "loompack", "kit", "compiled", "Base.md"))
	if err != nil || !strings.Contains(string(md), "hello.") {
		t.Errorf("compiled output: %q %v", md, err)
	}
	if len(res.Compiled) != 1 || res.Compiled[0] != "Base.md" {
		t.Errorf("compiled = %v", res.Compiled)
	}
	// Meta files are not reported as "written prompts".
	for _, w := range res.Written {
		if strings.HasPrefix(w, ".") {
			t.Errorf("meta file listed as written: %s", w)
		}
	}
	pj, err := os.ReadFile(filepath.Join(cwd, "loompack", "kit", "pack.json"))
	if err != nil || !strings.Contains(string(pj), `"version": "1.2.3"`) {
		t.Errorf("pack.json: %s %v", pj, err)
	}
}

func TestInstallUsesLoomDirWhenPresent(t *testing.T) {
	newRegistry(t, pack("kit", "1.0.0", ""))
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "loom"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Install("kit", cwd)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, "loom", "loompack", "kit")
	if res.PackDir != want {
		t.Errorf("PackDir = %s, want %s", res.PackDir, want)
	}
}

func TestInstallErrors(t *testing.T) {
	reg := newRegistry(t, pack("kit", "1.0.0", ""))
	cwd := t.TempDir()

	if _, err := Install("ghost", cwd); err == nil || !strings.Contains(err.Error(), `"ghost" not found`) {
		t.Errorf("missing pack: %v", err)
	}

	reg.mu.Lock()
	reg.bundles["empty"] = Bundle{Slug: "empty", Version: "1.0.0"}
	reg.mu.Unlock()
	if _, err := Install("empty", cwd); err == nil || !strings.Contains(err.Error(), "no files") {
		t.Errorf("empty pack: %v", err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer bad.Close()
	t.Setenv("LOOM_REGISTRY_URL", bad.URL)
	if _, err := Install("kit", cwd); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("server error: %v", err)
	}

	garbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	}))
	defer garbage.Close()
	t.Setenv("LOOM_REGISTRY_URL", garbage.URL)
	if _, err := Install("kit", cwd); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("non-JSON reply: %v", err)
	}

	t.Setenv("LOOM_REGISTRY_URL", "http://127.0.0.1:1") // nothing listens here
	if _, err := Install("kit", cwd); err == nil {
		t.Error("unreachable registry must fail")
	}
}

func TestInstallWithDepsResolvesTheWholeGraphOnce(t *testing.T) {
	// app -> lib>=1.0.0, util ; lib -> util  (util is reached twice but installed once)
	reg := newRegistry(t,
		pack("app", "1.0.0", "lib>=1.0.0\nutil\n"),
		pack("lib", "1.2.0", "util  # any version\n"),
		pack("util", "0.5.0", ""),
	)
	cwd := t.TempDir()
	results, conflicts, err := InstallWithDeps("app", cwd)
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("err=%v conflicts=%v", err, conflicts)
	}
	if got := strings.Join(slugs(results), ","); got != "app,lib,util" {
		t.Errorf("install order = %s, want app,lib,util (depth-first)", got)
	}
	for slug, n := range reg.hits {
		if n != 1 {
			t.Errorf("%s downloaded %d times, want 1", slug, n)
		}
	}
	direct := 0
	for _, r := range results {
		if r.DirectRequest {
			direct++
			if r.Meta.Slug != "app" {
				t.Errorf("wrong pack marked direct: %s", r.Meta.Slug)
			}
		}
	}
	if direct != 1 {
		t.Errorf("%d results marked direct, want 1", direct)
	}

	pl, err := deps.ReadPackLock(cwd)
	if err != nil {
		t.Fatal(err)
	}
	for slug, ver := range map[string]string{"app": "1.0.0", "lib": "1.2.0", "util": "0.5.0"} {
		e := pl.Find(slug)
		if e == nil || e.Version != ver {
			t.Errorf("lock entry for %s = %+v, want %s", slug, e, ver)
		}
	}
	if e := pl.Find("lib"); e != nil && (len(e.RequiredBy) != 1 || e.RequiredBy[0] != "app") {
		t.Errorf("lib should record who required it: %+v", e)
	}
}

func TestInstallWithDepsSkipsWhatIsAlreadyInstalled(t *testing.T) {
	reg := newRegistry(t, pack("app", "1.0.0", "lib>=1.0.0\n"), pack("lib", "1.0.0", ""))
	cwd := t.TempDir()
	if _, _, err := InstallWithDeps("app", cwd); err != nil {
		t.Fatal(err)
	}
	before := reg.hits["lib"]

	results, _, err := InstallWithDeps("app", cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("a satisfied install must do nothing, installed %v", slugs(results))
	}
	if reg.hits["lib"] != before {
		t.Error("lib was downloaded again")
	}

	// A dependency deleted from disk is restored by re-running the install of its
	// dependent, even though the dependent itself is already satisfied.
	if err := os.RemoveAll(filepath.Join(cwd, "loompack", "lib")); err != nil {
		t.Fatal(err)
	}
	results, _, err = InstallWithDeps("app", cwd)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(slugs(results), ","); got != "lib" {
		t.Errorf("only the missing dependency should be reinstalled, got %q", got)
	}
	if _, statErr := os.Stat(filepath.Join(cwd, "loompack", "lib", "source")); statErr != nil {
		t.Errorf("lib was not restored: %v", statErr)
	}
}

func TestInstallWithDepsReportsVersionConflicts(t *testing.T) {
	// app needs lib>=2.0.0 and tool; tool needs lib<2.0.0; the registry has lib 1.5.0.
	newRegistry(t,
		pack("app", "1.0.0", "lib>=2.0.0\ntool\n"),
		pack("tool", "1.0.0", "lib<2.0.0\n"),
		pack("lib", "1.5.0", ""),
	)
	cwd := t.TempDir()
	_, conflicts, err := InstallWithDeps("app", cwd)
	if err != nil {
		t.Fatalf("conflicts are reported, not fatal: %v", err)
	}
	if len(conflicts) != 1 || conflicts[0].Slug != "lib" {
		t.Fatalf("conflicts = %+v", conflicts)
	}
	msg := conflicts[0].Error()
	if !strings.Contains(msg, ">=2.0.0 (required by app)") {
		t.Errorf("message should name the requirement and who imposed it: %s", msg)
	}
	// The lockfile is still written so the state is inspectable.
	if _, err := os.Stat(deps.PackLockPath(cwd)); err != nil {
		t.Errorf("loompack.lock should be written: %v", err)
	}
}

func TestInstallWithDepsHandlesCycles(t *testing.T) {
	newRegistry(t, pack("a", "1.0.0", "b\n"), pack("b", "1.0.0", "a\n"))
	results, conflicts, err := InstallWithDeps("a", t.TempDir())
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("a dependency cycle must terminate cleanly: %v %v", err, conflicts)
	}
	if len(results) != 2 {
		t.Errorf("got %v", slugs(results))
	}
}

func TestInstallWithDepsMissingDependency(t *testing.T) {
	newRegistry(t, pack("app", "1.0.0", "ghost>=1.0.0\n"))
	results, _, err := InstallWithDeps("app", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("error should name the missing dependency: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("app itself was installed before the failure: %v", slugs(results))
	}
}

func TestInstallWithDepsBadDependencyFileIsNotFatal(t *testing.T) {
	// An unparsable .dependency.loom is treated as "no dependencies" (documented behaviour).
	newRegistry(t, pack("app", "1.0.0", "this is !! not valid\n"))
	results, _, err := InstallWithDeps("app", t.TempDir())
	if err != nil || len(results) != 1 {
		t.Errorf("err=%v results=%v", err, slugs(results))
	}
}

func TestInstallWithDepsCorruptLockfile(t *testing.T) {
	newRegistry(t, pack("app", "1.0.0", ""))
	cwd := t.TempDir()
	if err := os.WriteFile(deps.PackLockPath(cwd), []byte("not [valid toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallWithDeps("app", cwd); err == nil {
		t.Error("a corrupt loompack.lock should stop the install rather than be silently overwritten")
	}
}
