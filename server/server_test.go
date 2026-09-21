package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sayandeepgiri/promptloom/server/internal/config"
)

func testCfg(secret string) *config.Config {
	return &config.Config{Port: "0", UploadSecret: secret, MaxBodyBytes: 4096, ReadRPM: 1000, WriteRPM: 1000}
}

func call(h http.Handler, method, path, secret, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if secret != "" {
		req.Header.Set("X-Upload-Secret", secret)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// None of these requests may reach the database: they must all be stopped by the
// middleware or input validation first.
func TestWritesAreProtected(t *testing.T) {
	const secret = "0123456789abcdef-secret"
	open := newRouter(testCfg(""), nil)
	closed := newRouter(testCfg(secret), nil)

	// PL-101: with no secret configured, writes fail closed.
	for _, r := range []struct{ method, path string }{
		{"POST", "/api/v1/vaults"}, {"DELETE", "/api/v1/vaults/go-backend"},
	} {
		if got := call(open, r.method, r.path, "", "{}").Code; got != http.StatusServiceUnavailable {
			t.Errorf("%s %s without configured secret: got %d, want 503", r.method, r.path, got)
		}
		if got := call(open, r.method, r.path, "anything", "{}").Code; got != http.StatusServiceUnavailable {
			t.Errorf("%s %s guessing secret on open server: got %d, want 503", r.method, r.path, got)
		}
		// With a secret configured, missing / wrong / wrong-case secrets get 401.
		for _, s := range []string{"", "wrong", strings.ToUpper(secret)} {
			if got := call(closed, r.method, r.path, s, "{}").Code; got != http.StatusUnauthorized {
				t.Errorf("%s %s with secret %q: got %d, want 401", r.method, r.path, s, got)
			}
		}
	}
}

func TestUploadValidationRunsAfterAuth(t *testing.T) {
	const secret = "0123456789abcdef-secret"
	h := newRouter(testCfg(secret), nil)

	traversal := `{"name":"x","slug":"x","version":"1.0.0","files":[{"path":"../../evil","file_type":"prompt","content":"x"}]}`
	rec := call(h, "POST", "/api/v1/vaults", secret, traversal)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "../../evil") {
		t.Errorf("traversal path: got %d %s", rec.Code, rec.Body.String())
	}
	if got := call(h, "POST", "/api/v1/vaults", secret, "not json").Code; got != http.StatusBadRequest {
		t.Errorf("bad json: got %d", got)
	}
	if got := call(h, "POST", "/api/v1/vaults", secret, strings.Repeat("x", 5000)).Code; got != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize body: got %d, want 413", got)
	}
}

func TestBadSlugsRejectedBeforeStore(t *testing.T) {
	h := newRouter(testCfg("0123456789abcdef-secret"), nil)
	for _, p := range []string{"/api/v1/vaults/Bad_Slug!", "/api/v1/vaults/a%20b", "/api/v1/vaults/UPPER/bundle"} {
		if got := call(h, "GET", p, "", "").Code; got != http.StatusBadRequest {
			t.Errorf("GET %s: got %d, want 400", p, got)
		}
	}
}

func TestHealthAndHeaders(t *testing.T) {
	h := newRouter(testCfg(""), nil)
	rec := call(h, "GET", "/healthz", "", "")
	if rec.Code != http.StatusOK || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("healthz: %d %v", rec.Code, rec.Header())
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORS headers must be absent by default")
	}
}

func TestWriteRateLimit(t *testing.T) {
	cfg := testCfg("0123456789abcdef-secret")
	cfg.WriteRPM = 3
	h := newRouter(cfg, nil)
	codes := []int{}
	for i := 0; i < 5; i++ {
		codes = append(codes, call(h, "POST", "/api/v1/vaults", "wrong", "{}").Code)
	}
	// Failed auth attempts count against the budget, so brute force is throttled.
	if codes[3] != http.StatusTooManyRequests || codes[4] != http.StatusTooManyRequests {
		t.Errorf("expected 429 after burst, got %v", codes)
	}
}
