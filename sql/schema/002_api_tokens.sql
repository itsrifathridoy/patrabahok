-- Local API auth tokens for patrabahokd (see cli/internal/authtoken).
-- Tokens themselves are never stored — only a SHA-256 hash of the token value.

CREATE TABLE IF NOT EXISTS api_tokens (
  id            INT UNSIGNED NOT NULL AUTO_INCREMENT,
  name          VARCHAR(100) NOT NULL,
  token_hash    CHAR(64) NOT NULL COMMENT 'SHA-256 hex digest of the bearer token',
  scopes        VARCHAR(255) NOT NULL DEFAULT '*' COMMENT 'comma-separated scopes, or *',
  created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_used_at  TIMESTAMP NULL DEFAULT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_api_tokens_hash (token_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO schema_migrations (version) VALUES (2);
