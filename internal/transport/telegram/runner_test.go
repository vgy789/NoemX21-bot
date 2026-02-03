package telegram

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/config"
)

func TestNewTelegramService(t *testing.T) {
	cfg := &config.TelegramBot{}
	logger := slog.Default()
	service := NewTelegramService(cfg, logger)

	require.NotNil(t, service)

	ts, ok := service.(*telegramService)
	require.True(t, ok, "NewTelegramService did not return *telegramService")

	assert.Equal(t, cfg, ts.cfg)
	assert.Equal(t, logger, ts.log)
	assert.NotNil(t, ts.engine, "FSM engine should be initialized")
}
