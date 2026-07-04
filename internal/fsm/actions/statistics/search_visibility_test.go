package statistics

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"go.uber.org/mock/gomock"
)

func TestPeerSearchRejectsRequesterWithoutEffectiveTelegramVisibility(t *testing.T) {
	ctrl := gomock.NewController(t)
	queries := mock.NewMockQuerier(ctrl)
	registry := fsm.NewLogicRegistry()
	Register(registry, &config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), queries, nil, nil, nil, nil)

	action, ok := registry.Get("get_peer_data_with_permissions")
	require.True(t, ok)
	queries.EXPECT().IsTelegramAccountEffectivelySearchable(gomock.Any(), "42").Return(false, nil)

	_, result, err := action(context.Background(), 42, map[string]any{"login": "student", "language": "ru"})
	require.NoError(t, err)
	require.Equal(t, false, result["search_allowed"])
	require.Equal(t, false, result["peer_found"])
}
