-- 001_init_schema.up.sql

CREATE TYPE ENUM_STUDENT_STATUS AS ENUM ('ACTIVE', 'TEMPORARY_BLOCKING', 'EXPELLED', 'BLOCKED', 'FROZEN', 'STUDY_COMPLETED');
CREATE TYPE ENUM_PLATFORM AS ENUM ('telegram', 'rocketchat');
CREATE TYPE ENUM_USER_ROLE AS ENUM ('user', 'admin', 'moderator', 'owner');

CREATE TABLE campuses (
    id UUID PRIMARY KEY,
    short_name VARCHAR(255) NOT NULL UNIQUE,
    full_name VARCHAR(255) NOT NULL,
    timezone VARCHAR(100),
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE coalitions (
    id SMALLINT PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE students (
    s21_login VARCHAR(21) PRIMARY KEY,
    rocketchat_id VARCHAR(100) NOT NULL UNIQUE,
    campus_id UUID REFERENCES campuses(id),
    coalition_id SMALLINT REFERENCES coalitions(id),
    status ENUM_STUDENT_STATUS DEFAULT 'ACTIVE',
    parallel_name VARCHAR(100),
    level INT DEFAULT 0,
    exp_value INT DEFAULT 0,
    prp INT DEFAULT 0,
    crp INT DEFAULT 0,
    coins INT DEFAULT 0,
    timezone VARCHAR(100) NOT NULL DEFAULT 'UTC',
    alternative_contact VARCHAR(42),
    has_coffee_ban BOOLEAN DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE user_accounts (
    id BIGSERIAL PRIMARY KEY,
    student_id VARCHAR(21) NOT NULL REFERENCES students(s21_login) ON DELETE CASCADE,
    platform ENUM_PLATFORM NOT NULL,
    external_id VARCHAR(255) NOT NULL,
    username VARCHAR(255),
    is_searchable BOOLEAN DEFAULT true,
    role ENUM_USER_ROLE DEFAULT 'user',
    linked_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(platform, external_id),
    UNIQUE(student_id, platform)
);

CREATE TABLE user_bot_settings (
    id BIGSERIAL PRIMARY KEY,
    user_account_id BIGINT NOT NULL UNIQUE REFERENCES user_accounts(id) ON DELETE CASCADE,
    language_code VARCHAR(3),
    notifications_enabled BOOLEAN DEFAULT true,
    review_post_campus_ids JSONB DEFAULT '[]',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE platform_credentials (
    student_id VARCHAR(21) PRIMARY KEY REFERENCES students(s21_login) ON DELETE CASCADE,
    password_enc BYTEA,
    password_nonce BYTEA,
    access_token TEXT,
    access_expires_at TIMESTAMP WITH TIME ZONE,
    refresh_token_enc BYTEA,
    refresh_nonce BYTEA,
    refresh_expires_at TIMESTAMP WITH TIME ZONE,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE rocketchat_credentials (
    student_id VARCHAR(21) PRIMARY KEY REFERENCES students(s21_login) ON DELETE CASCADE,
    rc_token_enc BYTEA,
    rc_nonce BYTEA,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE auth_verification_codes (
    id BIGSERIAL PRIMARY KEY,
    student_id VARCHAR(21) REFERENCES students(s21_login) ON DELETE CASCADE,
    code VARCHAR(10) NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
