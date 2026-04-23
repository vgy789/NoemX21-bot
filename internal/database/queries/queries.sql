-- queries.sql

-- name: GetRegisteredUserByS21Login :one
SELECT * FROM registered_users WHERE s21_login = $1;

-- name: GetRegisteredUserByRocketChatId :one
SELECT * FROM registered_users WHERE rocketchat_id = $1;

-- name: UpsertRegisteredUser :one
INSERT INTO registered_users (
    s21_login, rocketchat_id,
    timezone, alternative_contact, has_coffee_ban
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (s21_login) DO UPDATE SET
    rocketchat_id = COALESCE(EXCLUDED.rocketchat_id, registered_users.rocketchat_id),
    timezone = COALESCE(EXCLUDED.timezone, registered_users.timezone),
    alternative_contact = COALESCE(EXCLUDED.alternative_contact, registered_users.alternative_contact),
    has_coffee_ban = COALESCE(EXCLUDED.has_coffee_ban, registered_users.has_coffee_ban),
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: GetUserAccountByExternalId :one
SELECT * FROM user_accounts 
WHERE platform = $1 AND external_id = $2;

-- name: UpdateUserAccountSearchableByExternalId :one
UPDATE user_accounts
SET is_searchable = $3
WHERE platform = $1 AND external_id = $2
RETURNING *;

-- name: DeleteUserAccountByExternalId :exec
DELETE FROM user_accounts
WHERE platform = $1 AND external_id = $2;

-- name: GetUserAccountByS21Login :one
SELECT * FROM user_accounts
WHERE s21_login = $1;

-- name: GetUserAccountByID :one
SELECT * FROM user_accounts
WHERE id = $1;

-- name: CreateUserAccount :one
INSERT INTO user_accounts (
    s21_login, platform, external_id, username, is_searchable, role
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: UpsertTelegramGroup :one
INSERT INTO telegram_groups (
    chat_id, chat_title, owner_telegram_user_id, owner_telegram_username, is_initialized, is_active
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (chat_id) DO UPDATE SET
    chat_title = EXCLUDED.chat_title,
    owner_telegram_user_id = EXCLUDED.owner_telegram_user_id,
    owner_telegram_username = EXCLUDED.owner_telegram_username,
    is_initialized = EXCLUDED.is_initialized,
    is_active = EXCLUDED.is_active,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: ListTelegramGroupsByOwner :many
SELECT * FROM telegram_groups
WHERE owner_telegram_user_id = $1
  AND is_active = true
  AND is_initialized = true
ORDER BY chat_title;

-- name: GetTelegramGroupByChatID :one
SELECT * FROM telegram_groups
WHERE chat_id = $1;

-- name: DeactivateTelegramGroup :exec
UPDATE telegram_groups
SET is_active = false,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1;

-- name: DeactivateTelegramGroupIfOwner :execrows
UPDATE telegram_groups
SET is_active = false,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
  AND owner_telegram_user_id = $2;

-- name: UnlinkTelegramGroupOwner :exec
UPDATE telegram_groups
SET owner_telegram_user_id = 0,
    owner_telegram_username = '',
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1;

-- name: UnlinkTelegramGroupOwnerIfOwner :execrows
UPDATE telegram_groups
SET owner_telegram_user_id = 0,
    owner_telegram_username = '',
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
  AND owner_telegram_user_id = $2;

-- name: UpdateTelegramGroupMemberTagsEnabledByOwner :execrows
UPDATE telegram_groups
SET member_tags_enabled = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
  AND owner_telegram_user_id = $2;

-- name: UpdateTelegramGroupMemberTagFormatByOwner :execrows
UPDATE telegram_groups
SET member_tag_format = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
  AND owner_telegram_user_id = $2;

-- name: UpdateTelegramGroupDefenderEnabledByOwner :execrows
UPDATE telegram_groups
SET defender_enabled = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
  AND owner_telegram_user_id = $2;

-- name: UpdateTelegramGroupDefenderRemoveBlockedByOwner :execrows
UPDATE telegram_groups
SET defender_remove_blocked = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
  AND owner_telegram_user_id = $2;

-- name: UpdateTelegramGroupDefenderBanDurationSecByOwner :execrows
UPDATE telegram_groups
SET defender_ban_duration_sec = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
  AND owner_telegram_user_id = $2;

-- name: UpdateTelegramGroupForumFlagsByChatID :execrows
UPDATE telegram_groups
SET is_forum = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1;

-- name: UpdateTelegramGroupPRRNotificationsEnabledByOwner :execrows
UPDATE telegram_groups
SET prr_notifications_enabled = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
  AND owner_telegram_user_id = $2;

-- name: UpdateTelegramGroupPRRNotificationDestinationByOwner :execrows
UPDATE telegram_groups
SET prr_notifications_thread_id = $3,
    prr_notifications_thread_label = $4,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
  AND owner_telegram_user_id = $2;

-- name: UpdateTelegramGroupPRRWithdrawnBehaviorByOwner :execrows
UPDATE telegram_groups
SET prr_withdrawn_behavior = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
  AND owner_telegram_user_id = $2;

-- name: UpdateTelegramGroupTeamNotificationsEnabledByOwner :execrows
UPDATE telegram_groups
SET team_notifications_enabled = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
  AND owner_telegram_user_id = $2;

-- name: UpdateTelegramGroupTeamNotificationDestinationByOwner :execrows
UPDATE telegram_groups
SET team_notifications_thread_id = $3,
    team_notifications_thread_label = $4,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
  AND owner_telegram_user_id = $2;

-- name: UpdateTelegramGroupTeamWithdrawnBehaviorByOwner :execrows
UPDATE telegram_groups
SET team_withdrawn_behavior = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
  AND owner_telegram_user_id = $2;

-- name: ListTelegramGroupsWithPRRNotifications :many
SELECT * FROM telegram_groups
WHERE is_active = true
  AND is_initialized = true
  AND prr_notifications_enabled = true
ORDER BY chat_title;

-- name: ListTelegramGroupsWithTeamNotifications :many
SELECT * FROM telegram_groups
WHERE is_active = true
  AND is_initialized = true
  AND team_notifications_enabled = true
ORDER BY chat_title;

-- name: ListTelegramGroupPRRProjectFilters :many
SELECT * FROM telegram_group_prr_project_filters
WHERE chat_id = $1
ORDER BY created_at ASC, project_id;

-- name: UpsertTelegramGroupPRRProjectFilterByOwner :execrows
INSERT INTO telegram_group_prr_project_filters (chat_id, project_id)
SELECT g.chat_id, $3
FROM telegram_groups g
WHERE g.chat_id = $1
  AND g.owner_telegram_user_id = $2
ON CONFLICT (chat_id, project_id) DO NOTHING;

-- name: DeleteTelegramGroupPRRProjectFilterByOwner :execrows
DELETE FROM telegram_group_prr_project_filters f
USING telegram_groups g
WHERE f.chat_id = $1
  AND f.project_id = $3
  AND g.chat_id = f.chat_id
  AND g.owner_telegram_user_id = $2;

-- name: ClearTelegramGroupPRRProjectFiltersByOwner :execrows
DELETE FROM telegram_group_prr_project_filters f
USING telegram_groups g
WHERE f.chat_id = $1
  AND g.chat_id = f.chat_id
  AND g.owner_telegram_user_id = $2;

-- name: ListTelegramGroupPRRCampusFilters :many
SELECT * FROM telegram_group_prr_campus_filters
WHERE chat_id = $1
ORDER BY created_at ASC, campus_id;

-- name: UpsertTelegramGroupPRRCampusFilterByOwner :execrows
INSERT INTO telegram_group_prr_campus_filters (chat_id, campus_id)
SELECT g.chat_id, $3
FROM telegram_groups g
WHERE g.chat_id = $1
  AND g.owner_telegram_user_id = $2
ON CONFLICT (chat_id, campus_id) DO NOTHING;

-- name: DeleteTelegramGroupPRRCampusFilterByOwner :execrows
DELETE FROM telegram_group_prr_campus_filters f
USING telegram_groups g
WHERE f.chat_id = $1
  AND f.campus_id = $3
  AND g.chat_id = f.chat_id
  AND g.owner_telegram_user_id = $2;

-- name: ClearTelegramGroupPRRCampusFiltersByOwner :execrows
DELETE FROM telegram_group_prr_campus_filters f
USING telegram_groups g
WHERE f.chat_id = $1
  AND g.chat_id = f.chat_id
  AND g.owner_telegram_user_id = $2;

-- name: UpsertTelegramGroupPRRMessage :exec
INSERT INTO telegram_group_prr_messages (
    review_request_id,
    chat_id,
    message_id,
    message_thread_id,
    last_rendered_status
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (review_request_id, chat_id) DO UPDATE SET
    message_id = EXCLUDED.message_id,
    message_thread_id = EXCLUDED.message_thread_id,
    last_rendered_status = EXCLUDED.last_rendered_status,
    last_rendered_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP;

-- name: UpdateTelegramGroupPRRMessageStatus :exec
UPDATE telegram_group_prr_messages
SET last_rendered_status = $3,
    last_rendered_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE review_request_id = $1
  AND chat_id = $2;

-- name: ListTelegramGroupPRRMessagesByReviewRequest :many
SELECT * FROM telegram_group_prr_messages
WHERE review_request_id = $1
ORDER BY created_at ASC;

-- name: DeleteTelegramGroupPRRMessageByReviewRequestAndChat :execrows
DELETE FROM telegram_group_prr_messages
WHERE review_request_id = $1
  AND chat_id = $2;

-- name: DeleteTelegramGroupPRRMessagesByReviewRequest :exec
DELETE FROM telegram_group_prr_messages
WHERE review_request_id = $1;

-- name: ListTelegramGroupTeamProjectFilters :many
SELECT * FROM telegram_group_team_project_filters
WHERE chat_id = $1
ORDER BY created_at ASC, project_id;

-- name: UpsertTelegramGroupTeamProjectFilterByOwner :execrows
INSERT INTO telegram_group_team_project_filters (chat_id, project_id)
SELECT g.chat_id, $3
FROM telegram_groups g
WHERE g.chat_id = $1
  AND g.owner_telegram_user_id = $2
ON CONFLICT (chat_id, project_id) DO NOTHING;

-- name: DeleteTelegramGroupTeamProjectFilterByOwner :execrows
DELETE FROM telegram_group_team_project_filters f
USING telegram_groups g
WHERE f.chat_id = $1
  AND f.project_id = $3
  AND g.chat_id = f.chat_id
  AND g.owner_telegram_user_id = $2;

-- name: ClearTelegramGroupTeamProjectFiltersByOwner :execrows
DELETE FROM telegram_group_team_project_filters f
USING telegram_groups g
WHERE f.chat_id = $1
  AND g.chat_id = f.chat_id
  AND g.owner_telegram_user_id = $2;

-- name: ListTelegramGroupTeamCampusFilters :many
SELECT * FROM telegram_group_team_campus_filters
WHERE chat_id = $1
ORDER BY created_at ASC, campus_id;

-- name: UpsertTelegramGroupTeamCampusFilterByOwner :execrows
INSERT INTO telegram_group_team_campus_filters (chat_id, campus_id)
SELECT g.chat_id, $3
FROM telegram_groups g
WHERE g.chat_id = $1
  AND g.owner_telegram_user_id = $2
ON CONFLICT (chat_id, campus_id) DO NOTHING;

-- name: DeleteTelegramGroupTeamCampusFilterByOwner :execrows
DELETE FROM telegram_group_team_campus_filters f
USING telegram_groups g
WHERE f.chat_id = $1
  AND f.campus_id = $3
  AND g.chat_id = f.chat_id
  AND g.owner_telegram_user_id = $2;

-- name: ClearTelegramGroupTeamCampusFiltersByOwner :execrows
DELETE FROM telegram_group_team_campus_filters f
USING telegram_groups g
WHERE f.chat_id = $1
  AND g.chat_id = f.chat_id
  AND g.owner_telegram_user_id = $2;

-- name: UpsertTelegramGroupTeamMessage :exec
INSERT INTO telegram_group_team_messages (
    team_search_request_id,
    chat_id,
    message_id,
    message_thread_id,
    last_rendered_status
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (team_search_request_id, chat_id) DO UPDATE SET
    message_id = EXCLUDED.message_id,
    message_thread_id = EXCLUDED.message_thread_id,
    last_rendered_status = EXCLUDED.last_rendered_status,
    last_rendered_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP;

-- name: UpdateTelegramGroupTeamMessageStatus :exec
UPDATE telegram_group_team_messages
SET last_rendered_status = $3,
    last_rendered_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE team_search_request_id = $1
  AND chat_id = $2;

-- name: ListTelegramGroupTeamMessagesByRequest :many
SELECT * FROM telegram_group_team_messages
WHERE team_search_request_id = $1
ORDER BY created_at ASC;

-- name: DeleteTelegramGroupTeamMessageByRequestAndChat :execrows
DELETE FROM telegram_group_team_messages
WHERE team_search_request_id = $1
  AND chat_id = $2;

-- name: DeleteTelegramGroupTeamMessagesByRequest :exec
DELETE FROM telegram_group_team_messages
WHERE team_search_request_id = $1;

-- name: ListTelegramGroupDefenderCampusFilters :many
SELECT * FROM telegram_group_defender_campus_filters
WHERE chat_id = $1
ORDER BY created_at ASC, campus_id;

-- name: UpsertTelegramGroupDefenderCampusFilterByOwner :execrows
INSERT INTO telegram_group_defender_campus_filters (chat_id, campus_id)
SELECT g.chat_id, $3
FROM telegram_groups g
WHERE g.chat_id = $1
  AND g.owner_telegram_user_id = $2
ON CONFLICT (chat_id, campus_id) DO NOTHING;

-- name: DeleteTelegramGroupDefenderCampusFilterByOwner :execrows
DELETE FROM telegram_group_defender_campus_filters f
USING telegram_groups g
WHERE f.chat_id = $1
  AND f.campus_id = $3
  AND g.chat_id = f.chat_id
  AND g.owner_telegram_user_id = $2;

-- name: ClearTelegramGroupDefenderCampusFiltersByOwner :execrows
DELETE FROM telegram_group_defender_campus_filters f
USING telegram_groups g
WHERE f.chat_id = $1
  AND g.chat_id = f.chat_id
  AND g.owner_telegram_user_id = $2;

-- name: ListTelegramGroupDefenderTribeFilters :many
SELECT * FROM telegram_group_defender_tribe_filters
WHERE chat_id = $1
ORDER BY created_at ASC, coalition_id;

-- name: UpsertTelegramGroupDefenderTribeFilterByOwner :execrows
INSERT INTO telegram_group_defender_tribe_filters (chat_id, campus_id, coalition_id)
SELECT g.chat_id, $3, $4
FROM telegram_groups g
WHERE g.chat_id = $1
  AND g.owner_telegram_user_id = $2
ON CONFLICT (chat_id, campus_id, coalition_id) DO NOTHING;

-- name: DeleteTelegramGroupDefenderTribeFilterByOwner :execrows
DELETE FROM telegram_group_defender_tribe_filters f
USING telegram_groups g
WHERE f.chat_id = $1
  AND f.campus_id = $3
  AND f.coalition_id = $4
  AND g.chat_id = f.chat_id
  AND g.owner_telegram_user_id = $2;

-- name: ClearTelegramGroupDefenderTribeFiltersByOwner :execrows
DELETE FROM telegram_group_defender_tribe_filters f
USING telegram_groups g
WHERE f.chat_id = $1
  AND g.chat_id = f.chat_id
  AND g.owner_telegram_user_id = $2;

-- name: UpsertTelegramGroupMember :one
INSERT INTO telegram_group_members (
    chat_id, telegram_user_id, is_member, is_bot, last_status, last_seen_at
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (chat_id, telegram_user_id) DO UPDATE SET
    is_member = EXCLUDED.is_member,
    is_bot = EXCLUDED.is_bot,
    last_status = EXCLUDED.last_status,
    last_seen_at = EXCLUDED.last_seen_at,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: MarkTelegramGroupMemberLeft :exec
UPDATE telegram_group_members
SET is_member = false,
    last_status = $3,
    last_seen_at = $4,
    updated_at = CURRENT_TIMESTAMP
WHERE chat_id = $1
  AND telegram_user_id = $2;

-- name: ListTelegramGroupKnownMembers :many
SELECT * FROM telegram_group_members
WHERE chat_id = $1
  AND is_member = true
ORDER BY telegram_user_id;

-- name: UpsertTelegramGroupWhitelist :one
INSERT INTO telegram_group_whitelists (
    chat_id, telegram_user_id, added_by_account_id
) VALUES (
    $1, $2, $3
)
ON CONFLICT (chat_id, telegram_user_id) DO UPDATE SET
    added_by_account_id = EXCLUDED.added_by_account_id,
    created_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: DeleteTelegramGroupWhitelistByOwner :execrows
DELETE FROM telegram_group_whitelists w
USING telegram_groups g
WHERE w.chat_id = $1
  AND w.telegram_user_id = $2
  AND g.chat_id = w.chat_id
  AND g.owner_telegram_user_id = $3;

-- name: ListTelegramGroupWhitelists :many
SELECT * FROM telegram_group_whitelists
WHERE chat_id = $1
ORDER BY created_at DESC
LIMIT sqlc.arg(row_limit);

-- name: ExistsTelegramGroupWhitelist :one
SELECT EXISTS (
    SELECT 1
    FROM telegram_group_whitelists
    WHERE chat_id = $1
      AND telegram_user_id = $2
);

-- name: InsertTelegramGroupLog :exec
INSERT INTO telegram_group_logs (
    chat_id, source, telegram_user_id, action, reason, details
) VALUES (
    $1, $2, $3, $4, $5, $6
);

-- name: ListTelegramGroupLogs :many
SELECT * FROM telegram_group_logs
WHERE chat_id = $1
ORDER BY created_at DESC
LIMIT sqlc.arg(row_limit);

-- name: ListMemberTagGroupsByTelegramUser :many
SELECT g.*
FROM telegram_groups g
JOIN telegram_group_members m ON m.chat_id = g.chat_id
WHERE m.telegram_user_id = $1
  AND m.is_member = true
  AND g.is_active = true
  AND g.is_initialized = true
  AND g.member_tags_enabled = true
ORDER BY g.chat_title;

-- name: GetUserBotSettings :one
SELECT * FROM user_bot_settings 
WHERE user_account_id = $1;

-- name: GetUserAccountIDByExternalId :one
SELECT id
FROM user_accounts
WHERE platform = $1
  AND external_id = $2;

-- name: UpsertUserBotSettings :one
INSERT INTO user_bot_settings (
    user_account_id, language_code, notifications_enabled, review_post_campus_ids
) VALUES (
    $1, $2, $3, $4
)
ON CONFLICT (user_account_id) DO UPDATE SET
    language_code = EXCLUDED.language_code,
    notifications_enabled = EXCLUDED.notifications_enabled,
    review_post_campus_ids = EXCLUDED.review_post_campus_ids,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: GetPlatformCredentials :one
SELECT * FROM platform_credentials
WHERE s21_login = $1;

-- name: UpsertPlatformCredentials :exec
INSERT INTO platform_credentials (
    s21_login, password_enc, password_nonce, 
    access_token_enc, access_nonce, access_expires_at, refresh_token_enc, 
    refresh_nonce, refresh_expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (s21_login) DO UPDATE SET
    password_enc = EXCLUDED.password_enc,
    password_nonce = EXCLUDED.password_nonce,
    access_token_enc = EXCLUDED.access_token_enc,
    access_nonce = EXCLUDED.access_nonce,
    access_expires_at = EXCLUDED.access_expires_at,
    refresh_token_enc = EXCLUDED.refresh_token_enc,
    refresh_nonce = EXCLUDED.refresh_nonce,
    refresh_expires_at = EXCLUDED.refresh_expires_at,
    updated_at = CURRENT_TIMESTAMP;

-- name: GetRocketChatCredentials :one
SELECT * FROM rocketchat_credentials
WHERE s21_login = $1;

-- name: UpsertRocketChatCredentials :exec
INSERT INTO rocketchat_credentials (
    s21_login, rc_token_enc, rc_nonce
) VALUES (
    $1, $2, $3
)
ON CONFLICT (s21_login) DO UPDATE SET
    rc_token_enc = EXCLUDED.rc_token_enc,
    rc_nonce = EXCLUDED.rc_nonce,
    updated_at = CURRENT_TIMESTAMP;

-- name: CreateAuthVerificationCode :one
INSERT INTO auth_verification_codes (
    s21_login, code, expires_at
) VALUES (
    $1, $2, $3
)
RETURNING *;

-- name: GetLastAuthVerificationCode :one
SELECT * FROM auth_verification_codes
WHERE s21_login = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: GetValidAuthVerificationCode :one
SELECT * FROM auth_verification_codes
WHERE s21_login = $1 AND code = $2 AND expires_at > CURRENT_TIMESTAMP
ORDER BY created_at DESC
LIMIT 1;

-- name: DeleteAuthVerificationCode :exec
DELETE FROM auth_verification_codes
WHERE s21_login = $1 AND code = $2;

-- name: DeleteAllAuthVerificationCodes :exec
DELETE FROM auth_verification_codes
WHERE s21_login = $1;

-- name: DeleteExpiredAuthVerificationCodes :exec
DELETE FROM auth_verification_codes
WHERE expires_at < CURRENT_TIMESTAMP;

-- name: GetFSMState :one
SELECT * FROM fsm_user_states WHERE user_id = $1;

-- name: UpsertFSMState :exec
INSERT INTO fsm_user_states (
    user_id, current_flow, current_state, context, language
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (user_id) DO UPDATE SET
    current_flow = EXCLUDED.current_flow,
    current_state = EXCLUDED.current_state,
    context = EXCLUDED.context,
    language = EXCLUDED.language,
    updated_at = CURRENT_TIMESTAMP;

-- name: CreateApiKey :one
INSERT INTO api_keys (
    user_account_id, key_hash, prefix, expires_at
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: GetApiKeyByHash :one
SELECT * FROM api_keys
WHERE key_hash = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP);

-- name: RevokeOldApiKeys :exec
UPDATE api_keys
SET revoked_at = CURRENT_TIMESTAMP
WHERE user_account_id = $1 AND revoked_at IS NULL;

-- name: GetActiveApiKey :one
SELECT * FROM api_keys
WHERE user_account_id = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
ORDER BY created_at DESC
LIMIT 1;

SELECT * FROM campuses WHERE short_name = $1;

-- name: GetCampusByID :one
SELECT id, short_name, full_name, name_en, name_ru, timezone, is_active, leader_name, leader_form_link, created_at, updated_at FROM campuses WHERE id = $1;
 
INSERT INTO club_categories (name) VALUES ($1)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: UpsertCoalition :exec
INSERT INTO coalitions (campus_id, id, name)
VALUES ($1, $2, $3)
ON CONFLICT (campus_id, id) DO UPDATE SET name = EXCLUDED.name;

-- name: ExistsCoalitionByID :one
SELECT EXISTS (
    SELECT 1
    FROM coalitions
    WHERE campus_id = $1 AND id = $2
);

-- name: ListCoalitionsByCampus :many
SELECT * FROM coalitions
WHERE campus_id = $1
ORDER BY name;

-- name: UpsertClub :one
INSERT INTO clubs (
    id, campus_id, leader_login, name, description, category_id,
    external_link, is_local, is_active, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, CURRENT_TIMESTAMP
)
ON CONFLICT (campus_id, id) DO UPDATE SET
    leader_login = EXCLUDED.leader_login,
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    category_id = EXCLUDED.category_id,
    external_link = EXCLUDED.external_link,
    is_local = EXCLUDED.is_local,
    is_active = EXCLUDED.is_active,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: DeactivateClubsByCampus :exec
UPDATE clubs
SET is_active = false, updated_at = CURRENT_TIMESTAMP
WHERE campus_id = $1;


-- name: UpsertCampus :one
INSERT INTO campuses (id, short_name, full_name, name_en, name_ru, timezone, is_active, leader_name, leader_form_link)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (id) DO UPDATE SET
    short_name = EXCLUDED.short_name,
    full_name = EXCLUDED.full_name,
    name_en = COALESCE(EXCLUDED.name_en, campuses.name_en),
    name_ru = COALESCE(EXCLUDED.name_ru, campuses.name_ru),
    timezone = EXCLUDED.timezone,
    is_active = EXCLUDED.is_active,
    leader_name = COALESCE(EXCLUDED.leader_name, campuses.leader_name),
    leader_form_link = COALESCE(EXCLUDED.leader_form_link, campuses.leader_form_link),
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: UpsertSkill :one
INSERT INTO skills (id, name, category)
VALUES ($1, $2, $3)
ON CONFLICT (name) DO UPDATE SET
    category = EXCLUDED.category,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: UpsertParticipantSkill :exec
INSERT INTO participant_skills (s21_login, skill_id, value)
VALUES ($1, $2, $3)
ON CONFLICT (s21_login, skill_id) DO UPDATE SET
    value = EXCLUDED.value,
    updated_at = CURRENT_TIMESTAMP;

-- name: GetParticipantSkills :many
SELECT s.name, s.category, ss.value
FROM participant_skills ss
JOIN skills s ON ss.skill_id = s.id
WHERE ss.s21_login = $1;

-- name: GetParticipantStatsCache :one
SELECT
    c.s21_login,
    COALESCE(NULLIF(BTRIM(camp.name_ru), ''), NULLIF(BTRIM(camp.name_en), ''), NULLIF(BTRIM(camp.short_name), ''), '') AS campus_name,
    co.name AS coalition_name,
    c.status,
    c.level,
    c.exp_value,
    c.prp,
    c.crp,
    c.coins,
    c.parallel_name,
    c.class_name,
    c.integrity,
    c.friendliness,
    c.punctuality,
    c.thoroughness,
    c.updated_at,
    c.lat_synced_at
FROM participant_stats_cache c
LEFT JOIN campuses camp ON c.campus_id = camp.id
LEFT JOIN coalitions co ON c.campus_id = co.campus_id AND c.coalition_id = co.id
WHERE c.s21_login = $1;

-- name: UpsertParticipantStatsCache :exec
INSERT INTO participant_stats_cache (
    s21_login, campus_id, coalition_id, status, level, exp_value,
    prp, crp, coins, parallel_name, class_name,
    integrity, friendliness, punctuality, thoroughness,
    lat_synced_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    CURRENT_TIMESTAMP
)
ON CONFLICT (s21_login) DO UPDATE SET
    campus_id = EXCLUDED.campus_id,
    coalition_id = EXCLUDED.coalition_id,
    status = EXCLUDED.status,
    level = EXCLUDED.level,
    exp_value = EXCLUDED.exp_value,
    prp = EXCLUDED.prp,
    crp = EXCLUDED.crp,
    coins = EXCLUDED.coins,
    parallel_name = EXCLUDED.parallel_name,
    class_name = EXCLUDED.class_name,
    integrity = EXCLUDED.integrity,
    friendliness = EXCLUDED.friendliness,
    punctuality = EXCLUDED.punctuality,
    thoroughness = EXCLUDED.thoroughness,
    updated_at = CURRENT_TIMESTAMP,
    lat_synced_at = CURRENT_TIMESTAMP;

-- name: GetMyProfile :one
-- Профиль зарегистрированного пользователя: регистрационные данные + статистика из кеша.
SELECT
    r.s21_login,
    r.rocketchat_id,
    r.timezone,
    r.alternative_contact,
    r.has_coffee_ban,
    camp.id AS campus_id,
    COALESCE(NULLIF(BTRIM(camp.name_ru), ''), NULLIF(BTRIM(camp.name_en), ''), NULLIF(BTRIM(camp.short_name), ''), '') AS campus_name,
    co.name AS coalition_name,
    c.status,
    c.level,
    c.exp_value,
    c.prp,
    c.crp,
    c.coins,
    c.parallel_name,
    c.class_name,
    c.integrity,
    c.friendliness,
    c.punctuality,
    c.thoroughness
FROM registered_users r
LEFT JOIN participant_stats_cache c ON r.s21_login = c.s21_login
LEFT JOIN campuses camp ON c.campus_id = camp.id
LEFT JOIN coalitions co ON c.campus_id = co.campus_id AND c.coalition_id = co.id
WHERE r.s21_login = $1;

-- name: GetPeerProfile :one
-- Профиль пира: из кеша статистики + telegram username если зарегистрирован.
SELECT
    c.s21_login,
    COALESCE(ua.username, '') AS telegram_username,
    COALESCE(ua.external_id, '') AS external_id,
    ua.is_searchable,
    COALESCE(NULLIF(BTRIM(camp.name_ru), ''), NULLIF(BTRIM(camp.name_en), ''), NULLIF(BTRIM(camp.short_name), ''), '') AS campus_name,
    co.name AS coalition_name,
    c.status,
    c.level,
    c.exp_value,
    c.prp,
    c.crp,
    c.coins,
    c.parallel_name,
    c.class_name,
    c.integrity,
    c.friendliness,
    c.punctuality,
    c.thoroughness
FROM participant_stats_cache c
LEFT JOIN campuses camp ON c.campus_id = camp.id
LEFT JOIN coalitions co ON c.campus_id = co.campus_id AND c.coalition_id = co.id
LEFT JOIN user_accounts ua ON c.s21_login = ua.s21_login AND ua.platform = 'telegram'
WHERE c.s21_login = $1;
-- name: GetCampusByShortName :one
SELECT id, short_name, full_name, name_en, name_ru, timezone, is_active, created_at, updated_at, leader_name, leader_form_link
FROM campuses
WHERE short_name = $1;

-- name: UpsertClubCategory :one
INSERT INTO club_categories (name)
VALUES ($1)
ON CONFLICT (name) DO UPDATE SET
    name = EXCLUDED.name
RETURNING *;

-- name: GetLocalClubs :many
SELECT 
    c.id,
    c.name,
    c.description,
    c.leader_login,
    c.external_link,
    cat.name as category_name,
    c.is_local,
    c.is_active,
    COALESCE(NULLIF(BTRIM(camp.name_ru), ''), NULLIF(BTRIM(camp.name_en), ''), NULLIF(BTRIM(camp.short_name), ''), '') as campus_name
FROM clubs c
JOIN club_categories cat ON c.category_id = cat.id
JOIN campuses camp ON c.campus_id = camp.id
WHERE c.is_active = true AND c.is_local = true AND c.campus_id = $1
ORDER BY c.name;

-- name: GetGlobalClubs :many
SELECT 
    c.id,
    c.name,
    c.description,
    c.leader_login,
    c.external_link,
    cat.name as category_name,
    c.is_local,
    c.is_active,
    COALESCE(NULLIF(BTRIM(camp.name_ru), ''), NULLIF(BTRIM(camp.name_en), ''), NULLIF(BTRIM(camp.short_name), ''), '') as campus_name
FROM clubs c
JOIN club_categories cat ON c.category_id = cat.id
JOIN campuses camp ON c.campus_id = camp.id
WHERE c.is_active = true AND c.is_local = false
ORDER BY c.name;


-- name: UpsertRoom :one
INSERT INTO rooms (
    id, campus_id, name, min_duration, max_duration, is_active, description, capacity, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, CURRENT_TIMESTAMP
)
ON CONFLICT (campus_id, id) DO UPDATE SET
    name = EXCLUDED.name,
    min_duration = EXCLUDED.min_duration,
    max_duration = EXCLUDED.max_duration,
    is_active = EXCLUDED.is_active,
    description = EXCLUDED.description,
    capacity = EXCLUDED.capacity,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: DeactivateRoomsByCampus :exec
UPDATE rooms
SET is_active = false, updated_at = CURRENT_TIMESTAMP
WHERE campus_id = $1;

-- name: UpsertBook :one
INSERT INTO books (
    id, campus_id, title, author, category, total_stock, description, is_active, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, CURRENT_TIMESTAMP
)
ON CONFLICT (campus_id, id) DO UPDATE SET
    title = EXCLUDED.title,
    author = EXCLUDED.author,
    category = EXCLUDED.category,
    total_stock = EXCLUDED.total_stock,
    description = EXCLUDED.description,
    is_active = EXCLUDED.is_active,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: DeactivateBooksByCampus :exec
UPDATE books
SET is_active = false, updated_at = CURRENT_TIMESTAMP
WHERE campus_id = $1;

-- name: HasActiveRooms :one
SELECT EXISTS(SELECT 1 FROM rooms WHERE campus_id = $1 AND is_active = true);

-- name: HasActiveBooks :one
SELECT EXISTS(SELECT 1 FROM books WHERE campus_id = $1 AND is_active = true);

-- name: GetActiveRoomsByCampus :many
SELECT * FROM rooms
WHERE campus_id = $1 AND is_active = true
ORDER BY id;

-- name: GetRoomByID :one
SELECT * FROM rooms
WHERE campus_id = $1 AND id = $2;

-- name: CreateRoomBooking :one
INSERT INTO room_bookings (
    campus_id, room_id, user_id, booking_date, start_time, duration_minutes
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (campus_id, room_id, booking_date, start_time) DO NOTHING
RETURNING *;

-- name: GetRoomBookingsByDate :many
SELECT rb.*, COALESCE(ua.s21_login, 'unknown') as nickname
FROM room_bookings rb
LEFT JOIN user_accounts ua ON rb.user_id = ua.id
WHERE rb.campus_id = $1 AND rb.room_id = $2 AND rb.booking_date = $3
ORDER BY rb.start_time;



-- name: GetUserRoomBookings :many
SELECT rb.*, r.name as room_name, c.short_name as campus_short_name
FROM room_bookings rb
JOIN rooms r ON rb.campus_id = r.campus_id AND rb.room_id = r.id
JOIN campuses c ON rb.campus_id = c.id
WHERE rb.user_id = $1 AND rb.booking_date >= CURRENT_DATE
ORDER BY rb.booking_date, rb.start_time;

-- name: CancelRoomBooking :exec
DELETE FROM room_bookings
WHERE id = $1 AND user_id = $2;

-- name: UpdateRoomBookingDuration :exec
UPDATE room_bookings
SET duration_minutes = $3
WHERE id = $1 AND user_id = $2;

-- name: GetBooksByCampus :many
SELECT b.*,
       (b.total_stock - (SELECT count(*) FROM book_loans bl WHERE bl.campus_id = b.campus_id AND bl.book_id = b.id AND bl.returned_at IS NULL))::int as available_stock
FROM books b
WHERE b.campus_id = $1 AND b.is_active = true
ORDER BY b.title
LIMIT $2 OFFSET $3;

-- name: GetBooksByCampusAndCategory :many
SELECT b.*,
       (b.total_stock - (SELECT count(*) FROM book_loans bl WHERE bl.campus_id = b.campus_id AND bl.book_id = b.id AND bl.returned_at IS NULL))::int as available_stock
FROM books b
WHERE b.campus_id = $1 AND b.is_active = true AND b.category = $2
ORDER BY b.title
LIMIT $3 OFFSET $4;

-- name: GetBooksByCampusAndAuthor :many
SELECT b.*,
       (b.total_stock - (SELECT count(*) FROM book_loans bl WHERE bl.campus_id = b.campus_id AND bl.book_id = b.id AND bl.returned_at IS NULL))::int as available_stock
FROM books b
WHERE b.campus_id = $1 AND b.is_active = true AND b.author = $2
ORDER BY b.title
LIMIT $3 OFFSET $4;

-- name: SearchBooks :many
SELECT b.*,
       (b.total_stock - (SELECT count(*) FROM book_loans bl WHERE bl.campus_id = b.campus_id AND bl.book_id = b.id AND bl.returned_at IS NULL))::int as available_stock
FROM books b
WHERE b.campus_id = $1 AND b.is_active = true AND (b.title ILIKE '%' || $2 || '%' OR b.author ILIKE '%' || $2 || '%')
ORDER BY b.title
LIMIT $3 OFFSET $4;


-- name: GetBookByID :one
SELECT b.*, 
       (b.total_stock - (SELECT count(*) FROM book_loans bl WHERE bl.campus_id = b.campus_id AND bl.book_id = b.id AND bl.returned_at IS NULL))::int as available_stock
FROM books b
WHERE b.campus_id = $1 AND b.id = $2;

-- name: CreateBookLoan :one
WITH availability AS (
    SELECT (total_stock - (SELECT count(*) FROM book_loans WHERE campus_id = $1 AND book_id = $2 AND returned_at IS NULL)) as available
    FROM books
    WHERE campus_id = $1 AND id = $2
)
INSERT INTO book_loans (
    campus_id, book_id, user_id, due_at
)
SELECT $1, $2, $3, $4
FROM availability
WHERE available > 0
RETURNING *;

-- name: GetUserBookLoans :many
SELECT bl.*, b.title as book_title, b.author as book_author
FROM book_loans bl
JOIN books b ON bl.campus_id = b.campus_id AND bl.book_id = b.id
WHERE bl.user_id = $1 AND bl.returned_at IS NULL
ORDER BY bl.due_at;

-- name: GetBookLoanHolders :many
SELECT ua.s21_login AS s21_login
FROM book_loans bl
JOIN user_accounts ua ON ua.id = bl.user_id
WHERE bl.campus_id = $1 AND bl.book_id = $2 AND bl.returned_at IS NULL
ORDER BY bl.borrowed_at;

-- name: ReturnBookLoan :exec
UPDATE book_loans
SET returned_at = CURRENT_TIMESTAMP
WHERE id = $1 AND user_id = $2 AND returned_at IS NULL;

-- name: GetBookCategories :many
SELECT DISTINCT category FROM books
WHERE campus_id = $1 AND is_active = true
ORDER BY category;

-- name: GetBookAuthors :many
SELECT DISTINCT author FROM books
WHERE campus_id = $1 AND is_active = true
ORDER BY author;

-- name: CountBooksByCampus :one
SELECT 
    count(*)::int as total_books,
    (count(*) - (SELECT count(*) FROM book_loans bl WHERE bl.campus_id = $1 AND bl.returned_at IS NULL))::int as available_books
FROM books
WHERE campus_id = $1 AND is_active = true;

-- name: GetUserActiveLoanCount :one
SELECT count(*)::int 
FROM book_loans 
WHERE user_id = $1 AND returned_at IS NULL;

-- name: CountSearchBooks :one
SELECT count(*)::int
FROM books
WHERE campus_id = $1 AND is_active = true AND (title ILIKE '%' || $2 || '%' OR author ILIKE '%' || $2 || '%');

-- name: CountBooksByCategory :one
SELECT count(*)::int
FROM books
WHERE campus_id = $1 AND is_active = true AND category = $2;

-- name: GetDistinctUserTimezones :many
SELECT DISTINCT timezone 
FROM registered_users 
WHERE timezone IS NOT NULL AND timezone != ''
ORDER BY timezone;

-- name: GetCampusesWithBookingsForTimezone :many
SELECT DISTINCT c.id, c.short_name, c.full_name, c.timezone
FROM campuses c
INNER JOIN rooms r ON r.campus_id = c.id AND r.is_active = true
INNER JOIN room_bookings rb ON rb.campus_id = c.id AND rb.booking_date = CURRENT_DATE
WHERE (c.timezone = $1 OR EXISTS (
    SELECT 1 FROM registered_users s 
    LEFT JOIN participant_stats_cache psc ON s.s21_login = psc.s21_login
    WHERE psc.campus_id = c.id AND s.timezone = $1
))
AND c.is_active = true
GROUP BY c.id, c.short_name, c.full_name, c.timezone;

-- name: GetAllActiveCampuses :many
SELECT id, short_name, full_name, name_en, name_ru, timezone
FROM campuses
WHERE is_active = true
ORDER BY short_name;

-- name: CountOpenReviewRequestsByUser :one
SELECT count(*)::int
FROM review_requests
WHERE requester_user_id = $1
  AND status NOT IN ('CLOSED', 'WITHDRAWN');

-- name: CountSearchingReviewRequests :one
SELECT count(*)::int
FROM review_requests
WHERE status = 'SEARCHING';

-- name: ExistsOpenReviewRequestByUserAndProject :one
SELECT EXISTS (
    SELECT 1
    FROM review_requests
    WHERE requester_user_id = $1
      AND project_id = $2
      AND status NOT IN ('CLOSED', 'WITHDRAWN')
);

-- name: CreateReviewRequest :one
INSERT INTO review_requests (
    requester_user_id,
    requester_s21_login,
    requester_campus_id,
    project_id,
    project_name,
    project_type,
    availability_text,
    request_note_text,
    requester_timezone,
    requester_timezone_offset,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'SEARCHING'
)
RETURNING *;

-- name: GetGlobalReviewProjectGroups :many
SELECT
    project_id,
    project_name,
    project_type,
    count(*)::int AS requests_count
FROM review_requests
WHERE status = 'SEARCHING'
GROUP BY project_id, project_name, project_type
ORDER BY requests_count DESC, project_name ASC;

-- name: GetOpenReviewRequestsByProject :many
SELECT
    rr.id,
    rr.requester_user_id,
    rr.requester_s21_login,
    rr.requester_campus_id,
    rr.project_id,
    rr.project_name,
    rr.project_type,
    rr.availability_text,
    rr.request_note_text,
    rr.requester_timezone,
    rr.requester_timezone_offset,
    rr.status,
    rr.view_count,
    rr.response_count,
    rr.created_at,
    rr.updated_at,
    rr.closed_at,
    COALESCE(NULLIF(BTRIM(c.name_ru), ''), NULLIF(BTRIM(c.name_en), ''), NULLIF(BTRIM(c.short_name), ''), '') AS requester_campus_name,
    COALESCE(psc.level::text, '0') AS requester_level,
    COALESCE(
        CASE
            WHEN ua.is_searchable = true AND COALESCE(trim(ua.username), '') <> '' THEN ua.username
            ELSE ''::text
        END,
        ''
    ) AS requester_telegram_username
FROM review_requests rr
LEFT JOIN campuses c ON rr.requester_campus_id = c.id
LEFT JOIN participant_stats_cache psc ON rr.requester_s21_login = psc.s21_login
LEFT JOIN user_accounts ua ON rr.requester_user_id = ua.id AND ua.platform = 'telegram'
WHERE rr.project_id = $1
  AND rr.status = 'SEARCHING'
ORDER BY rr.created_at DESC;

-- name: GetReviewRequestsForCleanup :many
SELECT
    id,
    requester_s21_login,
    project_id
FROM review_requests
WHERE status NOT IN ('CLOSED', 'WITHDRAWN')
  AND updated_at < $1
ORDER BY updated_at ASC
LIMIT $2;

-- name: GetMyOpenReviewRequests :many
SELECT
    rr.id,
    rr.requester_user_id,
    rr.requester_s21_login,
    rr.requester_campus_id,
    rr.project_id,
    rr.project_name,
    rr.project_type,
    rr.availability_text,
    rr.request_note_text,
    rr.requester_timezone,
    rr.requester_timezone_offset,
    rr.status,
    rr.view_count,
    rr.response_count,
    rr.created_at,
    rr.updated_at,
    rr.closed_at,
    COALESCE(NULLIF(BTRIM(c.name_ru), ''), NULLIF(BTRIM(c.name_en), ''), NULLIF(BTRIM(c.short_name), ''), '') AS requester_campus_name,
    COALESCE(psc.level::text, '0') AS requester_level,
    COALESCE(
        CASE
            WHEN ua.is_searchable = true AND COALESCE(trim(ua.username), '') <> '' THEN ua.username
            ELSE ''::text
        END,
        ''
    ) AS requester_telegram_username
FROM review_requests rr
LEFT JOIN campuses c ON rr.requester_campus_id = c.id
LEFT JOIN participant_stats_cache psc ON rr.requester_s21_login = psc.s21_login
LEFT JOIN user_accounts ua ON rr.requester_user_id = ua.id AND ua.platform = 'telegram'
WHERE rr.requester_user_id = $1
  AND rr.status NOT IN ('CLOSED', 'WITHDRAWN')
ORDER BY rr.created_at DESC;

-- name: GetMyReviewRequestByID :one
SELECT
    rr.id,
    rr.requester_user_id,
    rr.requester_s21_login,
    rr.requester_campus_id,
    rr.project_id,
    rr.project_name,
    rr.project_type,
    rr.availability_text,
    rr.request_note_text,
    rr.requester_timezone,
    rr.requester_timezone_offset,
    rr.status,
    rr.view_count,
    rr.response_count,
    rr.negotiating_reviewer_user_id,
    COALESCE(rr.negotiating_reviewer_s21_login, '') AS negotiating_reviewer_s21_login,
    COALESCE(rr.negotiating_reviewer_telegram_username, '') AS negotiating_reviewer_telegram_username,
    COALESCE(rr.negotiating_reviewer_rocketchat_id, '') AS negotiating_reviewer_rocketchat_id,
    COALESCE(rr.negotiating_reviewer_alternative_contact, '') AS negotiating_reviewer_alternative_contact,
    rr.negotiating_started_at,
    rr.created_at,
    rr.updated_at,
    rr.closed_at,
    COALESCE(NULLIF(BTRIM(c.name_ru), ''), NULLIF(BTRIM(c.name_en), ''), NULLIF(BTRIM(c.short_name), ''), '') AS requester_campus_name,
    COALESCE(psc.level::text, '0') AS requester_level,
    COALESCE(
        CASE
            WHEN ua.is_searchable = true AND COALESCE(trim(ua.username), '') <> '' THEN ua.username
            ELSE ''::text
        END,
        ''
    ) AS requester_telegram_username
FROM review_requests rr
LEFT JOIN campuses c ON rr.requester_campus_id = c.id
LEFT JOIN participant_stats_cache psc ON rr.requester_s21_login = psc.s21_login
LEFT JOIN user_accounts ua ON rr.requester_user_id = ua.id AND ua.platform = 'telegram'
WHERE rr.id = $1
  AND rr.requester_user_id = $2;

-- name: GetReviewRequestByID :one
SELECT
    rr.id,
    rr.requester_user_id,
    rr.requester_s21_login,
    rr.requester_campus_id,
    rr.project_id,
    rr.project_name,
    rr.project_type,
    rr.availability_text,
    rr.request_note_text,
    rr.requester_timezone,
    rr.requester_timezone_offset,
    rr.status,
    rr.view_count,
    rr.response_count,
    rr.created_at,
    rr.updated_at,
    rr.closed_at,
    COALESCE(NULLIF(BTRIM(c.name_ru), ''), NULLIF(BTRIM(c.name_en), ''), NULLIF(BTRIM(c.short_name), ''), '') AS requester_campus_name,
    COALESCE(psc.level::text, '0') AS requester_level,
    COALESCE(
        CASE
            WHEN ua.is_searchable = true AND COALESCE(trim(ua.username), '') <> '' THEN ua.username
            ELSE ''::text
        END,
        ''
    ) AS requester_telegram_username
FROM review_requests rr
LEFT JOIN campuses c ON rr.requester_campus_id = c.id
LEFT JOIN participant_stats_cache psc ON rr.requester_s21_login = psc.s21_login
LEFT JOIN user_accounts ua ON rr.requester_user_id = ua.id AND ua.platform = 'telegram'
WHERE rr.id = $1;

-- name: IncrementReviewRequestViewCount :one
UPDATE review_requests
SET view_count = view_count + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING view_count;

-- name: MarkReviewRequestNegotiatingAndIncrementResponses :one
UPDATE review_requests
SET response_count = response_count + 1,
    status = 'NEGOTIATING',
    updated_at = CURRENT_TIMESTAMP,
    negotiating_reviewer_user_id = $2,
    negotiating_reviewer_s21_login = $3,
    negotiating_reviewer_telegram_username = $4,
    negotiating_reviewer_rocketchat_id = $5,
    negotiating_reviewer_alternative_contact = $6,
    negotiating_started_at = CURRENT_TIMESTAMP
WHERE id = $1
  AND status = 'SEARCHING'
RETURNING response_count, status;

-- name: SetReviewRequestStatus :one
UPDATE review_requests
SET status = sqlc.arg(status)::enum_review_status,
    updated_at = CURRENT_TIMESTAMP,
    closed_at = CASE
        WHEN sqlc.arg(status)::enum_review_status IN ('CLOSED', 'WITHDRAWN') THEN CURRENT_TIMESTAMP
        ELSE NULL
    END,
    negotiating_reviewer_user_id = CASE WHEN sqlc.arg(status)::enum_review_status = 'SEARCHING' THEN NULL ELSE negotiating_reviewer_user_id END,
    negotiating_reviewer_s21_login = CASE WHEN sqlc.arg(status)::enum_review_status = 'SEARCHING' THEN NULL ELSE negotiating_reviewer_s21_login END,
    negotiating_reviewer_telegram_username = CASE WHEN sqlc.arg(status)::enum_review_status = 'SEARCHING' THEN NULL ELSE negotiating_reviewer_telegram_username END,
    negotiating_reviewer_rocketchat_id = CASE WHEN sqlc.arg(status)::enum_review_status = 'SEARCHING' THEN NULL ELSE negotiating_reviewer_rocketchat_id END,
    negotiating_reviewer_alternative_contact = CASE WHEN sqlc.arg(status)::enum_review_status = 'SEARCHING' THEN NULL ELSE negotiating_reviewer_alternative_contact END,
    negotiating_started_at = CASE WHEN sqlc.arg(status)::enum_review_status = 'SEARCHING' THEN NULL ELSE negotiating_started_at END
WHERE id = $1
  AND requester_user_id = $2
RETURNING id, status;

-- name: CloseReviewRequestByID :exec
UPDATE review_requests
SET status = 'CLOSED',
    updated_at = CURRENT_TIMESTAMP,
    closed_at = CURRENT_TIMESTAMP
WHERE id = $1
  AND status NOT IN ('CLOSED', 'WITHDRAWN');

-- name: CountOpenTeamSearchRequestsByUser :one
SELECT count(*)::int
FROM team_search_requests
WHERE requester_user_id = $1
  AND status NOT IN ('CLOSED', 'WITHDRAWN');

-- name: CountSearchingTeamSearchRequests :one
SELECT count(*)::int
FROM team_search_requests
WHERE status = 'SEARCHING';

-- name: ExistsOpenTeamSearchRequestByUserAndProject :one
SELECT EXISTS (
    SELECT 1
    FROM team_search_requests
    WHERE requester_user_id = $1
      AND project_id = $2
      AND status NOT IN ('CLOSED', 'WITHDRAWN')
);

-- name: CreateTeamSearchRequest :one
INSERT INTO team_search_requests (
    requester_user_id,
    requester_s21_login,
    requester_campus_id,
    project_id,
    project_name,
    project_type,
    planned_start_text,
    request_note_text,
    requester_timezone,
    requester_timezone_offset,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'SEARCHING'
)
RETURNING *;

-- name: GetGlobalTeamProjectGroups :many
SELECT
    project_id,
    project_name,
    project_type,
    count(*)::int AS requests_count
FROM team_search_requests
WHERE status = 'SEARCHING'
GROUP BY project_id, project_name, project_type
ORDER BY requests_count DESC, project_name ASC;

-- name: GetOpenTeamSearchRequestsByProject :many
SELECT
    tsr.id,
    tsr.requester_user_id,
    tsr.requester_s21_login,
    tsr.requester_campus_id,
    tsr.project_id,
    tsr.project_name,
    tsr.project_type,
    tsr.planned_start_text,
    tsr.request_note_text,
    tsr.requester_timezone,
    tsr.requester_timezone_offset,
    tsr.status,
    tsr.view_count,
    tsr.response_count,
    tsr.created_at,
    tsr.updated_at,
    tsr.closed_at,
    COALESCE(NULLIF(BTRIM(c.name_ru), ''), NULLIF(BTRIM(c.name_en), ''), NULLIF(BTRIM(c.short_name), ''), '') AS requester_campus_name,
    COALESCE(psc.level::text, '0') AS requester_level,
    COALESCE(
        CASE
            WHEN ua.is_searchable = true AND COALESCE(trim(ua.username), '') <> '' THEN ua.username
            ELSE ''::text
        END,
        ''
    ) AS requester_telegram_username
FROM team_search_requests tsr
LEFT JOIN campuses c ON tsr.requester_campus_id = c.id
LEFT JOIN participant_stats_cache psc ON tsr.requester_s21_login = psc.s21_login
LEFT JOIN user_accounts ua ON tsr.requester_user_id = ua.id AND ua.platform = 'telegram'
WHERE tsr.project_id = $1
  AND tsr.status = 'SEARCHING'
ORDER BY tsr.created_at DESC;

-- name: GetTeamSearchRequestsForCleanup :many
SELECT
    id,
    requester_s21_login,
    project_id
FROM team_search_requests
WHERE status NOT IN ('CLOSED', 'WITHDRAWN')
  AND updated_at < $1
ORDER BY updated_at ASC
LIMIT $2;

-- name: GetMyOpenTeamSearchRequests :many
SELECT
    tsr.id,
    tsr.requester_user_id,
    tsr.requester_s21_login,
    tsr.requester_campus_id,
    tsr.project_id,
    tsr.project_name,
    tsr.project_type,
    tsr.planned_start_text,
    tsr.request_note_text,
    tsr.requester_timezone,
    tsr.requester_timezone_offset,
    tsr.status,
    tsr.view_count,
    tsr.response_count,
    tsr.created_at,
    tsr.updated_at,
    tsr.closed_at,
    COALESCE(NULLIF(BTRIM(c.name_ru), ''), NULLIF(BTRIM(c.name_en), ''), NULLIF(BTRIM(c.short_name), ''), '') AS requester_campus_name,
    COALESCE(psc.level::text, '0') AS requester_level,
    COALESCE(
        CASE
            WHEN ua.is_searchable = true AND COALESCE(trim(ua.username), '') <> '' THEN ua.username
            ELSE ''::text
        END,
        ''
    ) AS requester_telegram_username
FROM team_search_requests tsr
LEFT JOIN campuses c ON tsr.requester_campus_id = c.id
LEFT JOIN participant_stats_cache psc ON tsr.requester_s21_login = psc.s21_login
LEFT JOIN user_accounts ua ON tsr.requester_user_id = ua.id AND ua.platform = 'telegram'
WHERE tsr.requester_user_id = $1
  AND tsr.status NOT IN ('CLOSED', 'WITHDRAWN')
ORDER BY tsr.created_at DESC;

-- name: GetMyTeamSearchRequestByID :one
SELECT
    tsr.id,
    tsr.requester_user_id,
    tsr.requester_s21_login,
    tsr.requester_campus_id,
    tsr.project_id,
    tsr.project_name,
    tsr.project_type,
    tsr.planned_start_text,
    tsr.request_note_text,
    tsr.requester_timezone,
    tsr.requester_timezone_offset,
    tsr.status,
    tsr.view_count,
    tsr.response_count,
    tsr.negotiating_peer_user_id,
    COALESCE(tsr.negotiating_peer_s21_login, '') AS negotiating_peer_s21_login,
    COALESCE(tsr.negotiating_peer_telegram_username, '') AS negotiating_peer_telegram_username,
    COALESCE(tsr.negotiating_peer_rocketchat_id, '') AS negotiating_peer_rocketchat_id,
    COALESCE(tsr.negotiating_peer_alternative_contact, '') AS negotiating_peer_alternative_contact,
    tsr.negotiating_started_at,
    tsr.created_at,
    tsr.updated_at,
    tsr.closed_at,
    COALESCE(NULLIF(BTRIM(c.name_ru), ''), NULLIF(BTRIM(c.name_en), ''), NULLIF(BTRIM(c.short_name), ''), '') AS requester_campus_name,
    COALESCE(psc.level::text, '0') AS requester_level,
    COALESCE(
        CASE
            WHEN ua.is_searchable = true AND COALESCE(trim(ua.username), '') <> '' THEN ua.username
            ELSE ''::text
        END,
        ''
    ) AS requester_telegram_username
FROM team_search_requests tsr
LEFT JOIN campuses c ON tsr.requester_campus_id = c.id
LEFT JOIN participant_stats_cache psc ON tsr.requester_s21_login = psc.s21_login
LEFT JOIN user_accounts ua ON tsr.requester_user_id = ua.id AND ua.platform = 'telegram'
WHERE tsr.id = $1
  AND tsr.requester_user_id = $2;

-- name: GetTeamSearchRequestByID :one
SELECT
    tsr.id,
    tsr.requester_user_id,
    tsr.requester_s21_login,
    tsr.requester_campus_id,
    tsr.project_id,
    tsr.project_name,
    tsr.project_type,
    tsr.planned_start_text,
    tsr.request_note_text,
    tsr.requester_timezone,
    tsr.requester_timezone_offset,
    tsr.status,
    tsr.view_count,
    tsr.response_count,
    tsr.created_at,
    tsr.updated_at,
    tsr.closed_at,
    COALESCE(NULLIF(BTRIM(c.name_ru), ''), NULLIF(BTRIM(c.name_en), ''), NULLIF(BTRIM(c.short_name), ''), '') AS requester_campus_name,
    COALESCE(psc.level::text, '0') AS requester_level,
    COALESCE(
        CASE
            WHEN ua.is_searchable = true AND COALESCE(trim(ua.username), '') <> '' THEN ua.username
            ELSE ''::text
        END,
        ''
    ) AS requester_telegram_username
FROM team_search_requests tsr
LEFT JOIN campuses c ON tsr.requester_campus_id = c.id
LEFT JOIN participant_stats_cache psc ON tsr.requester_s21_login = psc.s21_login
LEFT JOIN user_accounts ua ON tsr.requester_user_id = ua.id AND ua.platform = 'telegram'
WHERE tsr.id = $1;

-- name: IncrementTeamSearchRequestViewCount :one
UPDATE team_search_requests
SET view_count = view_count + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING view_count;

-- name: MarkTeamSearchRequestNegotiatingAndIncrementResponses :one
UPDATE team_search_requests
SET response_count = response_count + 1,
    status = 'NEGOTIATING',
    updated_at = CURRENT_TIMESTAMP,
    negotiating_peer_user_id = $2,
    negotiating_peer_s21_login = $3,
    negotiating_peer_telegram_username = $4,
    negotiating_peer_rocketchat_id = $5,
    negotiating_peer_alternative_contact = $6,
    negotiating_started_at = CURRENT_TIMESTAMP
WHERE id = $1
  AND status = 'SEARCHING'
RETURNING response_count, status;

-- name: SetTeamSearchRequestStatus :one
UPDATE team_search_requests
SET status = sqlc.arg(status)::enum_review_status,
    updated_at = CURRENT_TIMESTAMP,
    closed_at = CASE
        WHEN sqlc.arg(status)::enum_review_status IN ('CLOSED', 'WITHDRAWN') THEN CURRENT_TIMESTAMP
        ELSE NULL
    END,
    negotiating_peer_user_id = CASE WHEN sqlc.arg(status)::enum_review_status = 'SEARCHING' THEN NULL ELSE negotiating_peer_user_id END,
    negotiating_peer_s21_login = CASE WHEN sqlc.arg(status)::enum_review_status = 'SEARCHING' THEN NULL ELSE negotiating_peer_s21_login END,
    negotiating_peer_telegram_username = CASE WHEN sqlc.arg(status)::enum_review_status = 'SEARCHING' THEN NULL ELSE negotiating_peer_telegram_username END,
    negotiating_peer_rocketchat_id = CASE WHEN sqlc.arg(status)::enum_review_status = 'SEARCHING' THEN NULL ELSE negotiating_peer_rocketchat_id END,
    negotiating_peer_alternative_contact = CASE WHEN sqlc.arg(status)::enum_review_status = 'SEARCHING' THEN NULL ELSE negotiating_peer_alternative_contact END,
    negotiating_started_at = CASE WHEN sqlc.arg(status)::enum_review_status = 'SEARCHING' THEN NULL ELSE negotiating_started_at END
WHERE id = $1
  AND requester_user_id = $2
RETURNING id, status;

-- name: CloseTeamSearchRequestByID :exec
UPDATE team_search_requests
SET status = 'CLOSED',
    updated_at = CURRENT_TIMESTAMP,
    closed_at = CURRENT_TIMESTAMP
WHERE id = $1
  AND status NOT IN ('CLOSED', 'WITHDRAWN');

-- name: UpsertCourseCatalog :exec
INSERT INTO courses (
    id,
    title,
    code,
    sync_batch_id,
    updated_at
) VALUES (
    $1, $2, $3, $4, CURRENT_TIMESTAMP
)
ON CONFLICT (id) DO UPDATE SET
    title = EXCLUDED.title,
    code = EXCLUDED.code,
    sync_batch_id = EXCLUDED.sync_batch_id,
    updated_at = CURRENT_TIMESTAMP;

-- name: UpsertProjectCatalog :exec
INSERT INTO projects (
    id,
    course_id,
    title,
    code,
    sync_batch_id,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, CURRENT_TIMESTAMP
)
ON CONFLICT (id) DO UPDATE SET
    course_id = EXCLUDED.course_id,
    title = EXCLUDED.title,
    code = EXCLUDED.code,
    sync_batch_id = EXCLUDED.sync_batch_id,
    updated_at = CURRENT_TIMESTAMP;

-- name: UpsertNodeCatalog :one
INSERT INTO nodes (
    name,
    parent_id,
    sync_batch_id,
    updated_at
) VALUES (
    $1, $2, $3, CURRENT_TIMESTAMP
)
ON CONFLICT ((COALESCE(parent_id, 0)), lower(name)) DO UPDATE SET
    sync_batch_id = EXCLUDED.sync_batch_id,
    updated_at = CURRENT_TIMESTAMP
RETURNING id;

-- name: UpsertProjectNodeCatalog :exec
INSERT INTO project_nodes (
    project_id,
    node_id,
    sync_batch_id
) VALUES (
    $1, $2, $3
)
ON CONFLICT (project_id, node_id) DO UPDATE SET
    sync_batch_id = EXCLUDED.sync_batch_id;

-- name: DeleteStaleProjectNodesCatalog :exec
DELETE FROM project_nodes
WHERE sync_batch_id <> $1;

-- name: DeleteStaleProjectsCatalog :exec
DELETE FROM projects
WHERE sync_batch_id <> $1;

-- name: DeleteStaleCoursesCatalog :exec
DELETE FROM courses
WHERE sync_batch_id <> $1;

-- name: DeleteStaleNodesCatalog :exec
DELETE FROM nodes
WHERE sync_batch_id <> $1;

-- name: DeleteStaleProjectSearchCatalog :exec
DELETE FROM project_search
WHERE sync_batch_id <> $1;

-- name: CountSearchCatalogProjects :one
SELECT count(*)::int
FROM projects p
LEFT JOIN project_search ps ON ps.project_id = p.id
WHERE (
    $1 = ''
    OR ps.document @@ websearch_to_tsquery('simple', $1)
    OR lower(COALESCE(ps.search_text, '')) LIKE '%' || lower($1) || '%'
    OR lower(p.title) LIKE '%' || lower($1) || '%'
    OR lower(COALESCE(p.code, '')) LIKE '%' || lower($1) || '%'
    OR lower(COALESCE(c.title, '')) LIKE '%' || lower($1) || '%'
    OR lower(COALESCE(c.code, '')) LIKE '%' || lower($1) || '%'
);

-- name: SearchCatalogProjects :many
SELECT
    p.id AS project_id,
    p.title AS project_title,
    COALESCE(p.code, '') AS project_code,
    p.course_id,
    COALESCE(c.title, '') AS course_title,
    COALESCE(c.code, '') AS course_code,
    COALESCE(string_agg(DISTINCT n.name, ', ' ORDER BY n.name), '') AS node_names
FROM projects p
LEFT JOIN courses c ON c.id = p.course_id
LEFT JOIN project_nodes pn ON pn.project_id = p.id
LEFT JOIN nodes n ON n.id = pn.node_id
LEFT JOIN project_search ps ON ps.project_id = p.id
WHERE (
    $1 = ''
    OR ps.document @@ websearch_to_tsquery('simple', $1)
    OR lower(COALESCE(ps.search_text, '')) LIKE '%' || lower($1) || '%'
    OR lower(p.title) LIKE '%' || lower($1) || '%'
    OR lower(COALESCE(p.code, '')) LIKE '%' || lower($1) || '%'
    OR lower(COALESCE(c.title, '')) LIKE '%' || lower($1) || '%'
    OR lower(COALESCE(c.code, '')) LIKE '%' || lower($1) || '%'
)
GROUP BY p.id, c.id
ORDER BY
    CASE WHEN p.id = ANY($4::BIGINT[]) THEN 0 ELSE 1 END,
    p.title ASC
LIMIT $2 OFFSET $3;

-- name: SearchCatalogProjectsAll :many
SELECT
    p.id AS project_id,
    p.title AS project_title,
    COALESCE(p.code, '') AS project_code,
    p.course_id,
    COALESCE(c.title, '') AS course_title,
    COALESCE(c.code, '') AS course_code,
    COALESCE(string_agg(DISTINCT n.name, ', ' ORDER BY n.name), '') AS node_names
FROM projects p
LEFT JOIN courses c ON c.id = p.course_id
LEFT JOIN project_nodes pn ON pn.project_id = p.id
LEFT JOIN nodes n ON n.id = pn.node_id
LEFT JOIN project_search ps ON ps.project_id = p.id
WHERE (
    $1 = ''
    OR ps.document @@ websearch_to_tsquery('simple', $1)
)
GROUP BY p.id, c.id
ORDER BY p.title ASC;

-- name: SearchCatalogCourses :many
SELECT
    c.id,
    c.title,
    COALESCE(c.code, '') AS code,
    count(p.id)::int AS project_count
FROM courses c
LEFT JOIN projects p ON p.course_id = c.id
WHERE (
    $1 = ''
    OR to_tsvector('simple', concat_ws(' ', c.id::text, c.title, COALESCE(c.code, ''))) @@ websearch_to_tsquery('simple', $1)
    OR lower(c.title) LIKE '%' || lower($1) || '%'
    OR lower(COALESCE(c.code, '')) LIKE '%' || lower($1) || '%'
    OR EXISTS (
        SELECT 1
        FROM projects p2
        JOIN project_search ps ON ps.project_id = p2.id
        WHERE p2.course_id = c.id
          AND (
              ps.document @@ websearch_to_tsquery('simple', $1)
              OR lower(COALESCE(ps.search_text, '')) LIKE '%' || lower($1) || '%'
              OR lower(p2.title) LIKE '%' || lower($1) || '%'
              OR lower(COALESCE(p2.code, '')) LIKE '%' || lower($1) || '%'
          )
    )
)
GROUP BY c.id
ORDER BY c.title ASC;

-- name: SearchCatalogNodes :many
WITH RECURSIVE node_paths AS (
    SELECT
        n.id,
        n.name,
        n.parent_id,
        n.name::text AS path
    FROM nodes n
    WHERE n.parent_id IS NULL

    UNION ALL

    SELECT
        child.id,
        child.name,
        child.parent_id,
        (np.path || ' / ' || child.name)::text AS path
    FROM nodes child
    JOIN node_paths np ON np.id = child.parent_id
)
SELECT
    np.id,
    np.name,
    np.parent_id,
    np.path,
    COALESCE(cnt.project_count, 0)::int AS project_count
FROM node_paths np
LEFT JOIN (
    SELECT node_id, count(DISTINCT project_id)::int AS project_count
    FROM project_nodes
    GROUP BY node_id
) cnt ON cnt.node_id = np.id
WHERE (
    $1 = ''
    OR to_tsvector('simple', concat_ws(' ', np.id::text, np.path, np.name)) @@ websearch_to_tsquery('simple', $1)
    OR lower(np.path) LIKE '%' || lower($1) || '%'
    OR lower(np.name) LIKE '%' || lower($1) || '%'
)
ORDER BY np.path ASC;

-- name: GetCatalogProjectIDsByCourse :many
SELECT id
FROM projects
WHERE course_id = $1
ORDER BY id;

-- name: GetCatalogProjectIDsByNodeRecursive :many
WITH RECURSIVE subtree(node_id) AS (
    SELECT n.id
    FROM nodes n
    WHERE n.id = $1

    UNION ALL

    SELECT n.id
    FROM nodes n
    JOIN subtree s ON n.parent_id = s.node_id
)
SELECT DISTINCT pn.project_id AS project_id
FROM project_nodes pn
JOIN subtree s ON s.node_id = pn.node_id
ORDER BY project_id;

-- name: GetCatalogProjectTitlesByIDs :many
SELECT id, title
FROM projects
WHERE id = ANY($1::BIGINT[])
ORDER BY title ASC;
