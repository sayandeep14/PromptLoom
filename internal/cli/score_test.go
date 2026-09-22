package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/llm"
)

const scoreToml = `[project]
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

const scorePrompt = `prompt Reviewer {
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

const scoreSuite = `
prompt = "Reviewer"
threshold = 70

[[case]]
name = "explains reasoning"
input = "review this"
criteria = ["explains the reasoning"]
`

func scoreProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "loom.toml"), []byte(scoreToml), 0o644)
	os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
	os.WriteFile(filepath.Join(dir, "prompts", "r.prompt.loom"), []byte(scorePrompt), 0o644)
	os.MkdirAll(filepath.Join(dir, "evals"), 0o755)
	os.WriteFile(filepath.Join(dir, "evals", "Reviewer.eval.toml"), []byte(scoreSuite), 0o644)
	return dir
}

// gate scripts a fake Gemini endpoint: `modelReply` answers the prompt under test; `judgeScore`
// grades any answer that contains "because" as 90, everything else as 30; when `refined` is set,
// it is returned (in place of modelReply) once the SYSTEM prompt sent contains "REFINED-MARKER".
type gate struct {
	modelReply string
	refined    string
}

func newGateServer(t *testing.T, g *gate) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := scriptedReply(string(body), g)
		json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{map[string]any{"text": text}}}}}})
	}))
	old := llm.GeminiBaseURL
	llm.GeminiBaseURL = srv.URL + "/v1beta"
	t.Cleanup(func() { srv.Close(); llm.GeminiBaseURL = old })
}

func scriptedReply(body string, g *gate) string {
	if strings.Contains(body, "strict, impartial grader") {
		score := 30
		if strings.Contains(body, "because") {
			score = 90
		}
		b, _ := json.Marshal(map[string]any{"criteria": []map[string]any{{"score": score, "note": "n"}}})
		return string(b)
	}
	if strings.Contains(body, "You improve PromptLoom prompt declarations") {
		return g.refined
	}
	if g.refined != "" && strings.Contains(body, "REFINED-MARKER") {
		return "verdict: because reasons"
	}
	return g.modelReply
}

const refinedReviewer = "prompt Reviewer {\n  persona :=\n    REFINED-MARKER You are a reviewer.\n\n  instructions :=\n    - Be brief.\n\n  contract {\n    must_include:\n      - verdict\n  }\n}\n"

func resetOptimizeFlags() {
	scoreModels, scoreJudge, scoreDir, scoreFailUnder, scoreJSON = "", "", "", 0, false
	optimizeModels, optimizeJudge, optimizeRefiner, optimizeDir = "", "", "", ""
	optimizeIterations, optimizeTolerance, optimizeYes = 0, 0, false
	evalModels, evalJudge, evalDir, evalRefine, evalYes = "", "", "", false, false
	evalRecord, evalCompare, evalStrict = false, false, false
	evalTolerance, evalThreshold = 0, 0
}

func captureStdout(t *testing.T, f func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := f()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String(), err
}

func TestScoreCommand(t *testing.T) {
	dir := scoreProject(t)
	t.Chdir(dir)
	t.Setenv("GEMINI_API_KEY", "k")
	resetOptimizeFlags()

	newGateServer(t, &gate{modelReply: "verdict: because reasons"})
	out, err := captureStdout(t, func() error { return runScore(scoreCmd, []string{"Reviewer"}) })
	if err != nil || !strings.Contains(out, "score 90.0/100") || !strings.Contains(out, "PASS") {
		t.Fatalf("%v\n%s", err, out)
	}

	newGateServer(t, &gate{modelReply: "verdict: nope"})
	out, err = captureStdout(t, func() error { return runScore(scoreCmd, []string{"Reviewer"}) })
	if err == nil || !strings.Contains(out, "FAIL") {
		t.Fatalf("%v\n%s", err, out)
	}

	// --fail-under
	newGateServer(t, &gate{modelReply: "verdict: because reasons"})
	scoreFailUnder = 95
	if err := runScore(scoreCmd, []string{"Reviewer"}); err == nil || !strings.Contains(err.Error(), "fail-under") {
		t.Errorf("%v", err)
	}
	resetOptimizeFlags()

	// --json
	newGateServer(t, &gate{modelReply: "verdict: because reasons"})
	scoreJSON = true
	out, err = captureStdout(t, func() error { return runScore(scoreCmd, []string{"Reviewer"}) })
	var got struct {
		Prompt string
		Mean   float64
	}
	if jsonErr := json.Unmarshal([]byte(out), &got); err != nil || jsonErr != nil || got.Prompt != "Reviewer" || got.Mean != 90 {
		t.Errorf("%v %v %s", err, jsonErr, out)
	}
	resetOptimizeFlags()

	// no suite for the prompt
	os.Remove(filepath.Join(dir, "evals", "Reviewer.eval.toml"))
	if err := runScore(scoreCmd, []string{"Reviewer"}); err == nil {
		t.Error("no suite must be an error")
	}
}

func TestOptimizeCommandPreviewAndApply(t *testing.T) {
	dir := scoreProject(t)
	t.Chdir(dir)
	t.Setenv("GEMINI_API_KEY", "k")
	resetOptimizeFlags()

	// preview: nothing written
	newGateServer(t, &gate{modelReply: "verdict: nope", refined: refinedReviewer})
	out, err := captureStdout(t, func() error { return runOptimize(optimizeCmd, []string{"Reviewer"}) })
	if err == nil || !strings.Contains(out, "prompts/r.prompt.loom") || !strings.Contains(out, "preview only") {
		t.Fatalf("%v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "prompts", "r.prompt.loom")); string(b) != scorePrompt {
		t.Error("a preview must not write")
	}

	// --yes: applies, and (since the rewritten prompt now scores well) reaches a passing score
	optimizeYes = true
	out, err = captureStdout(t, func() error { return runOptimize(optimizeCmd, []string{"Reviewer"}) })
	if err != nil || !strings.Contains(out, "reached a passing score") {
		t.Fatalf("%v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "prompts", "r.prompt.loom")); !strings.Contains(string(b), "REFINED-MARKER") {
		t.Errorf("the accepted change must be written: %s", b)
	}
	resetOptimizeFlags()

	// permission.write blocks it
	dir2 := scoreProject(t)
	os.WriteFile(filepath.Join(dir2, ".loom.config"), []byte(`{"permission":{"read":["*"],"write":["nowhere/**"]}}`), 0o644)
	t.Chdir(dir2)
	newGateServer(t, &gate{modelReply: "verdict: nope", refined: refinedReviewer})
	optimizeYes = true
	_, err = captureStdout(t, func() error { return runOptimize(optimizeCmd, []string{"Reviewer"}) })
	if err == nil || !strings.Contains(err.Error(), "permission.write") {
		t.Errorf("%v", err)
	}
	resetOptimizeFlags()
}

func TestEvalRefineFlag(t *testing.T) {
	dir := scoreProject(t)
	t.Chdir(dir)
	t.Setenv("GEMINI_API_KEY", "k")
	resetOptimizeFlags()

	// preview: eval fails, refine shows a diff, exit code follows the ORIGINAL eval result
	newGateServer(t, &gate{modelReply: "verdict: nope", refined: refinedReviewer})
	evalRefine = true
	out, err := captureStdout(t, func() error { return runEval(evalCmd, nil) })
	if err == nil || !strings.Contains(out, "refine") || !strings.Contains(out, "prompts/r.prompt.loom") {
		t.Fatalf("%v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "prompts", "r.prompt.loom")); string(b) != scorePrompt {
		t.Error("a preview (no --yes) must not write")
	}
	resetOptimizeFlags()

	// --refine --yes: applies, then eval is re-run and its (now passing) result decides the exit code
	newGateServer(t, &gate{modelReply: "verdict: nope", refined: refinedReviewer})
	evalRefine, evalYes = true, true
	out, err = captureStdout(t, func() error { return runEval(evalCmd, nil) })
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "prompts", "r.prompt.loom")); !strings.Contains(string(b), "REFINED-MARKER") {
		t.Errorf("%s", b)
	}
	resetOptimizeFlags()

	// nothing failing: --refine has nothing to do and does not error
	newGateServer(t, &gate{modelReply: "verdict: because reasons"})
	evalRefine = true
	out, err = captureStdout(t, func() error { return runEval(evalCmd, nil) })
	if err != nil || strings.Contains(out, "prompts/r.prompt.loom") {
		t.Fatalf("%v\n%s", err, out)
	}
	resetOptimizeFlags()
}
