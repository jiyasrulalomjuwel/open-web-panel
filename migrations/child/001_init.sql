-- Child Panel Database Schema (one per account)

CREATE TABLE IF NOT EXISTS domains (
    id          INT AUTO_INCREMENT PRIMARY KEY,
    domain      VARCHAR(255) NOT NULL UNIQUE,
    type        ENUM('primary','addon','parked','subdomain') NOT NULL,
    parent_id   INT DEFAULT NULL,
    doc_root    VARCHAR(512) NOT NULL,
    ssl_enabled BOOLEAN DEFAULT FALSE,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS email_accounts (
    id              INT AUTO_INCREMENT PRIMARY KEY,
    email           VARCHAR(255) NOT NULL UNIQUE,
    domain_id       INT NOT NULL,
    password_hash   VARCHAR(255) NOT NULL,
    quota_mb        INT NOT NULL DEFAULT 500,
    status          ENUM('active','suspended') DEFAULT 'active',
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS user_databases (
    id                  INT AUTO_INCREMENT PRIMARY KEY,
    db_name             VARCHAR(64) NOT NULL UNIQUE,
    db_user             VARCHAR(64) NOT NULL,
    password_encrypted  TEXT NOT NULL,
    host                VARCHAR(255) DEFAULT 'localhost',
    size_mb             INT DEFAULT 0,
    created_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ftp_accounts (
    id              INT AUTO_INCREMENT PRIMARY KEY,
    username        VARCHAR(64) NOT NULL UNIQUE,
    password_hash   VARCHAR(255) NOT NULL,
    home_dir        VARCHAR(512) NOT NULL,
    quota_mb        INT NOT NULL DEFAULT 1000,
    status          ENUM('active','suspended') DEFAULT 'active',
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cron_jobs (
    id          INT AUTO_INCREMENT PRIMARY KEY,
    command     TEXT NOT NULL,
    schedule    VARCHAR(100) NOT NULL,
    description VARCHAR(255) DEFAULT NULL,
    enabled     BOOLEAN DEFAULT TRUE,
    last_run_at TIMESTAMP NULL,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS dns_records (
    id          INT AUTO_INCREMENT PRIMARY KEY,
    domain_id   INT NOT NULL,
    type        ENUM('A','AAAA','CNAME','MX','TXT','NS','SRV','CAA') NOT NULL,
    name        VARCHAR(255) NOT NULL,
    value       TEXT NOT NULL,
    priority    INT DEFAULT NULL,
    ttl         INT DEFAULT 3600,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS backups (
    id              INT AUTO_INCREMENT PRIMARY KEY,
    type            ENUM('full','partial','database','files') NOT NULL,
    status          ENUM('running','completed','failed') NOT NULL,
    path            VARCHAR(512) DEFAULT NULL,
    size_mb         INT DEFAULT NULL,
    started_at      TIMESTAMP NULL,
    completed_at    TIMESTAMP NULL,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ip_blocker (
    id          INT AUTO_INCREMENT PRIMARY KEY,
    ip_address  VARCHAR(45) NOT NULL,
    reason      VARCHAR(255) DEFAULT NULL,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS redirects (
    id          INT AUTO_INCREMENT PRIMARY KEY,
    domain_id   INT NOT NULL,
    source_path VARCHAR(512) NOT NULL,
    target_url  VARCHAR(1024) NOT NULL,
    code        INT DEFAULT 301,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
