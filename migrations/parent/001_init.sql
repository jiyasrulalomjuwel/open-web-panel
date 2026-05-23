-- Parent Panel Database Schema

CREATE TABLE IF NOT EXISTS packages (
    id              INT AUTO_INCREMENT PRIMARY KEY,
    name            VARCHAR(100) NOT NULL UNIQUE,
    disk_mb         INT NOT NULL DEFAULT 1000,
    bandwidth_mb    INT NOT NULL DEFAULT 10000,
    max_db          INT NOT NULL DEFAULT 5,
    max_email       INT NOT NULL DEFAULT 10,
    max_ftp         INT NOT NULL DEFAULT 5,
    max_domains     INT NOT NULL DEFAULT 3,
    max_subdomains  INT NOT NULL DEFAULT 10,
    ssh_access      BOOLEAN NOT NULL DEFAULT FALSE,
    backup_enabled  BOOLEAN NOT NULL DEFAULT TRUE,
    is_default      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS admins (
    id              INT AUTO_INCREMENT PRIMARY KEY,
    username        VARCHAR(64) NOT NULL UNIQUE,
    password_hash   VARCHAR(255) NOT NULL,
    role            ENUM('root','admin','support') NOT NULL DEFAULT 'admin',
    totp_secret     VARCHAR(64) DEFAULT NULL,
    last_login_at   TIMESTAMP NULL,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS resellers (
    id              INT AUTO_INCREMENT PRIMARY KEY,
    account_id      INT NOT NULL UNIQUE,
    max_accounts    INT NOT NULL DEFAULT 10,
    can_create_packages BOOLEAN DEFAULT FALSE,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS accounts (
    id                  INT AUTO_INCREMENT PRIMARY KEY,
    username            VARCHAR(32) NOT NULL UNIQUE,
    domain              VARCHAR(255) NOT NULL,
    email               VARCHAR(255) NOT NULL,
    password_hash       VARCHAR(255) NOT NULL,
    package_id          INT NOT NULL,
    reseller_id         INT DEFAULT NULL,
    status              ENUM('active','suspended','pending','terminated') DEFAULT 'pending',
    home_dir            VARCHAR(512) NOT NULL,
    ip_address          VARCHAR(45) DEFAULT NULL,
    disk_used_mb        INT DEFAULT 0,
    bandwidth_used_mb   INT DEFAULT 0,
    suspended_reason    TEXT,
    created_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (package_id) REFERENCES packages(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS audit_log (
    id          BIGINT AUTO_INCREMENT PRIMARY KEY,
    actor_type  ENUM('admin','reseller','account','system') NOT NULL,
    actor_id    INT NOT NULL,
    action      VARCHAR(255) NOT NULL,
    target_type VARCHAR(100) DEFAULT NULL,
    target_id   INT DEFAULT NULL,
    details     JSON DEFAULT NULL,
    ip_address  VARCHAR(45) DEFAULT NULL,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id          INT AUTO_INCREMENT PRIMARY KEY,
    user_id     INT NOT NULL,
    token_hash  VARCHAR(255) NOT NULL UNIQUE,
    scope       ENUM('parent','child') NOT NULL,
    expires_at  TIMESTAMP NOT NULL,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS server_config (
    key_name    VARCHAR(128) PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Default package
INSERT INTO packages (name, is_default) VALUES ('default', TRUE)
ON DUPLICATE KEY UPDATE name = name;

-- Default root admin (password set via OWP_ADMIN_PASSWORD env var, default: admin123)
-- During automated installation, the password is randomly generated and stored in .env
INSERT INTO admins (username, password_hash, role)
VALUES ('admin', '$2a$12$LJ3m4ys3GZfnYMz8kVsKaOTSxL0OPhGJDAh0tXHE6C3vYQKQIXzO.', 'root')
ON DUPLICATE KEY UPDATE username = username;
