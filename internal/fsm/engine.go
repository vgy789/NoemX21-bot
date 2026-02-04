package fsm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// Engine handles the core FSM logic.
type Engine struct {
	parser    *FlowParser
	repo      StateRepository
	log       *slog.Logger
	registry  *LogicRegistry
	sanitizer VariableSanitizer
}

// NewEngine creates a new FSM engine.
func NewEngine(parser *FlowParser, repo StateRepository, log *slog.Logger, registry *LogicRegistry, sanitizer VariableSanitizer) *Engine {
	if sanitizer == nil {
		sanitizer = func(s string) string { return s }
	}
	if registry == nil {
		registry = NewLogicRegistry()
	}
	return &Engine{
		parser:    parser,
		repo:      repo,
		log:       log,
		registry:  registry,
		sanitizer: sanitizer,
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
func (e *Engine) Registry() *LogicRegistry {
	return e.registry
}

func (e *Engine) InitState(ctx context.Context, userID int64, flowName, stateName string, initialContext map[string]interface{}) error {

	e.log.Info("initializing state", "user_id", userID, "flow", flowName, "state", stateName)
	if initialContext == nil {
		initialContext = make(map[string]interface{})
	}
	newState := &UserState{
		UserID:       userID,
		CurrentFlow:  flowName,
		CurrentState: stateName,
		Language:     DefaultLanguage,
		Context:      initialContext,
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
	state, err := e.repo.GetState(ctx, userID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, fmt.Errorf("no active state")
	}

	e.log.Debug("processing input", "user_id", userID, "input", input, "flow", state.CurrentFlow, "state", state.CurrentState)

	// 1. Check if input triggers a global action (e.g. language change)
	if action, ok := e.registry.Get("input:" + input); ok {
		e.log.Info("executing global input action", "input", input)
		_, updates, err := action(ctx, userID, map[string]interface{}{"input": input})
		if err != nil {
			e.log.Error("global action failed", "input", input, "error", err)
		} else if len(updates) > 0 {
			e.updateStateContext(state, updates)
			// Save state after updates
			if err := e.repo.SetState(ctx, state); err != nil {
				return nil, err
			}
		}
	}

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

// evaluateSystemState determines the next state via registry actions and transitions.
func (e *Engine) evaluateSystemState(spec *State, state *UserState) string {
	actionName := spec.Logic.Action
	if actionName == "" {
		actionName = spec.Logic.Check // Fallback to 'check' field
	}

	// var results map[string]interface{}

	if actionName != "" {
		if action, ok := e.registry.Get(actionName); ok {
			e.log.Debug("executing system action", "action", actionName)
			// Prepare payload
			payload := spec.Logic.Payload
			// Inject implicit context
			if payload == nil {
				payload = make(map[string]interface{})
			}
			payload["_last_input"] = state.Context["last_input"]

			next, updates, err := action(context.Background(), state.UserID, payload)
			if err != nil {
				e.log.Error("system action failed", "action", actionName, "error", err)
				// Ideally handle error transition here
			}

			// Update context
			e.updateStateContext(state, updates)
			// results = updates

			// If action returned a forced next state, use it
			if next != "" {
				return next
			}
		} else {
			e.log.Warn("system action not found in registry", "action", actionName)
		}
	}

	// Evaluate transitions based on state context + results
	for _, t := range spec.Transitions {
		if t.Condition == "" {
			return t.NextState // Unconditional transition
		}
		if e.evaluateCondition(t.Condition, state.Context) {
			return t.NextState
		}
	}

	return ""
}

func (e *Engine) evaluateCondition(condition string, ctx map[string]interface{}) bool {
	// Simple evaluator: "key == value"
	// TODO: Use a real expression engine if needed
	parts := strings.Split(condition, "==")
	if len(parts) != 2 {
		return false
	}
	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])
	val = strings.Trim(val, "'\"") // Trim quotes

	ctxVal, ok := ctx[key]
	if !ok {
		return false
	}

	return fmt.Sprintf("%v", ctxVal) == val
}

func (e *Engine) updateStateContext(state *UserState, updates map[string]interface{}) {
	if state.Context == nil {
		state.Context = make(map[string]interface{})
	}
	for k, v := range updates {
		state.Context[k] = v
	}
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
	rowMap := make(map[int]int) // RowID -> Index in buttons slice

	for _, btn := range flowState.Interface.Buttons {
		label := e.getButtonLabel(btn, userState)

		// Apply replacements to labels too
		label = e.replaceVariables(label, userState)

		item := ButtonRender{
			Text: label,
			Data: btn.ID,
		}

		if btn.Row > 0 {
			if idx, ok := rowMap[btn.Row]; ok {
				buttons[idx] = append(buttons[idx], item)
				continue
			}
			rowMap[btn.Row] = len(buttons)
			buttons = append(buttons, []ButtonRender{item})
		} else {
			buttons = append(buttons, []ButtonRender{item})
		}
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
			// Escape values using the configured sanitizer
			escapedVal := e.sanitizer(val)
			result = strings.ReplaceAll(result, key, escapedVal)
		}
	}

	return result
}

func (e *Engine) getReplacementMap(state *UserState) map[string]string {
	// Get language-aware defaults
	replacements := GetDefaultVariables(state.Language)

	// Merge with Context (Context overrides defaults)
	for k, v := range state.Context {
		key := fmt.Sprintf("{%s}", k)
		replacements[key] = fmt.Sprintf("%v", v)
	}
	return replacements
}
