package db

import (
	"context"
	"strings"
)

const getTelegramUserAccountByUsername = `
SELECT id, s21_login, platform, external_id, username, is_searchable, role, linked_at
FROM user_accounts
WHERE platform = $1
  AND lower(trim(both '@' from username)) = lower(trim(both '@' from $2))
ORDER BY linked_at DESC
LIMIT 1
`

const getTelegramUserAccountByS21Login = `
SELECT id, s21_login, platform, external_id, username, is_searchable, role, linked_at
FROM user_accounts
WHERE platform = $1
  AND lower(s21_login) = lower($2)
ORDER BY linked_at DESC
LIMIT 1
`

// GetTelegramUserAccountByUsername resolves telegram account row by @username.
func (q *Queries) GetTelegramUserAccountByUsername(ctx context.Context, username string) (UserAccount, error) {
	row := q.db.QueryRow(ctx, getTelegramUserAccountByUsername, EnumPlatformTelegram, strings.TrimSpace(username))
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

// GetTelegramUserAccountByS21Login resolves telegram account row by school login.
func (q *Queries) GetTelegramUserAccountByS21Login(ctx context.Context, s21Login string) (UserAccount, error) {
	row := q.db.QueryRow(ctx, getTelegramUserAccountByS21Login, EnumPlatformTelegram, strings.TrimSpace(s21Login))
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
