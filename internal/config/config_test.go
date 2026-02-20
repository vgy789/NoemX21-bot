package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMustLoad(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	t.Setenv("PRODUCTION", "true")
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("ROCKETCHAT_API_URL", "http://localhost:3000")
	t.Setenv("ROCKETCHAT_USER_ID", "user")
	t.Setenv("ROCKETCHAT_AUTH_TOKEN", "token")
	t.Setenv("SCHOOL21_USER_LOGIN", "test_login")
	t.Setenv("SCHOOL21_API_URL", "http://localhost:8080")
	t.Setenv("AEAD_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SCHOOL21_USER_PASSWORD", "test_password")

	cfg := MustLoad()

	assert.NotNil(t, cfg)
	assert.Equal(t, "test-token", string(cfg.Telegram.Token))
	assert.Equal(t, "postgres://user:pass@localhost:5432/db", string(cfg.DBURL))
	assert.True(t, cfg.Production)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "test_login", cfg.Init.SchoolLogin)
	assert.Equal(t, "http://localhost:8080", cfg.Init.S21BaseURL)
	assert.Equal(t, "test_password", string(cfg.Init.SchoolPassword))
}

func TestMustLoad_Panic(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("ROCKETCHAT_API_URL", "")
	t.Setenv("ROCKETCHAT_USER_ID", "")
	t.Setenv("ROCKETCHAT_AUTH_TOKEN", "")
	t.Setenv("SCHOOL21_USER_LOGIN", "")
	t.Setenv("SCHOOL21_API_URL", "")
	t.Setenv("AEAD_KEY", "")
	t.Setenv("SCHOOL21_USER_PASSWORD", "")

	assert.Panics(t, func() {
		MustLoad()
	})
}

func TestConfig_Defaults(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	t.Setenv("ROCKETCHAT_API_URL", "http://localhost:3000")
	t.Setenv("ROCKETCHAT_USER_ID", "user")
	t.Setenv("ROCKETCHAT_AUTH_TOKEN", "token")
	t.Setenv("SCHOOL21_USER_LOGIN", "test_login")
	t.Setenv("SCHOOL21_API_URL", "http://localhost:8080")
	t.Setenv("AEAD_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SCHOOL21_USER_PASSWORD", "test_password")

	cfg := MustLoad()

	assert.Equal(t, "debug", cfg.LogLevel)
	assert.False(t, cfg.Telegram.Webhook.Enabled)
	assert.Equal(t, 8080, cfg.Telegram.Webhook.ListenPort)
	assert.Equal(t, "/telegram/webhook", cfg.Telegram.Webhook.ListenPath)
	assert.Equal(t, int64(9), cfg.Telegram.Polling.Timeout)
	assert.Equal(t, 25000000000, int(cfg.Telegram.Polling.RequestTimeout))
	assert.Equal(t, 0, cfg.Telegram.Polling.MaxRoutines)
	assert.True(t, cfg.Telegram.Polling.DropPendingUpdates)
	assert.Equal(t, 8081, cfg.APIServer.Port)
	assert.Equal(t, "tmp/skills_radar", cfg.Charts.TempDir)
	assert.False(t, cfg.Production)
	assert.Equal(t, "main", cfg.GitSync.Branch)
	assert.Equal(t, "5m", cfg.GitSync.Interval)
	assert.Equal(t, "data", cfg.GitSync.LocalPath)
}
