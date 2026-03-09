package initialization

import (
	"context"
	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/app"
	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/crypto"
	"github.com/vgy789/noemx21-bot/internal/database"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/pkg/imgcache"
	"github.com/vgy789/noemx21-bot/internal/service"
	"github.com/vgy789/noemx21-bot/internal/service/schedule_generator"
	"github.com/vgy789/noemx21-bot/internal/sync/gitsync"
)

// Builder provides a fluent interface for initializing the application components.
type Builder struct {
	ctx      context.Context
	cfg      *config.Config
	log      *slog.Logger
	dbURL    string
	aeadKey  string
	s21Base  string
	school   string
	schoolPW string
}

// NewBuilder creates a new initialization builder.
func NewBuilder() *Builder {
	return &Builder{}
}

// WithContext sets the context for initialization.
func (b *Builder) WithContext(ctx context.Context) *Builder {
	b.ctx = ctx
	return b
}

// WithConfig sets the configuration for initialization.
func (b *Builder) WithConfig(cfg *config.Config) *Builder {
	b.cfg = cfg
	b.dbURL = cfg.DBURL.Expose()
	b.aeadKey = cfg.Init.AEADKey.Expose()
	b.s21Base = cfg.Init.S21BaseURL
	b.school = cfg.Init.SchoolLogin
	b.schoolPW = cfg.Init.SchoolPassword.Expose()
	return b
}

// WithLogger sets the logger for initialization.
func (b *Builder) WithLogger(log *slog.Logger) *Builder {
	b.log = log
	return b
}

// BuildDatabase initializes the database connection.
func (b *Builder) BuildDatabase() (*db.DBWrapper, error) {
	if b.dbURL == "" {
		return nil, nil
	}
	return InitializeDatabase(b.ctx, b.dbURL, b.log)
}

// BuildCrypter initializes the crypter.
func (b *Builder) BuildCrypter() (*crypto.Crypter, error) {
	if b.aeadKey == "" {
		return nil, nil
	}
	return InitializeCrypter(b.aeadKey, b.log)
}

// BuildS21Client initializes the S21 client.
func (b *Builder) BuildS21Client() *s21.Client {
	return InitializeS21Client(b.s21Base)
}

// BuildRocketChatClient initializes the RocketChat client.
func (b *Builder) BuildRocketChatClient() *rocketchat.Client {
	return InitializeRocketChatClient(b.cfg)
}

// BuildCredentialService initializes the credential service.
func (b *Builder) BuildCredentialService(repo *db.Queries, crypter *crypto.Crypter, s21Client *s21.Client) *service.CredentialService {
	return service.NewCredentialService(repo, crypter, s21Client, b.log)
}

// BuildApp creates the application instance.
func (b *Builder) BuildApp(repo *db.DBWrapper, rcClient *rocketchat.Client, s21Client *s21.Client, credService *service.CredentialService) *app.App {
	imgCache := imgcache.New()
	gitSync := gitsync.New(b.cfg.GitSync, repo.Queries, b.log)
	campusSvc := service.NewCampusService(repo.Queries, s21Client, b.cfg, b.log, credService)
	scheduleGen := schedule_generator.New(b.cfg, b.log, repo.Queries, imgCache)

	return app.New(b.cfg, b.log, repo, rcClient, s21Client, credService, gitSync, campusSvc, scheduleGen, scheduleGen, imgCache)

}

// Run runs the application using the builder's context.
func (b *Builder) Run() error {
	if b.log == nil {
		return nil
	}
	if b.ctx == nil {
		b.ctx = context.Background()
	}

	b.log.Info("Bot started", "version", "0.0.1", "production", b.cfg.Production, "log_level", b.cfg.LogLevel)

	// Initialize Database
	repo, err := b.BuildDatabase()
	if err != nil {
		return err
	}
	if err := database.NewMigrator(repo.Pool, b.log).Apply(b.ctx); err != nil {
		return err
	}

	// Initialize RocketChat and S21 clients
	rcClient := b.BuildRocketChatClient()
	s21Client := b.BuildS21Client()

	// Build and run the app
	var credService *service.CredentialService
	if b.aeadKey != "" {
		crypter, err := b.BuildCrypter()
		if err != nil {
			return err
		}
		credService = b.BuildCredentialService(repo.Queries, crypter, s21Client)
		if err := SeedCredentials(b.ctx, credService, b.cfg, b.log); err != nil {
			return err
		}
	} else if b.school != "" {
		b.log.Warn("SCHOOL21_USER_LOGIN provided but AEAD_KEY is missing. Skipping credential seeding.")
	}

	app := b.BuildApp(repo, rcClient, s21Client, credService)
	if err := app.Run(b.ctx); err != nil {
		return err
	}

	return nil
}
