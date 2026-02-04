package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMustLoad(t *testing.T) {
	// Create temp files for secrets
	tokenFile := "test_token.txt"
	dbFile := "test_db.txt"
	_ = os.WriteFile(tokenFile, []byte("test-token"), 0644)
	_ = os.WriteFile(dbFile, []byte("postgres://user:pass@localhost:5432/db"), 0644)
	defer os.Remove(tokenFile)
	defer os.Remove(dbFile)

	// Set required env vars to point to these files
	t.Setenv("TELEGRAM_BOT_TOKEN", tokenFile)
	t.Setenv("DATABASE_URL", dbFile)
	t.Setenv("PRODUCTION", "true")
	t.Setenv("LOG_LEVEL", "info")

	// Create a dummy .env file to satisfy godotenv.Load if necessary,
	// though MustLoad ignores its error.

	cfg := MustLoad()

	assert.NotNil(t, cfg)
	assert.Equal(t, "test-token", string(cfg.Telegram.Token))
	assert.Equal(t, "postgres://user:pass@localhost:5432/db", string(cfg.DBURL))
	assert.True(t, cfg.Production)
	assert.Equal(t, "info", cfg.LogLevel)
}

func TestMustLoad_Panic(t *testing.T) {
	// Unset required env vars
	os.Unsetenv("TELEGRAM_BOT_TOKEN")
	os.Unsetenv("DATABASE_URL")

	assert.Panics(t, func() {
		MustLoad()
	})
}
