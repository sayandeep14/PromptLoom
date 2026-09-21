package stale

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/ast"
)

func depMap(deps []DepVersion) map[string]string {
	m := map[string]string{}
	for _, d := range deps {
		m[d.Name] = d.Version
	}
	return m
}

func TestParseGoMod(t *testing.T) {
	src := `module vault

go 1.22

toolchain go1.22.3

require github.com/spf13/cobra v1.8.0

require (
	github.com/BurntSushi/toml v1.3.2
	github.com/foo/bar/v2 v2.1.0 // indirect
	// github.com/commented v9.9.9
)

replace github.com/spf13/cobra => ../cobra v1.0.0
exclude github.com/x/y v0.0.1
retract v1.0.0
`
	deps, _ := parseGoMod([]byte(src))
	got := depMap(deps)
	want := map[string]string{"go": "1.22", "cobra": "1.8.0", "toml": "1.3.2", "bar": "2.1.0"}
	if len(got) != len(want) {
		t.Errorf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q want %q (all: %v)", k, got[k], v, got)
		}
	}
}

func TestParsePackageJSON(t *testing.T) {
	deps, err := parsePackageJSON([]byte(`{"dependencies":{"react":"^18.2.0","lodash":"~4.17.1"},"devDependencies":{"jest":">=29.0.0 <30","tag":"latest"}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := depMap(deps)
	if got["react"] != "18.2.0" || got["lodash"] != "4.17.1" || got["jest"] != "29.0.0" || got["tag"] != "latest" {
		t.Errorf("%v", got)
	}
	// stable order: dependencies (sorted), then devDependencies (sorted)
	if deps[0].Name != "lodash" || deps[1].Name != "react" || deps[2].Name != "jest" {
		t.Errorf("order: %v", deps)
	}
	if _, err := parsePackageJSON([]byte("{nope")); err == nil {
		t.Error("malformed JSON must error")
	}
}

func TestParsePomXML(t *testing.T) {
	src := `<project>
  <parent><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-parent</artifactId><version>3.2.1</version></parent>
  <properties><java.version>17</java.version><junitVersion>5.10.0</junitVersion><other>x</other></properties>
  <dependencies>
    <dependency><groupId>g</groupId><artifactId>lib</artifactId><version>1.2.3</version></dependency>
    <dependency><groupId>g</groupId><artifactId>managed</artifactId></dependency>
  </dependencies>
</project>`
	deps, err := parsePomXML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got := depMap(deps)
	if got["spring-boot-starter-parent"] != "3.2.1" || got["lib"] != "1.2.3" || got["java"] != "17" || got["junit"] != "5.10.0" {
		t.Errorf("%v", got)
	}
	if _, ok := got["managed"]; ok {
		t.Error("a dependency with no version has nothing to compare")
	}
}

func TestParseCargoToml(t *testing.T) {
	src := "[package]\nname = \"x\"\nversion = \"0.1.0\"\n\n[dependencies]\nserde = \"1.0.190\"\ntokio = { version = \"1.35\", features = [\"full\"] }\nlocal = { path = \"../local\" }\n\n[dev-dependencies]\ncriterion = \"0.5\" # bench\n\n[features]\nfoo = \"9.9\"\n"
	deps, _ := parseCargoToml([]byte(src))
	got := depMap(deps)
	if got["serde"] != "1.0.190" || got["tokio"] != "1.35" || got["criterion"] != "0.5" {
		t.Errorf("%v", got)
	}
	for _, bad := range []string{"name", "version", "local", "foo"} {
		if _, ok := got[bad]; ok {
			t.Errorf("%q is not a dependency", bad)
		}
	}
}

func TestParsePyproject(t *testing.T) {
	src := `[project]
name = "mypkg"
version = "1.0"
requires-python = ">=3.9"
dependencies = [
  "requests>=2.31.0",
  "pydantic[email]==2.5.1",
  "numpy",
]

[tool.poetry.dependencies]
python = "^3.10"
django = "^4.2"
celery = { version = "^5.3.0", extras = ["redis"] }

[tool.other]
foo = "9.9"
`
	deps, _ := parsePyprojectToml([]byte(src))
	got := depMap(deps)
	want := map[string]string{"requests": "2.31.0", "pydantic": "2.5.1", "django": "4.2", "celery": "5.3.0"}
	if len(got) != len(want) {
		t.Errorf("%v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q want %q (%v)", k, got[k], v, got)
		}
	}
}

func TestParseRequirements(t *testing.T) {
	deps, _ := parseRequirementsTxt([]byte("# c\n\nflask==2.3.1\nrequests>=2.0,<3\nDjango~=4.2\n-r other.txt\nuntyped\n"))
	got := depMap(deps)
	if got["flask"] != "2.3.1" || got["requests"] != "2.0" || got["Django"] != "4.2" || len(got) != 3 {
		t.Errorf("%v", got)
	}
}

func TestScanDepsReadsEveryFileAndSkipsBrokenOnes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module m\n\ngo 1.21\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte("{broken"), 0o644)
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask==2.0.0\n"), 0o644)
	deps, err := ScanDeps(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 2 || deps[0].Source != "go.mod" || deps[1].Source != "requirements.txt" {
		t.Errorf("%+v", deps)
	}
	if got, err := ScanDeps(t.TempDir()); err != nil || len(got) != 0 {
		t.Errorf("empty project: %v %v", got, err)
	}
}

func TestIsCompatibleVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"17", "17.0.1", true}, {"17.0.1", "17", true}, {"3.2", "3.2.1", true}, {"3.2.1", "3.2.1", true},
		{"3.1", "3.10.2", false}, // string prefix, different minor
		{"1", "18", false}, {"3.1", "3.2.1", false}, {"2", "3.0.0", false},
	}
	for _, c := range cases {
		if got := isCompatibleVersion(c.a, c.b); got != c.want {
			t.Errorf("isCompatibleVersion(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestMentionsNameIsAWholeWordMatch(t *testing.T) {
	for text, want := range map[string]bool{
		"Use Go 1.20": true, "use go": true, "go.": true, "(go)": true, "good algorithm": false, "cargo 1.2": false,
		"React 17": true, "reactive 17": false, "spring-boot 2.7": false,
	} {
		name := "go"
		if strings.Contains(text, "eact") {
			name = "react"
		}
		if strings.Contains(text, "spring") {
			name = "spring-boot"
			want = true
		}
		if got := mentionsName(text, name); got != want {
			t.Errorf("mentionsName(%q,%q) = %v, want %v", text, name, got, want)
		}
	}
	// names with regex metacharacters must not blow up or over-match
	if mentionsName("c++ 11", "c++") != true || mentionsName("cxx", "c.x") {
		t.Error("QuoteMeta")
	}
}

func TestCheckPromptFindsStaleVersions(t *testing.T) {
	deps := []DepVersion{{Name: "react", Version: "18.2.0"}, {Name: "go", Version: "1.22"}, {Name: "any", Version: "*"}}
	rp := &ast.ResolvedPrompt{
		Name:         "P",
		Persona:      "You know React 16 well.",
		Objective:    "Ship it in 3 steps, that is a good plan.", // "good" must not match go
		Instructions: []string{"Target Go 1.19.", "Use React 18 hooks.", "any 5"},
	}
	got := checkPrompt(rp, deps)
	var lines []string
	for _, f := range got {
		lines = append(lines, f.Field+":"+f.Dep+":"+f.Mention)
	}
	want := "persona:react:16,instructions:go:1.19"
	if strings.Join(lines, ",") != want {
		t.Errorf("got %v, want %s", lines, want)
	}
	if got[0].Prompt != "P" || got[0].Version != "18.2.0" {
		t.Errorf("%+v", got[0])
	}
}

func TestScanEndToEnd(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module m\n\ngo 1.22\n"), 0o644)
	f, err := Scan([]*ast.ResolvedPrompt{{Name: "A", Notes: "Requires Go 1.18"}, {Name: "B", Notes: "Go 1.22 only"}}, dir)
	if err != nil || len(f) != 1 || f[0].Prompt != "A" || f[0].Mention != "1.18" {
		t.Errorf("%+v %v", f, err)
	}
	if f, _ := Scan([]*ast.ResolvedPrompt{{Name: "A", Notes: "Go 1.1"}}, t.TempDir()); f != nil {
		t.Errorf("no dependency files means nothing to report: %v", f)
	}
	if s := f0String(); !strings.Contains(s, "1.18") || !strings.Contains(s, "1.22") {
		t.Error(s)
	}
}

func f0String() string {
	return Finding{Dep: "go", Version: "1.22", Mention: "1.18", Prompt: "A", Field: "notes"}.String()
}

func TestOutputIsDeterministic(t *testing.T) {
	deps := []DepVersion{{Name: "react", Version: "18.0.0"}}
	rp := &ast.ResolvedPrompt{Name: "P", Summary: "react 1", Persona: "react 2", Context: "react 3", Objective: "react 4", Notes: "react 5",
		Instructions: []string{"react 6"}, Constraints: []string{"react 7"}, Examples: []string{"react 8"}, Format: []string{"react 9"}}
	first := checkPrompt(rp, deps)
	if len(first) != 9 {
		t.Fatalf("%d findings", len(first))
	}
	for i := 0; i < 20; i++ {
		again := checkPrompt(rp, deps)
		for j := range first {
			if again[j] != first[j] {
				t.Fatal("finding order changed between runs")
			}
		}
	}
}
