package optimize

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/agent"
	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/eval"
	"github.com/sayandeep14/PromptLoom/internal/llm"
)

const optToml = `[project]
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

const optPrompt = `prompt Reviewer {
  persona :=
    You are a reviewer.

  instructions :=
    - Be brief.

  contract {
    must_include:
      - verdict
  }
}
`

const optSuite = `
prompt = "Reviewer"
threshold = 70

[[case]]
name = "explains reasoning"
input = "review this"
criteria = ["explains the reasoning"]
`

func optProject(t *testing.T, promptSrc string) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "loom.toml"), []byte(optToml), 0o644)
	os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
	os.WriteFile(filepath.Join(dir, "prompts", "r.prompt.loom"), []byte(promptSrc), 0o644)
	os.MkdirAll(filepath.Join(dir, "evals"), 0o755)
	os.WriteFile(filepath.Join(dir, "evals", "Reviewer.eval.toml"), []byte(optSuite), 0o644)
	return dir
}

// fakeModel answers the prompt under test; reply is a function of the call count (1-based), so a
// test can make later calls (after a rewrite) answer differently.
type fakeModel struct {
	reply func(n int) string
	seen  []llm.Request
}

func (f *fakeModel) Complete(_ context.Context, r llm.Request) (string, error) {
	f.seen = append(f.seen, r)
	return f.reply(len(f.seen)), nil
}

// fakeJudge scores an answer by a function of its text (e.g. "contains 'because' -> 90, else 40").
type fakeJudge struct{ score func(answer string) int }

func (f *fakeJudge) Complete(_ context.Context, r llm.Request) (string, error) {
	ans := between(r.User, "<answer>\n", "\n</answer>")
	b, _ := json.Marshal(map[string]any{"criteria": []map[string]any{{"score": f.score(ans), "note": "n"}}})
	return string(b), nil
}

// fakeRefiner returns replies[i] on its i-th call, substituting the prompt's persona for one that
// (in these tests) makes fakeJudge score higher.
type fakeRefiner struct {
	replies []string
	n       int
	seen    []llm.Request
}

func (f *fakeRefiner) Complete(_ context.Context, r llm.Request) (string, error) {
	f.seen = append(f.seen, r)
	reply := f.replies[min(f.n, len(f.replies)-1)]
	f.n++
	return reply, nil
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return s
	}
	s = s[i+len(a):]
	if j := strings.Index(s, b); j >= 0 {
		return s[:j]
	}
	return s
}

const (
	underTest = "model-under-test"
	judgeSpec = "the-judge"
)

// evalParams wires fake completers into eval.Params: `underTest` selects the model, `judgeSpec`
// selects the judge, so the two never get confused with each other or with a default ("").
func evalParams(model, judge eval.Completer) eval.Params {
	return eval.Params{
		Models: []string{underTest}, Judge: judgeSpec,
		ClientFor: func(_ *config.Config, _, m string) (eval.Completer, string, error) {
			if m == judgeSpec {
				return judge, "judge", nil
			}
			return model, "model", nil
		},
	}
}

func permAllowAll(dir string) *agent.Permission {
	p, err := agent.LoadPermission(dir)
	if err != nil {
		panic(err)
	}
	return p
}

// ---- Score ----

func TestComputeScore(t *testing.T) {
	dir := optProject(t, optPrompt)
	model := &fakeModel{reply: func(int) string { return "verdict: because reasons" }}
	judge := &fakeJudge{score: func(a string) int {
		if strings.Contains(a, "because") {
			return 90
		}
		return 30
	}}
	s, err := ComputeScore(context.Background(), dir, "Reviewer", evalParams(model, judge))
	if err != nil || s.Mean != 90 || s.Cases != 1 || s.Passed != 1 || !s.OK() {
		t.Fatalf("%+v %v", s, err)
	}
	if !strings.Contains(s.Text(), "PASS") {
		t.Errorf("%s", s.Text())
	}

	model2 := &fakeModel{reply: func(int) string { return "verdict: nope" }}
	s2, err := ComputeScore(context.Background(), dir, "Reviewer", evalParams(model2, judge))
	if err != nil || s2.OK() || s2.Mean != 30 {
		t.Fatalf("%+v %v", s2, err)
	}
	if !strings.Contains(s2.Text(), "FAIL") {
		t.Errorf("%s", s2.Text())
	}

	// no eval suite for the prompt
	noSuite := optProject(t, optPrompt)
	os.Remove(filepath.Join(noSuite, "evals", "Reviewer.eval.toml"))
	if _, err := ComputeScore(context.Background(), noSuite, "Reviewer", evalParams(model, judge)); err == nil {
		t.Error("no suite should be an error, not a silent zero score")
	}
}

// ---- Step ----

func TestStepAlreadyPassing(t *testing.T) {
	dir := optProject(t, optPrompt)
	model := &fakeModel{reply: func(int) string { return "verdict: because reasons" }}
	judge := &fakeJudge{score: func(string) int { return 95 }}
	step, err := Step(context.Background(), dir, "Reviewer", StepOptions{Eval: evalParams(model, judge), Refiner: &fakeRefiner{}})
	if err != nil || step.HasCandidate() || !strings.Contains(step.Message, "already passing") {
		t.Fatalf("%+v %v", step, err)
	}
}

const refinedPrompt = "prompt Reviewer {\n  persona :=\n    You are a reviewer who always explains their reasoning because it helps the reader.\n\n  instructions :=\n    - Be brief.\n\n  contract {\n    must_include:\n      - verdict\n  }\n}\n"

func TestStepProposesAndDiffsAChange(t *testing.T) {
	dir := optProject(t, optPrompt)
	model := &fakeModel{reply: func(int) string { return "verdict: nope, no reasons here" }}
	judge := &fakeJudge{score: func(a string) int {
		if strings.Contains(a, "because") {
			return 95
		}
		return 30
	}}
	refiner := &fakeRefiner{replies: []string{refinedPrompt}}
	step, err := Step(context.Background(), dir, "Reviewer", StepOptions{Eval: evalParams(model, judge), Refiner: refiner})
	if err != nil {
		t.Fatal(err)
	}
	if !step.HasCandidate() || step.Before.Mean != 30 {
		t.Fatalf("%+v", step)
	}
	if !strings.Contains(step.NewSrc, "always explains their reasoning") || !strings.Contains(step.Diff, "+") {
		t.Errorf("%s\n%s", step.NewSrc, step.Diff)
	}
	if !strings.Contains(refiner.seen[0].User, "explains the reasoning") || !strings.Contains(refiner.seen[0].User, "review this") {
		t.Errorf("%s", refiner.seen[0].User)
	}
	// Step never writes
	onDisk, _ := os.ReadFile(filepath.Join(dir, "prompts", "r.prompt.loom"))
	if string(onDisk) != optPrompt {
		t.Error("Step must not write to disk")
	}
	if step.Text() == "" || !strings.Contains(step.Text(), "prompts/r.prompt.loom") {
		t.Errorf("%s", step.Text())
	}
}

func TestStepReportsARejectedProposalWithoutFailing(t *testing.T) {
	dir := optProject(t, optPrompt)
	model := &fakeModel{reply: func(int) string { return "verdict: nope" }}
	judge := &fakeJudge{score: func(string) int { return 20 }}
	refiner := &fakeRefiner{replies: []string{"not a valid loom declaration at all"}}
	step, err := Step(context.Background(), dir, "Reviewer", StepOptions{Eval: evalParams(model, judge), Refiner: refiner})
	if err != nil || step.HasCandidate() || !strings.Contains(step.Message, "rejected") {
		t.Fatalf("%+v %v", step, err)
	}
}

// ---- Apply ----

func TestApplyChecksPermissionAndWritesAtomically(t *testing.T) {
	restrictedDir := optProject(t, optPrompt)
	os.WriteFile(filepath.Join(restrictedDir, ".loom.config"), []byte(`{"permission":{"read":["*"],"write":["nowhere/**"]}}`), 0o644)
	restricted, err := agent.LoadPermission(restrictedDir)
	if err != nil {
		t.Fatal(err)
	}
	restrictedFile := filepath.Join(restrictedDir, "prompts", "r.prompt.loom")
	if err := Apply(restricted, restrictedFile, "new content"); err == nil || !strings.Contains(err.Error(), "permission.write") {
		t.Errorf("%v", err)
	}
	if b, _ := os.ReadFile(restrictedFile); string(b) != optPrompt {
		t.Error("a refused write must not touch the file")
	}

	openDir := optProject(t, optPrompt)
	openFile := filepath.Join(openDir, "prompts", "r.prompt.loom")
	if err := Apply(permAllowAll(openDir), openFile, "prompt Reviewer {\n  persona :=\n    new\n}\n"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(openFile); !strings.Contains(string(b), "new") {
		t.Errorf("%s", b)
	}
	if left, _ := filepath.Glob(filepath.Join(openDir, "prompts", ".loomoptimize-*")); len(left) != 0 {
		t.Errorf("temp file left behind: %v", left)
	}
}

// ---- Loop ----

func TestLoopWithoutApplyIsAOneStepPreview(t *testing.T) {
	dir := optProject(t, optPrompt)
	model := &fakeModel{reply: func(int) string { return "verdict: nope" }}
	judge := &fakeJudge{score: func(a string) int {
		if strings.Contains(a, "because") {
			return 95
		}
		return 20
	}}
	refiner := &fakeRefiner{replies: []string{refinedPrompt}}
	res, err := Loop(context.Background(), dir, "Reviewer", LoopOptions{Eval: evalParams(model, judge), Refiner: refiner, Apply: false})
	if err != nil || len(res.Steps) != 1 || !strings.Contains(res.Reason, "preview") {
		t.Fatalf("%+v %v", res, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "prompts", "r.prompt.loom")); string(b) != optPrompt {
		t.Error("a preview must not write")
	}
	if res.Text() == "" {
		t.Error("Text")
	}
}

func TestLoopAppliesAndStopsOncePassing(t *testing.T) {
	dir := optProject(t, optPrompt)
	model := &fakeModel{reply: func(n int) string {
		if n == 1 {
			return "verdict: nope"
		}
		return "verdict: because reasons" // after the rewrite, the "model" answers better
	}}
	judge := &fakeJudge{score: func(a string) int {
		if strings.Contains(a, "because") {
			return 95
		}
		return 20
	}}
	refiner := &fakeRefiner{replies: []string{refinedPrompt}}
	res, err := Loop(context.Background(), dir, "Reviewer", LoopOptions{
		Eval: evalParams(model, judge), Refiner: refiner, Apply: true, Permission: permAllowAll(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 || res.Reason != "reached a passing score" || res.Final.Mean != 95 {
		t.Fatalf("%+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "prompts", "r.prompt.loom")); !strings.Contains(string(b), "always explains") {
		t.Errorf("the accepted change must be written: %s", b)
	}
}

func TestLoopRevertsARegression(t *testing.T) {
	dir := optProject(t, optPrompt)
	// before the rewrite the model scores 30 (not passing); the refiner's proposal makes it
	// WORSE (a bad proposal that still parses and applies fine) — the regression must be caught
	// and reverted, even though the score was never passing to begin with.
	model := &fakeModel{reply: func(n int) string {
		if n == 1 {
			return "verdict: nope"
		}
		return "verdict: much worse now"
	}}
	judge := &fakeJudge{score: func(a string) int {
		if strings.Contains(a, "worse") {
			return 10
		}
		return 30
	}}
	refiner := &fakeRefiner{replies: []string{refinedPrompt}}
	res, err := Loop(context.Background(), dir, "Reviewer", LoopOptions{
		Eval: evalParams(model, judge), Refiner: refiner, Apply: true, Permission: permAllowAll(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Reason, "reverted") || res.Final.Mean != 30 {
		t.Fatalf("%+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "prompts", "r.prompt.loom")); string(b) != optPrompt {
		t.Errorf("the file must be back to its original content:\n%s", b)
	}
}

func TestLoopStopsWhenTheRefinerHasNothingUsefulToPropose(t *testing.T) {
	dir := optProject(t, optPrompt)
	model := &fakeModel{reply: func(int) string { return "verdict: nope" }}
	judge := &fakeJudge{score: func(string) int { return 10 }}
	refiner := &fakeRefiner{replies: []string{"garbage, not DSL"}}
	res, err := Loop(context.Background(), dir, "Reviewer", LoopOptions{
		Eval: evalParams(model, judge), Refiner: refiner, Apply: true, Permission: permAllowAll(dir),
	})
	if err != nil || len(res.Steps) != 1 || !strings.Contains(res.Reason, "rejected") {
		t.Fatalf("%+v %v", res, err)
	}
}

// A rewrite that does not change the score (before == after, within ONE iteration) stops
// immediately, without needing a second iteration to notice.
func TestLoopStopsAtThePlateauWithinOneIteration(t *testing.T) {
	dir := optProject(t, optPrompt)
	model := &fakeModel{reply: func(int) string { return "verdict: same" }}
	judge := &fakeJudge{score: func(string) int { return 40 }}
	refiner := &fakeRefiner{replies: []string{refinedPrompt}}
	res, err := Loop(context.Background(), dir, "Reviewer", LoopOptions{
		Eval: evalParams(model, judge), Refiner: refiner, Apply: true, Permission: permAllowAll(dir), MaxIterations: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 || !strings.Contains(res.Reason, "stopped improving") {
		t.Fatalf("a same-score rewrite must stop after one iteration, not spin: %+v", res)
	}
}

// A score that keeps improving, but never enough to pass, runs until the iteration limit.
func TestLoopReachesTheIterationLimit(t *testing.T) {
	dir := optProject(t, optPrompt)
	scores := []int{30, 40, 40, 50, 50, 60} // (before,after) x 3 iterations, +10 each time, never 70
	model := &fakeModel{reply: func(n int) string { return fmt.Sprintf("verdict: step %d", n) }}
	judge := &fakeJudge{score: func(a string) int {
		i := strings.Index(a, "step ")
		var n int
		fmt.Sscanf(a[i+len("step "):], "%d", &n)
		return scores[n-1]
	}}
	refiner := &fakeRefiner{replies: []string{
		"prompt Reviewer {\n  persona :=\n    v1\n\n  instructions :=\n    - Be brief.\n\n  contract {\n    must_include:\n      - verdict\n  }\n}\n",
		"prompt Reviewer {\n  persona :=\n    v2\n\n  instructions :=\n    - Be brief.\n\n  contract {\n    must_include:\n      - verdict\n  }\n}\n",
		"prompt Reviewer {\n  persona :=\n    v3\n\n  instructions :=\n    - Be brief.\n\n  contract {\n    must_include:\n      - verdict\n  }\n}\n",
	}}
	res, err := Loop(context.Background(), dir, "Reviewer", LoopOptions{
		Eval: evalParams(model, judge), Refiner: refiner, Apply: true, Permission: permAllowAll(dir), MaxIterations: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 3 || res.Reason != "reached the iteration limit (3)" || res.Final.Mean != 60 {
		t.Fatalf("%+v", res)
	}
}

var _ = fmt.Sprintf
