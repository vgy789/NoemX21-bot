package actions

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// RegisterSettingsActions registers actions related to user settings.
func RegisterSettingsActions(
	registry *fsm.LogicRegistry,
	log *slog.Logger,
	queries db.Querier,
) {
	updateLanguage := func(ctx context.Context, userID int64, langCode string) {
		ua, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			log.Warn("user account not found when updating language", "user_id", userID)
			return
		}

		settings, err := queries.GetUserBotSettings(ctx, ua.ID)
		notifications := pgtype.Bool{Bool: true, Valid: true}
		reviews := []byte("[]")

		if err == nil {
			notifications = settings.NotificationsEnabled
			reviews = settings.ReviewPostCampusIds
		}

		_, err = queries.UpsertUserBotSettings(ctx, db.UpsertUserBotSettingsParams{
			UserAccountID:        ua.ID,
			LanguageCode:         pgtype.Text{String: langCode, Valid: true},
			NotificationsEnabled: notifications,
			ReviewPostCampusIds:  reviews,
		})
		if err != nil {
			log.Error("failed to update user language", "error", err, "user_id", userID, "lang", langCode)
		} else {
			log.Info("updated user language", "user_id", userID, "lang", langCode)
		}
	}

	registry.Register("input:set_ru", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		log.Info("switching language to RU", "user_id", userID)
		updateLanguage(ctx, userID, fsm.LangRu)
		return "", map[string]interface{}{"language": fsm.LangRu}, nil
	})
	registry.Register("input:set_en", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		log.Info("switching language to EN", "user_id", userID)
		updateLanguage(ctx, userID, fsm.LangEn)
		return "", map[string]interface{}{"language": fsm.LangEn}, nil
	})
	registry.Register("input:ru", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		log.Info("settings: switching language to RU", "user_id", userID)
		updateLanguage(ctx, userID, fsm.LangRu)
		return "", map[string]interface{}{"language": fsm.LangRu}, nil
	})
	registry.Register("input:en", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		log.Info("settings: switching language to EN", "user_id", userID)
		updateLanguage(ctx, userID, fsm.LangEn)
		return "", map[string]interface{}{"language": fsm.LangEn}, nil
	})
}
