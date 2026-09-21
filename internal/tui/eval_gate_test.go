package tui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/eval"
	"github.com/sayandeep14/PromptLoom/internal/llm"
)

func evalProject(t *testing.T) string {
	dir := syncProject(t, "prompt Assistant {\n  persona :=\n    You help.\n  contract {\n    must_include:\n      - ok\n  }\n}\n")
	// syncProject configures deploy targets; drop them so the gate under test is the only new one
	writeFile(t, filepath.Join(dir, "loom.toml"), strings.SplitN(syncToml, "[[targets]]", 2)[0])
	writeFile(t, filepath.Join(dir, "evals", "Assistant.eval.toml"),
		"prompt = \"Assistant\"\n[[case]]\nname = \"helps\"\ninput = \"hi\"\ncriteria = [\"is helpful\"]\n")
	return dir
}

// fakeModelAPI answers the model with "ok" and grades every criterion with `score`.
func fakeModelAPI(t *testing.T, score *int) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reply := "ok"
		if strings.Contains(string(body), "strict, impartial grader") {
			b, _ := json.Marshal(map[string]any{"criteria": []map[string]any{{"score": *score, "note": "n"}}})
			reply = string(b)
		}
		json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{map[string]any{"text": reply}}}}}})
	}))
	old := llm.GeminiBaseURL
	llm.GeminiBaseURL = srv.URL + "/v1beta"
	t.Cleanup(func() { srv.Close(); llm.GeminiBaseURL = old })
}

func TestCIRunsEvalSuitesWhenAKeyIsAvailable(t *testing.T) {
	dir := evalProject(t)

	// no key: the gate is skipped, not failed (like the test gate)
	t.Setenv("GEMINI_API_KEY", "")
	out, failed, _ := RunCI(dir)
	if !strings.Contains(out, "eval") || !strings.Contains(out, "$GEMINI_API_KEY not set") {
		t.Errorf("%s", out)
	}
	_ = failed

	score := 90
	fakeModelAPI(t, &score)
	t.Setenv("GEMINI_API_KEY", "k")
	out, _, _ = RunCI(dir)
	if !strings.Contains(out, "1/1 cases passed") {
		t.Errorf("a passing suite:\n%s", out)
	}

	// record a baseline, then make the answers worse (still above the pass mark): CI must fail
	if _, err := eval.RunProject(t.Context(), dir, eval.Params{Record: true}); err != nil {
		t.Fatal(err)
	}
	score = 75
	out, hasFail, _ := RunCI(dir)
	if !hasFail || !strings.Contains(out, "1 regressed") {
		t.Errorf("a regression against the baseline must fail the gate:\n%s", out)
	}

	// a suite whose cases fall below the pass mark fails too
	score = 30
	if out, hasFail, _ := RunCI(dir); !hasFail || !strings.Contains(out, "1 failed") {
		t.Errorf("%s", out)
	}
}

func TestCIHasNoEvalGateWithoutSuites(t *testing.T) {
	dir := syncProject(t, okPrompt)
	t.Setenv("GEMINI_API_KEY", "k")
	if out, _, _ := RunCI(dir); strings.Contains(out, "eval ") {
		t.Errorf("no suites, no gate:\n%s", out)
	}
}
