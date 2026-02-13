package service

import (
	"context"
	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

// EnsureCoalitionPresent checks whether a coalition with given ID exists in DB.
// If it's missing, it upserts the coalition name provided by S21 API.
func EnsureCoalitionPresent(ctx context.Context, queries db.Querier, coalition *s21.ParticipantCoalitionV1DTO, log *slog.Logger) error {
	if coalition == nil || coalition.CoalitionID == 0 {
		return nil
	}

	// Just use UpsertCoalition directly as it's safe (ON CONFLICT DO UPDATE) and cheap.
	err := queries.UpsertCoalition(ctx, db.UpsertCoalitionParams{
		ID:   int16(coalition.CoalitionID),
		Name: coalition.CoalitionName,
	})
	if err != nil {
		log.Error("failed to upsert coalition", "id", coalition.CoalitionID, "name", coalition.CoalitionName, "error", err)
		return err
	}

	return nil
}
