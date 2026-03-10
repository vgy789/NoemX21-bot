package service

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

// EnsureCoalitionPresent checks whether a coalition with given ID exists in DB.
// If it's missing, it fetches all coalitions by participant campus and upserts them.
func EnsureCoalitionPresent(
	ctx context.Context,
	queries db.Querier,
	s21Client *s21.Client,
	token string,
	coalition *s21.ParticipantCoalitionV1DTO,
	campusID string,
	log *slog.Logger,
) error {
	if coalition == nil || coalition.CoalitionID == 0 {
		return nil
	}

	var campusUUID pgtype.UUID
	if err := campusUUID.Scan(campusID); err != nil {
		log.Warn("invalid or empty campus id, skipping coalition upsert", "campus_id", campusID, "coalition_id", coalition.CoalitionID, "error", err)
		return nil
	}

	exists, err := queries.ExistsCoalitionByID(ctx, db.ExistsCoalitionByIDParams{
		CampusID: campusUUID,
		ID:       int16(coalition.CoalitionID),
	})
	if err != nil {
		log.Warn("failed to check coalition in db, will continue with upsert", "campus_id", campusID, "id", coalition.CoalitionID, "error", err)
	} else if exists {
		return nil
	}

	if campusID != "" && token != "" && s21Client != nil {
		campusCoalitions, fetchErr := s21Client.GetCampusCoalitions(ctx, token, campusID)
		if fetchErr != nil {
			log.Warn("failed to fetch campus coalitions, fallback to single coalition upsert", "campus_id", campusID, "coalition_id", coalition.CoalitionID, "error", fetchErr)
		} else {
			found := false
			for _, c := range campusCoalitions {
				if c.CoalitionID == 0 {
					continue
				}
				if err := queries.UpsertCoalition(ctx, db.UpsertCoalitionParams{
					CampusID: campusUUID,
					ID:       int16(c.CoalitionID),
					Name:     c.Name,
				}); err != nil {
					log.Error("failed to upsert coalition from campus list", "campus_id", campusID, "id", c.CoalitionID, "name", c.Name, "error", err)
					return err
				}
				if c.CoalitionID == coalition.CoalitionID {
					found = true
				}
			}
			if found {
				return nil
			}
			log.Warn("participant coalition was not found in campus coalitions response, fallback to single upsert", "campus_id", campusID, "coalition_id", coalition.CoalitionID)
		}
	}

	// Fallback: keep old behavior and upsert only participant coalition.
	err = queries.UpsertCoalition(ctx, db.UpsertCoalitionParams{
		CampusID: campusUUID,
		ID:       int16(coalition.CoalitionID),
		Name:     coalition.CoalitionName,
	})
	if err != nil {
		log.Error("failed to upsert coalition", "id", coalition.CoalitionID, "name", coalition.CoalitionName, "error", err)
		return err
	}

	return nil
}
