package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
	pwdHash     []byte         // bcrypt hash of session password
	encKey      []byte         // AES-256 key (random or argon2id-derived)
	relockTimer *time.Timer
	mu          sync.Mutex
	http        *http.Server
	done        chan struct{}   // closed when Shutdown is called
}

// New creates a Server. password is the plain-text password entered at start.
func New(cfg *config.Config, workDir, password string) (*Server, error) {
	hash, err := icrypto.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	var encKey []byte
	if cfg.Locker.Recoverable {
		// Key derived from password — used to encrypt mapping to disk.
		key, _, err := icrypto.DeriveKey(password, nil)
		if err != nil {
			return nil, fmt.Errorf("derive key: %w", err)
		}
		encKey = key
	} else {
		key, err := icrypto.RandomKey()
		if err != nil {
			return nil, fmt.Errorf("random key: %w", err)
		}
		encKey = key
	}
	_ = encKey // used in future recoverable mode persistence

	return &Server{
		cfg:     cfg,
		workDir: workDir,
		state:   locker.NewState(),
		pwdHash: hash,
		encKey:  encKey,
		done:    make(chan struct{}),
	}, nil
}

// Done returns a channel closed when the server shuts down.
func (s *Server) Done() <-chan struct{} { return s.done }

// Start begins listening on the configured port. Non-blocking (runs in goroutine).
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/ping", s.handlePing)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/lock", s.handleLock)
	mux.HandleFunc("POST /api/unlock", s.handleUnlock)
	mux.HandleFunc("POST /api/autolock", s.handleAutolock)
	mux.HandleFunc("POST /api/stop", s.handleStop)

	addr := ":" + s.cfg.Locker.Port
	s.http = &http.Server{Addr: addr, Handler: mux}

	go func() {
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("loomlocker server error: %v", err)
		}
	}()
	return nil
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

// Lock locks all secrets. Does not require a password.
func (s *Server) Lock() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doLock()
}

// Unlock verifies password, restores secrets, starts auto-relock timer.
func (s *Server) Unlock(password string) error {
	if !s.VerifyPassword(password) {
		return fmt.Errorf("invalid password")
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
	if err := locker.LockSecrets(s.cfg.Secret, s.workDir, s.state); err != nil {
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
	if err := locker.UnlockSecrets(s.cfg.Secret, s.workDir, s.state); err != nil {
		return err
	}
	s.state.Clear()
	s.startRelockTimer()
	return nil
}

func (s *Server) startRelockTimer() {
	s.cancelRelockTimer()
	dur := time.Duration(s.cfg.Locker.UnlockDurationSeconds) * time.Second
	s.relockTimer = time.AfterFunc(dur, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		_ = s.doLock()
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
		writeError(w, http.StatusUnauthorized, err.Error())
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
		if !s.VerifyPassword(body.Password) {
			writeError(w, http.StatusUnauthorized, "password required to stop while locked")
			return
		}
		s.mu.Lock()
		_ = s.doUnlock()
		s.mu.Unlock()
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
