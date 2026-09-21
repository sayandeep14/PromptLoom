package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/llm"
)

// ---- sanitizer ----

func TestSanitizeRemovesTerminalControlSequences(t *testing.T) {
	cases := map[string]string{
		"plain text":                             "plain text",
		"line one\nline two\tindented":           "line one\nline two\tindented",
		"日本語 é 😀":                                "日本語 é 😀",
		"\x1b[31mred\x1b[0m text":                "red text",
		"\x1b[2J\x1b[Hclear":                     "clear",
		"before\x1b]0;evil title\x07after":       "beforeafter",
		"a\x1b]52;c;ZXZpbA==\x1b\\b":             "ab", // OSC 52 writes to the clipboard
		"x\x1bPdevice control\x1b\\y":            "xy",
		"a\x1bcb":                                "ab", // ESC c resets the terminal
		"bell\x07 and null\x00 and del\x7f gone": "bell and null and del gone",
		"c1\u009b31m controls":                   "c131m controls",
		"carriage\rreturn":                       "carriage\nreturn", // could overwrite a line
		"backspace\x08\x08\x08hidden":            "backspacehidden",
		"\x1b[?1049h alt screen":                 " alt screen",
		"unterminated \x1b[31":                   "unterminated ",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

// A stream delivers text in arbitrary pieces; a sequence cut in two must still be removed.
func TestSanitizerRemovesSequencesSplitAcrossChunks(t *testing.T) {
	input := "a\x1b[31mred\x1b[0m b\x1b]0;title\x07 c\x1b]52;c;ZXZpbA==\x1b\\ d"
	want := "ared b c d"
	for size := 1; size <= len(input); size++ {
		var f Sanitizer
		var got strings.Builder
		for i := 0; i < len(input); i += size {
			got.WriteString(f.Filter(input[i:min(i+size, len(input))]))
		}
		if got.String() != want {
			t.Errorf("chunks of %d: %q, want %q", size, got.String(), want)
		}
	}
}

// ---- session ----

type fakeModel struct {
	replies []string
	err     error
	partial string
	seen    []llm.Request
	stream  bool
}

func (f *fakeModel) next() string {
	r := f.replies[0]
	f.replies = f.replies[1:]
	return r
}

func (f *fakeModel) Stream(_ context.Context, r llm.Request, onDelta func(string)) (string, llm.Usage, error) {
	f.seen = append(f.seen, r)
	f.stream = true
	if f.err != nil {
		if f.partial != "" && onDelta != nil {
			onDelta(f.partial)
		}
		return f.partial, llm.Usage{}, f.err
	}
	text := f.next()
	for _, w := range strings.SplitAfter(text, " ") {
		if onDelta != nil {
			onDelta(w)
		}
	}
	return text, llm.Usage{InputTokens: 10, OutputTokens: 2}, nil
}

func (f *fakeModel) Complete(_ context.Context, r llm.Request) (string, error) {
	f.seen = append(f.seen, r)
	if f.err != nil {
		return "", f.err
	}
	return f.next(), nil
}

func TestSessionKeepsHistoryAndSendsItInOrder(t *testing.T) {
	m := &fakeModel{replies: []string{"first answer", "second answer", "third answer"}}
	s := &Session{Model: m, System: "you are helpful", Stream: true, MaxTokens: 100}

	var pieces []string
	r, err := s.Send(context.Background(), "one", func(d string) { pieces = append(pieces, d) })
	if err != nil || r.Text != "first answer" || len(pieces) != 2 || r.Usage.OutputTokens != 2 || r.Partial {
		t.Fatalf("%+v %v %v", r, err, pieces)
	}
	if _, err := s.Send(context.Background(), "two", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(context.Background(), "three", nil); err != nil {
		t.Fatal(err)
	}
	if s.Turns() != 3 {
		t.Errorf("turns = %d", s.Turns())
	}
	last := m.seen[2]
	if last.System != "you are helpful" || last.User != "three" || last.MaxTokens != 100 || len(last.History) != 4 {
		t.Fatalf("%+v", last)
	}
	want := []llm.Message{{Role: "user", Content: "one"}, {Role: "assistant", Content: "first answer"}, {Role: "user", Content: "two"}, {Role: "assistant", Content: "second answer"}}
	for i, w := range want {
		if last.History[i] != w {
			t.Errorf("history[%d] = %+v, want %+v", i, last.History[i], w)
		}
	}
	// the first request had no history
	if len(m.seen[0].History) != 0 {
		t.Errorf("%+v", m.seen[0])
	}
	s.Reset()
	if s.Turns() != 0 || len(s.History()) != 0 || s.System != "you are helpful" {
		t.Error("reset clears the conversation but keeps the system prompt")
	}
}

func TestNonStreamingSessionUsesOneCall(t *testing.T) {
	m := &fakeModel{replies: []string{"whole reply"}}
	s := &Session{Model: m, System: "s"}
	var got []string
	r, err := s.Send(context.Background(), "q", func(d string) { got = append(got, d) })
	if err != nil || r.Text != "whole reply" || len(got) != 1 || got[0] != "whole reply" || m.stream {
		t.Errorf("%+v %v %v", r, err, got)
	}
}

// A failed or cancelled turn must not poison the history: the same message can be sent again.
func TestFailedTurnLeavesTheHistoryAlone(t *testing.T) {
	m := &fakeModel{replies: []string{"ok one", "ok two"}}
	s := &Session{Model: m, Stream: true}
	s.Send(context.Background(), "one", nil)

	m.err, m.partial = context.Canceled, "half an ans"
	r, err := s.Send(context.Background(), "two", nil)
	if !errors.Is(err, context.Canceled) || !r.Partial || r.Text != "half an ans" {
		t.Errorf("%+v %v", r, err)
	}
	if s.Turns() != 1 || len(s.History()) != 2 {
		t.Errorf("a failed turn was recorded: %+v", s.History())
	}
	m.err = nil
	if r, err := s.Send(context.Background(), "two", nil); err != nil || r.Text != "ok two" || s.Turns() != 2 {
		t.Errorf("retry: %+v %v", r, err)
	}
	if got := m.seen[2].History; len(got) != 2 {
		t.Errorf("the retry must not include the failed attempt: %+v", got)
	}
}

func TestTurnLimit(t *testing.T) {
	m := &fakeModel{replies: []string{"a", "b", "c"}}
	s := &Session{Model: m, MaxTurns: 2}
	s.Send(context.Background(), "1", nil)
	s.Send(context.Background(), "2", nil)
	_, err := s.Send(context.Background(), "3", nil)
	if err == nil || !strings.Contains(err.Error(), "reached 2 turns") || !strings.Contains(err.Error(), "/reset") || len(m.seen) != 2 {
		t.Errorf("%v (%d calls)", err, len(m.seen))
	}
	if (&Session{Model: m}).MaxTurns != 0 || DefaultMaxTurns != 50 {
		t.Error("default")
	}
}

func TestContractIsCheckedOnEveryReply(t *testing.T) {
	ct := &ast.ContractBlock{MustInclude: []string{"verdict"}}
	m := &fakeModel{replies: []string{"verdict: fine", "no such word"}}
	s := &Session{Model: m, Contract: ct}
	if r, _ := s.Send(context.Background(), "a", nil); len(r.ContractFailures) != 0 {
		t.Errorf("%+v", r.ContractFailures)
	}
	if r, _ := s.Send(context.Background(), "b", nil); len(r.ContractFailures) != 1 {
		t.Errorf("%+v", r.ContractFailures)
	}
}

func TestRequestShowsExactlyWhatWouldBeSent(t *testing.T) {
	s := &Session{Model: &fakeModel{replies: []string{"a"}}, System: "sys", MaxTokens: 5}
	s.Send(context.Background(), "one", nil)
	r := s.Request("two")
	if r.System != "sys" || r.User != "two" || r.MaxTokens != 5 || len(r.History) != 2 {
		t.Errorf("%+v", r)
	}
	if s.Turns() != 1 {
		t.Error("building a request must not change the session")
	}
}

// ---- permissions ----

func project(t *testing.T, cfg string) string {
	dir := t.TempDir()
	if cfg != "" {
		os.WriteFile(filepath.Join(dir, ".loom.config"), []byte(cfg), 0o644)
	}
	for _, f := range []string{"src/a.go", "src/deep/b.go", "docs/guide.md", "README.md", "secrets/key.txt", ".env"} {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(f)), 0o755)
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644)
	}
	return dir
}

func TestPermissionDefaultsAllowEverything(t *testing.T) {
	for _, cfg := range []string{"", `{"custom":{}}`, `{"permission":{"read":["*"],"write":["*"]}}`} {
		dir := project(t, cfg)
		p, err := LoadPermission(dir)
		if err != nil || p.CheckRead(filepath.Join(dir, "secrets/key.txt")) != nil || p.CheckWrite("out.md") != nil || !p.AllowsAllReads() {
			t.Errorf("%q: %v", cfg, err)
		}
	}
}

func TestPermissionPatterns(t *testing.T) {
	dir := project(t, `{"permission":{"read":["src/**","docs/*.md","README.md","*.txt"],"write":["out/"]}}`)
	p, err := LoadPermission(dir)
	if err != nil {
		t.Fatal(err)
	}
	ok := []string{"src/a.go", "src/deep/b.go", "src", "docs/guide.md", "README.md", "secrets/key.txt"} // the last by "*.txt"
	deny := []string{".env", "docs/other.pdf", "Makefile", "srcx/a.go"}
	for _, f := range ok {
		if err := p.CheckRead(f); err != nil {
			t.Errorf("read %s should be allowed: %v", f, err)
		}
		if err := p.CheckRead(filepath.Join(dir, f)); err != nil {
			t.Errorf("absolute %s: %v", f, err)
		}
	}
	for _, f := range deny {
		err := p.CheckRead(f)
		if err == nil || !strings.Contains(err.Error(), "permission.read") || !strings.Contains(err.Error(), "src/**") {
			t.Errorf("read %s must be refused with the setting named: %v", f, err)
		}
	}
	if p.AllowsAllReads() {
		t.Error("restricted")
	}
	// write is separate
	if p.CheckWrite("out/transcript.md") != nil || p.CheckWrite("out/sub/x.md") != nil {
		t.Error("out/ is writable")
	}
	if err := p.CheckWrite("src/a.go"); err == nil || !strings.Contains(err.Error(), "permission.write") {
		t.Errorf("%v", err)
	}
}

func TestPermissionEmptyListAllowsNothingAndPathsOutsideTheProjectAreRefused(t *testing.T) {
	dir := project(t, `{"permission":{"read":[],"write":["out/**"]}}`)
	p, _ := LoadPermission(dir)
	if err := p.CheckRead("README.md"); err == nil || !strings.Contains(err.Error(), "allowed: nothing") {
		t.Errorf("%v", err)
	}
	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	os.WriteFile(outside, []byte("x"), 0o644)
	dir2 := project(t, `{"permission":{"read":["src/**","**"]}}`)
	p2, _ := LoadPermission(dir2)
	if err := p2.CheckRead(outside); err == nil {
		t.Error("a path outside the project must not match project-relative patterns")
	}
	if err := p2.CheckRead("../../etc/passwd"); err == nil {
		t.Error("dotdot escapes")
	}
}

func TestPermissionResolvesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := project(t, `{"permission":{"read":["src/**"]}}`)
	// src/link → ../secrets/key.txt : allowed by name, but it points outside src/
	if err := os.Symlink(filepath.Join(dir, "secrets", "key.txt"), filepath.Join(dir, "src", "link.txt")); err != nil {
		t.Skip(err)
	}
	p, _ := LoadPermission(dir)
	if err := p.CheckRead("src/link.txt"); err == nil {
		t.Error("a link inside an allowed directory must not expose a file outside it")
	}
}

func TestPermissionConfigIsFoundFromASubdirectoryAndBrokenConfigIsAnError(t *testing.T) {
	dir := project(t, `{"permission":{"read":["docs/**"]}}`)
	p, err := LoadPermission(filepath.Join(dir, "src", "deep"))
	if err != nil || p.CheckRead("docs/guide.md") != nil || p.CheckRead("README.md") == nil {
		t.Errorf("%v", err)
	}
	bad := project(t, `{not json`)
	if _, err := LoadPermission(bad); err == nil || !strings.Contains(err.Error(), ".loom.config") {
		t.Errorf("%v", err)
	}
}

func TestMatchesTable(t *testing.T) {
	for _, c := range []struct {
		pat, rel string
		want     bool
	}{
		{"src/**", "src/a.go", true}, {"src/*", "src/deep/b.go", true}, {"src/", "src/a.go", true}, {"src", "src", true}, {"src", "src/a.go", false},
		{"*.go", "src/deep/b.go", true}, {"a.go", "src/a.go", true}, {"docs/*.md", "docs/x.md", true}, {"docs/*.md", "docs/sub/x.md", false},
		{"", "a", false}, {"src/**", "srcx/a", false}, {"src/**", "src", true},
	} {
		if got := matches(c.pat, c.rel); got != c.want {
			t.Errorf("matches(%q, %q) = %v, want %v", c.pat, c.rel, got, c.want)
		}
	}
}

func TestCheckSourcesRunsBeforeAnythingIsRead(t *testing.T) {
	dir := project(t, `{"permission":{"read":["src/**"]}}`)
	p, _ := LoadPermission(dir)
	if err := p.CheckSources(dir, []string{"file:src/a.go", "dir:src/deep", "stdin"}, ""); err != nil {
		t.Errorf("%v", err)
	}
	for _, bad := range [][]string{{"file:.env"}, {"dir:secrets"}, {"file:../outside.txt"}, {"git:diff"}, {"git:staged"}} {
		if err := p.CheckSources(dir, bad, ""); err == nil || !strings.Contains(err.Error(), "--with "+bad[0]) {
			t.Errorf("%v must be refused, naming the source: %v", bad, err)
		}
	}
	if err := p.CheckSources(dir, nil, "review-kit"); err == nil || !strings.Contains(err.Error(), "--context review-kit") {
		t.Errorf("bundles need unrestricted reads: %v", err)
	}
	// with unrestricted reads (the default) everything passes
	open, _ := LoadPermission(project(t, ""))
	if err := open.CheckSources(dir, []string{"file:.env", "git:diff"}, "b"); err != nil {
		t.Errorf("%v", err)
	}
}
