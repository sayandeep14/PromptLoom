package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sayandeepgiri/promptloom/loomlocker/internal/config"
	icrypto "github.com/sayandeepgiri/promptloom/loomlocker/internal/crypto"
	"github.com/sayandeepgiri/promptloom/loomlocker/internal/locker"
)

// Server is the loomlocker HTTP server + shared lock state.
type Server struct {
	cfg         *config.Config
	workDir     string
	state       *locker.State
	pwdHash     []byte // bcrypt hash of session password
	encKey      []byte // Argon2id-derived key (recoverable mode only)
	salt        []byte // salt for encKey, stored in the journal
	relockTimer *time.Timer
	mu          sync.Mutex
	http        *http.Server
	listener    net.Listener
	limiter     *attemptLimiter
	done        chan struct{} // closed when Shutdown is called
}

// journalPath is where the encrypted recovery file lives (recoverable mode).
func (s *Server) journalPath() string {
	return filepath.Join(s.workDir, locker.JournalFilename)
}

// New creates a Server. password is the plain-text password entered at start.
func New(cfg *config.Config, workDir, password string) (*Server, error) {
	hash, err := icrypto.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	srv := &Server{
		cfg:     cfg,
		workDir: workDir,
		state:   locker.NewState(),
		pwdHash: hash,
		limiter: newAttemptLimiter(),
		done:    make(chan struct{}),
	}

	if cfg.Locker.Recoverable {
		// Encrypted journal: if this process dies while secrets are locked, the real
		// values can still be restored with `loomlocker recover` and the password.
		if _, err := os.Stat(srv.journalPath()); err == nil {
			return nil, fmt.Errorf("found %s from an earlier session that ended while secrets were locked.\n"+
				"  Your real values are saved (encrypted) in it. Restore them first:  loomlocker recover",
				locker.JournalFilename)
		}
		key, salt, err := icrypto.DeriveKey(password, nil)
		if err != nil {
			return nil, fmt.Errorf("derive key: %w", err)
		}
		srv.encKey, srv.salt = key, salt
	}
	return srv, nil
}

// Done returns a channel closed when the server shuts down.
func (s *Server) Done() <-chan struct{} { return s.done }

// Addr returns the address the server is listening on (after Start).
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Start begins listening. It binds to the loopback interface ONLY: the unlock endpoint
// must never be reachable from the network. Listen errors (for example the port is taken)
// are returned instead of being lost in a goroutine.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", "127.0.0.1:"+s.cfg.Locker.Port)
	if err != nil {
		return fmt.Errorf("listen on 127.0.0.1:%s: %w", s.cfg.Locker.Port, err)
	}
	s.listener = ln
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/ping", s.handlePing)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/lock", s.handleLock)
	mux.HandleFunc("POST /api/unlock", s.handleUnlock)
	mux.HandleFunc("POST /api/autolock", s.handleAutolock)
	mux.HandleFunc("POST /api/stop", s.handleStop)

	s.http = &http.Server{
		Handler:           guard(port, mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	go func() {
		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("loomlocker server error: %v", err)
		}
	}()
	return nil
}

// guard protects the local API from web pages and other hosts:
//   - Host must be a loopback name (defeats DNS rebinding)
//   - requests carrying an Origin header are refused: browsers add it to cross-origin
//     requests, while the CLI and client libraries never send it
//   - bodies are small, and JSON when present
func guard(port string, next http.Handler) http.Handler {
	allowedHosts := map[string]bool{
		"localhost:" + port: true, "127.0.0.1:" + port: true, "[::1]:" + port: true,
		"localhost": true, "127.0.0.1": true, "[::1]": true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedHosts[strings.ToLower(r.Host)] {
			writeError(w, http.StatusForbidden, "forbidden host")
			return
		}
		if r.Header.Get("Origin") != "" {
			writeError(w, http.StatusForbidden, "cross-origin requests are not allowed")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodPost && r.ContentLength != 0 {
			if ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0])); ct != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		next.ServeHTTP(w, r)
	})
}

// Shutdown stops the HTTP server cleanly and signals Done.
func (s *Server) Shutdown() {
	if s.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.http.Shutdown(ctx)
	}
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

// VerifyPassword checks a plain-text password against the stored hash.
func (s *Server) VerifyPassword(password string) bool {
	return icrypto.VerifyPassword(password, s.pwdHash)
}

// ErrInvalidPassword is returned for a wrong password.
var ErrInvalidPassword = errors.New("invalid password")

// TooManyAttemptsError is returned while password guessing is being slowed down.
type TooManyAttemptsError struct{ RetryAfter time.Duration }

func (e *TooManyAttemptsError) Error() string {
	return fmt.Sprintf("too many wrong passwords; try again in %ds", int(e.RetryAfter.Seconds())+1)
}

// checkPassword verifies password and applies an increasing delay after repeated failures.
func (s *Server) checkPassword(password string) error {
	if wait := s.limiter.blockedFor(); wait > 0 {
		return &TooManyAttemptsError{RetryAfter: wait}
	}
	if !s.VerifyPassword(password) {
		s.limiter.failed()
		return ErrInvalidPassword
	}
	s.limiter.succeeded()
	return nil
}

// Lock locks all secrets. Does not require a password.
func (s *Server) Lock() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doLock()
}

// Unlock verifies password, restores secrets, starts auto-relock timer.
func (s *Server) Unlock(password string) error {
	if err := s.checkPassword(password); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doUnlock()
}

// IsLocked returns the current lock state (safe for concurrent reads).
func (s *Server) IsLocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Locked
}

// Status returns a copy of key status fields.
func (s *Server) Status() StatusInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return StatusInfo{
		Locked:      s.state.Locked,
		LockedAt:    s.state.LockedAt,
		SecretCount: s.state.SecretCount(),
		Files:       s.state.Files(),
		Port:        s.cfg.Locker.Port,
	}
}

// UnlockDurationSeconds returns the configured auto-relock window.
func (s *Server) UnlockDurationSeconds() int {
	return s.cfg.Locker.UnlockDurationSeconds
}

// StatusInfo is the data returned by GET /api/status.
type StatusInfo struct {
	Locked      bool      `json:"locked"`
	LockedAt    time.Time `json:"locked_at,omitempty"`
	SecretCount int       `json:"secret_count"`
	Files       []string  `json:"files"`
	Port        string    `json:"port"`
}

// ---- internal (must be called with mu held) ----

func (s *Server) doLock() error {
	if s.state.Locked {
		return nil // idempotent
	}
	var journal func(map[string]map[string]string) error
	if s.cfg.Locker.Recoverable {
		// Saved BEFORE any file is touched, so a crash can never lose the real values.
		journal = func(m map[string]map[string]string) error {
			return locker.WriteJournal(s.journalPath(), s.encKey, s.salt, m)
		}
	}
	if err := locker.LockSecretsWith(s.cfg.Secret, s.workDir, s.state, journal); err != nil {
		return err
	}
	s.state.Locked = true
	s.state.LockedAt = time.Now()
	s.cancelRelockTimer()
	return nil
}

func (s *Server) doUnlock() error {
	if !s.state.Locked {
		return nil // idempotent
	}
	missing, err := locker.UnlockSecretsReport(s.workDir, s.state)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		log.Printf("loomlocker: %d locked value(s) were no longer in their files (edited while locked?): %s",
			len(missing), strings.Join(missing, ", "))
	}
	s.state.Clear()
	if s.cfg.Locker.Recoverable {
		_ = os.Remove(s.journalPath()) // real values are back in the files
	}
	s.startRelockTimer()
	return nil
}

func (s *Server) startRelockTimer() {
	s.cancelRelockTimer()
	dur := time.Duration(s.cfg.Locker.UnlockDurationSeconds) * time.Second
	s.relockTimer = time.AfterFunc(dur, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.doLock(); err != nil {
			// The secrets stay UNLOCKED; never fail silently.
			log.Printf("loomlocker: WARNING automatic re-lock failed, secrets are still unlocked: %v", err)
		}
	})
}

func (s *Server) cancelRelockTimer() {
	if s.relockTimer != nil {
		s.relockTimer.Stop()
		s.relockTimer = nil
	}
}

// ---- HTTP handlers ----

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"locked": s.IsLocked(),
		"port":   s.cfg.Locker.Port,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Status())
}

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request) {
	if err := s.Lock(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "locked",
		"count":  s.Status().SecretCount,
	})
}

func (s *Server) handleUnlock(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Password == "" {
		writeError(w, http.StatusBadRequest, "password required")
		return
	}
	if err := s.Unlock(body.Password); err != nil {
		var tooMany *TooManyAttemptsError
		if errors.As(err, &tooMany) {
			w.Header().Set("Retry-After", strconv.Itoa(int(tooMany.RetryAfter.Seconds())+1))
			writeError(w, http.StatusTooManyRequests, err.Error())
			return
		}
		if errors.Is(err, ErrInvalidPassword) {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "could not restore secrets: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "unlocked",
		"auto_relock_sec": s.cfg.Locker.UnlockDurationSeconds,
	})
}

func (s *Server) handleAutolock(w http.ResponseWriter, r *http.Request) {
	if err := s.Lock(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "locked"})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	locked := s.state.Locked
	s.mu.Unlock()

	if locked {
		var body struct {
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if err := s.checkPassword(body.Password); err != nil {
			var tooMany *TooManyAttemptsError
			if errors.As(err, &tooMany) {
				w.Header().Set("Retry-After", strconv.Itoa(int(tooMany.RetryAfter.Seconds())+1))
				writeError(w, http.StatusTooManyRequests, err.Error())
				return
			}
			writeError(w, http.StatusUnauthorized, "password required to stop while locked")
			return
		}
		s.mu.Lock()
		err := s.doUnlock()
		s.mu.Unlock()
		if err != nil {
			// Do NOT stop: the process still holds the only copy of the real values.
			writeError(w, http.StatusInternalServerError, "could not restore secrets, not stopping: "+err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
	// Shutdown in a goroutine so the response can be sent first.
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.Shutdown()
	}()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
