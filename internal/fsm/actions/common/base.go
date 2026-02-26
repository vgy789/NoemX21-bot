package common

import (
	"context"

	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// RegisterBase registers basic actions.
func RegisterBase(registry *fsm.LogicRegistry) {
	registry.Register("none", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", nil, nil
	})

	registry.Register("not_reset_user_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", nil, nil
	})

	registry.Register("set_variables", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", payload, nil
	})
}
