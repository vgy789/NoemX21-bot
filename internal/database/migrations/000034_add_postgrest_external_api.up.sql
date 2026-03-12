CREATE EXTENSION IF NOT EXISTS pgcrypto;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'api_anon') THEN
        CREATE ROLE api_anon NOLOGIN;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'api_user') THEN
        CREATE ROLE api_user NOLOGIN;
    END IF;
END $$;

DO $$
BEGIN
    EXECUTE format('GRANT api_anon TO %I', current_user);
    EXECUTE format('GRANT api_user TO %I', current_user);
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_type
        WHERE typname = 'enum_api_principal_kind'
    ) THEN
        CREATE TYPE enum_api_principal_kind AS ENUM ('personal', 'service');
    END IF;
END $$;

ALTER TABLE api_keys RENAME TO api_keys_legacy;

CREATE TABLE api_principals (
    id BIGSERIAL PRIMARY KEY,
    kind enum_api_principal_kind NOT NULL,
    display_name VARCHAR(255) NOT NULL,
    telegram_user_id BIGINT,
    user_account_id BIGINT UNIQUE REFERENCES user_accounts(id) ON DELETE CASCADE,
    campus_id UUID REFERENCES campuses(id) ON DELETE SET NULL,
    scopes TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    allow_login_exposure BOOLEAN NOT NULL DEFAULT false,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (
        (kind = 'personal' AND user_account_id IS NOT NULL)
        OR (kind = 'service' AND user_account_id IS NULL)
    )
);

INSERT INTO api_principals (
    kind,
    display_name,
    telegram_user_id,
    user_account_id,
    scopes,
    allow_login_exposure,
    is_active,
    created_at,
    updated_at
)
SELECT
    'personal'::enum_api_principal_kind,
    'Personal key for ' || ua.s21_login,
    CASE
        WHEN ua.platform = 'telegram' AND ua.external_id ~ '^[0-9]+$' THEN ua.external_id::BIGINT
        ELSE NULL
    END,
    ua.id,
    ARRAY['self.read']::TEXT[],
    false,
    true,
    MIN(ak.created_at),
    CURRENT_TIMESTAMP
FROM api_keys_legacy ak
JOIN user_accounts ua ON ua.id = ak.user_account_id
GROUP BY ua.id, ua.s21_login, ua.platform, ua.external_id;

CREATE TABLE api_keys (
    id BIGSERIAL PRIMARY KEY,
    api_principal_id BIGINT NOT NULL REFERENCES api_principals(id) ON DELETE CASCADE,
    key_hash VARCHAR(64) NOT NULL,
    prefix VARCHAR(32) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ
);

INSERT INTO api_keys (
    id,
    api_principal_id,
    key_hash,
    prefix,
    created_at,
    revoked_at,
    expires_at,
    last_used_at
)
SELECT
    ak.id,
    ap.id,
    ak.key_hash,
    ak.prefix,
    ak.created_at,
    ak.revoked_at,
    ak.expires_at,
    NULL
FROM api_keys_legacy ak
JOIN api_principals ap ON ap.user_account_id = ak.user_account_id;

SELECT setval(pg_get_serial_sequence('api_keys', 'id'), COALESCE((SELECT MAX(id) FROM api_keys), 1), true);
SELECT setval(pg_get_serial_sequence('api_principals', 'id'), COALESCE((SELECT MAX(id) FROM api_principals), 1), true);

DROP TABLE api_keys_legacy;

CREATE INDEX idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX idx_api_keys_api_principal_id ON api_keys(api_principal_id);
CREATE INDEX idx_api_principals_kind ON api_principals(kind);
CREATE INDEX idx_api_principals_campus_id ON api_principals(campus_id);

CREATE SCHEMA IF NOT EXISTS api_private;
CREATE SCHEMA IF NOT EXISTS api_v1;

REVOKE ALL ON SCHEMA api_private FROM PUBLIC;
REVOKE ALL ON SCHEMA api_v1 FROM PUBLIC;
GRANT USAGE ON SCHEMA api_private TO api_user;
GRANT USAGE ON SCHEMA api_v1 TO api_anon, api_user;

CREATE OR REPLACE FUNCTION api_private.base64url_encode(data BYTEA)
RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
AS $$
    SELECT replace(replace(replace(trim(trailing '=' FROM encode(data, 'base64')), '+', '-'), '/', '_'), E'\n', '');
$$;

CREATE OR REPLACE FUNCTION api_private.request_claims()
RETURNS JSONB
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(NULLIF(current_setting('request.jwt.claims', true), ''), '{}')::JSONB;
$$;

CREATE OR REPLACE FUNCTION api_private.current_principal_id()
RETURNS BIGINT
LANGUAGE SQL
STABLE
AS $$
    SELECT NULLIF(api_private.request_claims()->>'principal_id', '')::BIGINT;
$$;

CREATE OR REPLACE FUNCTION api_private.current_principal_kind()
RETURNS TEXT
LANGUAGE SQL
STABLE
AS $$
    SELECT NULLIF(api_private.request_claims()->>'principal_kind', '');
$$;

CREATE OR REPLACE FUNCTION api_private.current_campus_id()
RETURNS UUID
LANGUAGE SQL
STABLE
AS $$
    SELECT NULLIF(api_private.request_claims()->>'campus_id', '')::UUID;
$$;

CREATE OR REPLACE FUNCTION api_private.jwt_signing_secret()
RETURNS TEXT
LANGUAGE plpgsql
STABLE
AS $$
DECLARE
    secret TEXT;
BEGIN
    secret := NULLIF(current_setting('app.settings.jwt_secret', true), '');
    IF secret IS NULL THEN
        RAISE EXCEPTION 'app.settings.jwt_secret is not configured'
            USING ERRCODE = '55000';
    END IF;

    RETURN secret;
END;
$$;

CREATE OR REPLACE FUNCTION api_private.sign_jwt(payload JSONB)
RETURNS TEXT
LANGUAGE plpgsql
STABLE
SET search_path = public, pg_temp
AS $$
DECLARE
    jwt_header TEXT := api_private.base64url_encode(convert_to('{"alg":"HS256","typ":"JWT"}', 'utf8'));
    jwt_payload TEXT := api_private.base64url_encode(convert_to(payload::TEXT, 'utf8'));
    jwt_signature TEXT;
BEGIN
    jwt_signature := api_private.base64url_encode(
        hmac(jwt_header || '.' || jwt_payload, api_private.jwt_signing_secret(), 'sha256')
    );

    RETURN jwt_header || '.' || jwt_payload || '.' || jwt_signature;
END;
$$;

CREATE OR REPLACE FUNCTION api_private.require_api_principal()
RETURNS api_principals
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
    principal api_principals;
BEGIN
    SELECT *
    INTO principal
    FROM api_principals
    WHERE id = api_private.current_principal_id();

    IF principal.id IS NULL OR principal.is_active IS NOT TRUE THEN
        RAISE EXCEPTION 'api principal is missing or inactive'
            USING ERRCODE = '28000';
    END IF;

    RETURN principal;
END;
$$;

CREATE OR REPLACE FUNCTION api_private.pre_request()
RETURNS VOID
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
    principal api_principals;
BEGIN
    IF current_user <> 'api_user' THEN
        RETURN;
    END IF;

    principal := api_private.require_api_principal();

    IF api_private.current_principal_kind() IS NOT NULL
       AND api_private.current_principal_kind() <> principal.kind::TEXT THEN
        RAISE EXCEPTION 'jwt principal kind mismatch'
            USING ERRCODE = '28000';
    END IF;

    IF principal.campus_id IS DISTINCT FROM api_private.current_campus_id() THEN
        RAISE EXCEPTION 'jwt campus_id mismatch'
            USING ERRCODE = '28000';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION api_private.check_registration_internal(
    p_external_id TEXT,
    p_login TEXT,
    p_platform TEXT DEFAULT 'telegram'
)
RETURNS BOOLEAN
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
    normalized_platform enum_platform;
BEGIN
    IF COALESCE(trim(p_external_id), '') = '' OR COALESCE(trim(p_login), '') = '' THEN
        RETURN false;
    END IF;

    CASE lower(COALESCE(p_platform, 'telegram'))
        WHEN 'telegram' THEN normalized_platform := 'telegram'::enum_platform;
        WHEN 'rocketchat' THEN normalized_platform := 'rocketchat'::enum_platform;
        ELSE
            RETURN false;
    END CASE;

    RETURN EXISTS (
        SELECT 1
        FROM user_accounts ua
        WHERE ua.platform = normalized_platform
          AND ua.external_id = p_external_id
          AND lower(ua.s21_login) = lower(p_login)
    );
END;
$$;

CREATE OR REPLACE FUNCTION api_private.exchange_api_key_internal(p_api_key TEXT)
RETURNS TABLE (
    access_token TEXT,
    token_type TEXT,
    expires_in INTEGER,
    principal_id BIGINT,
    principal_kind TEXT,
    scopes TEXT[],
    campus_id UUID
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
    key_row api_keys;
    principal api_principals;
    lookup_key_hash TEXT;
    issued_at TIMESTAMPTZ := clock_timestamp();
    expires_at TIMESTAMPTZ := issued_at + INTERVAL '1 hour';
    payload JSONB;
BEGIN
    IF p_api_key IS NULL OR p_api_key !~ '^noemx_sk_[0-9a-f]{64}$' THEN
        RAISE EXCEPTION 'invalid api key'
            USING ERRCODE = '28000';
    END IF;

    lookup_key_hash := encode(digest(p_api_key, 'sha256'), 'hex');

    SELECT ak.*
    INTO key_row
    FROM api_keys ak
    JOIN api_principals ap ON ap.id = ak.api_principal_id
    WHERE ak.key_hash = lookup_key_hash
      AND ak.revoked_at IS NULL
      AND (ak.expires_at IS NULL OR ak.expires_at > issued_at)
      AND ap.is_active = true;

    IF key_row.id IS NULL THEN
        RAISE EXCEPTION 'invalid api key'
            USING ERRCODE = '28000';
    END IF;

    SELECT *
    INTO principal
    FROM api_principals
    WHERE id = key_row.api_principal_id;

    UPDATE api_keys
    SET last_used_at = issued_at
    WHERE id = key_row.id;

    payload := jsonb_build_object(
        'role', 'api_user',
        'sub', principal.id::TEXT,
        'principal_id', principal.id,
        'principal_kind', principal.kind,
        'campus_id', principal.campus_id,
        'scopes', to_jsonb(principal.scopes),
        'allow_login_exposure', principal.allow_login_exposure,
        'iat', floor(extract(epoch FROM issued_at)),
        'exp', floor(extract(epoch FROM expires_at))
    );

    RETURN QUERY
    SELECT
        api_private.sign_jwt(payload),
        'Bearer'::TEXT,
        3600,
        principal.id,
        principal.kind::TEXT,
        principal.scopes,
        principal.campus_id;
END;
$$;

CREATE OR REPLACE FUNCTION api_private.create_service_key(
    p_display_name TEXT,
    p_campus_id UUID,
    p_scopes TEXT[] DEFAULT ARRAY['campus.stats.read', 'campus.logins.read', 'registration.check']::TEXT[],
    p_allow_login_exposure BOOLEAN DEFAULT false,
    p_expires_at TIMESTAMPTZ DEFAULT NULL
)
RETURNS TABLE (
    principal_id BIGINT,
    key_prefix TEXT,
    raw_key TEXT,
    scopes TEXT[],
    campus_id UUID,
    allow_login_exposure BOOLEAN
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
    random_part TEXT;
    generated_key TEXT;
    key_hash TEXT;
    created_principal api_principals;
BEGIN
    IF COALESCE(trim(p_display_name), '') = '' THEN
        RAISE EXCEPTION 'display_name is required'
            USING ERRCODE = '22023';
    END IF;

    IF p_campus_id IS NULL THEN
        RAISE EXCEPTION 'campus_id is required'
            USING ERRCODE = '22023';
    END IF;

    IF p_scopes IS NULL OR array_length(p_scopes, 1) IS NULL THEN
        RAISE EXCEPTION 'at least one scope is required'
            USING ERRCODE = '22023';
    END IF;

    INSERT INTO api_principals (
        kind,
        display_name,
        campus_id,
        scopes,
        allow_login_exposure,
        is_active
    )
    VALUES (
        'service',
        p_display_name,
        p_campus_id,
        p_scopes,
        p_allow_login_exposure,
        true
    )
    RETURNING *
    INTO created_principal;

    random_part := encode(gen_random_bytes(32), 'hex');
    generated_key := 'noemx_sk_' || random_part;
    key_hash := encode(digest(generated_key, 'sha256'), 'hex');

    INSERT INTO api_keys (
        api_principal_id,
        key_hash,
        prefix,
        expires_at
    )
    VALUES (
        created_principal.id,
        key_hash,
        'noemx_sk_' || substr(random_part, 1, 4),
        p_expires_at
    );

    RETURN QUERY
    SELECT
        created_principal.id,
        'noemx_sk_' || substr(random_part, 1, 4),
        generated_key,
        created_principal.scopes,
        created_principal.campus_id,
        created_principal.allow_login_exposure;
END;
$$;

CREATE OR REPLACE FUNCTION api_v1.exchange_api_key(api_key TEXT)
RETURNS TABLE (
    access_token TEXT,
    token_type TEXT,
    expires_in INTEGER,
    principal_id BIGINT,
    principal_kind TEXT,
    scopes TEXT[],
    campus_id UUID
)
LANGUAGE SQL
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
    SELECT *
    FROM api_private.exchange_api_key_internal(api_key);
$$;

CREATE OR REPLACE FUNCTION api_v1.check_registration(
    external_id TEXT,
    login TEXT,
    platform TEXT DEFAULT 'telegram'
)
RETURNS TABLE (registered BOOLEAN)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
    principal api_principals;
BEGIN
    principal := api_private.require_api_principal();

    IF NOT principal.scopes @> ARRAY['registration.check']::TEXT[] THEN
        RAISE EXCEPTION 'missing registration.check scope'
            USING ERRCODE = '42501';
    END IF;

    RETURN QUERY
    SELECT api_private.check_registration_internal(external_id, login, platform);
END;
$$;

CREATE OR REPLACE VIEW api_v1.me_book_loans
WITH (security_barrier = true)
AS
SELECT
    bl.campus_id,
    c.short_name AS campus_short_name,
    bl.book_id,
    b.title AS book_title,
    b.author AS book_author,
    bl.borrowed_at,
    bl.due_at,
    bl.returned_at
FROM book_loans bl
JOIN books b ON b.campus_id = bl.campus_id AND b.id = bl.book_id
JOIN campuses c ON c.id = bl.campus_id
JOIN api_principals ap ON ap.user_account_id = bl.user_id
WHERE ap.id = api_private.current_principal_id()
  AND ap.kind = 'personal'
  AND ap.is_active = true
  AND ap.scopes @> ARRAY['self.read']::TEXT[];

CREATE OR REPLACE VIEW api_v1.me_room_bookings
WITH (security_barrier = true)
AS
SELECT
    rb.campus_id,
    c.short_name AS campus_short_name,
    rb.room_id,
    r.name AS room_name,
    rb.booking_date,
    rb.start_time,
    rb.duration_minutes,
    rb.created_at
FROM room_bookings rb
JOIN rooms r ON r.campus_id = rb.campus_id AND r.id = rb.room_id
JOIN campuses c ON c.id = rb.campus_id
JOIN api_principals ap ON ap.user_account_id = rb.user_id
WHERE ap.id = api_private.current_principal_id()
  AND ap.kind = 'personal'
  AND ap.is_active = true
  AND ap.scopes @> ARRAY['self.read']::TEXT[];

CREATE OR REPLACE VIEW api_v1.campus_book_loans
WITH (security_barrier = true)
AS
SELECT
    bl.campus_id,
    c.short_name AS campus_short_name,
    bl.book_id,
    b.title AS book_title,
    b.author AS book_author,
    ua.s21_login,
    bl.borrowed_at,
    bl.due_at,
    bl.returned_at
FROM book_loans bl
JOIN books b ON b.campus_id = bl.campus_id AND b.id = bl.book_id
JOIN campuses c ON c.id = bl.campus_id
JOIN user_accounts ua ON ua.id = bl.user_id
JOIN api_principals ap ON ap.campus_id = bl.campus_id
WHERE ap.id = api_private.current_principal_id()
  AND ap.kind = 'service'
  AND ap.is_active = true
  AND ap.allow_login_exposure = true
  AND ap.scopes @> ARRAY['campus.logins.read']::TEXT[];

CREATE OR REPLACE VIEW api_v1.campus_room_bookings
WITH (security_barrier = true)
AS
SELECT
    rb.campus_id,
    c.short_name AS campus_short_name,
    rb.room_id,
    r.name AS room_name,
    rb.booking_date,
    rb.start_time,
    rb.duration_minutes,
    ua.s21_login
FROM room_bookings rb
JOIN rooms r ON r.campus_id = rb.campus_id AND r.id = rb.room_id
JOIN campuses c ON c.id = rb.campus_id
JOIN user_accounts ua ON ua.id = rb.user_id
JOIN api_principals ap ON ap.campus_id = rb.campus_id
WHERE ap.id = api_private.current_principal_id()
  AND ap.kind = 'service'
  AND ap.is_active = true
  AND ap.allow_login_exposure = true
  AND ap.scopes @> ARRAY['campus.logins.read']::TEXT[];

CREATE OR REPLACE VIEW api_v1.campus_book_loan_daily_stats
WITH (security_barrier = true)
AS
WITH scoped_loans AS (
    SELECT
        bl.campus_id,
        c.short_name AS campus_short_name,
        COALESCE(c.timezone, 'UTC') AS campus_timezone,
        bl.user_id,
        bl.borrowed_at,
        bl.returned_at
    FROM book_loans bl
    JOIN campuses c ON c.id = bl.campus_id
    JOIN api_principals ap ON ap.campus_id = bl.campus_id
    WHERE ap.id = api_private.current_principal_id()
      AND ap.kind = 'service'
      AND ap.is_active = true
      AND ap.scopes @> ARRAY['campus.stats.read']::TEXT[]
),
events AS (
    SELECT
        campus_id,
        campus_short_name,
        (borrowed_at AT TIME ZONE campus_timezone)::DATE AS stat_date,
        1 AS loans_started,
        0 AS loans_returned,
        user_id
    FROM scoped_loans
    UNION ALL
    SELECT
        campus_id,
        campus_short_name,
        (returned_at AT TIME ZONE campus_timezone)::DATE AS stat_date,
        0 AS loans_started,
        1 AS loans_returned,
        user_id
    FROM scoped_loans
    WHERE returned_at IS NOT NULL
)
SELECT
    campus_id,
    campus_short_name,
    stat_date,
    SUM(loans_started)::INT AS loans_started,
    SUM(loans_returned)::INT AS loans_returned,
    COUNT(DISTINCT user_id)::INT AS unique_users
FROM events
GROUP BY campus_id, campus_short_name, stat_date;

CREATE OR REPLACE VIEW api_v1.campus_room_booking_daily_stats
WITH (security_barrier = true)
AS
SELECT
    rb.campus_id,
    c.short_name AS campus_short_name,
    rb.booking_date AS stat_date,
    COUNT(*)::INT AS booking_count,
    COUNT(DISTINCT rb.user_id)::INT AS unique_users,
    COUNT(DISTINCT rb.room_id)::INT AS unique_rooms,
    COALESCE(SUM(rb.duration_minutes), 0)::INT AS total_duration_minutes
FROM room_bookings rb
JOIN campuses c ON c.id = rb.campus_id
JOIN api_principals ap ON ap.campus_id = rb.campus_id
WHERE ap.id = api_private.current_principal_id()
  AND ap.kind = 'service'
  AND ap.is_active = true
  AND ap.scopes @> ARRAY['campus.stats.read']::TEXT[]
GROUP BY rb.campus_id, c.short_name, rb.booking_date;

GRANT EXECUTE ON FUNCTION api_v1.exchange_api_key(TEXT) TO api_anon, api_user;
GRANT EXECUTE ON FUNCTION api_v1.check_registration(TEXT, TEXT, TEXT) TO api_user;
GRANT EXECUTE ON FUNCTION api_private.create_service_key(TEXT, UUID, TEXT[], BOOLEAN, TIMESTAMPTZ) TO api_user;
GRANT SELECT ON ALL TABLES IN SCHEMA api_v1 TO api_user;
