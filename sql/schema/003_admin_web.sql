-- Admin web dashboard accounts and sessions (see cli/internal/adminauth).
-- Passwords are argon2id-hashed (a real KDF, unlike api_tokens' fast SHA-256 hash —
-- these ARE human-chosen, comparatively low-entropy secrets). Session tokens follow
-- the same pattern as api_tokens: only a SHA-256 hash of the bearer/cookie value is
-- stored, since the session token itself is already a high-entropy random value.

CREATE TABLE IF NOT EXISTS admin_users (
  id             INT UNSIGNED NOT NULL AUTO_INCREMENT,
  username       VARCHAR(64) NOT NULL,
  password_hash  VARCHAR(255) NOT NULL COMMENT 'argon2id, PHC-ish encoded',
  created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_login_at  TIMESTAMP NULL DEFAULT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_admin_users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS admin_sessions (
  id             INT UNSIGNED NOT NULL AUTO_INCREMENT,
  admin_user_id  INT UNSIGNED NOT NULL,
  token_hash     CHAR(64) NOT NULL COMMENT 'SHA-256 hex digest of the session cookie value',
  created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at     TIMESTAMP NOT NULL,
  ip_address     VARCHAR(64) DEFAULT NULL,
  user_agent     VARCHAR(255) DEFAULT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_admin_sessions_hash (token_hash),
  KEY idx_admin_sessions_user (admin_user_id),
  CONSTRAINT fk_admin_sessions_user FOREIGN KEY (admin_user_id)
    REFERENCES admin_users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO schema_migrations (version) VALUES (3);
