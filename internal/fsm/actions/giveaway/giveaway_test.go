package giveaway

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

func TestIsEligibleProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		project s21.ParticipantProjectV1DTO
		want    bool
	}{
		{
			name: "accepted regular project",
			project: s21.ParticipantProjectV1DTO{
				ID:          1,
				Title:       "C_05_printf",
				CourseTitle: "C Basic",
			},
			want: true,
		},
		{
			name: "bootcamp in title",
			project: s21.ParticipantProjectV1DTO{
				ID:          2,
				Title:       "Java Bootcamp Day 01",
				CourseTitle: "Java",
			},
			want: false,
		},
		{
			name: "bootcamp in course title",
			project: s21.ParticipantProjectV1DTO{
				ID:          3,
				Title:       "Project",
				CourseTitle: "CPP Bootcamp",
			},
			want: false,
		},
		{
			name: "qa exam pattern",
			project: s21.ParticipantProjectV1DTO{
				ID:          4,
				Title:       "QA_Ex01",
				CourseTitle: "QA",
			},
			want: false,
		},
		{
			name: "bsa exam pattern",
			project: s21.ParticipantProjectV1DTO{
				ID:          5,
				Title:       "BSA_EX-final",
				CourseTitle: "BSA",
			},
			want: false,
		},
		{
			name: "generic exam pattern",
			project: s21.ParticipantProjectV1DTO{
				ID:          6,
				Title:       "Piscine Exam rank 02",
				CourseTitle: "Piscine",
			},
			want: false,
		},
		{
			name: "contains _ex- fragment",
			project: s21.ParticipantProjectV1DTO{
				ID:          7,
				Title:       "Algo_Ex-03",
				CourseTitle: "Algo",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isEligibleProject(tt.project))
		})
	}
}

func TestBuildExportTextSortedByLogin(t *testing.T) {
	t.Parallel()

	input := []db.SapphireGiveawayParticipant{
		{S21Login: "zeta", CountedProjectsCount: 1},
		{S21Login: "alpha", CountedProjectsCount: 5},
		{S21Login: "beta", CountedProjectsCount: 0},
	}

	require.Equal(t, "alpha:5 beta:0 zeta:1", buildExportText(input))
}

func TestIsSapphireCoalitionName(t *testing.T) {
	t.Parallel()

	require.True(t, isSapphireCoalitionName("Sapphire"))
	require.True(t, isSapphireCoalitionName("Sapphires Team"))
	require.True(t, isSapphireCoalitionName("Сапфиры"))
	require.True(t, isSapphireCoalitionName("Трайб сапфиров"))
	require.False(t, isSapphireCoalitionName("Emerald"))
	require.False(t, isSapphireCoalitionName("—"))
}

func TestFormatProgressPage(t *testing.T) {
	t.Parallel()

	rows := []db.SapphireGiveawayParticipant{
		{S21Login: "a", CountedProjectsCount: 1},
		{S21Login: "b", CountedProjectsCount: 2},
		{S21Login: "c", CountedProjectsCount: 3},
	}

	text, page, total, hasPrev, hasNext := formatProgressPage(rows, 1, 2)
	require.Equal(t, 1, page)
	require.Equal(t, 2, total)
	require.False(t, hasPrev)
	require.True(t, hasNext)
	require.Contains(t, text, "1. a")
	require.Contains(t, text, "2. b")
	require.Contains(t, text, "Страница 1/2")

	text, page, total, hasPrev, hasNext = formatProgressPage(rows, 99, 2)
	require.Equal(t, 2, page)
	require.Equal(t, 2, total)
	require.True(t, hasPrev)
	require.False(t, hasNext)
	require.Contains(t, text, "3. c")
	require.Contains(t, text, "Страница 2/2")
}
