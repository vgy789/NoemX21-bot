package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"go.uber.org/mock/gomock"
)

// mockQuerier implements db.Querier by wrapping a mock.MockQuerier
type mockQuerier struct {
	mock *mock.MockQuerier
}

func (m *mockQuerier) CreateUserAccount(ctx context.Context, arg db.CreateUserAccountParams) (db.UserAccount, error) {
	return m.mock.CreateUserAccount(ctx, arg)
}

func (m *mockQuerier) CreateAuthVerificationCode(ctx context.Context, arg db.CreateAuthVerificationCodeParams) (db.AuthVerificationCode, error) {
	return m.mock.CreateAuthVerificationCode(ctx, arg)
}

func (m *mockQuerier) DeleteAuthVerificationCode(ctx context.Context, arg db.DeleteAuthVerificationCodeParams) error {
	return m.mock.DeleteAuthVerificationCode(ctx, arg)
}

func (m *mockQuerier) DeleteExpiredAuthVerificationCodes(ctx context.Context) error {
	return m.mock.DeleteExpiredAuthVerificationCodes(ctx)
}

func (m *mockQuerier) GetPlatformCredentials(ctx context.Context, studentID string) (db.PlatformCredential, error) {
	return m.mock.GetPlatformCredentials(ctx, studentID)
}

func (m *mockQuerier) GetRocketChatCredentials(ctx context.Context, studentID string) (db.RocketchatCredential, error) {
	return m.mock.GetRocketChatCredentials(ctx, studentID)
}

func (m *mockQuerier) GetFSMState(ctx context.Context, userID int64) (db.FsmUserState, error) {
	return m.mock.GetFSMState(ctx, userID)
}

func (m *mockQuerier) UpsertFSMState(ctx context.Context, arg db.UpsertFSMStateParams) error {
	return m.mock.UpsertFSMState(ctx, arg)
}

func (m *mockQuerier) GetStudentByRocketChatId(ctx context.Context, rocketchatID string) (db.Student, error) {
	return m.mock.GetStudentByRocketChatId(ctx, rocketchatID)
}

func (m *mockQuerier) GetStudentByS21Login(ctx context.Context, s21Login string) (db.Student, error) {
	return m.mock.GetStudentByS21Login(ctx, s21Login)
}

func (m *mockQuerier) GetStudentProfile(ctx context.Context, s21Login string) (db.GetStudentProfileRow, error) {
	return m.mock.GetStudentProfile(ctx, s21Login)
}

func (m *mockQuerier) GetLastAuthVerificationCode(ctx context.Context, studentID pgtype.Text) (db.AuthVerificationCode, error) {
	return m.mock.GetLastAuthVerificationCode(ctx, studentID)
}

func (m *mockQuerier) GetUserAccountByExternalId(ctx context.Context, arg db.GetUserAccountByExternalIdParams) (db.UserAccount, error) {
	return m.mock.GetUserAccountByExternalId(ctx, arg)
}

func (m *mockQuerier) GetUserAccountByStudentId(ctx context.Context, studentID string) (db.UserAccount, error) {
	return m.mock.GetUserAccountByStudentId(ctx, studentID)
}

func (m *mockQuerier) GetUserBotSettings(ctx context.Context, userAccountID int64) (db.UserBotSetting, error) {
	return m.mock.GetUserBotSettings(ctx, userAccountID)
}

func (m *mockQuerier) GetValidAuthVerificationCode(ctx context.Context, arg db.GetValidAuthVerificationCodeParams) (db.AuthVerificationCode, error) {
	return m.mock.GetValidAuthVerificationCode(ctx, arg)
}

func (m *mockQuerier) UpsertPlatformCredentials(ctx context.Context, arg db.UpsertPlatformCredentialsParams) error {
	return m.mock.UpsertPlatformCredentials(ctx, arg)
}

func (m *mockQuerier) UpsertRocketChatCredentials(ctx context.Context, arg db.UpsertRocketChatCredentialsParams) error {
	return m.mock.UpsertRocketChatCredentials(ctx, arg)
}

func (m *mockQuerier) UpsertStudent(ctx context.Context, arg db.UpsertStudentParams) (db.Student, error) {
	return m.mock.UpsertStudent(ctx, arg)
}

func (m *mockQuerier) CreateApiKey(ctx context.Context, arg db.CreateApiKeyParams) (db.ApiKey, error) {
	return m.mock.CreateApiKey(ctx, arg)
}

func (m *mockQuerier) GetApiKeyByHash(ctx context.Context, keyHash string) (db.ApiKey, error) {
	return m.mock.GetApiKeyByHash(ctx, keyHash)
}

func (m *mockQuerier) RevokeOldApiKeys(ctx context.Context, userAccountID int64) error {
	return m.mock.RevokeOldApiKeys(ctx, userAccountID)
}

func (m *mockQuerier) GetActiveApiKey(ctx context.Context, userAccountID int64) (db.ApiKey, error) {
	return m.mock.GetActiveApiKey(ctx, userAccountID)
}

func (m *mockQuerier) DeleteAllAuthVerificationCodes(ctx context.Context, studentID pgtype.Text) error {
	return m.mock.DeleteAllAuthVerificationCodes(ctx, studentID)
}

func (m *mockQuerier) UpsertUserBotSettings(ctx context.Context, arg db.UpsertUserBotSettingsParams) (db.UserBotSetting, error) {
	return m.mock.UpsertUserBotSettings(ctx, arg)
}

func (m *mockQuerier) DeactivateClubsByCampus(ctx context.Context, campusID pgtype.UUID) error {
	return m.mock.DeactivateClubsByCampus(ctx, campusID)
}

func (m *mockQuerier) GetCampusByShortName(ctx context.Context, shortName string) (db.Campuse, error) {
	return m.mock.GetCampusByShortName(ctx, shortName)
}

func (m *mockQuerier) UpsertClub(ctx context.Context, arg db.UpsertClubParams) (db.Club, error) {
	return m.mock.UpsertClub(ctx, arg)
}

func (m *mockQuerier) UpsertClubCategory(ctx context.Context, name string) (db.ClubCategory, error) {
	return m.mock.UpsertClubCategory(ctx, name)
}

func (m *mockQuerier) UpsertCampus(ctx context.Context, arg db.UpsertCampusParams) (db.Campuse, error) {
	return m.mock.UpsertCampus(ctx, arg)
}

func TestStudentService_GetProfileByTelegramID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	svc := NewStudentService(&mockQuerier{mock: mockRepo})
	ctx := context.Background()
	tgID := int64(12345)

	t.Run("success", func(t *testing.T) {
		mockRepo.EXPECT().
			GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
				Platform:   db.EnumPlatformTelegram,
				ExternalID: "12345",
			}).
			Return(db.UserAccount{StudentID: "peer-login"}, nil)

		mockRepo.EXPECT().
			GetStudentProfile(ctx, "peer-login").
			Return(db.GetStudentProfileRow{
				S21Login:      "peer-login",
				CampusName:    pgtype.Text{String: "Novisibirsk", Valid: true},
				CoalitionName: pgtype.Text{String: "Sapphires", Valid: true},
				Level:         pgtype.Int4{Int32: 15, Valid: true},
				ExpValue:      pgtype.Int4{Int32: 10000, Valid: true},
				Prp:           pgtype.Int4{Int32: 10, Valid: true},
				Crp:           pgtype.Int4{Int32: 5, Valid: true},
				Coins:         pgtype.Int4{Int32: 500, Valid: true},
			}, nil)

		profile, err := svc.GetProfileByTelegramID(ctx, tgID)
		assert.NoError(t, err)
		assert.NotNil(t, profile)
		assert.Equal(t, "peer-login", profile.Login)
		assert.Equal(t, "Novisibirsk", profile.CampusName)
		assert.Equal(t, int32(15), profile.Level)
	})

	t.Run("user account not found", func(t *testing.T) {
		mockRepo.EXPECT().
			GetUserAccountByExternalId(ctx, gomock.Any()).
			Return(db.UserAccount{}, fmt.Errorf("not found"))

		profile, err := svc.GetProfileByTelegramID(ctx, tgID)
		assert.Error(t, err)
		assert.Nil(t, profile)
		assert.Contains(t, err.Error(), "user account not found")
	})

	t.Run("student profile not found", func(t *testing.T) {
		mockRepo.EXPECT().
			GetUserAccountByExternalId(ctx, gomock.Any()).
			Return(db.UserAccount{StudentID: "peer-login"}, nil)

		mockRepo.EXPECT().
			GetStudentProfile(ctx, "peer-login").
			Return(db.GetStudentProfileRow{}, fmt.Errorf("not found"))

		profile, err := svc.GetProfileByTelegramID(ctx, tgID)
		assert.Error(t, err)
		assert.Nil(t, profile)
		assert.Contains(t, err.Error(), "student profile not found")
	})
}
