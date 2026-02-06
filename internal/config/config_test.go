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
	rcUrlFile := "test_rc_url.txt"
	rcUserFile := "test_rc_user.txt"
	rcTokenFile := "test_rc_token.txt"

	_ = os.WriteFile(tokenFile, []byte("test-token"), 0644)
	_ = os.WriteFile(dbFile, []byte("postgres://user:pass@localhost:5432/db"), 0644)
	_ = os.WriteFile(rcUrlFile, []byte("http://localhost:3000"), 0644)
	_ = os.WriteFile(rcUserFile, []byte("user"), 0644)
	_ = os.WriteFile(rcTokenFile, []byte("token"), 0644)

	defer func() { _ = os.Remove(tokenFile) }()
	defer func() { _ = os.Remove(dbFile) }()
	defer func() { _ = os.Remove(rcUrlFile) }()
	defer func() { _ = os.Remove(rcUserFile) }()
	defer func() { _ = os.Remove(rcTokenFile) }()

	// Set required env vars to point to these files
	t.Setenv("TELEGRAM_BOT_TOKEN", tokenFile)
	t.Setenv("DATABASE_URL", dbFile)
	t.Setenv("PRODUCTION", "true")
	t.Setenv("LOG_LEVEL", "info")

	// Set RocketChat vars (note: they don't have ,file tag in current config.go)
	t.Setenv("ROCKETCHAT_API_URL", "http://localhost:3000")
	t.Setenv("ROCKETCHAT_USER_ID", "user")
	t.Setenv("ROCKETCHAT_AUTH_TOKEN", "token")

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
	_ = os.Unsetenv("TELEGRAM_BOT_TOKEN")
	_ = os.Unsetenv("DATABASE_URL")

	assert.Panics(t, func() {
		MustLoad()
	})
}
