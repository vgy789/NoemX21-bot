package settings

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

// Register registers settings-related actions.
func Register(registry *fsm.LogicRegistry, log *slog.Logger, queries db.Querier, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("SETTINGS_MENU", "settings.yaml/SETTINGS_MENU")
	}

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

	// API Token actions
	registry.Register("generate_api_token", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		ua, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", nil, fmt.Errorf("user account not found: %w", err)
		}

		apiKeySvc := service.NewApiKeyService(queries)
		token, err := apiKeySvc.GenerateApiKey(ctx, ua.ID)
		if err != nil {
			return "", nil, err
		}

		return "", map[string]interface{}{
			"my_botapi_token": token,
		}, nil
	})

	registry.Register("load_api_token", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		ua, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", nil, fmt.Errorf("user account not found: %w", err)
		}

		apiKeySvc := service.NewApiKeyService(queries)
		prefix, err := apiKeySvc.GetActiveApiKey(ctx, ua.ID)
		if err != nil {
			return "", nil, err
		}

		if prefix == "" {
			prefix = "нет / none"
		}

		return "", map[string]interface{}{
			"my_botapi_token": prefix,
		}, nil
	})
}
