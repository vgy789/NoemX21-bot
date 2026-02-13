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

-- name: DeleteUserAccountByExternalId :exec
DELETE FROM user_accounts
WHERE platform = $1 AND external_id = $2;

-- name: GetUserAccountByS21Login :one
SELECT * FROM user_accounts
WHERE s21_login = $1;

-- name: CreateUserAccount :one
INSERT INTO user_accounts (
    s21_login, platform, external_id, username, is_searchable, role
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: GetUserBotSettings :one
SELECT * FROM user_bot_settings 
WHERE user_account_id = $1;

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
    access_token, access_expires_at, refresh_token_enc, 
    refresh_nonce, refresh_expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (s21_login) DO UPDATE SET
    password_enc = EXCLUDED.password_enc,
    password_nonce = EXCLUDED.password_nonce,
    access_token = EXCLUDED.access_token,
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
SELECT id, short_name, full_name, timezone, is_active, created_at, updated_at FROM campuses WHERE id = $1;
 
INSERT INTO club_categories (name) VALUES ($1)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: UpsertCoalition :exec
INSERT INTO coalitions (id, name)
VALUES ($1, $2)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name;

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
INSERT INTO campuses (id, short_name, full_name, timezone, is_active)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO UPDATE SET
    short_name = EXCLUDED.short_name,
    full_name = EXCLUDED.full_name,
    timezone = EXCLUDED.timezone,
    is_active = EXCLUDED.is_active,
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
    camp.short_name AS campus_name,
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
LEFT JOIN coalitions co ON c.coalition_id = co.id
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
    camp.short_name AS campus_name,
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
LEFT JOIN coalitions co ON c.coalition_id = co.id
WHERE r.s21_login = $1;

-- name: GetPeerProfile :one
-- Профиль пира: из кеша статистики + telegram username если зарегистрирован.
SELECT
    c.s21_login,
    COALESCE(ua.username, '') AS telegram_username,
    COALESCE(ua.external_id, '') AS external_id,
    camp.short_name AS campus_name,
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
LEFT JOIN coalitions co ON c.coalition_id = co.id
LEFT JOIN user_accounts ua ON c.s21_login = ua.s21_login AND ua.platform = 'telegram'
WHERE c.s21_login = $1;
-- name: GetCampusByShortName :one
SELECT * FROM campuses WHERE short_name = $1;

-- name: UpsertClubCategory :one
INSERT INTO club_categories (name)
VALUES ($1)
ON CONFLICT (name) DO UPDATE SET
    name = EXCLUDED.name
RETURNING *;