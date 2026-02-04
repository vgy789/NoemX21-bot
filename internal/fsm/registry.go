package fsm

import "context"

// SystemAction is a function that executes business logic.
// It returns the next state name (if transition is determined) or an empty string.
// It can also return data to update the context.
type SystemAction func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error)

// LogicRegistry holds available system actions.
type LogicRegistry struct {
	actions map[string]SystemAction
}

func NewLogicRegistry() *LogicRegistry {
	return &LogicRegistry{
		actions: make(map[string]SystemAction),
	}
}

func (r *LogicRegistry) Register(name string, action SystemAction) {
	r.actions[name] = action
}

func (r *LogicRegistry) Get(name string) (SystemAction, bool) {
	act, ok := r.actions[name]
	return act, ok
}

// VariableSanitizer cleans text before insertion into templates
type VariableSanitizer func(string) string
