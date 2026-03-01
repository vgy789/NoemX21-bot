ALTER TABLE review_requests
    DROP COLUMN IF EXISTS negotiating_started_at,
    DROP COLUMN IF EXISTS negotiating_reviewer_alternative_contact,
    DROP COLUMN IF EXISTS negotiating_reviewer_rocketchat_id,
    DROP COLUMN IF EXISTS negotiating_reviewer_telegram_username,
    DROP COLUMN IF EXISTS negotiating_reviewer_s21_login,
    DROP COLUMN IF EXISTS negotiating_reviewer_user_id;
