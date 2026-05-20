package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/sayandeepgiri/promptloom/server/internal/handlers"
)

func newRouter() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/vaults", handlers.ListVaults)
	mux.HandleFunc("GET /api/v1/vaults/{slug}", handlers.GetVault)
	mux.HandleFunc("GET /api/v1/vaults/{slug}/bundle", handlers.GetBundle)
	mux.HandleFunc("POST /api/v1/vaults", handlers.UploadVault)
	mux.HandleFunc("DELETE /api/v1/vaults/{slug}", handlers.DeleteVault)

	// Health check.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	return corsMiddleware(mux)
}

// corsMiddleware adds CORS headers based on CORS_ORIGINS env var.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origins := os.Getenv("CORS_ORIGINS")
		if origins == "" {
			origins = "*"
		}
		origin := r.Header.Get("Origin")
		if origins == "*" || containsOrigin(origins, origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Upload-Secret")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func containsOrigin(allowed, origin string) bool {
	for _, o := range strings.Split(allowed, ",") {
		if strings.TrimSpace(o) == origin {
			return true
		}
	}
	return false
}

func startServer() error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Printf("PromptLoom registry listening on %s", addr)
	return http.ListenAndServe(addr, newRouter())
}
