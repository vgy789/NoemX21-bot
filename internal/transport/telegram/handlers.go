package telegram

import (
	"context"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// registerHandlers registers handlers for the dispatcher.
func (s *telegramService) registerHandlers(d *ext.Dispatcher) {
	d.AddHandler(handlers.NewCommand("start", s.handleStart))
	d.AddHandler(handlers.NewCallback(nil, s.handleCallback))
}

// handleStart handles the start command.
func (s *telegramService) handleStart(b *gotgbot.Bot, ctx *ext.Context) error {
	userID := ctx.EffectiveUser.Id
	s.log.Info("user started the bot", "user_id", userID, "username", ctx.EffectiveUser.Username)

	bgCtx := context.Background()

	s.log.Debug("initializing registration flow", "user_id", userID)
	if err := s.engine.InitState(bgCtx, userID, fsm.FlowRegistration, fsm.StateSelectLanguage); err != nil {
		s.log.Error("failed to init state", "error", err, "user_id", userID)
		return err
	}

	render, err := s.engine.GetCurrentRender(bgCtx, userID)
	if err != nil {
		s.log.Error("failed to get render", "error", err, "user_id", userID)
		return err
	}

	return s.sendRender(b, ctx.EffectiveChat.Id, render)
}

// handleCallback handles callback queries (buttons).
func (s *telegramService) handleCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	cb := ctx.CallbackQuery
	userID := ctx.EffectiveUser.Id
	bgCtx := context.Background()

	s.log.Debug("callback received", "user_id", userID, "data", cb.Data)

	render, err := s.engine.Process(bgCtx, userID, cb.Data)
	if err != nil {
		s.log.Warn("fsm transition failed, attempting fallback to current state", "error", err, "user_id", userID, "data", cb.Data)

		// Fallback: try to just get the current state and re-render it
		render, err = s.engine.GetCurrentRender(bgCtx, userID)
		if err != nil {
			s.log.Error("fallback render failed", "error", err, "user_id", userID)
			_, _ = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Сессия истекла, введите /start"})
			return nil
		}

		// Inform user that something went wrong but we recovered
		_, _ = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Кнопка устарела, обновляю меню..."})
	} else {
		s.log.Debug("callback processed successfully", "user_id", userID)
		_, _ = cb.Answer(b, nil)
	}

	return s.updateMessageRender(b, ctx.EffectiveChat.Id, cb.Message.GetMessageId(), render)
}

func (s *telegramService) sendRender(b *gotgbot.Bot, chatID int64, render *fsm.RenderObject) error {
	_, err := b.SendMessage(chatID, render.Text, &gotgbot.SendMessageOpts{
		ParseMode:   "Markdown",
		ReplyMarkup: buildMarkup(render.Buttons),
	})
	return err
}

func (s *telegramService) updateMessageRender(b *gotgbot.Bot, chatID int64, messageID int64, render *fsm.RenderObject) error {
	_, _, err := b.EditMessageText(render.Text, &gotgbot.EditMessageTextOpts{
		ChatId:      chatID,
		MessageId:   messageID,
		ParseMode:   "Markdown",
		ReplyMarkup: buildMarkup(render.Buttons),
	})
	if err != nil && strings.Contains(err.Error(), "message is not modified") {
		s.log.Debug("message not modified, ignoring error")
		return nil
	}
	return err
}

func buildMarkup(rows [][]fsm.ButtonRender) gotgbot.InlineKeyboardMarkup {
	var inlineRows [][]gotgbot.InlineKeyboardButton
	for _, row := range rows {
		var inlineRow []gotgbot.InlineKeyboardButton
		for _, btn := range row {
			inlineRow = append(inlineRow, gotgbot.InlineKeyboardButton{
				Text:         btn.Text,
				CallbackData: btn.Data,
			})
		}
		inlineRows = append(inlineRows, inlineRow)
	}
	return gotgbot.InlineKeyboardMarkup{InlineKeyboard: inlineRows}
}
