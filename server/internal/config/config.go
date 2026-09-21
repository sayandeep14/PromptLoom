// Package config loads and validates the registry server's environment.
package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// MinSecretLen is the shortest UPLOAD_SECRET the server accepts.
const MinSecretLen = 16

// AdminName is the identity of UPLOAD_SECRET holders.
const AdminName = "admin"

// Token is one accepted upload secret and the publisher it stands for.
type Token struct {
	Name   string
	Secret string
	Admin  bool
}

// Config holds all runtime settings derived from environment variables.
type Config struct {
	Port         string
	Tokens       []Token  // empty => write endpoints are disabled (fail closed)
	CORSOrigins  []string // empty => no CORS headers are sent; ["*"] => any origin
	MaxBodyBytes int64    // cap on a single request body
	ReadRPM      int      // per-IP read requests per minute
	WriteRPM     int      // per-IP write requests per minute
	TrustProxy   bool     // take the client IP from X-Forwarded-For
	AutoMigrate  bool     // apply the (idempotent) schema on startup
}

// Load reads the configuration from the environment and validates it.
func Load() (*Config, error) {
	c := &Config{
		Port:        getenv("PORT", "8080"),
		TrustProxy:  truthy(os.Getenv("TRUST_PROXY")),
		AutoMigrate: truthy(os.Getenv("AUTO_MIGRATE")),
	}

	for _, o := range strings.Split(os.Getenv("CORS_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.CORSOrigins = append(c.CORSOrigins, o)
		}
	}

	var err error
	if c.MaxBodyBytes, err = getInt64("MAX_BODY_BYTES", 8<<20); err != nil {
		return nil, err
	}
	readRPM, err := getInt64("RATE_LIMIT_READ_PER_MIN", 120)
	if err != nil {
		return nil, err
	}
	writeRPM, err := getInt64("RATE_LIMIT_WRITE_PER_MIN", 10)
	if err != nil {
		return nil, err
	}
	c.ReadRPM, c.WriteRPM = int(readRPM), int(writeRPM)

	if c.MaxBodyBytes <= 0 || c.ReadRPM <= 0 || c.WriteRPM <= 0 {
		return nil, fmt.Errorf("MAX_BODY_BYTES, RATE_LIMIT_READ_PER_MIN and RATE_LIMIT_WRITE_PER_MIN must be positive")
	}
	if c.Tokens, err = parseTokens(os.Getenv("UPLOAD_SECRET"), os.Getenv("UPLOAD_TOKENS")); err != nil {
		return nil, err
	}
	return c, nil
}

// nameRe is what a publisher name may look like: it ends up in the database and in logs.
var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// parseTokens reads the two ways of configuring who may write:
//
//	UPLOAD_SECRET  one or more comma-separated secrets for the admin identity (several at once
//	               lets you rotate: add the new secret, switch clients, then remove the old one)
//	UPLOAD_TOKENS  name=secret,name=secret — one publisher identity per entry; a name may appear
//	               more than once for the same reason
//
// A secret can belong to one identity only, or the request would be ambiguous.
func parseTokens(adminSecrets, publishers string) ([]Token, error) {
	var out []Token
	seen := map[string]string{}
	add := func(name, secret string, admin bool) error {
		if len(secret) < MinSecretLen {
			return fmt.Errorf("the secret for %q is too short (%d chars, need at least %d) — generate one with: openssl rand -hex 32",
				name, len(secret), MinSecretLen)
		}
		if other, dup := seen[secret]; dup && other != name {
			return fmt.Errorf("the same secret is configured for both %q and %q", other, name)
		}
		seen[secret] = name
		out = append(out, Token{Name: name, Secret: secret, Admin: admin})
		return nil
	}

	for _, s := range strings.Split(adminSecrets, ",") {
		if s = strings.TrimSpace(s); s != "" {
			if err := add(AdminName, s, true); err != nil {
				return nil, fmt.Errorf("UPLOAD_SECRET: %w", err)
			}
		}
	}
	for _, entry := range strings.Split(publishers, ",") {
		if entry = strings.TrimSpace(entry); entry == "" {
			continue
		}
		name, secret, ok := strings.Cut(entry, "=")
		name, secret = strings.TrimSpace(name), strings.TrimSpace(secret)
		if !ok || !nameRe.MatchString(name) {
			return nil, fmt.Errorf("UPLOAD_TOKENS: %q is not name=secret (names are lowercase letters, digits, . _ -)", redactEntry(entry))
		}
		if name == AdminName {
			return nil, fmt.Errorf("UPLOAD_TOKENS: %q is reserved for UPLOAD_SECRET", AdminName)
		}
		if err := add(name, secret, false); err != nil {
			return nil, fmt.Errorf("UPLOAD_TOKENS: %w", err)
		}
	}
	return out, nil
}

// redactEntry keeps a configuration error from echoing a secret back into the logs.
func redactEntry(entry string) string {
	if name, _, ok := strings.Cut(entry, "="); ok {
		return name + "=…"
	}
	return "(no '=')"
}

func truthy(v string) bool { return v == "1" || strings.EqualFold(v, "true") }

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt64(key string, def int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", key, v)
	}
	return n, nil
}
