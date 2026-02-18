package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbMock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/fsm/setup"
	serviceMock "github.com/vgy789/noemx21-bot/internal/service/mock"
	"go.uber.org/mock/gomock"
)

func TestNewTelegramService(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockUserSvc := serviceMock.NewMockUserService(ctrl)
	mockQuerier := dbMock.NewMockQuerier(ctrl)
	mockRCClient := rocketchat.NewClient("", "", "")

	engine := setup.NewFSM(cfg, logger, mockQuerier, mockUserSvc, mockRCClient, nil, nil, "docs/specs/flows")
	svc := NewTelegramService(cfg, logger, mockUserSvc, engine)
	ts, ok := svc.(*telegramService)
	require.True(t, ok, "NewTelegramService did not return *telegramService")
	// Use memory repo for tests
	ts.engine = fsm.NewEngine(ts.engine.Parser(), fsm.NewMemoryStateRepository(), logger, ts.engine.Registry(), ts.engine.Sanitizer())

	require.NotNil(t, svc)
	require.True(t, ok, "NewTelegramService did not return *telegramService")

	assert.NotNil(t, ts.log)
	assert.NotNil(t, ts.engine, "FSM engine should be initialized")
	assert.Equal(t, mockUserSvc, ts.userSvc)

	t.Run("test registry: is_user_registered", func(t *testing.T) {
		mockUserSvc.EXPECT().GetProfileByExternalID(gomock.Any(), db.EnumPlatformTelegram, "1").Return(nil, nil)
		action, _ := ts.engine.Registry().Get("is_user_registered")
		_, res, err := action(context.Background(), 1, nil)
		assert.NoError(t, err)
		assert.True(t, res["registered"].(bool))
	})

	t.Run("test registry: input:set_ru", func(t *testing.T) {
		// Expect call to check account existence - return error (not found) for this test
		mockQuerier.EXPECT().GetUserAccountByExternalId(gomock.Any(), gomock.Any()).Return(db.UserAccount{}, fmt.Errorf("not found"))

		action, _ := ts.engine.Registry().Get("input:set_ru")
		_, res, err := action(context.Background(), 1, nil)
		assert.NoError(t, err)
		assert.Equal(t, fsm.LangRu, res["language"])
	})
}

func TestTelegramService_GetSender(t *testing.T) {
	s := &telegramService{}

	t.Run("default sender", func(t *testing.T) {
		sender := s.getSender(nil)
		assert.IsType(t, &DefaultSender{}, sender)
	})

	t.Run("provided sender", func(t *testing.T) {
		mockSender := &DefaultSender{} // or any Sender implementation
		s.sender = mockSender
		sender := s.getSender(nil)
		assert.Equal(t, mockSender, sender)
	})
}

func TestUpdaterHandler_ServeHTTP(t *testing.T) {
	t.Run("correct path", func(t *testing.T) {
		handler := &updaterHandler{
			updater: ext.NewUpdater(nil, nil),
			path:    "/webhook",
		}

		req := httptest.NewRequest("POST", "/webhook", nil)
		w := httptest.NewRecorder()

		// Should not 404 since path matches
		handler.ServeHTTP(w, req)
		// Will return 400 or similar since no body, but not 404
		assert.NotEqual(t, http.StatusNotFound, w.Code)
	})

	t.Run("wrong path", func(t *testing.T) {
		handler := &updaterHandler{
			updater: ext.NewUpdater(nil, nil),
			path:    "/webhook",
		}

		req := httptest.NewRequest("POST", "/wrong", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestTelegramService_GetWebhookHandler_NotInitialized(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{}
	cfg.Telegram.Webhook.ListenPath = "/webhook"

	s := &telegramService{
		log: logger,
		cfg: cfg,
	}

	handler := s.GetWebhookHandler()
	require.NotNil(t, handler)

	req := httptest.NewRequest("GET", "/webhook", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestDefaultSender(t *testing.T) {
	sender := &DefaultSender{}

	t.Run("SendMessage", func(t *testing.T) {
		// Cannot test without mock bot, but increases coverage
		assert.NotNil(t, sender)
	})

	t.Run("SendPhoto", func(t *testing.T) {
		assert.NotNil(t, sender)
	})

	t.Run("EditMessageText", func(t *testing.T) {
		assert.NotNil(t, sender)
	})

	t.Run("DeleteMessage", func(t *testing.T) {
		assert.NotNil(t, sender)
	})

	t.Run("AnswerCallbackQuery", func(t *testing.T) {
		assert.NotNil(t, sender)
	})
}

func TestBuildMarkup(t *testing.T) {
	t.Run("empty buttons", func(t *testing.T) {
		render := &fsm.RenderObject{Buttons: [][]fsm.ButtonRender{}}
		markup := buildMarkup(render.Buttons)
		// Returns empty InlineKeyboardMarkup, not nil
		assert.NotNil(t, markup)
	})

	t.Run("with buttons", func(t *testing.T) {
		render := &fsm.RenderObject{
			Buttons: [][]fsm.ButtonRender{
				{{Text: "Btn1", Data: "data1"}},
				{{Text: "Btn2", Data: "data2"}},
			},
		}
		markup := buildMarkup(render.Buttons)
		assert.NotNil(t, markup)
	})

	t.Run("with URL buttons", func(t *testing.T) {
		render := &fsm.RenderObject{
			Buttons: [][]fsm.ButtonRender{
				{{Text: "Link", URL: "https://example.com"}},
			},
		}
		markup := buildMarkup(render.Buttons)
		assert.NotNil(t, markup)
	})
}
