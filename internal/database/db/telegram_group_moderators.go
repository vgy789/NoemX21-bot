package db

import (
	"context"
)

const listTelegramGroupsManagedByUser = `
SELECT
    g.chat_id,
    g.chat_title,
    g.owner_telegram_user_id,
    g.owner_telegram_username,
    g.is_initialized,
    g.is_active,
    g.created_at,
    g.updated_at,
    g.member_tags_enabled,
    g.member_tag_format,
	    g.defender_enabled,
	    g.defender_remove_blocked,
	    g.defender_ban_duration_sec,
    g.is_forum,
    g.prr_notifications_enabled,
    g.prr_notifications_thread_id,
    g.prr_notifications_thread_label,
    g.prr_withdrawn_behavior,
    g.moderation_commands_enabled,
	    g.team_notifications_enabled,
	    g.team_notifications_thread_id,
	    g.team_notifications_thread_label,
	    g.team_withdrawn_behavior,
	    g.defender_recheck_known_members
FROM telegram_groups g
WHERE g.is_active = true
  AND g.is_initialized = true
  AND (
      g.owner_telegram_user_id = $1
      OR EXISTS (
          SELECT 1
          FROM telegram_group_moderators m
          WHERE m.chat_id = g.chat_id
            AND m.telegram_user_id = $1
            AND m.full_access = true
      )
  )
ORDER BY g.chat_title
`

const listTelegramGroupModerators = `
SELECT id, chat_id, telegram_user_id, can_ban, can_mute, full_access, added_by_account_id, created_at, updated_at
FROM telegram_group_moderators
WHERE chat_id = $1
ORDER BY updated_at DESC, telegram_user_id ASC
`

const upsertTelegramGroupModerator = `
INSERT INTO telegram_group_moderators (
    chat_id, telegram_user_id, can_ban, can_mute, full_access, added_by_account_id
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (chat_id, telegram_user_id) DO UPDATE SET
    can_ban = EXCLUDED.can_ban,
    can_mute = EXCLUDED.can_mute,
    full_access = EXCLUDED.full_access,
    updated_at = CURRENT_TIMESTAMP
RETURNING id, chat_id, telegram_user_id, can_ban, can_mute, full_access, added_by_account_id, created_at, updated_at
`

const updateTelegramGroupModeratorPermissions = `
UPDATE telegram_group_moderators
SET can_ban = $3,
    can_mute = $4,
    full_access = $5,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
  AND telegram_user_id = $2
RETURNING id, chat_id, telegram_user_id, can_ban, can_mute, full_access, added_by_account_id, created_at, updated_at
`

const getTelegramGroupModeratorByChatAndUser = `
SELECT id, chat_id, telegram_user_id, can_ban, can_mute, full_access, added_by_account_id, created_at, updated_at
FROM telegram_group_moderators
WHERE chat_id = $1
  AND telegram_user_id = $2
`

const deleteTelegramGroupModeratorByChatAndUser = `
DELETE FROM telegram_group_moderators
WHERE chat_id = $1
  AND telegram_user_id = $2
`

const existsTelegramGroupModeratorFullAccess = `
SELECT EXISTS (
    SELECT 1
    FROM telegram_group_moderators
    WHERE chat_id = $1
      AND telegram_user_id = $2
      AND full_access = true
)
`

type UpsertTelegramGroupModeratorParams struct {
	ChatID           int64 `json:"chat_id"`
	TelegramUserID   int64 `json:"telegram_user_id"`
	CanBan           bool  `json:"can_ban"`
	CanMute          bool  `json:"can_mute"`
	FullAccess       bool  `json:"full_access"`
	AddedByAccountID int64 `json:"added_by_account_id"`
}

type UpdateTelegramGroupModeratorPermissionsParams struct {
	ChatID         int64 `json:"chat_id"`
	TelegramUserID int64 `json:"telegram_user_id"`
	CanBan         bool  `json:"can_ban"`
	CanMute        bool  `json:"can_mute"`
	FullAccess     bool  `json:"full_access"`
}

func (q *Queries) ListTelegramGroupsManagedByUser(ctx context.Context, telegramUserID int64) ([]TelegramGroup, error) {
	rows, err := q.db.Query(ctx, listTelegramGroupsManagedByUser, telegramUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TelegramGroup, 0)
	for rows.Next() {
		var i TelegramGroup
		if err := rows.Scan(
			&i.ChatID,
			&i.ChatTitle,
			&i.OwnerTelegramUserID,
			&i.OwnerTelegramUsername,
			&i.IsInitialized,
			&i.IsActive,
			&i.CreatedAt,
			&i.UpdatedAt,
			&i.MemberTagsEnabled,
			&i.MemberTagFormat,
			&i.DefenderEnabled,
			&i.DefenderRemoveBlocked,
			&i.DefenderBanDurationSec,
			&i.IsForum,
			&i.PrrNotificationsEnabled,
			&i.PrrNotificationsThreadID,
			&i.PrrNotificationsThreadLabel,
			&i.PrrWithdrawnBehavior,
			&i.ModerationCommandsEnabled,
			&i.TeamNotificationsEnabled,
			&i.TeamNotificationsThreadID,
			&i.TeamNotificationsThreadLabel,
			&i.TeamWithdrawnBehavior,
			&i.DefenderRecheckKnownMembers,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (q *Queries) ListTelegramGroupModerators(ctx context.Context, chatID int64) ([]TelegramGroupModerator, error) {
	rows, err := q.db.Query(ctx, listTelegramGroupModerators, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TelegramGroupModerator, 0)
	for rows.Next() {
		var i TelegramGroupModerator
		if err := rows.Scan(
			&i.ID,
			&i.ChatID,
			&i.TelegramUserID,
			&i.CanBan,
			&i.CanMute,
			&i.FullAccess,
			&i.AddedByAccountID,
			&i.CreatedAt,
			&i.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (q *Queries) UpsertTelegramGroupModerator(ctx context.Context, arg UpsertTelegramGroupModeratorParams) (TelegramGroupModerator, error) {
	row := q.db.QueryRow(ctx, upsertTelegramGroupModerator,
		arg.ChatID,
		arg.TelegramUserID,
		arg.CanBan,
		arg.CanMute,
		arg.FullAccess,
		arg.AddedByAccountID,
	)

	var i TelegramGroupModerator
	err := row.Scan(
		&i.ID,
		&i.ChatID,
		&i.TelegramUserID,
		&i.CanBan,
		&i.CanMute,
		&i.FullAccess,
		&i.AddedByAccountID,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

func (q *Queries) UpdateTelegramGroupModeratorPermissions(ctx context.Context, arg UpdateTelegramGroupModeratorPermissionsParams) (TelegramGroupModerator, error) {
	row := q.db.QueryRow(ctx, updateTelegramGroupModeratorPermissions,
		arg.ChatID,
		arg.TelegramUserID,
		arg.CanBan,
		arg.CanMute,
		arg.FullAccess,
	)

	var i TelegramGroupModerator
	err := row.Scan(
		&i.ID,
		&i.ChatID,
		&i.TelegramUserID,
		&i.CanBan,
		&i.CanMute,
		&i.FullAccess,
		&i.AddedByAccountID,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

func (q *Queries) GetTelegramGroupModeratorByChatAndUser(ctx context.Context, chatID, telegramUserID int64) (TelegramGroupModerator, error) {
	row := q.db.QueryRow(ctx, getTelegramGroupModeratorByChatAndUser, chatID, telegramUserID)
	var i TelegramGroupModerator
	err := row.Scan(
		&i.ID,
		&i.ChatID,
		&i.TelegramUserID,
		&i.CanBan,
		&i.CanMute,
		&i.FullAccess,
		&i.AddedByAccountID,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

func (q *Queries) DeleteTelegramGroupModeratorByChatAndUser(ctx context.Context, chatID, telegramUserID int64) error {
	_, err := q.db.Exec(ctx, deleteTelegramGroupModeratorByChatAndUser, chatID, telegramUserID)
	return err
}

func (q *Queries) ExistsTelegramGroupModeratorFullAccess(ctx context.Context, chatID, telegramUserID int64) (bool, error) {
	row := q.db.QueryRow(ctx, existsTelegramGroupModeratorFullAccess, chatID, telegramUserID)
	var ok bool
	err := row.Scan(&ok)
	return ok, err
}
