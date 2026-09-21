package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var ok200 = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

func do(h http.Handler, method, target string, hdr map[string]string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRequireSecret(t *testing.T) {
	const secret = "s3cret-value-1234567890"
	h := RequireSecret(secret)(ok200)

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"correct", secret, http.StatusOK},
		{"missing", "", http.StatusUnauthorized},
		{"wrong", "nope", http.StatusUnauthorized},
		{"different case must fail", strings.ToUpper(secret), http.StatusUnauthorized},
		{"prefix must fail", secret[:5], http.StatusUnauthorized},
		{"suffix appended must fail", secret + "x", http.StatusUnauthorized},
	}
	for _, c := range cases {
		hdr := map[string]string{}
		if c.header != "" {
			hdr[SecretHeader] = c.header
		}
		if got := do(h, "POST", "/", hdr, "").Code; got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestRequireSecretFailsClosedWhenUnset(t *testing.T) {
	h := RequireSecret("")(ok200)
	// Neither an empty header nor any header may get through.
	for _, hdr := range []map[string]string{nil, {SecretHeader: ""}, {SecretHeader: "anything"}} {
		if got := do(h, "POST", "/", hdr, "").Code; got != http.StatusServiceUnavailable {
			t.Errorf("headers %v: got %d, want 503", hdr, got)
		}
	}
}

func TestMaxBody(t *testing.T) {
	read := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		for {
			if _, err := r.Body.Read(buf); err != nil {
				if err.Error() == "EOF" {
					w.WriteHeader(http.StatusOK)
				} else {
					w.WriteHeader(http.StatusRequestEntityTooLarge)
				}
				return
			}
		}
	})
	h := MaxBody(10)(read)
	if got := do(h, "POST", "/", nil, "12345").Code; got != http.StatusOK {
		t.Errorf("small body: got %d", got)
	}
	if got := do(h, "POST", "/", nil, strings.Repeat("x", 100)).Code; got != http.StatusRequestEntityTooLarge {
		t.Errorf("large body: got %d", got)
	}
}

func TestLimiterBurstAndRefill(t *testing.T) {
	l := NewLimiter(60) // 1 token/sec, burst 60
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }

	for i := 0; i < 60; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d denied within burst", i)
		}
	}
	ok, wait := l.Allow("a")
	if ok || wait <= 0 {
		t.Fatalf("expected denial with positive wait, got ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Error("a different key must have its own budget")
	}
	now = now.Add(2 * time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Error("token should have refilled after 2s")
	}
}

func TestLimiterEvictsIdleKeys(t *testing.T) {
	l := NewLimiter(10)
	l.maxKeys = 3
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }
	for _, k := range []string{"a", "b", "c"} {
		l.Allow(k)
	}
	now = now.Add(time.Hour)
	l.Allow("d")
	if len(l.buckets) > 3 {
		t.Errorf("map grew to %d entries", len(l.buckets))
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	h := RateLimit(NewLimiter(2), false)(ok200)
	for i := 0; i < 2; i++ {
		if got := do(h, "GET", "/", nil, "").Code; got != http.StatusOK {
			t.Fatalf("request %d: %d", i, got)
		}
	}
	rec := do(h, "GET", "/", nil, "")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("got %d, Retry-After=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	req.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")
	if got := ClientIP(req, false); got != "10.0.0.1" {
		t.Errorf("untrusted proxy: got %q (spoofable header must be ignored)", got)
	}
	if got := ClientIP(req, true); got != "203.0.113.9" {
		t.Errorf("trusted proxy: got %q, want the proxy-appended entry", got)
	}
}

func TestCORS(t *testing.T) {
	origin := map[string]string{"Origin": "https://evil.example"}

	if v := do(CORS(nil)(ok200), "GET", "/", origin, "").Header().Get("Access-Control-Allow-Origin"); v != "" {
		t.Errorf("default must send no CORS headers, got %q", v)
	}
	h := CORS([]string{"https://app.example"})(ok200)
	if v := do(h, "GET", "/", origin, "").Header().Get("Access-Control-Allow-Origin"); v != "" {
		t.Errorf("unlisted origin got %q", v)
	}
	rec := do(h, "GET", "/", map[string]string{"Origin": "https://app.example"}, "")
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example" || rec.Header().Get("Vary") != "Origin" {
		t.Errorf("listed origin headers wrong: %v", rec.Header())
	}
	if v := do(CORS([]string{"*"})(ok200), "GET", "/", origin, "").Header().Get("Access-Control-Allow-Origin"); v != "*" {
		t.Errorf("wildcard: got %q", v)
	}
	if got := do(h, "OPTIONS", "/", origin, "").Code; got != http.StatusNoContent {
		t.Errorf("preflight: got %d", got)
	}
}

func TestChainOrder(t *testing.T) {
	var order []string
	mk := func(n string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, n)
				next.ServeHTTP(w, r)
			})
		}
	}
	do(Chain(ok200, mk("a"), mk("b")), "GET", "/", nil, "")
	if strings.Join(order, "") != "ab" {
		t.Errorf("got order %v, want a then b", order)
	}
}
