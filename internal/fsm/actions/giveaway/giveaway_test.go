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
		tt := tt
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
