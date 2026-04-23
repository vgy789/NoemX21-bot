package db

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockDBTX struct {
	mock.Mock
}

func (m *MockDBTX) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	a := m.Called(ctx, sql, args)
	return a.Get(0).(pgconn.CommandTag), a.Error(1)
}

func (m *MockDBTX) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	a := m.Called(ctx, sql, args)
	return a.Get(0).(pgx.Rows), a.Error(1)
}

func (m *MockDBTX) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	a := m.Called(ctx, sql, args)
	return a.Get(0).(pgx.Row)
}

type MockRow struct {
	mock.Mock
}

func (m *MockRow) Scan(dest ...any) error {
	return m.Called(dest...).Error(0)
}

func TestQueries_GetRegisteredUserByS21Login(t *testing.T) {
	mockDB := new(MockDBTX)
	q := New(mockDB)
	ctx := context.Background()

	mockRow := new(MockRow)
	mockDB.On("QueryRow", ctx, getRegisteredUserByS21Login, mock.Anything).Return(mockRow)
	// RegisteredUser has 7 fields
	scans := make([]any, 7)
	for i := range scans {
		scans[i] = mock.Anything
	}
	mockRow.On("Scan", scans...).Return(nil)

	_, err := q.GetRegisteredUserByS21Login(ctx, "testuser")
	assert.NoError(t, err)
}

func TestQueries_WithTx(t *testing.T) {
	q := &Queries{}
	tx := &mockTx{} // We need a mockTx
	q2 := q.WithTx(tx)
	assert.Equal(t, tx, q2.db)
}

type mockTx struct {
	pgx.Tx
}

func TestQueries_CreateUserAccount(t *testing.T) {
	mockDB := new(MockDBTX)
	q := New(mockDB)
	ctx := context.Background()

	mockRow := new(MockRow)
	mockDB.On("QueryRow", ctx, createUserAccount, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockRow)
	mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	_, err := q.CreateUserAccount(ctx, CreateUserAccountParams{})
	assert.NoError(t, err)
}

func TestQueries_GetMyProfile(t *testing.T) {
	mockDB := new(MockDBTX)
	q := New(mockDB)
	ctx := context.Background()

	mockRow := new(MockRow)
	mockDB.On("QueryRow", ctx, getMyProfile, mock.Anything).Return(mockRow)
	// GetMyProfileRow has 20 fields
	scans := make([]any, 20)
	for i := range scans {
		scans[i] = mock.Anything
	}
	mockRow.On("Scan", scans...).Return(nil)

	_, err := q.GetMyProfile(ctx, "testuser")
	assert.NoError(t, err)
}

func TestQueries_UpsertPlatformCredentials(t *testing.T) {
	mockDB := new(MockDBTX)
	q := New(mockDB)
	ctx := context.Background()

	mockDB.On("Exec", ctx, upsertPlatformCredentials, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(pgconn.CommandTag{}, nil)

	err := q.UpsertPlatformCredentials(ctx, UpsertPlatformCredentialsParams{})
	assert.NoError(t, err)
}

func TestQueries_UpsertRegisteredUser(t *testing.T) {
	mockDB := new(MockDBTX)
	q := New(mockDB)
	ctx := context.Background()

	mockRow := new(MockRow)
	// UpsertRegisteredUser has 6 arguments
	args := make([]any, 6+2) // ctx, query, then 6 args
	args[0] = ctx
	args[1] = upsertRegisteredUser
	for i := 2; i < len(args); i++ {
		args[i] = mock.Anything
	}
	mockDB.On("QueryRow", args...).Return(mockRow)

	// Result scan (same as RegisteredUser - 7 fields)
	scans := make([]any, 7)
	for i := range scans {
		scans[i] = mock.Anything
	}
	mockRow.On("Scan", scans...).Return(nil)

	_, err := q.UpsertRegisteredUser(ctx, UpsertRegisteredUserParams{})
	assert.NoError(t, err)
}

func TestQueries_UpsertUserBotSettings(t *testing.T) {
	mockDB := new(MockDBTX)
	q := New(mockDB)
	ctx := context.Background()

	mockRow := new(MockRow)
	mockDB.On("QueryRow", ctx, upsertUserBotSettings, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockRow)
	mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	_, err := q.UpsertUserBotSettings(ctx, UpsertUserBotSettingsParams{})
	assert.NoError(t, err)
}

func TestQueries_Remaining(t *testing.T) {
	mockDB := new(MockDBTX)
	q := New(mockDB)
	ctx := context.Background()

	t.Run("GetPlatformCredentials", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getPlatformCredentials, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetPlatformCredentials(ctx, "test")
	})

	t.Run("GetRocketChatCredentials", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getRocketChatCredentials, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetRocketChatCredentials(ctx, "test")
	})

	t.Run("GetRegisteredUserByRocketChatId", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getRegisteredUserByRocketChatId, mock.Anything).Return(mockRow)
		// 7 fields
		scans := make([]any, 7)
		for i := range scans {
			scans[i] = mock.Anything
		}
		mockRow.On("Scan", scans...).Return(nil)
		_, _ = q.GetRegisteredUserByRocketChatId(ctx, "test")
	})

	t.Run("GetUserBotSettings", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getUserBotSettings, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetUserBotSettings(ctx, 1)
	})

	t.Run("GetUserAccountByExternalId", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getUserAccountByExternalId, mock.Anything, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetUserAccountByExternalId(ctx, GetUserAccountByExternalIdParams{})
	})

	t.Run("GetPeerProfile", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getPeerProfile, mock.Anything).Return(mockRow)
		// GetPeerProfileRow has 18 fields
		scans := make([]any, 18)
		for i := range scans {
			scans[i] = mock.Anything
		}
		mockRow.On("Scan", scans...).Return(nil)
		_, _ = q.GetPeerProfile(ctx, "login")
	})

	t.Run("GetActiveApiKey", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getActiveApiKey, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetActiveApiKey(ctx, 1)
	})

	t.Run("GetApiKeyByHash", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getApiKeyByHash, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetApiKeyByHash(ctx, "hash")
	})

	t.Run("RevokeOldApiKeys", func(t *testing.T) {
		mockDB.On("Exec", ctx, revokeOldApiKeys, mock.Anything).Return(pgconn.CommandTag{}, nil)
		_ = q.RevokeOldApiKeys(ctx, 1)
	})

	t.Run("GetUserAccountByS21Login", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getUserAccountByS21Login, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetUserAccountByS21Login(ctx, "student1")
	})

	t.Run("GetCampusByShortName", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getCampusByShortName, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetCampusByShortName(ctx, "moscow")
	})

	t.Run("GetFSMState", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getFSMState, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetFSMState(ctx, 1)
	})

	t.Run("UpsertFSMState", func(t *testing.T) {
		mockDB.On("Exec", ctx, upsertFSMState, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(pgconn.CommandTag{}, nil)
		_ = q.UpsertFSMState(ctx, UpsertFSMStateParams{})
	})

	t.Run("DeleteExpiredAuthVerificationCodes", func(t *testing.T) {
		mockDB.On("Exec", ctx, deleteExpiredAuthVerificationCodes, mock.Anything).Return(pgconn.CommandTag{}, nil)
		_ = q.DeleteExpiredAuthVerificationCodes(ctx)
	})

	t.Run("CreateApiKey", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, createApiKey, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.CreateApiKey(ctx, CreateApiKeyParams{})
	})

	t.Run("CreateAuthVerificationCode", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, createAuthVerificationCode, mock.Anything, mock.Anything, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.CreateAuthVerificationCode(ctx, CreateAuthVerificationCodeParams{})
	})

	t.Run("GetLastAuthVerificationCode", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getLastAuthVerificationCode, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetLastAuthVerificationCode(ctx, pgtype.Text{})
	})

	t.Run("GetValidAuthVerificationCode", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getValidAuthVerificationCode, mock.Anything, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetValidAuthVerificationCode(ctx, GetValidAuthVerificationCodeParams{})
	})

	t.Run("DeactivateClubsByCampus", func(t *testing.T) {
		mockDB.On("Exec", ctx, deactivateClubsByCampus, mock.Anything).Return(pgconn.CommandTag{}, nil)
		_ = q.DeactivateClubsByCampus(ctx, pgtype.UUID{})
	})
}

func TestQueries_UpsertRocketChatCredentials(t *testing.T) {
	mockDB := new(MockDBTX)
	q := New(mockDB)
	ctx := context.Background()

	mockDB.On("Exec", ctx, upsertRocketChatCredentials, mock.Anything).Return(pgconn.CommandTag{}, nil)

	err := q.UpsertRocketChatCredentials(ctx, UpsertRocketChatCredentialsParams{})
	assert.NoError(t, err)
}

func TestQueries_MoreTests(t *testing.T) {
	mockDB := new(MockDBTX)
	q := New(mockDB)
	ctx := context.Background()

	t.Run("UpsertCampus", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, upsertCampus, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.UpsertCampus(ctx, UpsertCampusParams{})
	})

	t.Run("UpsertCoalition", func(t *testing.T) {
		mockDB.On("Exec", ctx, upsertCoalition, mock.Anything, mock.Anything).Return(pgconn.CommandTag{}, nil)
		_ = q.UpsertCoalition(ctx, UpsertCoalitionParams{})
	})

	t.Run("UpsertParticipantStatsCache", func(t *testing.T) {
		mockDB.On("Exec", ctx, upsertParticipantStatsCache, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(pgconn.CommandTag{}, nil)
		_ = q.UpsertParticipantStatsCache(ctx, UpsertParticipantStatsCacheParams{})
	})

	t.Run("UpsertParticipantSkill", func(t *testing.T) {
		mockDB.On("Exec", ctx, upsertParticipantSkill, mock.Anything, mock.Anything, mock.Anything).Return(pgconn.CommandTag{}, nil)
		_ = q.UpsertParticipantSkill(ctx, UpsertParticipantSkillParams{})
	})

	t.Run("DeleteAuthVerificationCode", func(t *testing.T) {
		mockDB.On("Exec", ctx, deleteAuthVerificationCode, mock.Anything, mock.Anything).Return(pgconn.CommandTag{}, nil)
		_ = q.DeleteAuthVerificationCode(ctx, DeleteAuthVerificationCodeParams{})
	})

	t.Run("DeleteAllAuthVerificationCodes", func(t *testing.T) {
		mockDB.On("Exec", ctx, deleteAllAuthVerificationCodes, mock.Anything).Return(pgconn.CommandTag{}, nil)
		_ = q.DeleteAllAuthVerificationCodes(ctx, pgtype.Text{})
	})

	t.Run("GetCampusByID", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getCampusByID, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetCampusByID(ctx, pgtype.UUID{})
	})

	t.Run("DeleteUserAccountByExternalId", func(t *testing.T) {
		mockDB.On("Exec", ctx, deleteUserAccountByExternalId, mock.Anything, mock.Anything).Return(pgconn.CommandTag{}, nil)
		_ = q.DeleteUserAccountByExternalId(ctx, DeleteUserAccountByExternalIdParams{})
	})
}
