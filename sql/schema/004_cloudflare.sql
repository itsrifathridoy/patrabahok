-- Optional connected Cloudflare account for one-click DNS record setup (see
-- cli/internal/cloudflare). Single-row table: id is always 1. The API token itself is
-- AES-GCM encrypted before it ever reaches this column — the encryption key lives in
-- /etc/patrabahok/secrets.env, not in the database (see cli/internal/secretkey) — so a
-- database-only compromise doesn't hand over live Cloudflare API access.

CREATE TABLE IF NOT EXISTS cloudflare_settings (
  id                    TINYINT UNSIGNED NOT NULL,
  api_token_encrypted   TEXT NOT NULL,
  connected_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO schema_migrations (version) VALUES (4);
