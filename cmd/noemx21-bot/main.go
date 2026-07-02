package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/initialization"
	"github.com/vgy789/noemx21-bot/internal/service/membertagimport"
)

func main() {
	// Parse command-line flags
	migrate := flag.Bool("migrate", false, "Apply database migrations and exit")
	migrateRollback := flag.Bool("migrate-rollback", false, "Rollback the last migration and exit")
	migrateStatus := flag.Bool("migrate-status", false, "Show migration status and exit")
	importMemberTags := flag.String("import-member-tags", "", "Import legacy member-tag mappings from JSON")
	dryRun := flag.Bool("dry-run", false, "Validate an import without changing the database")
	applyImport := flag.Bool("apply", false, "Apply a validated member-tag import")
	suppressMemberTagID := flag.Int64("suppress-member-tag-id", 0, "Suppress legacy member tags by Telegram user ID")
	suppressMemberTagLogin := flag.String("suppress-member-tag-login", "", "Suppress legacy member tags by School 21 login")
	suppressionReason := flag.String("suppression-reason", "administrator_request", "Non-personal suppression reason")
	flag.Parse()

	if strings.TrimSpace(*importMemberTags) != "" && *dryRun {
		if *applyImport {
			fmt.Fprintln(os.Stderr, "Choose exactly one of -dry-run or -apply")
			os.Exit(2)
		}
		if err := runMemberTagImportDryRun(*importMemberTags); err != nil {
			fmt.Fprintf(os.Stderr, "Member-tag import validation failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if strings.TrimSpace(*importMemberTags) == "" && (*dryRun || *applyImport) {
		fmt.Fprintln(os.Stderr, "-dry-run and -apply require -import-member-tags")
		os.Exit(2)
	}

	cfg := config.MustLoad()
	log := initialization.SetupLogger(cfg.Production, cfg.LogLevel)

	ctx := context.Background()

	builder := initialization.NewBuilder().
		WithContext(ctx).
		WithConfig(cfg).
		WithLogger(log)

	if strings.TrimSpace(*importMemberTags) != "" || *suppressMemberTagID > 0 || strings.TrimSpace(*suppressMemberTagLogin) != "" {
		if strings.TrimSpace(*importMemberTags) != "" && !*applyImport {
			fmt.Fprintln(os.Stderr, "Member-tag import requires exactly one of -dry-run or -apply")
			os.Exit(2)
		}
		repo, err := builder.BuildDatabase()
		if err != nil || repo == nil {
			fmt.Fprintf(os.Stderr, "Member-tag operation failed: database unavailable: %v\n", err)
			os.Exit(1)
		}
		defer repo.Pool.Close()
		if err := database.NewMigrator(repo.Pool, log).Apply(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Member-tag migration failed: %v\n", err)
			os.Exit(1)
		}
		if strings.TrimSpace(*importMemberTags) != "" {
			if err := runMemberTagImportApply(ctx, repo, *importMemberTags); err != nil {
				fmt.Fprintf(os.Stderr, "Member-tag import failed: %v\n", err)
				os.Exit(1)
			}
		}
		if *suppressMemberTagID > 0 || strings.TrimSpace(*suppressMemberTagLogin) != "" {
			if err := suppressMemberTag(ctx, repo.Queries, *suppressMemberTagID, *suppressMemberTagLogin, *suppressionReason); err != nil {
				fmt.Fprintf(os.Stderr, "Member-tag suppression failed: %v\n", err)
				os.Exit(1)
			}
		}
		return
	}

	if *migrate || *migrateRollback || *migrateStatus {
		repo, err := builder.BuildDatabase()
		if err != nil {
			log.Error("failed to connect to database", "error", err)
			fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
			os.Exit(1)
		}
		defer repo.Pool.Close()
		if err := database.Run(ctx, repo.Pool, log, *migrate, *migrateRollback, *migrateStatus); err != nil {
			log.Error("migration failed", "error", err)
			fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := builder.Run(); err != nil {
		log.Error("application runtime failed", "error", err)
		fmt.Fprintf(os.Stderr, "Application failed: %v\n", err)
		os.Exit(1)
	}
}

func runMemberTagImportDryRun(path string) error {
	report, _, err := readMemberTagImport(path)
	if err != nil {
		return err
	}
	printMemberTagReport(report, 0)
	return nil
}

func runMemberTagImportApply(ctx context.Context, repo *db.DBWrapper, path string) error {
	report, observedAt, err := readMemberTagImport(path)
	if err != nil {
		return err
	}
	queued, err := membertagimport.Apply(ctx, repo.Pool, report, observedAt)
	if err != nil {
		return err
	}
	printMemberTagReport(report, queued)
	return nil
}

func readMemberTagImport(path string) (membertagimport.Report, time.Time, error) {
	file, err := os.Open(path)
	if err != nil {
		return membertagimport.Report{}, time.Time{}, fmt.Errorf("open import: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return membertagimport.Report{}, time.Time{}, fmt.Errorf("stat import: %w", err)
	}
	if !info.Mode().IsRegular() {
		return membertagimport.Report{}, time.Time{}, fmt.Errorf("import path must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return membertagimport.Report{}, time.Time{}, fmt.Errorf("import file permissions must not allow group or other access")
	}
	report, err := membertagimport.Parse(file)
	return report, info.ModTime().UTC(), err
}

func printMemberTagReport(report membertagimport.Report, queued int64) {
	fmt.Printf("rows=%d accepted=%d skipped_invalid=%d skipped_conflict_rows=%d conflict_ids=%d skipped_null_status=%d queued=%d source_digest=%s\n",
		report.TotalRows, report.AcceptedRows, report.SkippedInvalidRows, report.SkippedConflictRows,
		report.SkippedConflictIDs, report.SkippedNullStatusRows, queued, report.SourceDigest)
	for _, issue := range report.Issues {
		fmt.Printf("issue line=%d code=%s ref=%s\n", issue.Line, issue.Code, issue.SafeHash)
	}
}

func suppressMemberTag(ctx context.Context, queries *db.Queries, telegramID int64, login, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "administrator_request"
	}
	switch reason {
	case "administrator_request", "privacy_request", "mapping_correction":
	default:
		return fmt.Errorf("unsupported suppression reason")
	}
	if telegramID > 0 {
		if err := queries.SuppressLegacyMemberTagByTelegram(ctx, db.SuppressLegacyMemberTagByTelegramParams{TelegramUserID: pgtype.Int8{Int64: telegramID, Valid: true}, Reason: reason}); err != nil {
			return err
		}
		if _, err := queries.EnqueueSuppressedLegacyMemberTagsByTelegram(ctx, telegramID); err != nil {
			return err
		}
	}
	login = strings.ToLower(strings.TrimSpace(login))
	if login != "" {
		if err := queries.SuppressLegacyMemberTagByLogin(ctx, db.SuppressLegacyMemberTagByLoginParams{Lower: login, Reason: reason}); err != nil {
			return err
		}
		if _, err := queries.EnqueueSuppressedLegacyMemberTagsByLogin(ctx, login); err != nil {
			return err
		}
	}
	fmt.Println("member-tag suppression recorded; matching managed tags queued for cleanup")
	return nil
}
