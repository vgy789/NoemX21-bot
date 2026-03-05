package settings

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"go.uber.org/mock/gomock"
)

func TestLoadProfileSettings(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, log, q, nil)

	action, ok := reg.Get("load_profile_settings")
	require.True(t, ok)

	q.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "42",
	}).Return(db.UserAccount{
		S21Login:     "student",
		IsSearchable: pgtype.Bool{Bool: true, Valid: true},
	}, nil)
	q.EXPECT().GetMyProfile(gomock.Any(), "student").Return(db.GetMyProfileRow{
		AlternativeContact: pgtype.Text{String: "peer@example.com", Valid: true},
	}, nil)

	_, updates, err := action(context.Background(), 42, nil)
	require.NoError(t, err)
	require.Equal(t, "✅ Виден", updates["my_searchable_status_ru"])
	require.Equal(t, "✅ Visible", updates["my_searchable_status_en"])
	require.Equal(t, "peer@example.com", updates["my_alt_contact"])
	require.Equal(t, true, updates["has_alt_contact"])
}

func TestToggleSearchable(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, log, q, nil)

	action, ok := reg.Get("toggle_searchable")
	require.True(t, ok)

	q.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "42",
	}).Return(db.UserAccount{
		IsSearchable: pgtype.Bool{Bool: true, Valid: true},
	}, nil)
	q.EXPECT().UpdateUserAccountSearchableByExternalId(gomock.Any(), db.UpdateUserAccountSearchableByExternalIdParams{
		Platform:     db.EnumPlatformTelegram,
		ExternalID:   "42",
		IsSearchable: pgtype.Bool{Bool: false, Valid: true},
	}).Return(db.UserAccount{}, nil)

	_, updates, err := action(context.Background(), 42, nil)
	require.NoError(t, err)
	require.Equal(t, "❌ Не виден", updates["my_searchable_status_ru"])
	require.Equal(t, "❌ Not visible", updates["is_searchable_label_en"])
}

func TestSetAlternativeContact(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, log, q, nil)

	action, ok := reg.Get("set_alternative_contact")
	require.True(t, ok)

	q.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "42",
	}).Return(db.UserAccount{
		S21Login: "student",
	}, nil)
	q.EXPECT().GetRegisteredUserByS21Login(gomock.Any(), "student").Return(db.RegisteredUser{
		S21Login:     "student",
		RocketchatID: "rc-id",
		Timezone:     "UTC",
		HasCoffeeBan: pgtype.Bool{Bool: false, Valid: true},
	}, nil)
	q.EXPECT().UpsertRegisteredUser(gomock.Any(), db.UpsertRegisteredUserParams{
		S21Login:           "student",
		RocketchatID:       "rc-id",
		Timezone:           "UTC",
		AlternativeContact: pgtype.Text{String: "peer@example.com", Valid: true},
		HasCoffeeBan:       pgtype.Bool{Bool: false, Valid: true},
	}).Return(db.RegisteredUser{}, nil)

	_, updates, err := action(context.Background(), 42, map[string]any{"last_input": "peer@example.com"})
	require.NoError(t, err)
	require.Equal(t, "peer@example.com", updates["my_alt_contact"])
	require.Equal(t, true, updates["has_alt_contact"])
}

func TestCheckTelegramUsername(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, log, q, nil)

	action, ok := reg.Get("check_telegram_username")
	require.True(t, ok)

	q.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "42",
	}).Return(db.UserAccount{
		Username: pgtype.Text{String: "@vgy789", Valid: true},
	}, nil)

	_, updates, err := action(context.Background(), 42, nil)
	require.NoError(t, err)
	require.Equal(t, true, updates["has_telegram_username"])
	require.Equal(t, "vgy789", updates["telegram_username"])
}

func TestCheckTelegramUsernameMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, log, q, nil)

	action, ok := reg.Get("check_telegram_username")
	require.True(t, ok)

	q.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "42",
	}).Return(db.UserAccount{
		Username: pgtype.Text{String: "   ", Valid: true},
	}, nil)

	_, updates, err := action(context.Background(), 42, nil)
	require.NoError(t, err)
	require.Equal(t, false, updates["has_telegram_username"])
	require.Equal(t, "", updates["telegram_username"])
}
