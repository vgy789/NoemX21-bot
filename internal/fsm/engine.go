package fsm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// Engine handles the core FSM logic.
type Engine struct {
	parser *FlowParser
	repo   StateRepository
	log    *slog.Logger
}

// NewEngine creates a new FSM engine.
func NewEngine(parser *FlowParser, repo StateRepository, log *slog.Logger) *Engine {
	return &Engine{
		parser: parser,
		repo:   repo,
		log:    log,
	}
}

// RenderObject represents the data to be sent to the user.
type RenderObject struct {
	Text    string
	Buttons [][]ButtonRender
}

type ButtonRender struct {
	Text string
	Data string
}

// InitState initializes or resets the user state to specific flow/state.
func (e *Engine) InitState(ctx context.Context, userID int64, flowName, stateName string) error {
	e.log.Info("initializing state", "user_id", userID, "flow", flowName, "state", stateName)
	newState := &UserState{
		UserID:       userID,
		CurrentFlow:  flowName,
		CurrentState: stateName,
		Language:     LangRu, // Default to RU
		Context:      make(map[string]interface{}),
	}
	return e.repo.SetState(ctx, newState)
}

// GetCurrentRender returns the RenderObject for the user's current state.
func (e *Engine) GetCurrentRender(ctx context.Context, userID int64) (*RenderObject, error) {
	state, err := e.repo.GetState(ctx, userID)
	if err != nil {
		e.log.Error("failed to get state", "user_id", userID, "error", err)
		return nil, err
	}
	if state == nil {
		return nil, fmt.Errorf("user state not found")
	}

	flow, err := e.parser.GetFlow(state.CurrentFlow)
	if err != nil {
		e.log.Error("failed to load flow", "flow", state.CurrentFlow, "error", err)
		return nil, err
	}

	flowState, ok := flow.States[state.CurrentState]
	if !ok {
		e.log.Error("state not found in flow", "state", state.CurrentState, "flow", state.CurrentFlow)
		return nil, fmt.Errorf("state %s not found in flow %s", state.CurrentState, state.CurrentFlow)
	}

	return e.renderState(state, &flowState), nil
}

// Process handles an input (callback) and transitions the state.
func (e *Engine) Process(ctx context.Context, userID int64, input string) (*RenderObject, error) {
	e.log.Debug("processing input", "user_id", userID, "input", input)

	state, err := e.repo.GetState(ctx, userID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, fmt.Errorf("no active state")
	}

	// 1. Handle special inputs (like language selection)
	e.handleSpecialInputs(state, input, userID)

	flow, err := e.parser.GetFlow(state.CurrentFlow)
	if err != nil {
		return nil, err
	}

	currentStateSpec, ok := flow.States[state.CurrentState]
	if !ok {
		return nil, fmt.Errorf("current state %s invalid", state.CurrentState)
	}

	// 2. Find transition based on input (button ID)
	nextStateRaw := e.findNextState(currentStateSpec, input)
	if nextStateRaw == "" {
		e.log.Warn("no transition found for input", "input", input, "state", state.CurrentState)
		return nil, fmt.Errorf("unknown input or no transition for: %s", input)
	}

	// 3. Apply transition and process system states recursively
	if err := e.transitionTo(ctx, state, nextStateRaw); err != nil {
		return nil, err
	}

	// Return Render for the final reached state
	return e.GetCurrentRender(ctx, userID)
}

func (e *Engine) handleSpecialInputs(state *UserState, input string, userID int64) {
	switch input {
	case InputSetRu:
		state.Language = LangRu
		e.log.Info("language set to RU", "user_id", userID)
	case InputSetEn:
		state.Language = LangEn
		e.log.Info("language set to EN", "user_id", userID)
	}
}

func (e *Engine) findNextState(stateSpec State, input string) string {
	for _, btn := range stateSpec.Interface.Buttons {
		if btn.ID == input {
			return btn.NextState
		}
	}
	return ""
}

// transitionTo updates the user state and automatically processes subsequent system states.
func (e *Engine) transitionTo(ctx context.Context, state *UserState, target string) error {
	targetFlow := state.CurrentFlow
	targetState := target

	if strings.Contains(target, "/") {
		parts := strings.Split(target, "/")
		targetFlow = parts[0]
		targetState = parts[1]
	}

	e.log.Info("transitioning", "from", state.CurrentState, "to", targetState, "flow", targetFlow)

	// Update current state
	state.CurrentFlow = targetFlow
	state.CurrentState = targetState

	// Save intermediate state
	if err := e.repo.SetState(ctx, state); err != nil {
		return err
	}

	// Process system state if necessary
	flow, err := e.parser.GetFlow(targetFlow)
	if err != nil {
		return err
	}

	spec, ok := flow.States[targetState]
	if !ok {
		return fmt.Errorf("target state %s not found in flow %s", targetState, targetFlow)
	}

	if spec.Type == StateTypeSystem {
		e.log.Debug("auto-processing system state", "state", targetState)
		next := e.evaluateSystemState(&spec, state)
		if next != "" {
			return e.transitionTo(ctx, state, next)
		}
	}

	return nil
}

// evaluateSystemState determines the next state for a system state based on logic/transitions.
func (e *Engine) evaluateSystemState(spec *State, _ *UserState) string {
	// Mock logic evaluations for now based on registration flow
	if spec.Logic.Check == "is_user_registered" {
		// Mock: user is not registered
		for _, t := range spec.Transitions {
			if t.Condition == "registered == false" {
				return t.NextState
			}
		}
	}

	// Fallback: first transition if no condition matches
	if len(spec.Transitions) > 0 {
		return spec.Transitions[0].NextState
	}

	return ""
}

func (e *Engine) renderState(userState *UserState, flowState *State) *RenderObject {
	// Text
	text := "Template error"
	if txt, ok := flowState.Interface.Text[userState.Language]; ok {
		text = txt
	} else if txt, ok := flowState.Interface.Text[LangEn]; ok {
		text = txt // Fallback
	}

	// Apply dynamic replacements
	text = e.replaceVariables(text, userState)

	// Buttons
	var buttons [][]ButtonRender
	// Simple row layout: 1 button per row for now
	for _, btn := range flowState.Interface.Buttons {
		label := e.getButtonLabel(btn, userState)

		// Apply replacements to labels too
		label = e.replaceVariables(label, userState)

		buttons = append(buttons, []ButtonRender{{
			Text: label,
			Data: btn.ID,
		}})
	}

	return &RenderObject{
		Text:    text,
		Buttons: buttons,
	}
}

func (e *Engine) getButtonLabel(btn Button, userState *UserState) string {
	label := "Label Error"

	// Handle map[string]interface{}
	if labelMap, ok := btn.Label.(map[string]interface{}); ok {
		if val, ok := labelMap[userState.Language]; ok {
			label = val.(string)
		} else if val, ok := labelMap[LangEn]; ok {
			label = val.(string)
		}
	} else if labelStr, ok := btn.Label.(string); ok {
		label = labelStr
	}
	return label
}

// replaceVariables replaces {var} placeholders with values from UserState.Context or defaults
func (e *Engine) replaceVariables(text string, state *UserState) string {
	replacements := e.getReplacementMap(state)

	// Apply all replacements
	result := text
	e.log.Debug("applying template replacements", "original_text_len", len(text))

	for key, val := range replacements {
		if strings.Contains(result, key) {
			e.log.Info("replacing template variable", "key", key, "val", val)
			result = strings.ReplaceAll(result, key, val)
		}
	}

	// Escape special characters that break Telegram Markdown (like underscores in commands)
	return e.escapeMarkdown(result)
}

func (e *Engine) getReplacementMap(state *UserState) map[string]string {
	// Default variables
	replacements := map[string]string{
		VarS21Login:      DefaultS21Login,
		VarLevel:         DefaultLevel,
		VarCoalition:     DefaultCoalition,
		VarLanguageFlag:  DefaultFlagRu,
		VarLanguageFlag2: DefaultFlagRu,
	}

	// If language is EN, change default flag
	if state.Language == LangEn {
		replacements[VarLanguageFlag] = DefaultFlagEn
		replacements[VarLanguageFlag2] = DefaultFlagEn
	}

	// Merge with Context (Context overrides defaults)
	for k, v := range state.Context {
		key := fmt.Sprintf("{%s}", k)
		replacements[key] = fmt.Sprintf("%v", v)
	}
	return replacements
}

// escapeMarkdown escapes underscores that are likely to break Telegram's Markdown V1 parser.
// In V1, single underscores start italics. We escape them if they are part of a word.
func (e *Engine) escapeMarkdown(text string) string {
	// Simple escaping: replace _ with \_
	// But only if it's not already escaped.
	// A more robust way is to use a regex, but a simple replace is often enough for V1.
	return strings.ReplaceAll(text, "_", "\\_")
}
