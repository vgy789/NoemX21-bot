package config

import (
	"testing"
	"time"

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
	t.Setenv("TELEGRAM_WEBHOOK_ENABLED", "false")
	t.Setenv("TELEGRAM_WEBHOOK_PATH", "/telegram/webhook")
	t.Setenv("TELEGRAM_WEBHOOK_PORT", "8080")
	t.Setenv("POLLING_TIMEOUT", "9")
	t.Setenv("REQUEST_TIMEOUT", "25s")
	t.Setenv("MAX_ROUTINES", "0")
	t.Setenv("DROP_PENDING_UPDATES", "true")
	t.Setenv("API_SERVER_PORT", "8081")
	t.Setenv("CHART_TEMP_DIR", "tmp/skills_radar")
	t.Setenv("GIT_BRANCH", "main")
	t.Setenv("GIT_SYNC_INTERVAL", "5m")
	t.Setenv("GIT_LOCAL_PATH", "data")
	t.Setenv("OTP_EMAIL_ENABLED", "false")
	t.Setenv("OTP_EMAIL_SMTP_PORT", "587")
	t.Setenv("OTP_EMAIL_SMTP_TIMEOUT", "20s")
	t.Setenv("OTP_EMAIL_SUBJECT", "NOEMX21-BOT | Verification code")
	t.Setenv("OTP_EMAIL_TEMPLATE_PATH", "internal/service/templates/otp_email.html.tmpl")

	cfg := MustLoad()

	assert.Equal(t, "debug", cfg.LogLevel)
	assert.False(t, cfg.Telegram.Webhook.Enabled)
	assert.Equal(t, 8080, cfg.Telegram.Webhook.ListenPort)
	assert.Equal(t, "/telegram/webhook", cfg.Telegram.Webhook.ListenPath)
	assert.Equal(t, int64(9), cfg.Telegram.Polling.Timeout)
	assert.Equal(t, 25*time.Second, cfg.Telegram.Polling.RequestTimeout)
	assert.Equal(t, 0, cfg.Telegram.Polling.MaxRoutines)
	assert.True(t, cfg.Telegram.Polling.DropPendingUpdates)
	assert.Equal(t, 8081, cfg.APIServer.Port)
	assert.Equal(t, "tmp/skills_radar", cfg.Charts.TempDir)
	assert.False(t, cfg.Production)
	assert.Equal(t, "main", cfg.GitSync.Branch)
	assert.Equal(t, "5m", cfg.GitSync.Interval)
	assert.Equal(t, "data", cfg.GitSync.LocalPath)
	assert.False(t, cfg.EmailOTP.Enabled)
	assert.Equal(t, 587, cfg.EmailOTP.SMTPPort)
	assert.Equal(t, 20*time.Second, cfg.EmailOTP.SMTPTimeout)
	assert.Equal(t, "", cfg.EmailOTP.TestTo)
	assert.Equal(t, "NOEMX21-BOT | Verification code", cfg.EmailOTP.Subject)
	assert.Equal(t, "internal/service/templates/otp_email.html.tmpl", cfg.EmailOTP.TemplatePath)
}
