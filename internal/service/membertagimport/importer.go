package membertagimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

var (
	fromIDPattern = regexp.MustCompile(`^user([1-9][0-9]*)$`)
	loginPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
)

type inputRow struct {
	FromID   string `json:"from_id"`
	Login    string `json:"text"`
	IsActive *bool  `json:"isActive"`
}

type Mapping struct {
	TelegramUserID int64
	Login          string
	Active         bool
	Line           int
}

type Issue struct {
	Line     int
	Code     string
	SafeHash string
}

type Report struct {
	TotalRows             int
	AcceptedRows          int
	SkippedInvalidRows    int
	SkippedConflictRows   int
	SkippedConflictIDs    int
	SkippedNullStatusRows int
	Issues                []Issue
	Mappings              []Mapping
	SourceDigest          string
}

func Parse(r io.Reader) (Report, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Report{}, fmt.Errorf("read import: %w", err)
	}
	digest := sha256.Sum256(data)
	report := Report{SourceDigest: hex.EncodeToString(digest[:])}

	var rawRows []json.RawMessage
	if err := json.Unmarshal(data, &rawRows); err != nil {
		return Report{}, fmt.Errorf("decode import json: %w", err)
	}
	report.TotalRows = len(rawRows)

	type candidate struct {
		telegramUserID int64
		login          string
		active         *bool
		line           int
	}
	candidates := make([]candidate, 0, len(rawRows))
	byTelegram := make(map[int64][]candidate)
	for i, rawRow := range rawRows {
		line := i + 1
		var row inputRow
		if err := json.Unmarshal(rawRow, &row); err != nil {
			report.SkippedInvalidRows++
			report.Issues = append(report.Issues, issue(line, "invalid_row_types", string(rawRow)))
			continue
		}
		match := fromIDPattern.FindStringSubmatch(strings.TrimSpace(row.FromID))
		login := strings.ToLower(strings.TrimSpace(row.Login))
		if len(match) != 2 || !loginPattern.MatchString(login) {
			report.SkippedInvalidRows++
			report.Issues = append(report.Issues, issue(line, "invalid_shape", row.FromID+"\x00"+row.Login))
			continue
		}
		telegramID, parseErr := strconv.ParseInt(match[1], 10, 64)
		if parseErr != nil || telegramID <= 0 {
			report.SkippedInvalidRows++
			report.Issues = append(report.Issues, issue(line, "invalid_telegram_id", row.FromID))
			continue
		}
		if row.IsActive == nil {
			report.SkippedNullStatusRows++
		}
		candidate := candidate{
			telegramUserID: telegramID,
			login:          login,
			active:         row.IsActive,
			line:           line,
		}
		candidates = append(candidates, candidate)
		byTelegram[telegramID] = append(byTelegram[telegramID], candidate)
	}

	conflicts := make(map[int64]bool)
	for telegramID, group := range byTelegram {
		values := make(map[string]bool)
		for _, candidate := range group {
			values[candidate.login] = true
		}
		if len(values) > 1 {
			conflicts[telegramID] = true
			report.SkippedConflictIDs++
			report.SkippedConflictRows += len(group)
			for _, candidate := range group {
				report.Issues = append(report.Issues, issue(candidate.line, "conflicting_telegram_mapping", strconv.FormatInt(telegramID, 10)))
			}
		}
	}

	seen := make(map[int64]bool)
	for _, candidate := range candidates {
		if conflicts[candidate.telegramUserID] || seen[candidate.telegramUserID] {
			continue
		}
		if candidate.active == nil {
			report.Issues = append(report.Issues, issue(candidate.line, "missing_active_snapshot", strconv.FormatInt(candidate.telegramUserID, 10)+"\x00"+candidate.login))
			continue
		}
		seen[candidate.telegramUserID] = true
		report.Mappings = append(report.Mappings, Mapping{
			TelegramUserID: candidate.telegramUserID, Login: candidate.login, Active: *candidate.active, Line: candidate.line,
		})
	}
	sort.Slice(report.Mappings, func(i, j int) bool { return report.Mappings[i].TelegramUserID < report.Mappings[j].TelegramUserID })
	sort.Slice(report.Issues, func(i, j int) bool { return report.Issues[i].Line < report.Issues[j].Line })
	report.AcceptedRows = len(report.Mappings)
	return report, nil
}

func Apply(ctx context.Context, pool *pgxpool.Pool, report Report, observedAt time.Time) (int64, error) {
	if pool == nil {
		return 0, errors.New("database pool is nil")
	}
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin member-tag import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	for _, mapping := range report.Mappings {
		if err := queries.DeleteLegacyMemberTagMappingByLoginExceptTelegram(ctx, db.DeleteLegacyMemberTagMappingByLoginExceptTelegramParams{
			S21Login: mapping.Login, TelegramUserID: mapping.TelegramUserID,
		}); err != nil {
			return 0, fmt.Errorf("replace legacy mapping: %w", err)
		}
		if err := queries.UpsertLegacyMemberTagMapping(ctx, db.UpsertLegacyMemberTagMappingParams{
			TelegramUserID:     mapping.TelegramUserID,
			S21Login:           mapping.Login,
			ActiveSnapshot:     mapping.Active,
			SnapshotObservedAt: dbTimestamp(observedAt),
			SourceDigest:       report.SourceDigest,
		}); err != nil {
			return 0, fmt.Errorf("upsert legacy mapping: %w", err)
		}
	}
	queued, err := queries.EnqueueKnownLegacyMemberTags(ctx)
	if err != nil {
		return 0, fmt.Errorf("enqueue known legacy tags: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit member-tag import: %w", err)
	}
	return queued, nil
}

func issue(line int, code, value string) Issue {
	sum := sha256.Sum256([]byte(value))
	return Issue{Line: line, Code: code, SafeHash: hex.EncodeToString(sum[:8])}
}

func dbTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
