package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/config"
)

const secretKey = "SUPER-SECRET-KEY-123"

type fake struct {
	srv    *httptest.Server
	reqs   []*http.Request
	bodies []string
	status int
	reply  string
	delay  time.Duration
}

func newFake(t *testing.T, status int, reply string) *fake {
	t.Helper()
	f := &fake{status: status, reply: reply}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.reqs = append(f.reqs, r)
		f.bodies = append(f.bodies, string(b))
		if f.delay > 0 {
			time.Sleep(f.delay)
		}
		w.WriteHeader(f.status)
		io.WriteString(w, f.reply)
	}))
	g, a, o := GeminiBaseURL, AnthropicURL, OpenAIURL
	GeminiBaseURL, AnthropicURL, OpenAIURL = f.srv.URL+"/v1beta", f.srv.URL+"/v1/messages", f.srv.URL+"/v1/chat/completions"
	t.Cleanup(func() { f.srv.Close(); GeminiBaseURL, AnthropicURL, OpenAIURL = g, a, o })
	return f
}

const (
	geminiOK    = `{"candidates":[{"content":{"parts":[{"text":"pondering","thought":true},{"text":"hello "},{"text":"world"}]}}]}`
	anthropicOK = `{"content":[{"type":"thinking","text":"hmm"},{"type":"text","text":"hello "},{"type":"text","text":"world"}]}`
	openaiOK    = `{"choices":[{"message":{"content":"hello world"}}]}`
)

func client(provider string) *Client {
	return &Client{Provider: provider, Model: "some-model", APIKey: secretKey, Timeout: 5 * time.Second}
}

func TestEachProviderSendsItsRequestAndReturnsTheText(t *testing.T) {
	cases := []struct {
		provider, reply string
		check           func(t *testing.T, r *http.Request, body map[string]any)
	}{
		{Gemini, geminiOK, func(t *testing.T, r *http.Request, body map[string]any) {
			if r.Header.Get("x-goog-api-key") != secretKey || r.URL.RawQuery != "" || !strings.HasSuffix(r.URL.Path, "/models/some-model:generateContent") {
				t.Errorf("%s %v", r.URL, r.Header)
			}
			if body["systemInstruction"] == nil || body["generationConfig"].(map[string]any)["maxOutputTokens"].(float64) != 77 {
				t.Errorf("%v", body)
			}
		}},
		{Anthropic, anthropicOK, func(t *testing.T, r *http.Request, body map[string]any) {
			if r.Header.Get("x-api-key") != secretKey || r.Header.Get("anthropic-version") == "" {
				t.Errorf("%v", r.Header)
			}
			if body["system"] != "be brief" || body["model"] != "some-model" || body["max_tokens"].(float64) != 77 {
				t.Errorf("%v", body)
			}
		}},
		{OpenAI, openaiOK, func(t *testing.T, r *http.Request, body map[string]any) {
			if r.Header.Get("Authorization") != "Bearer "+secretKey {
				t.Errorf("%v", r.Header)
			}
			msgs := body["messages"].([]any)
			if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" || msgs[1].(map[string]any)["content"] != "the question" {
				t.Errorf("%v", body)
			}
		}},
	}
	for _, c := range cases {
		f := newFake(t, 200, c.reply)
		out, err := client(c.provider).Complete(context.Background(), Request{System: "be brief", User: "the question", MaxTokens: 77})
		if err != nil || out != "hello world" {
			t.Errorf("%s: %q %v", c.provider, out, err)
			continue
		}
		var body map[string]any
		json.Unmarshal([]byte(f.bodies[0]), &body)
		c.check(t, f.reqs[0], body)
		// no provider may put the key anywhere but its header
		if strings.Contains(f.reqs[0].URL.String(), secretKey) || strings.Contains(f.bodies[0], secretKey) {
			t.Errorf("%s leaked the key into the URL or body", c.provider)
		}
	}
}

func TestAnthropicHasADefaultTokenLimitAndSystemIsOptional(t *testing.T) {
	f := newFake(t, 200, anthropicOK)
	if _, err := client(Anthropic).Complete(context.Background(), Request{User: "q"}); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	json.Unmarshal([]byte(f.bodies[0]), &body)
	if body["max_tokens"].(float64) != 4096 || body["system"] != nil {
		t.Errorf("%v", body)
	}
	f2 := newFake(t, 200, openaiOK)
	client(OpenAI).Complete(context.Background(), Request{User: "q"})
	if strings.Contains(f2.bodies[0], `"system"`) {
		t.Errorf("no system message when there is no system prompt: %s", f2.bodies[0])
	}
}

func TestErrorsNeverContainTheKey(t *testing.T) {
	for _, p := range []string{Gemini, Anthropic, OpenAI} {
		g, a, o := GeminiBaseURL, AnthropicURL, OpenAIURL
		GeminiBaseURL, AnthropicURL, OpenAIURL = "http://127.0.0.1:1/v1beta", "http://127.0.0.1:1/m", "http://127.0.0.1:1/c"
		_, err := client(p).Complete(context.Background(), Request{User: "q"})
		GeminiBaseURL, AnthropicURL, OpenAIURL = g, a, o
		if err == nil || strings.Contains(err.Error(), secretKey) || !strings.Contains(err.Error(), "127.0.0.1:1") {
			t.Errorf("%s: %v", p, err)
		}
	}
	// a key echoed back by the SERVER is scrubbed from what we return
	c := client(Gemini)
	if got := scrub("echo "+secretKey, c.APIKey); strings.Contains(got, secretKey) || scrub("abc", "") != "abc" {
		t.Errorf("scrub: %q", got)
	}
}

func TestAPIErrorsAndUnreadableBodies(t *testing.T) {
	cases := []struct {
		provider, reply, want string
		status                int
	}{
		{Gemini, `{"error":{"code":429,"message":"quota exceeded"}}`, "quota exceeded", 429},
		{Gemini, `<html>bad gateway</html>`, "HTTP 502", 502},
		{Gemini, `{"candidates":[]}`, "no content", 200},
		{Gemini, `{}`, "HTTP 500 with no content", 500},
		{Anthropic, `{"error":{"type":"overloaded_error","message":"busy"}}`, "busy", 529},
		{Anthropic, ``, "(empty body)", 500},
		{Anthropic, `{"content":[]}`, "no content", 200},
		{Anthropic, `{"content":[{"type":"tool_use"}]}`, "no content", 200},
		{Anthropic, `{"content":[{"type":"thinking","text":"only thoughts"}]}`, "no content", 200},
		{OpenAI, `{"error":{"type":"invalid_request_error","message":"bad key"}}`, "bad key", 401},
		{OpenAI, `nope`, "HTTP 503", 503},
		{OpenAI, `{"choices":[]}`, "no content", 200},
	}
	for _, c := range cases {
		newFake(t, c.status, c.reply)
		_, err := client(c.provider).Complete(context.Background(), Request{User: "q"})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %q: %v (want %q)", c.provider, c.reply, err, c.want)
		}
	}
}

func TestCompleteReportsUsageThroughOnUsage(t *testing.T) {
	cases := []struct {
		provider, reply string
	}{
		{Gemini, `{"candidates":[{"content":{"parts":[{"text":"hi"}]}}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":4}}`},
		{Anthropic, `{"content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":11,"output_tokens":4}}`},
		{OpenAI, `{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":11,"completion_tokens":4}}`},
	}
	for _, c := range cases {
		newFake(t, 200, c.reply)
		cl := client(c.provider)
		var got Usage
		calls := 0
		cl.OnUsage = func(u Usage) { got, calls = u, calls+1 }
		if _, err := cl.Complete(context.Background(), Request{User: "q"}); err != nil {
			t.Fatalf("%s: %v", c.provider, err)
		}
		if calls != 1 || got.InputTokens != 11 || got.OutputTokens != 4 {
			t.Errorf("%s: OnUsage called %d time(s) with %+v", c.provider, calls, got)
		}
	}
}

func TestCompleteDoesNotReportUsageOnAnError(t *testing.T) {
	newFake(t, 500, `{"error":{"message":"boom"}}`)
	cl := client(Gemini)
	calls := 0
	cl.OnUsage = func(Usage) { calls++ }
	if _, err := cl.Complete(context.Background(), Request{User: "q"}); err == nil {
		t.Fatal("expected an error")
	}
	if calls != 0 {
		t.Errorf("OnUsage must not be called on a failed request, got %d call(s)", calls)
	}
}

func TestOversizedResponsesAreCut(t *testing.T) {
	newFake(t, 200, `{"candidates":[{"content":{"parts":[{"text":"`+strings.Repeat("x", MaxResponseBytes+1000)+`"}]}}]}`)
	_, err := client(Gemini).Complete(context.Background(), Request{User: "q"})
	if err == nil || !strings.Contains(err.Error(), "unreadable body") {
		t.Errorf("a response past the limit is treated as unreadable, not buffered whole: %v", err)
	}
}

func TestTimeoutIsReportedAsSuch(t *testing.T) {
	f := newFake(t, 200, geminiOK)
	f.delay = 300 * time.Millisecond
	c := client(Gemini)
	c.Timeout = 50 * time.Millisecond
	_, err := c.Complete(context.Background(), Request{User: "q"})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("%v", err)
	}
}

func TestBadModelAndProvider(t *testing.T) {
	newFake(t, 200, geminiOK)
	for _, m := range []string{"", "x/../../evil", "a b", "-lead", strings.Repeat("a", 101), "m?key=1"} {
		c := client(Gemini)
		c.Model = m
		if _, err := c.Complete(context.Background(), Request{User: "q"}); err == nil || !strings.Contains(err.Error(), "invalid model") {
			t.Errorf("model %q: %v", m, err)
		}
	}
	c := client("nonsense")
	if _, err := c.Complete(context.Background(), Request{User: "q"}); err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("%v", err)
	}
}

func TestFromConfig(t *testing.T) {
	cfg := func(provider, model, env string) *config.Config {
		c := config.Defaults()
		c.Testing.Provider, c.Testing.DefaultModel, c.Testing.APIKeyEnv = provider, model, env
		return c
	}
	t.Setenv("GEMINI_API_KEY", "g")
	t.Setenv("ANTHROPIC_API_KEY", "a")
	t.Setenv("OPENAI_API_KEY", "o")
	t.Setenv("CUSTOM_KEY", "c")

	for _, tc := range []struct {
		provider, model, env             string
		wantProvider, wantModel, wantEnv string
	}{
		{"", "", "", Gemini, "gemini-2.5-flash", "GEMINI_API_KEY"},
		{"Anthropic", "", "", Anthropic, "claude-sonnet-4-6", "ANTHROPIC_API_KEY"},
		{"openai", "", "", OpenAI, "gpt-4o-mini", "OPENAI_API_KEY"},
		{"openai", "gpt-x", "CUSTOM_KEY", OpenAI, "gpt-x", "CUSTOM_KEY"},
	} {
		c, err := FromConfig(cfg(tc.provider, tc.model, tc.env))
		if tc.model == "" && tc.env == "" {
			// Defaults() pre-fills Gemini's values; clear them the way a project without [testing] would
			cc := cfg(tc.provider, "", "")
			c, err = FromConfig(cc)
		}
		if err != nil || c.Provider != tc.wantProvider || c.Model != tc.wantModel || c.KeyEnv != tc.wantEnv {
			t.Errorf("%+v: %+v %v", tc, c, err)
		}
	}

	t.Setenv("GEMINI_API_KEY", "")
	if _, err := FromConfig(cfg("gemini", "", "")); err == nil || !strings.Contains(err.Error(), "$GEMINI_API_KEY") || !strings.Contains(err.Error(), ".loomsecret") {
		t.Errorf("%v", err)
	}
	if _, err := FromConfig(cfg("mistral", "", "")); err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("%v", err)
	}
}

func TestAnthropicBlocksWithoutATypeCountAsText(t *testing.T) {
	newFake(t, 200, `{"content":[{"text":"from a gateway"}]}`)
	if out, err := client(Anthropic).Complete(context.Background(), Request{User: "q"}); err != nil || out != "from a gateway" {
		t.Errorf("%q %v", out, err)
	}
}

func TestNewOverridesProviderAndModel(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "g")
	t.Setenv("ANTHROPIC_API_KEY", "a")
	t.Setenv("PROJECT_KEY", "p")
	cfg := config.Defaults()
	cfg.Testing.APIKeyEnv, cfg.Testing.DefaultModel = "PROJECT_KEY", "project-model"

	// the project's own settings apply to the project's provider
	c, err := New(cfg, "", "")
	if err != nil || c.Provider != Gemini || c.Model != "project-model" || c.KeyEnv != "PROJECT_KEY" {
		t.Errorf("%+v %v", c, err)
	}
	// a model override keeps the provider and its key
	if c, _ := New(cfg, "", "other-model"); c.Model != "other-model" || c.KeyEnv != "PROJECT_KEY" {
		t.Errorf("%+v", c)
	}
	// another provider brings ITS key variable and default model, not the project's
	c, err = New(cfg, "anthropic", "")
	if err != nil || c.Provider != Anthropic || c.KeyEnv != "ANTHROPIC_API_KEY" || c.Model != "claude-sonnet-4-6" {
		t.Errorf("%+v %v", c, err)
	}
	if c, _ := New(cfg, "anthropic", "claude-x"); c.Model != "claude-x" {
		t.Errorf("%+v", c)
	}
	if _, err := New(cfg, "openai", ""); err == nil || !strings.Contains(err.Error(), "$OPENAI_API_KEY") {
		t.Errorf("%v", err)
	}
}

func TestParseSpec(t *testing.T) {
	for in, want := range map[string][2]string{
		"gpt-4o": {"", "gpt-4o"}, "openai:gpt-4o": {"openai", "gpt-4o"}, "Anthropic:claude-x": {"anthropic", "claude-x"},
		"model:v1": {"", "model:v1"}, " gemini:m ": {"gemini", "m"}, "": {"", ""},
	} {
		if p, m := ParseSpec(in); p != want[0] || m != want[1] {
			t.Errorf("ParseSpec(%q) = %q, %q; want %q, %q", in, p, m, want[0], want[1])
		}
	}
}
