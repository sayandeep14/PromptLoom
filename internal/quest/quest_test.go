package quest

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

func TestLoadQuestParsesStepsAndDefaults(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "Onboard.quest.toml", `
description = "Bring a new hire up to speed"

[[step]]
name = "summarize"
prompt = "Summarizer"
input = "{{quest.input}}"

[[step]]
name = "plan"
prompt = "Planner"
input = "Given: {{quest.previous}}, make a plan"
vars = { audience = "eng" }
`)
	q, err := LoadQuest(p)
	if err != nil {
		t.Fatal(err)
	}
	if q.Name != "Onboard" || q.Path != p || len(q.Steps) != 2 {
		t.Fatalf("%+v", q)
	}
	if q.Description != "Bring a new hire up to speed" {
		t.Errorf("description = %q", q.Description)
	}
	if q.Steps[1].Vars["audience"] != "eng" {
		t.Errorf("vars = %+v", q.Steps[1].Vars)
	}
}

func TestLoadQuestReadsInputFileRelativeToTheQuestFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ticket.txt", "a bug report")
	p := write(t, dir, "quests/Triage.quest.toml", `
[[step]]
name = "triage"
prompt = "Triager"
input_file = "../ticket.txt"
`)
	q, err := LoadQuest(p)
	if err != nil {
		t.Fatal(err)
	}
	if q.Steps[0].Input != "a bug report" {
		t.Errorf("input = %q", q.Steps[0].Input)
	}
}

func TestLoadQuestRejectsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "Bad.quest.toml", `
weird_key = "x"
[[step]]
name = "a"
prompt = "P"
input = "hi"
`)
	_, err := LoadQuest(p)
	if err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateCatchesEveryBadShape(t *testing.T) {
	cases := map[string]string{
		"no steps": `description = "x"`,
		"unnamed step": `
[[step]]
prompt = "P"
input = "hi"`,
		"duplicate name": `
[[step]]
name = "a"
prompt = "P"
input = "hi"
[[step]]
name = "a"
prompt = "Q"
input = "hi"`,
		"missing prompt": `
[[step]]
name = "a"
input = "hi"`,
		"missing input": `
[[step]]
name = "a"
prompt = "P"`,
		"both input and input_file": `
[[step]]
name = "a"
prompt = "P"
input = "hi"
input_file = "x.txt"`,
		"previous in first step": `
[[step]]
name = "a"
prompt = "P"
input = "{{quest.previous}}"`,
		"unknown template token": `
[[step]]
name = "a"
prompt = "P"
input = "{{quest.oops}}"`,
	}
	for label, body := range cases {
		t.Run(label, func(t *testing.T) {
			dir := t.TempDir()
			p := write(t, dir, "Q.quest.toml", body)
			if _, err := LoadQuest(p); err == nil {
				t.Fatalf("%s: expected an error", label)
			}
		})
	}
}

func TestSubstitute(t *testing.T) {
	got := substitute("in={{quest.input}} prev={{quest.previous}}", "IN", "PREV")
	if got != "in=IN prev=PREV" {
		t.Errorf("got %q", got)
	}
}

func TestFindQuests(t *testing.T) {
	dir := t.TempDir()
	if got, err := FindQuests(filepath.Join(dir, "missing")); err != nil || got != nil {
		t.Fatalf("missing dir: %v %v", got, err)
	}
	write(t, dir, "B.quest.toml", "[[step]]\nname=\"a\"\nprompt=\"P\"\ninput=\"hi\"")
	write(t, dir, "A.quest.toml", "[[step]]\nname=\"a\"\nprompt=\"P\"\ninput=\"hi\"")
	write(t, dir, "ignored.txt", "x")
	got, err := FindQuests(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "A.quest.toml"), filepath.Join(dir, "B.quest.toml")}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v want %v", got, want)
	}
}
