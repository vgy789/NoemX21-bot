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

func TestStudentService_GetProfileByTelegramID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	svc := NewStudentService(mockRepo)
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
