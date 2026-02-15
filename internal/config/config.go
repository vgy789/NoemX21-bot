package config

import (
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

const (
	envPath = "env/.env"
)

// TelegramBot is a configuration for the telegram bot.
type TelegramBot struct {
	Token   Secret `env:"TELEGRAM_BOT_TOKEN,notEmpty"`
	Polling struct {
		// 8-byte aligned fields first
		RequestTimeout time.Duration `env:"REQUEST_TIMEOUT" envDefault:"25s"`
		Timeout        int64         `env:"POLLING_TIMEOUT" envDefault:"9"`
		// 4-byte aligned fields
		MaxRoutines int `env:"MAX_ROUTINES" envDefault:"0"`
		// 1-byte aligned fields last
		DropPendingUpdates bool `env:"DROP_PENDING_UPDATES" envDefault:"true"`
	}
}

// Config is a configuration for the application.
type Config struct {
	LogLevel   string `env:"LOG_LEVEL" envDefault:"debug"`
	DBURL      Secret `env:"DATABASE_URL,notEmpty"`
	Telegram   TelegramBot
	GitSync    GitSync
	RocketChat struct {
		URL       Secret `env:"ROCKETCHAT_API_URL,notEmpty"`
		UserID    Secret `env:"ROCKETCHAT_USER_ID,notEmpty"`
		AuthToken Secret `env:"ROCKETCHAT_AUTH_TOKEN,notEmpty"`
	}
	Init struct {
		SchoolLogin    string `env:"SCHOOL21_USER_LOGIN"`
		S21BaseURL     string `env:"SCHOOL21_API_URL"`
		AEADKey        Secret `env:"AEAD_KEY"` // 32 bytes hex
		SchoolPassword Secret `env:"SCHOOL21_USER_PASSWORD"`
	}
	APIServer struct {
		Port int `env:"API_SERVER_PORT" envDefault:"8081"`
	}
	Charts struct {
		TempDir string `env:"CHART_TEMP_DIR" envDefault:"tmp"`
	}
	Production bool `env:"PRODUCTION" envDefault:"false"`
}

type GitSync struct {
	RepoURL      string `env:"GIT_REPO_URL"`
	Branch       string `env:"GIT_BRANCH" envDefault:"main"`
	Interval     string `env:"GIT_SYNC_INTERVAL" envDefault:"5m"`
	LocalPath    string `env:"GIT_LOCAL_PATH" envDefault:"data"`
	SSHKeyBase64 Secret `env:"SSH_KEY_BASE64"`
}

// MustLoad reads config from .env file OR environment variables.
func MustLoad() *Config {
	var cfg Config
	_ = godotenv.Load(envPath)
	if err := env.Parse(&cfg); err != nil {
		panic("failed to parse config: " + err.Error())
	}

	return &cfg
}
