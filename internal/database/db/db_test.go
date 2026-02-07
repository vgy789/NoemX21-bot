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

func (m *MockDBTX) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	a := m.Called(ctx, sql, args)
	return a.Get(0).(pgconn.CommandTag), a.Error(1)
}

func (m *MockDBTX) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	a := m.Called(ctx, sql, args)
	return a.Get(0).(pgx.Rows), a.Error(1)
}

func (m *MockDBTX) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	a := m.Called(ctx, sql, args)
	return a.Get(0).(pgx.Row)
}

type MockRow struct {
	mock.Mock
}

func (m *MockRow) Scan(dest ...interface{}) error {
	return m.Called(dest...).Error(0)
}

func TestQueries_GetStudentByS21Login(t *testing.T) {
	mockDB := new(MockDBTX)
	q := New(mockDB)
	ctx := context.Background()

	mockRow := new(MockRow)
	mockDB.On("QueryRow", ctx, getStudentByS21Login, mock.Anything).Return(mockRow)
	mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	_, err := q.GetStudentByS21Login(ctx, "testuser")
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

func TestQueries_GetStudentProfile(t *testing.T) {
	mockDB := new(MockDBTX)
	q := New(mockDB)
	ctx := context.Background()

	mockRow := new(MockRow)
	mockDB.On("QueryRow", ctx, getStudentProfile, mock.Anything).Return(mockRow)
	mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	_, err := q.GetStudentProfile(ctx, "testuser")
	assert.NoError(t, err)
}

func TestQueries_UpsertPlatformCredentials(t *testing.T) {
	mockDB := new(MockDBTX)
	q := New(mockDB)
	ctx := context.Background()

	mockDB.On("Exec", ctx, upsertPlatformCredentials, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(pgconn.CommandTag{}, nil)

	err := q.UpsertPlatformCredentials(ctx, UpsertPlatformCredentialsParams{})
	assert.NoError(t, err)
}

func TestQueries_UpsertStudent(t *testing.T) {
	mockDB := new(MockDBTX)
	q := New(mockDB)
	ctx := context.Background()

	mockRow := new(MockRow)
	mockDB.On("QueryRow", ctx, upsertStudent, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockRow)
	mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	_, err := q.UpsertStudent(ctx, UpsertStudentParams{})
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
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetPlatformCredentials(ctx, "test")
	})

	t.Run("GetRocketChatCredentials", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getRocketChatCredentials, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetRocketChatCredentials(ctx, "test")
	})

	t.Run("GetStudentByRocketChatId", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getStudentByRocketChatId, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetStudentByRocketChatId(ctx, "test")
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
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
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

	t.Run("GetUserAccountByStudentId", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getUserAccountByStudentId, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		_, _ = q.GetUserAccountByStudentId(ctx, "student1")
	})

	t.Run("GetCampusByShortName", func(t *testing.T) {
		mockRow := new(MockRow)
		mockDB.On("QueryRow", ctx, getCampusByShortName, mock.Anything).Return(mockRow)
		mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
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
