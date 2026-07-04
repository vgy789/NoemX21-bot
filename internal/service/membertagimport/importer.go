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
	FromID           string   `json:"from_id"`
	Login            string   `json:"text"`
	IsActive         *bool    `json:"isActive"`
	CampusID         *string  `json:"campus_id"`
	CoalitionID      *int16   `json:"coalition_id"`
	CoalitionName    *string  `json:"coalition_name"`
	Status           *string  `json:"status"`
	Level            *int32   `json:"level"`
	ExpValue         *int32   `json:"exp_value"`
	Prp              *int32   `json:"prp"`
	Crp              *int32   `json:"crp"`
	Coins            *int32   `json:"coins"`
	ParallelName     *string  `json:"parallel_name"`
	ClassName        *string  `json:"class_name"`
	Integrity        *float32 `json:"integrity"`
	Friendliness     *float32 `json:"friendliness"`
	Punctuality      *float32 `json:"punctuality"`
	Thoroughness     *float32 `json:"thoroughness"`
	ProfileUpdatedAt *string  `json:"profile_updated_at"`
}

type Mapping struct {
	TelegramUserID int64
	Login          string
	Active         bool
	Line           int
	Stats          *ProfileStats
}

type ProfileStats struct {
	CampusID         pgtype.UUID
	CoalitionID      pgtype.Int2
	CoalitionName    string
	Status           db.EnumStudentStatus
	Level            int32
	ExpValue         int32
	Prp              int32
	Crp              int32
	Coins            int32
	ParallelName     pgtype.Text
	ClassName        pgtype.Text
	Integrity        pgtype.Float4
	Friendliness     pgtype.Float4
	Punctuality      pgtype.Float4
	Thoroughness     pgtype.Float4
	ProfileUpdatedAt time.Time
}

type Issue struct {
	Line     int
	Code     string
	SafeHash string
}

type Report struct {
	TotalRows               int
	AcceptedRows            int
	SkippedInvalidRows      int
	SkippedConflictRows     int
	SkippedConflictIDs      int
	SkippedNullStatusRows   int
	AcceptedStatsRows       int
	SkippedInvalidStatsRows int
	Issues                  []Issue
	Mappings                []Mapping
	SourceDigest            string
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
		stats          *ProfileStats
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
		stats, statsErr := parseProfileStats(row)
		if statsErr != nil {
			report.SkippedInvalidStatsRows++
			report.Issues = append(report.Issues, issue(line, "invalid_profile_stats", row.Login))
		}
		candidate.stats = stats
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
		if candidate.stats != nil {
			report.AcceptedStatsRows++
		}
		report.Mappings = append(report.Mappings, Mapping{
			TelegramUserID: candidate.telegramUserID, Login: candidate.login, Active: *candidate.active, Line: candidate.line, Stats: candidate.stats,
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
		if mapping.Stats != nil {
			stats := *mapping.Stats
			if stats.CampusID.Valid {
				if _, err := queries.GetCampusByID(ctx, stats.CampusID); err != nil {
					stats.CampusID = pgtype.UUID{}
					stats.CoalitionID = pgtype.Int2{}
				} else if stats.CoalitionID.Valid {
					exists, err := queries.ExistsCoalitionByID(ctx, db.ExistsCoalitionByIDParams{CampusID: stats.CampusID, ID: stats.CoalitionID.Int16})
					if err != nil || !exists {
						stats.CoalitionID = pgtype.Int2{}
					}
				} else if stats.CoalitionName != "" {
					coalition, err := queries.GetCoalitionByCampusAndName(ctx, db.GetCoalitionByCampusAndNameParams{CampusID: stats.CampusID, Lower: stats.CoalitionName})
					if err == nil {
						stats.CoalitionID = pgtype.Int2{Int16: coalition.ID, Valid: true}
					}
				}
			}
			if err := queries.UpsertImportedParticipantStatsCache(ctx, db.UpsertImportedParticipantStatsCacheParams{
				S21Login: mapping.Login, CampusID: stats.CampusID, CoalitionID: stats.CoalitionID,
				Status: stats.Status, Level: stats.Level, ExpValue: stats.ExpValue, Prp: stats.Prp, Crp: stats.Crp, Coins: stats.Coins,
				ParallelName: stats.ParallelName, ClassName: stats.ClassName, Integrity: stats.Integrity,
				Friendliness: stats.Friendliness, Punctuality: stats.Punctuality, Thoroughness: stats.Thoroughness,
				UpdatedAt: dbTimestamp(stats.ProfileUpdatedAt),
			}); err != nil {
				return 0, fmt.Errorf("upsert imported profile stats: %w", err)
			}
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

func parseProfileStats(row inputRow) (*ProfileStats, error) {
	if row.ProfileUpdatedAt == nil || strings.TrimSpace(*row.ProfileUpdatedAt) == "" {
		return nil, nil
	}
	if row.Status == nil || row.Level == nil || row.ExpValue == nil || row.Prp == nil || row.Crp == nil || row.Coins == nil {
		return nil, errors.New("incomplete required profile stats")
	}
	updatedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(*row.ProfileUpdatedAt))
	if err != nil {
		return nil, fmt.Errorf("parse profile_updated_at: %w", err)
	}
	status := db.EnumStudentStatus(strings.ToUpper(strings.TrimSpace(*row.Status)))
	if !validStudentStatus(status) {
		return nil, errors.New("invalid student status")
	}
	stats := &ProfileStats{
		Status: status, Level: *row.Level, ExpValue: *row.ExpValue, Prp: *row.Prp, Crp: *row.Crp, Coins: *row.Coins,
		ParallelName: nullableText(row.ParallelName), ClassName: nullableText(row.ClassName),
		Integrity: nullableFloat4(row.Integrity), Friendliness: nullableFloat4(row.Friendliness),
		Punctuality: nullableFloat4(row.Punctuality), Thoroughness: nullableFloat4(row.Thoroughness),
		ProfileUpdatedAt: updatedAt.UTC(),
		CoalitionName:    strings.TrimSpace(valueOrEmpty(row.CoalitionName)),
	}
	if row.CampusID != nil && strings.TrimSpace(*row.CampusID) != "" {
		if err := stats.CampusID.Scan(strings.TrimSpace(*row.CampusID)); err != nil {
			return nil, errors.New("invalid campus id")
		}
	}
	if row.CoalitionID != nil {
		stats.CoalitionID = pgtype.Int2{Int16: *row.CoalitionID, Valid: true}
	}
	return stats, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func validStudentStatus(status db.EnumStudentStatus) bool {
	switch status {
	case db.EnumStudentStatusACTIVE, db.EnumStudentStatusTEMPORARYBLOCKING, db.EnumStudentStatusEXPELLED,
		db.EnumStudentStatusBLOCKED, db.EnumStudentStatusFROZEN, db.EnumStudentStatusSTUDYCOMPLETED:
		return true
	default:
		return false
	}
}

func nullableText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: strings.TrimSpace(*value), Valid: true}
}

func nullableFloat4(value *float32) pgtype.Float4 {
	if value == nil {
		return pgtype.Float4{}
	}
	return pgtype.Float4{Float32: *value, Valid: true}
}

func issue(line int, code, value string) Issue {
	sum := sha256.Sum256([]byte(value))
	return Issue{Line: line, Code: code, SafeHash: hex.EncodeToString(sum[:8])}
}

func dbTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
