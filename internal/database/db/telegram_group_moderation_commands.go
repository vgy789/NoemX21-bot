package db

import "context"

const getTelegramGroupModerationCommandsEnabledByChatID = `
SELECT moderation_commands_enabled
FROM telegram_groups
WHERE chat_id = $1
`

const updateTelegramGroupModerationCommandsEnabled = `
UPDATE telegram_groups
SET moderation_commands_enabled = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
`

type UpdateTelegramGroupModerationCommandsEnabledParams struct {
	ChatID                    int64 `json:"chat_id"`
	ModerationCommandsEnabled bool  `json:"moderation_commands_enabled"`
}

func (q *Queries) GetTelegramGroupModerationCommandsEnabledByChatID(ctx context.Context, chatID int64) (bool, error) {
	row := q.db.QueryRow(ctx, getTelegramGroupModerationCommandsEnabledByChatID, chatID)
	var enabled bool
	err := row.Scan(&enabled)
	return enabled, err
}

func (q *Queries) UpdateTelegramGroupModerationCommandsEnabled(ctx context.Context, arg UpdateTelegramGroupModerationCommandsEnabledParams) (int64, error) {
	res, err := q.db.Exec(ctx, updateTelegramGroupModerationCommandsEnabled, arg.ChatID, arg.ModerationCommandsEnabled)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected(), nil
}
