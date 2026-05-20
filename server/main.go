package main

import (
	"context"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/sayandeepgiri/promptloom/server/internal/db"
)

func main() {
	// Load .env if present (ignored in production where real env vars are set).
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("warn: could not load .env: %v", err)
	}

	ctx := context.Background()
	if err := db.Connect(ctx); err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	if err := startServer(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
