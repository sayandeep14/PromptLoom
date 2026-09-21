package lockerclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckLoopback(t *testing.T) {
	for _, ok := range []string{"http://localhost", "http://127.0.0.1", "http://[::1]", "http://localhost:8053"} {
		if err := CheckLoopback(ok); err != nil {
			t.Errorf("%q should be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"http://example.com", "http://10.0.0.5", "http://0.0.0.0", "https://localhost", "localhost",
		"http://localhost.evil.com", "http://127.0.0.1.evil.com", "http://evil.com/localhost", "", "ftp://localhost",
	} {
		if err := CheckLoopback(bad); err == nil {
			t.Errorf("%q must be refused: the password would be sent to another host", bad)
		} else if bad != "" && !strings.Contains(err.Error(), "lockhost") {
			t.Errorf("the error should name the setting: %v", err)
		}
	}
}

// fake server speaking the loomlocker API
func fake(t *testing.T, locked bool, unlockStatus int) (*Client, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/ping":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "locked": locked})
		case "/api/unlock":
			var b struct{ Password string }
			_ = json.NewDecoder(r.Body).Decode(&b)
			if r.Header.Get("Content-Type") != "application/json" || b.Password != "pw" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(unlockStatus)
		case "/api/lock":
			w.WriteHeader(200)
		}
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL), &calls
}

func TestClientAgainstAServer(t *testing.T) {
	c, _ := fake(t, true, 200)
	if !c.IsRunning() || !c.IsLocked() {
		t.Error("running and locked expected")
	}
	if err := c.Unlock("wrong"); err == nil || !strings.Contains(err.Error(), "invalid password") {
		t.Errorf("wrong password: %v", err)
	}
	if err := c.Unlock("pw"); err != nil {
		t.Errorf("right password: %v", err)
	}
	if err := c.Lock(); err != nil {
		t.Errorf("lock: %v", err)
	}
}

func TestClientWhenNothingIsListening(t *testing.T) {
	c := New("http://127.0.0.1:1")
	if c.IsRunning() || c.IsLocked() {
		t.Error("an unreachable server is neither running nor locked")
	}
	if err := c.Unlock("pw"); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("unlock: %v", err)
	}
	if err := c.Lock(); err == nil {
		t.Error("lock on an unreachable server must be an error")
	}
}

func TestClientServerErrors(t *testing.T) {
	c, _ := fake(t, true, http.StatusInternalServerError)
	if err := c.Unlock("pw"); err == nil || !strings.Contains(err.Error(), "unlock failed") {
		t.Errorf("a 500 must surface: %v", err)
	}
}
