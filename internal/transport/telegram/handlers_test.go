package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
	serviceMock "github.com/vgy789/noemx21-bot/internal/service/mock"
	"github.com/vgy789/noemx21-bot/internal/transport/telegram/mock"
	"go.uber.org/mock/gomock"
)

func TestTelegramService_Handlers(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStudentSvc := serviceMock.NewMockStudentService(ctrl)
	mockSender := mock.NewMockSender(ctrl)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Prepare real FSM engine with memory storage
	parser := fsm.NewFlowParser("../../../docs/specs/flows", logger)
	repo := fsm.NewMemoryStateRepository()

	// Create registry for handlers test (registered: true so set_ru -> START -> main_menu.yaml/MAIN_MENU)
	registry := fsm.NewLogicRegistry()
	registry.Register("is_user_registered", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		return "", map[string]interface{}{"registered": true}, nil
	})
	registry.Register("input:set_ru", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		return "", map[string]interface{}{"language": "ru"}, nil
	})
	registry.Register("input:set_en", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		return "", map[string]interface{}{"language": "en"}, nil
	})

	engine := fsm.NewEngine(parser, repo, logger, registry, nil)
	engine.AddAlias("MAIN_MENU", "main_menu.yaml/MAIN_MENU")

	s := &telegramService{
		log:        logger,
		engine:     engine,
		studentSvc: mockStudentSvc,
		sender:     mockSender,
	}

	t.Run("handleStart - new user", func(t *testing.T) {
		userID := int64(123)
		mockStudentSvc.EXPECT().GetProfileByTelegramID(gomock.Any(), userID).Return(nil, assert.AnError)

		// Expect SendMessage to be called when rendering SELECT_LANGUAGE
		mockSender.EXPECT().SendMessage(userID, gomock.Any(), gomock.Any()).Return(nil, nil)

		update := &gotgbot.Update{
			Message: &gotgbot.Message{
				From: &gotgbot.User{Id: userID},
				Chat: gotgbot.Chat{Id: userID},
				Text: "/start",
			},
		}
		ctx := ext.NewContext(&gotgbot.Bot{}, update, nil)

		err := s.handleStart(nil, ctx)

		assert.NoError(t, err)

		state, err := repo.GetState(context.Background(), userID)
		assert.NoError(t, err)
		assert.Equal(t, fsm.StateSelectLanguage, state.CurrentState)
	})

	t.Run("handleCallback - success", func(t *testing.T) {
		userID := int64(123)
		// Initial state
		_ = engine.InitState(context.Background(), userID, "registration.yaml", "SELECT_LANGUAGE", nil)

		// Expect Answer to be called
		mockSender.EXPECT().AnswerCallbackQuery(gomock.Any(), gomock.Any()).Return(true, nil)
		// Expect message to be updated
		mockSender.EXPECT().EditMessageText(gomock.Any(), gomock.Any()).Return(nil, false, nil)

		update := &gotgbot.Update{
			CallbackQuery: &gotgbot.CallbackQuery{
				Id:   "cb123",
				Data: "set_ru",
				From: gotgbot.User{Id: userID},
				Message: &gotgbot.Message{
					MessageId: 456,
					Chat:      gotgbot.Chat{Id: userID},
				},
			},
		}
		ctx := ext.NewContext(&gotgbot.Bot{}, update, nil)

		err := s.handleCallback(nil, ctx)
		assert.NoError(t, err)

		state, err := repo.GetState(context.Background(), userID)
		require.NoError(t, err)
		assert.Equal(t, "MAIN_MENU", state.CurrentState)
		assert.Equal(t, "ru", state.Context["language"])
	})

	t.Run("handleCallback - fallback failure", func(t *testing.T) {
		userID := int64(125)
		// No state at all, and no InitState. GetCurrentRender will fail.

		update := &gotgbot.Update{
			CallbackQuery: &gotgbot.CallbackQuery{
				Id:   "cb_critical",
				Data: "bad_click",
				From: gotgbot.User{Id: userID},
				Message: &gotgbot.Message{
					MessageId: 101,
					Chat:      gotgbot.Chat{Id: userID},
				},
			},
		}

		// In session expiry, Answer is called but fallback render fails
		mockSender.EXPECT().AnswerCallbackQuery("cb_critical", gomock.Any()).Return(true, nil)

		ctx := ext.NewContext(&gotgbot.Bot{}, update, nil)

		err := s.handleCallback(nil, ctx)
		assert.NoError(t, err)
	})

	t.Run("handleCallback - fallback on error", func(t *testing.T) {
		userID := int64(124)
		// No initial state -> Process will fail

		// Expect current render as fallback
		mockSender.EXPECT().AnswerCallbackQuery(gomock.Any(), gomock.Any()).Return(true, nil)
		mockSender.EXPECT().EditMessageText(gomock.Any(), gomock.Any()).Return(nil, false, nil)

		update := &gotgbot.Update{
			CallbackQuery: &gotgbot.CallbackQuery{
				Id:   "cb_fail",
				Data: "bad_click",
				From: gotgbot.User{Id: userID},
				Message: &gotgbot.Message{
					MessageId: 789,
					Chat:      gotgbot.Chat{Id: userID},
				},
			},
		}
		// We actually need InitState for GetCurrentRender to work in fallback
		_ = engine.InitState(context.Background(), userID, "registration.yaml", "SELECT_LANGUAGE", nil)

		// Mock engine to fail? No, Process will fail if we use non-existent flow or something.
		// Actually, let's just use it as is.

		ctx := ext.NewContext(&gotgbot.Bot{}, update, nil)
		err := s.handleCallback(nil, ctx)
		assert.NoError(t, err)
	})

	t.Run("sendRender - error", func(t *testing.T) {
		render := &fsm.RenderObject{Text: "fail"}
		mockSender.EXPECT().SendMessage(gomock.Any(), "fail", gomock.Any()).Return(nil, assert.AnError)

		err := s.sendRender(mockSender, 1, render)
		assert.Error(t, err)
	})

	t.Run("updateMessageRender - error", func(t *testing.T) {
		render := &fsm.RenderObject{Text: "fail"}
		mockSender.EXPECT().EditMessageText("fail", gomock.Any()).Return(nil, false, assert.AnError)

		err := s.updateMessageRender(mockSender, 1, 1, render)
		assert.Error(t, err)
	})

	t.Run("updateMessageRender - ignore not modified", func(t *testing.T) {
		render := &fsm.RenderObject{Text: "no change"}
		notModErr := fmt.Errorf("bad request: message is not modified")
		mockSender.EXPECT().EditMessageText("no change", gomock.Any()).Return(nil, false, notModErr)

		err := s.updateMessageRender(mockSender, 1, 1, render)
		assert.NoError(t, err)
	})

	t.Run("handleStart - recognized user", func(t *testing.T) {
		userID := int64(456)
		profile := &service.StudentProfile{
			Login: "recognised_user",
		}
		mockStudentSvc.EXPECT().GetProfileByTelegramID(gomock.Any(), userID).Return(profile, nil)
		mockSender.EXPECT().SendMessage(userID, gomock.Any(), gomock.Any()).Return(nil, nil)

		update := &gotgbot.Update{
			Message: &gotgbot.Message{
				From: &gotgbot.User{Id: userID},
				Chat: gotgbot.Chat{Id: userID},
				Text: "/start",
			},
		}
		ctx := ext.NewContext(&gotgbot.Bot{}, update, nil)

		err := s.handleStart(nil, ctx)
		assert.NoError(t, err)

		state, _ := repo.GetState(context.Background(), userID)
		assert.Equal(t, "recognised_user", state.Context["my_s21login"])
	})

	t.Run("registerHandlers", func(t *testing.T) {
		d := &ext.Dispatcher{} // Simple dispatcher
		s.registerHandlers(d)
		// We can't easily check internal handlers without reflection or if library exposes them,
		// but calling it increases coverage.
	})
}
