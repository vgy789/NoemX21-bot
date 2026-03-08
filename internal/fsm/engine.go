package fsm

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// preformattedContextKeys are context variable names whose value is already
// safe Markdown (built with intentional */_ and user content escaped). They
// must not be sanitized on substitution so that *Лидер:* etc. render as bold.
var preformattedContextKeys = map[string]bool{
	"club_card":                             true,
	"clubs_list":                            true,
	"books_list_numbered":                   true,
	"my_books_list_formatted":               true,
	"formatted_book_list_with_icons":        true,
	"loans_list_formatted":                  true,
	"free_slots_list":                       true,
	"my_bookings_list":                      true,
	"my_bookings_formatted":                 true,
	"hot_slots_list":                        true,
	"peer_contact_line":                     true,
	"alternative_contact_line":              true,
	"my_selected_negotiating_contact_block": true,
	"current_project_filters_text":          true,
}

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
	Alert   string
	Buttons [][]ButtonRender
}

type ButtonRender struct {
	Text string
	Data string
	URL  string // If set, this is a URL button instead of callback button
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

func (e *Engine) InitState(ctx context.Context, userID int64, flowName, stateName string, initialContext map[string]any) error {
	return e.InitStateWithLanguage(ctx, userID, flowName, stateName, initialContext, LangRu)
}

func (e *Engine) InitStateWithLanguage(ctx context.Context, userID int64, flowName, stateName string, initialContext map[string]any, language string) error {
	e.log.Info("initializing state", "user_id", userID, "flow", flowName, "state", stateName)
	if initialContext == nil {
		initialContext = make(map[string]any)
	}
	if strings.TrimSpace(language) == "" {
		language = LangRu
	}
	newState := &UserState{
		UserID:       userID,
		CurrentFlow:  flowName,
		CurrentState: stateName,
		Language:     language,
		Context:      initialContext,
	}

	// 1. Process OnEnter hooks for the initial state
	if flow, err := e.parser.GetFlow(flowName); err == nil {
		if spec, ok := flow.States[stateName]; ok {
			e.runHooks(ctx, spec.OnEnter, newState)
		}
	}

	return e.repo.SetState(ctx, newState)
}

// GetCurrentRender returns the RenderObject for the user's current state.
func (e *Engine) runHooks(ctx context.Context, hooks []Logic, state *UserState) {
	for _, hook := range hooks {
		_, _, _ = e.runAction(ctx, hook, state, nil)
	}
}

func (e *Engine) runAction(ctx context.Context, logic Logic, state *UserState, extra map[string]any) (string, map[string]any, error) {
	actionName := logic.Action
	if action, ok := e.registry.Get(actionName); ok {
		// Prepare payload
		payload := make(map[string]any)

		// Inject implicit context
		maps.Copy(payload, state.Context)
		if extra != nil {
			maps.Copy(payload, extra)
		}

		payload["_last_input"] = state.Context["last_input"]
		payload["last_input"] = state.Context["last_input"]

		// Inject language for localization
		payload["language"] = state.Language

		if logic.Payload != nil {
			for k, v := range logic.Payload {
				switch val := v.(type) {
				case string:
					payload[k] = e.replaceVariablesOpts(val, state, false)
				case map[string]any:
					// Support localization within payload: if map has "ru"/"en" keys, pick the right one
					if localized, ok := val[state.Language].(string); ok {
						payload[k] = e.replaceVariablesOpts(localized, state, false)
					} else if fallback, ok := val[LangEn].(string); ok {
						payload[k] = e.replaceVariablesOpts(fallback, state, false)
					} else {
						payload[k] = v
					}

				case []any:
					newList := make([]any, len(val))
					for i, item := range val {
						if s, ok := item.(string); ok {
							newList[i] = e.replaceVariablesOpts(s, state, false)
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

		e.log.Debug("executing action", "action", actionName, "payload", payload)
		next, updates, err := action(ctx, state.UserID, payload)
		if err != nil {
			e.log.Error("action failed", "action", actionName, "error", err)
			return "", nil, err
		}

		// Update context
		e.updateStateContext(state, updates)
		return next, updates, nil
	}

	e.log.Warn("action not found in registry", "action", actionName)
	return "", nil, fmt.Errorf("action not found: %s", actionName)
}

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

	return e.renderState(ctx, state, &flowState), nil
}

// Process handles an input (callback) and transitions the state.
func (e *Engine) Process(ctx context.Context, userID int64, input string) (*RenderObject, error) {
	// Сохраняем оригинальный регистр входа (важно для динамических callback-данных,
	// таких как названия категорий), убираем только пробелы по краям.
	input = strings.TrimSpace(input)
	state, err := e.repo.GetState(ctx, userID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, fmt.Errorf("no active state")
	}

	e.log.Debug("processing input", "user_id", userID, "input", input, "flow", state.CurrentFlow, "state", state.CurrentState)

	// Check for busy state
	if busy, ok := state.Context["is_busy"].(bool); ok && busy {
		e.log.Warn("ignoring input, engine is busy", "user_id", userID)
		return nil, ErrEngineBusy
	}

	// 1. Check if input triggers a global action (e.g. language change)
	if action, ok := e.registry.Get("input:" + input); ok {
		e.log.Info("executing global input action", "input", input)
		_, updates, err := action(ctx, userID, map[string]any{"input": input})
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
	nextStateRaw, buttonAction := e.findNextState(currentStateSpec, input, state)

	// If a button was pressed (we found a next state), record it as last_input
	if nextStateRaw != "" {
		if state.Context == nil {
			state.Context = make(map[string]any)
		}
		state.Context["last_input"] = input

		if buttonAction != "" {
			_, updates, _ := e.runAction(ctx, Logic{Action: buttonAction}, state, map[string]any{"id": input})
			e.updateStateContext(state, updates)
		}
	}

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

		// For input states, we look for a transition with trigger "on_valid_input" or "on_input"
		var inputTransitionAction string
		for _, t := range currentStateSpec.Transitions {
			if t.Trigger == "on_valid_input" || t.Trigger == "on_input" {
				if t.Condition != "" && !e.evaluateCondition(t.Condition, state.Context) {
					continue
				}
				nextStateRaw = t.NextState
				inputTransitionAction = t.Action
				break
			}
		}
		if nextStateRaw != "" && inputTransitionAction != "" {
			evalCtx := make(map[string]any)
			maps.Copy(evalCtx, state.Context)
			evalCtx["id"] = input
			evalCtx["message"] = map[string]any{"text": input}
			actionNext, updates, _ := e.runAction(ctx, Logic{Action: inputTransitionAction}, state, evalCtx)
			if actionNext != "" {
				nextStateRaw = actionNext
			}
			e.updateStateContext(state, updates)
		}
	}

	if nextStateRaw == "" {
		// 2.2 Check for transitions with trigger "button_click" or "message"
		evalCtx := make(map[string]any)
		maps.Copy(evalCtx, state.Context)
		evalCtx["id"] = input
		evalCtx["message"] = map[string]any{"text": input}

		// Try button_click first
		for _, t := range currentStateSpec.Transitions {
			if t.Trigger == "button_click" {
				if t.Condition == "" || e.evaluateCondition(t.Condition, evalCtx) {
					nextStateRaw = t.NextState
					if t.Action != "" {
						actionNext, updates, _ := e.runAction(ctx, Logic{Action: t.Action}, state, evalCtx)
						if actionNext != "" {
							nextStateRaw = actionNext
						}
						e.updateStateContext(state, updates)
					}
					break
				}
			}
		}

		// If no button_click matched, try message trigger
		if nextStateRaw == "" {
			for _, t := range currentStateSpec.Transitions {
				if t.Trigger == "message" {
					if t.Condition == "" || e.evaluateCondition(t.Condition, evalCtx) {
						nextStateRaw = t.NextState
						if t.Action != "" {
							actionNext, updates, _ := e.runAction(ctx, Logic{Action: t.Action}, state, evalCtx)
							if actionNext != "" {
								nextStateRaw = actionNext
							}
							e.updateStateContext(state, updates)
						}
						break
					}
				}
			}
		}

		if nextStateRaw != "" {
			// Record last_input for fallback triggers too
			if state.Context == nil {
				state.Context = make(map[string]any)
			}
			state.Context["last_input"] = input
		}

	}

	if nextStateRaw == "" {
		e.log.Warn("no transition found for input", "input", input, "state", state.CurrentState)
		return nil, fmt.Errorf("unknown input or no transition for: %s", input)
	}

	// 3. Apply variable substitution to the target state name
	nextStateRaw = e.replaceVariablesOpts(nextStateRaw, state, false)

	// 4. Apply transition and process system states recursively
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

func (e *Engine) findNextState(stateSpec State, input string, userState *UserState) (string, string) {
	for _, btn := range stateSpec.Interface.Buttons {
		// Evaluate condition if present
		if btn.Condition != "" && userState != nil {
			if !e.evaluateCondition(btn.Condition, userState.Context) {
				continue // Skip if condition is not met
			}
		}

		// Direct match against static ID
		if btn.ID == input {
			return btn.NextState, btn.Action
		}
		// Try matching after replacing variables in the ID (allows IDs like "{category_1}")
		// This handles dynamic button IDs that contain template variables
		if userState != nil {
			replacedID := e.replaceVariablesOpts(btn.ID, userState, false)
			if replacedID == input {
				return btn.NextState, btn.Action
			}
		}
	}
	return "", ""
}

// transitionTo updates the user state and automatically processes subsequent system states.
func (e *Engine) transitionTo(ctx context.Context, state *UserState, target string) error {
	targetFlow := state.CurrentFlow
	targetState := target

	// 1. Process OnExit hooks for the current state
	if oldFlow, err := e.parser.GetFlow(state.CurrentFlow); err == nil {
		if oldSpec, ok := oldFlow.States[state.CurrentState]; ok {
			e.runHooks(ctx, oldSpec.OnExit, state)
		}
	}

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

	// Process OnEnter hooks for the new state
	flow, err := e.parser.GetFlow(targetFlow)
	if err != nil {
		return err
	}
	spec, ok := flow.States[targetState]
	if !ok {
		return fmt.Errorf("target state %s not found in flow %s", targetState, targetFlow)
	}

	e.runHooks(ctx, spec.OnEnter, state)

	// Save final state after all hooks
	if err := e.repo.SetState(ctx, state); err != nil {
		return err
	}

	// Process system state if necessary
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
	if spec.Logic.Action != "" {
		next, _, _ := e.runAction(ctx, spec.Logic, state, nil)
		if next != "" {
			return next
		}
	}

	// Evaluate transitions based on state context + results
	for _, t := range spec.Transitions {
		if t.Condition == "" {
			return e.replaceVariablesOpts(t.NextState, state, false) // Unconditional transition
		}
		if e.evaluateCondition(t.Condition, state.Context) {
			return e.replaceVariablesOpts(t.NextState, state, false)
		}
	}

	return ""
}

func (e *Engine) evaluateCondition(condition string, ctx map[string]any) bool {
	// Support OR logic (lower precedence)
	orParts := strings.SplitSeq(condition, "||")
	for orPart := range orParts {
		// Support AND logic (higher precedence)
		andParts := strings.Split(orPart, "&&")
		allAndTrue := true
		for _, andPart := range andParts {
			if !e.evaluateSingleCondition(strings.TrimSpace(andPart), ctx) {
				allAndTrue = false
				break
			}
		}
		// If all AND parts in this OR block are true, the whole condition is true
		if allAndTrue {
			return true
		}
	}
	return false
}

func (e *Engine) evaluateSingleCondition(condition string, ctx map[string]any) bool {
	var operator string
	if strings.Contains(condition, "==") {
		operator = "=="
	} else if strings.Contains(condition, "!=") {
		operator = "!="
	} else if strings.Contains(condition, ">=") {
		operator = ">="
	} else if strings.Contains(condition, "<=") {
		operator = "<="
	} else if strings.Contains(condition, ".startswith(") {
		operator = ".startswith("
	} else if strings.HasPrefix(condition, "is_numeric(") && strings.HasSuffix(condition, ")") {
		inner := condition[11 : len(condition)-1]
		val := e.getContextValue(ctx, inner)
		if val == nil {
			return false
		}
		s := fmt.Sprintf("%v", val)
		_, err := strconv.ParseFloat(s, 64)
		return err == nil
	} else if strings.HasPrefix(condition, "not is_numeric(") && strings.HasSuffix(condition, ")") {
		inner := condition[15 : len(condition)-1]
		val := e.getContextValue(ctx, inner)
		if val == nil {
			return true // It's "not numeric" if it's nil
		}
		s := fmt.Sprintf("%v", val)
		_, err := strconv.ParseFloat(s, 64)
		return err != nil
	} else if strings.Contains(condition, ">") {
		operator = ">"
	} else if strings.Contains(condition, "<") {
		operator = "<"
	} else {
		// Variable-only check: e.g. "has_rooms" (check if truthy/exists)
		val := e.getContextValue(ctx, condition)
		if val == nil {
			return false
		}
		if b, ok := val.(bool); ok {
			return b
		}
		s := strings.ToLower(fmt.Sprintf("%v", val))
		return s != "" && s != "false" && s != "0"
	}

	parts := strings.Split(condition, operator)
	if len(parts) != 2 {
		return false
	}

	key := strings.TrimSpace(parts[0])
	valStr := strings.TrimSpace(parts[1])
	valStr = strings.Trim(valStr, "'\"") // Trim quotes

	// Get context value with support for dot notation
	ctxVal := e.getContextValue(ctx, key)

	// Numeric comparison
	if operator == ">" || operator == "<" || operator == ">=" || operator == "<=" {
		v1, err1 := toFloat(ctxVal)
		v2, err2 := strconv.ParseFloat(valStr, 64)
		if err1 == nil && err2 == nil {
			switch operator {
			case ">":
				return v1 > v2
			case "<":
				return v1 < v2
			case ">=":
				return v1 >= v2
			case "<=":
				return v1 <= v2
			}
		}
		return false
	}

	// String comparison
	ctxString := fmt.Sprintf("%v", ctxVal)
	if operator == ".startswith(" {
		// Format: key.startswith('prefix') or key.startswith("prefix")
		startIndex := strings.Index(condition, "(")
		endIndex := strings.LastIndex(condition, ")")
		if startIndex != -1 && endIndex != -1 && endIndex > startIndex {
			prefix := condition[startIndex+1 : endIndex]
			prefix = strings.Trim(prefix, "'\"")
			return strings.HasPrefix(ctxString, prefix)
		}
		return false
	}

	if operator == "==" {
		return ctxString == valStr
	} else {
		return ctxString != valStr
	}
}

func toFloat(v any) (float64, error) {
	if v == nil {
		return 0, fmt.Errorf("nil value")
	}
	switch val := v.(type) {
	case int:
		return float64(val), nil
	case int32:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case float32:
		return float64(val), nil
	case float64:
		return val, nil
	case string:
		return strconv.ParseFloat(val, 64)
	default:
		return 0, fmt.Errorf("unsupported type: %T", v)
	}
}

func (e *Engine) getContextValue(ctx map[string]any, key string) any {
	parts := strings.Split(key, ".")
	var current any = ctx

	for _, part := range parts {
		if m, ok := current.(map[string]any); ok {
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

func (e *Engine) updateStateContext(state *UserState, updates map[string]any) {
	if state.Context == nil {
		state.Context = make(map[string]any)
	}
	if len(updates) > 0 {
		e.log.Debug("updating context", "updates", updates)
	}
	if lang, ok := updates["language"].(string); ok {
		state.Language = lang
	}
	maps.Copy(state.Context, updates)
}

func (e *Engine) renderState(ctx context.Context, userState *UserState, flowState *State) *RenderObject {
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

	// Apply dynamic replacements with sanitizer so values from context/DB (e.g. campus "24_04_NSK")
	// are escaped for Telegram Markdown and not interpreted as italic/bold.
	text = e.replaceVariablesOpts(text, userState, true)

	// Buttons
	var buttons [][]ButtonRender
	rowMap := make(map[int]int) // RowID -> Index in buttons slice (для явных row)

	// Для авто-группировки: до 2 коротких кнопок (label <= 10 символов) в строке
	shortRowIndex := -1
	shortCount := 0

	for _, btn := range flowState.Interface.Buttons {
		// Check condition if present
		if btn.Condition != "" {
			if !e.evaluateCondition(btn.Condition, userState.Context) {
				continue
			}
		}

		// Подстановки без санитайзера, чтобы не экранировать "_" и "*" в названиях
		label := e.replaceVariablesOpts(e.getButtonLabel(btn, userState), userState, false)
		url := ""
		if btn.URL != "" {
			url = e.replaceVariablesOpts(btn.URL, userState, false)
		}

		// Apply replacements to button IDs too (allows dynamic callback data like {category_1})
		buttonID := e.replaceVariablesOpts(btn.ID, userState, false)

		// Skip buttons if both ID (callback data) and URL are empty - they are invalid for inline keyboard.
		// Also skip if buttonID is empty (Telegram requires callback_data or url for inline buttons).
		if strings.TrimSpace(buttonID) == "" && strings.TrimSpace(url) == "" {
			continue
		}

		item := ButtonRender{Text: label, Data: buttonID, URL: url}

		// Явно заданный row в YAML — уважаем как раньше
		if btn.Row > 0 {
			if idx, ok := rowMap[btn.Row]; ok {
				buttons[idx] = append(buttons[idx], item)
				continue
			}
			rowMap[btn.Row] = len(buttons)
			buttons = append(buttons, []ButtonRender{item})
			continue
		}

		// Авто-группировка: если текст короткий (<=12 символов), кладём по 2 кнопки в строку
		if runeLen := len([]rune(label)); runeLen <= 12 {
			if shortRowIndex == -1 || shortCount == 2 {
				// начинаем новую строку для коротких кнопок
				buttons = append(buttons, []ButtonRender{item})
				shortRowIndex = len(buttons) - 1
				shortCount = 1
			} else {
				// дополняем существующую строку второй кнопкой
				buttons[shortRowIndex] = append(buttons[shortRowIndex], item)
				shortCount = 2
			}
			continue
		}

		// Длинные кнопки — по одной в строке
		buttons = append(buttons, []ButtonRender{item})
	}

	// Image
	imagePath := flowState.Interface.Image
	if imagePath != "" {
		imagePath = e.replaceVariablesOpts(imagePath, userState, false)
	}

	// Alert
	alert := ""
	if val, ok := userState.Context["_alert"].(string); ok {
		e.log.Debug("found alert in context", "alert", val)
		alert = e.replaceVariables(val, userState)
		delete(userState.Context, "_alert")
		// Save state to clear alert if it was a persistent repo
		if err := e.repo.SetState(ctx, userState); err != nil {
			e.log.Error("failed to clear alert from repo", "error", err)
		}
	}

	return &RenderObject{
		Text:    text,
		Buttons: buttons,
		Image:   imagePath,
		Alert:   alert,
	}
}

func (e *Engine) getButtonLabel(btn Button, userState *UserState) string {
	label := "Label Error"

	// Handle map[string]interface{}
	if labelMap, ok := btn.Label.(map[string]any); ok {
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
	keys := make([]string, 0, len(replacements))
	for key := range replacements {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		// Replace longer keys first to avoid partial replacements,
		// e.g. "$context.campus" before "$context.campus_id".
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})

	for _, key := range keys {
		val := replacements[key]
		if strings.Contains(result, key) {
			e.log.Info("replacing template variable", "key", key, "val", val)

			var finalVal string
			varName := e.replacementKeyToVarName(key)
			skipSanitize := preformattedContextKeys[varName]
			if sanitize && e.sanitizer != nil && !skipSanitize {
				finalVal = e.sanitizer(val)
			} else {
				finalVal = val
			}

			result = strings.ReplaceAll(result, key, finalVal)
		}
	}

	// Clean up any remaining {var} or $context.var or $updates.var tags that weren't replaced
	re := regexp.MustCompile(`\{[a-zA-Z0-9_.]+\}|\$[a-z]+\.[a-zA-Z0-9_.]+`)
	result = re.ReplaceAllString(result, "")

	return result
}

func (e *Engine) getReplacementMap(state *UserState) map[string]string {
	// Get language-aware defaults
	replacements := GetDefaultVariables(state.Language)

	// Always expose user_id (state.UserID) for template replacements.
	userID := fmt.Sprintf("%d", state.UserID)
	replacements["{user_id}"] = userID
	replacements["$context.user_id"] = userID
	replacements["$updates.user_id"] = userID

	// Merge with Context (Context overrides defaults)
	for k, v := range state.Context {
		val := fmt.Sprintf("%v", v)
		replacements[fmt.Sprintf("{%s}", k)] = val
		replacements[fmt.Sprintf("$context.%s", k)] = val
		replacements[fmt.Sprintf("$updates.%s", k)] = val
	}
	return replacements
}

// replacementKeyToVarName extracts the context variable name from a replacement key
// (e.g. "{club_card}" or "$context.club_card" -> "club_card").
func (e *Engine) replacementKeyToVarName(key string) string {
	if strings.HasPrefix(key, "{") {
		return strings.Trim(key, "{}")
	}
	if idx := strings.LastIndex(key, "."); idx >= 0 {
		return key[idx+1:]
	}
	return key
}
