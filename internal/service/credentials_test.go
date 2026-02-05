package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/crypto"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"go.uber.org/mock/gomock"
)

type fakeS21Client struct {
	authResp       *s21.AuthResponse
	authErr        error
	participant    *s21.ParticipantV1DTO
	participantErr error
}

func (f *fakeS21Client) Auth(ctx context.Context, username, password string) (*s21.AuthResponse, error) {
	if f.authErr != nil {
		return nil, f.authErr
	}
	return f.authResp, nil
}

func (f *fakeS21Client) GetParticipant(ctx context.Context, token, login string) (*s21.ParticipantV1DTO, error) {
	if f.participantErr != nil {
		return nil, f.participantErr
	}
	return f.participant, nil
}

func TestCredentialSeeder_Verify(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	hexKey := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	crypter, err := crypto.NewCrypter(hexKey)
	require.NoError(t, err)

	plainPwd := []byte("secret")
	aad := []byte("alice")
	enc, nonce, err := crypter.Encrypt(plainPwd, aad)
	require.NoError(t, err)

	creds := db.PlatformCredential{
		StudentID:     "alice",
		PasswordEnc:   enc,
		PasswordNonce: nonce,
	}

	mockRepo := mock.NewMockQuerier(ctrl)
	mockRepo.EXPECT().
		GetPlatformCredentials(gomock.Any(), "alice").
		Return(creds, nil)

	s21Fake := &fakeS21Client{
		authResp: &s21.AuthResponse{AccessToken: "tok"},
		participant: &s21.ParticipantV1DTO{
			Login:        "alice",
			Status:       "ACTIVE",
			ParallelName: "Core program",
		},
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	seeder := NewCredentialSeeder(mockRepo, crypter, s21Fake, log)

	err = seeder.Verify(context.Background(), "alice")
	assert.NoError(t, err)
}

func TestCredentialSeeder_Verify_getCredsFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	crypter, _ := crypto.NewCrypter("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	mockRepo := mock.NewMockQuerier(ctrl)
	mockRepo.EXPECT().
		GetPlatformCredentials(gomock.Any(), "bob").
		Return(db.PlatformCredential{}, fmt.Errorf("db error"))

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	seeder := NewCredentialSeeder(mockRepo, crypter, &fakeS21Client{}, log)

	err := seeder.Verify(context.Background(), "bob")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "platform credentials")
}

func TestCredentialSeeder_Verify_authFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	hexKey := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	crypter, _ := crypto.NewCrypter(hexKey)
	enc, nonce, _ := crypter.Encrypt([]byte("pwd"), []byte("u"))
	creds := db.PlatformCredential{StudentID: "u", PasswordEnc: enc, PasswordNonce: nonce}

	mockRepo := mock.NewMockQuerier(ctrl)
	mockRepo.EXPECT().GetPlatformCredentials(gomock.Any(), "u").Return(creds, nil)

	s21Fake := &fakeS21Client{authErr: fmt.Errorf("auth failed")}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	seeder := NewCredentialSeeder(mockRepo, crypter, s21Fake, log)

	err := seeder.Verify(context.Background(), "u")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "authentication")
}

func TestCredentialSeeder_Seed_noLogin(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	seeder := NewCredentialSeeder(mock.NewMockQuerier(ctrl), nil, &fakeS21Client{}, log)

	cfg := &config.Config{}
	err := seeder.Seed(context.Background(), cfg)
	assert.NoError(t, err)
}

func TestCredentialSeeder_Seed_newStudent_missingRocketChatID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	mockRepo.EXPECT().
		GetStudentByS21Login(gomock.Any(), "newuser").
		Return(db.Student{}, pgx.ErrNoRows)

	cfg := &config.Config{}
	cfg.Init.SchoolLogin = "newuser"
	cfg.Init.SchoolPassword = config.Secret("")
	cfg.RocketChat.UserID = config.Secret("") // Empty RocketChat user ID
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	seeder := NewCredentialSeeder(mockRepo, nil, &fakeS21Client{}, log)

	err := seeder.Seed(context.Background(), cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ROCKETCHAT_USER_ID")
}

func TestCredentialSeeder_Seed_studentExists_noPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	mockRepo.EXPECT().
		GetStudentByS21Login(gomock.Any(), "existing").
		Return(db.Student{S21Login: "existing"}, nil)

	cfg := &config.Config{}
	cfg.Init.SchoolLogin = "existing"
	cfg.Init.SchoolPassword = config.Secret("")
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	seeder := NewCredentialSeeder(mockRepo, nil, &fakeS21Client{}, log)

	err := seeder.Seed(context.Background(), cfg)
	assert.NoError(t, err)
}

func TestCredentialSeeder_Seed_upsertPlatformCreds(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	hexKey := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	crypter, _ := crypto.NewCrypter(hexKey)

	mockRepo := mock.NewMockQuerier(ctrl)
	mockRepo.EXPECT().
		GetStudentByS21Login(gomock.Any(), "stu").
		Return(db.Student{S21Login: "stu"}, nil)
	mockRepo.EXPECT().
		GetPlatformCredentials(gomock.Any(), "stu").
		Return(db.PlatformCredential{}, pgx.ErrNoRows)
	mockRepo.EXPECT().
		UpsertPlatformCredentials(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, arg db.UpsertPlatformCredentialsParams) error {
			assert.Equal(t, "stu", arg.StudentID)
			assert.NotEmpty(t, arg.PasswordEnc)
			assert.NotEmpty(t, arg.PasswordNonce)
			return nil
		})

	cfg := &config.Config{}
	cfg.Init.SchoolLogin = "stu"
	cfg.Init.SchoolPassword = config.Secret("mypassword")
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	seeder := NewCredentialSeeder(mockRepo, crypter, &fakeS21Client{}, log)

	err := seeder.Seed(context.Background(), cfg)
	assert.NoError(t, err)
}

func TestCredentialSeeder_Seed_upsertRocketChat(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	crypter, _ := crypto.NewCrypter("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")

	mockRepo := mock.NewMockQuerier(ctrl)
	mockRepo.EXPECT().GetStudentByS21Login(gomock.Any(), "u").Return(db.Student{S21Login: "u"}, nil)
	mockRepo.EXPECT().GetPlatformCredentials(gomock.Any(), "u").Return(db.PlatformCredential{}, pgx.ErrNoRows)
	mockRepo.EXPECT().UpsertPlatformCredentials(gomock.Any(), gomock.Any()).Return(nil)
	mockRepo.EXPECT().
		UpsertRocketChatCredentials(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, arg db.UpsertRocketChatCredentialsParams) error {
			assert.Equal(t, "u", arg.StudentID)
			assert.NotEmpty(t, arg.RcTokenEnc)
			return nil
		})

	cfg := &config.Config{}
	cfg.Init.SchoolLogin = "u"
	cfg.Init.SchoolPassword = config.Secret("p")
	cfg.RocketChat.AuthToken = config.Secret("rctoken")
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	seeder := NewCredentialSeeder(mockRepo, crypter, &fakeS21Client{}, log)

	err := seeder.Seed(context.Background(), cfg)
	assert.NoError(t, err)
}

func TestCredentialSeeder_Seed_newStudent_upsertStudent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	mockRepo.EXPECT().
		GetStudentByS21Login(gomock.Any(), "newbie").
		Return(db.Student{}, pgx.ErrNoRows)
	mockRepo.EXPECT().
		UpsertStudent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, arg db.UpsertStudentParams) (db.Student, error) {
			assert.Equal(t, "newbie", arg.S21Login)
			assert.Equal(t, "rc-id", arg.RocketchatID)
			return db.Student{}, nil
		})

	cfg := &config.Config{}
	cfg.Init.SchoolLogin = "newbie"
	cfg.RocketChat.UserID = config.Secret("rc-id")
	cfg.Init.SchoolPassword = config.Secret("")
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	seeder := NewCredentialSeeder(mockRepo, nil, &fakeS21Client{}, log)

	err := seeder.Seed(context.Background(), cfg)
	assert.NoError(t, err)
}
