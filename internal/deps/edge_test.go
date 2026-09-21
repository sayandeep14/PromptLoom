package deps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseContentEdgeCases(t *testing.T) {
	content := "\n# a full-line comment\n\n  python   # trailing comment\nbackend >= 2.0.0\nserver<=3.0.1\nwebdev==0.0.1 as dev\nshared~=1.4.0\nunder_score-name>1.0\n"
	got, err := ParseContent(content)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"python", "backend>=2.0.0", "server<=3.0.1", "webdev==0.0.1", "shared~=1.4.0", "under_score-name>1.0"}
	if len(got) != len(want) {
		t.Fatalf("got %d deps: %+v", len(got), got)
	}
	for i, d := range got {
		if d.String() != want[i] {
			t.Errorf("dep %d = %q, want %q", i, d.String(), want[i])
		}
	}
	if got[3].Alias != "dev" {
		t.Errorf("alias not parsed: %+v", got[3])
	}
	if got[0].Op != "" || got[0].Version != "" {
		t.Errorf("a bare name means any version: %+v", got[0])
	}
}

func TestParseContentRejectsGarbageWithLineNumber(t *testing.T) {
	for _, bad := range []string{"has space name", "pack>=", "!!!", "pack = 1.0.0", "pack=>1.0"} {
		_, err := ParseContent("ok\n" + bad + "\n")
		if err == nil {
			t.Errorf("%q should be rejected", bad)
			continue
		}
		if !strings.Contains(err.Error(), "line 2") {
			t.Errorf("%q: error should name line 2: %v", bad, err)
		}
	}
}

func TestParseFileErrorsNameTheFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".dependency.loom")
	if err := os.WriteFile(p, []byte("good\n???\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ParseFile(p)
	if err == nil || !strings.Contains(err.Error(), ".dependency.loom:2") {
		t.Errorf("got %v", err)
	}
	if _, err := ParseFile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a missing file is an error")
	}
}

func TestSemverParsing(t *testing.T) {
	ok := map[string]semver{
		"1":             {Major: 1},
		"1.2":           {Major: 1, Minor: 2},
		"1.2.3":         {Major: 1, Minor: 2, Patch: 3},
		"10.20.30":      {Major: 10, Minor: 20, Patch: 30},
		"1.2.3-beta.1":  {Major: 1, Minor: 2, Patch: 3, Pre: "beta.1"},
		"1.2.3+build.5": {Major: 1, Minor: 2, Patch: 3},
		"1.2.3-rc1+b7":  {Major: 1, Minor: 2, Patch: 3, Pre: "rc1"},
	}
	for in, want := range ok {
		got, err := parseSemver(in)
		if err != nil || got != want {
			t.Errorf("parseSemver(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "a.b.c", "1.x.3", "v1.2.3", "1..3"} {
		if _, err := parseSemver(bad); err == nil {
			t.Errorf("parseSemver(%q) should fail", bad)
		}
	}
}

// The registry accepts pre-release and build versions, so the client must too.
func TestSatisfiesPrereleaseAndBuild(t *testing.T) {
	cases := []struct {
		installed, op, required string
		want                    bool
	}{
		{"1.2.3-beta.1", ">=", "1.0.0", true},
		{"1.2.3-beta.1", "==", "1.2.3-beta.1", true},
		{"1.2.3-beta.1", "<", "1.2.3", true}, // a pre-release is older than its release
		{"1.2.3", ">", "1.2.3-beta.1", true}, // ... and the release is newer
		{"1.2.3-alpha", "<", "1.2.3-beta", true},
		{"1.2.3-beta", ">=", "1.2.3", false},
		{"1.2.3+build9", "==", "1.2.3", true}, // build metadata never affects precedence
		{"2.0.0-rc1", ">=", "2.0.0", false},
		{"1.2.3-beta.1", "~=", "1.2.0", true}, // newer than 1.2.0 and same major.minor
	}
	for _, c := range cases {
		if got := Satisfies(c.installed, c.op, c.required); got != c.want {
			t.Errorf("Satisfies(%q %s %q) = %v, want %v", c.installed, c.op, c.required, got, c.want)
		}
	}
}

func TestSatisfiesBoundariesAndUnparseable(t *testing.T) {
	cases := []struct {
		installed, op, required string
		want                    bool
	}{
		{"1.0.0", ">=", "1.0.0", true},
		{"1.0.0", ">", "1.0.0", false},
		{"1.0.0", "<=", "1.0.0", true},
		{"1.0.0", "<", "1.0.0", false},
		{"1.10.0", ">", "1.9.0", true}, // numeric, not lexicographic
		{"0.9.9", "<", "0.10.0", true},
		{"1.4.9", "~=", "1.4.0", true},
		{"1.5.0", "~=", "1.4.0", false},
		{"2.4.0", "~=", "1.4.0", false},
		{"1.3.9", "~=", "1.4.0", false},
		{"garbage", ">=", "1.0.0", false}, // unknown installed version is conservative
		{"1.0.0", ">=", "garbage", false},
		{"1.0.0", "", "", true},         // no constraint = any version
		{"1.0.0", "!!", "1.0.0", false}, // unknown operator never satisfies
	}
	for _, c := range cases {
		if got := Satisfies(c.installed, c.op, c.required); got != c.want {
			t.Errorf("Satisfies(%q %s %q) = %v, want %v", c.installed, c.op, c.required, got, c.want)
		}
	}
}

func TestInstalledAndMissing(t *testing.T) {
	dir := t.TempDir()
	mk := func(slug, pack string) {
		p := filepath.Join(dir, slug)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if pack != "" {
			if err := os.WriteFile(filepath.Join(p, "pack.json"), []byte(pack), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("with-meta", "{\n  \"slug\": \"with-meta\",\n  \"version\": \"1.2.3\"\n}")
	mk("no-meta", "")
	if err := os.WriteFile(filepath.Join(dir, "stray-file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := Installed(dir)
	if got["with-meta"] != "1.2.3" {
		t.Errorf("version not read from pack.json: %v", got)
	}
	if v, ok := got["no-meta"]; !ok || v != "" {
		t.Errorf("a pack without pack.json is installed with unknown version: %v", got)
	}
	if _, ok := got["stray-file"]; ok {
		t.Error("plain files are not packs")
	}
	if len(Installed(filepath.Join(dir, "does-not-exist"))) != 0 {
		t.Error("a missing directory means nothing installed")
	}

	need := []Dependency{{Name: "with-meta"}, {Name: "absent"}, {Name: "no-meta"}, {Name: "also-absent"}}
	miss := Missing(need, got)
	if len(miss) != 2 || miss[0].Name != "absent" || miss[1].Name != "also-absent" {
		t.Errorf("Missing = %+v", miss)
	}
}

func TestWriteDefaultProducesAParsableFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".dependency.loom")
	if err := WriteDefault(p); err != nil {
		t.Fatal(err)
	}
	if got, err := ParseFile(p); err != nil || len(got) != 0 {
		t.Errorf("the template must parse and declare nothing: %v %v", got, err)
	}
}

func TestPackLockUpsertKeepsOrderAndFind(t *testing.T) {
	pl := &PackLock{}
	pl.Upsert(PackLockEntry{Slug: "b", Version: "1.0.0"})
	pl.Upsert(PackLockEntry{Slug: "a", Version: "1.0.0"})
	pl.Upsert(PackLockEntry{Slug: "b", Version: "2.0.0", RequiredBy: []string{"a"}})
	if len(pl.Packs) != 2 {
		t.Fatalf("upsert must not duplicate: %+v", pl.Packs)
	}
	if e := pl.Find("b"); e == nil || e.Version != "2.0.0" {
		t.Errorf("Find(b) = %+v", e)
	}
	if pl.Find("zzz") != nil {
		t.Error("Find of an unknown slug must be nil")
	}
}
