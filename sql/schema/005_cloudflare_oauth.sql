-- Adds OAuth 2.0 Authorization Code support to cloudflare_settings (see
-- cli/internal/cloudflare/oauth.go), as an alternative to pasting a raw scoped API
-- token: the admin registers an OAuth client for their own Cloudflare account (Manage
-- Account > OAuth clients) and connects via a "Connect with Cloudflare" redirect
-- instead. Everything sensitive here is AES-GCM encrypted before storage, same as
-- api_token_encrypted (see cli/internal/secretkey) — the encryption key never enters
-- this database.

ALTER TABLE cloudflare_settings
  MODIFY COLUMN api_token_encrypted TEXT NULL,
  ADD COLUMN oauth_client_id VARCHAR(255) NULL,
  ADD COLUMN oauth_client_secret_encrypted TEXT NULL,
  ADD COLUMN oauth_access_token_encrypted TEXT NULL,
  ADD COLUMN oauth_refresh_token_encrypted TEXT NULL,
  ADD COLUMN oauth_expires_at DATETIME NULL;

INSERT IGNORE INTO schema_migrations (version) VALUES (5);
