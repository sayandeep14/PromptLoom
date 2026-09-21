package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sayandeep14/PromptLoom/loomlocker/internal/config"
	"github.com/sayandeep14/PromptLoom/loomlocker/internal/locker"
)

const password = "session-pw"

type env struct {
	srv  *Server
	base string
	dir  string
	env  string // path of the secret file
}

func newEnv(t *testing.T, mutate func(*config.Config)) *env {
	t.Helper()
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("API_KEY=real-secret\nDEBUG=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Secret: []string{".env:{API_KEY}"},
		Locker: config.LockerConfig{Active: true, LockHost: "http://localhost", Port: "0", UnlockDurationSeconds: 1},
	}
	if mutate != nil {
		mutate(cfg)
	}
	srv, err := New(cfg, dir, password)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Shutdown)
	return &env{srv: srv, base: "http://" + srv.Addr(), dir: dir, env: envPath}
}

func (e *env) file(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(e.env)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func do(t *testing.T, method, url, contentType, body string, hdr map[string]string) (int, string, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range hdr {
		if strings.EqualFold(k, "Host") {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func postJSON(t *testing.T, e *env, path string, v any) (int, string, http.Header) {
	t.Helper()
	b, _ := json.Marshal(v)
	return do(t, "POST", e.base+path, "application/json", string(b), nil)
}

func TestListensOnLoopbackOnly(t *testing.T) {
	e := newEnv(t, nil)
	host, _, err := net.SplitHostPort(e.srv.Addr())
	if err != nil || host != "127.0.0.1" {
		t.Fatalf("server listens on %q, must be 127.0.0.1 only (never 0.0.0.0)", e.srv.Addr())
	}
}

func TestStartReportsAPortInUse(t *testing.T) {
	e := newEnv(t, nil)
	_, port, _ := net.SplitHostPort(e.srv.Addr())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("A=1\n"), 0o644)
	other, err := New(&config.Config{Secret: []string{".env"},
		Locker: config.LockerConfig{LockHost: "http://localhost", Port: port, UnlockDurationSeconds: 1}}, dir, password)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Start(); err == nil {
		t.Error("Start must fail when the port is taken (it used to print 'started' and listen on nothing)")
	}
}

func TestLockUnlockLifecycle(t *testing.T) {
	e := newEnv(t, nil)

	code, _, _ := do(t, "GET", e.base+"/api/ping", "", "", nil)
	if code != 200 {
		t.Fatalf("ping: %d", code)
	}

	if code, body, _ := do(t, "POST", e.base+"/api/lock", "", "", nil); code != 200 {
		t.Fatalf("lock: %d %s", code, body)
	}
	locked := e.file(t)
	if strings.Contains(locked, "real-secret") || !strings.Contains(locked, "DEBUG=1") || !strings.Contains(locked, "API_KEY=lk_") {
		t.Errorf("locked file: %q", locked)
	}

	if code, _, _ := postJSON(t, e, "/api/unlock", map[string]string{"password": "wrong"}); code != http.StatusUnauthorized {
		t.Errorf("wrong password: %d", code)
	}
	if e.file(t) != locked {
		t.Error("a wrong password must not change the file")
	}

	if code, body, _ := postJSON(t, e, "/api/unlock", map[string]string{"password": password}); code != 200 {
		t.Fatalf("unlock: %d %s", code, body)
	}
	if got := e.file(t); got != "API_KEY=real-secret\nDEBUG=1\n" {
		t.Errorf("unlocked file: %q", got)
	}

	// auto-relock (1s window)
	deadline := time.Now().Add(5 * time.Second)
	for e.srv.IsLocked() == false && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !e.srv.IsLocked() || strings.Contains(e.file(t), "real-secret") {
		t.Errorf("secrets must re-lock automatically after the unlock window: %q", e.file(t))
	}
}

func TestAutolockCancelsTheTimerAndLocksNow(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Locker.UnlockDurationSeconds = 30 })
	do(t, "POST", e.base+"/api/lock", "", "", nil)
	postJSON(t, e, "/api/unlock", map[string]string{"password": password})
	if e.srv.IsLocked() {
		t.Fatal("should be unlocked")
	}
	if code, _, _ := do(t, "POST", e.base+"/api/autolock", "", "", nil); code != 200 {
		t.Fatalf("autolock: %d", code)
	}
	if !e.srv.IsLocked() || strings.Contains(e.file(t), "real-secret") {
		t.Error("autolock must lock immediately, not after the window")
	}
}

// ---- protections ----

func TestRefusesForeignHostHeaders(t *testing.T) {
	e := newEnv(t, nil)
	for _, host := range []string{"evil.example.com", "evil.example.com:8053", "192.168.1.5", "localhost.evil.com"} {
		if code, _, _ := do(t, "GET", e.base+"/api/status", "", "", map[string]string{"Host": host}); code != http.StatusForbidden {
			t.Errorf("Host %q: got %d, want 403 (DNS rebinding defence)", host, code)
		}
	}
	if code, _, _ := do(t, "GET", e.base+"/api/ping", "", "", map[string]string{"Host": "localhost"}); code != 200 {
		t.Errorf("Host localhost must be accepted: %d", code)
	}
}

func TestRefusesBrowserRequests(t *testing.T) {
	e := newEnv(t, nil)
	do(t, "POST", e.base+"/api/lock", "", "", nil)
	locked := e.file(t)
	for _, origin := range []string{"https://evil.example.com", "http://localhost:3000", "null"} {
		hdr := map[string]string{"Origin": origin}
		for _, req := range []struct{ method, path, ct, body string }{
			{"GET", "/api/status", "", ""},
			{"POST", "/api/unlock", "application/json", `{"password":"session-pw"}`},
			{"POST", "/api/stop", "", ""},
			{"POST", "/api/lock", "", ""},
		} {
			if code, _, _ := do(t, req.method, e.base+req.path, req.ct, req.body, hdr); code != http.StatusForbidden {
				t.Errorf("%s %s with Origin %q: got %d, want 403", req.method, req.path, origin, code)
			}
		}
	}
	if e.file(t) != locked || !e.srv.IsLocked() {
		t.Error("no browser request may change the lock state, even with the right password")
	}
}

func TestBodiesMustBeJSONAndSmall(t *testing.T) {
	e := newEnv(t, nil)
	do(t, "POST", e.base+"/api/lock", "", "", nil)

	// a text/plain POST is what a page can send without a CORS preflight
	if code, _, _ := do(t, "POST", e.base+"/api/unlock", "text/plain", `{"password":"session-pw"}`, nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: %d, want 415", code)
	}
	if code, _, _ := do(t, "POST", e.base+"/api/unlock", "application/x-www-form-urlencoded", "password=session-pw", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("form body: %d, want 415", code)
	}
	if !e.srv.IsLocked() {
		t.Fatal("still locked")
	}
	if code, _, _ := do(t, "POST", e.base+"/api/unlock", "application/json; charset=utf-8", `{"password":"session-pw"}`, nil); code != 200 {
		t.Errorf("application/json with a charset must work: %d", code)
	}

	big := `{"password":"` + strings.Repeat("a", 10<<10) + `"}`
	if code, _, _ := do(t, "POST", e.base+"/api/unlock", "application/json", big, nil); code == 200 {
		t.Error("an oversized body must be rejected")
	}
}

func TestResponsesAreNotCached(t *testing.T) {
	e := newEnv(t, nil)
	_, _, h := do(t, "GET", e.base+"/api/status", "", "", nil)
	if h.Get("Cache-Control") != "no-store" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers: %v", h)
	}
	if h.Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORS must never be enabled")
	}
}

func TestPasswordGuessingIsSlowedDown(t *testing.T) {
	e := newEnv(t, nil)
	do(t, "POST", e.base+"/api/lock", "", "", nil)

	for i := 0; i < freeAttempts; i++ {
		if code, _, _ := postJSON(t, e, "/api/unlock", map[string]string{"password": "guess" + strconv.Itoa(i)}); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, code)
		}
	}
	code, body, h := postJSON(t, e, "/api/unlock", map[string]string{"password": "one more"})
	if code != http.StatusTooManyRequests || h.Get("Retry-After") == "" {
		t.Fatalf("after %d failures: %d %s (Retry-After=%q)", freeAttempts, code, body, h.Get("Retry-After"))
	}
	// even the RIGHT password is refused while backing off, so guessing cannot continue
	if code, _, _ := postJSON(t, e, "/api/unlock", map[string]string{"password": password}); code != http.StatusTooManyRequests {
		t.Errorf("the correct password during the lockout: %d, want 429", code)
	}
	// stop is guarded by the same limiter
	if code, _, _ := postJSON(t, e, "/api/stop", map[string]string{"password": "x"}); code != http.StatusTooManyRequests {
		t.Errorf("stop during the lockout: %d, want 429", code)
	}
	if !e.srv.IsLocked() {
		t.Error("still locked")
	}
}

func TestStopWhileLockedNeedsThePasswordAndRestoresFirst(t *testing.T) {
	e := newEnv(t, nil)
	do(t, "POST", e.base+"/api/lock", "", "", nil)

	if code, _, _ := do(t, "POST", e.base+"/api/stop", "", "", nil); code != http.StatusUnauthorized {
		t.Errorf("stop without a password while locked: %d, want 401", code)
	}
	select {
	case <-e.srv.Done():
		t.Fatal("the server must not stop without the password")
	case <-time.After(300 * time.Millisecond):
	}
	if strings.Contains(e.file(t), "real-secret") {
		t.Error("still locked")
	}

	if code, _, _ := postJSON(t, e, "/api/stop", map[string]string{"password": password}); code != 200 {
		t.Fatalf("stop with the password: %d", code)
	}
	select {
	case <-e.srv.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("the server should have stopped")
	}
	if got := e.file(t); got != "API_KEY=real-secret\nDEBUG=1\n" {
		t.Errorf("stopping must restore the real values first: %q", got)
	}
}

func TestFailedLockLeavesFilesAndStateAlone(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Secret = []string{".env:{API_KEY}", ".env:{MISSING}"} })
	code, body, _ := do(t, "POST", e.base+"/api/lock", "", "", nil)
	if code != http.StatusInternalServerError || !strings.Contains(body, "MISSING") {
		t.Errorf("lock: %d %s", code, body)
	}
	if got := e.file(t); got != "API_KEY=real-secret\nDEBUG=1\n" {
		t.Errorf("a failed lock must not change any file: %q", got)
	}
	if e.srv.IsLocked() {
		t.Error("state must stay unlocked")
	}
}

func TestRelockFailureIsLoggedNotSilent(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Locker.UnlockDurationSeconds = 1 })
	do(t, "POST", e.base+"/api/lock", "", "", nil)
	postJSON(t, e, "/api/unlock", map[string]string{"password": password})
	os.Remove(e.env) // the file disappears during the unlock window: the automatic re-lock cannot work
	time.Sleep(1500 * time.Millisecond)
	// must not crash or deadlock; the server keeps answering
	if code, _, _ := do(t, "GET", e.base+"/api/status", "", "", nil); code != 200 {
		t.Errorf("server unresponsive after a failed re-lock: %d", code)
	}
}

// ---- recoverable mode ----

func TestRecoverableModeSurvivesACrash(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Locker.Recoverable = true })
	journal := filepath.Join(e.dir, locker.JournalFilename)

	if _, err := os.Stat(journal); err == nil {
		t.Fatal("no journal before anything is locked")
	}
	if code, body, _ := do(t, "POST", e.base+"/api/lock", "", "", nil); code != 200 {
		t.Fatalf("lock: %d %s", code, body)
	}
	raw, err := os.ReadFile(journal)
	if err != nil {
		t.Fatalf("recoverable mode must save the journal: %v", err)
	}
	if bytes.Contains(raw, []byte("real-secret")) || bytes.Contains(raw, []byte(password)) {
		t.Error("the journal must be encrypted")
	}
	if strings.Contains(e.file(t), "real-secret") {
		t.Fatal("locked")
	}

	// --- the process dies here: no unlock, no shutdown ---
	mapping, err := locker.ReadJournal(journal, password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locker.RestoreFromMapping(mapping); err != nil {
		t.Fatal(err)
	}
	if got := e.file(t); got != "API_KEY=real-secret\nDEBUG=1\n" {
		t.Errorf("recovery restored %q", got)
	}
}

func TestRecoverableUnlockRemovesTheJournal(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Locker.Recoverable = true; c.Locker.UnlockDurationSeconds = 30 })
	journal := filepath.Join(e.dir, locker.JournalFilename)
	do(t, "POST", e.base+"/api/lock", "", "", nil)
	if _, err := os.Stat(journal); err != nil {
		t.Fatal("journal expected while locked")
	}
	postJSON(t, e, "/api/unlock", map[string]string{"password": password})
	if _, err := os.Stat(journal); err == nil {
		t.Error("the journal must be removed once the real values are back in the files")
	}
	do(t, "POST", e.base+"/api/lock", "", "", nil)
	if _, err := os.Stat(journal); err != nil {
		t.Error("locking again must write a fresh journal")
	}
}

func TestStartupRefusesWhenAJournalIsWaiting(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("A=1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, locker.JournalFilename), []byte("leftover"), 0o600)
	_, err := New(&config.Config{Secret: []string{".env"},
		Locker: config.LockerConfig{Recoverable: true, LockHost: "http://localhost", Port: "0", UnlockDurationSeconds: 1}}, dir, password)
	if err == nil || !strings.Contains(err.Error(), "loomlocker recover") {
		t.Errorf("must point at `loomlocker recover`: %v", err)
	}
}

func TestNonRecoverableModeWritesNothingToDisk(t *testing.T) {
	e := newEnv(t, nil)
	do(t, "POST", e.base+"/api/lock", "", "", nil)
	entries, _ := os.ReadDir(e.dir)
	for _, en := range entries {
		if en.Name() != ".env" {
			t.Errorf("unexpected file %q: non-recoverable mode keeps the mapping in memory only", en.Name())
		}
	}
}

// ---- limiter ----

func TestAttemptLimiterBackoff(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newAttemptLimiter()
	l.now = func() time.Time { return now }

	for i := 0; i < freeAttempts-1; i++ {
		l.failed()
		if l.blockedFor() != 0 {
			t.Fatalf("no delay expected before %d failures", freeAttempts)
		}
	}
	l.failed()
	if d := l.blockedFor(); d != time.Second {
		t.Errorf("first delay = %v, want 1s", d)
	}
	now = now.Add(2 * time.Second)
	if l.blockedFor() != 0 {
		t.Error("the delay must expire")
	}
	l.failed()
	if d := l.blockedFor(); d != 2*time.Second {
		t.Errorf("second delay = %v, want 2s (doubling)", d)
	}
	for i := 0; i < 30; i++ {
		now = now.Add(time.Hour)
		l.failed()
	}
	if d := l.blockedFor(); d != maxBackoff {
		t.Errorf("delay must be capped at %v, got %v", maxBackoff, d)
	}
	l.succeeded()
	if l.blockedFor() != 0 {
		t.Error("success must reset the limiter")
	}
	l.failed()
	if l.blockedFor() != 0 {
		t.Error("after a reset the free attempts are available again")
	}
}
