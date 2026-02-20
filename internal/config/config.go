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
	APIURL  string `env:"TELEGRAM_API_URL"` // Custom Telegram Bot API URL (for local bot API servers)
	Webhook struct {
		Enabled    bool   `env:"TELEGRAM_WEBHOOK_ENABLED" envDefault:"false"`
		URL        string `env:"TELEGRAM_WEBHOOK_URL"`
		ListenPath string `env:"TELEGRAM_WEBHOOK_PATH" envDefault:"/telegram/webhook"`
		ListenPort int    `env:"TELEGRAM_WEBHOOK_PORT" envDefault:"8080"`
		Secret     Secret `env:"TELEGRAM_WEBHOOK_SECRET"`
	}
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
		SchoolLogin    string `env:"SCHOOL21_USER_LOGIN,notEmpty"`
		S21BaseURL     string `env:"SCHOOL21_API_URL,notEmpty"`
		AEADKey        Secret `env:"AEAD_KEY,notEmpty"` // 32 bytes hex
		SchoolPassword Secret `env:"SCHOOL21_USER_PASSWORD,notEmpty"`
	}
	APIServer struct {
		Port int `env:"API_SERVER_PORT" envDefault:"8081"`
	}
	Charts struct {
		TempDir string `env:"CHART_TEMP_DIR" envDefault:"tmp/skills_radar"`
	}
	ScheduleImages struct {
		Enabled  bool          `env:"SCHEDULE_IMAGES_ENABLED" envDefault:"true"`
		Interval time.Duration `env:"SCHEDULE_IMAGES_INTERVAL" envDefault:"30s"`
		TempDir  string        `env:"SCHEDULE_IMAGES_TEMP_DIR" envDefault:"tmp/schedules"`
	}
	Production    bool `env:"PRODUCTION" envDefault:"false"`
	TestModeNoOTP bool `env:"TEST_MODE_NO_OTP" envDefault:"false"` // Skip OTP verification for testing
}

type GitSync struct {
	SSHRepoURL   string `env:"GIT_REPO_URL"`
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
