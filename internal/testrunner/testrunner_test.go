package testrunner

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/parser"
	"github.com/sayandeep14/PromptLoom/internal/registry"
)

const key = "AIzaSy-SUPER-SECRET-KEY-123"

type fakeAPI struct {
	srv    *httptest.Server
	mu     sync.Mutex
	last   *http.Request
	body   string
	handle func(w http.ResponseWriter, r *http.Request)
}

func newAPI(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) *fakeAPI {
	t.Helper()
	f := &fakeAPI{handle: handle}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.last, f.body = r, string(b)
		f.mu.Unlock()
		handle(w, r)
	}))
	t.Cleanup(f.srv.Close)
	oldG, oldA := geminiBaseURL, anthropicURL
	geminiBaseURL, anthropicURL = f.srv.URL+"/v1beta/models", f.srv.URL+"/v1/messages"
	t.Cleanup(func() { geminiBaseURL, anthropicURL = oldG, oldA })
	return f
}

func geminiOK(text string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{map[string]any{"text": text}}}}}})
	}
}

func reg(t *testing.T, srcs ...string) *registry.Registry {
	t.Helper()
	r := registry.New()
	for i, s := range srcs {
		nodes, err := parser.Parse("f"+string(rune('0'+i))+".loom", s)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Register(nodes); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func cfg(provider string) *config.Config {
	c := config.Defaults()
	c.Testing.Provider = provider
	c.Testing.APIKeyEnv = "LOOM_TEST_KEY"
	c.Testing.TimeoutSec = 5
	return c
}

const withContract = `
prompt Summary {
  persona :=
    You summarise.
  contract {
    required_sections:
      - Summary
    must_not_include:
      - "As an AI"
  }
}`

func setKey(t *testing.T, v string) {
	t.Helper()
	old, had := os.LookupEnv("LOOM_TEST_KEY")
	os.Setenv("LOOM_TEST_KEY", v)
	t.Cleanup(func() {
		if had {
			os.Setenv("LOOM_TEST_KEY", old)
		} else {
			os.Unsetenv("LOOM_TEST_KEY")
		}
	})
}

func TestPassAndFailAgainstTheContract(t *testing.T) {
	setKey(t, key)
	dir := t.TempDir()
	r := reg(t, withContract)

	newAPI(t, geminiOK("## Summary\nAll good."))
	if res := Run("Summary", r, cfg("gemini"), dir, Options{}); res.Err != nil || !res.Passed || len(res.Failures) != 0 {
		t.Errorf("a conforming reply should pass: %+v", res)
	}

	newAPI(t, geminiOK("As an AI, here is text without headings"))
	res := Run("Summary", r, cfg("gemini"), dir, Options{})
	if res.Err != nil || res.Passed || len(res.Failures) != 2 {
		t.Errorf("expected 2 contract failures: %+v", res)
	}
}

func TestSkippedWithoutAContractAndUnknownPrompt(t *testing.T) {
	r := reg(t, "prompt NoContract {\n  persona :=\n    x\n}")
	res := Run("NoContract", r, cfg("gemini"), t.TempDir(), Options{})
	if !res.Skipped || res.SkipReason == "" || res.Err != nil {
		t.Errorf("%+v", res)
	}
	if res := Run("Ghost", r, cfg("gemini"), t.TempDir(), Options{}); res.Err == nil {
		t.Error("an unknown prompt is an error")
	}
}

func TestMissingKeyIsAClearErrorAndNothingIsSent(t *testing.T) {
	os.Unsetenv("LOOM_TEST_KEY")
	api := newAPI(t, geminiOK("x"))
	res := Run("Summary", reg(t, withContract), cfg("gemini"), t.TempDir(), Options{})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "LOOM_TEST_KEY") {
		t.Errorf("the error should name the variable: %v", res.Err)
	}
	if api.last != nil {
		t.Error("no request may be sent without a key")
	}
}

func TestDefaultKeyVariablePerProvider(t *testing.T) {
	c := config.Defaults()
	for provider, want := range map[string]string{"gemini": "GEMINI_API_KEY", "anthropic": "ANTHROPIC_API_KEY", "": "GEMINI_API_KEY"} {
		c.Testing.Provider, c.Testing.APIKeyEnv = provider, ""
		os.Unsetenv(want)
		if _, err := resolveAPIKey(c); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("provider %q: %v", provider, err)
		}
	}
}

// The Gemini key used to be in the URL query, and Go's HTTP errors print the URL.
func TestAPIKeyIsNeverInTheURLOrInErrors(t *testing.T) {
	setKey(t, key)
	api := newAPI(t, geminiOK("## Summary\nok"))
	if res := Run("Summary", reg(t, withContract), cfg("gemini"), t.TempDir(), Options{}); res.Err != nil {
		t.Fatal(res.Err)
	}
	if strings.Contains(api.last.URL.String(), key) || api.last.URL.RawQuery != "" {
		t.Errorf("the key must not be in the URL: %s", api.last.URL)
	}
	if api.last.Header.Get("x-goog-api-key") != key {
		t.Error("the key belongs in the x-goog-api-key header")
	}

	// a network failure: the connection is refused. The message must not contain the key.
	geminiBaseURL = "http://127.0.0.1:1/v1beta/models"
	anthropicURL = "http://127.0.0.1:1/v1/messages"
	for _, provider := range []string{"gemini", "anthropic"} {
		res := Run("Summary", reg(t, withContract), cfg(provider), t.TempDir(), Options{})
		if res.Err == nil {
			t.Fatalf("%s: expected a connection error", provider)
		}
		if strings.Contains(res.Err.Error(), key) {
			t.Errorf("%s: the API key leaked into the error: %v", provider, res.Err)
		}
	}
}

func TestErrorResponsesAreReadable(t *testing.T) {
	setKey(t, key)
	r := reg(t, withContract)
	cases := map[string]struct {
		handler func(http.ResponseWriter, *http.Request)
		want    string
	}{
		"api error json": {func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"code":400,"message":"API key not valid"}}`))
		}, "API key not valid"},
		"html error page": {func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(502)
			w.Write([]byte("<html><body>Bad Gateway</body></html>"))
		}, "HTTP 502"},
		"no candidates": {func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"candidates":[]}`)) }, "no content"},
		"empty body":    {func(w http.ResponseWriter, _ *http.Request) {}, "(empty body)"},
	}
	for name, c := range cases {
		newAPI(t, c.handler)
		res := Run("Summary", r, cfg("gemini"), t.TempDir(), Options{})
		if res.Err == nil || !strings.Contains(res.Err.Error(), c.want) {
			t.Errorf("%s: want an error containing %q, got %v", name, c.want, res.Err)
		}
	}
}

func TestAnthropicRequestAndResponse(t *testing.T) {
	setKey(t, "sk-ant-secret")
	api := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"content": []any{
			map[string]any{"type": "thinking", "text": "ignored"},
			map[string]any{"type": "text", "text": "## Summary\nfrom claude"},
		}})
	})
	res := Run("Summary", reg(t, withContract), cfg("anthropic"), t.TempDir(), Options{Model: "claude-x"})
	if res.Err != nil || !res.Passed || res.Response != "## Summary\nfrom claude" {
		t.Fatalf("%+v", res)
	}
	h := api.last.Header
	if h.Get("x-api-key") != "sk-ant-secret" || h.Get("anthropic-version") == "" {
		t.Errorf("headers: %v", h)
	}
	var sent struct {
		Model  string `json:"model"`
		System string `json:"system"`
	}
	json.Unmarshal([]byte(api.body), &sent)
	if sent.Model != "claude-x" || !strings.Contains(sent.System, "You summarise.") {
		t.Errorf("request body: %s", api.body)
	}

	newAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"error":{"type":"overloaded","message":"busy"}}`))
	})
	if res := Run("Summary", reg(t, withContract), cfg("anthropic"), t.TempDir(), Options{}); res.Err == nil || !strings.Contains(res.Err.Error(), "busy") {
		t.Errorf("api error: %v", res.Err)
	}
	newAPI(t, func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"content":[{"type":"tool_use"}]}`)) })
	if res := Run("Summary", reg(t, withContract), cfg("anthropic"), t.TempDir(), Options{}); res.Err == nil {
		t.Error("a reply without a text block is an error")
	}
}

func TestUnknownProviderIsAnErrorNotGemini(t *testing.T) {
	setKey(t, key)
	api := newAPI(t, geminiOK("x"))
	res := Run("Summary", reg(t, withContract), cfg("openai"), t.TempDir(), Options{})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "openai") || !strings.Contains(res.Err.Error(), "gemini, anthropic") {
		t.Errorf("got %v", res.Err)
	}
	if api.last != nil {
		t.Error("the prompt (and key) must not be sent to a provider the user did not choose")
	}
}

func TestModelNamesAreValidated(t *testing.T) {
	setKey(t, key)
	api := newAPI(t, geminiOK("## Summary"))
	for _, bad := range []string{"../../admin", "a b", "model?x=1", "m/../n", "x#frag", "-flag", strings.Repeat("m", 101)} {
		res := Run("Summary", reg(t, withContract), cfg("gemini"), t.TempDir(), Options{Model: bad})
		if res.Err == nil || !strings.Contains(res.Err.Error(), "invalid model") {
			t.Errorf("model %q must be refused: %v", bad, res.Err)
		}
	}
	if api.last != nil {
		t.Error("nothing may be sent for an invalid model name")
	}
	for _, ok := range []string{"gemini-2.5-flash", "claude-sonnet-5", "gpt.4o", "model:v1"} {
		if res := Run("Summary", reg(t, withContract), cfg("gemini"), t.TempDir(), Options{Model: ok}); res.Err != nil {
			t.Errorf("model %q should be accepted: %v", ok, res.Err)
		}
	}
}

func TestModelSelectionOrder(t *testing.T) {
	setKey(t, key)
	api := newAPI(t, geminiOK("## Summary"))
	c := cfg("gemini")
	c.Testing.DefaultModel = ""
	Run("Summary", reg(t, withContract), c, t.TempDir(), Options{})
	if !strings.Contains(api.last.URL.Path, "gemini-2.0-flash:generateContent") {
		t.Errorf("built-in default: %s", api.last.URL.Path)
	}
	c.Testing.DefaultModel = "from-config"
	Run("Summary", reg(t, withContract), c, t.TempDir(), Options{})
	if !strings.Contains(api.last.URL.Path, "from-config:") {
		t.Errorf("config default: %s", api.last.URL.Path)
	}
	Run("Summary", reg(t, withContract), c, t.TempDir(), Options{Model: "from-flag"})
	if !strings.Contains(api.last.URL.Path, "from-flag:") {
		t.Errorf("--model wins: %s", api.last.URL.Path)
	}
}

func TestTimeout(t *testing.T) {
	setKey(t, key)
	newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
		geminiOK("x")(w, r)
	})
	c := cfg("gemini")
	c.Testing.TimeoutSec = 1
	start := time.Now()
	res := Run("Summary", reg(t, withContract), c, t.TempDir(), Options{})
	if res.Err == nil || time.Since(start) > 2500*time.Millisecond {
		t.Errorf("a slow model must time out after ~1s: err=%v took %v", res.Err, time.Since(start))
	}
}

func TestFixtureInputIsSentInsteadOfTheStub(t *testing.T) {
	setKey(t, key)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "tests"), 0o755)
	os.WriteFile(filepath.Join(dir, "tests", "Summary.input.md"), []byte("MY OWN INPUT"), 0o644)
	api := newAPI(t, geminiOK("## Summary"))
	Run("Summary", reg(t, withContract), cfg("gemini"), dir, Options{})
	if !strings.Contains(api.body, "MY OWN INPUT") {
		t.Errorf("fixture not used: %s", api.body)
	}

	api2 := newAPI(t, geminiOK("## Summary"))
	Run("Summary", reg(t, withContract), cfg("gemini"), t.TempDir(), Options{})
	if !strings.Contains(api2.body, "SELECT * FROM users") {
		t.Errorf("the built-in stub should be sent when there is no fixture: %s", api2.body)
	}
}

func TestRecordAndCompare(t *testing.T) {
	setKey(t, key)
	dir := t.TempDir()
	r := reg(t, withContract)
	newAPI(t, geminiOK("unused")) // never let a test reach a real endpoint

	if res := Run("Summary", r, cfg("gemini"), dir, Options{Compare: true}); res.Err == nil || !strings.Contains(res.Err.Error(), "--record") {
		t.Errorf("comparing without a baseline should say to record one: %v", res.Err)
	}

	newAPI(t, geminiOK("As an AI: no headings")) // a baseline that already violates the contract
	if res := Run("Summary", r, cfg("gemini"), dir, Options{Record: true}); res.Err != nil {
		t.Fatal(res.Err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "tests", "Summary.baseline.md"))
	if err != nil || string(b) != "As an AI: no headings" {
		t.Fatalf("baseline: %q %v", b, err)
	}

	// same failures as the baseline => not a regression
	if res := Run("Summary", r, cfg("gemini"), dir, Options{Compare: true}); res.Err != nil || !res.Passed {
		t.Errorf("identical behaviour must pass: %+v", res)
	}
	// a NEW failure relative to the baseline => regression
	newAPI(t, geminiOK("## Summary\nAs an AI I cannot"))
	baselineGood := filepath.Join(dir, "tests", "Summary.baseline.md")
	os.WriteFile(baselineGood, []byte("## Summary\nfine"), 0o644)
	res := Run("Summary", r, cfg("gemini"), dir, Options{Compare: true})
	if res.Passed || len(res.Failures) != 1 || res.Failures[0].Kind != "forbidden-content" {
		t.Errorf("a new violation is a regression: %+v", res)
	}
}

func TestBaselineForNamespacedPromptNames(t *testing.T) {
	dir := t.TempDir()
	if err := writeBaseline(filepath.Join(dir, "tests"), "team/Reviewer", "x"); err != nil {
		t.Errorf("a namespaced prompt name must not break --record: %v", err)
	}
}

func TestRunAllIsSortedAndCoversEveryPrompt(t *testing.T) {
	setKey(t, key)
	newAPI(t, geminiOK("## Summary"))
	src := ""
	for _, n := range []string{"Zeta", "Alpha", "Mid", "Beta"} {
		src += "prompt " + n + " {\n  persona :=\n    x\n  contract {\n    required_sections:\n      - Summary\n  }\n}\n"
	}
	results := RunAll(reg(t, src), cfg("gemini"), t.TempDir(), Options{})
	var names []string
	for _, r := range results {
		names = append(names, r.PromptName)
	}
	if strings.Join(names, ",") != "Alpha,Beta,Mid,Zeta" {
		t.Errorf("results must be in a stable, sorted order: %v", names)
	}
}
