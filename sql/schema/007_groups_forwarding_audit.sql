-- Workspace-style mail features (see cli/internal/mailbox/groups.go, forwards.go and
-- cli/internal/audit):
--   * groups: one address that delivers to every member, with a posting policy enforced
--     by patrabahokd's Postfix policy service (cli/internal/policy)
--   * per-mailbox forwarding, optionally keeping a copy
--   * an audit log of administrative changes
-- Postfix expands groups and forwards through templates/postfix/mysql-virtual-alias-maps.cf.tmpl.

CREATE TABLE IF NOT EXISTS mail_groups (
  id              INT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id       INT UNSIGNED NOT NULL,
  email           VARCHAR(255) NOT NULL,
  name            VARCHAR(255) NOT NULL,
  description     VARCHAR(1000) NOT NULL DEFAULT '',
  post_policy     ENUM('anyone','organization','members','owners') NOT NULL DEFAULT 'organization'
                  COMMENT 'who may send to the group, enforced at SMTP RCPT time',
  allow_external  TINYINT(1) NOT NULL DEFAULT 0 COMMENT 'whether addresses outside hosted domains may be members',
  created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_mail_groups_email (email),
  KEY idx_mail_groups_domain (domain_id),
  CONSTRAINT fk_mail_groups_domain FOREIGN KEY (domain_id)
    REFERENCES virtual_domains (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS mail_group_members (
  group_id    INT UNSIGNED NOT NULL,
  member      VARCHAR(255) NOT NULL COMMENT 'any address: a mailbox, another group, or (if allowed) an external address',
  role        ENUM('member','owner') NOT NULL DEFAULT 'member',
  created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (group_id, member),
  KEY idx_mail_group_members_member (member),
  CONSTRAINT fk_mail_group_members_group FOREIGN KEY (group_id)
    REFERENCES mail_groups (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS mailbox_forwards (
  email        VARCHAR(255) NOT NULL,
  destination  VARCHAR(255) NOT NULL,
  keep_copy    TINYINT(1) NOT NULL DEFAULT 1,
  created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (email),
  CONSTRAINT fk_mailbox_forwards_user FOREIGN KEY (email)
    REFERENCES virtual_users (email) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS audit_log (
  id      BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  actor   VARCHAR(255) NOT NULL COMMENT 'admin:<username>, cli, token:<name>, or apikey:<domain>',
  action  VARCHAR(64) NOT NULL,
  target  VARCHAR(255) NOT NULL DEFAULT '',
  detail  VARCHAR(1000) NOT NULL DEFAULT '',
  ip      VARCHAR(64) NOT NULL DEFAULT '',
  PRIMARY KEY (id),
  KEY idx_audit_log_at (at),
  KEY idx_audit_log_target (target)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO schema_migrations (version) VALUES (7);
