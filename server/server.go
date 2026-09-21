package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sayandeep14/PromptLoom/server/internal/config"
	"github.com/sayandeep14/PromptLoom/server/internal/handlers"
	mw "github.com/sayandeep14/PromptLoom/server/internal/middleware"
)

// newRouter wires every route with its protections:
//
//	reads : rate limit
//	writes: rate limit (stricter) → upload secret (identifies the publisher) → body-size cap
//
// Every request is access-logged (no headers, queries or bodies) and HTTPS requests get HSTS.
//
// The rate limit sits before authentication so brute-forcing the secret is throttled.
func newRouter(cfg *config.Config, st handlers.Store) http.Handler {
	api := handlers.New(st, mw.IdentityFrom)
	mux := http.NewServeMux()

	readLimit := mw.RateLimit(mw.NewLimiter(cfg.ReadRPM), cfg.TrustProxy)
	writeLimit := mw.RateLimit(mw.NewLimiter(cfg.WriteRPM), cfg.TrustProxy)
	creds := make([]mw.Credential, len(cfg.Tokens))
	for i, t := range cfg.Tokens {
		creds[i] = mw.Credential{Name: t.Name, Secret: t.Secret, Admin: t.Admin}
	}
	requireSecret := mw.Authenticate(creds)
	maxBody := mw.MaxBody(cfg.MaxBodyBytes)

	read := func(h http.HandlerFunc) http.Handler { return mw.Chain(h, readLimit) }
	write := func(h http.HandlerFunc) http.Handler {
		return mw.Chain(h, writeLimit, requireSecret, maxBody)
	}

	mux.Handle("GET /api/v1/vaults", read(api.ListVaults))
	mux.Handle("GET /api/v1/vaults/{slug}", read(api.GetVault))
	mux.Handle("GET /api/v1/vaults/{slug}/bundle", read(api.GetBundle))
	mux.Handle("POST /api/v1/vaults", write(api.UploadVault))
	mux.Handle("DELETE /api/v1/vaults/{slug}", write(api.DeleteVault))

	// Health check — deliberately outside the rate limiter so probes never get throttled.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	return mw.Chain(mux, mw.AccessLog(log.Printf, cfg.TrustProxy), mw.SecurityHeaders(cfg.TrustProxy), mw.CORS(cfg.CORSOrigins))
}

func startServer(cfg *config.Config, st handlers.Store) error {
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newRouter(cfg, st),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	errc := make(chan error, 1)
	go func() {
		log.Printf("PromptLoom registry listening on %s", srv.Addr)
		errc <- srv.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case sig := <-stop:
		log.Printf("received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}
