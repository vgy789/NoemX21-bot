package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/crypto"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

type S21Client interface {
	Auth(ctx context.Context, username, password string) (*s21.AuthResponse, error)
	GetParticipant(ctx context.Context, token string, login string) (*s21.ParticipantV1DTO, error)
}

type CredentialService struct {
	repo      db.Querier
	crypter   *crypto.Crypter
	s21Client S21Client
	log       *slog.Logger
}

func NewCredentialService(repo db.Querier, crypter *crypto.Crypter, s21Client S21Client, log *slog.Logger) *CredentialService {
	return &CredentialService{repo: repo, crypter: crypter, s21Client: s21Client, log: log}
}

// GetValidToken retrieves a valid access token for the user, refreshing or re-authenticating if needed.
func (s *CredentialService) GetValidToken(ctx context.Context, login string) (string, error) {
	// 1. Check existing token
	creds, err := s.repo.GetPlatformCredentials(ctx, login)
	if err == nil {
		if creds.AccessToken.Valid && creds.AccessExpiresAt.Time.After(time.Now().Add(time.Minute)) {
			// Token is valid and has at least 1 minute remaining
			return creds.AccessToken.String, nil
		}
	} else if err != pgx.ErrNoRows {
		return "", fmt.Errorf("failed to get credentials: %w", err)
	}

	// 2. Token invalid or missing, need to authenticate
	// We need the password. If we don't have creds record, we can't do anything unless we have a separate way (like passed in),
	// but here we rely on DB storage.
	if err == pgx.ErrNoRows {
		return "", fmt.Errorf("no credentials found for %s", login)
	}

	if len(creds.PasswordEnc) == 0 {
		return "", fmt.Errorf("no password stored for %s", login)
	}

	decryptedPassword, err := s.crypter.Decrypt(creds.PasswordEnc, creds.PasswordNonce, []byte(login))
	if err != nil {
		return "", fmt.Errorf("failed to decrypt password: %w", err)
	}

	// 3. Authenticate
	authResp, err := s.s21Client.Auth(ctx, login, string(decryptedPassword))
	if err != nil {
		return "", fmt.Errorf("authentication failed: %w", err)
	}

	// 4. Save new token
	expiration := time.Now().Add(time.Duration(authResp.ExpiresIn) * time.Second)
	refreshExpiration := time.Now().Add(time.Duration(authResp.RefreshExpiresIn) * time.Second)

	err = s.repo.UpsertPlatformCredentials(ctx, db.UpsertPlatformCredentialsParams{
		S21Login:         login,
		PasswordEnc:      creds.PasswordEnc,   // Keep existing
		PasswordNonce:    creds.PasswordNonce, // Keep existing
		AccessToken:      pgtype.Text{String: authResp.AccessToken, Valid: true},
		AccessExpiresAt:  pgtype.Timestamptz{Time: expiration, Valid: true},
		RefreshTokenEnc:  nil, // We are not storing refresh token encrypted yet, or maybe we should? api doesn't give refresh token?
		RefreshNonce:     nil, // The auth response likely has refresh token, but let's stick to access for now.
		RefreshExpiresAt: pgtype.Timestamptz{Time: refreshExpiration, Valid: true},
	})
	if err != nil {
		s.log.Error("failed to save new token", "login", login, "error", err)
		// Proceed anyway since we have the token
	}

	return authResp.AccessToken, nil
}

// Verify retrieves credentials from DB, decrypts them, and checks the user via S21 API.
func (s *CredentialService) Verify(ctx context.Context, login string) error {
	s.log.Info("Starting AEAD decryption and API verification", "login", login)

	// 1. Retrieve Platform Credentials
	creds, err := s.repo.GetPlatformCredentials(ctx, login)
	if err != nil {
		return fmt.Errorf("failed to get platform credentials from DB: %w", err)
	}

	// 2. Decrypt Password
	decryptedPassword, err := s.crypter.Decrypt(creds.PasswordEnc, creds.PasswordNonce, []byte(login))
	if err != nil {
		return fmt.Errorf("AEAD password decryption failed: %w", err)
	}
	// Don't log password

	// 3. Authenticate to get Access Token
	authResp, err := s.s21Client.Auth(ctx, login, string(decryptedPassword))
	if err != nil {
		s.log.Error("Authentication failed", "login", login, "error", err)
		return fmt.Errorf("authentication failed: %w", err)
	}
	s.log.Info("Authentication successful", "login", login)

	// 4. Verify/Call S21 API
	participant, err := s.s21Client.GetParticipant(ctx, authResp.AccessToken, login)
	if err != nil {
		s.log.Error("API verification failed", "login", login, "error", err)
		return fmt.Errorf("API verification failed: %w", err)
	}

	// 5. Validate status and parallelName
	parallelName := ""
	if participant.ParallelName != nil {
		parallelName = *participant.ParallelName
	}

	if participant.Status == "ACTIVE" && parallelName == "Core program" {
		s.log.Info("User verification successful",
			"login", login,
			"status", participant.Status,
			"parallel", parallelName)
	} else {
		s.log.Warn("User verification failed: criteria not met",
			"login", login,
			"status", participant.Status,
			"parallel", parallelName,
			"expected_status", "ACTIVE",
			"expected_parallel", "Core program")
	}

	return nil
}

// Seed encrypts and stores the initial credentials from configuration.
func (s *CredentialService) Seed(ctx context.Context, cfg *config.Config) error {
	if cfg.Init.SchoolLogin == "" {
		s.log.Info("No SCHOOL21_USER_LOGIN provided, skipping seeding credentials")
		return nil
	}

	s.log.Info("Seeding credentials for user", "login", cfg.Init.SchoolLogin)

	// 1. Ensure Registered User Record Exists
	_, err := s.repo.GetRegisteredUserByS21Login(ctx, cfg.Init.SchoolLogin)
	if err != nil {
		if err == pgx.ErrNoRows {
			// Create new registered user
			if cfg.RocketChat.UserID.Expose() == "" {
				return fmt.Errorf("cannot create registered user: ROCKETCHAT_USER_ID is missing")
			}

			s.log.Info("Creating new registered user record", "login", cfg.Init.SchoolLogin)

			upsertParams := db.UpsertRegisteredUserParams{
				S21Login:           cfg.Init.SchoolLogin,
				RocketchatID:       cfg.RocketChat.UserID.Expose(),
				Timezone:           "UTC",
				AlternativeContact: pgtype.Text{Valid: false},
				HasCoffeeBan:       pgtype.Bool{Valid: false},
			}

			_, err = s.repo.UpsertRegisteredUser(ctx, upsertParams)
			if err != nil {
				return fmt.Errorf("failed to create registered user: %w", err)
			}
		} else {
			return fmt.Errorf("failed to check registered user existence: %w", err)
		}
	} else {
		s.log.Info("Registered user record already exists", "login", cfg.Init.SchoolLogin)
	}

	// 2. Encrypt and Upsert Platform Credentials (School21)
	pwdPlain := cfg.Init.SchoolPassword.Expose()
	if pwdPlain != "" {
		pwdEnc, pwdNonce, err := s.crypter.Encrypt([]byte(pwdPlain), []byte(cfg.Init.SchoolLogin))
		if err != nil {
			return fmt.Errorf("failed to encrypt school password: %w", err)
		}

		// Check existing to preserve other fields
		existing, err := s.repo.GetPlatformCredentials(ctx, cfg.Init.SchoolLogin)
		var params db.UpsertPlatformCredentialsParams
		if err == nil {
			// Found existing, check what to preserve
			// Map existing to params
			params = db.UpsertPlatformCredentialsParams{
				S21Login:         cfg.Init.SchoolLogin,
				PasswordEnc:      pwdEnc,   // Update this
				PasswordNonce:    pwdNonce, // Update this
				AccessToken:      existing.AccessToken,
				AccessExpiresAt:  existing.AccessExpiresAt,
				RefreshTokenEnc:  existing.RefreshTokenEnc,
				RefreshNonce:     existing.RefreshNonce,
				RefreshExpiresAt: existing.RefreshExpiresAt,
			}
		} else {
			if err != pgx.ErrNoRows {
				return fmt.Errorf("failed to get platform credentials: %w", err)
			}
			// Not found, create new with just password
			params = db.UpsertPlatformCredentialsParams{
				S21Login:      cfg.Init.SchoolLogin,
				PasswordEnc:   pwdEnc,
				PasswordNonce: pwdNonce,
				// Others nil/invalid
			}
		}

		err = s.repo.UpsertPlatformCredentials(ctx, params)
		if err != nil {
			return fmt.Errorf("failed to upsert platform credentials: %w", err)
		}
		s.log.Info("Updated platform credentials")
	}

	// 3. Encrypt and Upsert RocketChat Credentials
	rcTokenPlain := cfg.RocketChat.AuthToken.Expose()
	if rcTokenPlain != "" {
		rcEnc, rcNonce, err := s.crypter.Encrypt([]byte(rcTokenPlain), []byte(cfg.Init.SchoolLogin))
		if err != nil {
			return fmt.Errorf("failed to encrypt RC token: %w", err)
		}

		// RocketChat credentials table only has the token, so simpler.
		// But still good to allow for future expansion if more fields added.
		// Currently UpsertRocketChatCredentials sets everything.

		rcParams := db.UpsertRocketChatCredentialsParams{
			S21Login:   cfg.Init.SchoolLogin,
			RcTokenEnc: rcEnc,
			RcNonce:    rcNonce,
		}

		err = s.repo.UpsertRocketChatCredentials(ctx, rcParams)
		if err != nil {
			return fmt.Errorf("failed to upsert RC credentials: %w", err)
		}
		s.log.Info("Updated RocketChat credentials")
	}

	return nil
}
