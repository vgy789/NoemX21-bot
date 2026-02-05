package actions

import (
	"context"
	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// RegisterBaseActions registers utility actions that don't depend on specific services.
func RegisterBaseActions(registry *fsm.LogicRegistry, log *slog.Logger) {
	registry.Register("not_reset_user_context", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		return "", nil, nil
	})
}
