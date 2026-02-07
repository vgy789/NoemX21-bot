package common

import (
	"context"

	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// RegisterBase registers basic actions.
func RegisterBase(registry *fsm.LogicRegistry) {
	registry.Register("not_reset_user_context", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		return "", nil, nil
	})

	registry.Register("set_variables", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		return "", payload, nil
	})
}
