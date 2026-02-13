package telegram

import (
	"net/http"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/vgy789/noemx21-bot/internal/config"
)

// MustNew creates new telgram bot instance.
func MustNew(cfg *config.TelegramBot) *gotgbot.Bot {
	clientTimeout := max(time.Duration(cfg.Polling.Timeout+10)*time.Second, time.Second*60)

	b, err := gotgbot.NewBot(cfg.Token.Expose(), &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{
			Client: http.Client{
				Timeout: clientTimeout,
			},
			UseTestEnvironment: false,
			DefaultRequestOpts: &gotgbot.RequestOpts{
				Timeout: gotgbot.DefaultTimeout,
				APIURL:  gotgbot.DefaultAPIURL,
			},
		},
	})
	if err != nil {
		panic("failed to create bot: " + err.Error())
	}
	return b
}
