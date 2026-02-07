package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/crypto"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"go.uber.org/mock/gomock"
)

func TestNewCampusService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{}

	svc := NewCampusService(q, nil, cfg, log, nil)
	require.NotNil(t, svc)
}

func TestCampusService_Start(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{}

	svc := NewCampusService(q, nil, cfg, log, nil)
	err := svc.Start()
	assert.NoError(t, err)
	svc.cron.Stop()
}

func TestCampusService_UpdateCampuses_noLogin(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{} // SchoolLogin empty

	svc := NewCampusService(q, nil, cfg, log, nil)
	err := svc.UpdateCampuses(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SCHOOL21_USER_LOGIN")
}

func TestCampusService_UpdateCampuses_getCredsFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	mockRepo.EXPECT().
		GetPlatformCredentials(gomock.Any(), "school").
		Return(db.PlatformCredential{}, fmt.Errorf("no rows"))

	cfg := &config.Config{}
	cfg.Init.SchoolLogin = "school"
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := NewCampusService(mockRepo, nil, cfg, log, nil)

	err := svc.UpdateCampuses(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get platform credentials")
}

func TestCampusService_UpdateCampuses_decryptFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	hexKey := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	crypter, err := crypto.NewCrypter(hexKey)
	require.NoError(t, err)
	// Tampered credentials so Decrypt fails
	creds := db.PlatformCredential{
		StudentID:     "school",
		PasswordEnc:   []byte("bad"),
		PasswordNonce: []byte("bad"),
	}

	mockRepo := mock.NewMockQuerier(ctrl)
	mockRepo.EXPECT().
		GetPlatformCredentials(gomock.Any(), "school").
		Return(creds, nil)

	cfg := &config.Config{}
	cfg.Init.SchoolLogin = "school"
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	seeder := NewCredentialSeeder(mockRepo, crypter, nil, log)
	svc := NewCampusService(mockRepo, nil, cfg, log, seeder)

	err = svc.UpdateCampuses(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decrypt")
}
