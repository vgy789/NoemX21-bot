package common

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

func TestPrepareAvailableProjectsForPRR_HidesPaginationCaptionForSinglePage(t *testing.T) {
	reg := fsm.NewLogicRegistry()
	registerReviewActions(reg, nil, nil, nil, nil, slog.Default())

	action, ok := reg.Get("prepare_available_projects_for_prr")
	require.True(t, ok)

	_, updates, err := action(context.Background(), 0, map[string]any{
		"page": 1,
		"available_projects": []any{
			map[string]any{"id": "1", "name": "C Piscine", "type": "project"},
			map[string]any{"id": "2", "name": "Libft", "type": "project"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "", updates["available_projects_page_caption_ru"])
	require.Equal(t, "", updates["available_projects_page_caption_en"])
}

func TestPrepareAvailableProjectsForPRR_ShowsPaginationCaptionForMultiplePages(t *testing.T) {
	reg := fsm.NewLogicRegistry()
	registerReviewActions(reg, nil, nil, nil, nil, slog.Default())

	action, ok := reg.Get("prepare_available_projects_for_prr")
	require.True(t, ok)

	_, updates, err := action(context.Background(), 0, map[string]any{
		"page": 1,
		"available_projects": []any{
			map[string]any{"id": "1", "name": "P1", "type": "project"},
			map[string]any{"id": "2", "name": "P2", "type": "project"},
			map[string]any{"id": "3", "name": "P3", "type": "project"},
			map[string]any{"id": "4", "name": "P4", "type": "project"},
			map[string]any{"id": "5", "name": "P5", "type": "project"},
			map[string]any{"id": "6", "name": "P6", "type": "project"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "1/2", updates["available_projects_page_caption_ru"])
	require.Equal(t, "1/2", updates["available_projects_page_caption_en"])
}
