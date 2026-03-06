package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

const unlinkUserAccountByExternalId = `
UPDATE user_accounts
SET external_id = $3,
    username = NULL,
    is_searchable = false,
    linked_at = CURRENT_TIMESTAMP
WHERE platform = $1 AND external_id = $2
RETURNING id, s21_login, platform, external_id, username, is_searchable, role, linked_at
`

type UnlinkUserAccountByExternalIdParams struct {
	Platform      EnumPlatform `json:"platform"`
	ExternalID    string       `json:"external_id"`
	NewExternalID string       `json:"new_external_id"`
}

// UnlinkUserAccountByExternalId detaches platform external_id from a user account without deleting history-bound row.
func (q *Queries) UnlinkUserAccountByExternalId(ctx context.Context, arg UnlinkUserAccountByExternalIdParams) (UserAccount, error) {
	row := q.db.QueryRow(ctx, unlinkUserAccountByExternalId, arg.Platform, arg.ExternalID, arg.NewExternalID)
	var i UserAccount
	err := row.Scan(
		&i.ID,
		&i.S21Login,
		&i.Platform,
		&i.ExternalID,
		&i.Username,
		&i.IsSearchable,
		&i.Role,
		&i.LinkedAt,
	)
	return i, err
}

const rebindUserAccountByS21Login = `
UPDATE user_accounts
SET external_id = $3,
    username = $4,
    is_searchable = true,
    linked_at = CURRENT_TIMESTAMP
WHERE s21_login = $1 AND platform = $2
RETURNING id, s21_login, platform, external_id, username, is_searchable, role, linked_at
`

type RebindUserAccountByS21LoginParams struct {
	S21Login   string       `json:"s21_login"`
	Platform   EnumPlatform `json:"platform"`
	ExternalID string       `json:"external_id"`
	Username   pgtype.Text  `json:"username"`
}

// RebindUserAccountByS21Login binds a pre-existing platform account row to a new external_id.
func (q *Queries) RebindUserAccountByS21Login(ctx context.Context, arg RebindUserAccountByS21LoginParams) (UserAccount, error) {
	row := q.db.QueryRow(ctx, rebindUserAccountByS21Login, arg.S21Login, arg.Platform, arg.ExternalID, arg.Username)
	var i UserAccount
	err := row.Scan(
		&i.ID,
		&i.S21Login,
		&i.Platform,
		&i.ExternalID,
		&i.Username,
		&i.IsSearchable,
		&i.Role,
		&i.LinkedAt,
	)
	return i, err
}
