package telegram

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/pkg/netretry"
)

// New creates a new telegram bot instance.
func New(cfg *config.TelegramBot) (*gotgbot.Bot, error) {
	clientTimeout := max(time.Duration(cfg.Polling.Timeout+10)*time.Second, time.Second*60)

	// Set up request opts with default or custom API URL
	// Use longer timeout for file uploads (photos, documents)
	requestOpts := &gotgbot.RequestOpts{
		Timeout: 120 * time.Second, // Increased timeout for file uploads
		APIURL:  gotgbot.DefaultAPIURL,
	}

	// Override API URL if custom URL is provided
	if cfg.APIURL != "" {
		requestOpts.APIURL = cfg.APIURL
	}

	botOpts := &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{
			Client: http.Client{
				Timeout: clientTimeout,
			},
			UseTestEnvironment: false,
			DefaultRequestOpts: requestOpts,
		},
	}

	var b *gotgbot.Bot
	err := netretry.Do(context.Background(), func() error {
		var initErr error
		b, initErr = gotgbot.NewBot(cfg.Token.Expose(), botOpts)
		if initErr != nil {
			errMsg := config.RedactString(initErr.Error(), cfg.Token)
			wrapped := fmt.Errorf("failed to create bot: %s", errMsg)
			// Invalid token/config errors are not transient and should not be retried.
			if isPermanentInitError(initErr) {
				return netretry.Permanent(wrapped)
			}
			return wrapped
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return b, nil
}

func isPermanentInitError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "invalid token") ||
		strings.Contains(msg, "not enough rights") ||
		strings.Contains(msg, "bad request")
}

// MustNew creates new telegram bot instance.
func MustNew(cfg *config.TelegramBot) *gotgbot.Bot {
	b, err := New(cfg)
	if err != nil {
		panic(err.Error())
	}
	return b
}
