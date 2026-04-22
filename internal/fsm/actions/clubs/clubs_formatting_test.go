package clubs

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

func TestFormatClubCard_NormalizesLegacyMarkdownEscapes(t *testing.T) {
	card := formatClubCard(db.GetGlobalClubsRow{
		Name:         "CPP2\\_s21\\_containers",
		Description:  pgtype.Text{String: "Проект про C\\_containers", Valid: true},
		LeaderLogin:  pgtype.Text{String: "peer\\_reviewer", Valid: true},
		CategoryName: "Dev\\_Club",
		CampusName:   "24\\_04\\_NSK",
	})

	require.Contains(t, card, "*CPP2\\_s21\\_containers*")
	require.Contains(t, card, "Проект про C\\_containers")
	require.Contains(t, card, "peer\\_reviewer")
	require.Contains(t, card, "Dev\\_Club")
	require.Contains(t, card, "24\\_04\\_NSK")
	require.NotContains(t, card, "\\\\\\_")
}

func TestClubData_NormalizesLegacyMarkdownEscapesForButtons(t *testing.T) {
	buttonData := clubData(db.GetGlobalClubsRow{
		ID:         7,
		Name:       "CPP2\\_s21\\_containers",
		CampusName: "21 Kazan",
	})

	require.Equal(t, int16(7), buttonData.ID)
	require.Equal(t, "CPP2_s21_containers", buttonData.Name)
	require.Equal(t, "21 Kazan", buttonData.CampusName)
	require.False(t, buttonData.IsLocal)
}

func TestClubButtonCallbackHelpers(t *testing.T) {
	require.Equal(t, "club_5_callback", clubButtonCallback(5))
	require.Equal(t, 5, parseClubButtonSlot("club_5_callback"))
	require.Equal(t, 0, parseClubButtonSlot("21"))
}
