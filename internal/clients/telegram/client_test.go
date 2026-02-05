package telegram

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vgy789/noemx21-bot/internal/config"
)

func TestMustNew_PanicsOnInvalidToken(t *testing.T) {
	cfg := &config.TelegramBot{}
	cfg.Token = config.Secret("")
	cfg.Polling.Timeout = 9

	assert.Panics(t, func() { MustNew(cfg) })
}
