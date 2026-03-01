ALTER TABLE review_requests
    ADD COLUMN IF NOT EXISTS negotiating_reviewer_user_id BIGINT REFERENCES user_accounts(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS negotiating_reviewer_s21_login VARCHAR(100),
    ADD COLUMN IF NOT EXISTS negotiating_reviewer_telegram_username VARCHAR(64),
    ADD COLUMN IF NOT EXISTS negotiating_reviewer_rocketchat_id VARCHAR(100),
    ADD COLUMN IF NOT EXISTS negotiating_reviewer_alternative_contact VARCHAR(255),
    ADD COLUMN IF NOT EXISTS negotiating_started_at TIMESTAMPTZ;
