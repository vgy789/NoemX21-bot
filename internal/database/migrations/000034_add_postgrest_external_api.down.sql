DROP SCHEMA IF EXISTS api_v1 CASCADE;
DROP SCHEMA IF EXISTS api_private CASCADE;

ALTER TABLE api_keys RENAME TO api_keys_postgrest;

CREATE TABLE api_keys (
    id BIGSERIAL PRIMARY KEY,
    user_account_id BIGINT NOT NULL REFERENCES user_accounts(id) ON DELETE CASCADE,
    key_hash VARCHAR(64) NOT NULL,
    prefix VARCHAR(32) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ
);

INSERT INTO api_keys (
    id,
    user_account_id,
    key_hash,
    prefix,
    created_at,
    revoked_at,
    expires_at
)
SELECT
    ak.id,
    ap.user_account_id,
    ak.key_hash,
    ak.prefix,
    ak.created_at,
    ak.revoked_at,
    ak.expires_at
FROM api_keys_postgrest ak
JOIN api_principals ap ON ap.id = ak.api_principal_id
WHERE ap.user_account_id IS NOT NULL;

SELECT setval(pg_get_serial_sequence('api_keys', 'id'), COALESCE((SELECT MAX(id) FROM api_keys), 1), true);

DROP TABLE api_keys_postgrest;
DROP TABLE IF EXISTS api_principals;
DROP TYPE IF EXISTS enum_api_principal_kind;

CREATE INDEX idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX idx_api_keys_user_account_id ON api_keys(user_account_id);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'api_user') THEN
        REVOKE ALL PRIVILEGES ON SCHEMA public FROM api_user;
        DROP ROLE api_user;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'api_anon') THEN
        REVOKE ALL PRIVILEGES ON SCHEMA public FROM api_anon;
        DROP ROLE api_anon;
    END IF;
EXCEPTION
    WHEN dependent_objects_still_exist THEN
        NULL;
END $$;
