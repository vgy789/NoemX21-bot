package fsm

import (
	"context"
	"fmt"
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
		assert.Contains(t, render.Text, "Личный кабинет")
	})

	t.Run("render buttons grouped by row", func(t *testing.T) {
		userState := &UserState{Language: LangRu}
		flowState := &State{
			Interface: Interface{
				Text: map[string]string{LangRu: "Buttons text"},
				Buttons: []Button{
					{ID: "b1", Label: "B1", Row: 1},
					{ID: "b2", Label: "B2", Row: 1},
					{ID: "b3", Label: "B3", Row: 2},
					{ID: "b4", Label: "B4"}, // No row
				},
			},
		}

		render := engine.renderState(userState, flowState)
		assert.Len(t, render.Buttons, 3) // Row 1, Row 2, Row 3(auto)
		assert.Len(t, render.Buttons[0], 2)
		assert.Equal(t, "b1", render.Buttons[0][0].Data)
		assert.Equal(t, "b2", render.Buttons[0][1].Data)
		assert.Len(t, render.Buttons[1], 1)
		assert.Equal(t, "b3", render.Buttons[1][0].Data)
		assert.Len(t, render.Buttons[2], 1)
		assert.Equal(t, "b4", render.Buttons[2][0].Data)
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
			Language: DefaultLanguage,
			Context:  map[string]interface{}{},
		}

		text := fmt.Sprintf("Login: %s, Level: %s", VarS21Login, VarLevel)
		result := engine.replaceVariables(text, state)

		assert.Contains(t, result, "jonnabin")
		assert.Contains(t, result, "11")
	})

	t.Run("replace language flag for ru", func(t *testing.T) {
		state := &UserState{
			Language: "ru",
			Context:  map[string]interface{}{},
		}

		text := "Flag: {my_lang_emoji}"
		result := engine.replaceVariables(text, state)

		assert.Contains(t, result, "🇷🇺")
	})

	t.Run("replace language flag for en", func(t *testing.T) {
		state := &UserState{
			Language: "en",
			Context:  map[string]interface{}{},
		}

		text := "Flag: {my_lang_emoji}"
		result := engine.replaceVariables(text, state)

		assert.Contains(t, result, "🇺🇸")
	})

	t.Run("replace context variables", func(t *testing.T) {
		state := &UserState{
			Language: "ru",
			Context: map[string]interface{}{
				"s21_login": "vgy_789", // With underscore
				"level":     "5*",      // With asterisk
			},
		}

		text := "User: {s21_login}, Level: {level}"
		result := engine.replaceVariables(text, state)

		// Variable values should be escaped
		assert.Contains(t, result, "vgy\\_789")
		assert.Contains(t, result, "5*")
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

		// Click set_ru
		// Now it transitions directly to main_menu.yaml/MAIN_MENU (per user changes)
		render, err := engine.Process(ctx, userID, "set_ru")
		require.NoError(t, err)
		require.NotNil(t, render)

		state, err := repo.GetState(ctx, userID)
		require.NoError(t, err)
		assert.Equal(t, "main_menu.yaml", state.CurrentFlow)
		assert.Equal(t, "MAIN_MENU", state.CurrentState)

		assert.Contains(t, render.Text, "Личный кабинет")
	})
}

func TestEngine_EscapeMarkdown(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	parser := NewFlowParser("../../docs/specs/flows", logger)
	repo := NewMemoryStateRepository()
	engine := NewEngine(parser, repo, logger)

	t.Run("escape underscores", func(t *testing.T) {
		text := "vgy_789"
		result := engine.escapeMarkdown(text)

		assert.Equal(t, "vgy\\_789", result)
	})

	t.Run("empty text", func(t *testing.T) {
		result := engine.escapeMarkdown("")
		assert.Equal(t, "", result)
	})
}
