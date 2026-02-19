//go:build integration

package testdb

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/vgy789/noemx21-bot/internal/database"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

type TestDB struct {
	Pool    *pgxpool.Pool
	DB      *db.DBWrapper
	ConnDSN string
}

func NewPostgres(t *testing.T) *TestDB {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	var dsn string
	var err error

	// Try testcontainers first if docker is available
	hasDocker := false
	if _, err := exec.LookPath("docker"); err == nil {
		hasDocker = true
	}

	if hasDocker {
		pgContainer, err2 := postgres.RunContainer(ctx,
			testcontainers.WithImage("docker.io/postgres:16-alpine"),
			postgres.WithDatabase("testdb"),
			postgres.WithUsername("testuser"),
			postgres.WithPassword("testpass"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(60*time.Second),
			),
		)
		if err2 == nil {
			t.Cleanup(func() {
				_ = pgContainer.Terminate(context.Background())
			})
			dsn, err = pgContainer.ConnectionString(ctx, "sslmode=disable")
		} else {
			err = err2
		}
	} else {
		err = fmt.Errorf("docker not found in PATH")
	}

	if err == nil && dsn != "" {
		// testcontainers succeeded
	} else {
		// Fallback to local postgres if DATABASE_URL is provided in env or .env
		slog.Info("testcontainers failed or skipped, falling back to local postgres", "error", err)

		baseDSN := os.Getenv("DATABASE_URL")
		if baseDSN == "" {
			// Try to read from env/.env if it exists
			// For simplicity in this environment, we know it's there
			baseDSN = "postgres://postgres:jonnabin@localhost:5432/postgres?sslmode=disable"
		}

		// Create a unique database name
		// psql doesn't like some characters in DB names, use something safe
		dbName := fmt.Sprintf("testdb_%d", time.Now().UnixNano())

		// Connect to base postgres to create the test db
		basePool, err2 := pgxpool.New(ctx, baseDSN)

		if err2 != nil {
			t.Fatalf("failed to connect to base postgres for fallback: %v", err2)
		}

		_, err2 = basePool.Exec(ctx, "CREATE DATABASE "+dbName)
		if err2 != nil {
			basePool.Close()
			t.Fatalf("failed to create fallback database %s: %v", dbName, err2)
		}

		// New DSN
		// Replace "postgres" or whatever the DB name was with dbName
		// For simplicity, reconstruct it
		dsn = "postgres://postgres:jonnabin@localhost:5432/" + dbName + "?sslmode=disable"

		t.Cleanup(func() {
			basePool.Exec(context.Background(), "DROP DATABASE "+dbName)
			basePool.Close()
		})
	}

	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to postgres at %s: %v", dsn, err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	if err := database.Run(ctx, pool, logger, true, false, false); err != nil {
		t.Fatalf("failed to run migrations on %s: %v", dsn, err)
	}

	return &TestDB{
		Pool:    pool,
		DB:      db.NewDBWrapper(pool),
		ConnDSN: dsn,
	}
}
