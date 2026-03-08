ALTER TABLE platform_credentials
    DROP COLUMN access_token_enc,
    DROP COLUMN access_nonce,
    ADD COLUMN access_token TEXT;
