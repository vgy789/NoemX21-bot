package telegram

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbmock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/service/membertagimport"
	"go.uber.org/mock/gomock"
)

func TestIsConfiguredBotOwner(t *testing.T) {
	ctrl := gomock.NewController(t)
	queries := dbmock.NewMockQuerier(ctrl)
	cfg := &config.Config{}
	cfg.Init.SchoolLogin = "OwnerLogin"
	s := &telegramService{cfg: cfg, queries: queries}

	queries.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform: db.EnumPlatformTelegram, ExternalID: "42001",
	}).Return(db.UserAccount{S21Login: " ownerlogin "}, nil)

	assert.True(t, s.isConfiguredBotOwner(context.Background(), 42001))
}

func TestIsConfiguredBotOwnerRejectsDifferentAccount(t *testing.T) {
	ctrl := gomock.NewController(t)
	queries := dbmock.NewMockQuerier(ctrl)
	cfg := &config.Config{}
	cfg.Init.SchoolLogin = "owner"
	s := &telegramService{cfg: cfg, queries: queries}

	queries.EXPECT().GetUserAccountByExternalId(gomock.Any(), gomock.Any()).Return(db.UserAccount{S21Login: "student"}, nil)

	assert.False(t, s.isConfiguredBotOwner(context.Background(), 42001))
}

func TestFormatMemberTagImportReportContainsOnlyAggregateValues(t *testing.T) {
	report := membertagimport.Report{
		TotalRows: 4062, AcceptedRows: 4046, SkippedInvalidRows: 2, SkippedConflictRows: 7,
		SkippedConflictIDs: 3, SkippedNullStatusRows: 5, SourceDigest: "secret-digest",
		Mappings: []membertagimport.Mapping{{TelegramUserID: 42001, Login: "private-login"}},
		Issues:   []membertagimport.Issue{{Line: 12, Code: "invalid_shape", SafeHash: "private-hash"}},
	}

	text := formatMemberTagImportReport(report, 101)

	require.Contains(t, text, "Строк: 4062")
	assert.Contains(t, text, "Принято: 4046")
	assert.Contains(t, text, "Поставлено в очередь: 101")
	assert.NotContains(t, text, "42001")
	assert.NotContains(t, text, "private-login")
	assert.NotContains(t, text, "secret-digest")
	assert.NotContains(t, text, "private-hash")
}
