package main

import (
	"context"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/sayandeepgiri/promptloom/server/internal/config"
	"github.com/sayandeepgiri/promptloom/server/internal/db"
)

func main() {
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

	if err := startServer(cfg); err != nil {
		log.Fatalf("server: %v", err)
	}
}
