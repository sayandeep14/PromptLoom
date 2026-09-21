package db

import (
	"context"
	_ "embed"
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

//go:embed schema.sql
var schemaSQL string

// migrationLockID is an arbitrary key for the Postgres advisory lock that
// serialises concurrent migrations (e.g. several replicas starting together).
const migrationLockID = 7263001

// Migrate applies the embedded schema. It is idempotent (every statement is
// IF NOT EXISTS / OR REPLACE), so it is safe to run on every start.
func Migrate(ctx context.Context) error {
	if Pool == nil {
		return fmt.Errorf("database is not connected")
	}
	conn, err := Pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("take migration lock: %w", err)
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockID) //nolint:errcheck

	if _, err := conn.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}
