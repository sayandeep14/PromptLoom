package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/sayandeepgiri/promptloom/server/internal/config"
	"github.com/sayandeepgiri/promptloom/server/internal/db"
	"github.com/sayandeepgiri/promptloom/server/internal/store"
)

func main() {
	// `registry healthcheck` lets a container probe itself without curl (distroless image).
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

	// Load .env if present (ignored in production where real env vars are set).
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("warn: could not load .env: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.UploadSecret == "" {
		log.Printf("warn: UPLOAD_SECRET is not set — publish and delete are DISABLED (read-only registry)")
	}
	if len(cfg.CORSOrigins) == 1 && cfg.CORSOrigins[0] == "*" {
		log.Printf("warn: CORS_ORIGINS=* allows any website to call this API from a browser")
	}

	ctx := context.Background()
	if err := db.Connect(ctx); err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	if cfg.AutoMigrate {
		if err := db.Migrate(ctx); err != nil {
			log.Fatalf("migrate: %v", err)
		}
		log.Printf("database schema is up to date")
	}

	if err := startServer(cfg, store.PG{}); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// healthcheck GETs the local /healthz and returns a process exit code.
func healthcheck() int {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
