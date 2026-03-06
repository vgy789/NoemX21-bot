package settings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

const unlinkedExternalIDPrefix = "unlinked:"

type userAccountUnlinker interface {
	UnlinkUserAccountByExternalId(ctx context.Context, arg db.UnlinkUserAccountByExternalIdParams) (db.UserAccount, error)
}

// Register registers settings-related actions.
func Register(registry *fsm.LogicRegistry, log *slog.Logger, queries db.Querier, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("SETTINGS_MENU", "settings.yaml/SETTINGS_MENU")
	}

	getTelegramAccount := func(ctx context.Context, userID int64) (db.UserAccount, error) {
		return queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
	}

	updateLanguage := func(ctx context.Context, userID int64, langCode string) {
		ua, err := getTelegramAccount(ctx, userID)
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

	registry.Register("input:set_ru", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		log.Info("switching language to RU", "user_id", userID)
		updateLanguage(ctx, userID, fsm.LangRu)
		return "", map[string]any{"language": fsm.LangRu}, nil
	})
	registry.Register("input:set_en", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		log.Info("switching language to EN", "user_id", userID)
		updateLanguage(ctx, userID, fsm.LangEn)
		return "", map[string]any{"language": fsm.LangEn}, nil
	})
	registry.Register("input:ru", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		log.Info("settings: switching language to RU", "user_id", userID)
		updateLanguage(ctx, userID, fsm.LangRu)
		return "", map[string]any{"language": fsm.LangRu}, nil
	})
	registry.Register("input:en", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		log.Info("settings: switching language to EN", "user_id", userID)
		updateLanguage(ctx, userID, fsm.LangEn)
		return "", map[string]any{"language": fsm.LangEn}, nil
	})

	registry.Register("load_profile_settings", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		vars := map[string]any{
			"my_searchable_status_ru":   "❌ Не виден",
			"my_searchable_status_en":   "❌ Not visible",
			"is_searchable_label_ru":    "❌ Не виден",
			"is_searchable_label_en":    "❌ Not visible",
			"my_alt_contact":            "❌ Not set",
			"my_alt_contact_display_ru": "❌ Не задан",
			"my_alt_contact_display_en": "❌ Not set",
			"has_alt_contact":           false,
		}

		ua, err := getTelegramAccount(ctx, userID)
		if err != nil {
			log.Warn("user account not found when loading profile settings", "user_id", userID, "error", err)
			return "", vars, nil
		}

		if ua.IsSearchable.Valid && ua.IsSearchable.Bool {
			vars["my_searchable_status_ru"] = "✅ Виден"
			vars["my_searchable_status_en"] = "✅ Visible"
			vars["is_searchable_label_ru"] = "✅ Виден"
			vars["is_searchable_label_en"] = "✅ Visible"
		}

		profile, err := queries.GetMyProfile(ctx, ua.S21Login)
		if err != nil {
			log.Warn("user profile not found when loading profile settings", "user_id", userID, "s21_login", ua.S21Login, "error", err)
			return "", vars, nil
		}

		if profile.AlternativeContact.Valid {
			alt := strings.TrimSpace(profile.AlternativeContact.String)
			if alt != "" {
				vars["my_alt_contact"] = alt
				vars["my_alt_contact_display_ru"] = alt
				vars["my_alt_contact_display_en"] = alt
				vars["has_alt_contact"] = true
			}
		}

		return "", vars, nil
	})

	registry.Register("check_telegram_username", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		ua, err := getTelegramAccount(ctx, userID)
		if err != nil {
			log.Warn("user account not found when checking telegram username", "user_id", userID, "error", err)
			return "", map[string]any{
				"has_telegram_username": false,
				"telegram_username":     "",
			}, nil
		}

		username := ""
		if ua.Username.Valid {
			username = strings.TrimSpace(strings.TrimPrefix(ua.Username.String, "@"))
		}

		return "", map[string]any{
			"has_telegram_username": username != "",
			"telegram_username":     username,
		}, nil
	})

	registry.Register("toggle_searchable", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		ua, err := getTelegramAccount(ctx, userID)
		if err != nil {
			return "", nil, fmt.Errorf("user account not found: %w", err)
		}

		newValue := true
		if ua.IsSearchable.Valid {
			newValue = !ua.IsSearchable.Bool
		}

		if _, err := queries.UpdateUserAccountSearchableByExternalId(ctx, db.UpdateUserAccountSearchableByExternalIdParams{
			Platform:     db.EnumPlatformTelegram,
			ExternalID:   fmt.Sprintf("%d", userID),
			IsSearchable: pgtype.Bool{Bool: newValue, Valid: true},
		}); err != nil {
			return "", nil, fmt.Errorf("failed to update user searchable status: %w", err)
		}

		if newValue {
			return "", map[string]any{
				"my_searchable_status_ru": "✅ Виден",
				"my_searchable_status_en": "✅ Visible",
				"is_searchable_label_ru":  "✅ Виден",
				"is_searchable_label_en":  "✅ Visible",
				"is_searchable":           true,
			}, nil
		}

		return "", map[string]any{
			"my_searchable_status_ru": "❌ Не виден",
			"my_searchable_status_en": "❌ Not visible",
			"is_searchable_label_ru":  "❌ Не виден",
			"is_searchable_label_en":  "❌ Not visible",
			"is_searchable":           false,
		}, nil
	})

	registry.Register("set_alternative_contact", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		input, _ := payload["last_input"].(string)
		contact := strings.TrimSpace(input)
		if contact == "" {
			return "", nil, fmt.Errorf("empty contact")
		}
		if len([]rune(contact)) > 42 {
			return "", nil, fmt.Errorf("contact is too long")
		}

		ua, err := getTelegramAccount(ctx, userID)
		if err != nil {
			return "", nil, fmt.Errorf("user account not found: %w", err)
		}

		r, err := queries.GetRegisteredUserByS21Login(ctx, ua.S21Login)
		if err != nil {
			return "", nil, fmt.Errorf("registered user not found: %w", err)
		}

		_, err = queries.UpsertRegisteredUser(ctx, db.UpsertRegisteredUserParams{
			S21Login:           r.S21Login,
			RocketchatID:       r.RocketchatID,
			Timezone:           r.Timezone,
			AlternativeContact: pgtype.Text{String: contact, Valid: true},
			HasCoffeeBan:       r.HasCoffeeBan,
		})
		if err != nil {
			return "", nil, fmt.Errorf("failed to update alternative contact: %w", err)
		}

		return "", map[string]any{
			"my_alt_contact":            contact,
			"my_alt_contact_display_ru": contact,
			"my_alt_contact_display_en": contact,
			"has_alt_contact":           true,
		}, nil
	})

	registry.Register("delete_alternative_contact", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		ua, err := getTelegramAccount(ctx, userID)
		if err != nil {
			return "", nil, fmt.Errorf("user account not found: %w", err)
		}

		r, err := queries.GetRegisteredUserByS21Login(ctx, ua.S21Login)
		if err != nil {
			return "", nil, fmt.Errorf("registered user not found: %w", err)
		}

		_, err = queries.UpsertRegisteredUser(ctx, db.UpsertRegisteredUserParams{
			S21Login:           r.S21Login,
			RocketchatID:       r.RocketchatID,
			Timezone:           r.Timezone,
			AlternativeContact: pgtype.Text{String: "", Valid: true},
			HasCoffeeBan:       r.HasCoffeeBan,
		})
		if err != nil {
			return "", nil, fmt.Errorf("failed to delete alternative contact: %w", err)
		}

		return "", map[string]any{
			"my_alt_contact":            "not set",
			"my_alt_contact_display_ru": "не задан",
			"my_alt_contact_display_en": "not set",
			"has_alt_contact":           false,
		}, nil
	})

	// API Token actions
	registry.Register("generate_api_token", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		ua, err := getTelegramAccount(ctx, userID)
		if err != nil {
			return "", nil, fmt.Errorf("user account not found: %w", err)
		}

		apiKeySvc := service.NewApiKeyService(queries)
		token, err := apiKeySvc.GenerateApiKey(ctx, ua.ID)
		if err != nil {
			return "", nil, err
		}

		return "", map[string]any{
			"my_botapi_token": token,
		}, nil
	})

	registry.Register("load_api_token", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		ua, err := getTelegramAccount(ctx, userID)
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

		return "", map[string]any{
			"my_botapi_token": prefix,
		}, nil
	})

	// Delete profile action: remove user account record
	registry.Register("delete_profile", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		externalID := fmt.Sprintf("%d", userID)

		ua, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: externalID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				log.Info("user account already unlinked", "user_id", userID)
				return "", map[string]any{
					"success":                             true,
					"delete_profile_active_loans":         0,
					"delete_profile_active_room_bookings": 0,
				}, nil
			}
			log.Error("failed to get user account for deletion", "error", err, "user_id", userID)
			return "", nil, err
		}

		activeLoansCount, err := queries.GetUserActiveLoanCount(ctx, ua.ID)
		if err != nil {
			log.Error("failed to count active loans before unlink", "error", err, "user_id", userID, "user_account_id", ua.ID)
			return "", nil, err
		}

		activeRoomBookingsRows, err := queries.GetUserRoomBookings(ctx, ua.ID)
		if err != nil {
			log.Error("failed to get active room bookings before unlink", "error", err, "user_id", userID, "user_account_id", ua.ID)
			return "", nil, err
		}

		activeLoans := int(activeLoansCount)
		activeRoomBookings := len(activeRoomBookingsRows)
		updates := map[string]any{
			"success":                             false,
			"delete_profile_active_loans":         activeLoans,
			"delete_profile_active_room_bookings": activeRoomBookings,
		}

		// Do not unlink while the user still has active obligations.
		if activeLoans > 0 || activeRoomBookings > 0 {
			log.Info("delete profile blocked by active obligations", "user_id", userID, "active_loans", activeLoans, "active_room_bookings", activeRoomBookings)
			return "", updates, nil
		}

		if err := queries.RevokeOldApiKeys(ctx, ua.ID); err != nil {
			log.Warn("failed to revoke api keys during unlink", "error", err, "user_id", userID, "user_account_id", ua.ID)
		}

		// Prefer detaching external_id instead of deleting the row because historical records
		// (book loans/bookings) reference user_accounts via FK.
		if unlinker, ok := queries.(userAccountUnlinker); ok {
			newExternalID := fmt.Sprintf("%s%d:%d", unlinkedExternalIDPrefix, userID, time.Now().UnixNano())
			unlinked, err := unlinker.UnlinkUserAccountByExternalId(ctx, db.UnlinkUserAccountByExternalIdParams{
				Platform:      db.EnumPlatformTelegram,
				ExternalID:    externalID,
				NewExternalID: newExternalID,
			})
			if err != nil {
				log.Error("failed to unlink user account", "error", err, "user_id", userID, "user_account_id", ua.ID)
				return "", updates, err
			}
			log.Info("unlinked user account", "user_account_id", unlinked.ID, "user_id", userID, "old_external_id", externalID)
			updates["success"] = true
			return "", updates, nil
		}

		// Legacy fallback path for mocks/tests that don't provide unlink method.
		if err := queries.DeleteUserAccountByExternalId(ctx, db.DeleteUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: externalID,
		}); err != nil {
			log.Error("failed to delete user account (legacy unlink)", "error", err, "user_id", userID, "user_account_id", ua.ID)
			return "", updates, err
		}

		log.Info("deleted user account (legacy unlink)", "user_account_id", ua.ID, "user_id", userID)
		updates["success"] = true
		return "", updates, nil
	})
}
