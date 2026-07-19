// Package common holds shared infrastructure used by every module:
// the database pool, JSON helpers, typed errors and HTTP middleware.
package common

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool opens a pgx connection pool and verifies connectivity with a ping.
// A pool (not a single connection) is used so concurrent HTTP requests each
// borrow their own connection without contention.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// RunMigrations applies every SQL statement block passed in, in order.
// The SQL is written to be idempotent (IF NOT EXISTS), so it is safe to run
// on every boot — a pragmatic approach for a capstone without a migration CLI.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, statements ...string) error {
	for i, sql := range statements {
		if _, err := pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("apply migration #%d: %w", i+1, err)
		}
	}
	return nil
}
