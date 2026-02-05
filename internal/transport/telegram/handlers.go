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
	d.AddHandler(handlers.NewCallback(func(cq *gotgbot.CallbackQuery) bool { return true }, s.handleCallback))
	d.AddHandler(handlers.NewMessage(func(msg *gotgbot.Message) bool { return true }, s.handleTextMessage))
}

// handleStart handles the start command.
func (s *telegramService) handleStart(b *gotgbot.Bot, ctx *ext.Context) error {
	userID := ctx.EffectiveUser.Id
	s.log.Info("user started the bot", "user_id", userID, "username", ctx.EffectiveUser.Username)

	bgCtx := context.Background()
	initialContext := make(map[string]interface{})

	// 1. Try to identify the student via Service
	profile, err := s.studentSvc.GetProfileByTelegramID(bgCtx, userID)
	if err == nil {
		// User recognized! Populate context if data is present.
		if profile.Login != "" {
			initialContext["my_s21login"] = profile.Login
		}
		if profile.CampusName != "" {
			initialContext["my_campus"] = profile.CampusName
		}
		if profile.CoalitionName != "" {
			initialContext["my_coalition"] = profile.CoalitionName
		}
		initialContext["my_level"] = profile.Level
		initialContext["my_exp"] = profile.Exp
		initialContext["my_prps"] = profile.PRP
		initialContext["my_crps"] = profile.CRP
		initialContext["my_coins"] = profile.Coins
	} else {
		s.log.Debug("user not found, identifying as new user", "user_id", userID, "error", err)
	}

	// 2. Initialize FSM (Always start with Registration/Language selection as requested)
	if err := s.engine.InitState(bgCtx, userID, fsm.FlowRegistration, fsm.StateSelectLanguage, initialContext); err != nil {
		s.log.Error("failed to init state", "error", err, "user_id", userID)
		return err
	}

	render, err := s.engine.GetCurrentRender(bgCtx, userID)
	if err != nil {
		s.log.Error("failed to get render", "error", err, "user_id", userID)
		return err
	}

	return s.sendRender(s.getSender(b), ctx.EffectiveChat.Id, render)
}

// handleTextMessage handles text messages (e.g., OTP code input).
func (s *telegramService) handleTextMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	userID := ctx.EffectiveUser.Id
	text := ctx.Message.Text
	bgCtx := context.Background()

	s.log.Debug("text message received", "user_id", userID, "text", text)

	// Process the text message through FSM
	render, err := s.engine.Process(bgCtx, userID, text)
	if err != nil {
		s.log.Warn("fsm text processing failed", "error", err, "user_id", userID, "text", text)

		// Fallback: try to just get the current state and re-render it
		render, err = s.engine.GetCurrentRender(bgCtx, userID)
		if err != nil {
			s.log.Error("fallback render failed", "error", err, "user_id", userID)
			_, _ = s.getSender(b).SendMessage(ctx.EffectiveChat.Id, "Произошла ошибка. Введите /start", nil)
			return nil
		}
	}

	return s.updateMessageRender(s.getSender(b), ctx.EffectiveChat.Id, ctx.Message.GetMessageId(), render)
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
			_, _ = s.getSender(b).AnswerCallbackQuery(cb.Id, &gotgbot.AnswerCallbackQueryOpts{Text: "Сессия истекла, введите /start"})
			return nil
		}

		// Inform user that something went wrong but we recovered
		_, _ = s.getSender(b).AnswerCallbackQuery(cb.Id, &gotgbot.AnswerCallbackQueryOpts{Text: "Кнопка устарела, обновляю меню..."})
	} else {
		s.log.Debug("callback processed successfully", "user_id", userID)
		_, _ = s.getSender(b).AnswerCallbackQuery(cb.Id, nil)
	}

	return s.updateMessageRender(s.getSender(b), ctx.EffectiveChat.Id, cb.Message.GetMessageId(), render)
}

func (s *telegramService) sendRender(sender Sender, chatID int64, render *fsm.RenderObject) error {
	_, err := sender.SendMessage(chatID, render.Text, &gotgbot.SendMessageOpts{
		ParseMode:   "Markdown",
		ReplyMarkup: buildMarkup(render.Buttons),
	})
	return err
}

func (s *telegramService) updateMessageRender(sender Sender, chatID int64, messageID int64, render *fsm.RenderObject) error {
	_, _, err := sender.EditMessageText(render.Text, &gotgbot.EditMessageTextOpts{
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
