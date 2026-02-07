-- queries.sql

-- name: GetStudentByS21Login :one
SELECT * FROM students WHERE s21_login = $1;

-- name: GetStudentProfile :one
SELECT s.*, c.short_name as campus_name, cool.name as coalition_name
FROM students s
LEFT JOIN campuses c ON s.campus_id = c.id
LEFT JOIN coalitions cool ON s.coalition_id = cool.id
WHERE s.s21_login = $1;

-- name: GetStudentByRocketChatId :one
SELECT * FROM students WHERE rocketchat_id = $1;

-- name: UpsertStudent :one
INSERT INTO students (
    s21_login, rocketchat_id, campus_id, coalition_id, status, 
    timezone, alternative_contact, has_coffee_ban,
    level, exp_value, prp, crp, coins, parallel_name
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
ON CONFLICT (s21_login) DO UPDATE SET
    rocketchat_id = COALESCE(EXCLUDED.rocketchat_id, students.rocketchat_id),
    campus_id = COALESCE(EXCLUDED.campus_id, students.campus_id),
    coalition_id = COALESCE(EXCLUDED.coalition_id, students.coalition_id),
    status = COALESCE(EXCLUDED.status, students.status),
    timezone = COALESCE(EXCLUDED.timezone, students.timezone),
    alternative_contact = COALESCE(EXCLUDED.alternative_contact, students.alternative_contact),
    has_coffee_ban = COALESCE(EXCLUDED.has_coffee_ban, students.has_coffee_ban),
    level = COALESCE(EXCLUDED.level, students.level),
    exp_value = COALESCE(EXCLUDED.exp_value, students.exp_value),
    prp = COALESCE(EXCLUDED.prp, students.prp),
    crp = COALESCE(EXCLUDED.crp, students.crp),
    coins = COALESCE(EXCLUDED.coins, students.coins),
    parallel_name = COALESCE(EXCLUDED.parallel_name, students.parallel_name),
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: UpdateStudentStats :exec
UPDATE students SET
    level = $2,
    exp_value = $3,
    prp = $4,
    crp = $5,
    coins = $6,
    coalition_id = $7,
    updated_at = CURRENT_TIMESTAMP
WHERE s21_login = $1;

-- name: GetUserAccountByExternalId :one
SELECT * FROM user_accounts 
WHERE platform = $1 AND external_id = $2;

-- name: GetUserAccountByStudentId :one
SELECT * FROM user_accounts
WHERE student_id = $1;

-- name: CreateUserAccount :one
INSERT INTO user_accounts (
    student_id, platform, external_id, username, is_searchable, role
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
WHERE student_id = $1;

-- name: UpsertPlatformCredentials :exec
INSERT INTO platform_credentials (
    student_id, password_enc, password_nonce, 
    access_token, access_expires_at, refresh_token_enc, 
    refresh_nonce, refresh_expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (student_id) DO UPDATE SET
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
WHERE student_id = $1;

-- name: UpsertRocketChatCredentials :exec
INSERT INTO rocketchat_credentials (
    student_id, rc_token_enc, rc_nonce
) VALUES (
    $1, $2, $3
)
ON CONFLICT (student_id) DO UPDATE SET
    rc_token_enc = EXCLUDED.rc_token_enc,
    rc_nonce = EXCLUDED.rc_nonce,
    updated_at = CURRENT_TIMESTAMP;

-- name: CreateAuthVerificationCode :one
INSERT INTO auth_verification_codes (
    student_id, code, expires_at
) VALUES (
    $1, $2, $3
)
RETURNING *;

-- name: GetLastAuthVerificationCode :one
SELECT * FROM auth_verification_codes
WHERE student_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: GetValidAuthVerificationCode :one
SELECT * FROM auth_verification_codes
WHERE student_id = $1 AND code = $2 AND expires_at > CURRENT_TIMESTAMP
ORDER BY created_at DESC
LIMIT 1;

-- name: DeleteAuthVerificationCode :exec
DELETE FROM auth_verification_codes
WHERE student_id = $1 AND code = $2;

-- name: DeleteAllAuthVerificationCodes :exec
DELETE FROM auth_verification_codes
WHERE student_id = $1;

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

-- name: GetCampusByShortName :one
SELECT * FROM campuses WHERE short_name = $1;

-- name: UpsertClubCategory :one
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
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    category = EXCLUDED.category,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: UpsertStudentSkill :exec
INSERT INTO student_skills (student_id, skill_id, value)
VALUES ($1, $2, $3)
ON CONFLICT (student_id, skill_id) DO UPDATE SET
    value = EXCLUDED.value,
    updated_at = CURRENT_TIMESTAMP;

-- name: GetStudentSkills :many
SELECT s.name, s.category, ss.value
FROM student_skills ss
JOIN skills s ON ss.skill_id = s.id
WHERE ss.student_id = $1;

-- name: GetPeerProfile :one
SELECT 
    s.s21_login, 
    COALESCE(ua.username, '') as telegram_username,
    c.short_name as campus_name,
    cool.name as coalition_name,
    s.level,
    s.exp_value,
    s.coins
FROM students s
LEFT JOIN campuses c ON s.campus_id = c.id
LEFT JOIN coalitions cool ON s.coalition_id = cool.id
LEFT JOIN user_accounts ua ON s.s21_login = ua.student_id AND ua.platform = 'telegram'
WHERE s.s21_login = $1;
