ALTER TABLE platform_credentials
    DROP COLUMN access_token,
    ADD COLUMN access_token_enc BYTEA,
    ADD COLUMN access_nonce BYTEA;
