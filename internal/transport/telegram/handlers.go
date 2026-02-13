package telegram

import (
	"context"
	"os"
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
	initialContext := make(map[string]any)

	// 1. Try to identify the student via Service
	profile, err := s.userSvc.GetProfileByTelegramID(bgCtx, userID)
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
	// Store telegram username for later use in registration
	if ctx.EffectiveUser.Username != "" {
		initialContext["telegram_username"] = ctx.EffectiveUser.Username
	}
	initialContext["telegram_first_name"] = ctx.EffectiveUser.FirstName
	initialContext["telegram_last_name"] = ctx.EffectiveUser.LastName

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
	bgCtx := context.WithValue(context.Background(), fsm.ContextKeyUserInfo, &fsm.UserInfo{
		ID:        userID,
		Username:  ctx.EffectiveUser.Username,
		FirstName: ctx.EffectiveUser.FirstName,
		LastName:  ctx.EffectiveUser.LastName,
		Platform:  "Telegram",
	})

	s.log.Debug("text message received", "user_id", userID, "text", text)

	// Process the text message through FSM
	render, err := s.engine.Process(bgCtx, userID, text)
	if err != nil {
		if err == fsm.ErrEngineBusy {
			s.log.Debug("engine is busy, ignoring text message", "user_id", userID)
			_, _ = s.getSender(b).SendMessage(ctx.EffectiveChat.Id, "⏳ Пожалуйста, подождите, идёт обновление данных...", nil)
			return nil
		}
		s.log.Warn("fsm text processing failed", "error", err, "user_id", userID, "text", text)

		// Fallback: try to just get the current state and re-render it
		render, err = s.engine.GetCurrentRender(bgCtx, userID)
		if err != nil {
			s.log.Error("fallback render failed", "error", err, "user_id", userID)
			_, _ = s.getSender(b).SendMessage(ctx.EffectiveChat.Id, "Произошла ошибка. Введите /start", nil)
			return nil
		}
	}

	return s.sendRender(s.getSender(b), ctx.EffectiveChat.Id, render)
}

// handleCallback handles callback queries (buttons).
func (s *telegramService) handleCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	cb := ctx.CallbackQuery
	userID := ctx.EffectiveUser.Id
	bgCtx := context.WithValue(context.Background(), fsm.ContextKeyUserInfo, &fsm.UserInfo{
		ID:        userID,
		Username:  ctx.EffectiveUser.Username,
		FirstName: ctx.EffectiveUser.FirstName,
		LastName:  ctx.EffectiveUser.LastName,
		Platform:  "Telegram",
	})

	s.log.Debug("callback received", "user_id", userID, "data", cb.Data)

	render, err := s.engine.Process(bgCtx, userID, cb.Data)
	if err != nil {
		if err == fsm.ErrEngineBusy {
			s.log.Debug("engine is busy, ignoring callback", "user_id", userID)
			_, _ = s.getSender(b).AnswerCallbackQuery(cb.Id, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "⏳ Пожалуйста, подождите...",
				ShowAlert: false,
			})
			return nil
		}
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
		opts := &gotgbot.AnswerCallbackQueryOpts{}
		if render != nil && render.Alert != "" {
			s.log.Debug("sending callback alert", "text", render.Alert)
			opts.Text = render.Alert
			opts.ShowAlert = false // Use toast notification
		}
		_, _ = s.getSender(b).AnswerCallbackQuery(cb.Id, opts)
	}

	return s.updateMessageRender(s.getSender(b), ctx.EffectiveChat.Id, cb.Message.GetMessageId(), render)
}

func (s *telegramService) sendRender(sender Sender, chatID int64, render *fsm.RenderObject) error {
	if render.Image != "" {
		f, err := os.Open(render.Image)
		if err != nil {
			s.log.Error("failed to open image", "path", render.Image, "error", err)
		} else {
			defer func() { _ = f.Close() }()
			// Trying InputFileByReader as NamedFile was undefined
			_, err = sender.SendPhoto(chatID, gotgbot.InputFileByReader("chart.png", f), &gotgbot.SendPhotoOpts{
				Caption:     render.Text,
				ParseMode:   "Markdown",
				ReplyMarkup: buildMarkup(render.Buttons),
			})
			return err
		}
	}

	_, err := sender.SendMessage(chatID, render.Text, &gotgbot.SendMessageOpts{
		ParseMode:   "Markdown",
		ReplyMarkup: buildMarkup(render.Buttons),
	})
	return err
}

func (s *telegramService) updateMessageRender(sender Sender, chatID int64, messageID int64, render *fsm.RenderObject) error {
	// If message has an image, we MUST use delete/send because Telegram doesn't support
	// converting a photo message to a text message via EditMessageText, or vice versa.
	// We use the presence of render.Image to determine the NEW type.
	// Note: We don't easily know if the OLD message had a photo, but we hit the error fallback if it did.

	if render.Image != "" {
		s.log.Debug("render has image, switching to photo message", "image", render.Image)
		_, _ = sender.DeleteMessage(chatID, messageID)
		return s.sendRender(sender, chatID, render)
	}

	// Try editing text. If it fails because the original was a photo, fallback to delete/send.
	_, _, err := sender.EditMessageText(render.Text, &gotgbot.EditMessageTextOpts{
		ChatId:      chatID,
		MessageId:   messageID,
		ParseMode:   "Markdown",
		ReplyMarkup: buildMarkup(render.Buttons),
	})
	if err != nil {
		if strings.Contains(err.Error(), "message is not modified") {
			s.log.Debug("message not modified, ignoring error")
			return nil
		}

		// Bad Request: there is no text in the message to edit (happens when editing a photo message)
		if strings.Contains(err.Error(), "there is no text in the message to edit") ||
			strings.Contains(err.Error(), "message can't be edited") {
			s.log.Debug("message type mismatch (photo/text), using delete/send fallback")
			_, _ = sender.DeleteMessage(chatID, messageID)
			return s.sendRender(sender, chatID, render)
		}

		s.log.Warn("edit failed, fallback to delete/send", "error", err)
		_, _ = sender.DeleteMessage(chatID, messageID)
		return s.sendRender(sender, chatID, render)
	}
	return nil
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
