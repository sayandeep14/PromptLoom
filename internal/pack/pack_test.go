package pack

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

type tarEntry struct {
	name     string
	body     string
	typeflag byte
	link     string
}

// makeArchive writes an .lpack with exactly the given entries (no validation).
func makeArchive(t *testing.T, entries []tarEntry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		flag := e.typeflag
		if flag == 0 {
			flag = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Typeflag: flag, Linkname: e.link}
		if flag == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if flag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	tw.Close()
	gz.Close()
	path := filepath.Join(t.TempDir(), "evil.lpack")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const manifest = "[pack]\nname = \"demo\"\nversion = \"1.0.0\"\n"

func project(t *testing.T) (dir, canary string) {
	t.Helper()
	dir = t.TempDir()
	canary = filepath.Join(dir, "precious.txt")
	if err := os.WriteFile(canary, []byte("do not touch"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, canary
}

func files(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func TestInitCreatesAValidManifestOnce(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(dir)
	if err != nil || m.Pack.Name != "my-pack" || m.Pack.Version != "1.0.0" {
		t.Errorf("%+v %v", m, err)
	}
	if err := Init(dir); err == nil {
		t.Error("Init must not overwrite an existing pack.toml")
	}
}

func TestBuildInstallListRemoveRoundTrip(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "pack.toml"), []byte(manifest), 0o644)
	os.MkdirAll(filepath.Join(src, "prompts", "sub"), 0o755)
	os.MkdirAll(filepath.Join(src, "blocks"), 0o755)
	os.WriteFile(filepath.Join(src, "prompts", "A.prompt.loom"), []byte("prompt A {\n}\n"), 0o644)
	os.WriteFile(filepath.Join(src, "prompts", "sub", "B.prompt.loom"), []byte("prompt B {\n}\n"), 0o644)
	os.WriteFile(filepath.Join(src, "blocks", "R.block.loom"), []byte("block R {\n}\n"), 0o644)
	os.WriteFile(filepath.Join(src, "notes.txt"), []byte("not part of the pack"), 0o644)

	archive, err := Build(src)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(archive) != "demo-1.0.0.lpack" {
		t.Errorf("archive name %q", archive)
	}

	dst := t.TempDir()
	if err := Install(archive, dst); err != nil {
		t.Fatal(err)
	}
	got := files(t, dst)
	want := []string{"blocks/demo/R.block.loom", "packs/demo.toml", "prompts/demo/A.prompt.loom", "prompts/demo/sub/B.prompt.loom"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("installed files:\n got  %v\n want %v", got, want)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "prompts", "demo", "A.prompt.loom")); string(b) != "prompt A {\n}\n" {
		t.Errorf("content changed: %q", b)
	}

	packs, err := List(dst)
	if err != nil || len(packs) != 1 || packs[0].Name != "demo" || packs[0].Version != "1.0.0" {
		t.Errorf("List = %+v %v", packs, err)
	}

	if err := Remove("demo", dst); err != nil {
		t.Fatal(err)
	}
	if left := files(t, dst); len(left) != 0 {
		t.Errorf("Remove left files behind: %v", left)
	}
	if err := Remove("demo", dst); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("removing twice: %v", err)
	}
}

func TestListOnAProjectWithoutPacks(t *testing.T) {
	if got, err := List(t.TempDir()); err != nil || len(got) != 0 {
		t.Errorf("%v %v", got, err)
	}
}

// ---- security ----

func TestInstallRefusesPathTraversal(t *testing.T) {
	for _, name := range []string{
		"prompts/../../precious-outside.txt",
		"prompts/../../../etc/cron.d/evil",
		"blocks/../../escaped",
		"prompts/a/../../../escaped",
		"/etc/passwd",
		"prompts/..",
		"prompts/x\\..\\..\\y",
	} {
		dir, canary := project(t)
		archive := makeArchive(t, []tarEntry{{name: "pack.toml", body: manifest}, {name: name, body: "owned"}})
		if err := Install(archive, dir); err == nil {
			t.Errorf("entry %q must be refused", name)
		}
		// nothing at all may be written, not even the safe parts
		if got := files(t, dir); len(got) != 1 || got[0] != "precious.txt" {
			t.Errorf("entry %q: a rejected archive must change nothing, found %v", name, got)
		}
		if b, _ := os.ReadFile(canary); string(b) != "do not touch" {
			t.Errorf("entry %q modified an existing file", name)
		}
		parent := filepath.Dir(dir)
		for _, escaped := range []string{"precious-outside.txt", "escaped"} {
			if _, err := os.Stat(filepath.Join(parent, escaped)); err == nil {
				t.Errorf("entry %q wrote outside the project: %s", name, escaped)
			}
		}
	}
}

func TestInstallRefusesEvilPackNames(t *testing.T) {
	for _, name := range []string{"..", "../..", "a/b", ".hidden", "", "x y", "a\\b", strings.Repeat("n", 65)} {
		dir, _ := project(t)
		toml := "[pack]\nname = \"" + strings.ReplaceAll(name, "\\", "\\\\") + "\"\nversion = \"1\"\n"
		archive := makeArchive(t, []tarEntry{{name: "pack.toml", body: toml}, {name: "prompts/A.prompt.loom", body: "x"}})
		if err := Install(archive, dir); err == nil {
			t.Errorf("pack name %q must be refused", name)
		}
	}
}

func TestInstallRefusesLinksAndSpecialFiles(t *testing.T) {
	for _, e := range []tarEntry{
		{name: "prompts/link", typeflag: tar.TypeSymlink, link: "/etc/passwd"},
		{name: "prompts/hard", typeflag: tar.TypeLink, link: "pack.toml"},
		{name: "prompts/dev", typeflag: tar.TypeChar},
		{name: "prompts/fifo", typeflag: tar.TypeFifo},
	} {
		dir, _ := project(t)
		archive := makeArchive(t, []tarEntry{{name: "pack.toml", body: manifest}, e})
		if err := Install(archive, dir); err == nil {
			t.Errorf("%s (type %c) must be refused", e.name, e.typeflag)
		}
	}
}

func TestInstallLimits(t *testing.T) {
	dir, _ := project(t)
	huge := strings.Repeat("A", maxFileBytes+1)
	archive := makeArchive(t, []tarEntry{{name: "pack.toml", body: manifest}, {name: "prompts/big.txt", body: huge}})
	if err := Install(archive, dir); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("one oversized file: %v", err)
	}

	many := []tarEntry{{name: "pack.toml", body: manifest}}
	for i := 0; i < maxEntries+1; i++ {
		many = append(many, tarEntry{name: "prompts/f" + strings.Repeat("x", 3) + string(rune('a'+i%26)) + strings.Repeat("y", i%7) + itoa(i), body: "x"})
	}
	if err := Install(makeArchive(t, many), dir); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Errorf("too many entries: %v", err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}

func TestInstallErrors(t *testing.T) {
	dir := t.TempDir()
	if err := Install(filepath.Join(dir, "missing.lpack"), dir); err == nil {
		t.Error("missing archive")
	}
	notGzip := filepath.Join(dir, "x.lpack")
	os.WriteFile(notGzip, []byte("this is not a gzip file"), 0o644)
	if err := Install(notGzip, dir); err == nil {
		t.Error("garbage archive")
	}
	noManifest := makeArchive(t, []tarEntry{{name: "prompts/A.prompt.loom", body: "x"}})
	if err := Install(noManifest, dir); err == nil || !strings.Contains(err.Error(), "pack.toml") {
		t.Errorf("missing manifest: %v", err)
	}
	if err := Install(makeArchive(t, []tarEntry{{name: "pack.toml", body: "[pack\nbroken"}}), dir); err == nil {
		t.Error("broken manifest")
	}
	if err := Install(makeArchive(t, []tarEntry{{name: "pack.toml", body: "[pack]\nversion=\"1\"\n"}}), dir); err == nil {
		t.Error("manifest without a name")
	}
}

// `loom pack remove ..` used to resolve to the project root and delete everything.
func TestRemoveRefusesNamesThatEscape(t *testing.T) {
	dir, canary := project(t)
	os.MkdirAll(filepath.Join(dir, "prompts", "keep"), 0o755)
	os.WriteFile(filepath.Join(dir, "prompts", "keep", "A.prompt.loom"), []byte("x"), 0o644)
	for _, name := range []string{"..", ".", "../..", "../precious.txt", "keep/..", "", "a/b", "/"} {
		if err := Remove(name, dir); err == nil {
			t.Errorf("Remove(%q) must be refused", name)
		}
	}
	if b, _ := os.ReadFile(canary); string(b) != "do not touch" {
		t.Error("the project was damaged")
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts", "keep", "A.prompt.loom")); err != nil {
		t.Error("an unrelated pack was deleted")
	}
}

func TestBuildValidatesTheManifest(t *testing.T) {
	cases := map[string]string{
		"no name":           "[pack]\nversion = \"1\"\n",
		"no version":        "[pack]\nname = \"x\"\n",
		"traversal in name": "[pack]\nname = \"../evil\"\nversion = \"1\"\n",
		"slash in name":     "[pack]\nname = \"a/b\"\nversion = \"1\"\n",
		"bad version":       "[pack]\nname = \"x\"\nversion = \"1/../..\"\n",
	}
	for label, body := range cases {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "pack.toml"), []byte(body), 0o644)
		if _, err := Build(dir); err == nil {
			t.Errorf("%s must be rejected", label)
		}
		if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(dir), "*.lpack")); len(leftovers) > 0 {
			t.Errorf("%s: an archive was written outside the project: %v", label, leftovers)
		}
	}
	if _, err := Build(t.TempDir()); err == nil {
		t.Error("no pack.toml at all")
	}
}

// A symlink inside prompts/ pointing at a secret must not be followed into a published archive.
func TestBuildDoesNotFollowSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	secret := filepath.Join(t.TempDir(), "id_rsa")
	os.WriteFile(secret, []byte("PRIVATE KEY MATERIAL"), 0o600)

	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "pack.toml"), []byte(manifest), 0o644)
	os.MkdirAll(filepath.Join(src, "prompts"), 0o755)
	os.WriteFile(filepath.Join(src, "prompts", "A.prompt.loom"), []byte("prompt A {\n}\n"), 0o644)
	if err := os.Symlink(secret, filepath.Join(src, "prompts", "leak.txt")); err != nil {
		t.Skip("cannot create symlinks here")
	}

	archive, err := Build(src)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(archive)
	gz, _ := gzip.NewReader(bytes.NewReader(raw))
	plain := new(bytes.Buffer)
	plain.ReadFrom(gz)
	if bytes.Contains(plain.Bytes(), []byte("PRIVATE KEY MATERIAL")) || bytes.Contains(plain.Bytes(), []byte("leak.txt")) {
		t.Error("a symlink was followed into the archive")
	}
	if !bytes.Contains(plain.Bytes(), []byte("A.prompt.loom")) {
		t.Error("regular files must still be included")
	}
}

func TestArchivePathsUseForwardSlashes(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "pack.toml"), []byte(manifest), 0o644)
	os.MkdirAll(filepath.Join(src, "prompts", "deep", "er"), 0o755)
	os.WriteFile(filepath.Join(src, "prompts", "deep", "er", "X.prompt.loom"), []byte("x"), 0o644)
	archive, err := Build(src)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(archive)
	defer f.Close()
	gz, _ := gzip.NewReader(f)
	tr := tar.NewReader(gz)
	found := false
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		if strings.Contains(h.Name, "\\") {
			t.Errorf("backslash in archive path %q (breaks on other platforms)", h.Name)
		}
		if h.Name == "prompts/deep/er/X.prompt.loom" {
			found = true
		}
	}
	if !found {
		t.Error("nested file missing from the archive")
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"go-backend", "my_pack", "Pack.v2", "a", "0day"} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", ".", "..", ".x", "-x", "a/b", "a\\b", "a b", "a\x00b", "é"} {
		if ValidateName(bad) == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}
