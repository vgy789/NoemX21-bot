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
    timezone, alternative_contact, has_coffee_ban
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (s21_login) DO UPDATE SET
    rocketchat_id = EXCLUDED.rocketchat_id,
    campus_id = EXCLUDED.campus_id,
    coalition_id = EXCLUDED.coalition_id,
    status = EXCLUDED.status,
    timezone = EXCLUDED.timezone,
    alternative_contact = EXCLUDED.alternative_contact,
    has_coffee_ban = EXCLUDED.has_coffee_ban,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

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

-- name: GetValidAuthVerificationCode :one
SELECT * FROM auth_verification_codes
WHERE student_id = $1 AND code = $2 AND expires_at > CURRENT_TIMESTAMP
ORDER BY created_at DESC
LIMIT 1;

-- name: DeleteAuthVerificationCode :exec
DELETE FROM auth_verification_codes
WHERE student_id = $1 AND code = $2;

-- name: DeleteExpiredAuthVerificationCodes :exec
DELETE FROM auth_verification_codes
WHERE expires_at < CURRENT_TIMESTAMP;
