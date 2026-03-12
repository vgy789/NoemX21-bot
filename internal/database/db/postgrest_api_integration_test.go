//go:build integration

package db_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/testutil/testdb"
)

func mustUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()

	var id pgtype.UUID
	require.NoError(t, id.Scan(value))
	return id
}

func TestPostgRESTExternalAPIObjects(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tdb := testdb.NewPostgres(t)
	q := tdb.DB.Queries

	campusID := mustUUID(t, "11111111-1111-1111-1111-111111111111")

	_, err := q.UpsertCampus(ctx, db.UpsertCampusParams{
		ID:             campusID,
		ShortName:      "msk",
		FullName:       "Moscow",
		Timezone:       pgtype.Text{String: "Europe/Moscow", Valid: true},
		IsActive:       true,
		LeaderName:     pgtype.Text{},
		LeaderFormLink: pgtype.Text{},
	})
	require.NoError(t, err)

	_, err = q.UpsertRegisteredUser(ctx, db.UpsertRegisteredUserParams{
		S21Login:           "student1",
		RocketchatID:       "rc-1",
		Timezone:           "Europe/Moscow",
		AlternativeContact: pgtype.Text{},
		HasCoffeeBan:       pgtype.Bool{Bool: false, Valid: true},
	})
	require.NoError(t, err)

	user1, err := q.CreateUserAccount(ctx, db.CreateUserAccountParams{
		S21Login:     "student1",
		Platform:     db.EnumPlatformTelegram,
		ExternalID:   "1001",
		Username:     pgtype.Text{String: "student1", Valid: true},
		IsSearchable: pgtype.Bool{Bool: true, Valid: true},
		Role:         db.NullEnumUserRole{},
	})
	require.NoError(t, err)

	_, err = q.UpsertRegisteredUser(ctx, db.UpsertRegisteredUserParams{
		S21Login:           "student2",
		RocketchatID:       "rc-2",
		Timezone:           "Europe/Moscow",
		AlternativeContact: pgtype.Text{},
		HasCoffeeBan:       pgtype.Bool{Bool: false, Valid: true},
	})
	require.NoError(t, err)

	user2, err := q.CreateUserAccount(ctx, db.CreateUserAccountParams{
		S21Login:     "student2",
		Platform:     db.EnumPlatformTelegram,
		ExternalID:   "1002",
		Username:     pgtype.Text{String: "student2", Valid: true},
		IsSearchable: pgtype.Bool{Bool: true, Valid: true},
		Role:         db.NullEnumUserRole{},
	})
	require.NoError(t, err)

	_, err = q.UpsertBook(ctx, db.UpsertBookParams{
		ID:          1,
		CampusID:    campusID,
		Title:       "Algorithms",
		Author:      "Knuth",
		Category:    "CS",
		TotalStock:  2,
		Description: pgtype.Text{},
		IsActive:    pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)

	_, err = q.UpsertBook(ctx, db.UpsertBookParams{
		ID:          2,
		CampusID:    campusID,
		Title:       "Databases",
		Author:      "Date",
		Category:    "CS",
		TotalStock:  2,
		Description: pgtype.Text{},
		IsActive:    pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)

	_, err = q.CreateBookLoan(ctx, db.CreateBookLoanParams{
		CampusID: campusID,
		BookID:   1,
		UserID:   user1.ID,
		DueAt:    pgtype.Timestamptz{Time: time.Now().Add(48 * time.Hour), Valid: true},
	})
	require.NoError(t, err)

	_, err = q.CreateBookLoan(ctx, db.CreateBookLoanParams{
		CampusID: campusID,
		BookID:   2,
		UserID:   user2.ID,
		DueAt:    pgtype.Timestamptz{Time: time.Now().Add(48 * time.Hour), Valid: true},
	})
	require.NoError(t, err)

	principal, err := q.EnsurePersonalApiPrincipal(ctx, db.EnsurePersonalApiPrincipalParams{
		DisplayName:    "Personal key for student1",
		TelegramUserID: pgtype.Int8{Int64: 1001, Valid: true},
		UserAccountID:  pgtype.Int8{Int64: user1.ID, Valid: true},
		Scopes:         []string{"self.read"},
	})
	require.NoError(t, err)

	rawKey := "noemx_sk_" + strings.Repeat("a", 64)
	keyHash := sha256.Sum256([]byte(rawKey))

	_, err = q.CreateApiKey(ctx, db.CreateApiKeyParams{
		ApiPrincipalID: principal.ID,
		KeyHash:        hex.EncodeToString(keyHash[:]),
		Prefix:         "noemx_sk_aaaa",
	})
	require.NoError(t, err)

	conn, err := tdb.Pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	_, err = conn.Exec(ctx, "SELECT set_config('app.settings.jwt_secret', $1, false)", "integration-secret")
	require.NoError(t, err)

	var (
		accessToken   string
		tokenType     string
		expiresIn     int32
		principalID   int64
		principalKind string
		scopes        []string
		issuedCampus  pgtype.UUID
	)

	err = conn.QueryRow(ctx, "SELECT access_token, token_type, expires_in, principal_id, principal_kind, scopes, campus_id FROM api_v1.exchange_api_key($1)", rawKey).
		Scan(&accessToken, &tokenType, &expiresIn, &principalID, &principalKind, &scopes, &issuedCampus)
	require.NoError(t, err)
	require.NotEmpty(t, accessToken)
	require.Equal(t, "Bearer", tokenType)
	require.Equal(t, int32(3600), expiresIn)
	require.Equal(t, principal.ID, principalID)
	require.Equal(t, "personal", principalKind)
	require.Equal(t, []string{"self.read"}, scopes)
	require.False(t, issuedCampus.Valid)

	var servicePrincipalID int64
	err = conn.QueryRow(
		ctx,
		`INSERT INTO api_principals (kind, display_name, campus_id, scopes, allow_login_exposure)
		 VALUES ('service', 'Campus registration check', $1, ARRAY['registration.check']::TEXT[], false)
		 RETURNING id`,
		campusID,
	).Scan(&servicePrincipalID)
	require.NoError(t, err)

	serviceRawKey := "noemx_sk_" + strings.Repeat("b", 64)
	serviceKeyHash := sha256.Sum256([]byte(serviceRawKey))

	_, err = conn.Exec(
		ctx,
		`INSERT INTO api_keys (api_principal_id, key_hash, prefix)
		 VALUES ($1, $2, $3)`,
		servicePrincipalID,
		hex.EncodeToString(serviceKeyHash[:]),
		"noemx_sk_bbbb",
	)
	require.NoError(t, err)

	_, err = conn.Exec(ctx, "SET ROLE api_user")
	require.NoError(t, err)
	defer func() {
		_, _ = conn.Exec(context.Background(), "RESET ROLE")
	}()

	claims := fmt.Sprintf(`{"role":"api_user","principal_id":%d,"principal_kind":"service"}`, servicePrincipalID)
	_, err = conn.Exec(ctx, "SELECT set_config('request.jwt.claims', $1, false)", claims)
	require.NoError(t, err)

	var registered bool
	err = conn.QueryRow(ctx, "SELECT registered FROM api_v1.check_registration($1, $2, $3)", "1001", "student1", "telegram").
		Scan(&registered)
	require.NoError(t, err)
	require.True(t, registered)

	claims = fmt.Sprintf(`{"role":"api_user","principal_id":%d,"principal_kind":"personal"}`, principal.ID)
	_, err = conn.Exec(ctx, "SELECT set_config('request.jwt.claims', $1, false)", claims)
	require.NoError(t, err)
	defer func() {
		_, _ = conn.Exec(context.Background(), "RESET request.jwt.claims")
	}()

	rows, err := conn.Query(ctx, "SELECT book_title FROM api_v1.me_book_loans ORDER BY book_title")
	require.NoError(t, err)
	defer rows.Close()

	var titles []string
	for rows.Next() {
		var title string
		require.NoError(t, rows.Scan(&title))
		titles = append(titles, title)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"Algorithms"}, titles)
}
