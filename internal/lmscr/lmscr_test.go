package lmscr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadScriptParsesStepsAndVars(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "Release.lmscr", `
description = "check, then deploy"
vars = { env = "staging" }

[[step]]
name = "check"
run = "eval"
args = ["--compare"]

[[step]]
name = "deploy"
run = "deploy"
args = ["--target", "{{vars.env}}"]
when = "on_success"
`)
	s, err := LoadScript(p)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "Release" || s.Path != p || len(s.Steps) != 2 || s.Description != "check, then deploy" {
		t.Fatalf("%+v", s)
	}
	if s.Vars["env"] != "staging" {
		t.Errorf("vars = %+v", s.Vars)
	}
	if s.Steps[1].Run != "deploy" || s.Steps[1].Args[1] != "{{vars.env}}" {
		t.Errorf("%+v", s.Steps[1])
	}
	if s.Steps[0].Effective() != "on_success" || s.Steps[1].Effective() != "on_success" {
		t.Errorf("Effective defaults: %+v", s.Steps)
	}
}

func TestLoadScriptRejectsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "Bad.lmscr", `
weird_key = "x"
[[step]]
name = "a"
run = "weave"
`)
	_, err := LoadScript(p)
	if err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateCatchesEveryBadShape(t *testing.T) {
	cases := map[string]string{
		"no steps": `description = "x"`,
		"unnamed step": `
[[step]]
run = "weave"`,
		"duplicate name": `
[[step]]
name = "a"
run = "weave"
[[step]]
name = "a"
run = "eval"`,
		"missing run": `
[[step]]
name = "a"`,
		"unknown when": `
[[step]]
name = "a"
run = "weave"
when = "sometimes"`,
	}
	for label, body := range cases {
		t.Run(label, func(t *testing.T) {
			dir := t.TempDir()
			p := write(t, dir, "S.lmscr", body)
			if _, err := LoadScript(p); err == nil {
				t.Fatalf("%s: expected an error", label)
			}
		})
	}
}

func TestReferencedVarsAndSubstitute(t *testing.T) {
	s := &Script{Steps: []Step{
		{Args: []string{"--target", "{{vars.env}}"}},
		{Args: []string{"{{vars.env}}-{{vars.repo}}"}},
	}}
	got := s.referencedVars()
	if len(got) != 2 || got[0] != "env" || got[1] != "repo" {
		t.Fatalf("got %v", got)
	}
	out := Substitute("{{vars.env}}-{{vars.repo}}", map[string]string{"env": "prod", "repo": "demo"})
	if out != "prod-demo" {
		t.Errorf("got %q", out)
	}
}

func TestFindScripts(t *testing.T) {
	dir := t.TempDir()
	if got, err := FindScripts(filepath.Join(dir, "missing")); err != nil || got != nil {
		t.Fatalf("missing dir: %v %v", got, err)
	}
	write(t, dir, "B.lmscr", "[[step]]\nname=\"a\"\nrun=\"weave\"")
	write(t, dir, "A.lmscr", "[[step]]\nname=\"a\"\nrun=\"weave\"")
	write(t, dir, "ignored.txt", "x")
	got, err := FindScripts(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "A.lmscr"), filepath.Join(dir, "B.lmscr")}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v want %v", got, want)
	}
}
