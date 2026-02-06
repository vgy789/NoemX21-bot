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
	Token   Secret `env:"TELEGRAM_BOT_TOKEN,file,notEmpty"`
	Polling struct {
		DropPendingUpdates bool          `env:"DROP_PENDING_UPDATES" envDefault:"true"`
		Timeout            int64         `env:"POLLING_TIMEOUT" envDefault:"9"`
		RequestTimeout     time.Duration `env:"REQUEST_TIMEOUT" envDefault:"10s"`
		MaxRoutines        int           `env:"MAX_ROUTINES" envDefault:"0"`
	}
}

// Config is a configuration for the application.
type Config struct {
	Production bool   `env:"PRODUCTION" envDefault:"false"`
	LogLevel   string `env:"LOG_LEVEL" envDefault:"debug"`
	Telegram   TelegramBot
	DBURL      Secret `env:"DATABASE_URL,file,notEmpty"`
	RocketChat struct {
		URL       Secret `env:"ROCKETCHAT_API_URL,notEmpty"`
		UserID    Secret `env:"ROCKETCHAT_USER_ID,notEmpty"`
		AuthToken Secret `env:"ROCKETCHAT_AUTH_TOKEN,notEmpty"`
	}
	Init struct {
		AEADKey        Secret `env:"AEAD_KEY"` // 32 bytes hex
		SchoolLogin    string `env:"SCHOOL21_USER_LOGIN"`
		SchoolPassword Secret `env:"SCHOOL21_USER_PASSWORD"`
		S21BaseURL     string `env:"SCHOOL21_API_URL"`
	}
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
