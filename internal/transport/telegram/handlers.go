package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	chatmemberfilters "github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/chatmember"
	"github.com/jackc/pgx/v5"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

type telegramNotifier struct {
	sender Sender
}

func (n *telegramNotifier) NotifyUser(_ context.Context, userID int64, text string) error {
	if n == nil || n.sender == nil || strings.TrimSpace(text) == "" {
		return nil
	}
	_, err := n.sender.SendMessage(userID, text, nil)
	if err != nil {
		return fmt.Errorf("telegram notify failed: %w", err)
	}
	return nil
}

func (n *telegramNotifier) NotifyUserRender(_ context.Context, userID int64, render *fsm.RenderObject) error {
	if n == nil || n.sender == nil || render == nil {
		return nil
	}
	text := strings.TrimSpace(render.Text)
	image := strings.TrimSpace(render.Image)
	if text == "" && image == "" {
		return nil
	}

	if image != "" {
		cleanPath := filepath.Clean(image)
		if strings.Contains(cleanPath, "..") || strings.HasPrefix(cleanPath, "/") {
			return fmt.Errorf("telegram notify render failed: illegal image path %q", image)
		}

		fileToClose, err := os.Open(cleanPath)
		if err != nil {
			return fmt.Errorf("telegram notify render failed: open image %q: %w", cleanPath, err)
		}
		defer func() { _ = fileToClose.Close() }()

		_, err = n.sender.SendPhoto(userID, gotgbot.InputFileByReader("chart.png", fileToClose), &gotgbot.SendPhotoOpts{
			Caption:     render.Text,
			ParseMode:   "Markdown",
			ReplyMarkup: buildMarkup(render.Buttons),
		})
		if err != nil {
			return fmt.Errorf("telegram notify render failed: %w", err)
		}
		return nil
	}

	_, err := n.sender.SendMessage(userID, render.Text, &gotgbot.SendMessageOpts{
		ParseMode:   "Markdown",
		ReplyMarkup: buildMarkup(render.Buttons),
	})
	if err != nil {
		return fmt.Errorf("telegram notify render failed: %w", err)
	}
	return nil
}

// registerHandlers registers handlers for the dispatcher.
func (s *telegramService) registerHandlers(d *ext.Dispatcher) {
	d.AddHandler(handlers.NewCommand("start", s.handleStart))
	d.AddHandler(handlers.NewCommand("init", s.handleGroupInit))
	d.AddHandler(handlers.NewCommand("group_init", s.handleGroupInit))
	d.AddHandler(handlers.NewCommand("prr_here", s.handlePRRHere))
	d.AddHandler(handlers.NewMyChatMember(chatmemberfilters.All, s.handleMyChatMember))
	d.AddHandler(handlers.NewChatMember(chatmemberfilters.All, s.handleChatMember))
	d.AddHandler(handlers.NewCallback(func(cq *gotgbot.CallbackQuery) bool { return true }, s.withCallbackDebugMiddleware(s.handleCallback)))
	d.AddHandler(handlers.NewMessage(func(msg *gotgbot.Message) bool { return true }, s.withDurationCleanupMiddleware(s.handleTextMessage)))
}

func isPrivateChat(chat *gotgbot.Chat) bool {
	if chat == nil {
		return true
	}
	chatType := strings.TrimSpace(chat.Type)
	return chatType == "" || chatType == "private"
}

func isGroupChat(chat *gotgbot.Chat) bool {
	if chat == nil {
		return false
	}
	switch strings.TrimSpace(chat.Type) {
	case "group", "supergroup":
		return true
	default:
		return false
	}
}

func (s *telegramService) withCallbackDebugMiddleware(next handlers.Response) handlers.Response {
	return func(b *gotgbot.Bot, ctx *ext.Context) error {
		cb := ctx.CallbackQuery
		if cb != nil {
			s.log.Debug("callback_data middleware",
				"user_id", ctx.EffectiveUser.Id,
				"callback_data", cb.Data,
			)
		}
		return next(b, ctx)
	}
}

func (s *telegramService) withDurationCleanupMiddleware(next handlers.Response) handlers.Response {
	return func(b *gotgbot.Bot, ctx *ext.Context) error {
		if ctx.Message != nil && s.shouldCleanupDurationInput(ctx.EffectiveUser.Id) {
			chatID := ctx.EffectiveChat.Id
			messageID := int64(ctx.Message.MessageId)
			sender := s.getSender(b)
			go func() {
				time.Sleep(1 * time.Second)
				if _, err := sender.DeleteMessage(chatID, messageID); err != nil {
					s.log.Debug("duration input cleanup failed",
						"chat_id", chatID,
						"message_id", messageID,
						"error", err,
					)
				}
			}()
		}
		return next(b, ctx)
	}
}

func (s *telegramService) shouldCleanupDurationInput(userID int64) bool {
	if s.engine == nil || s.engine.Repo() == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	state, err := s.engine.Repo().GetState(ctx, userID)
	if err != nil || state == nil {
		return false
	}

	return state.CurrentFlow == "booking.yaml" && state.CurrentState == "BOOKING_DURATION_CHOICE"
}

// handleStart handles the start command.
func (s *telegramService) handleStart(b *gotgbot.Bot, ctx *ext.Context) error {
	if !isPrivateChat(ctx.EffectiveChat) {
		return s.handleGroupStart(b, ctx)
	}

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

	msg, err := s.sendRender(s.getSender(b), ctx.EffectiveChat.Id, render)
	if err == nil {
		s.setLastBotMessageID(userID, msg)
	}
	return err
}

func (s *telegramService) handleGroupStart(b *gotgbot.Bot, ctx *ext.Context) error {
	if !isGroupChat(ctx.EffectiveChat) {
		return nil
	}
	_, err := s.getSender(b).SendMessage(ctx.EffectiveChat.Id,
		"Бот добавлен в группу.\n\nВладелец группы должен выполнить команду /init, чтобы завершить инициализацию.",
		nil,
	)
	return err
}

func (s *telegramService) handleGroupInit(b *gotgbot.Bot, ctx *ext.Context) error {
	if !isGroupChat(ctx.EffectiveChat) {
		_, _ = s.getSender(b).SendMessage(ctx.EffectiveChat.Id, "Эта команда доступна только в группе.", nil)
		return nil
	}
	if b == nil || ctx.EffectiveUser == nil || ctx.EffectiveChat == nil {
		return nil
	}
	if s.queries == nil {
		s.log.Error("telegram queries is nil, cannot initialize group", "chat_id", ctx.EffectiveChat.Id)
		_, _ = s.getSender(b).SendMessage(ctx.EffectiveChat.Id, "Не удалось инициализировать группу. Попробуйте позже.", nil)
		return nil
	}

	chatID := ctx.EffectiveChat.Id
	userID := ctx.EffectiveUser.Id

	ownerMember, err := b.GetChatMember(chatID, userID, nil)
	if err != nil {
		s.log.Warn("failed to verify group owner", "chat_id", chatID, "user_id", userID, "error", err)
		_, _ = s.getSender(b).SendMessage(chatID, "Не удалось проверить права. Убедитесь, что бот администратор группы.", nil)
		return nil
	}
	if ownerMember.GetStatus() != gotgbot.ChatMemberStatusOwner {
		rows, revokeErr := s.queries.UnlinkTelegramGroupOwnerIfOwner(context.Background(), db.UnlinkTelegramGroupOwnerIfOwnerParams{
			ChatID:              chatID,
			OwnerTelegramUserID: userID,
		})
		if revokeErr != nil {
			s.log.Warn("failed to unlink stale owner access", "chat_id", chatID, "user_id", userID, "error", revokeErr)
		}
		if rows > 0 {
			_, _ = s.getSender(b).SendMessage(chatID, "Владелец группы изменился. Твоя привязка к админке этой группы снята. Бот продолжает работать. Новый владелец должен выполнить /init.", nil)
			return nil
		}
		_, _ = s.getSender(b).SendMessage(chatID, "Инициализацию может выполнить только владелец группы.", nil)
		return nil
	}

	botMember, err := b.GetChatMember(chatID, b.Id, nil)
	if err != nil {
		s.log.Warn("failed to verify bot status in group", "chat_id", chatID, "error", err)
		_, _ = s.getSender(b).SendMessage(chatID, "Не удалось проверить права бота. Назначьте бота администратором и повторите /init.", nil)
		return nil
	}
	botStatus := botMember.GetStatus()
	if botStatus != gotgbot.ChatMemberStatusAdministrator && botStatus != gotgbot.ChatMemberStatusOwner {
		_, _ = s.getSender(b).SendMessage(chatID, "Для инициализации назначьте бота администратором группы, затем повторите /init.", nil)
		return nil
	}

	_, err = s.queries.GetUserAccountByExternalId(context.Background(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: fmt.Sprintf("%d", userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || strings.Contains(strings.ToLower(err.Error()), "no rows") {
			_, _ = s.getSender(b).SendMessage(chatID, registrationRequiredGroupInitText(b), nil)
			return nil
		}
		s.log.Error("failed to verify group owner registration", "chat_id", chatID, "user_id", userID, "error", err)
		_, _ = s.getSender(b).SendMessage(chatID, "Не удалось проверить регистрацию владельца. Попробуйте позже.", nil)
		return nil
	}

	title := strings.TrimSpace(ctx.EffectiveChat.Title)
	if title == "" {
		title = fmt.Sprintf("Group %d", chatID)
	}

	_, err = s.queries.UpsertTelegramGroup(context.Background(), db.UpsertTelegramGroupParams{
		ChatID:                chatID,
		ChatTitle:             title,
		OwnerTelegramUserID:   userID,
		OwnerTelegramUsername: ctx.EffectiveUser.Username,
		IsInitialized:         true,
		IsActive:              true,
	})
	if err != nil {
		s.log.Error("failed to upsert telegram group", "chat_id", chatID, "user_id", userID, "error", err)
		_, _ = s.getSender(b).SendMessage(chatID, "Не удалось сохранить группу. Попробуйте позже.", nil)
		return nil
	}

	_, err = s.getSender(b).SendMessage(chatID,
		"Группа успешно инициализирована.\nПрава управления закреплены за текущим владельцем (инициатором /init).\n\nОткрой личный чат с ботом → Клубы → Настройка групп. Там появится кнопка для настройки группы.",
		nil,
	)
	return err
}

func (s *telegramService) handlePRRHere(b *gotgbot.Bot, ctx *ext.Context) error {
	if !isGroupChat(ctx.EffectiveChat) {
		_, _ = s.getSender(b).SendMessage(ctx.EffectiveChat.Id, "Эта команда доступна только в группе.", nil)
		return nil
	}
	if b == nil || ctx.EffectiveUser == nil || ctx.EffectiveChat == nil || ctx.EffectiveMessage == nil || s.queries == nil {
		return nil
	}

	chatID := ctx.EffectiveChat.Id
	userID := ctx.EffectiveUser.Id

	group, err := s.queries.GetTelegramGroupByChatID(context.Background(), chatID)
	if err != nil || !group.IsActive || !group.IsInitialized {
		_, _ = s.getSender(b).SendMessage(chatID, "Группа не инициализирована. Сначала выполни /init.", nil)
		return nil
	}
	if group.OwnerTelegramUserID != userID {
		_, _ = s.getSender(b).SendMessage(chatID, "Только владелец группы может настраивать PRR-радар.", nil)
		return nil
	}

	_, _ = s.queries.UpdateTelegramGroupForumFlagsByChatID(context.Background(), db.UpdateTelegramGroupForumFlagsByChatIDParams{
		ChatID:  chatID,
		IsForum: ctx.EffectiveChat.IsForum,
	})

	threadID := int64(ctx.EffectiveMessage.MessageThreadId)
	threadLabel := normalizeThreadLabel(threadID)

	_, updateErr := s.queries.UpdateTelegramGroupPRRNotificationDestinationByOwner(context.Background(), db.UpdateTelegramGroupPRRNotificationDestinationByOwnerParams{
		ChatID:                      chatID,
		OwnerTelegramUserID:         userID,
		PrrNotificationsThreadID:    threadID,
		PrrNotificationsThreadLabel: threadLabel,
	})
	if updateErr != nil {
		s.log.Warn("failed to store PRR topic", "chat_id", chatID, "thread_id", threadID, "error", updateErr)
		_, _ = s.getSender(b).SendMessage(chatID, "Не удалось сохранить тред для PRR-уведомлений.", nil)
		return nil
	}

	text := fmt.Sprintf("Цель PRR-радара сохранена: `%s`.", fsm.EscapeMarkdown(threadLabel))
	_, _ = s.getSender(b).SendMessage(chatID, text, &gotgbot.SendMessageOpts{ParseMode: "Markdown"})
	return nil
}

func registrationRequiredGroupInitText(b *gotgbot.Bot) string {
	base := "Для инициализации владелец группы должен быть зарегистрирован в боте.\n\n" +
		"1) Открой личный чат с ботом\n" +
		"2) Выполни /start и заверши регистрацию\n" +
		"3) Вернись в группу и повтори /init"

	if b == nil {
		return base
	}

	username := strings.TrimSpace(b.Username)
	username = strings.TrimPrefix(username, "@")
	if username == "" || username == "<missing>" {
		return base
	}

	link := fmt.Sprintf("https://t.me/%s?start=register_group_owner", username)
	return base + "\n\nБыстрый переход: " + link
}

func (s *telegramService) handleChatMember(b *gotgbot.Bot, ctx *ext.Context) error {
	if ctx.ChatMember == nil || s.queries == nil {
		return nil
	}

	chat := &ctx.ChatMember.Chat
	if !isGroupChat(chat) {
		return nil
	}

	updated := ctx.ChatMember.NewChatMember.GetUser()
	if updated.Id != 0 {
		isMember := isChatMemberActive(ctx.ChatMember.NewChatMember)
		if isMember {
			s.upsertKnownGroupMember(context.Background(), chat.Id, updated.Id, updated.IsBot, ctx.ChatMember.NewChatMember.GetStatus(), true)
		} else {
			s.markKnownGroupMemberLeft(context.Background(), chat.Id, updated.Id, ctx.ChatMember.NewChatMember.GetStatus())
		}
	}

	group, err := s.queries.GetTelegramGroupByChatID(context.Background(), chat.Id)
	if err != nil {
		// Group is not initialized in DB yet or failed to load: nothing to revoke.
		return nil
	}
	if !group.IsActive || !group.IsInitialized || updated.Id == 0 {
		return nil
	}

	updatedUserID := updated.Id
	newStatus := ctx.ChatMember.NewChatMember.GetStatus()
	shouldDeactivate := (updatedUserID == group.OwnerTelegramUserID && newStatus != gotgbot.ChatMemberStatusOwner) ||
		(updatedUserID != group.OwnerTelegramUserID && newStatus == gotgbot.ChatMemberStatusOwner)
	if shouldDeactivate {
		if err := s.queries.UnlinkTelegramGroupOwner(context.Background(), chat.Id); err != nil {
			s.log.Warn("failed to auto-unlink group owner on owner change", "chat_id", chat.Id, "error", err)
			return nil
		}
		s.log.Info("group owner auto-unlinked on owner change", "chat_id", chat.Id, "stored_owner_user_id", group.OwnerTelegramUserID, "updated_user_id", updatedUserID, "new_status", newStatus)
	}

	if b != nil && !updated.IsBot {
		s.tryAutoDefenderForKnownGroup(context.Background(), b, group, updated.Id)
		s.tryAutoAssignMemberTag(context.Background(), b, chat.Id, updated.Id)
	}

	return nil
}

func (s *telegramService) handleMyChatMember(b *gotgbot.Bot, ctx *ext.Context) error {
	if ctx.MyChatMember == nil {
		return nil
	}
	chat := &ctx.MyChatMember.Chat
	if !isGroupChat(chat) {
		return nil
	}

	chatID := chat.Id
	oldStatus := ""
	newStatus := ""
	if ctx.MyChatMember.OldChatMember != nil {
		oldStatus = ctx.MyChatMember.OldChatMember.GetStatus()
	}
	if ctx.MyChatMember.NewChatMember != nil {
		newStatus = ctx.MyChatMember.NewChatMember.GetStatus()
	}

	added := (oldStatus == gotgbot.ChatMemberStatusLeft || oldStatus == gotgbot.ChatMemberStatusBanned || oldStatus == "") &&
		(newStatus == gotgbot.ChatMemberStatusMember || newStatus == gotgbot.ChatMemberStatusAdministrator || newStatus == gotgbot.ChatMemberStatusOwner)
	removed := newStatus == gotgbot.ChatMemberStatusLeft || newStatus == gotgbot.ChatMemberStatusBanned

	if added {
		_, _ = s.getSender(b).SendMessage(chatID,
			"Спасибо за добавление.\n\nВладелец группы должен выполнить /init для инициализации.",
			nil,
		)
		return nil
	}

	if removed && s.queries != nil {
		if err := s.queries.DeactivateTelegramGroup(context.Background(), chatID); err != nil {
			s.log.Warn("failed to deactivate telegram group", "chat_id", chatID, "error", err)
		}
	}

	return nil
}

// handleTextMessage handles text messages (e.g., OTP code input).
func (s *telegramService) handleTextMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	if !isPrivateChat(ctx.EffectiveChat) {
		if ctx.Message != nil {
			s.captureKnownMembersFromGroupMessage(context.Background(), b, ctx.Message)
		}
		return nil
	}

	userID := ctx.EffectiveUser.Id
	text := ctx.Message.Text
	chatID := ctx.EffectiveChat.Id
	messageID := int64(ctx.Message.MessageId)
	bgCtx := context.WithValue(context.Background(), fsm.ContextKeyUserInfo, &fsm.UserInfo{
		ID:        userID,
		Username:  ctx.EffectiveUser.Username,
		FirstName: ctx.EffectiveUser.FirstName,
		LastName:  ctx.EffectiveUser.LastName,
		Platform:  "Telegram",
	})
	bgCtx = context.WithValue(bgCtx, fsm.ContextKeyNotifier, &telegramNotifier{sender: s.getSender(b)})
	bgCtx = context.WithValue(bgCtx, fsm.ContextKeyPRRGroupBroadcaster, s)
	if runner := s.newMemberTagRunner(b); runner != nil {
		bgCtx = context.WithValue(bgCtx, fsm.ContextKeyMemberTagRunner, runner)
	}
	if runner := s.newDefenderRunner(b); runner != nil {
		bgCtx = context.WithValue(bgCtx, fsm.ContextKeyDefenderRunner, runner)
	}

	s.log.Debug("text message received", "user_id", userID, "text", text)

	// Process the text message through FSM
	render, err := s.engine.Process(bgCtx, userID, text)
	if err != nil {
		if err == fsm.ErrEngineBusy {
			s.log.Debug("engine is busy, ignoring text message", "user_id", userID)
			_, _ = s.getSender(b).SendMessage(chatID, "⏳ Пожалуйста, подождите, идёт обновление данных...", nil)
			s.deleteUserMessage(b, chatID, messageID)
			return nil
		}
		s.log.Warn("fsm text processing failed", "error", err, "user_id", userID, "text", text)

		// Fallback: try to just get the current state and re-render it
		render, err = s.engine.GetCurrentRender(bgCtx, userID)
		if err != nil {
			s.log.Error("fallback render failed", "error", err, "user_id", userID)
			_, _ = s.getSender(b).SendMessage(chatID, "Произошла ошибка. Введите /start", nil)
			s.deleteUserMessage(b, chatID, messageID)
			return nil
		}
	}

	lastBotMessageID := s.getLastBotMessageID(userID)
	if lastBotMessageID > 0 {
		newMessageID, editErr := s.updateMessageRender(s.getSender(b), chatID, lastBotMessageID, render)
		if editErr == nil {
			if newMessageID > 0 {
				s.setLastBotMessageID(userID, &gotgbot.Message{MessageId: newMessageID})
			}
			s.deleteUserMessage(b, chatID, messageID)
			return nil
		}
		err = editErr
	} else {
		err = s.sendRenderAndStore(userID, chatID, s.getSender(b), render)
	}
	s.deleteUserMessage(b, chatID, messageID)
	return err
}

func (s *telegramService) deleteUserMessage(b *gotgbot.Bot, chatID int64, messageID int64) {
	if messageID == 0 {
		return
	}
	if _, err := s.getSender(b).DeleteMessage(chatID, messageID); err != nil {
		s.log.Debug("user message cleanup failed",
			"chat_id", chatID,
			"message_id", messageID,
			"error", err,
		)
	}
}

// handleCallback handles callback queries (buttons).
func (s *telegramService) handleCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	if !isPrivateChat(ctx.EffectiveChat) {
		return nil
	}

	cb := ctx.CallbackQuery
	userID := ctx.EffectiveUser.Id
	bgCtx := context.WithValue(context.Background(), fsm.ContextKeyUserInfo, &fsm.UserInfo{
		ID:        userID,
		Username:  ctx.EffectiveUser.Username,
		FirstName: ctx.EffectiveUser.FirstName,
		LastName:  ctx.EffectiveUser.LastName,
		Platform:  "Telegram",
	})
	bgCtx = context.WithValue(bgCtx, fsm.ContextKeyNotifier, &telegramNotifier{sender: s.getSender(b)})
	bgCtx = context.WithValue(bgCtx, fsm.ContextKeyPRRGroupBroadcaster, s)
	if runner := s.newMemberTagRunner(b); runner != nil {
		bgCtx = context.WithValue(bgCtx, fsm.ContextKeyMemberTagRunner, runner)
	}
	if runner := s.newDefenderRunner(b); runner != nil {
		bgCtx = context.WithValue(bgCtx, fsm.ContextKeyDefenderRunner, runner)
	}

	s.log.Debug("callback received", "user_id", userID, "data", cb.Data)

	if action, prrID, ok := fsm.ParsePRRNotifyCallback(cb.Data); ok {
		render, err := s.handlePRRNotificationCallback(bgCtx, userID, action, prrID)
		if err != nil {
			s.log.Warn("failed to process PRR notification callback", "user_id", userID, "data", cb.Data, "error", err)
			_, _ = s.getSender(b).AnswerCallbackQuery(cb.Id, &gotgbot.AnswerCallbackQueryOpts{Text: "Кнопка устарела, обновляю меню..."})
			render, err = s.engine.GetCurrentRender(bgCtx, userID)
			if err != nil {
				_, _ = s.getSender(b).AnswerCallbackQuery(cb.Id, &gotgbot.AnswerCallbackQueryOpts{Text: "Сессия истекла, введите /start"})
				return nil
			}
		} else {
			_, _ = s.getSender(b).AnswerCallbackQuery(cb.Id, &gotgbot.AnswerCallbackQueryOpts{})
		}

		newMessageID, renderErr := s.updateMessageRender(s.getSender(b), ctx.EffectiveChat.Id, cb.Message.GetMessageId(), render)
		if renderErr == nil && newMessageID > 0 {
			s.setLastBotMessageID(userID, &gotgbot.Message{MessageId: newMessageID})
		}
		return renderErr
	}

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

		// Avoid noisy stale toasts for passive pagination-caption buttons.
		if suppressStaleButtonToast(cb.Data) {
			_, _ = s.getSender(b).AnswerCallbackQuery(cb.Id, &gotgbot.AnswerCallbackQueryOpts{})
		} else {
			// Inform user that something went wrong but we recovered.
			_, _ = s.getSender(b).AnswerCallbackQuery(cb.Id, &gotgbot.AnswerCallbackQueryOpts{Text: "Кнопка устарела, обновляю меню..."})
		}
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

	newMessageID, err := s.updateMessageRender(s.getSender(b), ctx.EffectiveChat.Id, cb.Message.GetMessageId(), render)
	if err == nil && newMessageID > 0 {
		s.setLastBotMessageID(userID, &gotgbot.Message{MessageId: newMessageID})
	}
	return err
}

func suppressStaleButtonToast(callbackData string) bool {
	switch strings.TrimSpace(callbackData) {
	case "campus_filter_page":
		return true
	default:
		return false
	}
}

func (s *telegramService) handlePRRNotificationCallback(ctx context.Context, userID int64, action fsm.PRRNotifyAction, prrID int64) (*fsm.RenderObject, error) {
	lang := fsm.LangRu
	if s.engine != nil && s.engine.Repo() != nil {
		state, err := s.engine.Repo().GetState(ctx, userID)
		if err == nil && state != nil && strings.TrimSpace(state.Language) != "" {
			lang = state.Language
		}
	}

	prrIDStr := ""
	if prrID > 0 {
		prrIDStr = fmt.Sprintf("%d", prrID)
	}
	initialContext := map[string]any{
		"selected_my_prr_id": prrIDStr,
		"prr_id":             prrIDStr,
	}

	switch action {
	case fsm.PRRNotifyActionResume:
		if err := s.engine.InitStateWithLanguage(ctx, userID, "reviews.yaml", "MY_PRR_DETAILS", initialContext, lang); err != nil {
			return nil, err
		}
		return s.engine.Process(ctx, userID, "resume_from_negotiating")
	case fsm.PRRNotifyActionClose:
		if err := s.engine.InitStateWithLanguage(ctx, userID, "reviews.yaml", "MY_PRR_DETAILS", initialContext, lang); err != nil {
			return nil, err
		}
		return s.engine.Process(ctx, userID, "close_prr")
	case fsm.PRRNotifyActionMenu:
		if err := s.engine.InitStateWithLanguage(ctx, userID, "reviews.yaml", "PRR_MAIN_MENU", map[string]any{}, lang); err != nil {
			return nil, err
		}
		return s.engine.GetCurrentRender(ctx, userID)
	default:
		return nil, fmt.Errorf("unsupported PRR notification callback action: %s", action)
	}
}

func (s *telegramService) sendRenderAndStore(userID int64, chatID int64, sender Sender, render *fsm.RenderObject) error {
	msg, err := s.sendRender(sender, chatID, render)
	if err == nil {
		s.setLastBotMessageID(userID, msg)
	}
	return err
}

func (s *telegramService) sendRender(sender Sender, chatID int64, render *fsm.RenderObject) (*gotgbot.Message, error) {
	if render.Image != "" {
		s.fileIDsMu.RLock()
		fileID, cached := s.fileIDs[render.Image]
		s.fileIDsMu.RUnlock()

		var photo gotgbot.InputFileOrString
		var fileToClose *os.File

		if cached {
			s.log.Info("using cached file_id for image", "image_key", render.Image)
			photo = gotgbot.InputFileByID(fileID)
		} else {
			if strings.HasPrefix(render.Image, "imgcache:") {
				if s.imgCache != nil {
					data, ok := s.imgCache.Get(render.Image)
					if ok {
						photo = gotgbot.InputFileByReader("chart.png", bytes.NewReader(data))
					} else {
						s.log.Error("image not found in imgcache", "key", render.Image)
						return s.sendRenderText(sender, chatID, render)
					}
				} else {
					s.log.Error("imgcache not initialized but requested", "key", render.Image)
					return s.sendRenderText(sender, chatID, render)
				}
			} else {
				// Path Traversal protection: clean and validate path
				cleanPath := filepath.Clean(render.Image)
				if strings.Contains(cleanPath, "..") || strings.HasPrefix(cleanPath, "/") {
					s.log.Error("illegal image path attempted", "path", render.Image)
					return s.sendRenderText(sender, chatID, render)
				}

				var err error
				fileToClose, err = os.Open(cleanPath)
				if err != nil {
					s.log.Error("failed to open image file", "path", cleanPath, "error", err)
					return s.sendRenderText(sender, chatID, render)
				}
				photo = gotgbot.InputFileByReader("chart.png", fileToClose)
			}
		}

		msg, err := sender.SendPhoto(chatID, photo, &gotgbot.SendPhotoOpts{
			Caption:     render.Text,
			ParseMode:   "Markdown",
			ReplyMarkup: buildMarkup(render.Buttons),
		})

		if fileToClose != nil {
			_ = fileToClose.Close()
		}

		if err == nil && !cached && msg != nil && len(msg.Photo) > 0 {
			largestPhoto := msg.Photo[len(msg.Photo)-1]
			s.fileIDsMu.Lock()
			s.fileIDs[render.Image] = largestPhoto.FileId
			s.fileIDsMu.Unlock()
			s.log.Debug("cached file_id for image", "image_key", render.Image, "file_id", largestPhoto.FileId)
		}

		return msg, err
	}

	return s.sendRenderText(sender, chatID, render)
}

func (s *telegramService) sendRenderText(sender Sender, chatID int64, render *fsm.RenderObject) (*gotgbot.Message, error) {
	msg, err := sender.SendMessage(chatID, render.Text, &gotgbot.SendMessageOpts{
		ParseMode:   "Markdown",
		ReplyMarkup: buildMarkup(render.Buttons),
	})
	return msg, err
}

func (s *telegramService) updateMessageRender(sender Sender, chatID int64, messageID int64, render *fsm.RenderObject) (int64, error) {
	// If message has an image, we MUST use delete/send because Telegram doesn't support
	// converting a photo message to a text message via EditMessageText, or vice versa.
	// We use the presence of render.Image to determine the NEW type.
	// Note: We don't easily know if the OLD message had a photo, but we hit the error fallback if it did.

	if render.Image != "" {
		s.log.Debug("render has image, switching to photo message", "image", render.Image)
		_, _ = sender.DeleteMessage(chatID, messageID)
		msg, err := s.sendRender(sender, chatID, render)
		return getMessageID(msg), err
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
			return messageID, nil
		}

		// Bad Request: there is no text in the message to edit (happens when editing a photo message)
		if strings.Contains(err.Error(), "there is no text in the message to edit") ||
			strings.Contains(err.Error(), "message can't be edited") {
			s.log.Debug("message type mismatch (photo/text), using delete/send fallback")
			_, _ = sender.DeleteMessage(chatID, messageID)
			msg, sendErr := s.sendRender(sender, chatID, render)
			return getMessageID(msg), sendErr
		}

		s.log.Warn("edit failed, fallback to delete/send", "error", err)
		_, _ = sender.DeleteMessage(chatID, messageID)
		msg, sendErr := s.sendRender(sender, chatID, render)
		return getMessageID(msg), sendErr
	}
	return messageID, nil
}

const lastBotMessageIDKey = "last_bot_message_id"

func (s *telegramService) setLastBotMessageID(userID int64, msg *gotgbot.Message) {
	if msg == nil || msg.MessageId == 0 || s.engine == nil || s.engine.Repo() == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	state, err := s.engine.Repo().GetState(ctx, userID)
	if err != nil || state == nil {
		return
	}
	if state.Context == nil {
		state.Context = make(map[string]any)
	}
	state.Context[lastBotMessageIDKey] = int64(msg.MessageId)
	_ = s.engine.Repo().SetState(ctx, state)
}

func (s *telegramService) getLastBotMessageID(userID int64) int64 {
	if s.engine == nil || s.engine.Repo() == nil {
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	state, err := s.engine.Repo().GetState(ctx, userID)
	if err != nil || state == nil || state.Context == nil {
		return 0
	}

	return parseMessageID(state.Context[lastBotMessageIDKey])
}

func parseMessageID(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
	}
	return 0
}

func getMessageID(msg *gotgbot.Message) int64 {
	if msg == nil {
		return 0
	}
	return int64(msg.MessageId)
}

func buildMarkup(rows [][]fsm.ButtonRender) gotgbot.InlineKeyboardMarkup {
	var inlineRows [][]gotgbot.InlineKeyboardButton
	for _, row := range rows {
		var inlineRow []gotgbot.InlineKeyboardButton
		for _, btn := range row {
			if btn.URL != "" {
				// URL button
				inlineRow = append(inlineRow, gotgbot.InlineKeyboardButton{
					Text: btn.Text,
					Url:  btn.URL,
				})
			} else {
				// Callback button
				inlineRow = append(inlineRow, gotgbot.InlineKeyboardButton{
					Text:         btn.Text,
					CallbackData: btn.Data,
				})
			}
		}
		inlineRows = append(inlineRows, inlineRow)
	}
	return gotgbot.InlineKeyboardMarkup{InlineKeyboard: inlineRows}
}
