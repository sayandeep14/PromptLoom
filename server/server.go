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

	"github.com/sayandeepgiri/promptloom/server/internal/config"
	"github.com/sayandeepgiri/promptloom/server/internal/handlers"
	mw "github.com/sayandeepgiri/promptloom/server/internal/middleware"
)

// newRouter wires every route with its protections:
//
//	reads : rate limit
//	writes: rate limit (stricter) → upload secret → body-size cap
//
// The rate limit sits before authentication so brute-forcing the secret is throttled.
func newRouter(cfg *config.Config) http.Handler {
	mux := http.NewServeMux()

	readLimit := mw.RateLimit(mw.NewLimiter(cfg.ReadRPM), cfg.TrustProxy)
	writeLimit := mw.RateLimit(mw.NewLimiter(cfg.WriteRPM), cfg.TrustProxy)
	requireSecret := mw.RequireSecret(cfg.UploadSecret)
	maxBody := mw.MaxBody(cfg.MaxBodyBytes)

	read := func(h http.HandlerFunc) http.Handler { return mw.Chain(h, readLimit) }
	write := func(h http.HandlerFunc) http.Handler {
		return mw.Chain(h, writeLimit, requireSecret, maxBody)
	}

	mux.Handle("GET /api/v1/vaults", read(handlers.ListVaults))
	mux.Handle("GET /api/v1/vaults/{slug}", read(handlers.GetVault))
	mux.Handle("GET /api/v1/vaults/{slug}/bundle", read(handlers.GetBundle))
	mux.Handle("POST /api/v1/vaults", write(handlers.UploadVault))
	mux.Handle("DELETE /api/v1/vaults/{slug}", write(handlers.DeleteVault))

	// Health check — deliberately outside the rate limiter so probes never get throttled.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	return mw.Chain(mux, mw.SecurityHeaders(), mw.CORS(cfg.CORSOrigins))
}

func startServer(cfg *config.Config) error {
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newRouter(cfg),
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
