package db

import (
	"context"
	"fmt"
	"time"

	"github.com/avast/retry-go"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBWrapper wraps sqlc Querier with connection pool and retry logic
type DBWrapper struct {
	*Queries
	Pool *pgxpool.Pool
}

func NewDBWrapper(pool *pgxpool.Pool) *DBWrapper {
	return &DBWrapper{
		Queries: New(pool),
		Pool:    pool,
	}
}

// WithRetry executes a function with retry logic
func (db *DBWrapper) WithRetry(ctx context.Context, operation func() error) error {
	return retry.Do(
		operation,
		retry.Context(ctx),
		retry.Attempts(3),
		retry.Delay(500*time.Millisecond),
		retry.LastErrorOnly(true),
	)
}

func Connect(ctx context.Context, connStr string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database url: %w", err)
	}

	// Adjust pool settings if needed
	config.MaxConns = 10

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}
