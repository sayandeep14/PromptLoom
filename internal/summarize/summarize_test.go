package summarize

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/llm"
)

const secretKey = "SUPER-SECRET-KEY-123"

// fakeAPI serves both providers and records what it was sent.
type fakeAPI struct {
	srv      *httptest.Server
	requests []*http.Request
	bodies   []string
	status   int
	reply    string
}

func newFakeAPI(t *testing.T, status int, reply string) *fakeAPI {
	t.Helper()
	f := &fakeAPI{status: status, reply: reply}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.requests = append(f.requests, r)
		f.bodies = append(f.bodies, string(b))
		w.WriteHeader(f.status)
		io.WriteString(w, f.reply)
	}))
	oldG, oldA := llm.GeminiBaseURL, llm.AnthropicURL
	llm.GeminiBaseURL, llm.AnthropicURL = f.srv.URL+"/v1beta", f.srv.URL+"/v1/messages"
	t.Cleanup(func() { f.srv.Close(); llm.GeminiBaseURL, llm.AnthropicURL = oldG, oldA })
	return f
}

func cfgFor(provider, model, envVar string) *config.Config {
	c := config.Defaults()
	c.Testing.Provider, c.Testing.DefaultModel, c.Testing.APIKeyEnv = provider, model, envVar
	return c
}

const geminiOK = `{"candidates":[{"content":{"parts":[{"text":"# Summary\nIt does things."}]}}]}`
const anthropicOK = `{"content":[{"text":"# Summary\nIt does things."}]}`

func TestGeminiKeyTravelsInAHeaderNotTheURL(t *testing.T) {
	f := newFakeAPI(t, 200, geminiOK)
	t.Setenv("GEMINI_API_KEY", secretKey)
	out, err := callLLM("sys", "user", cfgFor("gemini", "gemini-2.5-flash", ""), time.Second*5)
	if err != nil || !strings.HasPrefix(out, "# Summary") {
		t.Fatalf("%q %v", out, err)
	}
	r := f.requests[0]
	if strings.Contains(r.URL.String(), secretKey) || r.URL.RawQuery != "" {
		t.Errorf("key in URL: %s", r.URL)
	}
	if r.Header.Get("x-goog-api-key") != secretKey || !strings.HasSuffix(r.URL.Path, "/models/gemini-2.5-flash:generateContent") {
		t.Errorf("%s %v", r.URL.Path, r.Header)
	}
	if !strings.Contains(f.bodies[0], `"text":"sys"`) || !strings.Contains(f.bodies[0], `"text":"user"`) {
		t.Errorf("%s", f.bodies[0])
	}
}

func TestAnthropicRequestAndDefaultModel(t *testing.T) {
	f := newFakeAPI(t, 200, anthropicOK)
	t.Setenv("ANTHROPIC_API_KEY", secretKey)
	// no model configured: the default must be a Claude model, not the Gemini one
	if _, err := callLLM("sys", "user", cfgFor("anthropic", "", ""), time.Second*5); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Model    string `json:"model"`
		System   string `json:"system"`
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	json.Unmarshal([]byte(f.bodies[0]), &body)
	if !strings.HasPrefix(body.Model, "claude") || body.System != "sys" || body.Messages[0].Content != "user" {
		t.Errorf("%+v", body)
	}
	if f.requests[0].Header.Get("x-api-key") != secretKey || f.requests[0].Header.Get("anthropic-version") == "" {
		t.Errorf("%v", f.requests[0].Header)
	}
}

func TestProviderAndKeyErrors(t *testing.T) {
	newFakeAPI(t, 200, geminiOK)
	t.Setenv("GEMINI_API_KEY", "")
	if _, err := callLLM("s", "u", cfgFor("gemini", "", ""), time.Second); err == nil || !strings.Contains(err.Error(), "$GEMINI_API_KEY") {
		t.Errorf("%v", err)
	}
	t.Setenv("MY_KEY", "k")
	if _, err := callLLM("s", "u", cfgFor("mistral", "", "MY_KEY"), time.Second); err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("an unknown provider must not silently use Gemini: %v", err)
	}
	if _, err := callLLM("s", "u", cfgFor("gemini", "x/../../evil", "MY_KEY"), time.Second); err == nil || !strings.Contains(err.Error(), "invalid model") {
		t.Errorf("%v", err)
	}
}

func TestErrorsNeverContainTheKey(t *testing.T) {
	// nothing is listening: the transport error would echo the URL
	oldG := llm.GeminiBaseURL
	llm.GeminiBaseURL = "http://127.0.0.1:1/v1beta"
	defer func() { llm.GeminiBaseURL = oldG }()
	t.Setenv("GEMINI_API_KEY", secretKey)
	_, err := callLLM("s", "u", cfgFor("gemini", "gemini-2.5-flash", ""), time.Second*2)
	if err == nil || strings.Contains(err.Error(), secretKey) {
		t.Errorf("%v", err)
	}
}

func TestAPIErrorsAndBadBodies(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "k")
	t.Setenv("ANTHROPIC_API_KEY", "k")
	cases := []struct {
		provider, reply, want string
		status                int
	}{
		{"gemini", `{"error":{"code":429,"message":"quota exceeded"}}`, "quota exceeded", 429},
		{"gemini", `<html>bad gateway</html>`, "HTTP 502", 502},
		{"gemini", `{"candidates":[]}`, "no content", 200},
		{"anthropic", `{"error":{"type":"overloaded_error","message":"busy"}}`, "busy", 529},
		{"anthropic", ``, "HTTP 500", 500},
		{"anthropic", `{"content":[]}`, "no content", 200},
	}
	for _, c := range cases {
		newFakeAPI(t, c.status, c.reply)
		_, err := callLLM("s", "u", cfgFor(c.provider, "some-model", ""), time.Second*5)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %q: %v (want %q)", c.provider, c.reply, err, c.want)
		}
	}
}

func TestBuildPathContextNeverIncludesCredentials(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	write("src/main.go", "package main // visible")
	write("src/.env", "TOKEN=abc")
	write("src/.loomsecret", "GEMINI_API_KEY=abc")
	write("src/server.pem", "-----BEGIN PRIVATE KEY-----")
	write("src/logo.bin", "PNG\x00\x01binary")
	write("node_modules/dep/index.js", "skipped")
	os.Symlink("/etc/hosts", filepath.Join(dir, "src", "link"))

	ctx, err := buildPathContext([]string{"src", "node_modules"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ctx, "package main // visible") {
		t.Error("normal source must be included")
	}
	for _, leak := range []string{"TOKEN=abc", "GEMINI_API_KEY", "PRIVATE KEY", "binary", "localhost"} {
		if strings.Contains(ctx, leak) {
			t.Errorf("%q leaked into the model context:\n%s", leak, ctx)
		}
	}

	// naming a credentials file explicitly is refused (it would be uploaded to the provider)
	write(".loomsecret", "GEMINI_API_KEY=abc")
	if _, err := buildPathContext([]string{".loomsecret"}, dir); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Errorf("%v", err)
	}
	if _, err := buildPathContext([]string{"missing"}, dir); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("%v", err)
	}
}

func TestWorkspaceContext(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("TOKEN=abc"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte(strings.Repeat("é", 4000)), 0o644)
	os.MkdirAll(filepath.Join(dir, "cmd"), 0o755)
	os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("package main"), 0o644)
	os.MkdirAll(filepath.Join(dir, "node_modules", "x"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)

	ctx := buildWorkspaceContext(dir, buildFileTree(dir))
	for _, want := range []string{"cmd/", "go.mod", "cmd/                           1 files", "module x"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("missing %q in\n%s", want, ctx)
		}
	}
	for _, bad := range []string{"node_modules", ".git", "TOKEN=abc", "�"} {
		if strings.Contains(ctx, bad) {
			t.Errorf("unexpected %q", bad)
		}
	}
	if !strings.Contains(ctx, "[truncated]") {
		t.Error("a long README is truncated")
	}
}

func TestSummarizeSavesWhereAsked(t *testing.T) {
	newFakeAPI(t, 200, geminiOK)
	t.Setenv("GEMINI_API_KEY", "k")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644)
	cfg := cfgFor("gemini", "gemini-2.5-flash", "")

	res, err := SummarizeWorkspace(dir, cfg, Options{Save: true})
	if err != nil || res.SavedTo != filepath.Join(dir, ".loom", "context", "architecture-summary.md") {
		t.Fatalf("%+v %v", res, err)
	}
	if b, _ := os.ReadFile(res.SavedTo); !strings.HasPrefix(string(b), "# Summary") {
		t.Errorf("%q", b)
	}
	if res, _ := SummarizeWorkspace(dir, cfg, Options{}); res.SavedTo != "" {
		t.Error("nothing is written unless asked")
	}
	custom := filepath.Join(dir, "out", "s.md")
	if res, err := SummarizeWorkspace(dir, cfg, Options{OutputPath: custom}); err != nil || res.SavedTo != custom {
		t.Errorf("%+v %v", res, err)
	}

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	res, err = SummarizePaths([]string{"main.go"}, dir, cfg, Options{Save: true})
	if err != nil || res.SavedTo != filepath.Join(dir, ".loom", "context", "main-summary.md") {
		t.Errorf("%+v %v", res, err)
	}
	if _, err := SummarizePaths([]string{"nope.go"}, dir, cfg, Options{}); err == nil {
		t.Error("missing path")
	}
}

func TestClipAndSlug(t *testing.T) {
	s := strings.Repeat("é", 10) // 2 bytes each
	got := clip(s, 5)
	if strings.Contains(got, "�") || !strings.HasPrefix(got, "éé") || !strings.HasSuffix(got, "[truncated]") {
		t.Errorf("%q", got)
	}
	if clip("short", 100) != "short" {
		t.Error("short text is untouched")
	}
	for in, want := range map[string]string{"internal/My File.go": "my-file", "": "summary", "/": "summary", "..": "summary", "a.b.c": "a-b"} {
		var paths []string
		if in != "" {
			paths = []string{in}
		}
		if got := pathSlug(paths); got != want {
			t.Errorf("pathSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
