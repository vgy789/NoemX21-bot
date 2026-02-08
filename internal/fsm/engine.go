package fsm

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// Engine handles the core FSM logic.
type Engine struct {
	parser    *FlowParser
	repo      StateRepository
	log       *slog.Logger
	registry  *LogicRegistry
	sanitizer VariableSanitizer
	aliases   map[string]StateAddress
}

// StateAddress represents a specific state in a specific flow.
type StateAddress struct {
	Flow  string
	State string
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
		aliases:   make(map[string]StateAddress),
	}
}

// AddAlias adds a global state alias.
func (e *Engine) AddAlias(alias, target string) {
	parts := strings.Split(target, "/")
	if len(parts) != 2 {
		e.log.Error("invalid alias target, expected flow.yaml/STATE", "alias", alias, "target", target)
		return
	}
	e.aliases[alias] = StateAddress{Flow: parts[0], State: parts[1]}
}

// RenderObject represents the data to be sent to the user.
type RenderObject struct {
	Text    string
	Image   string
	Buttons [][]ButtonRender
}

type ButtonRender struct {
	Text string
	Data string
}

// Registry returns the physics engine's logic registry.
func (e *Engine) Registry() *LogicRegistry {
	return e.registry
}

// Repo returns the state repository.
func (e *Engine) Repo() StateRepository {
	return e.repo
}

// Parser returns the flow parser.
func (e *Engine) Parser() *FlowParser {
	return e.parser
}

// Sanitizer returns the variable sanitizer.
func (e *Engine) Sanitizer() func(string) string {
	return e.sanitizer
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
		Language:     LangRu,
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
	input = strings.ToLower(strings.TrimSpace(input))
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

	// If it's an input state and no button was pressed, validate the raw input
	if currentStateSpec.Type == StateTypeInput && nextStateRaw == "" {
		if currentStateSpec.Validation.Regex != "" {
			matched, err := regexp.MatchString(currentStateSpec.Validation.Regex, input)
			if err != nil {
				e.log.Error("regex compilation failed", "regex", currentStateSpec.Validation.Regex, "error", err)
			} else if !matched {
				e.log.Warn("input validation failed", "user_id", userID, "input", input, "regex", currentStateSpec.Validation.Regex)
				// Save last input as invalid and stay in same state
				state.Context["last_input"] = input
				state.Context["last_input_invalid"] = true
				// We don't set error_reason here anymore, allowing the YAML's error_invalid text to be the source of truth
				if err := e.repo.SetState(ctx, state); err != nil {
					return nil, err
				}
				return e.GetCurrentRender(ctx, userID)
			}
		}

		// Validation passed or not required
		state.Context["last_input"] = input
		state.Context["last_input_invalid"] = false

		// For input states, we look for a transition with trigger "on_valid_input"
		for _, t := range currentStateSpec.Transitions {
			if t.Trigger == "on_valid_input" {
				nextStateRaw = t.NextState
				break
			}
		}
	}

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

	// Resolve alias
	if resolved, ok := e.aliases[target]; ok {
		e.log.Debug("resolving state alias", "alias", target, "flow", resolved.Flow, "state", resolved.State)
		targetFlow = resolved.Flow
		targetState = resolved.State
	} else if strings.Contains(target, "/") {
		e.log.Error("hardcoded cross-flow navigation forbidden", "target", target, "user_id", state.UserID)
		return fmt.Errorf("hardcoded cross-flow navigation (with '/') is forbidden, use aliases: %s", target)
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
		next := e.evaluateSystemState(ctx, &spec, state)
		if next != "" {
			return e.transitionTo(ctx, state, next)
		}
	}

	return nil
}

// evaluateSystemState determines the next state via registry actions and transitions.
func (e *Engine) evaluateSystemState(ctx context.Context, spec *State, state *UserState) string {
	actionName := spec.Logic.Action

	// var results map[string]interface{}

	if actionName != "" {
		if action, ok := e.registry.Get(actionName); ok {
			e.log.Debug("executing system action", "action", actionName)
			// Prepare payload
			payload := make(map[string]interface{})
			if spec.Logic.Payload != nil {
				for k, v := range spec.Logic.Payload {
					switch val := v.(type) {
					case string:
						payload[k] = e.replaceVariables(val, state)
					case map[string]interface{}:
						// Support localization within payload: if map has "ru"/"en" keys, pick the right one
						if localized, ok := val[state.Language].(string); ok {
							payload[k] = e.replaceVariables(localized, state)
						} else if fallback, ok := val[LangEn].(string); ok {
							payload[k] = e.replaceVariables(fallback, state)
						} else {
							payload[k] = v
						}
					case []interface{}:
						newList := make([]interface{}, len(val))
						for i, item := range val {
							if s, ok := item.(string); ok {
								newList[i] = e.replaceVariables(s, state)
							} else {
								newList[i] = item
							}
						}
						payload[k] = newList
					default:
						payload[k] = v
					}
				}
			}

			// Inject implicit context
			payload["_last_input"] = state.Context["last_input"]

			next, updates, err := action(ctx, state.UserID, payload)
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
	// Support AND logic
	subConditions := strings.Split(condition, "&&")
	for _, sub := range subConditions {
		if !e.evaluateSingleCondition(strings.TrimSpace(sub), ctx) {
			return false
		}
	}
	return true
}

func (e *Engine) evaluateSingleCondition(condition string, ctx map[string]interface{}) bool {
	var operator string
	if strings.Contains(condition, "==") {
		operator = "=="
	} else if strings.Contains(condition, "!=") {
		operator = "!="
	} else {
		return false
	}

	parts := strings.Split(condition, operator)
	if len(parts) != 2 {
		return false
	}

	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])
	val = strings.Trim(val, "'\"") // Trim quotes

	// Get context value with support for dot notation
	ctxVal := e.getContextValue(ctx, key)
	ctxString := fmt.Sprintf("%v", ctxVal)

	if operator == "==" {
		return ctxString == val
	} else {
		return ctxString != val
	}
}

func (e *Engine) getContextValue(ctx map[string]interface{}, key string) interface{} {
	parts := strings.Split(key, ".")
	var current interface{} = ctx

	for _, part := range parts {
		if m, ok := current.(map[string]interface{}); ok {
			if val, exists := m[part]; exists {
				current = val
			} else {
				return nil
			}
		} else {
			return nil
		}
	}
	return current
}

func (e *Engine) updateStateContext(state *UserState, updates map[string]interface{}) {
	if state.Context == nil {
		state.Context = make(map[string]interface{})
	}
	if lang, ok := updates["language"].(string); ok {
		state.Language = lang
	}
	for k, v := range updates {
		state.Context[k] = v
	}
}

func (e *Engine) renderState(userState *UserState, flowState *State) *RenderObject {
	// Text
	text := "Internal state error"

	// Check if we should use error text
	isInvalid := false
	if val, ok := userState.Context["last_input_invalid"].(bool); ok {
		isInvalid = val
	}

	// Try to get error text if input was invalid
	var textFound bool
	if isInvalid && flowState.Interface.ErrorInvalid != nil {
		if txt, ok := flowState.Interface.ErrorInvalid[userState.Language]; ok {
			text = txt
			textFound = true
		} else if txt, ok := flowState.Interface.ErrorInvalid[LangEn]; ok {
			text = txt
			textFound = true
		}
	}

	// Fallback to normal text
	if !textFound {
		if txt, ok := flowState.Interface.Text[userState.Language]; ok {
			text = txt
		} else if txt, ok := flowState.Interface.Text[LangEn]; ok {
			text = txt // Fallback
		} else {
			e.log.Error("no text found for state", "flow", userState.CurrentFlow, "state", userState.CurrentState, "lang", userState.Language)
		}
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

	// Image
	imagePath := flowState.Interface.Image
	if imagePath != "" {
		imagePath = e.replaceVariablesOpts(imagePath, userState, false)
	}

	return &RenderObject{
		Text:    text,
		Buttons: buttons,
		Image:   imagePath,
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
	return e.replaceVariablesOpts(text, state, true)
}

func (e *Engine) replaceVariablesOpts(text string, state *UserState, sanitize bool) string {
	replacements := e.getReplacementMap(state)

	// Apply all replacements
	result := text
	e.log.Debug("applying template replacements", "original_text_len", len(text))

	for key, val := range replacements {
		if strings.Contains(result, key) {
			e.log.Info("replacing template variable", "key", key, "val", val)

			var finalVal string
			if sanitize && e.sanitizer != nil {
				finalVal = e.sanitizer(val)
			} else {
				finalVal = val
			}

			result = strings.ReplaceAll(result, key, finalVal)
		}
	}

	// Clean up any remaining {var} tags that weren't replaced to avoid showing raw braces to user
	re := regexp.MustCompile(`\{[a-zA-Z0-9_.]+\}`)
	result = re.ReplaceAllString(result, "")

	return result
}

func (e *Engine) getReplacementMap(state *UserState) map[string]string {
	// Get language-aware defaults
	replacements := GetDefaultVariables(state.Language)

	// Merge with Context (Context overrides defaults)
	for k, v := range state.Context {
		val := fmt.Sprintf("%v", v)
		replacements[fmt.Sprintf("{%s}", k)] = val
		replacements[fmt.Sprintf("$context.%s", k)] = val
		replacements[fmt.Sprintf("$updates.%s", k)] = val
	}
	return replacements
}
