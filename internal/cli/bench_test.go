package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/llm"
	"github.com/sayandeep14/PromptLoom/internal/usage"
)

const benchCliToml = `[project]
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

[[pricing]]
provider = "gemini"
model = "gemini-2.5-flash"
input_per_million = 1
output_per_million = 2
`

const benchCliPrompt = `prompt Reviewer {
  persona :=
    You are a reviewer.
}
`

func benchCliProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "loom.toml"), []byte(benchCliToml), 0o644)
	os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
	os.WriteFile(filepath.Join(dir, "prompts", "r.prompt.loom"), []byte(benchCliPrompt), 0o644)
	return dir
}

func resetBenchFlags() {
	benchInput, benchInputFile, benchModels, benchRuns, benchJSON = "", "", "", 1, false
	benchSets = nil
}

func newBenchGeminiServer(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"an answer"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2}}`)
	}))
	old := llm.GeminiBaseURL
	llm.GeminiBaseURL = srv.URL
	t.Cleanup(func() { srv.Close(); llm.GeminiBaseURL = old })
}

func TestBenchCommand(t *testing.T) {
	dir := benchCliProject(t)
	t.Chdir(dir)
	t.Setenv("GEMINI_API_KEY", "k")
	resetBenchFlags()
	newBenchGeminiServer(t)

	benchInput = "review this"
	benchRuns = 2
	out, err := captureStdout(t, func() error { return runBench(benchCmd, []string{"Reviewer"}) })
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	for _, want := range []string{"gemini:gemini-2.5-flash", "2", "$0."} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}

	got, err := usage.ReadAll(usage.DefaultPath(dir))
	if err != nil || len(got) != 2 {
		t.Fatalf("usage ledger: %v %v", got, err)
	}
	if got[0].Command != "bench" || !got[0].HasCost {
		t.Errorf("%+v", got[0])
	}
}

func TestBenchCommandJSON(t *testing.T) {
	dir := benchCliProject(t)
	t.Chdir(dir)
	t.Setenv("GEMINI_API_KEY", "k")
	resetBenchFlags()
	newBenchGeminiServer(t)

	benchInput = "x"
	benchJSON = true
	out, err := captureStdout(t, func() error { return runBench(benchCmd, []string{"Reviewer"}) })
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var res struct {
		Prompt string `json:"prompt"`
		Models []struct {
			Label string `json:"model"`
			Calls []struct {
				Input  int `json:"input_tokens"`
				Output int `json:"output_tokens"`
			} `json:"calls"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.Prompt != "Reviewer" || len(res.Models) != 1 || res.Models[0].Calls[0].Input != 10 {
		t.Fatalf("%+v", res)
	}
}

func TestBenchCommandBothInputFlagsIsAnError(t *testing.T) {
	dir := benchCliProject(t)
	t.Chdir(dir)
	resetBenchFlags()
	benchInput, benchInputFile = "a", "b"
	if err := runBench(benchCmd, []string{"Reviewer"}); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("%v", err)
	}
}
