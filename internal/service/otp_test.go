package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
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
	s21Login := "student1"

	t.Run("success", func(t *testing.T) {
		ctxWithS21 := context.WithValue(ctx, fsm.ContextKeyS21Login, s21Login)
		mockRepo.EXPECT().
			GetValidAuthVerificationCode(ctxWithS21, db.GetValidAuthVerificationCodeParams{
				S21Login: pgtype.Text{Valid: true, String: s21Login},
				Code:     "123456",
			}).
			Return(db.AuthVerificationCode{}, nil)
		mockRepo.EXPECT().
			DeleteAuthVerificationCode(ctxWithS21, db.DeleteAuthVerificationCodeParams{
				S21Login: pgtype.Text{Valid: true, String: s21Login},
				Code:     "123456",
			}).
			Return(nil)

		ok, err := svc.verifyOTP(ctxWithS21, 1, "123456")
		assert.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("S21 login not in context", func(t *testing.T) {
		ok, err := svc.verifyOTP(ctx, 1, "123456")
		assert.Error(t, err)
		assert.False(t, ok)
		assert.Contains(t, err.Error(), "S21 login not found")
	})

	t.Run("code not found", func(t *testing.T) {
		ctxWithS21 := context.WithValue(ctx, fsm.ContextKeyS21Login, s21Login)
		mockRepo.EXPECT().
			GetValidAuthVerificationCode(ctxWithS21, gomock.Any()).
			Return(db.AuthVerificationCode{}, &noRowsErr{})

		ok, err := svc.verifyOTP(ctxWithS21, 1, "000000")
		assert.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("database error", func(t *testing.T) {
		ctxWithS21 := context.WithValue(ctx, fsm.ContextKeyS21Login, s21Login)
		mockRepo.EXPECT().
			GetValidAuthVerificationCode(ctxWithS21, gomock.Any()).
			Return(db.AuthVerificationCode{}, assert.AnError)

		ok, err := svc.verifyOTP(ctxWithS21, 1, "111111")
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
	rcClient := rocketchat.NewClient("http://example.local/api/v1", "token", "user")
	svc := NewOTPService(mockRepo, rcClient, &config.Config{}, log)
	ctx := context.Background()

	// No previous OTP (no rows)
	mockRepo.EXPECT().
		GetLastAuthVerificationCode(ctx, gomock.Any()).
		Return(db.AuthVerificationCode{}, &noRowsErr{})
	mockRepo.EXPECT().
		DeleteAllAuthVerificationCodes(ctx, gomock.Any()).
		Return(nil)
	mockRepo.EXPECT().
		CreateAuthVerificationCode(ctx, gomock.Any()).
		Return(db.AuthVerificationCode{}, nil)
	// Student lookup fails
	mockRepo.EXPECT().
		GetRegisteredUserByS21Login(ctx, "unknown").
		Return(db.RegisteredUser{}, assert.AnError)

	err := svc.generateAndSendOTP(ctx, "unknown", fsm.UserInfo{ID: 1, Username: "u", Platform: "Telegram"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get registered user info")
}

func TestOTPService_GenerateAndSendOTP_RateLimited(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := NewOTPService(mockRepo, nil, &config.Config{}, log)
	ctx := context.Background()

	// Last OTP was created 30 seconds ago (within cooldown)
	mockRepo.EXPECT().
		GetLastAuthVerificationCode(ctx, gomock.Any()).
		Return(db.AuthVerificationCode{CreatedAt: pgtype.Timestamptz{Valid: true, Time: time.Now().Add(-30 * time.Second)}}, nil)

	err := svc.generateAndSendOTP(ctx, "student1", fsm.UserInfo{ID: 1, Username: "u", Platform: "Telegram"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "RATE_LIMIT:")
}

func TestOTPService_GenerateAndSendOTP_MissingRocketChatID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	rcClient := rocketchat.NewClient("http://example.local/api/v1", "token", "user")
	svc := NewOTPService(mockRepo, rcClient, &config.Config{}, log)
	ctx := context.Background()

	// No previous OTP
	mockRepo.EXPECT().
		GetLastAuthVerificationCode(ctx, gomock.Any()).
		Return(db.AuthVerificationCode{}, &noRowsErr{})
	mockRepo.EXPECT().
		DeleteAllAuthVerificationCodes(ctx, gomock.Any()).
		Return(nil)
	mockRepo.EXPECT().
		CreateAuthVerificationCode(ctx, gomock.Any()).
		Return(db.AuthVerificationCode{}, nil)
	// User has no RocketChat ID
	mockRepo.EXPECT().
		GetRegisteredUserByS21Login(ctx, "student1").
		Return(db.RegisteredUser{RocketchatID: ""}, nil)

	err := svc.generateAndSendOTP(ctx, "student1", fsm.UserInfo{ID: 1, Username: "u", Platform: "Telegram"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rocketchat_id")
}

func TestOTPService_GenerateAndSendOTP_EmailDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := NewOTPService(mockRepo, nil, &config.Config{}, log)
	ctx := context.WithValue(context.Background(), fsm.ContextKeyOTPDeliveryMethod, "email")

	mockRepo.EXPECT().
		GetLastAuthVerificationCode(ctx, gomock.Any()).
		Return(db.AuthVerificationCode{}, &noRowsErr{})
	mockRepo.EXPECT().
		DeleteAllAuthVerificationCodes(ctx, gomock.Any()).
		Return(nil)
	mockRepo.EXPECT().
		CreateAuthVerificationCode(ctx, gomock.Any()).
		Return(db.AuthVerificationCode{}, nil)

	err := svc.generateAndSendOTP(ctx, "student1", fsm.UserInfo{ID: 1, Username: "u", Platform: "Telegram"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "email otp is disabled")
}

func TestOTPService_RenderOTPEmailBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "otp_email.tmpl")
	err := os.WriteFile(templatePath, []byte("code={{ .Code }} login={{ .S21Login }} user={{ .TelegramUserID }}"), 0o600)
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.EmailOTP.TemplatePath = templatePath

	svc := NewOTPService(mockRepo, nil, cfg, log)
	body, err := svc.renderOTPEmailBody(
		"123456",
		"student1",
		"student1@student.21-school.ru",
		fsm.UserInfo{ID: 42, Username: "testuser", Platform: "Telegram"},
	)
	require.NoError(t, err)
	assert.Contains(t, body, "code=123456")
	assert.Contains(t, body, "login=student1")
	assert.Contains(t, body, "user=42")
}

func TestOTPService_RenderOTPEmailBody_EmbeddedTemplate(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := &config.Config{}
	cfg.EmailOTP.TemplatePath = "/app/internal/service/templates/otp_email.html.tmpl"

	svc := NewOTPService(mockRepo, nil, cfg, log)
	body, err := svc.renderOTPEmailBody(
		"654321",
		"student2",
		"student2@student.21-school.ru",
		fsm.UserInfo{ID: 100, Username: "embedded", Platform: "Telegram"},
	)
	require.NoError(t, err)
	assert.Contains(t, body, "654321")
	assert.Contains(t, body, "student2")
}

func TestEnsureCoalitionPresent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	ctx := context.Background()
	campusID := "ff19a3a7-12f5-4332-9582-624519c3eaea"
	var campusUUID pgtype.UUID
	require.NoError(t, campusUUID.Scan(campusID))

	t.Run("nil coalition", func(t *testing.T) {
		err := EnsureCoalitionPresent(ctx, mockQ, nil, "", nil, "", log)
		assert.NoError(t, err)
	})

	t.Run("zero coalition ID", func(t *testing.T) {
		coalition := &s21.ParticipantCoalitionV1DTO{CoalitionID: 0, CoalitionName: "Test"}
		err := EnsureCoalitionPresent(ctx, mockQ, nil, "", coalition, "", log)
		assert.NoError(t, err)
	})

	t.Run("already exists", func(t *testing.T) {
		coalition := &s21.ParticipantCoalitionV1DTO{CoalitionID: 5, CoalitionName: "Test Coalition"}
		mockQ.EXPECT().ExistsCoalitionByID(ctx, db.ExistsCoalitionByIDParams{
			CampusID: campusUUID,
			ID:       5,
		}).Return(true, nil)

		err := EnsureCoalitionPresent(ctx, mockQ, nil, "", coalition, campusID, log)
		assert.NoError(t, err)
	})

	t.Run("missing in db -> fetch campus coalitions and upsert all", func(t *testing.T) {
		coalition := &s21.ParticipantCoalitionV1DTO{CoalitionID: 5, CoalitionName: "Test Coalition"}

		gomock.InOrder(
			mockQ.EXPECT().ExistsCoalitionByID(ctx, db.ExistsCoalitionByIDParams{
				CampusID: campusUUID,
				ID:       5,
			}).Return(false, nil),
			mockQ.EXPECT().UpsertCoalition(ctx, db.UpsertCoalitionParams{CampusID: campusUUID, ID: 5, Name: "Blue"}).Return(nil),
			mockQ.EXPECT().UpsertCoalition(ctx, db.UpsertCoalitionParams{CampusID: campusUUID, ID: 7, Name: "Red"}).Return(nil),
		)

		mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(s21.CoalitionsResponse{
				Coalitions: []s21.CoalitionV1DTO{
					{CoalitionID: 5, Name: "Blue"},
					{CoalitionID: 7, Name: "Red"},
				},
			})
		})
		client := newMockS21Client(mockHandler)

		err := EnsureCoalitionPresent(ctx, mockQ, client, "token", coalition, campusID, log)
		assert.NoError(t, err)
	})

	t.Run("fetch error -> fallback to participant coalition upsert", func(t *testing.T) {
		coalition := &s21.ParticipantCoalitionV1DTO{CoalitionID: 5, CoalitionName: "Test Coalition"}
		gomock.InOrder(
			mockQ.EXPECT().ExistsCoalitionByID(ctx, db.ExistsCoalitionByIDParams{
				CampusID: campusUUID,
				ID:       5,
			}).Return(false, nil),
			mockQ.EXPECT().UpsertCoalition(ctx, db.UpsertCoalitionParams{CampusID: campusUUID, ID: 5, Name: "Test Coalition"}).Return(nil),
		)

		mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		client := newMockS21Client(mockHandler)

		err := EnsureCoalitionPresent(ctx, mockQ, client, "token", coalition, campusID, log)
		assert.NoError(t, err)
	})

	t.Run("fallback upsert error", func(t *testing.T) {
		coalition := &s21.ParticipantCoalitionV1DTO{CoalitionID: 5, CoalitionName: "Test Coalition"}
		gomock.InOrder(
			mockQ.EXPECT().ExistsCoalitionByID(ctx, db.ExistsCoalitionByIDParams{
				CampusID: campusUUID,
				ID:       5,
			}).Return(false, nil),
			mockQ.EXPECT().UpsertCoalition(ctx, db.UpsertCoalitionParams{CampusID: campusUUID, ID: 5, Name: "Test Coalition"}).Return(assert.AnError),
		)

		err := EnsureCoalitionPresent(ctx, mockQ, nil, "", coalition, campusID, log)
		assert.Error(t, err)
	})
}
