package initialization

import (
	"context"
	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/crypto"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/service"
)

// InitializeDatabase initializes the database connection.
func InitializeDatabase(ctx context.Context, dbURL string, log *slog.Logger) (*db.DBWrapper, error) {
	pool, err := db.Connect(ctx, dbURL)
	if err != nil {
		log.Error("failed to connect to database", "error", err)
		return nil, err
	}
	return db.NewDBWrapper(pool), nil
}

// InitializeCrypter initializes the crypter with the given key.
func InitializeCrypter(aeadKey string, log *slog.Logger) (*crypto.Crypter, error) {
	crypter, err := crypto.NewCrypter(aeadKey)
	if err != nil {
		log.Error("failed to initialize crypter", "error", err)
		return nil, err
	}
	return crypter, nil
}

// InitializeS21Client initializes the S21 client.
func InitializeS21Client(s21BaseURL string) *s21.Client {
	return s21.NewClient(s21BaseURL)
}

// InitializeRocketChatClient initializes the RocketChat client.
func InitializeRocketChatClient(cfg *config.Config) *rocketchat.Client {
	return rocketchat.NewClient(cfg.RocketChat.URL.Expose(), cfg.RocketChat.AuthToken.Expose(), cfg.RocketChat.UserID.Expose())
}

// InitializeSeeder initializes the credential seeder.
func InitializeSeeder(repo *db.Queries, crypter *crypto.Crypter, s21Client *s21.Client, log *slog.Logger) *service.CredentialSeeder {
	return service.NewCredentialSeeder(repo, crypter, s21Client, log)
}

// SeedCredentials seeds credentials from the configuration.
func SeedCredentials(ctx context.Context, seeder *service.CredentialSeeder, cfg *config.Config, log *slog.Logger) error {
	if cfg.Init.AEADKey.Expose() != "" {
		if err := seeder.Seed(ctx, cfg); err != nil {
			log.Error("failed to seed credentials", "error", err)
			return err
		}

		// Verify credentials via S21 API
		if cfg.Init.SchoolLogin != "" {
			if err := seeder.Verify(ctx, cfg.Init.SchoolLogin); err != nil {
				log.Error("credential verification failed", "error", err)
				// We don't exit here, just log the error as it might be an API issue or invalid initial creds
			}
		}
	} else if cfg.Init.SchoolLogin != "" {
		log.Warn("SCHOOL21_USER_LOGIN provided but AEAD_KEY is missing. Skipping credential seeding.")
	}
	return nil
}
