package fsm

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
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
			Context:      map[string]any{"test": "value"},
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
			Context:      map[string]any{"updated": true},
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
	engine := NewEngine(parser, repo, logger, nil, nil)

	ctx := context.Background()
	userID := int64(67890)

	t.Run("initialize state", func(t *testing.T) {
		err := engine.InitState(ctx, userID, "main_menu.yaml", "MAIN_MENU", nil)
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
	engine := NewEngine(parser, repo, logger, nil, nil)

	ctx := context.Background()
	userID := int64(11111)

	t.Run("render main menu", func(t *testing.T) {
		err := engine.InitState(ctx, userID, "main_menu.yaml", "MAIN_MENU", nil)
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

		render := engine.renderState(context.Background(), userState, flowState)
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
	engine := NewEngine(parser, repo, logger, nil, nil)
	engine.AddAlias("SETTINGS_MENU", "settings.yaml/SETTINGS_MENU")

	ctx := context.Background()
	userID := int64(22222)

	t.Run("transition to settings", func(t *testing.T) {
		// Initialize to main menu
		err := engine.InitState(ctx, userID, "main_menu.yaml", "MAIN_MENU", nil)
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
		err := engine.InitState(ctx, userID, "main_menu.yaml", "MAIN_MENU", nil)
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

	t.Run("input normalization", func(t *testing.T) {
		err := engine.InitState(ctx, userID, "main_menu.yaml", "MAIN_MENU", nil)
		require.NoError(t, err)

		// Click settings button with leading/trailing spaces (engine trims input; button id is "settings")
		render, err := engine.Process(ctx, userID, "  settings  ")
		require.NoError(t, err)
		require.NotNil(t, render)

		state, _ := repo.GetState(ctx, userID)
		assert.Equal(t, "SETTINGS_MENU", state.CurrentState)
	})
}

func TestEngine_ReplaceVariables(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	parser := NewFlowParser("../../docs/specs/flows", logger)
	repo := NewMemoryStateRepository()
	engine := NewEngine(parser, repo, logger, nil, nil)

	t.Run("replace default variables", func(t *testing.T) {
		state := &UserState{
			Language: DefaultLanguage,
			Context:  map[string]any{},
		}

		text := fmt.Sprintf("Login: %s, Level: %s", VarS21Login, VarLevel)
		result := engine.replaceVariables(text, state)

		assert.Contains(t, result, "Гость")
		assert.Contains(t, result, "99")
	})

	t.Run("replace language flag for ru", func(t *testing.T) {
		state := &UserState{
			Language: "ru",
			Context:  map[string]any{},
		}

		text := "Flag: {my_lang_emoji}"
		result := engine.replaceVariables(text, state)

		assert.Contains(t, result, "🇷🇺")
	})

	t.Run("replace language flag for en", func(t *testing.T) {
		state := &UserState{
			Language: "en",
			Context:  map[string]any{},
		}

		text := "Flag: {my_lang_emoji}"
		result := engine.replaceVariables(text, state)

		assert.Contains(t, result, "🇺🇸")
	})

	t.Run("replace context variables", func(t *testing.T) {
		sanitizer := func(s string) string {
			return strings.ReplaceAll(s, "_", "\\_")
		}
		engineEscaping := NewEngine(parser, repo, logger, nil, sanitizer)

		state := &UserState{
			Language: "ru",
			Context: map[string]any{
				"s21_login": "vgy_789", // With underscore
				"level":     "5*",      // With asterisk
			},
		}

		text := "User: {s21_login}, Level: {level}"
		result := engineEscaping.replaceVariables(text, state)

		// Variable values should be escaped
		assert.Contains(t, result, "vgy\\_789")
		assert.Contains(t, result, "5*")
	})

	t.Run("replace overlapping context keys deterministically", func(t *testing.T) {
		state := &UserState{
			Language: "ru",
			Context: map[string]any{
				"campus":    "21 Novosibirsk",
				"campus_id": "46e7d965-21e9-4936-bea9-f5ea0d1fddf2",
			},
		}
		text := "campus_id=$context.campus_id campus=$context.campus"
		result := engine.replaceVariables(text, state)
		assert.Contains(t, result, "campus_id=46e7d965-21e9-4936-bea9-f5ea0d1fddf2")
		assert.Contains(t, result, "campus=21 Novosibirsk")
	})
}

func TestEngine_RegistrationFlow(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	parser := NewFlowParser("../../docs/specs/flows", logger)
	repo := NewMemoryStateRepository()
	registry := NewLogicRegistry()
	registry.Register("is_user_registered", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", map[string]any{"registered": true}, nil
	})
	engine := NewEngine(parser, repo, logger, registry, nil)
	engine.AddAlias("MAIN_MENU", "main_menu.yaml/MAIN_MENU")

	ctx := context.Background()
	userID := int64(33333)

	t.Run("start -> select language -> input login", func(t *testing.T) {
		err := engine.InitState(ctx, userID, "registration.yaml", "SELECT_LANGUAGE", nil)
		require.NoError(t, err)

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

func TestLogicRegistry(t *testing.T) {
	registry := NewLogicRegistry()

	t.Run("register and get", func(t *testing.T) {
		action := func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
			return "NEXT", nil, nil
		}
		registry.Register("test_action", action)

		act, ok := registry.Get("test_action")
		assert.True(t, ok)
		assert.NotNil(t, act)
	})

	t.Run("get non-existent", func(t *testing.T) {
		act, ok := registry.Get("non_existent")
		assert.False(t, ok)
		assert.Nil(t, act)
	})
}

func TestEngine_EvaluateCondition(t *testing.T) {
	e := &Engine{}

	tests := []struct {
		name      string
		condition string
		ctx       map[string]any
		want      bool
	}{
		{"equal string", "key == value", map[string]any{"key": "value"}, true},
		{"not equal string", "key == value", map[string]any{"key": "other"}, false},
		{"quoted value", "key == 'value'", map[string]any{"key": "value"}, true},
		{"double quoted value", "key == \"value\"", map[string]any{"key": "value"}, true},
		{"numeric value", "count == 10", map[string]any{"count": 10}, true},
		{"boolean value", "flag == true", map[string]any{"flag": true}, true},
		{"missing key", "missing == val", map[string]any{"key": "val"}, false},
		{"invalid format", "invalid condition", map[string]any{"key": "val"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, e.evaluateCondition(tt.condition, tt.ctx))
		})
	}
}

func TestEngine_SystemStateWithRegistry(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := NewMemoryStateRepository()
	registry := NewLogicRegistry()

	registry.Register("check_auth", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		if userID == 1 {
			return "", map[string]any{"is_auth": true}, nil
		}
		return "FORCED_STATE", nil, nil
	})

	engine := NewEngine(nil, repo, logger, registry, nil)

	t.Run("action returns forced state", func(t *testing.T) {
		spec := &State{
			Logic: Logic{Action: "check_auth"},
		}
		state := &UserState{UserID: 2}
		next := engine.evaluateSystemState(context.Background(), spec, state)
		assert.Equal(t, "FORCED_STATE", next)
	})

	t.Run("action updates context and uses transitions", func(t *testing.T) {
		spec := &State{
			Logic: Logic{Action: "check_auth"},
			Transitions: []Transition{
				{Condition: "is_auth == true", NextState: "AUTH_SUCCESS"},
				{Condition: "is_auth == false", NextState: "AUTH_FAIL"},
			},
		}
		state := &UserState{UserID: 1, Context: make(map[string]any)}
		next := engine.evaluateSystemState(context.Background(), spec, state)
		assert.Equal(t, "AUTH_SUCCESS", next)
		assert.Equal(t, true, state.Context["is_auth"])
	})
}

func TestEngine_EdgeCases(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := NewMemoryStateRepository()
	parser := NewFlowParser("non_existent", logger) // will fail to find flows
	engine := NewEngine(parser, repo, logger, nil, nil)

	ctx := context.Background()
	userID := int64(999)

	t.Run("InitState does not validate flow existence", func(t *testing.T) {
		err := engine.InitState(ctx, userID, "ghost.yaml", "START", nil)
		assert.NoError(t, err)
	})

	t.Run("Process without InitState", func(t *testing.T) {
		render, err := engine.Process(ctx, userID, "click")
		assert.Error(t, err)
		assert.Nil(t, render)
	})

	t.Run("GetCurrentRender without InitState", func(t *testing.T) {
		render, err := engine.GetCurrentRender(ctx, userID)
		assert.Error(t, err)
		assert.Nil(t, render)
	})
}

func TestEngine_SpecialInputs(t *testing.T) {
	e := &Engine{log: slog.Default()}
	state := &UserState{}

	e.handleSpecialInputs(state, InputSetRu, 1)
	assert.Equal(t, LangRu, state.Language)

	e.handleSpecialInputs(state, InputSetEn, 1)
	assert.Equal(t, LangEn, state.Language)
}

func TestEngine_Process_WithActionError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := NewMemoryStateRepository()
	registry := NewLogicRegistry()
	parser := NewFlowParser("../../docs/specs/flows", logger)
	engine := NewEngine(parser, repo, logger, registry, nil)
	engine.AddAlias("MAIN_MENU", "main_menu.yaml/MAIN_MENU")

	// Register action that returns error
	registry.Register("fail_action", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", nil, fmt.Errorf("action failed")
	})

	ctx := context.Background()
	userID := int64(3001)

	// Init state
	err := engine.InitState(ctx, userID, "main_menu.yaml", "MAIN_MENU", nil)
	require.NoError(t, err)

	// Process with action error - should return error
	render, err := engine.Process(ctx, userID, "fail")
	assert.Error(t, err)
	assert.Nil(t, render)
}

func TestEngine_EvaluateSingleCondition_EdgeCases(t *testing.T) {
	e := &Engine{}

	tests := []struct {
		name      string
		condition string
		ctx       map[string]any
		want      bool
	}{
		{"empty condition", "", map[string]any{"key": "val"}, false},
		{"no operator", "key", map[string]any{"key": "val"}, true},
		{"unknown operator", "key ?? val", map[string]any{"key": "val"}, false},
		{"nil context", "key == val", nil, false},
		{"integer comparison", "count == 5", map[string]any{"count": 5}, true},
		{"boolean false", "flag == false", map[string]any{"flag": false}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, e.evaluateCondition(tt.condition, tt.ctx))
		})
	}
}

func TestEngine_GetContextValue_EdgeCases(t *testing.T) {
	e := &Engine{}
	ctx := map[string]any{"key": "value", "nested": map[string]any{"inner": "data"}}

	// Existing key
	val := e.getContextValue(ctx, "key")
	assert.Equal(t, "value", val)

	// Nested key
	val = e.getContextValue(ctx, "nested.inner")
	assert.Equal(t, "data", val)

	// Non-existing key
	val = e.getContextValue(ctx, "missing")
	assert.Nil(t, val)
}

func TestEngine_GetButtonLabel(t *testing.T) {
	e := &Engine{log: slog.Default()}
	state := &UserState{Language: LangRu}
	button := Button{
		ID:    "test_btn",
		Label: map[string]any{LangRu: "Тест", LangEn: "Test"},
	}

	label := e.getButtonLabel(button, state)
	assert.Equal(t, "Тест", label)

	// Missing language fallback
	state.Language = "fr"
	label = e.getButtonLabel(button, state)
	assert.Equal(t, "Test", label) // Falls back to English
}

func TestEngine_FindNextState(t *testing.T) {
	e := &Engine{}
	spec := State{
		Interface: Interface{
			Buttons: []Button{
				{ID: "btn1", NextState: "STATE1"},
				{ID: "btn2", NextState: "STATE2", Action: "action2"},
			},
		},
	}

	st, act, matched := e.findNextState(spec, "btn1", nil)
	assert.Equal(t, "STATE1", st)
	assert.Equal(t, "", act)
	assert.True(t, matched)

	st, act, matched = e.findNextState(spec, "btn2", nil)
	assert.Equal(t, "STATE2", st)
	assert.Equal(t, "action2", act)
	assert.True(t, matched)

	st, act, matched = e.findNextState(spec, "unknown", nil)
	assert.Equal(t, "", st)
	assert.Equal(t, "", act)
	assert.False(t, matched)
}

func TestEngine_MoreEdgeCases(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := NewMemoryStateRepository()
	parser := NewFlowParser("../../docs/specs/flows", logger)
	registry := NewLogicRegistry()
	engine := NewEngine(parser, repo, logger, registry, nil)
	engine.AddAlias("MAIN_MENU", "main_menu.yaml/MAIN_MENU")

	ctx := context.Background()
	userID := int64(1001)

	t.Run("transitionTo non-existent state", func(t *testing.T) {
		state := &UserState{UserID: userID, CurrentFlow: "registration.yaml"}
		err := engine.transitionTo(ctx, state, "NON_EXISTENT")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("evaluateSystemState with missing action", func(t *testing.T) {
		spec := &State{Logic: Logic{Action: "missing_action"}}
		state := &UserState{}
		// Should just log and return ""
		next := engine.evaluateSystemState(context.Background(), spec, state)
		assert.Equal(t, "", next)
	})

	t.Run("Process with action error", func(t *testing.T) {
		registry.Register("fail_action", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
			return "", nil, fmt.Errorf("action failed")
		})

		// Setup state to a state that has a button with this action
		// We'll use a mocked flow or just use evaluateSystemState directly if easier,
		// but Process uses transitions.

		// Actually testing Process error path is better.
		// We need a flow that has an action.
	})

	t.Run("replaceVariables with missing key", func(t *testing.T) {
		text := "Hello {{name}}"
		state := &UserState{Context: map[string]any{}}
		result := engine.replaceVariables(text, state)
		assert.Equal(t, "Hello {}", result) // Unknown tags are removed
	})

	t.Run("Process success with real flow", func(t *testing.T) {
		userID := int64(2001)
		registry.Register("input:set_ru", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
			return "", map[string]any{"language": "ru"}, nil
		})
		registry.Register("is_user_registered", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
			return "", map[string]any{"registered": true}, nil
		})
		_ = engine.InitState(ctx, userID, "registration.yaml", "SELECT_LANGUAGE", nil)

		render, err := engine.Process(ctx, userID, "set_ru")
		assert.NoError(t, err)
		assert.NotNil(t, render)

		state, _ := repo.GetState(ctx, userID)
		assert.Equal(t, "MAIN_MENU", state.CurrentState)
		assert.Equal(t, "ru", state.Context["language"])
	})

	t.Run("evaluateSystemState with action", func(t *testing.T) {
		spec := &State{
			Logic: Logic{
				Action: "is_registered",
			},
			Transitions: []Transition{
				{Condition: "registered == true", NextState: "NEXT_OK"},
				{Condition: "registered == false", NextState: "NEXT_FAIL"},
			},
		}

		state := &UserState{Context: map[string]any{}}

		// Registered = true
		registry.Register("is_registered", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
			return "", map[string]any{"registered": true}, nil
		})

		next := engine.evaluateSystemState(context.Background(), spec, state)
		assert.Equal(t, "NEXT_OK", next)
		assert.True(t, state.Context["registered"].(bool))

		// Registered = false
		registry.Register("is_registered", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
			return "", map[string]any{"registered": false}, nil
		})
		next = engine.evaluateSystemState(context.Background(), spec, state)
		assert.Equal(t, "NEXT_FAIL", next)
	})
}

func TestEngine_TransitionTo_WithFlowLookup(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := NewMemoryStateRepository()
	parser := NewFlowParser("../../docs/specs/flows", logger)
	registry := NewLogicRegistry()
	engine := NewEngine(parser, repo, logger, registry, nil)

	ctx := context.Background()
	userID := int64(5001)

	// Init to registration flow
	err := engine.InitState(ctx, userID, "registration.yaml", "SELECT_LANGUAGE", nil)
	require.NoError(t, err)

	state, _ := repo.GetState(ctx, userID)

	// Transition to START which should resolve to main_menu.yaml/MAIN_MENU via alias
	engine.AddAlias("START", "main_menu.yaml/MAIN_MENU")
	err = engine.transitionTo(ctx, state, "START")
	assert.NoError(t, err)
	assert.Equal(t, "main_menu.yaml", state.CurrentFlow)
	assert.Equal(t, "MAIN_MENU", state.CurrentState)
}

func TestEngine_RenderState_WithImage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := NewMemoryStateRepository()
	parser := NewFlowParser("../../docs/specs/flows", logger)
	engine := NewEngine(parser, repo, logger, nil, nil)

	state := &UserState{
		Language: "ru",
		Context:  map[string]any{},
	}

	// Create a state with image
	flowState := &State{
		Interface: Interface{
			Text:  map[string]string{"ru": "Test with image"},
			Image: "/nonexistent.png", // Will fail to open but tests the path
			Buttons: []Button{
				{ID: "btn1", Label: map[string]any{"ru": "Кнопка"}},
			},
		},
	}

	render := engine.renderState(context.Background(), state, flowState)
	assert.NotNil(t, render)
	assert.Contains(t, render.Text, "Test with image")
}

func TestEngine_EvaluateSingleCondition_QuotedValues(t *testing.T) {
	e := &Engine{}

	tests := []struct {
		name      string
		condition string
		ctx       map[string]any
		want      bool
	}{
		{"single quoted", "status == 'active'", map[string]any{"status": "active"}, true},
		{"double quoted", `status == "active"`, map[string]any{"status": "active"}, true},
		{"single quoted mismatch", "status == 'inactive'", map[string]any{"status": "active"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, e.evaluateCondition(tt.condition, tt.ctx))
		})
	}
}
