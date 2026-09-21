// Package config loads and validates the registry server's environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// MinSecretLen is the shortest UPLOAD_SECRET the server accepts.
const MinSecretLen = 16

// Config holds all runtime settings derived from environment variables.
type Config struct {
	Port         string
	UploadSecret string   // empty => write endpoints are disabled (fail closed)
	CORSOrigins  []string // empty => no CORS headers are sent; ["*"] => any origin
	MaxBodyBytes int64    // cap on a single request body
	ReadRPM      int      // per-IP read requests per minute
	WriteRPM     int      // per-IP write requests per minute
	TrustProxy   bool     // take the client IP from X-Forwarded-For
}

// Load reads the configuration from the environment and validates it.
func Load() (*Config, error) {
	c := &Config{
		Port:         getenv("PORT", "8080"),
		UploadSecret: os.Getenv("UPLOAD_SECRET"),
		TrustProxy:   os.Getenv("TRUST_PROXY") == "1" || strings.EqualFold(os.Getenv("TRUST_PROXY"), "true"),
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
	if c.UploadSecret != "" && len(c.UploadSecret) < MinSecretLen {
		return nil, fmt.Errorf("UPLOAD_SECRET is too short (%d chars, need at least %d) — generate one with: openssl rand -hex 32",
			len(c.UploadSecret), MinSecretLen)
	}
	return c, nil
}

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
