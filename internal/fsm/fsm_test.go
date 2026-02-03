package fsm

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlowParser_GetFlow(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	parser := NewFlowParser("../../docs/specs/flows", logger)

	t.Run("load main menu flow", func(t *testing.T) {
		flow, err := parser.GetFlow("main_menu.yaml")
		require.NoError(t, err)
		require.NotNil(t, flow)
		assert.Contains(t, flow.States, "MAIN_MENU")
	})

	t.Run("load settings flow", func(t *testing.T) {
		flow, err := parser.GetFlow("settings.yaml")
		require.NoError(t, err)
		require.NotNil(t, flow)
		assert.Contains(t, flow.States, "SETTINGS_MENU")
	})

	t.Run("cache works", func(t *testing.T) {
		// First call
		flow1, err1 := parser.GetFlow("main_menu.yaml")
		require.NoError(t, err1)

		// Second call should return cached version
		flow2, err2 := parser.GetFlow("main_menu.yaml")
		require.NoError(t, err2)

		// Should be the same pointer (cached)
		assert.Equal(t, flow1, flow2)
	})

	t.Run("non-existent flow", func(t *testing.T) {
		flow, err := parser.GetFlow("nonexistent.yaml")
		assert.Error(t, err)
		assert.Nil(t, flow)
	})
}

func TestMemoryStateRepository(t *testing.T) {
	repo := NewMemoryStateRepository()
	ctx := context.Background()
	userID := int64(12345)

	t.Run("get state for non-existent user", func(t *testing.T) {
		state, err := repo.GetState(ctx, userID)
		require.NoError(t, err)
		assert.Nil(t, state)
	})

	t.Run("set and get state", func(t *testing.T) {
		testState := &UserState{
			UserID:       userID,
			CurrentFlow:  "main_menu.yaml",
			CurrentState: "MAIN_MENU",
			Language:     "ru",
			Context:      map[string]interface{}{"test": "value"},
		}

		err := repo.SetState(ctx, testState)
		require.NoError(t, err)

		retrieved, err := repo.GetState(ctx, userID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)

		assert.Equal(t, testState.UserID, retrieved.UserID)
		assert.Equal(t, testState.CurrentFlow, retrieved.CurrentFlow)
		assert.Equal(t, testState.CurrentState, retrieved.CurrentState)
		assert.Equal(t, testState.Language, retrieved.Language)
		assert.Equal(t, testState.Context["test"], retrieved.Context["test"])
	})

	t.Run("update existing state", func(t *testing.T) {
		updatedState := &UserState{
			UserID:       userID,
			CurrentFlow:  "settings.yaml",
			CurrentState: "SETTINGS_MENU",
			Language:     "en",
			Context:      map[string]interface{}{"updated": true},
		}

		err := repo.SetState(ctx, updatedState)
		require.NoError(t, err)

		retrieved, err := repo.GetState(ctx, userID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)

		assert.Equal(t, "settings.yaml", retrieved.CurrentFlow)
		assert.Equal(t, "SETTINGS_MENU", retrieved.CurrentState)
		assert.Equal(t, "en", retrieved.Language)
	})
}

func TestEngine_InitState(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	parser := NewFlowParser("../../docs/specs/flows", logger)
	repo := NewMemoryStateRepository()
	engine := NewEngine(parser, repo, logger)

	ctx := context.Background()
	userID := int64(67890)

	t.Run("initialize state", func(t *testing.T) {
		err := engine.InitState(ctx, userID, "main_menu.yaml", "MAIN_MENU")
		require.NoError(t, err)

		state, err := repo.GetState(ctx, userID)
		require.NoError(t, err)
		require.NotNil(t, state)

		assert.Equal(t, userID, state.UserID)
		assert.Equal(t, "main_menu.yaml", state.CurrentFlow)
		assert.Equal(t, "MAIN_MENU", state.CurrentState)
		assert.Equal(t, "ru", state.Language)
	})
}

func TestEngine_GetCurrentRender(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	parser := NewFlowParser("../../docs/specs/flows", logger)
	repo := NewMemoryStateRepository()
	engine := NewEngine(parser, repo, logger)

	ctx := context.Background()
	userID := int64(11111)

	t.Run("render main menu", func(t *testing.T) {
		err := engine.InitState(ctx, userID, "main_menu.yaml", "MAIN_MENU")
		require.NoError(t, err)

		render, err := engine.GetCurrentRender(ctx, userID)
		require.NoError(t, err)
		require.NotNil(t, render)

		assert.NotEmpty(t, render.Text)
		assert.NotEmpty(t, render.Buttons)
		assert.Contains(t, render.Text, "Главное меню")
	})

	t.Run("no state initialized", func(t *testing.T) {
		nonExistentUser := int64(99999)
		render, err := engine.GetCurrentRender(ctx, nonExistentUser)
		assert.Error(t, err)
		assert.Nil(t, render)
	})
}

func TestEngine_Process(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	parser := NewFlowParser("../../docs/specs/flows", logger)
	repo := NewMemoryStateRepository()
	engine := NewEngine(parser, repo, logger)

	ctx := context.Background()
	userID := int64(22222)

	t.Run("transition to settings", func(t *testing.T) {
		// Initialize to main menu
		err := engine.InitState(ctx, userID, "main_menu.yaml", "MAIN_MENU")
		require.NoError(t, err)

		// Click settings button
		render, err := engine.Process(ctx, userID, "settings")
		require.NoError(t, err)
		require.NotNil(t, render)

		// Verify we're in settings now
		state, err := repo.GetState(ctx, userID)
		require.NoError(t, err)
		assert.Equal(t, "settings.yaml", state.CurrentFlow)
		assert.Equal(t, "SETTINGS_MENU", state.CurrentState)
	})

	t.Run("invalid transition", func(t *testing.T) {
		err := engine.InitState(ctx, userID, "main_menu.yaml", "MAIN_MENU")
		require.NoError(t, err)

		// Try invalid button
		render, err := engine.Process(ctx, userID, "nonexistent_button")
		assert.Error(t, err)
		assert.Nil(t, render)
	})

	t.Run("no active state", func(t *testing.T) {
		nonExistentUser := int64(88888)
		render, err := engine.Process(ctx, nonExistentUser, "settings")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active state")
		assert.Nil(t, render)
	})
}

func TestEngine_ReplaceVariables(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	parser := NewFlowParser("../../docs/specs/flows", logger)
	repo := NewMemoryStateRepository()
	engine := NewEngine(parser, repo, logger)

	t.Run("replace default variables", func(t *testing.T) {
		state := &UserState{
			Language: "ru",
			Context:  map[string]interface{}{},
		}

		text := "Login: {s21_login}, Level: {level}"
		result := engine.replaceVariables(text, state)

		assert.Contains(t, result, "student")
		assert.Contains(t, result, "0")
	})

	t.Run("replace language flag for ru", func(t *testing.T) {
		state := &UserState{
			Language: "ru",
			Context:  map[string]interface{}{},
		}

		text := "Flag: {language_flag}"
		result := engine.replaceVariables(text, state)

		assert.Contains(t, result, "🇷🇺")
	})

	t.Run("replace language flag for en", func(t *testing.T) {
		state := &UserState{
			Language: "en",
			Context:  map[string]interface{}{},
		}

		text := "Flag: {language_flag}"
		result := engine.replaceVariables(text, state)

		assert.Contains(t, result, "🇺🇸")
	})

	t.Run("replace context variables", func(t *testing.T) {
		state := &UserState{
			Language: "ru",
			Context: map[string]interface{}{
				"s21_login": "vgy789",
				"level":     "5",
			},
		}

		text := "User: {s21_login}, Level: {level}"
		result := engine.replaceVariables(text, state)

		assert.Contains(t, result, "vgy789")
		assert.Contains(t, result, "5")
	})
}

func TestEngine_RegistrationFlow(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	parser := NewFlowParser("../../docs/specs/flows", logger)
	repo := NewMemoryStateRepository()
	engine := NewEngine(parser, repo, logger)

	ctx := context.Background()
	userID := int64(33333)

	t.Run("start -> select language -> input login", func(t *testing.T) {
		// 1. Initial state (SELECT_LANGUAGE)
		err := engine.InitState(ctx, userID, "registration.yaml", "SELECT_LANGUAGE")
		require.NoError(t, err)

		// 2. Click set_ru
		// This should:
		// - set language to ru
		// - transition to START
		// - auto-transition from START to INPUT_LOGIN (based on registered == false mock)
		render, err := engine.Process(ctx, userID, "set_ru")
		require.NoError(t, err)
		require.NotNil(t, render)

		// Verify state
		state, err := repo.GetState(ctx, userID)
		require.NoError(t, err)
		assert.Equal(t, "ru", state.Language)
		assert.Equal(t, "registration.yaml", state.CurrentFlow)
		assert.Equal(t, "INPUT_LOGIN", state.CurrentState)

		// Verify render text (from INPUT_LOGIN)
		assert.Contains(t, render.Text, "Введи логин School21")
	})
}

func TestEngine_EscapeMarkdown(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	parser := NewFlowParser("../../docs/specs/flows", logger)
	repo := NewMemoryStateRepository()
	engine := NewEngine(parser, repo, logger)

	t.Run("escape underscores", func(t *testing.T) {
		text := "/api_token and /provider_credentials"
		result := engine.escapeMarkdown(text)

		// After escaping, underscores should be prefixed with backslash
		assert.Contains(t, result, "\\_")
		assert.Contains(t, result, "/api\\_token")
		assert.Contains(t, result, "/provider\\_credentials")
	})

	t.Run("empty text", func(t *testing.T) {
		result := engine.escapeMarkdown("")
		assert.Equal(t, "", result)
	})
}
