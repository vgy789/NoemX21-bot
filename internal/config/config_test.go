package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMustLoad(t *testing.T) {
	// Set required env vars (values are read from env, not from files)
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	t.Setenv("PRODUCTION", "true")
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("ROCKETCHAT_API_URL", "http://localhost:3000")
	t.Setenv("ROCKETCHAT_USER_ID", "user")
	t.Setenv("ROCKETCHAT_AUTH_TOKEN", "token")

	cfg := MustLoad()

	assert.NotNil(t, cfg)
	assert.Equal(t, "test-token", string(cfg.Telegram.Token))
	assert.Equal(t, "postgres://user:pass@localhost:5432/db", string(cfg.DBURL))
	assert.True(t, cfg.Production)
	assert.Equal(t, "info", cfg.LogLevel)
}

func TestMustLoad_Panic(t *testing.T) {
	// Unset required env vars
	_ = os.Unsetenv("TELEGRAM_BOT_TOKEN")
	_ = os.Unsetenv("DATABASE_URL")

	assert.Panics(t, func() {
		MustLoad()
	})
}
