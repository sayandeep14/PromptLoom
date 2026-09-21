// Package middleware holds the registry's HTTP hardening layers: authentication,
// rate limiting, request-size limits, CORS and response headers.
package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sayandeep14/PromptLoom/server/internal/models"
)

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares so the first one listed is the outermost.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// SecretHeader is the request header carrying the upload secret.
const SecretHeader = "X-Upload-Secret"

// Credential is one accepted secret and who it identifies.
type Credential struct {
	Name   string
	Secret string
	Admin  bool
}

type ctxKey int

const (
	identityKey ctxKey = iota
	logKey
)

// IdentityFrom returns who authenticated the request (zero value when nobody did).
func IdentityFrom(r *http.Request) models.Identity {
	id, _ := r.Context().Value(identityKey).(models.Identity)
	return id
}

// Authenticate guards write endpoints and records WHO is calling. It fails closed: with no
// credentials configured every request is refused with 503 rather than allowed through.
//
// Every configured secret is compared, in constant time, with no early exit, so how long a
// check takes reveals neither which secret was nearest nor how many are configured. Several
// secrets may be valid at once (rotation). Comparison is on SHA-256 digests, so length does
// not leak either, and it is case-sensitive.
func Authenticate(creds []Credential) Middleware {
	type entry struct {
		digest [sha256.Size]byte
		id     models.Identity
	}
	entries := make([]entry, len(creds))
	for i, c := range creds {
		entries[i] = entry{sha256.Sum256([]byte(c.Secret)), models.Identity{Name: c.Name, Admin: c.Admin}}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(entries) == 0 {
				writeError(w, http.StatusServiceUnavailable,
					"writes are disabled: the server has no UPLOAD_SECRET or UPLOAD_TOKENS configured")
				return
			}
			got := sha256.Sum256([]byte(r.Header.Get(SecretHeader)))
			var who models.Identity
			matched := 0
			for _, e := range entries {
				m := subtle.ConstantTimeCompare(got[:], e.digest[:])
				matched |= m
				if m == 1 {
					who = e.id
				}
			}
			if matched != 1 {
				w.Header().Set("WWW-Authenticate", `X-Upload-Secret realm="promptloom-registry"`)
				writeError(w, http.StatusUnauthorized, "missing or invalid "+SecretHeader)
				return
			}
			ctx := context.WithValue(r.Context(), identityKey, who)
			if holder, ok := r.Context().Value(logKey).(*accessRecord); ok {
				holder.identity = who.Name // so the access log can say who, without ever seeing a secret
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireSecret is Authenticate for a single admin secret.
func RequireSecret(secret string) Middleware {
	if secret == "" {
		return Authenticate(nil)
	}
	return Authenticate([]Credential{{Name: "admin", Secret: secret, Admin: true}})
}

type accessRecord struct{ identity string }

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// AccessLog writes one line per request: client, identity, method, path, status, size, time.
// It never logs headers, query strings or bodies, so a secret cannot end up in the log, and the
// path is quoted so a hostile URL cannot forge extra log lines.
func AccessLog(logf func(format string, args ...any), trustProxy bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &accessRecord{}
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), logKey, rec)))
			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			who := rec.identity
			if who == "" {
				who = "-"
			}
			logf("access ip=%s who=%s %s %q status=%d bytes=%d dur=%s",
				ClientIP(r, trustProxy), who, r.Method, r.URL.Path, status, sw.bytes, time.Since(start).Round(time.Millisecond))
		})
	}
}

// MaxBody caps the request body. Handlers should treat *http.MaxBytesError as 413.
func MaxBody(n int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > n {
				writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// lastForwarded is the last value of a comma-separated forwarding header (the one the nearest
// proxy appended).
func lastForwarded(v string) string {
	parts := strings.Split(v, ",")
	return strings.TrimSpace(parts[len(parts)-1])
}

// ClientIP returns the caller's IP. With trustProxy it uses the last entry of
// X-Forwarded-For (the one appended by the nearest proxy); otherwise the socket peer.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// RateLimit rejects callers that exceed the limiter's per-IP budget with 429.
func RateLimit(l *Limiter, trustProxy bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, retry := l.Allow(ClientIP(r, trustProxy))
			if !ok {
				secs := int(retry.Seconds()) + 1
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded, retry in "+strconv.Itoa(secs)+"s")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders adds conservative response headers to every reply, and HSTS when the request
// arrived over HTTPS (directly, or via a trusted proxy that says so in X-Forwarded-Proto).
func SecurityHeaders(trustProxy bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Cache-Control", "no-store")
			h.Set("Referrer-Policy", "no-referrer")
			if r.TLS != nil || (trustProxy && strings.EqualFold(lastForwarded(r.Header.Get("X-Forwarded-Proto")), "https")) {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CORS emits CORS headers only for allowed origins. With no origins configured
// (the default) no CORS headers are sent at all: the CLI does not need them and
// browsers are denied cross-origin access.
func CORS(origins []string) Middleware {
	allowAll := false
	allowed := map[string]bool{}
	for _, o := range origins {
		if o == "*" {
			allowAll = true
		}
		allowed[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (allowAll || allowed[origin]) {
				h := w.Header()
				if allowAll {
					h.Set("Access-Control-Allow-Origin", "*")
				} else {
					h.Set("Access-Control-Allow-Origin", origin)
					h.Add("Vary", "Origin")
				}
				h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type, "+SecretHeader)
				h.Set("Access-Control-Max-Age", "600")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
