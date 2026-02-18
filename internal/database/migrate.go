package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrationStatus represents the status of a single migration.
type MigrationStatus struct {
	Version string
	Status  string // "applied" or "pending"
}

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrator handles database migrations.
type Migrator struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewMigrator creates a new Migrator instance.
func NewMigrator(pool *pgxpool.Pool, log *slog.Logger) *Migrator {
	return &Migrator{
		pool: pool,
		log:  log,
	}
}

// migration represents a single migration file.
type migration struct {
	version string
	up      string
	down    string
}

// loadMigrations reads all migration files from the embedded filesystem.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// Group files by version
	migrationsMap := make(map[string]*migration)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		name := entry.Name()
		// Expected format: 000001_name.up.sql or 000001_name.down.sql
		// Extract version (first 6 digits) and direction (up/down before .sql)
		if len(name) < 17 { // minimum: 000001_x.up.sql
			continue
		}

		version := name[:6]

		// Find direction: look for .up.sql or .down.sql at the end
		var direction string
		if strings.HasSuffix(name, ".up.sql") {
			direction = "up"
		} else if strings.HasSuffix(name, ".down.sql") {
			direction = "down"
		} else {
			continue
		}

		if _, ok := migrationsMap[version]; !ok {
			migrationsMap[version] = &migration{version: version}
		}

		content, err := migrationFiles.ReadFile(filepath.Join("migrations", name))
		if err != nil {
			return nil, fmt.Errorf("failed to read migration file %s: %w", name, err)
		}

		switch direction {
		case "up":
			migrationsMap[version].up = string(content)
		case "down":
			migrationsMap[version].down = string(content)
		}
	}

	// Convert map to sorted slice
	migrations := make([]migration, 0, len(migrationsMap))
	for _, m := range migrationsMap {
		if m.up == "" {
			return nil, fmt.Errorf("migration %s is missing .up.sql file", m.version)
		}
		migrations = append(migrations, *m)
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	return migrations, nil
}

// ensureMigrationsTable creates the schema_migrations table if it doesn't exist.
func (m *Migrator) ensureMigrationsTable(ctx context.Context) error {
	// Check if table exists
	query := `
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'schema_migrations'
		)
	`
	var exists bool
	err := m.pool.QueryRow(ctx, query).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check schema_migrations table: %w", err)
	}

	if !exists {
		// Create new table with our schema
		createQuery := `
			CREATE TABLE schema_migrations (
				version VARCHAR(255) PRIMARY KEY,
				applied_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
			)
		`
		_, err := m.pool.Exec(ctx, createQuery)
		if err != nil {
			return fmt.Errorf("failed to create schema_migrations table: %w", err)
		}
	}

	return nil
}

// getAppliedMigrations returns a set of already applied migration versions.
func (m *Migrator) getAppliedMigrations(ctx context.Context) (map[string]bool, error) {
	// Try to get column info to determine schema type
	query := `
		SELECT data_type FROM information_schema.columns 
		WHERE table_name = 'schema_migrations' AND column_name = 'version'
	`
	var dataType string
	err := m.pool.QueryRow(ctx, query).Scan(&dataType)
	if err != nil {
		// Table might not exist or other error - return empty map
		return make(map[string]bool), nil
	}

	applied := make(map[string]bool)

	if dataType == "bigint" {
		// Existing golang-migrate schema - convert bigint versions to our format
		rows, err := m.pool.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
		if err != nil {
			return nil, fmt.Errorf("failed to query applied migrations: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var version int64
			if err := rows.Scan(&version); err != nil {
				return nil, fmt.Errorf("failed to scan migration version: %w", err)
			}
			// Convert bigint version to our format (e.g., 1 -> "000001")
			applied[fmt.Sprintf("%06d", version)] = true
		}

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("error iterating migration rows: %w", err)
		}
	} else {
		// Our VARCHAR schema
		rows, err := m.pool.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
		if err != nil {
			return nil, fmt.Errorf("failed to query applied migrations: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var version string
			if err := rows.Scan(&version); err != nil {
				return nil, fmt.Errorf("failed to scan migration version: %w", err)
			}
			applied[version] = true
		}

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("error iterating migration rows: %w", err)
		}
	}

	return applied, nil
}

// Apply runs all pending migrations.
func (m *Migrator) Apply(ctx context.Context) error {
	m.log.Info("starting database migrations")

	if err := m.ensureMigrationsTable(ctx); err != nil {
		return err
	}

	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		return err
	}

	// Filter pending migrations
	var pending []migration
	for _, mig := range migrations {
		if !applied[mig.version] {
			pending = append(pending, mig)
		}
	}

	if len(pending) == 0 {
		m.log.Info("no pending migrations")
		return nil
	}

	m.log.Info("found pending migrations", "count", len(pending))

	// Apply migrations in a transaction
	for _, mig := range pending {
		if err := m.applyMigration(ctx, &mig); err != nil {
			return fmt.Errorf("failed to apply migration %s: %w", mig.version, err)
		}
		m.log.Info("applied migration", "version", mig.version)
	}

	m.log.Info("all migrations applied successfully")
	return nil
}

// applyMigration applies a single migration in a transaction.
func (m *Migrator) applyMigration(ctx context.Context, mig *migration) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Execute the up migration
	_, err = tx.Exec(ctx, mig.up)
	if err != nil {
		return fmt.Errorf("failed to execute migration %s: %w", mig.version, err)
	}

	// Record the migration as applied - handle both schema types
	// Try VARCHAR schema first, then bigint schema
	_, err = tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, mig.version)
	if err != nil {
		// Try bigint schema (convert version to int)
		versionNum := strings.TrimLeft(mig.version, "0")
		if versionNum == "" {
			versionNum = "0"
		}
		_, err = tx.Exec(ctx, `INSERT INTO schema_migrations (version, dirty) VALUES ($1, false)`, versionNum)
		if err != nil {
			return fmt.Errorf("failed to record migration %s: %w", mig.version, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit migration %s: %w", mig.version, err)
	}

	return nil
}

// Rollback rolls back the last applied migration.
func (m *Migrator) Rollback(ctx context.Context) error {
	m.log.Info("rolling back last migration")

	if err := m.ensureMigrationsTable(ctx); err != nil {
		return err
	}

	// Get the last applied migration - handle both schema types
	var version string
	var versionInt int64
	query := `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`
	err := m.pool.QueryRow(ctx, query).Scan(&version)
	if err != nil {
		// Try bigint schema
		queryInt := `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`
		err = m.pool.QueryRow(ctx, queryInt).Scan(&versionInt)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				m.log.Info("no migrations to rollback")
				return nil
			}
			return fmt.Errorf("failed to get last migration: %w", err)
		}
		version = fmt.Sprintf("%06d", versionInt)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	var target *migration
	for i := range migrations {
		if migrations[i].version == version {
			target = &migrations[i]
			break
		}
	}

	if target == nil {
		return fmt.Errorf("migration %s not found in embedded files", version)
	}

	if target.down == "" {
		return fmt.Errorf("migration %s has no down migration", version)
	}

	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Execute the down migration
	_, err = tx.Exec(ctx, target.down)
	if err != nil {
		return fmt.Errorf("failed to execute down migration %s: %w", version, err)
	}

	// Remove the migration record - handle both schema types
	_, err = tx.Exec(ctx, `DELETE FROM schema_migrations WHERE version = $1`, version)
	if err != nil {
		// Try bigint schema
		versionNum := strings.TrimLeft(version, "0")
		if versionNum == "" {
			versionNum = "0"
		}
		_, err = tx.Exec(ctx, `DELETE FROM schema_migrations WHERE version = $1`, versionNum)
		if err != nil {
			return fmt.Errorf("failed to remove migration record %s: %w", version, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit rollback %s: %w", version, err)
	}

	m.log.Info("rolled back migration", "version", version)
	return nil
}

// Status returns the current migration status.
func (m *Migrator) Status(ctx context.Context) ([]MigrationStatus, error) {
	if err := m.ensureMigrationsTable(ctx); err != nil {
		return nil, err
	}

	migrations, err := loadMigrations()
	if err != nil {
		return nil, fmt.Errorf("failed to load migrations: %w", err)
	}

	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		return nil, err
	}

	statuses := make([]MigrationStatus, 0, len(migrations))
	for _, mig := range migrations {
		status := MigrationStatus{Version: mig.version, Status: "pending"}
		if applied[mig.version] {
			status.Status = "applied"
		}
		statuses = append(statuses, status)
	}

	return statuses, nil
}

// Run executes a migration command: apply, rollback, or status.
// Caller must close the pool. Exactly one of apply, rollback, status should be true.
func Run(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, apply, rollback, status bool) error {
	migrator := NewMigrator(pool, log)
	switch {
	case apply:
		log.Info("applying migrations")
		return migrator.Apply(ctx)
	case rollback:
		log.Info("rolling back migration")
		return migrator.Rollback(ctx)
	case status:
		return migrator.PrintStatus(ctx)
	default:
		return nil
	}
}

// PrintStatus prints the migration status to stdout.
func (m *Migrator) PrintStatus(ctx context.Context) error {
	statuses, err := m.Status(ctx)
	if err != nil {
		return err
	}

	fmt.Println("Migration Status:")
	fmt.Println(strings.Repeat("=", 50))
	for _, s := range statuses {
		icon := "○"
		if s.Status == "applied" {
			icon = "●"
		}
		fmt.Printf("  %s %s: %s\n", icon, s.Version, s.Status)
	}
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("Total: %d migrations", len(statuses))
	appliedCount := 0
	for _, s := range statuses {
		if s.Status == "applied" {
			appliedCount++
		}
	}
	fmt.Printf(" (%d applied, %d pending)\n", appliedCount, len(statuses)-appliedCount)

	return nil
}
