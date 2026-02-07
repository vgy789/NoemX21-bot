package service

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"go.uber.org/mock/gomock"
)

func TestNewOTPService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{}

	svc := NewOTPService(q, nil, cfg, log)
	require.NotNil(t, svc)
}

func TestOTPService_CleanupExpiredCodes(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := NewOTPService(mockRepo, nil, &config.Config{}, log)
	ctx := context.Background()

	mockRepo.EXPECT().
		DeleteExpiredAuthVerificationCodes(ctx).
		Return(nil)

	err := svc.CleanupExpiredCodes(ctx)
	assert.NoError(t, err)
}

func TestOTPService_VerifyOTP(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := NewOTPService(mockRepo, nil, &config.Config{}, log)
	ctx := context.Background()
	studentID := "student1"

	t.Run("success", func(t *testing.T) {
		ctxWithStudent := context.WithValue(ctx, fsm.ContextKeyStudentID, studentID)
		mockRepo.EXPECT().
			GetValidAuthVerificationCode(ctxWithStudent, db.GetValidAuthVerificationCodeParams{
				StudentID: pgtype.Text{Valid: true, String: studentID},
				Code:      "123456",
			}).
			Return(db.AuthVerificationCode{}, nil)
		mockRepo.EXPECT().
			DeleteAuthVerificationCode(ctxWithStudent, db.DeleteAuthVerificationCodeParams{
				StudentID: pgtype.Text{Valid: true, String: studentID},
				Code:      "123456",
			}).
			Return(nil)

		ok, err := svc.VerifyOTP(ctxWithStudent, 1, "123456")
		assert.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("student ID not in context", func(t *testing.T) {
		ok, err := svc.VerifyOTP(ctx, 1, "123456")
		assert.Error(t, err)
		assert.False(t, ok)
		assert.Contains(t, err.Error(), "student ID not found")
	})

	t.Run("code not found", func(t *testing.T) {
		ctxWithStudent := context.WithValue(ctx, fsm.ContextKeyStudentID, studentID)
		mockRepo.EXPECT().
			GetValidAuthVerificationCode(ctxWithStudent, gomock.Any()).
			Return(db.AuthVerificationCode{}, &noRowsErr{})

		ok, err := svc.VerifyOTP(ctxWithStudent, 1, "000000")
		assert.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("database error", func(t *testing.T) {
		ctxWithStudent := context.WithValue(ctx, fsm.ContextKeyStudentID, studentID)
		mockRepo.EXPECT().
			GetValidAuthVerificationCode(ctxWithStudent, gomock.Any()).
			Return(db.AuthVerificationCode{}, assert.AnError)

		ok, err := svc.VerifyOTP(ctxWithStudent, 1, "111111")
		assert.Error(t, err)
		assert.False(t, ok)
		assert.Contains(t, err.Error(), "database error")
	})
}

type noRowsErr struct{}

func (e *noRowsErr) Error() string {
	return "no rows in result set"
}

func TestOTPService_GenerateAndSendOTP_studentNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := NewOTPService(mockRepo, nil, &config.Config{}, log)
	ctx := context.Background()

	// No previous OTP (no rows)
	mockRepo.EXPECT().
		GetLastAuthVerificationCode(ctx, gomock.Any()).
		Return(db.AuthVerificationCode{}, &noRowsErr{})
	// Student lookup fails
	mockRepo.EXPECT().
		GetStudentByS21Login(ctx, "unknown").
		Return(db.Student{}, assert.AnError)

	err := svc.GenerateAndSendOTP(ctx, "unknown", fsm.UserInfo{ID: 1, Username: "u", Platform: "Telegram"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get student info")
}
