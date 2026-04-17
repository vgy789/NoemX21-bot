package db

import "context"

const listActiveInitializedTelegramGroups = `
SELECT chat_id, chat_title, owner_telegram_user_id, owner_telegram_username, is_initialized, is_active, created_at, updated_at, member_tags_enabled, member_tag_format, defender_enabled, defender_remove_blocked, defender_ban_duration_sec, is_forum, prr_notifications_enabled, prr_notifications_thread_id, prr_notifications_thread_label, prr_withdrawn_behavior
FROM telegram_groups
WHERE is_active = true
  AND is_initialized = true
ORDER BY chat_title
`

// ListActiveInitializedTelegramGroups returns all active initialized groups.
// It is used for startup consistency reconciliation.
func (q *Queries) ListActiveInitializedTelegramGroups(ctx context.Context) ([]TelegramGroup, error) {
	rows, err := q.db.Query(ctx, listActiveInitializedTelegramGroups)
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
