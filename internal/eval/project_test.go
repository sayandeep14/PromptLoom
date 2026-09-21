package eval

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/llm"
)

const projToml = `[project]
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

func projectDir(t *testing.T, suites map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "loom.toml"), []byte(projToml), 0o644)
	os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
	os.WriteFile(filepath.Join(dir, "prompts", "Reviewer.prompt.loom"), []byte(promptSrc), 0o644)
	for name, body := range suites {
		os.MkdirAll(filepath.Join(dir, "evals"), 0o755)
		os.WriteFile(filepath.Join(dir, "evals", name), []byte(body), 0o644)
	}
	return dir
}

// router hands out fake clients and remembers who asked for what. Every client answers as the
// judge when it is sent the judge instructions and as a model otherwise, so tests need not care
// which one the code asked for.
type router struct {
	asked  [][2]string
	models map[string]*fakeModel
	judge  *fakeJudge
	failOn string
}

type either struct {
	model *fakeModel
	judge *fakeJudge
}

func (e either) Complete(ctx context.Context, r llm.Request) (string, error) {
	if r.System == judgeSystem {
		return e.judge.Complete(ctx, r)
	}
	return e.model.Complete(ctx, r)
}

func (r *router) clientFor(_ *config.Config, provider, model string) (Completer, string, error) {
	r.asked = append(r.asked, [2]string{provider, model})
	if r.failOn != "" && model == r.failOn {
		return nil, "", errors.New("API key not set: $X is empty")
	}
	m, ok := r.models[model]
	if !ok {
		m = &fakeModel{}
		r.models[model] = m
	}
	return either{m, r.judge}, orDefault(model, "default-model"), nil
}

func orDefault(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func newRouter() *router {
	return &router{models: map[string]*fakeModel{}, judge: &fakeJudge{scores: all(90)}}
}

func TestRunProjectUsesTheDefaultModelAndJudge(t *testing.T) {
	dir := projectDir(t, map[string]string{"Reviewer.eval.toml": goodSuite})
	r := newRouter()
	out, err := RunProject(context.Background(), dir, Params{ClientFor: r.clientFor})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK() || out.Summary.Total != 2 || out.Summary.Passed != 2 || len(out.Suites) != 1 {
		t.Fatalf("%+v", out.Summary)
	}
	text := out.Text()
	for _, want := range []string{"Reviewer  (prompt Reviewer)", "✓ flags injection", "2 case(s): 2 passed, 0 failed, 0 errored"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
}

func TestModelsAndJudgeSelection(t *testing.T) {
	suite := strings.Replace(goodSuite, `threshold = 70`, "threshold = 70\njudge_model = \"suite-judge\"", 1)
	dir := projectDir(t, map[string]string{"Reviewer.eval.toml": suite})

	// --models are parsed as model or provider:model, and labelled that way
	r := newRouter()
	out, err := RunProject(context.Background(), dir, Params{Models: []string{"m1", "openai:gpt-x"}, ClientFor: r.clientFor})
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, res := range out.Suites[0].Results {
		labels = append(labels, res.Model)
	}
	if !contains(labels, "m1") || !contains(labels, "openai:gpt-x") || len(labels) != 4 {
		t.Errorf("%v", labels)
	}
	if !contains2(r.asked, [2]string{"openai", "gpt-x"}) {
		t.Errorf("the provider prefix must reach the client factory: %v", r.asked)
	}
	// the judge comes from the suite's judge_model...
	if !contains2(r.asked, [2]string{"", "suite-judge"}) {
		t.Errorf("suite judge: %v", r.asked)
	}
	// ...unless --judge says otherwise
	r = newRouter()
	if _, err := RunProject(context.Background(), dir, Params{Judge: "judge-model", ClientFor: r.clientFor}); err != nil {
		t.Fatal(err)
	}
	if contains2(r.asked, [2]string{"", "suite-judge"}) || !contains2(r.asked, [2]string{"", "judge-model"}) {
		t.Errorf("--judge must win over the suite: %v", r.asked)
	}
	// a client that cannot be built (no key) stops the run with the reason
	r = newRouter()
	r.failOn = "m1"
	if _, err := RunProject(context.Background(), dir, Params{Models: []string{"m1"}, ClientFor: r.clientFor}); err == nil || !strings.Contains(err.Error(), `model "m1"`) || !strings.Contains(err.Error(), "API key not set") {
		t.Errorf("%v", err)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
func contains2(l [][2]string, p [2]string) bool {
	for _, x := range l {
		if x == p {
			return true
		}
	}
	return false
}

func TestSuiteSelection(t *testing.T) {
	other := strings.Replace(goodSuite, `prompt = "Reviewer"`, `prompt = "Reviewer"`, 1)
	dir := projectDir(t, map[string]string{"Reviewer.eval.toml": goodSuite, "Second.eval.toml": other})
	r := newRouter()

	out, err := RunProject(context.Background(), dir, Params{ClientFor: r.clientFor})
	if err != nil || len(out.Suites) != 2 {
		t.Fatalf("%v %v", out, err)
	}
	// by suite name
	if out, err := RunProject(context.Background(), dir, Params{Names: []string{"Second"}, ClientFor: r.clientFor}); err != nil || len(out.Suites) != 1 || out.Suites[0].Suite.Name != "Second" {
		t.Errorf("%v %v", out, err)
	}
	// by prompt name: every suite that evaluates it
	if out, err := RunProject(context.Background(), dir, Params{Names: []string{"Reviewer"}, ClientFor: r.clientFor}); err != nil || len(out.Suites) != 2 {
		t.Errorf("%v %v", out, err)
	}
	if _, err := RunProject(context.Background(), dir, Params{Names: []string{"Nope"}, ClientFor: r.clientFor}); err == nil || !strings.Contains(err.Error(), "suites: Reviewer, Second") {
		t.Errorf("an unknown name lists the suites: %v", err)
	}
}

func TestNoSuitesAndBrokenSuites(t *testing.T) {
	dir := projectDir(t, nil)
	if _, err := RunProject(context.Background(), dir, Params{ClientFor: newRouter().clientFor}); err == nil || !strings.Contains(err.Error(), "no eval suites found") {
		t.Errorf("%v", err)
	}
	dir = projectDir(t, map[string]string{"Bad.eval.toml": "prompt = \"P\"\n"})
	if _, err := RunProject(context.Background(), dir, Params{ClientFor: newRouter().clientFor}); err == nil || !strings.Contains(err.Error(), "Bad.eval.toml") {
		t.Errorf("%v", err)
	}
}

func TestRecordThenCompareFlow(t *testing.T) {
	// no per-case min_score here: the pass mark is 70 for both cases, so 75 still passes
	dir := projectDir(t, map[string]string{"Reviewer.eval.toml": strings.Replace(goodSuite, "min_score = 90\n", "", 1)})
	ctx := context.Background()

	// --compare before anything was recorded: not a failure, but it says so
	r := newRouter()
	out, err := RunProject(ctx, dir, Params{Compare: true, ClientFor: r.clientFor})
	if err != nil || !out.OK() || !out.Suites[0].NoBaseline || !strings.Contains(out.Text(), "no baseline yet") {
		t.Fatalf("%v %v\n%s", out, err, out.Text())
	}

	// record
	out, err = RunProject(ctx, dir, Params{Record: true, ClientFor: r.clientFor})
	if err != nil || out.Suites[0].Recorded != 2 || !strings.Contains(out.Text(), "recorded 2 score(s) to evals/.baseline/Reviewer.json") {
		t.Fatalf("%v %v", out, err)
	}

	// same quality: OK. A drop within the tolerance: OK.
	same := newRouter()
	same.judge = &fakeJudge{scores: all(87)}
	if out, err := RunProject(ctx, dir, Params{Compare: true, ClientFor: same.clientFor}); err != nil || !out.OK() {
		t.Errorf("%v %v", out, err)
	}
	// a real drop (still above the pass mark) is a regression
	worse := newRouter()
	worse.judge = &fakeJudge{scores: all(75)}
	out, err = RunProject(ctx, dir, Params{Compare: true, ClientFor: worse.clientFor})
	if err != nil || out.OK() || out.Regressions != 2 || !out.Summary.OK() {
		t.Fatalf("regressions=%d summary=%+v err=%v", out.Regressions, out.Summary, err)
	}
	if !strings.Contains(out.Text(), "2 regressed against the baseline") || !strings.Contains(out.Text(), "▼ was 90") {
		t.Errorf("%s", out.Text())
	}
	// a wide tolerance forgives it; --strict makes any drop count
	if out, _ := RunProject(ctx, dir, Params{Compare: true, Tolerance: 20, ClientFor: worse.clientFor}); !out.OK() {
		t.Error("tolerance 20")
	}
	slight := newRouter()
	slight.judge = &fakeJudge{scores: all(89)}
	if out, _ := RunProject(ctx, dir, Params{Compare: true, Strict: true, ClientFor: slight.clientFor}); out.OK() {
		t.Error("strict: a 1-point drop counts")
	}
}

// A baseline with holes would hide the cases that could not run.
func TestRecordRefusesWhenCasesErrored(t *testing.T) {
	dir := projectDir(t, map[string]string{"Reviewer.eval.toml": goodSuite})
	r := newRouter()
	r.judge = &fakeJudge{raw: "not json"}
	if _, err := RunProject(context.Background(), dir, Params{Record: true, ClientFor: r.clientFor}); err == nil || !strings.Contains(err.Error(), "not recording") {
		t.Errorf("%v", err)
	}
	if _, err := os.Stat(BaselinePath(filepath.Join(dir, "evals"), "Reviewer")); err == nil {
		t.Error("nothing may be written")
	}
}

func TestFailingRunIsNotOK(t *testing.T) {
	dir := projectDir(t, map[string]string{"Reviewer.eval.toml": goodSuite})
	r := newRouter()
	r.judge = &fakeJudge{scores: all(10)}
	out, err := RunProject(context.Background(), dir, Params{ClientFor: r.clientFor})
	if err != nil || out.OK() || out.Summary.Failed != 2 {
		t.Errorf("%+v %v", out.Summary, err)
	}
	// --threshold lowers the bar for the whole run
	if out, _ := RunProject(context.Background(), dir, Params{Threshold: 5, ClientFor: r.clientFor}); !out.OK() {
		t.Error("threshold override")
	}
}

// The whole path with the real client: prompt rendered, sent to a (fake) Gemini endpoint, the
// answer judged by a second call, the key only ever in a header.
func TestRunProjectOverTheRealClient(t *testing.T) {
	const key = "AIza-EVAL-SECRET-KEY"
	var modelCalls, judgeCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("x-goog-api-key") != key || r.URL.RawQuery != "" {
			t.Errorf("the key must be in the header only: %s %v", r.URL, r.Header)
		}
		reply := "verdict: the query concatenates user input"
		if strings.Contains(string(body), "strict, impartial grader") {
			judgeCalls++
			reply = `{"criteria":[{"score":95,"note":"found it"},{"score":85,"note":"suggested ?"}]}`
			if strings.Contains(string(body), "clean code") {
				reply = `{"criteria":[{"score":92,"note":"ok"}]}`
			}
		} else {
			modelCalls++
		}
		json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{map[string]any{"text": reply}}}}}})
	}))
	defer srv.Close()
	old := llm.GeminiBaseURL
	llm.GeminiBaseURL = srv.URL + "/v1beta"
	defer func() { llm.GeminiBaseURL = old }()
	t.Setenv("GEMINI_API_KEY", key)

	dir := projectDir(t, map[string]string{"Reviewer.eval.toml": goodSuite})
	out, err := RunProject(context.Background(), dir, Params{})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK() || modelCalls != 2 || judgeCalls != 2 || out.Suites[0].Results[0].Score != 90 {
		t.Errorf("model calls %d, judge calls %d\n%s", modelCalls, judgeCalls, out.Text())
	}

	// without a key nothing is called and the error says what to set
	t.Setenv("GEMINI_API_KEY", "")
	modelCalls = 0
	if _, err := RunProject(context.Background(), dir, Params{}); err == nil || !strings.Contains(err.Error(), "$GEMINI_API_KEY") || modelCalls != 0 {
		t.Errorf("%v (%d calls)", err, modelCalls)
	}
}
