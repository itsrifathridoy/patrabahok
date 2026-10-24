-- Public per-domain API (see cli/internal/publicapi): one API key per domain, scoped to
-- managing that domain's mailboxes/aliases only. Like api_tokens, only a SHA-256 hash of
-- the key is stored — the plaintext is shown once, when an admin generates it.
-- Also adds a display name to mailboxes, settable through that API.

ALTER TABLE virtual_users
  ADD COLUMN IF NOT EXISTS display_name VARCHAR(255) NOT NULL DEFAULT '' AFTER email;

CREATE TABLE IF NOT EXISTS domain_api_keys (
  domain_id     INT UNSIGNED NOT NULL,
  key_hash      CHAR(64) NOT NULL COMMENT 'SHA-256 hex digest of the API key',
  key_hint      VARCHAR(16) NOT NULL COMMENT 'last few characters, for identifying a key in the UI',
  created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_used_at  TIMESTAMP NULL DEFAULT NULL,
  PRIMARY KEY (domain_id),
  UNIQUE KEY uq_domain_api_keys_hash (key_hash),
  CONSTRAINT fk_domain_api_keys_domain FOREIGN KEY (domain_id)
    REFERENCES virtual_domains (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO schema_migrations (version) VALUES (6);
