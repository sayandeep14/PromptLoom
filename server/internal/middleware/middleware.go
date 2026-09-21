// Package middleware holds the registry's HTTP hardening layers: authentication,
// rate limiting, request-size limits, CORS and response headers.
package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
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

// RequireSecret guards write endpoints. It fails closed: when no secret is
// configured every request is refused with 503 rather than allowed through.
// The comparison is constant-time and length-independent (both sides are
// hashed first) and, unlike the old implementation, case-sensitive.
func RequireSecret(secret string) Middleware {
	want := sha256.Sum256([]byte(secret))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if secret == "" {
				writeError(w, http.StatusServiceUnavailable,
					"writes are disabled: the server has no UPLOAD_SECRET configured")
				return
			}
			got := sha256.Sum256([]byte(r.Header.Get(SecretHeader)))
			if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
				w.Header().Set("WWW-Authenticate", `X-Upload-Secret realm="promptloom-registry"`)
				writeError(w, http.StatusUnauthorized, "missing or invalid "+SecretHeader)
				return
			}
			next.ServeHTTP(w, r)
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

// SecurityHeaders adds conservative response headers to every reply.
func SecurityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Cache-Control", "no-store")
			h.Set("Referrer-Policy", "no-referrer")
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
