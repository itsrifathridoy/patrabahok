-- patrabahok base schema: virtual domains/users/aliases for Postfix + Dovecot.
-- Applied by lib/phases/30-database.sh via a small migration runner.

CREATE TABLE IF NOT EXISTS schema_migrations (
  version      INT UNSIGNED NOT NULL PRIMARY KEY,
  applied_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS virtual_domains (
  id           INT UNSIGNED NOT NULL AUTO_INCREMENT,
  name         VARCHAR(255) NOT NULL,
  created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_virtual_domains_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS virtual_users (
  id            INT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id     INT UNSIGNED NOT NULL,
  email         VARCHAR(255) NOT NULL,
  password      VARCHAR(255) NOT NULL COMMENT 'Dovecot {SCHEME}hash, e.g. {SHA512-CRYPT}...',
  maildir       VARCHAR(255) NOT NULL COMMENT 'relative path under the vmail home, e.g. example.com/user/',
  quota_bytes   BIGINT UNSIGNED NOT NULL DEFAULT 1073741824,
  enabled       TINYINT(1) NOT NULL DEFAULT 1,
  created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_virtual_users_email (email),
  KEY idx_virtual_users_domain (domain_id),
  CONSTRAINT fk_virtual_users_domain FOREIGN KEY (domain_id)
    REFERENCES virtual_domains (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS virtual_aliases (
  id            INT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id     INT UNSIGNED NOT NULL,
  source        VARCHAR(255) NOT NULL,
  destination   VARCHAR(255) NOT NULL,
  created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_virtual_aliases_source_dest (source, destination),
  KEY idx_virtual_aliases_domain (domain_id),
  CONSTRAINT fk_virtual_aliases_domain FOREIGN KEY (domain_id)
    REFERENCES virtual_domains (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO schema_migrations (version) VALUES (1);
