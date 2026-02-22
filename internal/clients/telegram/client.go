package telegram

import (
	"net/http"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/vgy789/noemx21-bot/internal/config"
)

// MustNew creates new telegram bot instance.
func MustNew(cfg *config.TelegramBot) *gotgbot.Bot {
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

	b, err := gotgbot.NewBot(cfg.Token.Expose(), botOpts)
	if err != nil {
		panic("failed to create bot: " + err.Error())
	}
	return b
}
