package service

import (
	"context"
	"fmt"
	"log/slog"

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

type CredentialSeeder struct {
	repo      db.Querier
	crypter   *crypto.Crypter
	s21Client S21Client
	log       *slog.Logger
}

func NewCredentialSeeder(repo db.Querier, crypter *crypto.Crypter, s21Client S21Client, log *slog.Logger) *CredentialSeeder {
	return &CredentialSeeder{repo: repo, crypter: crypter, s21Client: s21Client, log: log}
}

// Verify retrieves credentials from DB, decrypts them, and checks the user via S21 API.
func (s *CredentialSeeder) Verify(ctx context.Context, login string) error {
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
	if participant.Status == "ACTIVE" && participant.ParallelName == "Core program" {
		s.log.Info("User verification successful",
			"login", login,
			"status", participant.Status,
			"parallel", participant.ParallelName)
	} else {
		s.log.Warn("User verification failed: criteria not met",
			"login", login,
			"status", participant.Status,
			"parallel", participant.ParallelName,
			"expected_status", "ACTIVE",
			"expected_parallel", "Core program")
	}

	return nil
}

// Seed encrypts and stores the initial credentials from configuration.
func (s *CredentialSeeder) Seed(ctx context.Context, cfg *config.Config) error {
	if cfg.Init.SchoolLogin == "" {
		s.log.Info("No SCHOOL21_USER_LOGIN provided, skipping seeding credentials")
		return nil
	}

	s.log.Info("Seeding credentials for user", "login", cfg.Init.SchoolLogin)

	// 1. Ensure Student Record Exists
	_, err := s.repo.GetStudentByS21Login(ctx, cfg.Init.SchoolLogin)
	if err != nil {
		if err == pgx.ErrNoRows {
			// Create new student
			if cfg.Init.RocketChatUserID == "" {
				return fmt.Errorf("cannot create student: ROCKETCHAT_USER_ID is missing")
			}

			s.log.Info("Creating new student record", "login", cfg.Init.SchoolLogin)
			// Assuming other fields can be default/null.
			// Status defaults to ACTIVE.
			// Timezone defaults to UTC (if not provided, but schema says NOT NULL DEFAULT 'UTC')
			// But UpsertStudentParams requires Timezone string.

			upsertParams := db.UpsertStudentParams{
				S21Login:     cfg.Init.SchoolLogin,
				RocketchatID: cfg.Init.RocketChatUserID,
				Status:       db.NullEnumStudentStatus{Valid: true, EnumStudentStatus: db.EnumStudentStatusACTIVE},
				Timezone:     "UTC", // Default
				// Others are nullable or have defaults handled by DB if we assume, but params struct requires them?
				// pgtype.UUID, pgtype.Int2 are nullable values.
				CampusID:           pgtype.UUID{Valid: false},
				CoalitionID:        pgtype.Int2{Valid: false},
				AlternativeContact: pgtype.Text{Valid: false},
				HasCoffeeBan:       pgtype.Bool{Valid: false},
			}

			_, err = s.repo.UpsertStudent(ctx, upsertParams)
			if err != nil {
				return fmt.Errorf("failed to create student: %w", err)
			}
		} else {
			return fmt.Errorf("failed to check student existence: %w", err)
		}
	} else {
		s.log.Info("Student record already exists", "login", cfg.Init.SchoolLogin)
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
				StudentID:        cfg.Init.SchoolLogin,
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
				StudentID:     cfg.Init.SchoolLogin,
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
	rcTokenPlain := cfg.Init.RocketChatToken.Expose()
	if rcTokenPlain != "" {
		rcEnc, rcNonce, err := s.crypter.Encrypt([]byte(rcTokenPlain), []byte(cfg.Init.SchoolLogin))
		if err != nil {
			return fmt.Errorf("failed to encrypt RC token: %w", err)
		}

		// RocketChat credentials table only has the token, so simpler.
		// But still good to allow for future expansion if more fields added.
		// Currently UpsertRocketChatCredentials sets everything.

		rcParams := db.UpsertRocketChatCredentialsParams{
			StudentID:  cfg.Init.SchoolLogin,
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
