package membertagimport

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	input := `[
  {"from_id":"user101","text":"Alpha","isActive":true,"reactions":["ignored"]},
  {"from_id":"user102","text":"Beta","isActive":false},
  {"from_id":"user103","text":"First","isActive":true},
  {"from_id":"user103","text":"Second","isActive":false},
  {"from_id":"user104","text":"NoStatus","isActive":null},
  {"from_id":"bad","text":"Invalid","isActive":true}
]`

	report, err := Parse(strings.NewReader(input))
	require.NoError(t, err)
	assert.Equal(t, 6, report.TotalRows)
	assert.Equal(t, 2, report.AcceptedRows)
	assert.Equal(t, 1, report.SkippedInvalidRows)
	assert.Equal(t, 2, report.SkippedConflictRows)
	assert.Equal(t, 1, report.SkippedConflictIDs)
	assert.Equal(t, 1, report.SkippedNullStatusRows)
	require.Len(t, report.Mappings, 2)
	assert.Equal(t, int64(101), report.Mappings[0].TelegramUserID)
	assert.Equal(t, "alpha", report.Mappings[0].Login)
	assert.True(t, report.Mappings[0].Active)
	assert.Equal(t, "beta", report.Mappings[1].Login)
	assert.False(t, report.Mappings[1].Active)
	for _, issue := range report.Issues {
		assert.NotEmpty(t, issue.SafeHash)
		assert.NotContains(t, issue.SafeHash, "First")
	}
}

func TestParseTreatsNullConflictAsConflictAndCountsNull(t *testing.T) {
	input := `[
  {"from_id":"user201","text":"first","isActive":null},
  {"from_id":"user201","text":"second","isActive":true}
]`
	report, err := Parse(strings.NewReader(input))
	require.NoError(t, err)
	assert.Zero(t, report.AcceptedRows)
	assert.Equal(t, 1, report.SkippedConflictIDs)
	assert.Equal(t, 2, report.SkippedConflictRows)
	assert.Equal(t, 1, report.SkippedNullStatusRows)
}

func TestParseRejectsNonArrayJSON(t *testing.T) {
	_, err := Parse(strings.NewReader(`{"from_id":"user1"}`))
	require.Error(t, err)
}
