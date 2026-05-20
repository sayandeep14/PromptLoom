package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool is the shared connection pool used by all store operations.
var Pool *pgxpool.Pool

// Connect initialises the connection pool from DATABASE_URL and verifies
// connectivity with a ping.
func Connect(ctx context.Context) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("database ping failed: %w", err)
	}
	Pool = pool
	return nil
}

// Close shuts down the connection pool gracefully.
func Close() {
	if Pool != nil {
		Pool.Close()
	}
}
