package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/agent"
	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/llm"
	"github.com/sayandeep14/PromptLoom/internal/tui"
)

const runToml = `[project]
name = "t"
version = "0"
[paths]
prompts = "prompts"
blocks = "blocks"
overlays = "overlays"
out = "dist"
[validation]
require_objective = false
require_format = false
`

const runPrompt = `prompt Reviewer {
  slot repo { required: true }
  slot token { secret: true, required: false }
  persona :=
    You review code for {{repo}}.
  instructions :=
    - Be concise.
  contract {
    must_include:
      - verdict
  }
}

prompt Plain {
  persona :=
    You are helpful.
}
`

func runProject(t *testing.T, loomConfig string) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "loom.toml"), []byte(runToml), 0o644)
	os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
	os.WriteFile(filepath.Join(dir, "prompts", "p.prompt.loom"), []byte(runPrompt), 0o644)
	if loomConfig != "" {
		os.WriteFile(filepath.Join(dir, ".loom.config"), []byte(loomConfig), 0o644)
	}
	os.WriteFile(filepath.Join(dir, "change.patch"), []byte("+ a change"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("API_KEY=hunter2"), 0o644)
	return dir
}

// scripted is a model that replays answers and records what it was sent.
type scripted struct {
	replies []string
	seen    []llm.Request
	err     error // returned instead of an answer
	partial string
}

func (s *scripted) Stream(_ context.Context, r llm.Request, onDelta func(string)) (string, llm.Usage, error) {
	s.seen = append(s.seen, r)
	if s.err != nil {
		if s.partial != "" {
			onDelta(s.partial)
		}
		return s.partial, llm.Usage{}, s.err
	}
	text := s.replies[0]
	s.replies = s.replies[1:]
	for _, w := range strings.SplitAfter(text, " ") {
		onDelta(w)
	}
	return text, llm.Usage{InputTokens: 11, OutputTokens: 3}, nil
}

func (s *scripted) Complete(ctx context.Context, r llm.Request) (string, error) {
	text, _, err := s.Stream(ctx, r, func(string) {})
	return text, err
}

type harness struct {
	out, errw bytes.Buffer
	model     *scripted
	created   bool
}

func (h *harness) params(dir, name string) runParams {
	return runParams{
		Name: name, Cwd: dir,
		Weave:    tui.WeaveOptions{Variables: map[string]string{"repo": "demo"}},
		Stdin:    strings.NewReader(""),
		Stdout:   &h.out,
		Stderr:   &h.errw,
		MaxTurns: agent.DefaultMaxTurns,
		NewModel: func(*config.Config, string, string) (agent.Model, error) {
			h.created = true
			return h.model, nil
		},
		TurnContext: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
	}
}

func newHarness(replies ...string) *harness { return &harness{model: &scripted{replies: replies}} }

func TestRunStreamsTheAnswerAndSendsTheRenderedPromptAsTheSystemMessage(t *testing.T) {
	dir, h := runProject(t, ""), newHarness("verdict: looks fine to me")
	p := h.params(dir, "Reviewer")
	p.Input = "please review"
	if err := executeRun(p); err != nil {
		t.Fatal(err)
	}
	if h.out.String() != "verdict: looks fine to me" {
		t.Errorf("piped output is exact, with no decoration: %q", h.out.String())
	}
	r := h.model.seen[0]
	if !strings.Contains(r.System, "You review code for demo.") || !strings.Contains(r.System, "Be concise.") || r.User != "please review" || len(r.History) != 0 {
		t.Errorf("%+v", r)
	}
	if strings.Contains(r.System, "{{") {
		t.Error("variables must be substituted")
	}
	if h.errw.Len() != 0 {
		t.Errorf("nothing on stderr when not a terminal: %q", h.errw.String())
	}
}

func TestInputSources(t *testing.T) {
	dir := runProject(t, "")
	// --input-file
	h := newHarness("verdict ok")
	p := h.params(dir, "Reviewer")
	p.InputFile = "change.patch"
	if err := executeRun(p); err != nil || h.model.seen[0].User != "+ a change" {
		t.Fatalf("%v %+v", err, h.model.seen)
	}
	// piped stdin
	h = newHarness("verdict ok")
	p = h.params(dir, "Reviewer")
	p.Stdin = strings.NewReader("from a pipe\nsecond line\n")
	if err := executeRun(p); err != nil || h.model.seen[0].User != "from a pipe\nsecond line\n" {
		t.Fatalf("%v %+v", err, h.model.seen)
	}
	// a terminal on stdin means "no input": the prompt alone is sent
	h = newHarness("verdict ok")
	p = h.params(dir, "Reviewer")
	p.StdinTTY = true
	if err := executeRun(p); err != nil || h.model.seen[0].User != defaultTask {
		t.Fatalf("%v %+v", err, h.model.seen)
	}
	// both flags, and a missing file
	p = h.params(dir, "Reviewer")
	p.Input, p.InputFile = "a", "b"
	if err := executeRun(p); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("%v", err)
	}
	p = h.params(dir, "Reviewer")
	p.InputFile = "missing.txt"
	if err := executeRun(p); err == nil || !strings.Contains(err.Error(), "--input-file") {
		t.Errorf("%v", err)
	}
}

// A model's reply is untrusted text: on a terminal, escape sequences must not reach it.
func TestTerminalOutputIsSanitisedButPipedOutputIsNot(t *testing.T) {
	evil := "verdict \x1b]0;pwned\x07\x1b[31mred\x1b[0m \x1b]52;c;ZXZpbA==\x1b\\done"
	dir := runProject(t, "")

	h := newHarness(evil)
	p := h.params(dir, "Reviewer")
	p.OutTTY = true
	p.Input = "x"
	if err := executeRun(p); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.out.String(), "\x1b") || strings.Contains(h.out.String(), "pwned") || !strings.HasPrefix(h.out.String(), "verdict red done") || !strings.HasSuffix(h.out.String(), "\n") {
		t.Errorf("terminal output: %q", h.out.String())
	}
	if !strings.Contains(h.errw.String(), "11 in / 3 out tokens") {
		t.Errorf("the footer reports usage on a terminal: %q", h.errw.String())
	}

	h = newHarness(evil)
	p = h.params(dir, "Reviewer")
	p.Input = "x"
	executeRun(p)
	if h.out.String() != evil {
		t.Errorf("piped output must be byte for byte what the model said: %q", h.out.String())
	}
}

func TestDryRunShowsEverythingAndCallsNothing(t *testing.T) {
	dir, h := runProject(t, ""), newHarness()
	p := h.params(dir, "Reviewer")
	p.DryRun, p.Input, p.Model, p.MaxTokens = true, "the question", "openai:gpt-x", 300
	if err := executeRun(p); err != nil {
		t.Fatal(err)
	}
	out := h.out.String()
	for _, want := range []string{"model: openai:gpt-x", "max tokens: 300", "You review code for demo.", "── message ──\nthe question", "nothing was sent"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if h.created || len(h.model.seen) != 0 {
		t.Error("a dry run must not even build a client (it needs no API key)")
	}
	// the same for chat
	p = h.params(dir, "Reviewer")
	p.DryRun, p.Chat = true, true
	h.out.Reset()
	if err := executeRun(p); err != nil || !strings.Contains(h.out.String(), "conversation") {
		t.Errorf("%v\n%s", err, h.out.String())
	}
}

func TestCheckEnforcesTheContract(t *testing.T) {
	dir := runProject(t, "")
	h := newHarness("this answer never states the word")
	p := h.params(dir, "Reviewer")
	p.Input, p.Check = "x", true
	err := executeRun(p)
	if err == nil || !strings.Contains(err.Error(), "violates the prompt's contract") || !strings.Contains(h.errw.String(), `required content "verdict" not found`) {
		t.Errorf("%v / %q", err, h.errw.String())
	}
	if h.out.String() == "" {
		t.Error("the answer is still shown")
	}
	// without --check the same answer is not an error
	h = newHarness("this answer never states the word")
	p = h.params(dir, "Reviewer")
	p.Input = "x"
	if err := executeRun(p); err != nil {
		t.Errorf("%v", err)
	}
	// a passing answer
	h = newHarness("verdict: fine")
	p = h.params(dir, "Reviewer")
	p.Input, p.Check = "x", true
	if err := executeRun(p); err != nil {
		t.Errorf("%v", err)
	}
	// a prompt with no contract cannot violate one
	h = newHarness("anything")
	p = h.params(dir, "Plain")
	p.Input, p.Check = "x", true
	if err := executeRun(p); err != nil {
		t.Errorf("%v", err)
	}
}

func TestJSONOutput(t *testing.T) {
	dir, h := runProject(t, ""), newHarness("verdict: ok")
	p := h.params(dir, "Reviewer")
	p.Input, p.JSON = "hello", true
	if err := executeRun(p); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Prompt, Model, Input, Output string
		Usage                        struct{ Input_tokens, Output_tokens int }
		Contract_failures            []string
	}
	if err := json.Unmarshal(h.out.Bytes(), &got); err != nil {
		t.Fatalf("%v\n%s", err, h.out.String())
	}
	if got.Prompt != "Reviewer" || got.Input != "hello" || got.Output != "verdict: ok" || got.Usage.Output_tokens != 3 || len(got.Contract_failures) != 0 || !strings.Contains(got.Model, ":") {
		t.Errorf("%+v", got)
	}
	// the JSON is the only thing on stdout, and --chat is refused
	p = h.params(dir, "Reviewer")
	p.JSON, p.Chat = true, true
	if err := executeRun(p); err == nil || !strings.Contains(err.Error(), "--json") {
		t.Errorf("%v", err)
	}
}

// ---- conversation ----

func TestChatKeepsHistoryAndHandlesCommands(t *testing.T) {
	dir := runProject(t, "")
	h := newHarness("verdict one", "verdict two", "verdict three")
	p := h.params(dir, "Reviewer")
	p.Chat = true
	p.Stdin = strings.NewReader("first question\n\nsecond question\n/show\n/reset\nthird question\n/exit\nnever sent\n")
	if err := executeRun(p); err != nil {
		t.Fatal(err)
	}
	if len(h.model.seen) != 3 {
		t.Fatalf("%d requests (blank lines and commands are not messages, and nothing after /exit is sent)", len(h.model.seen))
	}
	if len(h.model.seen[0].History) != 0 || len(h.model.seen[1].History) != 2 || h.model.seen[1].History[1].Content != "verdict one" {
		t.Errorf("history: %+v", h.model.seen)
	}
	if len(h.model.seen[2].History) != 0 || h.model.seen[2].User != "third question" {
		t.Errorf("/reset must clear the history: %+v", h.model.seen[2])
	}
	if !strings.Contains(h.errw.String(), "You review code for demo.") || !strings.Contains(h.errw.String(), "conversation cleared") {
		t.Errorf("/show and /reset report on stderr: %q", h.errw.String())
	}
	if !strings.Contains(h.out.String(), "verdict oneverdict twoverdict three") {
		t.Errorf("%q", h.out.String())
	}
}

func TestChatFirstMessageAndTurnLimit(t *testing.T) {
	dir := runProject(t, "")
	h := newHarness("verdict a", "verdict b", "verdict c")
	p := h.params(dir, "Reviewer")
	p.Chat, p.Input, p.MaxTurns = true, "opening", 2
	p.Stdin = strings.NewReader("second\nthird\n")
	if err := executeRun(p); err != nil {
		t.Fatal(err)
	}
	if h.model.seen[0].User != "opening" || len(h.model.seen) != 2 {
		t.Errorf("the opening message counts as turn 1 and the limit stops the next: %d requests", len(h.model.seen))
	}
	if !strings.Contains(h.errw.String(), "reached 2 turns") {
		t.Errorf("%q", h.errw.String())
	}
}

func TestInterruptedTurnInChatDoesNotEndTheConversation(t *testing.T) {
	dir := runProject(t, "")
	h := newHarness("verdict later")
	p := h.params(dir, "Reviewer")
	p.Chat = true
	p.Stdin = strings.NewReader("slow question\nretry\n")
	// the first request is cancelled after some output; the second succeeds
	calls := 0
	p.NewModel = func(*config.Config, string, string) (agent.Model, error) {
		return modelFunc(func(ctx context.Context, r llm.Request, onDelta func(string)) (string, llm.Usage, error) {
			calls++
			h.model.seen = append(h.model.seen, r)
			if calls == 1 {
				onDelta("half of an ans")
				return "half of an ans", llm.Usage{}, context.Canceled
			}
			onDelta("verdict later")
			return "verdict later", llm.Usage{}, nil
		}), nil
	}
	if err := executeRun(p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.errw.String(), "interrupted") || calls != 2 {
		t.Errorf("%q (%d calls)", h.errw.String(), calls)
	}
	if len(h.model.seen[1].History) != 0 {
		t.Errorf("the interrupted exchange must not be in the history: %+v", h.model.seen[1].History)
	}
}

type modelFunc func(ctx context.Context, r llm.Request, onDelta func(string)) (string, llm.Usage, error)

func (f modelFunc) Stream(ctx context.Context, r llm.Request, onDelta func(string)) (string, llm.Usage, error) {
	return f(ctx, r, onDelta)
}
func (f modelFunc) Complete(ctx context.Context, r llm.Request) (string, error) {
	t, _, err := f(ctx, r, func(string) {})
	return t, err
}

func TestInterruptedSingleRunKeepsThePartialAnswerAndFails(t *testing.T) {
	dir := runProject(t, "")
	h := newHarness()
	h.model.err, h.model.partial = context.Canceled, "verdict so f"
	p := h.params(dir, "Reviewer")
	p.Input = "x"
	err := executeRun(p)
	if err == nil || err.Error() != "interrupted" || h.out.String() != "verdict so f" || !strings.Contains(h.errw.String(), "(interrupted)") {
		t.Errorf("%v %q %q", err, h.out.String(), h.errw.String())
	}
}

// ---- safety ----

func TestPermissionReadBlocksAttachmentsBeforeAnythingIsSent(t *testing.T) {
	dir := runProject(t, `{"permission":{"read":["change.patch","prompts/**"],"write":["out/**"]}}`)

	for _, with := range [][]string{{"file:.env"}, {"dir:."}, {"git:diff"}} {
		h := newHarness("verdict")
		p := h.params(dir, "Reviewer")
		p.Weave.WithSources, p.Input = with, "x"
		err := executeRun(p)
		if err == nil || !strings.Contains(err.Error(), "--with "+with[0]) {
			t.Errorf("%v: %v", with, err)
		}
		if h.created || len(h.model.seen) != 0 {
			t.Errorf("%v: nothing may be sent (or even prepared) when the attachment is refused", with)
		}
	}
	// an allowed attachment works, and so does an allowed input file
	h := newHarness("verdict ok")
	p := h.params(dir, "Reviewer")
	p.Weave.WithSources, p.InputFile = []string{"file:change.patch"}, "change.patch"
	if err := executeRun(p); err != nil || !strings.Contains(h.model.seen[0].System, "+ a change") {
		t.Errorf("%v", err)
	}
	// a forbidden --input-file
	h = newHarness("verdict")
	p = h.params(dir, "Reviewer")
	p.InputFile = ".env"
	if err := executeRun(p); err == nil || !strings.Contains(err.Error(), "permission.read") || len(h.model.seen) != 0 {
		t.Errorf("%v", err)
	}
	// a bundle needs unrestricted reads
	p = h.params(dir, "Reviewer")
	p.Weave.ContextBundle = "kit"
	if err := executeRun(p); err == nil || !strings.Contains(err.Error(), "--context kit") {
		t.Errorf("%v", err)
	}
}

func TestTranscriptIsWrittenPrivatelyAndObeysPermissionWrite(t *testing.T) {
	dir := runProject(t, `{"permission":{"read":["*"],"write":["out/**"]}}`)

	h := newHarness("verdict: alpha", "verdict: beta")
	p := h.params(dir, "Reviewer")
	p.Chat, p.Out = true, "out/session.md"
	p.Stdin = strings.NewReader("hello\nagain\n")
	if err := executeRun(p); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "out", "session.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# loom run: Reviewer", "## System", "You review code for demo.", "## You\n\nhello", "## Assistant\n\nverdict: alpha", "## You\n\nagain", "verdict: beta"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("transcript lacks %q:\n%s", want, data)
		}
	}
	if fi, _ := os.Stat(filepath.Join(dir, "out", "session.md")); fi.Mode().Perm() != 0o600 && filepath.Separator == '/' {
		t.Errorf("a transcript may hold anything that was sent, so it is private: %v", fi.Mode().Perm())
	}
	// outside the writable area: refused before the model is called
	h = newHarness("verdict")
	p = h.params(dir, "Reviewer")
	p.Out, p.Input = "src/answer.md", "x"
	if err := executeRun(p); err == nil || !strings.Contains(err.Error(), "permission.write") || h.created {
		t.Errorf("%v (created=%v)", err, h.created)
	}
}

func TestSecretSlotsCannotBeSetOnTheCommandLine(t *testing.T) {
	dir, h := runProject(t, ""), newHarness("verdict")
	p := h.params(dir, "Reviewer")
	p.Weave.Variables = map[string]string{"repo": "demo", "token": "sk-live-123"}
	err := executeRun(p)
	if err == nil || strings.Contains(err.Error(), "sk-live-123") || len(h.model.seen) != 0 {
		t.Errorf("%v", err)
	}
}

func TestPreparationErrors(t *testing.T) {
	dir := runProject(t, "")
	h := newHarness("verdict")
	p := h.params(dir, "Reviewer")
	p.Weave.Variables = map[string]string{} // the required slot has no value
	if err := executeRun(p); err == nil || !strings.Contains(err.Error(), "repo") || len(h.model.seen) != 0 {
		t.Errorf("%v", err)
	}
	if err := executeRun(h.params(dir, "Nope")); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("%v", err)
	}
	// no API key / a locked key: the client cannot be built, and the message says why
	p = h.params(dir, "Reviewer")
	p.Input = "x"
	p.NewModel = func(*config.Config, string, string) (agent.Model, error) {
		return nil, errors.New("API key not set: $GEMINI_API_KEY is empty")
	}
	if err := executeRun(p); err == nil || !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Errorf("%v", err)
	}
	// an unknown provider in --model
	p = h.params(dir, "Reviewer")
	p.Model, p.Input = "mistral:large", "x"
	if err := executeRun(p); err != nil && !strings.Contains(err.Error(), "unknown provider") && !strings.Contains(err.Error(), "invalid") {
		t.Errorf("%v", err)
	}
}

func TestNoStreamUsesOneCall(t *testing.T) {
	dir, h := runProject(t, ""), newHarness("verdict whole")
	p := h.params(dir, "Reviewer")
	p.Input, p.NoStream = "x", true
	if err := executeRun(p); err != nil || h.out.String() != "verdict whole" {
		t.Errorf("%v %q", err, h.out.String())
	}
}

// The whole path with the real client: env key, SSE from a (fake) Gemini, key only in a header.
func TestRunOverTheRealClient(t *testing.T) {
	const key = "AIza-RUN-SECRET-KEY"
	var gotKey, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotKey, gotPath, gotBody = r.Header.Get("x-goog-api-key"), r.URL.String(), string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, piece := range []string{"verdict: ", "streamed ", "for real"} {
			fmt.Fprintf(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":%q}]}}]}\n\n", piece)
			fl.Flush()
		}
	}))
	defer srv.Close()
	old := llm.GeminiBaseURL
	llm.GeminiBaseURL = srv.URL + "/v1beta"
	defer func() { llm.GeminiBaseURL = old }()
	t.Setenv("GEMINI_API_KEY", key)

	dir := runProject(t, "")
	var out, errw bytes.Buffer
	err := executeRun(runParams{
		Name: "Reviewer", Cwd: dir, Input: "hi", Check: true, MaxTurns: 10,
		Weave:  tui.WeaveOptions{Variables: map[string]string{"repo": "demo"}},
		Stdout: &out, Stderr: &errw, Stdin: strings.NewReader(""),
	})
	if err != nil || out.String() != "verdict: streamed for real" {
		t.Fatalf("%v %q", err, out.String())
	}
	if gotKey != key || strings.Contains(gotPath, key) || !strings.Contains(gotPath, "streamGenerateContent") {
		t.Errorf("key=%q url=%s", gotKey, gotPath)
	}
	if !strings.Contains(gotBody, "You review code for demo.") || !strings.Contains(gotBody, `"text":"hi"`) {
		t.Errorf("%s", gotBody)
	}

	// no key: nothing is sent and the error says what to set
	t.Setenv("GEMINI_API_KEY", "")
	err = executeRun(runParams{Name: "Reviewer", Cwd: dir, Input: "hi", MaxTurns: 10,
		Weave:  tui.WeaveOptions{Variables: map[string]string{"repo": "demo"}},
		Stdout: &out, Stderr: &errw, Stdin: strings.NewReader("")})
	if err == nil || !strings.Contains(err.Error(), "$GEMINI_API_KEY") {
		t.Errorf("%v", err)
	}
	// a LoomLocker token in place of the key
	t.Setenv("GEMINI_API_KEY", "lk_7f3a9b2c1d4e5a6b")
	err = executeRun(runParams{Name: "Reviewer", Cwd: dir, Input: "hi", MaxTurns: 10,
		Weave:  tui.WeaveOptions{Variables: map[string]string{"repo": "demo"}},
		Stdout: &out, Stderr: &errw, Stdin: strings.NewReader("")})
	if err == nil || !strings.Contains(err.Error(), "LoomLocker token") {
		t.Errorf("%v", err)
	}
}
