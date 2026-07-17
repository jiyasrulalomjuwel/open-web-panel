package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	_ "crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	_ "github.com/mattn/go-sqlite3"

	"github.com/openwebcpanel/openwebcpanel/internal/shared/auth"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/captcha"
	sdb "github.com/openwebcpanel/openwebcpanel/internal/shared/db"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/filesystem"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/audit"
	)

// ---------- context key ----------
type contextKey string

const ClaimsKey contextKey = "claims"

func getClaims(r *http.Request) *auth.Claims {
	c, _ := r.Context().Value(ClaimsKey).(*auth.Claims)
	return c
}

func setClaims(r *http.Request, claims *auth.Claims) context.Context {
	return context.WithValue(r.Context(), ClaimsKey, claims)
}

// ---------- JSON helpers ----------

func jsonResp(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	jsonResp(w, status, map[string]string{"error": msg})
}

// ---------- Domain sanitization ----------

func sanitizeDomain(domain string) string {
	d := strings.ReplaceAll(domain, "..", "")
	d = strings.ReplaceAll(d, "/", "")
	d = strings.ReplaceAll(d, "\\", "")
	return d
}

// ---------- SQLite init ----------

func initDB(path string) (*sql.DB, error) {
	db, err := sdb.ConnectSQLite(path)
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")

	// Migrate existing databases — add columns if they don't exist yet.
	db.Exec("ALTER TABLE packages ADD COLUMN ram_limit_mb INTEGER NOT NULL DEFAULT 0")
		db.Exec("ALTER TABLE accounts ADD COLUMN ram_used_mb INTEGER DEFAULT 0")
		db.Exec("ALTER TABLE accounts ADD COLUMN ram_limit_mb INTEGER DEFAULT 0")
		db.Exec("ALTER TABLE error_pages ADD COLUMN action_type TEXT DEFAULT 'custom_html'")
		db.Exec("ALTER TABLE error_pages ADD COLUMN action_value TEXT DEFAULT ''")
		db.Exec("ALTER TABLE error_pages ADD COLUMN enabled INTEGER DEFAULT 1")
		db.Exec("ALTER TABLE error_pages ADD COLUMN last_triggered_at TEXT DEFAULT ''")
		db.Exec("ALTER TABLE error_pages ADD COLUMN hit_count INTEGER DEFAULT 0")
		db.Exec("ALTER TABLE error_pages ADD COLUMN custom_headers TEXT DEFAULT ''")
		db.Exec("ALTER TABLE error_pages ADD COLUMN custom_footer TEXT DEFAULT ''")
		db.Exec("ALTER TABLE error_pages ADD COLUMN seo_noindex INTEGER DEFAULT 0")
		db.Exec("ALTER TABLE error_pages ADD COLUMN seo_nofollow INTEGER DEFAULT 0")
		db.Exec("ALTER TABLE error_pages ADD COLUMN seo_canonical TEXT DEFAULT ''")
		db.Exec("ALTER TABLE error_pages ADD COLUMN template TEXT DEFAULT ''")
		db.Exec("ALTER TABLE error_pages ADD COLUMN language TEXT DEFAULT 'en'")
		db.Exec("ALTER TABLE error_pages ADD COLUMN updated_at TEXT DEFAULT ''")

	schema := `
	CREATE TABLE IF NOT EXISTS packages (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		name            TEXT NOT NULL UNIQUE,
		disk_mb         INTEGER NOT NULL DEFAULT 1000,
		bandwidth_mb    INTEGER NOT NULL DEFAULT 10000,
		max_db          INTEGER NOT NULL DEFAULT 5,
		max_email       INTEGER NOT NULL DEFAULT 10,
		max_ftp         INTEGER NOT NULL DEFAULT 5,
		max_domains     INTEGER NOT NULL DEFAULT 3,
		max_subdomains  INTEGER NOT NULL DEFAULT 10,
		ssh_access      INTEGER NOT NULL DEFAULT 0,
		backup_enabled  INTEGER NOT NULL DEFAULT 1,
		ram_limit_mb    INTEGER NOT NULL DEFAULT 0,
		is_default      INTEGER NOT NULL DEFAULT 0,
		created_at      TEXT DEFAULT (datetime('now')),
		updated_at      TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS admins (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		username        TEXT NOT NULL UNIQUE,
		password_hash   TEXT NOT NULL,
		role            TEXT NOT NULL DEFAULT 'admin' CHECK(role IN ('root','admin','support')),
		totp_secret     TEXT,
		last_login_at   TEXT,
		created_at      TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS accounts (
		id                  INTEGER PRIMARY KEY AUTOINCREMENT,
		username            TEXT NOT NULL UNIQUE,
		domain              TEXT NOT NULL,
		email               TEXT NOT NULL,
		password_hash       TEXT NOT NULL,
		package_id          INTEGER NOT NULL,
		reseller_id         INTEGER,
		status              TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('active','suspended','pending','terminated')),
		home_dir            TEXT NOT NULL,
		ip_address          TEXT,
		disk_used_mb        INTEGER DEFAULT 0,
		bandwidth_used_mb   INTEGER DEFAULT 0,
		ram_used_mb         INTEGER DEFAULT 0,
		ram_limit_mb        INTEGER DEFAULT 0,
		suspended_reason    TEXT,
		created_at          TEXT DEFAULT (datetime('now')),
		updated_at          TEXT DEFAULT (datetime('now')),
		FOREIGN KEY (package_id) REFERENCES packages(id)
	);

	CREATE TABLE IF NOT EXISTS audit_log (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		actor_type  TEXT NOT NULL CHECK(actor_type IN ('admin','reseller','account','system')),
		actor_id    INTEGER NOT NULL,
		action      TEXT NOT NULL,
		target_type TEXT,
		target_id   INTEGER,
		details     TEXT,
		ip_address  TEXT,
		created_at  TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS refresh_tokens (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id     INTEGER NOT NULL,
		token_hash  TEXT NOT NULL UNIQUE,
		scope       TEXT NOT NULL CHECK(scope IN ('parent','child')),
		expires_at  TEXT NOT NULL,
		created_at  TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS child_databases (
		id       INTEGER PRIMARY KEY AUTOINCREMENT,
		account_id INTEGER NOT NULL,
		db_name  TEXT NOT NULL,
		db_user  TEXT NOT NULL,
		host     TEXT DEFAULT 'localhost',
		size_mb  INTEGER DEFAULT 0,
		remote_access TEXT DEFAULT 'localhost',
		created_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS db_users (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		account_id  INTEGER NOT NULL,
		username    TEXT NOT NULL,
		password    TEXT NOT NULL,
		created_at  TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS db_user_assignments (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id     INTEGER NOT NULL,
		db_id       INTEGER NOT NULL,
		privileges  TEXT NOT NULL DEFAULT 'ALL PRIVILEGES',
		created_at  TEXT DEFAULT (datetime('now')),
		UNIQUE(user_id, db_id)
	);

	CREATE TABLE IF NOT EXISTS file_trash (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		account_id  INTEGER NOT NULL,
		original_path TEXT NOT NULL,
		trash_path   TEXT NOT NULL,
		size_bytes   INTEGER DEFAULT 0,
		is_dir       INTEGER DEFAULT 0,
		deleted_at   TEXT DEFAULT (datetime('now')),
		expires_at   TEXT DEFAULT (datetime('now', '+30 days'))
	);

	CREATE TABLE IF NOT EXISTS domains (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		account_id  INTEGER NOT NULL,
		domain      TEXT NOT NULL,
		type        TEXT NOT NULL DEFAULT 'addon' CHECK(type IN ('primary','addon','parked','subdomain')),
		parent_id   INTEGER,
		doc_root    TEXT NOT NULL,
		ssl_enabled INTEGER DEFAULT 0,
		created_at  TEXT DEFAULT (datetime('now'))
	);

		CREATE TABLE IF NOT EXISTS support_tickets (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id  INTEGER NOT NULL,
			subject     TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','closed','replied','pending')),
			created_at  TEXT DEFAULT (datetime('now')),
			updated_at  TEXT DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_tickets_account_id ON support_tickets(account_id);
		CREATE INDEX IF NOT EXISTS idx_tickets_updated_at ON support_tickets(updated_at DESC);
		CREATE INDEX IF NOT EXISTS idx_tickets_status ON support_tickets(status);

		CREATE TABLE IF NOT EXISTS ticket_messages (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			ticket_id   INTEGER NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
			sender_type TEXT NOT NULL CHECK(sender_type IN ('user','admin')),
			sender_id   INTEGER NOT NULL,
			message     TEXT NOT NULL,
			created_at  TEXT DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_messages_ticket_id ON ticket_messages(ticket_id);

		CREATE TABLE IF NOT EXISTS server_config (
			key_name    TEXT PRIMARY KEY,
			value       TEXT NOT NULL,
			updated_at  TEXT DEFAULT (datetime('now'))
		);

	CREATE TABLE IF NOT EXISTS pma_tokens (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		token_hash   TEXT NOT NULL UNIQUE,
		account_id   INTEGER NOT NULL,
		db_name      TEXT NOT NULL,
		db_user      TEXT NOT NULL,
		host         TEXT NOT NULL DEFAULT 'localhost',
		used         INTEGER NOT NULL DEFAULT 0,
		session_key  TEXT NOT NULL,
		created_at   TEXT DEFAULT (datetime('now')),
		expires_at   TEXT NOT NULL
	);

		CREATE TABLE IF NOT EXISTS bandwidth_logs (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id  INTEGER NOT NULL,
			bytes_in    INTEGER DEFAULT 0,
			bytes_out   INTEGER DEFAULT 0,
			logged_at   TEXT NOT NULL DEFAULT (date('now')),
			UNIQUE(account_id, logged_at)
		);

		CREATE TABLE IF NOT EXISTS notifications (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id  INTEGER,
			title       TEXT NOT NULL,
			message     TEXT NOT NULL,
			created_at  TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS notification_reads (
			notification_id INTEGER NOT NULL,
			account_id      INTEGER NOT NULL,
			read_at         TEXT DEFAULT (datetime('now')),
			PRIMARY KEY (notification_id, account_id)
		);

		CREATE TABLE IF NOT EXISTS ssl_certs (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id  INTEGER NOT NULL,
			domain_id   INTEGER NOT NULL,
			domain      TEXT NOT NULL,
			certificate TEXT,
			private_key TEXT,
			issuer      TEXT,
			expires_at  TEXT,
			auto_renew  INTEGER NOT NULL DEFAULT 1,
			status      TEXT NOT NULL DEFAULT 'pending',
			created_at  TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS cms_installs (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id      INTEGER NOT NULL,
			domain_id       INTEGER NOT NULL,
			domain          TEXT NOT NULL,
			cms_type        TEXT NOT NULL,
			version         TEXT,
			install_path    TEXT NOT NULL,
			install_url     TEXT NOT NULL,
			db_name         TEXT,
			db_user         TEXT,
			db_password     TEXT,
			admin_user      TEXT,
			admin_password  TEXT DEFAULT '',
			admin_email     TEXT DEFAULT '',
			admin_url       TEXT,
			site_name       TEXT DEFAULT '',
			site_description TEXT DEFAULT '',
			table_prefix    TEXT DEFAULT 'wp_',
			language        TEXT DEFAULT 'en_US',
			multisite       INTEGER DEFAULT 0,
			disable_cron    INTEGER DEFAULT 0,
			auto_upgrade    TEXT DEFAULT 'minor',
			protocol        TEXT DEFAULT 'http',
			install_subdir  TEXT DEFAULT '',
			plugins         TEXT DEFAULT '[]',
			status          TEXT NOT NULL DEFAULT 'pending',
			created_at      TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS email_accounts (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id      INTEGER NOT NULL,
			domain_id       INTEGER NOT NULL,
			email           TEXT NOT NULL UNIQUE,
			password_hash   TEXT NOT NULL,
			forward_to      TEXT DEFAULT '',
			quota_mb        INTEGER DEFAULT 100,
			send_limit      INTEGER DEFAULT 25,
			send_used       INTEGER DEFAULT 0,
			send_reset_date TEXT DEFAULT (date('now')),
			status          TEXT NOT NULL DEFAULT 'active',
			created_at      TEXT DEFAULT (datetime('now')),
			FOREIGN KEY (account_id) REFERENCES accounts(id),
			FOREIGN KEY (domain_id) REFERENCES domains(id)
		);

		CREATE TABLE IF NOT EXISTS email_messages (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			email_account_id INTEGER NOT NULL,
			folder           TEXT NOT NULL DEFAULT 'INBOX',
			from_addr        TEXT NOT NULL DEFAULT '',
			to_addr          TEXT NOT NULL DEFAULT '',
			subject          TEXT DEFAULT '',
			body_text        TEXT DEFAULT '',
			body_html        TEXT DEFAULT '',
			flags            TEXT DEFAULT '',
			message_id       TEXT,
			received_at      TEXT DEFAULT (datetime('now')),
			FOREIGN KEY (email_account_id) REFERENCES email_accounts(id)
		);

		CREATE TABLE IF NOT EXISTS form_submissions (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id  INTEGER NOT NULL,
			form_type   TEXT NOT NULL,
			metadata    TEXT,
			ip_address  TEXT,
			user_agent  TEXT,
			created_at  TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS cron_jobs (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id  INTEGER NOT NULL,
			command     TEXT NOT NULL,
			schedule    TEXT NOT NULL,
			description TEXT DEFAULT '',
			enabled     INTEGER NOT NULL DEFAULT 1,
			last_run_at TEXT DEFAULT '',
			created_at  TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS backups (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id      INTEGER NOT NULL,
			domain          TEXT NOT NULL,
			type            TEXT NOT NULL DEFAULT 'full' CHECK(type IN ('full','partial','database')),
			file_path       TEXT NOT NULL,
			file_size       INTEGER DEFAULT 0,
			status          TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','completed','failed')),
			backup_notes    TEXT DEFAULT '',
			created_at      TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS dns_records (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id  INTEGER NOT NULL,
			domain      TEXT NOT NULL,
			type        TEXT NOT NULL CHECK(type IN ('A','AAAA','CNAME','MX','TXT','NS','SRV','SOA')),
			name        TEXT NOT NULL,
			value       TEXT NOT NULL,
			priority    INTEGER DEFAULT 0,
			ttl         INTEGER DEFAULT 3600,
			enabled     INTEGER NOT NULL DEFAULT 1,
			created_at  TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS ftp_accounts (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id      INTEGER NOT NULL,
			username        TEXT NOT NULL,
			password_hash   TEXT NOT NULL,
			domain          TEXT NOT NULL,
			directory       TEXT NOT NULL,
			quota_mb        INTEGER DEFAULT 100,
			status          TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled','suspended','inactive')),
			created_at      TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS ssh_keys (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id      INTEGER NOT NULL,
			name            TEXT NOT NULL,
			public_key      TEXT NOT NULL,
			fingerprint     TEXT DEFAULT '',
			type            TEXT DEFAULT 'ssh-rsa',
			authorized      INTEGER NOT NULL DEFAULT 0,
			created_at      TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS api_tokens (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id      INTEGER NOT NULL,
			name            TEXT NOT NULL,
			token_hash      TEXT NOT NULL UNIQUE,
			token_prefix    TEXT NOT NULL,
			permissions     TEXT NOT NULL DEFAULT '[]',
			last_used_at    TEXT,
			expires_at      TEXT,
			enabled         INTEGER NOT NULL DEFAULT 1,
			created_at      TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS redirects (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id      INTEGER NOT NULL,
			domain_id       INTEGER,
			source_path     TEXT NOT NULL,
			target_url      TEXT NOT NULL,
			redirect_type   TEXT DEFAULT '301',
			status          TEXT DEFAULT 'active',
			created_at      TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS hotlink_protection (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id      INTEGER NOT NULL UNIQUE,
			enabled         INTEGER DEFAULT 0,
			allowed_domains TEXT DEFAULT '',
			created_at      TEXT DEFAULT (datetime('now')),
			updated_at      TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS error_pages (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id      INTEGER NOT NULL,
			domain_id       INTEGER NOT NULL,
			domain          TEXT NOT NULL,
			error_code      INTEGER NOT NULL,
			content         TEXT DEFAULT '',
			created_at      TEXT DEFAULT (datetime('now')),
			UNIQUE(domain_id, error_code)
		);

	CREATE TABLE IF NOT EXISTS login_attempts (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		username    TEXT NOT NULL,
		ip_address  TEXT NOT NULL,
		success     INTEGER NOT NULL DEFAULT 0,
		user_agent  TEXT DEFAULT '',
		created_at  TEXT DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_login_attempts_ip ON login_attempts(ip_address);
	CREATE INDEX IF NOT EXISTS idx_login_attempts_username ON login_attempts(username);

	CREATE TABLE IF NOT EXISTS captcha_sessions (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id  TEXT NOT NULL UNIQUE,
		answer      TEXT NOT NULL,
		expires_at  TEXT NOT NULL,
		used        INTEGER NOT NULL DEFAULT 0,
		created_at  TEXT DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_captcha_session_id ON captcha_sessions(session_id);
	CREATE INDEX IF NOT EXISTS idx_captcha_expires ON captcha_sessions(expires_at);

	CREATE TABLE IF NOT EXISTS blocked_ips (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		ip_address  TEXT NOT NULL UNIQUE,
		reason      TEXT DEFAULT '',
		blocked_by  TEXT DEFAULT 'system',
		failed_attempts INTEGER NOT NULL DEFAULT 0,
		created_at  TEXT DEFAULT (datetime('now')),
		updated_at  TEXT DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_blocked_ips_ip ON blocked_ips(ip_address);

	CREATE TABLE IF NOT EXISTS php_versions (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		version     TEXT NOT NULL UNIQUE,
		socket_path TEXT NOT NULL DEFAULT '',
		download_url TEXT NOT NULL DEFAULT '',
		status      TEXT NOT NULL DEFAULT 'not_installed' CHECK(status IN ('not_installed','downloaded','activated')),
		created_at  TEXT DEFAULT (datetime('now')),
		updated_at  TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS account_php_version (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		account_id      INTEGER NOT NULL UNIQUE,
		php_version_id  INTEGER NOT NULL,
		created_at      TEXT DEFAULT (datetime('now')),
		updated_at      TEXT DEFAULT (datetime('now')),
		FOREIGN KEY (account_id) REFERENCES accounts(id),
		FOREIGN KEY (php_version_id) REFERENCES php_versions(id)
	);
	`

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("run schema: %w", err)
	}

	// Schema migrations for new columns
	migrations := []string{
		"ALTER TABLE email_accounts ADD COLUMN forward_to TEXT DEFAULT ''",
		"ALTER TABLE email_accounts ADD COLUMN send_limit INTEGER DEFAULT 25",
		"ALTER TABLE email_accounts ADD COLUMN send_used INTEGER DEFAULT 0",
		"ALTER TABLE email_accounts ADD COLUMN send_reset_date TEXT DEFAULT (date('now'))",
		"ALTER TABLE cms_installs ADD COLUMN admin_email TEXT DEFAULT ''",
		"ALTER TABLE cms_installs ADD COLUMN admin_password TEXT DEFAULT ''",
		"ALTER TABLE cms_installs ADD COLUMN site_name TEXT DEFAULT ''",
		"ALTER TABLE cms_installs ADD COLUMN site_description TEXT DEFAULT ''",
		"ALTER TABLE cms_installs ADD COLUMN table_prefix TEXT DEFAULT 'wp_'",
		"ALTER TABLE cms_installs ADD COLUMN language TEXT DEFAULT 'en_US'",
		"ALTER TABLE cms_installs ADD COLUMN multisite INTEGER DEFAULT 0",
		"ALTER TABLE cms_installs ADD COLUMN disable_cron INTEGER DEFAULT 0",
		"ALTER TABLE cms_installs ADD COLUMN auto_upgrade TEXT DEFAULT 'minor'",
		"ALTER TABLE cms_installs ADD COLUMN protocol TEXT DEFAULT 'http'",
		"ALTER TABLE cms_installs ADD COLUMN install_subdir TEXT DEFAULT ''",
		"ALTER TABLE cms_installs ADD COLUMN plugins TEXT DEFAULT '[]'",
		// Store the MySQL password in pma_tokens so phpMyAdmin can authenticate
		// as the specific database user instead of the shared admin (owp_admin),
		// which would expose all databases on the server.
		"ALTER TABLE pma_tokens ADD COLUMN db_password TEXT DEFAULT ''",
		"ALTER TABLE login_attempts ADD COLUMN user_agent TEXT DEFAULT ''",
		}
	for _, m := range migrations {
		db.Exec(m)
	}

	// Migration: recreate php_versions with new CHECK constraint if old CHECK is active
	// SQLite cannot ALTER TABLE to change a CHECK constraint, so we recreate the table.
	var tblSQL string
	err = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='php_versions'").Scan(&tblSQL)
	if err == nil && !strings.Contains(tblSQL, "not_installed") {
		db.Exec(`CREATE TABLE IF NOT EXISTS php_versions_v2 (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			version     TEXT NOT NULL UNIQUE,
			socket_path TEXT NOT NULL DEFAULT '',
			download_url TEXT NOT NULL DEFAULT '',
			status      TEXT NOT NULL DEFAULT 'not_installed' CHECK(status IN ('not_installed','downloaded','activated')),
			created_at  TEXT DEFAULT (datetime('now')),
			updated_at  TEXT DEFAULT (datetime('now'))
		)`)
		db.Exec(`INSERT OR IGNORE INTO php_versions_v2 (id, version, socket_path, download_url, status, created_at, updated_at)
			SELECT id, version, socket_path, COALESCE(download_url, ''),
				CASE
					WHEN status = 'disabled' THEN 'not_installed'
					WHEN status = 'installed' THEN 'downloaded'
					WHEN status = 'enabled' THEN 'activated'
					ELSE 'not_installed'
				END,
				created_at, updated_at
			FROM php_versions`)
		db.Exec("DROP TABLE IF EXISTS php_versions_old")
		db.Exec("ALTER TABLE php_versions RENAME TO php_versions_old")
		db.Exec("ALTER TABLE php_versions_v2 RENAME TO php_versions")
		db.Exec("DROP TABLE IF EXISTS php_versions_old")
		log.Println("Migrated php_versions table to new CHECK constraint")
	}

	seedDefaultData(db)
	return db, nil
}

func seedDefaultData(db *sql.DB) {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM packages").Scan(&count)
	if count == 0 {
		db.Exec(`INSERT INTO packages (name, disk_mb, bandwidth_mb, ram_limit_mb, max_db, max_email, max_ftp, max_domains, max_subdomains, ssh_access, backup_enabled, is_default)
			VALUES ('default', 1000, 10000, 512, 5, 10, 5, 3, 10, 0, 1, 1)`)
		db.Exec(`INSERT INTO packages (name, disk_mb, bandwidth_mb, ram_limit_mb, max_db, max_email, max_ftp, max_domains, max_subdomains, ssh_access, backup_enabled)
			VALUES ('starter', 500, 5000, 256, 2, 5, 2, 1, 5, 0, 1)`)
		db.Exec(`INSERT INTO packages (name, disk_mb, bandwidth_mb, ram_limit_mb, max_db, max_email, max_ftp, max_domains, max_subdomains, ssh_access, backup_enabled)
			VALUES ('premium', 5000, 50000, 2048, 20, 50, 20, 10, 50, 1, 1)`)
		log.Println("Seeded 3 hosting packages")
	}

	db.QueryRow("SELECT COUNT(*) FROM admins").Scan(&count)
	if count == 0 {
		adminPass := os.Getenv("OWP_ADMIN_PASSWORD")
		if adminPass == "" {
			log.Fatal("OWP_ADMIN_PASSWORD environment variable must be set to a strong password")
		}
		if len(adminPass) < 8 {
			log.Fatal("OWP_ADMIN_PASSWORD must be at least 8 characters")
		}
		hash, err := auth.HashPassword(adminPass)
		if err != nil {
			log.Fatalf("Failed to hash admin password: %v", err)
		}
		db.Exec(`INSERT INTO admins (username, password_hash, role) VALUES ('admin', ?, 'root')`, hash)
	}

	// Seed default server config if empty
	var cfgCount int
	db.QueryRow("SELECT COUNT(*) FROM server_config").Scan(&cfgCount)
	if cfgCount == 0 {
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('suspend_auto_remove_days', '7')`)
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('default_upload_limit_mb', '2048')`)
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('max_upload_limit_mb', '5120')`)
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('site_name', 'OpenWebPanel')`)
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('smtp_relay_host', '')`)
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('smtp_relay_port', '587')`)
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('smtp_relay_username', '')`)
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('smtp_relay_password', '')`)
	}
	// Seed default test account for fast testing
	var accCount int
	db.QueryRow("SELECT COUNT(*) FROM accounts").Scan(&accCount)
	if accCount == 0 {
		hash, _ := auth.HashPassword("password")
		homeDir := getHomesBase() + "password"
		os.MkdirAll(homeDir+"/public_html", 0755)
		os.MkdirAll(homeDir+"/.owp", 0700)
		os.WriteFile(homeDir+"/public_html/index.html", []byte("<html><body><h1>Welcome</h1><p>Test site for password/password account</p></body></html>"), 0644)
		var pkgID int
		db.QueryRow("SELECT id FROM packages LIMIT 1").Scan(&pkgID)
		result, _ := db.Exec(`INSERT INTO accounts (username, domain, email, password_hash, package_id, status, home_dir) VALUES (?, ?, ?, ?, ?, 'active', ?)`, "password", "test.example.com", "test@example.com", hash, pkgID, homeDir)
		id, _ := result.LastInsertId()
		db.Exec(`INSERT INTO domains (account_id, domain, type, doc_root) VALUES (?, ?, 'primary', ?)`, id, "test.example.com", homeDir+"/public_html")
		log.Println("Seeded default test account: password / password")
	}

	// Migration: widen ftp_accounts status CHECK to include suspended/inactive
	// Check if old CHECK exists by attempting to insert a suspended status
	var oldCheck int
	db.QueryRow(`SELECT COUNT(*) FROM ftp_accounts WHERE status = 'suspended'`).Scan(&oldCheck)
	if oldCheck == 0 {
		// Old CHECK is still active; recreate the table
		db.Exec(`CREATE TABLE IF NOT EXISTS ftp_accounts_v2 (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id INTEGER NOT NULL,
			username TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			domain TEXT NOT NULL,
			directory TEXT NOT NULL,
			quota_mb INTEGER DEFAULT 100,
			status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled','suspended','inactive')),
			created_at TEXT DEFAULT (datetime('now'))
		)`)
		db.Exec(`INSERT OR IGNORE INTO ftp_accounts_v2 SELECT * FROM ftp_accounts`)
		db.Exec("DROP TABLE IF EXISTS ftp_accounts_old")
		db.Exec("ALTER TABLE ftp_accounts RENAME TO ftp_accounts_old")
		db.Exec("ALTER TABLE ftp_accounts_v2 RENAME TO ftp_accounts")
		db.Exec("DROP TABLE IF EXISTS ftp_accounts_old")
	}

	// Migration: add unique index on ftp_accounts(account_id, username) for race-safe dedup
	// Deduplicate any existing rows first to prevent CREATE UNIQUE INDEX from failing
	db.Exec(`DELETE FROM ftp_accounts WHERE id NOT IN (SELECT MIN(id) FROM ftp_accounts GROUP BY account_id, username)`)
	db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_ftp_accounts_uniq ON ftp_accounts(account_id, username)")

	// Seed default PHP versions (insert any missing ones)
	defaultVersions := []struct {
		version    string
		socketPath string
	}{
		{"8.2", "/run/php/php8.2-fpm.sock"},
		{"8.3", "/run/php/php8.3-fpm.sock"},
		{"8.4", "/run/php/php8.4-fpm.sock"},
		{"8.5", "/run/php/php8.5-fpm.sock"},
	}
	for _, pv := range defaultVersions {
		db.Exec(`INSERT OR IGNORE INTO php_versions (version, socket_path, status) VALUES (?, ?, 'not_installed')`,
			pv.version, pv.socketPath)
	}
}

// ---------- middleware ----------

func authMw(jwtManager *auth.JWTManager, requiredScope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := ""
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				parts := strings.SplitN(authHeader, " ", 2)
				if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
					tokenStr = parts[1]
				}
			}
					// NOTE: Query param token support removed for security.
			// Tokens in URLs can leak via Referer headers, server logs, and browser history.
			if tokenStr == "" {
				jsonError(w, 401, "missing authorization")
				return
			}
			claims, err := jwtManager.ValidateToken(tokenStr)
			if err != nil {
				jsonError(w, 401, "invalid or expired token")
				return
			}
			if requiredScope != "" && claims.Scope != requiredScope {
				jsonError(w, 403, "access denied: invalid scope")
				return
			}
			ctx := setClaims(r, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ========== ROUTES ==========

// --- CAPTCHA ---

func captchaHandler(db *sql.DB) http.HandlerFunc {
	captchaGen := captcha.New(db, captcha.DefaultConfig())
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := captchaGen.Generate()
		if err != nil {
			jsonError(w, 500, "failed to generate captcha")
			return
		}
		jsonResp(w, 200, result)
	}
}

// --- Auth ---

func authRoutes(r chi.Router, db *sql.DB, jwtManager *auth.JWTManager) {
	r.Post("/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username         string `json:"username"`
			Password         string `json:"password"`
			CaptchaSessionID string `json:"captcha_session_id"`
			CaptchaAnswer    string `json:"captcha_answer"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}

		clientIP := getClientIP(r)

		// Check if IP is blocked
		if isIPBlocked(db, clientIP) {
			recordLoginAttempt(db, req.Username, clientIP, r.UserAgent(), false)
			jsonError(w, 429, "too many failed login attempts - your IP has been temporarily blocked")
			return
		}

		// Verify CAPTCHA
		if !captcha.New(db, captcha.DefaultConfig()).Verify(req.CaptchaSessionID, req.CaptchaAnswer) {
			recordLoginAttempt(db, req.Username, clientIP, r.UserAgent(), false)
			jsonError(w, 403, "captcha verification failed")
			return
		}

		var adminID int
		var username, passwordHash, role string
		var lastLogin sql.NullString
		err := db.QueryRow(`SELECT id, username, password_hash, role, last_login_at FROM admins WHERE username = ?`,
			req.Username).Scan(&adminID, &username, &passwordHash, &role, &lastLogin)
		if err != nil {
			recordLoginAttempt(db, req.Username, clientIP, r.UserAgent(), false)
			jsonError(w, 401, "invalid credentials")
			return
		}
		if !auth.CheckPassword(passwordHash, req.Password) {
			recordLoginAttempt(db, req.Username, clientIP, r.UserAgent(), false)
			jsonError(w, 401, "invalid credentials")
			return
		}

		recordLoginAttempt(db, req.Username, clientIP, r.UserAgent(), true)
		db.Exec("UPDATE admins SET last_login_at = datetime('now') WHERE id = ?", adminID)

		claims := &auth.Claims{
			UserID:   adminID,
			Username: username,
			Role:     role,
			Scope:    "parent",
		}
		tokens, err := jwtManager.GenerateTokenPair(claims)
		if err != nil {
			jsonError(w, 500, "token generation failed")
			return
		}

		db.Exec(`INSERT INTO refresh_tokens (user_id, token_hash, scope, expires_at) VALUES (?, ?, 'parent', datetime('now', '+7 days'))`,
			adminID, tokens.RefreshToken)

		jsonResp(w, 200, map[string]interface{}{
			"access_token":  tokens.AccessToken,
			"refresh_token": tokens.RefreshToken,
			"expires_in":    tokens.ExpiresIn,
			"user":          map[string]interface{}{"id": adminID, "username": username, "role": role},
		})
	})

	r.With(authMw(jwtManager, "parent")).Get("/me", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		jsonResp(w, 200, map[string]interface{}{
			"id": c.UserID, "username": c.Username, "role": c.Role, "scope": c.Scope,
		})
	})

	// Refresh token endpoint (admin — only handles parent-scoped tokens)
	r.Post("/refresh", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		if req.RefreshToken == "" {
			jsonError(w, 400, "refresh_token required")
			return
		}

		var userID int
		var scope string
		err := db.QueryRow(`SELECT user_id, scope FROM refresh_tokens WHERE token_hash = ? AND expires_at > datetime('now')`,
			req.RefreshToken).Scan(&userID, &scope)
		if err != nil {
			jsonError(w, 401, "invalid or expired refresh token")
			return
		}
		if scope != "parent" {
			jsonError(w, 401, "invalid or expired refresh token")
			return
		}

		// Delete old refresh token (rotation)
		db.Exec("DELETE FROM refresh_tokens WHERE token_hash = ?", req.RefreshToken)

		var username, role string
		err = db.QueryRow("SELECT username, role FROM admins WHERE id = ?", userID).Scan(&username, &role)
		if err != nil {
			jsonError(w, 401, "user not found")
			return
		}
		claims := &auth.Claims{UserID: userID, Username: username, Role: role, Scope: "parent"}
		tokens, err := jwtManager.GenerateTokenPair(claims)
		if err != nil {
			jsonError(w, 500, "token generation failed")
			return
		}
		db.Exec(`INSERT INTO refresh_tokens (user_id, token_hash, scope, expires_at) VALUES (?, ?, 'parent', datetime('now', '+7 days'))`,
			userID, tokens.RefreshToken)
		jsonResp(w, 200, tokens)
	})
}

// --- Packages ---

func packageRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT id, name, disk_mb, bandwidth_mb, ram_limit_mb, max_db, max_email, max_ftp,
			max_domains, max_subdomains, ssh_access, backup_enabled, is_default, created_at, updated_at
			FROM packages ORDER BY name`)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		defer rows.Close()

		type Pkg struct {
			ID            int    `json:"id"`
			Name          string `json:"name"`
			DiskMB        int    `json:"disk_mb"`
			BandwidthMB   int    `json:"bandwidth_mb"`
			RamLimitMB    int    `json:"ram_limit_mb"`
			MaxDB         int    `json:"max_db"`
			MaxEmail      int    `json:"max_email"`
			MaxFTP        int    `json:"max_ftp"`
			MaxDomains    int    `json:"max_domains"`
			MaxSubdomains int    `json:"max_subdomains"`
			SSHAccess     bool   `json:"ssh_access"`
			BackupEnabled bool   `json:"backup_enabled"`
			IsDefault     bool   `json:"is_default"`
			CreatedAt     string `json:"created_at"`
			UpdatedAt     string `json:"updated_at"`
		}
		pkgs := make([]Pkg, 0)
		for rows.Next() {
			var p Pkg
			var ssh, backup, def int
			if err := rows.Scan(&p.ID, &p.Name, &p.DiskMB, &p.BandwidthMB, &p.RamLimitMB, &p.MaxDB, &p.MaxEmail, &p.MaxFTP,
				&p.MaxDomains, &p.MaxSubdomains, &ssh, &backup, &def, &p.CreatedAt, &p.UpdatedAt); err != nil {
				continue
			}
			p.SSHAccess = ssh == 1
			p.BackupEnabled = backup == 1
			p.IsDefault = def == 1
			pkgs = append(pkgs, p)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[PACKAGES] rows iteration error: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		jsonResp(w, 200, pkgs)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name          string `json:"name"`
			DiskMB        int    `json:"disk_mb"`
			BandwidthMB   int    `json:"bandwidth_mb"`
			RamLimitMB    int    `json:"ram_limit_mb"`
			MaxDB         int    `json:"max_db"`
			MaxEmail      int    `json:"max_email"`
			MaxFTP        int    `json:"max_ftp"`
			MaxDomains    int    `json:"max_domains"`
			MaxSubdomains int    `json:"max_subdomains"`
			SSHAccess     bool   `json:"ssh_access"`
			BackupEnabled bool   `json:"backup_enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		if req.Name == "" {
			jsonError(w, 400, "name is required")
			return
		}
		ssh := 0
		if req.SSHAccess {
			ssh = 1
		}
		backup := 0
		if req.BackupEnabled {
			backup = 1
		}
		auditLog(db, r, "package.create", map[string]interface{}{"name": req.Name})
		result, err := db.Exec(`INSERT INTO packages (name, disk_mb, bandwidth_mb, ram_limit_mb, max_db, max_email,
			max_ftp, max_domains, max_subdomains, ssh_access, backup_enabled)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			req.Name, req.DiskMB, req.BandwidthMB, req.RamLimitMB, req.MaxDB, req.MaxEmail,
			req.MaxFTP, req.MaxDomains, req.MaxSubdomains, ssh, backup)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		id, _ := result.LastInsertId()
		jsonResp(w, 201, map[string]interface{}{"id": id, "name": req.Name})
	})

	r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct {
			Name          string `json:"name"`
			DiskMB        int    `json:"disk_mb"`
			BandwidthMB   int    `json:"bandwidth_mb"`
			RamLimitMB    int    `json:"ram_limit_mb"`
			MaxDB         int    `json:"max_db"`
			MaxEmail      int    `json:"max_email"`
			MaxFTP        int    `json:"max_ftp"`
			MaxDomains    int    `json:"max_domains"`
			MaxSubdomains int    `json:"max_subdomains"`
			SSHAccess     bool   `json:"ssh_access"`
			BackupEnabled bool   `json:"backup_enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		ssh := 0
		if req.SSHAccess {
			ssh = 1
		}
		backup := 0
		if req.BackupEnabled {
			backup = 1
		}
		result, err := db.Exec(`UPDATE packages SET name=?, disk_mb=?, bandwidth_mb=?, ram_limit_mb=?, max_db=?, max_email=?,
			max_ftp=?, max_domains=?, max_subdomains=?, ssh_access=?, backup_enabled=?, updated_at=datetime('now')
			WHERE id=?`,
			req.Name, req.DiskMB, req.BandwidthMB, req.RamLimitMB, req.MaxDB, req.MaxEmail, req.MaxFTP,
			req.MaxDomains, req.MaxSubdomains, ssh, backup, id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			jsonError(w, 404, "package not found")
			return
		}
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		auditLog(db, r, "package.delete", map[string]interface{}{"id": id})
		result, err := db.Exec("DELETE FROM packages WHERE id = ? AND is_default = 0", id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			jsonError(w, 404, "package not found")
			return
		}
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}

// --- Accounts ---

func accountRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		statusFilter := r.URL.Query().Get("status")
		query := `SELECT a.id, a.username, a.domain, a.email, a.package_id,
			p.name, a.status, a.home_dir, a.ip_address,
			a.disk_used_mb, a.bandwidth_used_mb, a.ram_used_mb, a.ram_limit_mb,
			COALESCE(a.suspended_reason, ''), a.created_at, a.updated_at
			FROM accounts a JOIN packages p ON a.package_id = p.id`
		args := []interface{}{}
		if statusFilter != "" {
			query += " WHERE a.status = ?"
			args = append(args, statusFilter)
		}
		query += " ORDER BY a.id DESC"

		rows, err := db.Query(query, args...)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		defer rows.Close()

		type Acc struct {
			ID              int    `json:"id"`
			Username        string `json:"username"`
			Domain          string `json:"domain"`
			Email           string `json:"email"`
			PackageID       int    `json:"package_id"`
			PackageName     string `json:"package_name"`
			Status          string `json:"status"`
			HomeDir         string `json:"home_dir"`
			IPAddress       string `json:"ip_address"`
			DiskUsedMB      int    `json:"disk_used_mb"`
			BandwidthUsedMB int    `json:"bandwidth_used_mb"`
			RamUsedMB       int    `json:"ram_used_mb"`
			RamLimitMB      int    `json:"ram_limit_mb"`
			SuspendedReason string `json:"suspended_reason"`
			CreatedAt       string `json:"created_at"`
			UpdatedAt       string `json:"updated_at"`
		}
		accs := make([]Acc, 0)
			for rows.Next() {
			var a Acc
			var ip, reason sql.NullString
			if err := rows.Scan(&a.ID, &a.Username, &a.Domain, &a.Email, &a.PackageID,
				&a.PackageName, &a.Status, &a.HomeDir, &ip,
				&a.DiskUsedMB, &a.BandwidthUsedMB, &a.RamUsedMB, &a.RamLimitMB, &reason, &a.CreatedAt, &a.UpdatedAt); err != nil {
				continue
			}
			if ip.Valid {
				a.IPAddress = ip.String
			}
			if reason.Valid {
				a.SuspendedReason = reason.String
			}
			accs = append(accs, a)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[ACCOUNTS] rows iteration error: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		jsonResp(w, 200, accs)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
			Domain   string `json:"domain"`
			Email    string `json:"email"`
			Password string `json:"password"`
			PackageID int   `json:"package_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		if req.Username == "" || req.Domain == "" || req.Email == "" || req.Password == "" || req.PackageID == 0 {
			jsonError(w, 400, "all fields required: username, domain, email, password, package_id")
			return
		}

		// Verify the package exists before creating account
		var pkgCount int
		db.QueryRow("SELECT COUNT(*) FROM packages WHERE id = ?", req.PackageID).Scan(&pkgCount)
		if pkgCount == 0 {
			jsonError(w, 400, "selected package does not exist")
			return
		}

		var existing int
		db.QueryRow("SELECT COUNT(*) FROM accounts WHERE username = ?", req.Username).Scan(&existing)
		if existing > 0 {
			jsonError(w, 409, "username already exists")
			return
		}

		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			jsonError(w, 500, "failed to hash password")
			return
		}
		homeDir := getHomesBase() + req.Username

		os.MkdirAll(homeDir, 0755)
		os.MkdirAll(homeDir+"/public_html", 0755)
		os.MkdirAll(homeDir+"/.owp", 0700)

		// Create a default index.html for the account's website
		indexContent := fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>Welcome to %s</title>
<style>body{font-family:Arial;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#f0f4f8}
.card{background:#fff;padding:3rem;border-radius:12px;box-shadow:0 4px 24px rgba(0,0,0,.1);text-align:center;max-width:500px}
h1{color:#1a1a2e;margin-bottom:.5rem}p{color:#555}</style></head>
<body><div class="card"><h1>%s</h1><p>Site hosted by OpenWebPanel</p></div></body></html>`, req.Domain, req.Domain)
		os.WriteFile(homeDir+"/public_html/index.html", []byte(indexContent), 0644)

		result, err := db.Exec(`INSERT INTO accounts (username, domain, email, password_hash,
			package_id, home_dir, status) VALUES (?, ?, ?, ?, ?, ?, 'active')`,
			req.Username, req.Domain, req.Email, hash, req.PackageID, homeDir)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		id, _ := result.LastInsertId()

		auditLog(db, r, "account.create", map[string]interface{}{"id": id, "username": req.Username, "domain": req.Domain, "package_id": req.PackageID})

		// Auto-create primary domain entry so it appears in the domains list
		primaryDocRoot := homeDir + "/public_html"
		db.Exec(`INSERT INTO domains (account_id, domain, type, doc_root) VALUES (?, ?, 'primary', ?)`,
			id, req.Domain, primaryDocRoot)

		// Write Nginx vhost for the primary domain (use default PHP socket)
		if err := writeNginxVhost(sanitizeDomain(req.Domain), primaryDocRoot, "", ""); err != nil {
			log.Printf("[ACCOUNTS] Account created but vhost write failed for %s: %v", req.Domain, err)
			jsonResp(w, 201, map[string]interface{}{
				"id": id, "username": req.Username, "status": "active",
				"warning": "Account created but nginx vhost could not be created. Check server logs.",
			})
			return
		}
		reloadNginx()

		jsonResp(w, 201, map[string]interface{}{"id": id, "username": req.Username, "status": "active"})
	})

	r.Get("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		type Acc struct {
			ID              int    `json:"id"`
			Username        string `json:"username"`
			Domain          string `json:"domain"`
			Email           string `json:"email"`
			PackageID       int    `json:"package_id"`
			PackageName     string `json:"package_name"`
			Status          string `json:"status"`
			HomeDir         string `json:"home_dir"`
			IPAddress       string `json:"ip_address"`
			DiskUsedMB      int    `json:"disk_used_mb"`
			BandwidthUsedMB int    `json:"bandwidth_used_mb"`
			RamUsedMB       int    `json:"ram_used_mb"`
			SuspendedReason string `json:"suspended_reason"`
			CreatedAt       string `json:"created_at"`
			UpdatedAt       string `json:"updated_at"`
		}
		var a Acc
		var ip, reason sql.NullString
		err := db.QueryRow(`SELECT a.id, a.username, a.domain, a.email, a.package_id,
			p.name, a.status, a.home_dir, a.ip_address,
			a.disk_used_mb, a.bandwidth_used_mb, a.ram_used_mb, COALESCE(a.suspended_reason, ''), a.created_at, a.updated_at
			FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`, id).Scan(
			&a.ID, &a.Username, &a.Domain, &a.Email, &a.PackageID,
			&a.PackageName, &a.Status, &a.HomeDir, &ip,
			&a.DiskUsedMB, &a.BandwidthUsedMB, &a.RamUsedMB, &reason, &a.CreatedAt, &a.UpdatedAt)
		if err != nil {
			jsonError(w, 404, "account not found")
			return
		}
		if ip.Valid {
			a.IPAddress = ip.String
		}
		if reason.Valid {
			a.SuspendedReason = reason.String
		}
		jsonResp(w, 200, a)
	})

	r.Post("/{id}/suspend", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct{ Reason string `json:"reason"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		auditLog(db, r, "account.suspend", map[string]interface{}{"id": id, "reason": req.Reason})
		result, err := db.Exec("UPDATE accounts SET status='suspended', suspended_reason=?, updated_at=datetime('now') WHERE id=?", req.Reason, id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			jsonError(w, 404, "account not found")
			return
		}
		jsonResp(w, 200, map[string]string{"status": "suspended"})
	})

	r.Post("/{id}/reset-password", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct{ Password string `json:"password"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		if len(req.Password) < 8 {
			jsonError(w, 400, "password must be at least 8 characters")
			return
		}
		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			jsonError(w, 500, "failed to hash password")
			return
		}
		result, err := db.Exec("UPDATE accounts SET password_hash = ?, updated_at = datetime('now') WHERE id = ?", hash, id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			jsonError(w, 404, "account not found")
			return
		}
		auditLog(db, r, "account.password_reset", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "password reset"})
	})

	r.Post("/{id}/unsuspend", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		auditLog(db, r, "account.unsuspend", map[string]interface{}{"id": id})
		result, err := db.Exec("UPDATE accounts SET status='active', suspended_reason=NULL, updated_at=datetime('now') WHERE id=?", id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			jsonError(w, 404, "account not found")
			return
		}
		jsonResp(w, 200, map[string]string{"status": "active"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		auditLog(db, r, "account.terminate", map[string]interface{}{"id": id})
		result, err := db.Exec("UPDATE accounts SET status='terminated', updated_at=datetime('now') WHERE id=?", id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			jsonError(w, 404, "account not found")
			return
		}

		// Remove vhosts for this account's domains not used by any other active account
		domRows, _ := db.Query("SELECT domain FROM domains WHERE account_id = ?", id)
		if domRows != nil {
			for domRows.Next() {
				var domain string
				domRows.Scan(&domain)
				var activeCount int
				db.QueryRow(`SELECT COUNT(*) FROM domains d
					JOIN accounts a ON a.id = d.account_id
					WHERE d.domain = ? AND a.status = 'active' AND d.account_id != ?`, domain, id).Scan(&activeCount)
				if activeCount == 0 {
					removeNginxVhost(sanitizeDomain(domain))
					log.Printf("[ACCOUNTS] Removed vhost for %s (account %d terminated, no other active owners)", domain, id)
				}
			}
			domRows.Close()
			reloadNginx()
		}

		jsonResp(w, 200, map[string]string{"status": "terminated"})
	})

	r.Get("/{id}/upload-limit", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var defaultLimit, maxLimit, perAccount int
		db.QueryRow("SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = 'default_upload_limit_mb'), 2048)").Scan(&defaultLimit)
		db.QueryRow("SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = 'max_upload_limit_mb'), 5120)").Scan(&maxLimit)
		db.QueryRow("SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = 'upload_limit_' || ?), 0)", id).Scan(&perAccount)
		limit := defaultLimit
		if perAccount > 0 {
			limit = perAccount
		}
		jsonResp(w, 200, map[string]interface{}{
			"current_limit_mb": limit,
			"per_account_mb":   perAccount,
			"default_limit_mb": defaultLimit,
			"max_limit_mb":     maxLimit,
		})
	})

	r.Put("/{id}/upload-limit", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct{ LimitMB int `json:"limit_mb"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		var maxLimit int
		db.QueryRow("SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = 'max_upload_limit_mb'), 5120)").Scan(&maxLimit)
		if req.LimitMB > maxLimit {
			jsonError(w, 400, fmt.Sprintf("cannot exceed max limit of %d MB", maxLimit))
			return
		}
		if req.LimitMB < 0 {
			jsonError(w, 400, "limit must be >= 0")
			return
		}
		db.Exec(`INSERT OR REPLACE INTO server_config (key_name, value, updated_at) VALUES ('upload_limit_' || ?, ?, datetime('now'))`,
			id, fmt.Sprintf("%d", req.LimitMB))
		auditLog(db, r, "account.upload_limit", map[string]interface{}{"id": id, "limit_mb": req.LimitMB})
		jsonResp(w, 200, map[string]string{"status": "updated", "limit_mb": fmt.Sprintf("%d", req.LimitMB)})
	})

	r.Get("/{id}/ram-limit", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var pkgLimit, accountLimit, used int
		db.QueryRow(`SELECT COALESCE(p.ram_limit_mb, 0) FROM accounts a
			JOIN packages p ON a.package_id = p.id WHERE a.id = ?`, id).Scan(&pkgLimit)
		db.QueryRow("SELECT COALESCE(ram_limit_mb, 0) FROM accounts WHERE id = ?", id).Scan(&accountLimit)
		db.QueryRow("SELECT COALESCE(ram_used_mb, 0) FROM accounts WHERE id = ?", id).Scan(&used)
		effective := pkgLimit
		if accountLimit > 0 {
			effective = accountLimit
		}
		jsonResp(w, 200, map[string]interface{}{
			"current_limit_mb": effective,
			"per_account_mb":   accountLimit,
			"package_limit_mb": pkgLimit,
			"ram_used_mb":      used,
		})
	})

	r.Put("/{id}/ram-limit", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct{ LimitMB int `json:"limit_mb"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		if req.LimitMB < 0 {
			jsonError(w, 400, "limit must be >= 0")
			return
		}
		result, err := db.Exec("UPDATE accounts SET ram_limit_mb = ?, updated_at = datetime('now') WHERE id = ?", req.LimitMB, id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			jsonError(w, 404, "account not found")
			return
		}
		auditLog(db, r, "account.ram_limit", map[string]interface{}{"id": id, "limit_mb": req.LimitMB})
		jsonResp(w, 200, map[string]string{"status": "updated", "limit_mb": fmt.Sprintf("%d", req.LimitMB)})
	})
}

// --- Stats ---

func statsRoutes(r chi.Router, db *sql.DB) {
	r.Get("/overview", func(w http.ResponseWriter, r *http.Request) {
		stats := make(map[string]int)
		var count int

		db.QueryRow("SELECT COUNT(*) FROM accounts WHERE status = 'active'").Scan(&count)
		stats["active_accounts"] = count
		db.QueryRow("SELECT COUNT(*) FROM accounts WHERE status = 'suspended'").Scan(&count)
		stats["suspended_accounts"] = count
		db.QueryRow("SELECT COUNT(*) FROM accounts WHERE status = 'pending'").Scan(&count)
		stats["pending_accounts"] = count
		db.QueryRow("SELECT COALESCE(SUM(disk_used_mb), 0) FROM accounts").Scan(&count)
		stats["total_disk_used_mb"] = count
		db.QueryRow("SELECT COALESCE(SUM(bandwidth_used_mb), 0) FROM accounts").Scan(&count)
		stats["total_bandwidth_used_mb"] = count
		db.QueryRow("SELECT COUNT(*) FROM packages").Scan(&count)
		stats["total_packages"] = count

		db.QueryRow("SELECT COALESCE(SUM(ram_used_mb), 0) FROM accounts WHERE status = 'active'").Scan(&count)
		stats["total_ram_used_mb"] = count

		jsonResp(w, 200, stats)
	})
}

// --- Server ---

func getSharedIP() string {
	// If OWP_SHARED_IP is set to a non-loopback address, use it
	if ip := os.Getenv("OWP_SHARED_IP"); ip != "" && !strings.HasPrefix(ip, "127.") && ip != "localhost" {
		return ip
	}
	// Try to fetch public IP from external service first
	client := &http.Client{Timeout: 3 * time.Second}
	if resp, err := client.Get("https://api.ipify.org?format=text"); err == nil {
		defer resp.Body.Close()
		if body, err := io.ReadAll(resp.Body); err == nil {
			if ip := strings.TrimSpace(string(body)); ip != "" && !strings.HasPrefix(ip, "127.") {
				return ip
			}
		}
	}
	// Fall back to detecting primary network IP
	addrs, _ := os.ReadFile("/proc/net/fib_trie")
	for _, line := range strings.Split(string(addrs), "\n") {
		if strings.Contains(line, "host LOCAL") {
			continue
		}
		fields := strings.Fields(line)
		for _, f := range fields {
			if strings.Count(f, ".") == 3 && !strings.HasPrefix(f, "127.") {
				return f
			}
		}
	}
	return "127.0.0.1"
}

func serverRoutes(r chi.Router) {
	r.Get("/status", func(w http.ResponseWriter, r *http.Request) {
		// Real system data from /proc
		cpuPct := 0.0
		if stat, err := os.ReadFile("/proc/stat"); err == nil {
			var cpu string
			var user, nice, system, idle, iowait, irq, softirq, steal uint64
			fmt.Sscanf(string(stat), "%s %d %d %d %d %d %d %d %d", &cpu, &user, &nice, &system, &idle, &iowait, &irq, &softirq, &steal)
			total := user + nice + system + idle + iowait + irq + softirq + steal
			if total > 0 {
				cpuPct = float64(total-idle-iowait) / float64(total) * 100
			}
		}

		// RAM from /proc/meminfo
		var ramTotalKB, ramAvailKB uint64
		if mem, err := os.ReadFile("/proc/meminfo"); err == nil {
			for _, line := range strings.Split(string(mem), "\n") {
				var key string
				var val uint64
				fmt.Sscanf(line, "%s %d", &key, &val)
				switch key {
				case "MemTotal:":
					ramTotalKB = val
				case "MemAvailable:":
					ramAvailKB = val
				}
			}
		}

		// Uptime
		var uptime float64
		if up, err := os.ReadFile("/proc/uptime"); err == nil {
			fmt.Sscanf(string(up), "%f", &uptime)
		}

		// Disk usage on /
		var diskUsed, diskTotal uint64
		if out, err := runCmd("df", "-B1", "/"); err == nil {
			lines := strings.Split(strings.TrimSpace(out), "\n")
			if len(lines) >= 2 {
				var fs string
				var avail uint64
				fmt.Sscanf(lines[1], "%s %d %d %d", &fs, &diskTotal, &diskUsed, &avail)
			}
		}

		// Use actual disk metrics as fallback
		diskUsedMB := int(diskUsed / 1048576)
		diskTotalMB := int(diskTotal / 1048576)
		ramUsedMB := int((ramTotalKB - ramAvailKB) / 1024)
		ramTotalMB := int(ramTotalKB / 1024)

		// Read actual load averages from /proc/loadavg
		load1, load5, load15 := 0.0, 0.0, 0.0
		if loadData, err := os.ReadFile("/proc/loadavg"); err == nil {
			fmt.Sscanf(string(loadData), "%f %f %f", &load1, &load5, &load15)
		}

		jsonResp(w, 200, map[string]interface{}{
			"shared_ip":     getSharedIP(),
			"hostname":      getHostname(),
			"os":            getOS(),
			"cpu_percent":   float64(int(cpuPct*10)) / 10,
			"ram_used_mb":   ramUsedMB,
			"ram_total_mb":  ramTotalMB,
			"disk_used_mb":  diskUsedMB,
			"disk_total_mb": diskTotalMB,
			"disk_free_mb":  diskTotalMB - diskUsedMB,
			"load_1m":       load1, "load_5m": load5, "load_15m": load15,
			"uptime_hours":  int(uptime / 3600),
		})
	})
}

func getHostname() string {
	h, _ := os.Hostname()
	return h
}

func getOS() string {
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
			}
		}
	}
	return "Linux"
}

func runCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var (
	nginxPrefix = getEnvDefault("NGINX_PREFIX", "/etc/nginx")
	vhostDir    = nginxPrefix + "/vhosts/"
	nginxBin    = getEnvDefault("NGINX_BIN", nginxPrefix+"/sbin/nginx")
	nginxConf   = getEnvDefault("NGINX_CONF", nginxPrefix+"/nginx.conf")
)

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getNginxLogDir() string {
	return getEnvDefault("NGINX_LOG_DIR", nginxPrefix+"/logs")
}

type vhostInfo struct {
	domain     string
	docRoot    string
	socketPath string
}

func syncNginxVhosts(db *sql.DB) {
	os.MkdirAll(vhostDir, 0755)
	// Only write vhosts for active accounts, deduplicated by domain name.
	// When multiple active accounts share the same domain, the most recently
	// added domain record wins (highest id).
	rows, err := db.Query(`SELECT d.domain, d.doc_root, COALESCE(pv.socket_path, '') FROM domains d
		JOIN accounts a ON a.id = d.account_id
		LEFT JOIN account_php_version apv ON apv.account_id = a.id
		LEFT JOIN php_versions pv ON pv.id = apv.php_version_id
		WHERE a.status = 'active'
		AND d.id = (
			SELECT MAX(d2.id) FROM domains d2
			JOIN accounts a2 ON a2.id = d2.account_id
			WHERE d2.domain = d.domain AND a2.status = 'active'
		)`)
	if err != nil {
		log.Printf("[NGINX] Failed to query active domains: %v", err)
		return
	}
	// Collect all results first, then close the cursor to release the connection
	var vhosts []vhostInfo
	for rows.Next() {
		var v vhostInfo
		if err := rows.Scan(&v.domain, &v.docRoot, &v.socketPath); err != nil {
			continue
		}
		vhosts = append(vhosts, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("[NGINX] rows iteration error: %v", err)
	}

	activeVhosts := make(map[string]bool)
	for _, v := range vhosts {
		os.MkdirAll(v.docRoot, 0755)
		safeDomain := sanitizeDomain(v.domain)
		if err := writeNginxVhost(safeDomain, v.docRoot, "", v.socketPath); err != nil {
			log.Printf("[NGINX] Failed to write vhost for %s: %v", v.domain, err)
		} else {
			log.Printf("[NGINX] Synced vhost for %s", v.domain)
		}
		activeVhosts[v.domain] = true
	}

	// Remove vhost files for domains no longer owned by any active account
	if entries, err := os.ReadDir(vhostDir); err == nil {
		for _, e := range entries {
			name := strings.TrimSuffix(e.Name(), ".conf")
			if !activeVhosts[name] {
				os.Remove(vhostDir + e.Name())
				log.Printf("[NGINX] Removed vhost for terminated/inactive domain: %s", name)
			}
		}
	}

	// Restore SSL server blocks for domains with issued certs (only for active vhosts)
	certRows, err := db.Query("SELECT DISTINCT domain FROM ssl_certs WHERE status = 'issued'")
	if err != nil {
		return
	}
	var certDomains []string
	for certRows.Next() {
		var domain string
		certRows.Scan(&domain)
		certDomains = append(certDomains, domain)
	}
	certRows.Close()
	if err := certRows.Err(); err != nil {
		log.Printf("[NGINX] cert rows iteration error: %v", err)
	}
	for _, domain := range certDomains {
		if activeVhosts[domain] {
			addNginxSSL(db, domain)
		}
	}
}

func writeNginxVhost(domain, docRoot, accountIP, phpSocket string) error {
	logDir := getNginxLogDir()
	phpFpmSocket := phpSocket
	if phpFpmSocket == "" {
		phpFpmSocket = getEnvDefault("PHP_FPM_SOCKET", "/run/php/php8.3-fpm.sock")
	}
	os.MkdirAll(logDir, 0755)
	cfg := fmt.Sprintf(`# OpenWebPanel -- %s
server {
    listen 80;
    listen [::]:80;
    server_name %s www.%s;
    root %s;
    index index.html index.htm index.php;

    access_log ` + logDir + `/%s.access.log;
    error_log ` + logDir + `/%s.error.log;

    location / {
        try_files $uri $uri/ /index.php?$args;
    }

    location ~ \.php$ {
        fastcgi_pass unix:` + phpFpmSocket + `;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        include fastcgi_params;
    }

    # ACME HTTP-01 challenge (Let's Encrypt)
    location ^~ /.well-known/acme-challenge/ {
        proxy_pass http://127.0.0.1:9000;
        proxy_set_header Host $host;
    }

    location ~ /\. {
        deny all;
    }

    location ~ /\.owp {
        deny all;
    }
}
`, domain, domain, domain, docRoot, domain, domain)
	return os.WriteFile(vhostDir+domain+".conf", []byte(cfg), 0644)
}

func removeNginxVhost(domain string) error {
	return os.Remove(vhostDir + domain + ".conf")
}

func reloadNginx() {
	if out, err := runCmd("sudo", "-n", nginxBin, "-s", "reload"); err != nil {
		log.Printf("[NGINX] sudo -n reload failed: %v\n%s", err, out)
		if out2, err2 := runCmd(nginxBin, "-s", "reload"); err2 != nil {
			log.Printf("[NGINX] Direct reload also failed: %v\n%s", err2, out2)
		} else {
			log.Printf("[NGINX] Reloaded successfully")
		}
	} else {
		log.Printf("[NGINX] Reloaded successfully")
	}
}
func auditLog(db *sql.DB, r *http.Request, action string, details interface{}) {
	c := getClaims(r)
	actorType := audit.ActorAdmin
	actorID := 0
	if c != nil {
		actorID = c.UserID
		if c.Scope == "child" {
			actorType = audit.ActorAccount
			actorID = c.AccountID
		}
	}
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx >= 0 {
		ip = ip[:idx]
	}
	audit.New(db).Log(actorType, actorID, action, "", 0, details, ip)
}

const maxLoginAttempts = 5

func isIPBlocked(db *sql.DB, ipAddress string) bool {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM blocked_ips WHERE ip_address = ?", ipAddress).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

func recordLoginAttempt(db *sql.DB, username, ipAddress, userAgent string, success bool) {
	db.Exec(`INSERT INTO login_attempts (username, ip_address, success, user_agent) VALUES (?, ?, ?, ?)`,
		username, ipAddress, boolToInt(success), userAgent)
	if !success {
		var failedCount int
		db.QueryRow(`SELECT COUNT(*) FROM login_attempts 
			WHERE ip_address = ? AND success = 0 
			AND created_at > datetime('now', '-15 minutes')`, ipAddress).Scan(&failedCount)
		if failedCount >= maxLoginAttempts {
			db.Exec(`INSERT OR REPLACE INTO blocked_ips (ip_address, reason, blocked_by, failed_attempts, updated_at) 
				VALUES (?, 'Exceeded maximum failed login attempts', 'system', ?, datetime('now'))`,
				ipAddress, failedCount)
			log.Printf("[SECURITY] IP %s blocked: %d failed login attempts in 15 minutes", ipAddress, failedCount)
		}
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func getClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	// Only trust X-Forwarded-For from known reverse proxies
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		// In production, validate against trusted proxy list
		parts := strings.Split(fwd, ",")
		if ip := net.ParseIP(strings.TrimSpace(parts[0])); ip != nil {
			return ip.String()
		}
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		if ip := net.ParseIP(realIP); ip != nil {
			return ip.String()
		}
	}
	return host
}

func getHomesBase() string {
	homesBase := os.Getenv("OWP_HOMES_BASE")
	if homesBase == "" {
		homesBase = "./homes/"
	}
	// Resolve to absolute path
	if absPath, err := filepath.Abs(homesBase); err == nil {
		return absPath + "/"
	}
	return homesBase
}


// --- Child Auth ---

func childAuthRoutes(r chi.Router, db *sql.DB, jwtManager *auth.JWTManager) {
	r.Post("/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username         string `json:"username"`
			Password         string `json:"password"`
			CaptchaSessionID string `json:"captcha_session_id"`
			CaptchaAnswer    string `json:"captcha_answer"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}

		clientIP := getClientIP(r)

		// Check if IP is blocked
		if isIPBlocked(db, clientIP) {
			recordLoginAttempt(db, req.Username, clientIP, r.UserAgent(), false)
			jsonError(w, 429, "too many failed login attempts - your IP has been temporarily blocked")
			return
		}

		// Verify CAPTCHA
		if !captcha.New(db, captcha.DefaultConfig()).Verify(req.CaptchaSessionID, req.CaptchaAnswer) {
			recordLoginAttempt(db, req.Username, clientIP, r.UserAgent(), false)
			jsonError(w, 403, "captcha verification failed")
			return
		}

		var id int
		var username, passwordHash, status, homeDir string
		err := db.QueryRow(`SELECT id, username, password_hash, status, home_dir FROM accounts WHERE username = ?`, req.Username).Scan(
			&id, &username, &passwordHash, &status, &homeDir)
		if err != nil {
			recordLoginAttempt(db, req.Username, clientIP, r.UserAgent(), false)
			jsonError(w, 401, "invalid credentials")
			return
		}
		if status != "active" {
			recordLoginAttempt(db, req.Username, clientIP, r.UserAgent(), false)
			jsonError(w, 403, "account is "+status)
			return
		}
		if !auth.CheckPassword(passwordHash, req.Password) {
			recordLoginAttempt(db, req.Username, clientIP, r.UserAgent(), false)
			jsonError(w, 401, "invalid credentials")
			return
		}

		recordLoginAttempt(db, req.Username, clientIP, r.UserAgent(), true)

		claims := &auth.Claims{
			UserID:    id,
			Username:  username,
			Role:      "account",
			Scope:     "child",
			AccountID: id,
		}
		tokens, err := jwtManager.GenerateTokenPair(claims)
		if err != nil {
			jsonError(w, 500, "token failed")
			return
		}

		db.Exec(`INSERT INTO refresh_tokens (user_id, token_hash, scope, expires_at) VALUES (?, ?, 'child', datetime('now', '+7 days'))`,
			id, tokens.RefreshToken)

		jsonResp(w, 200, map[string]interface{}{
			"access_token":  tokens.AccessToken,
			"refresh_token": tokens.RefreshToken,
			"expires_in":    tokens.ExpiresIn,
			"user":          map[string]interface{}{"id": id, "username": username, "home_dir": homeDir},
		})
	})

	r.With(authMw(jwtManager, "child")).Get("/me", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		var homeDir string
		db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", c.AccountID).Scan(&homeDir)
		jsonResp(w, 200, map[string]interface{}{
			"id": c.UserID, "username": c.Username, "home_dir": homeDir, "role": c.Role,
		})
	})

	r.With(authMw(jwtManager, "child")).Put("/change-password", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		var req struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		var hash string
		db.QueryRow("SELECT password_hash FROM accounts WHERE id = ?", c.AccountID).Scan(&hash)
		if !auth.CheckPassword(hash, req.CurrentPassword) {
			jsonError(w, 400, "current password is incorrect")
			return
		}
		newHash, err := auth.HashPassword(req.NewPassword)
		if err != nil {
			jsonError(w, 500, "failed to hash password")
			return
		}
		db.Exec("UPDATE accounts SET password_hash = ? WHERE id = ?", newHash, c.AccountID)
		auditLog(db, r, "account.change_password", map[string]interface{}{"account_id": c.AccountID})
		jsonResp(w, 200, map[string]string{"status": "password changed"})
	})

	r.With(authMw(jwtManager, "child")).Get("/upload-limit", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		var defaultLimit, maxLimit, perAccount int
		db.QueryRow("SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = 'default_upload_limit_mb'), 2048)").Scan(&defaultLimit)
		db.QueryRow("SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = 'max_upload_limit_mb'), 5120)").Scan(&maxLimit)
		db.QueryRow("SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = 'upload_limit_' || ?), 0)", c.AccountID).Scan(&perAccount)
		if perAccount > 0 {
			defaultLimit = perAccount
		}
		var limitDisabled bool
		db.QueryRow("SELECT COALESCE((SELECT value FROM server_config WHERE key_name = 'upload_limit_disabled'), 'false')").Scan(&limitDisabled)
		jsonResp(w, 200, map[string]interface{}{
			"current_limit_mb": defaultLimit,
			"max_limit_mb":     maxLimit,
			"limit_disabled":   limitDisabled,
		})
	})

	r.With(authMw(jwtManager, "child")).Put("/upload-limit", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		var req struct{ LimitMB int `json:"limit_mb"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		var maxLimit int
		db.QueryRow("SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = 'max_upload_limit_mb'), 5120)").Scan(&maxLimit)
		if req.LimitMB > maxLimit {
			jsonError(w, 400, fmt.Sprintf("cannot exceed max limit of %d MB", maxLimit))
			return
		}
		db.Exec(`INSERT OR REPLACE INTO server_config (key_name, value, updated_at) VALUES ('upload_limit_' || ?, ?, datetime('now'))`,
			c.AccountID, fmt.Sprintf("%d", req.LimitMB))
		jsonResp(w, 200, map[string]string{"status": "updated", "limit_mb": fmt.Sprintf("%d", req.LimitMB)})
	})

	// Child refresh token endpoint
	r.Post("/refresh", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		if req.RefreshToken == "" {
			jsonError(w, 400, "refresh_token required")
			return
		}

		var userID int
		var scope string
		err := db.QueryRow(`SELECT user_id, scope FROM refresh_tokens WHERE token_hash = ? AND expires_at > datetime('now')`,
			req.RefreshToken).Scan(&userID, &scope)
		if err != nil {
			jsonError(w, 401, "invalid or expired refresh token")
			return
		}
		if scope != "child" {
			jsonError(w, 401, "invalid or expired refresh token")
			return
		}

		db.Exec("DELETE FROM refresh_tokens WHERE token_hash = ?", req.RefreshToken)

		var username, status string
		err = db.QueryRow("SELECT username, status FROM accounts WHERE id = ?", userID).Scan(&username, &status)
		if err != nil {
			jsonError(w, 401, "account not found")
			return
		}
		if status != "active" {
			jsonError(w, 403, "account is "+status)
			return
		}
		claims := &auth.Claims{UserID: userID, Username: username, Role: "account", Scope: "child", AccountID: userID}
		tokens, err := jwtManager.GenerateTokenPair(claims)
		if err != nil {
			jsonError(w, 500, "token generation failed")
			return
		}
		db.Exec(`INSERT INTO refresh_tokens (user_id, token_hash, scope, expires_at) VALUES (?, ?, 'child', datetime('now', '+7 days'))`,
			userID, tokens.RefreshToken)
		jsonResp(w, 200, tokens)
	})
}

// --- Child File Manager ---

func childFileRoutes(r chi.Router, db *sql.DB) {
	getHomeDir := func(r *http.Request) string {
		c := getClaims(r)
		if c == nil {
			return ""
		}
		var h string
		if err := db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", c.AccountID).Scan(&h); err != nil {
			return ""
		}
		return h
	}

	r.Get("/list", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" {
			jsonError(w, 403, "cannot determine home")
			return
		}
		userPath := r.URL.Query().Get("path")
		if userPath == "" {
			userPath = "/"
		}

		safePath, err := filesystem.SafePath(home, userPath)
		if err != nil {
			jsonError(w, 403, err.Error())
			return
		}

		entries, err := os.ReadDir(safePath)
		if err != nil {
			jsonError(w, 404, "directory not found")
			return
		}

		type FileEntry struct {
			Name    string `json:"name"`
			Type    string `json:"type"`
			Size    string `json:"size"`
			ModTime string `json:"mod_time"`
			Perm    string `json:"perm"`
		}
		res := make([]FileEntry, 0)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			typ := "file"
			if e.IsDir() {
				typ = "dir"
			}
			sz := ""
			if !e.IsDir() {
				sz = filesystem.HumanSize(info.Size())
			}
			res = append(res, FileEntry{
				Name:    e.Name(),
				Type:    typ,
				Size:    sz,
				ModTime: info.ModTime().Format(time.RFC3339),
				Perm:    info.Mode().String(),
			})
		}
		jsonResp(w, 200, map[string]interface{}{
			"path":    userPath,
			"entries": res,
		})
	})

	r.Post("/mkdir", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" {
			jsonError(w, 403, "no home")
			return
		}
		var req struct{ Path, Name string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body"); return
		}
		if strings.Contains(req.Name, "/") || strings.Contains(req.Name, "..") {
			jsonError(w, 400, "invalid directory name"); return
		}
		safe, err := filesystem.SafePath(home, req.Path)
		if err != nil {
			jsonError(w, 403, err.Error())
			return
		}
		if err := os.MkdirAll(filepath.Join(safe, req.Name), 0750); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonResp(w, 201, map[string]string{"status": "created"})
	})

	r.Post("/delete", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" {
			jsonError(w, 403, "no home")
			return
		}
		var req struct{ Path string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body"); return
		}
		safe, err := filesystem.SafePath(home, req.Path)
		if err != nil {
			jsonError(w, 403, err.Error())
			return
		}

		// Move to trash instead of permanent delete
		info, statErr := os.Stat(safe)
		if statErr != nil {
			jsonError(w, 404, "not found")
			return
		}
		c := getClaims(r)
		trashDir := filepath.Join(home, ".trash")
		os.MkdirAll(trashDir, 0750)
		trashPath := filepath.Join(trashDir, filepath.Base(safe))
		// Avoid collisions
		if _, err := os.Stat(trashPath); err == nil {
			trashPath = filepath.Join(trashDir, filepath.Base(safe)+"_"+strconv.FormatInt(time.Now().UnixNano(), 36))
		}

		// Record in DB first (dangling pointers are cleaned up by periodic job)
		isDir := 0
		if info.IsDir() {
			isDir = 1
		}
		relPath, _ := filepath.Rel(home, safe)
		var dbErr error
		_, dbErr = db.Exec(`INSERT INTO file_trash (account_id, original_path, trash_path, size_bytes, is_dir, expires_at)
			VALUES (?, ?, ?, ?, ?, datetime('now', '+30 days'))`,
			c.AccountID, relPath, trashPath, info.Size(), isDir)
		if dbErr != nil {
			jsonError(w, 500, "failed to record trash entry: "+err.Error())
			return
		}

		os.Rename(safe, trashPath)

		jsonResp(w, 200, map[string]string{"status": "trashed"})
	})

	r.Post("/rename", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" {
			jsonError(w, 403, "no home")
			return
		}
		var req struct {
			OldPath string `json:"old_path"`
			NewPath string `json:"new_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body"); return
		}
		old, err := filesystem.SafePath(home, req.OldPath)
		if err != nil {
			jsonError(w, 403, err.Error())
			return
		}
		newp, err := filesystem.SafePath(home, req.NewPath)
		if err != nil {
			jsonError(w, 403, err.Error())
			return
		}
		// Ensure neither path is inside protected directories
		relOld, _ := filepath.Rel(home, old)
		relNew, _ := filepath.Rel(home, newp)
		relOld = filepath.ToSlash(relOld)
		relNew = filepath.ToSlash(relNew)
		if strings.HasPrefix(relOld, ".owp") || strings.HasPrefix(relOld, ".trash") ||
			strings.HasPrefix(relNew, ".owp") || strings.HasPrefix(relNew, ".trash") {
			jsonError(w, 403, "cannot rename protected files"); return
		}
		if old == newp {
			jsonResp(w, 200, map[string]string{"status": "renamed"}); return
		}
		// Check if destination exists
		if _, err := os.Stat(newp); err == nil {
			jsonError(w, 409, "destination already exists"); return
		}
		if err := os.Rename(old, newp); err != nil {
			jsonError(w, 500, "rename failed: "+err.Error()); return
		}
		jsonResp(w, 200, map[string]string{"status": "renamed"})
	})

	r.Get("/read", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" {
			jsonError(w, 403, "no home")
			return
		}
		safe, err := filesystem.SafePath(home, r.URL.Query().Get("path"))
		if err != nil {
			jsonError(w, 403, err.Error())
			return
		}
		stat, err := os.Stat(safe)
		if err != nil {
			jsonError(w, 404, "not found")
			return
		}
		if stat.Size() > 10*1024*1024 {
			jsonError(w, 413, "file too large (>10MB)")
			return
		}
		data, err := os.ReadFile(safe)
		if err != nil {
			jsonError(w, 404, "not found")
			return
		}
		// Detect binary — if null bytes found, serve as base64
		isBinary := bytes.IndexByte(data, 0) != -1
		var content string
		contentType := "text"
		if isBinary {
			content = base64.StdEncoding.EncodeToString(data)
			contentType = "base64"
		} else {
			content = string(data)
		}
		if len(data) > 512*1024 {
			contentType = "large"
		}
		jsonResp(w, 200, map[string]interface{}{
			"path":    r.URL.Query().Get("path"),
			"content": content,
			"type":    contentType,
			"size":    len(data),
		})
	})

	r.Post("/write", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" {
			jsonError(w, 403, "no home")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
		var req struct{ Path, Content string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body"); return
		}
		safe, err := filesystem.SafePath(home, req.Path)
		if err != nil {
			jsonError(w, 403, err.Error())
			return
		}
		if err := os.WriteFile(safe, []byte(req.Content), 0640); err != nil {
			jsonError(w, 500, "write failed: "+err.Error()); return
		}
		jsonResp(w, 200, map[string]string{"status": "written"})
	})

	r.Get("/disk-usage", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" {
			jsonError(w, 403, "no home")
			return
		}
		var total int64
		walkDirForSize(home, &total, 0)
		jsonResp(w, 200, map[string]interface{}{
			"size_bytes": total,
			"size_mb":    total / (1024 * 1024),
			"human":      filesystem.HumanSize(total),
		})
	})

	// Upload file
	r.Post("/upload", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" {
			jsonError(w, 403, "no home")
			return
		}
		if err := r.ParseMultipartForm(200 << 20); err != nil {
			jsonError(w, 400, "failed to parse upload: "+err.Error())
			return
		}
		uploadPath := r.FormValue("path")
		file, header, err := r.FormFile("file")
		if err != nil {
			jsonError(w, 400, "failed to read file")
			return
		}
		defer file.Close()

		// Block dangerous file types
		blockedExt := map[string]bool{
			".exe": true, ".msi": true, ".bin": true, ".com": true,
			".scr": true, ".pif": true, ".jar": true,
			".bat": true, ".cmd": true, ".ps1": true, ".psm1": true,
			".psd1": true, ".vbs": true, ".vbe": true, ".jse": true,
			".wsf": true, ".wsh": true, ".msc": true,
			".dll": true, ".so": true, ".dylib": true, ".sys": true, ".drv": true,
			".sh": true, ".bash": true, ".zsh": true, ".ksh": true, ".csh": true,
			".class": true,
			".php": true, ".phtml": true, ".php3": true, ".php4": true, ".php5": true, ".php7": true, ".phar": true,
			".shtml": true, ".cgi": true, ".pl": true, ".py": true, ".rb": true,
			".htaccess": true, ".user.ini": true,
		}
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if blockedExt[ext] {
			jsonError(w, 403, "file type \""+ext+"\" is not allowed for security reasons")
			return
		}

		// Sanitize filename: strip path separators to prevent traversal
		safeName := filepath.Base(header.Filename)
		if safeName == "." || safeName == "/" {
			jsonError(w, 400, "invalid filename"); return
		}

		// Enforce per-account upload limit and RAM limit
		c := getClaims(r)
		if c != nil {
			if isRAMExceeded(db, c.AccountID) {
				jsonError(w, 429, "RAM limit exceeded. Uploads are temporarily blocked. Please contact your hosting administrator to upgrade your resource allocation.")
				return
			}
			var accountLimit, defaultLimit int
			db.QueryRow("SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = 'default_upload_limit_mb'), 2048)").Scan(&defaultLimit)
			db.QueryRow("SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = 'upload_limit_' || ?), 0)", c.AccountID).Scan(&accountLimit)
			limitMB := defaultLimit
			if accountLimit > 0 {
				limitMB = accountLimit
			}
			limitBytes := int64(limitMB) * 1024 * 1024
			if header.Size > limitBytes {
				jsonError(w, 413, fmt.Sprintf("file exceeds upload limit of %d MB", limitMB))
				return
			}
		}

		safe, err := filesystem.SafePath(home, uploadPath+"/"+safeName)
		if err != nil {
			jsonError(w, 403, err.Error())
			return
		}
		tmpPath := safe + ".tmp"
		dst, err := os.Create(tmpPath)
		if err != nil {
			jsonError(w, 500, "failed to create file: "+err.Error())
			return
		}
		defer os.Remove(tmpPath)

		if _, err := io.CopyBuffer(dst, file, make([]byte, 1024*1024)); err != nil {
			dst.Close()
			jsonError(w, 500, "failed to write file: "+err.Error())
			return
		}
		if err := dst.Close(); err != nil {
			jsonError(w, 500, "failed to close file: "+err.Error())
			return
		}
		if err := os.Rename(tmpPath, safe); err != nil {
			jsonError(w, 500, "failed to finalize file: "+err.Error())
			return
		}

		jsonResp(w, 200, map[string]interface{}{"status": "uploaded", "name": safeName})
	})

	// Compress to zip
	r.Post("/compress", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" {
			jsonError(w, 403, "no home")
			return
		}
		c := getClaims(r)
		if c != nil && isRAMExceeded(db, c.AccountID) {
			jsonError(w, 429, "RAM limit exceeded. File compression is temporarily blocked. Please contact your hosting administrator to upgrade your resource allocation.")
			return
		}
		var req struct {
			Path        string   `json:"path"`
			ArchiveName string   `json:"archive_name"`
			Files       []string `json:"files"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body"); return
		}
		safeDir, err := filesystem.SafePath(home, req.Path)
		if err != nil {
			jsonError(w, 403, err.Error()); return
		}
		archiveName := filepath.Base(req.ArchiveName)
		archivePath := filepath.Join(safeDir, archiveName)
		zf, err := os.Create(archivePath)
		if err != nil {
			jsonError(w, 500, "failed to create archive: "+err.Error()); return
		}
		defer zf.Close()
		zw := zip.NewWriter(zf)
		defer zw.Close()
		fileCount := 0
		for _, f := range req.Files {
			if fileCount >= 1000 {
				break
			}
			if strings.HasPrefix(f, "/") || strings.Contains(f, "..") {
				continue
			}
			src, err := filesystem.SafePath(safeDir, f)
			if err != nil {
				continue
			}
			info, err := os.Stat(src)
			if err != nil { continue }
			fileCount++
			if info.IsDir() { addDirToZip(zw, src, f) } else { addFileToZip(zw, src, f) }
		}
		jsonResp(w, 200, map[string]string{"status": "compressed"})
	})

	// Extract zip
	r.Post("/extract", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" { jsonError(w, 403, "no home"); return }
		c := getClaims(r)
		if c != nil && isRAMExceeded(db, c.AccountID) {
			jsonError(w, 429, "RAM limit exceeded. File extraction is temporarily blocked. Please contact your hosting administrator to upgrade your resource allocation.")
			return
		}
		var req struct{ Path string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body"); return
		}
		safePath, err := filesystem.SafePath(home, req.Path)
		if err != nil { jsonError(w, 403, err.Error()); return }
		r2, err := zip.OpenReader(safePath)
		if err != nil { jsonError(w, 400, "not a valid zip"); return }
		defer r2.Close()
		dest := filepath.Dir(safePath)
		for _, f := range r2.File {
			// Prevent zip slip: reject entries that escape dest
			fp := filepath.Join(dest, f.Name)
			if !strings.HasPrefix(fp, filepath.Clean(dest)+string(filepath.Separator)) && fp != filepath.Clean(dest) {
				continue
			}
			// Skip symlinks
			if f.FileInfo().Mode()&os.ModeSymlink != 0 {
				log.Printf("Skipping symlink entry in zip: %s", f.Name)
				continue
			}
			if f.FileInfo().IsDir() { os.MkdirAll(fp, 0750); continue }
			os.MkdirAll(filepath.Dir(fp), 0750)
			src, err := f.Open()
			if err != nil { log.Printf("Failed to open zip entry %s: %v", f.Name, err); continue }
			dst, err := os.Create(fp)
			if err != nil { src.Close(); log.Printf("Failed to create %s: %v", fp, err); continue }
			io.Copy(dst, src)
			src.Close(); dst.Close()
		}
		jsonResp(w, 200, map[string]string{"status": "extracted"})
	})

	// --- Trash ---
	r.Get("/trash", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		rows, err := db.Query(`SELECT id, original_path, size_bytes, is_dir, deleted_at FROM file_trash
			WHERE account_id = ? AND expires_at > datetime('now') ORDER BY deleted_at DESC LIMIT 100`, c.AccountID)
		if err != nil { jsonResp(w, 200, []interface{}{}); return }
		defer rows.Close()
		items := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id, sz, isDir int; var orig, del string
			rows.Scan(&id, &orig, &sz, &isDir, &del)
			items = append(items, map[string]interface{}{
				"id": id, "original_path": orig, "size_bytes": sz,
				"is_dir": isDir == 1, "deleted_at": del})
		}
		if err := rows.Err(); err != nil {
			log.Printf("[FILES] trash rows iteration error: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		jsonResp(w, 200, items)
	})

	r.Post("/trash/{id}/restore", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			jsonError(w, 400, "invalid id"); return
		}
		home := getHomeDir(r)
		if home == "" {
			jsonError(w, 403, "no home"); return
		}
		var origPath, trashPath string
		err = db.QueryRow("SELECT original_path, trash_path FROM file_trash WHERE id = ? AND account_id = ?",
			id, c.AccountID).Scan(&origPath, &trashPath)
		if err != nil {
			jsonError(w, 404, "trash entry not found"); return
		}
		origFull := filepath.Join(home, origPath)
		if err := os.Rename(trashPath, origFull); err != nil {
			jsonError(w, 500, "restore failed: "+err.Error())
			return
		}
		db.Exec("DELETE FROM file_trash WHERE id = ?", id)
		jsonResp(w, 200, map[string]string{"status": "restored"})
	})

	r.Delete("/trash/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			jsonError(w, 400, "invalid id"); return
		}
		var tp string
		err = db.QueryRow("SELECT trash_path FROM file_trash WHERE id = ? AND account_id = ?", id, c.AccountID).Scan(&tp)
		if err != nil {
			jsonError(w, 404, "trash entry not found"); return
		}
		os.RemoveAll(tp); db.Exec("DELETE FROM file_trash WHERE id = ?", id)
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	r.Post("/trash/empty", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		rows, _ := db.Query("SELECT id, trash_path FROM file_trash WHERE account_id = ?", c.AccountID)
		if rows != nil { for rows.Next() { var id int; var tp string; rows.Scan(&id, &tp); os.RemoveAll(tp) }; rows.Close() }
		db.Exec("DELETE FROM file_trash WHERE account_id = ?", c.AccountID)
		jsonResp(w, 200, map[string]string{"status": "emptied"})
	})

		r.Get("/download", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" {
			jsonError(w, 403, "no home")
			return
		}
		filePath := r.URL.Query().Get("path")
		if filePath == "" {
			jsonError(w, 400, "path required")
			return
		}
		safe, err := filesystem.SafePath(home, filePath)
		if err != nil {
			jsonError(w, 403, err.Error())
			return
		}
		// Protect dotfiles and system dirs
		baseName := filepath.Base(safe)
		if strings.HasPrefix(baseName, ".") {
			jsonError(w, 403, "access denied")
			return
		}
		rel, err := filepath.Rel(home, safe)
		if err != nil || strings.HasPrefix(rel, ".") {
			jsonError(w, 403, "access denied")
			return
		}
		f, err := os.Open(safe)
		if err != nil {
			jsonError(w, 404, "file not found")
			return
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil {
			jsonError(w, 500, "failed to stat file"); return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+baseName+"\"")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "private, no-cache")
		http.ServeContent(w, r, baseName, stat.ModTime(), f)
	})
}

func walkDirForSize(root string, total *int64, depth int) {
	if depth > 20 {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") && (e.Name() == ".owp" || e.Name() == ".trash") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if e.IsDir() {
			walkDirForSize(filepath.Join(root, e.Name()), total, depth+1)
		} else {
			*total += info.Size()
		}
	}
}

// ---------- RAM usage tracking ----------

func getAccountUsernames(db *sql.DB) map[int]string {
	rows, err := db.Query("SELECT id, username FROM accounts WHERE status = 'active'")
	if err != nil {
		log.Printf("[RAM] getAccountUsernames query failed: %v", err)
		return nil
	}
	defer rows.Close()
	usernames := make(map[int]string)
	for rows.Next() {
		var id int
		var username string
		if rows.Scan(&id, &username) == nil {
			usernames[id] = username
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[RAM] getAccountUsernames rows iteration error: %v", err)
	}
	return usernames
}

func getEffectiveRAMLimit(db *sql.DB, accountID int) int {
	var pkgLimit, accountLimit int
	if err := db.QueryRow(`SELECT COALESCE(p.ram_limit_mb, 0) FROM accounts a
		JOIN packages p ON a.package_id = p.id WHERE a.id = ?`, accountID).Scan(&pkgLimit); err != nil {
		log.Printf("[RAM] getEffectiveRAMLimit package query failed for account %d: %v", accountID, err)
	}
	if err := db.QueryRow("SELECT COALESCE(ram_limit_mb, 0) FROM accounts WHERE id = ?", accountID).Scan(&accountLimit); err != nil {
		log.Printf("[RAM] getEffectiveRAMLimit account query failed for account %d: %v", accountID, err)
	}
	if accountLimit > 0 {
		return accountLimit
	}
	return pkgLimit
}

// buildUIDToUsernameCache reads /etc/passwd once and returns a map of UID -> username.
func buildUIDToUsernameCache() map[int]string {
	cache := make(map[int]string)
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		log.Printf("[RAM] Failed to read /etc/passwd: %v", err)
		return cache
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) >= 3 {
			var u int
			if n, _ := fmt.Sscanf(parts[2], "%d", &u); n == 1 {
				cache[u] = parts[0]
			}
		}
	}
	return cache
}

// calculateAllRAMUsage scans /proc once and returns a map of username -> RAM used MB.
// This is vastly more efficient than per-account /proc scanning.
func calculateAllRAMUsage(uidCache map[int]string) map[string]int {
	usage := make(map[string]int)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		log.Printf("[RAM] Failed to read /proc: %v", err)
		return usage
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid := entry.Name()
		if len(pid) == 0 || pid[0] < '0' || pid[0] > '9' {
			continue
		}
		statusPath := "/proc/" + pid + "/status"
		data, err := os.ReadFile(statusPath)
		if err != nil {
			continue
		}
		var procUID, vmRSSKB int
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "Uid:") {
				fmt.Sscanf(line, "Uid:\t%d", &procUID)
			} else if strings.HasPrefix(line, "VmRSS:") {
				fmt.Sscanf(line, "VmRSS:\t%d kB", &vmRSSKB)
			}
		}
		if procUID > 0 && vmRSSKB > 0 {
			if username, ok := uidCache[procUID]; ok {
				usage[username] += vmRSSKB
			}
		}
	}
	// Convert KB to MB for all entries
	for username, kb := range usage {
		usage[username] = kb / 1024
	}
	return usage
}

func startRAMTracker(db *sql.DB) {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for range ticker.C {
			accounts := getAccountUsernames(db)
			if len(accounts) == 0 {
				continue
			}
			uidCache := buildUIDToUsernameCache()
			allUsage := calculateAllRAMUsage(uidCache)
			for id, username := range accounts {
				mb := allUsage[username]
				if _, err := db.Exec("UPDATE accounts SET ram_used_mb = ? WHERE id = ?", mb, id); err != nil {
					log.Printf("[RAM] Failed to update ram_used_mb for account %d (%s): %v", id, username, err)
				}
			}
		}
	}()
}

func checkRAMUnderLimit(db *sql.DB, accountID int) (int, int) {
	limit := getEffectiveRAMLimit(db, accountID)
	if limit <= 0 {
		return 0, 0 // unlimited
	}
	var used int
	if err := db.QueryRow("SELECT COALESCE(ram_used_mb, 0) FROM accounts WHERE id = ?", accountID).Scan(&used); err != nil {
		log.Printf("[RAM] checkRAMUnderLimit query failed for account %d: %v", accountID, err)
	}
	return used, limit
}

func isRAMExceeded(db *sql.DB, accountID int) bool {
	used, limit := checkRAMUnderLimit(db, accountID)
	if limit <= 0 {
		return false
	}
	return used >= limit
}

func ramLimitWarningHandler(db *sql.DB, accountID int) string {
	used, limit := checkRAMUnderLimit(db, accountID)
	if limit <= 0 {
		return ""
	}
	if used >= limit {
		return "Your account has reached its allocated RAM limit. Hosting services may be restricted until your resource allocation is upgraded. Please contact your hosting administrator or support team."
	}
	if limit > 0 && used >= (limit*95/100) {
		return "You are approaching your allocated RAM limit. If your usage reaches the configured limit, your hosting services may be restricted. Please contact your hosting administrator or support team to upgrade your resource allocation."
	}
	return ""
}

func addFileToZip(zw *zip.Writer, filePath, name string) {
	f, err := os.Open(filePath)
	if err != nil {
		log.Printf("addFileToZip: failed to open %s: %v", filePath, err)
		return
	}
	defer f.Close()
	w, err := zw.Create(name)
	if err != nil {
		log.Printf("addFileToZip: failed to create zip entry %s: %v", name, err)
		return
	}
	io.Copy(w, f)
}

func addDirToZip(zw *zip.Writer, dirPath, baseName string) {
	filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dirPath, path)
		if d.IsDir() {
			if rel != "." {
				zw.Create(baseName + "/" + rel + "/")
			}
			return nil
		}
		data, _ := os.ReadFile(path)
		w, _ := zw.Create(baseName + "/" + rel)
		w.Write(data)
		return nil
	})
}

// --- Child DB Manager ---

func childDbRoutes(r chi.Router, db *sql.DB, jwtManager *auth.JWTManager) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		rows, err := db.Query("SELECT id, db_name, db_user, host, size_mb FROM child_databases WHERE account_id = ?", c.AccountID)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()
		type DB struct {
			ID     int    `json:"id"`
			Name   string `json:"db_name"`
			User   string `json:"db_user"`
			Host   string `json:"host"`
			SizeMB int    `json:"size_mb"`
		}
		dbs := make([]DB, 0)
		for rows.Next() {
			var d DB
			rows.Scan(&d.ID, &d.Name, &d.User, &d.Host, &d.SizeMB)
			dbs = append(dbs, d)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[DATABASES] rows iteration error: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		jsonResp(w, 200, dbs)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		if isRAMExceeded(db, c.AccountID) {
			jsonError(w, 429, "RAM limit exceeded. New database creation is temporarily blocked. Please contact your hosting administrator to upgrade your resource allocation.")
			return
		}

		var req struct {
			DBName string `json:"db_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		if req.DBName == "" {
			jsonError(w, 400, "database name required")
			return
		}

		// Enforce max_db limit from the hosting package
		var maxDB, currentDB int
		err := db.QueryRow(`SELECT p.max_db FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`,
			c.AccountID).Scan(&maxDB)
		if err != nil {
			jsonError(w, 500, "failed to load package limits")
			return
		}
		db.QueryRow("SELECT COUNT(*) FROM child_databases WHERE account_id = ?", c.AccountID).Scan(&currentDB)
		if currentDB >= maxDB {
			jsonError(w, 403, fmt.Sprintf("database limit reached (%d/%d)", currentDB, maxDB))
			return
		}

		prefixedName := c.Username + "_" + req.DBName
		result, err := db.Exec(`INSERT INTO child_databases (account_id, db_name, db_user) VALUES (?, ?, ?)`,
			c.AccountID, prefixedName, c.Username+"_u")
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		id, _ := result.LastInsertId()
		jsonResp(w, 201, map[string]interface{}{
			"id": id, "db_name": prefixedName, "db_user": c.Username + "_u", "status": "created",
		})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		result, err := db.Exec("DELETE FROM child_databases WHERE id = ? AND account_id = ?", id, c.AccountID)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			jsonError(w, 404, "database not found")
			return
		}
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	// phpMyAdmin SSO — one-click login with signed token
	r.Get("/phpmyadmin", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		dbID := r.URL.Query().Get("db_id")
		var dbName, dbUser, dbPassword, host string
		err := db.QueryRow("SELECT db_name, db_user, COALESCE(host,'localhost') FROM child_databases WHERE id = ? AND account_id = ?",
			dbID, c.AccountID).Scan(&dbName, &dbUser, &host)
		if err != nil {
			jsonError(w, 404, "database not found")
			return
		}
		// Look up the MySQL password so phpMyAdmin can authenticate as this
		// specific database user — otherwise it falls back to the shared admin
		// user and exposes every database on the server.
		db.QueryRow("SELECT COALESCE(password,'') FROM db_users WHERE username = ? AND account_id = ?",
			dbUser, c.AccountID).Scan(&dbPassword)

		// Generate a one-time token (crypto random, stored server-side)
		raw := make([]byte, 32)
		rand.Read(raw)
		token := hex.EncodeToString(raw)

		// Session key issued after token validation
		sk := make([]byte, 32)
		rand.Read(sk)
		sessionKey := hex.EncodeToString(sk)

		// Store token — single-use, expires in 60s
		_, err = db.Exec(`INSERT INTO pma_tokens (token_hash, account_id, db_name, db_user, db_password, host, used, session_key, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, 0, ?, datetime('now', '+60 seconds'))`,
			token, c.AccountID, dbName, dbUser, dbPassword, host, sessionKey)
		if err != nil {
			jsonError(w, 500, "failed to create token")
			return
		}

		// Build URL using the configured public host (for remote access)
		publicHost := os.Getenv("OWP_PUBLIC_HOST")
		if publicHost == "" {
			// Fallback to request host (works for local dev)
			publicHost = r.Host
		}
		proto := "http"
		if r.TLS != nil || strings.HasPrefix(publicHost, "https://") {
			proto = "https"
			publicHost = strings.TrimPrefix(publicHost, "https://")
		}
		publicHost = strings.TrimPrefix(publicHost, "http://")
		proxyURL := fmt.Sprintf("%s://%s/pma/%s/", proto, publicHost, token)

		jsonResp(w, 200, map[string]interface{}{
			"url":        proxyURL,
			"token":      token,
			"expires_in": 60,
			"note":       "One-time use token. Invalidated after first access.",
		})
	})

	// --- Database Users ---
	r.Route("/users", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil {
				jsonError(w, 401, "unauthorized")
				return
			}
			rows, err := db.Query(`SELECT u.id, u.username, u.created_at,
				(SELECT GROUP_CONCAT(a.db_id) FROM db_user_assignments a WHERE a.user_id = u.id) as db_ids
				FROM db_users u WHERE u.account_id = ? ORDER BY u.username`, c.AccountID)
			if err != nil {
				jsonResp(w, 200, []interface{}{})
				return
			}
			defer rows.Close()
			users := make([]map[string]interface{}, 0)
			for rows.Next() {
				var id int
				var username, created string
				var dbIDs sql.NullString
				rows.Scan(&id, &username, &created, &dbIDs)
				users = append(users, map[string]interface{}{
					"id": id, "username": username, "created_at": created,
					"assigned_dbs": dbIDs.String,
				})
			}
			if err := rows.Err(); err != nil {
				log.Printf("[DBUSERS] rows iteration error: %v", err)
				jsonResp(w, 200, []interface{}{})
				return
			}
			jsonResp(w, 200, users)
		})

		r.Post("/", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil {
				jsonError(w, 401, "unauthorized")
				return
			}

			if isRAMExceeded(db, c.AccountID) {
				jsonError(w, 429, "RAM limit exceeded. New database user creation is temporarily blocked. Please contact your hosting administrator to upgrade your resource allocation.")
				return
			}

			var req struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonError(w, 400, "invalid request body")
				return
			}
			if req.Username == "" || req.Password == "" {
				jsonError(w, 400, "username and password required")
				return
			}

			// Enforce max_db users limit? Use max_db from package as user limit too
			var maxDB, currentUsers int
			db.QueryRow(`SELECT p.max_db FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`,
				c.AccountID).Scan(&maxDB)
			db.QueryRow("SELECT COUNT(*) FROM db_users WHERE account_id = ?", c.AccountID).Scan(&currentUsers)
			if currentUsers >= maxDB {
				jsonError(w, 403, fmt.Sprintf("database user limit reached (%d/%d)", currentUsers, maxDB))
				return
			}

			prefixedUser := c.Username + "_" + req.Username
			result, err := db.Exec(`INSERT INTO db_users (account_id, username, password) VALUES (?, ?, ?)`,
				c.AccountID, prefixedUser, req.Password)
			if err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			id, _ := result.LastInsertId()
			jsonResp(w, 201, map[string]interface{}{"id": id, "username": req.Username, "status": "created"})
		})

		r.Put("/{id}/password", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			id, _ := strconv.Atoi(chi.URLParam(r, "id"))
			var req struct{ Password string `json:"password"` }
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonError(w, 400, "invalid request body")
				return
			}
			result, err := db.Exec("UPDATE db_users SET password = ? WHERE id = ? AND account_id = ?", req.Password, id, c.AccountID)
			if err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			affected, _ := result.RowsAffected()
			if affected == 0 {
				jsonError(w, 404, "user not found")
				return
			}
			jsonResp(w, 200, map[string]string{"status": "password updated"})
		})

		r.Post("/{id}/assign", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil {
				jsonError(w, 401, "unauthorized")
				return
			}
			uid, _ := strconv.Atoi(chi.URLParam(r, "id"))
			var req struct{ DBID int `json:"db_id"`; Privileges string `json:"privileges"` }
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonError(w, 400, "invalid request body")
				return
			}
			if req.Privileges == "" {
				req.Privileges = "ALL PRIVILEGES"
			}
			db.Exec(`INSERT OR REPLACE INTO db_user_assignments (user_id, db_id, privileges) VALUES (?, ?, ?)`,
				uid, req.DBID, req.Privileges)
			jsonResp(w, 200, map[string]string{"status": "assigned"})
		})

		r.Post("/{id}/unassign", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil {
				jsonError(w, 401, "unauthorized")
				return
			}
			uid, _ := strconv.Atoi(chi.URLParam(r, "id"))
			var req struct{ DBID int `json:"db_id"` }
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonError(w, 400, "invalid request body")
				return
			}
			result, err := db.Exec("DELETE FROM db_user_assignments WHERE user_id = ? AND db_id = ?", uid, req.DBID)
			if err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			affected, _ := result.RowsAffected()
			if affected == 0 {
				jsonError(w, 404, "assignment not found")
				return
			}
			jsonResp(w, 200, map[string]string{"status": "unassigned"})
		})

		r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil {
				jsonError(w, 401, "unauthorized")
				return
			}
			id, _ := strconv.Atoi(chi.URLParam(r, "id"))
			_, err := db.Exec("DELETE FROM db_user_assignments WHERE user_id = ?", id)
			if err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			db.Exec("DELETE FROM db_users WHERE id = ? AND account_id = ?", id, c.AccountID)
			jsonResp(w, 200, map[string]string{"status": "deleted"})
		})
	})

	// --- Remote Database Access ---
	r.Route("/remote", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil {
				jsonError(w, 401, "unauthorized")
				return
			}
			rows, err := db.Query("SELECT id, db_name, remote_access FROM child_databases WHERE account_id = ?", c.AccountID)
			if err != nil {
				jsonResp(w, 200, []interface{}{})
				return
			}
			defer rows.Close()
			remotes := make([]map[string]interface{}, 0)
			for rows.Next() {
				var id int
				var name, access string
				rows.Scan(&id, &name, &access)
				remotes = append(remotes, map[string]interface{}{"id": id, "db_name": name, "remote_access": access})
			}
			if err := rows.Err(); err != nil {
				log.Printf("[REMOTE] rows iteration error: %v", err)
				jsonResp(w, 200, []interface{}{})
				return
			}
			jsonResp(w, 200, remotes)
		})

		r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			id, _ := strconv.Atoi(chi.URLParam(r, "id"))
			var req struct{ RemoteAccess string `json:"remote_access"` }
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonError(w, 400, "invalid request body")
				return
			}
			result, err := db.Exec("UPDATE child_databases SET remote_access = ? WHERE id = ? AND account_id = ?",
				req.RemoteAccess, id, c.AccountID)
			if err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			affected, _ := result.RowsAffected()
			if affected == 0 {
				jsonError(w, 404, "database not found")
				return
			}
			jsonResp(w, 200, map[string]string{"status": "updated"})
		})
	})
}

func childDomainRoutes(r chi.Router, db *sql.DB) {
	getHomeDir := func(accountID int) string {
		var h string
		db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", accountID).Scan(&h)
		return h
	}

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		rows, err := db.Query(`SELECT d.id, d.domain, d.type, COALESCE(d.doc_root,''), COALESCE(d.ssl_enabled,0), d.created_at,
			COALESCE((
				SELECT
					CASE
						WHEN COUNT(*) > 0 AND MAX(s.status) = 'issued' AND MAX(COALESCE(s.expires_at,'')) > datetime('now') THEN 'issued'
						WHEN COUNT(*) > 0 AND MAX(s.status) = 'issuing' THEN 'issuing'
						WHEN COUNT(*) > 0 AND MAX(s.status) = 'issued' AND MAX(COALESCE(s.expires_at,'')) <= datetime('now') THEN 'expired'
						WHEN COUNT(*) > 0 AND MAX(s.status) = 'failed' THEN 'failed'
						ELSE 'none'
					END
				FROM ssl_certs s WHERE s.domain = d.domain
			), 'none') AS ssl_status
			FROM domains d WHERE d.account_id = ? ORDER BY d.type, d.created_at DESC`, c.AccountID)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		domains := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id, ssl int
			var domain, typ, docRoot, created, certStatus string
			rows.Scan(&id, &domain, &typ, &docRoot, &ssl, &created, &certStatus)
			domains = append(domains, map[string]interface{}{
				"id": id, "domain": domain, "type": typ,
				"doc_root": docRoot, "ssl_enabled": ssl == 1,
				"ssl_status": certStatus, "created_at": created,
			})
		}
		if err := rows.Err(); err != nil {
			log.Printf("[DOMAINS] rows iteration error: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		jsonResp(w, 200, domains)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		if isRAMExceeded(db, c.AccountID) {
			jsonError(w, 429, "RAM limit exceeded. New domain creation is temporarily blocked. Please contact your hosting administrator to upgrade your resource allocation.")
			return
		}

		var req struct {
			Domain string `json:"domain"`
			Type   string `json:"type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		if req.Domain == "" {
			jsonError(w, 400, "domain name required")
			return
		}
		if req.Type == "" {
			req.Type = "addon"
		}

		// Enforce limits from package (separate checks for domains vs subdomains)
		var maxDomains, maxSubdomains, currentDomains, currentSubdomains int
		err := db.QueryRow(`SELECT p.max_domains, p.max_subdomains FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`,
			c.AccountID).Scan(&maxDomains, &maxSubdomains)
		if err != nil {
			jsonError(w, 500, "failed to load limits")
			return
		}
		// Count non-subdomain domains (addon, parked, primary)
		db.QueryRow("SELECT COUNT(*) FROM domains WHERE account_id = ? AND type != 'subdomain'", c.AccountID).Scan(&currentDomains)
		// Count subdomains separately
		db.QueryRow("SELECT COUNT(*) FROM domains WHERE account_id = ? AND type = 'subdomain'", c.AccountID).Scan(&currentSubdomains)

		if req.Type == "subdomain" {
			if currentSubdomains >= maxSubdomains {
				jsonError(w, 403, fmt.Sprintf("subdomain limit reached (%d/%d)", currentSubdomains, maxSubdomains))
				return
			}
		} else {
			if currentDomains >= maxDomains {
				jsonError(w, 403, fmt.Sprintf("domain limit reached (%d/%d)", currentDomains, maxDomains))
				return
			}
		}

		homeDir := getHomeDir(c.AccountID)
		// Primary uses public_html; addon/subdomain get own dir at home level
		var docRoot string
		if req.Type == "primary" {
			docRoot = homeDir + "/public_html"
		} else {
			safeDomain := sanitizeDomain(req.Domain)
			cleanDomain := strings.TrimPrefix(safeDomain, "www.")
			docRoot = homeDir + "/" + cleanDomain
		}
		os.MkdirAll(docRoot, 0755)

		// Create default index.html for the domain
		if req.Type != "parked" {
			indexContent := fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>Welcome to %s</title>
<style>body{font-family:Arial;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#f0f4f8}
.card{background:#fff;padding:3rem;border-radius:12px;box-shadow:0 4px 24px rgba(0,0,0,.1);text-align:center;max-width:500px}
h1{color:#1a1a2e;margin-bottom:.5rem}p{color:#555}</style></head>
<body><div class="card"><h1>%s</h1><p>Site hosted by OpenWebPanel</p></div></body></html>`, req.Domain, req.Domain)
			os.WriteFile(docRoot+"/index.html", []byte(indexContent), 0644)
		}

		result, err := db.Exec(`INSERT INTO domains (account_id, domain, type, doc_root) VALUES (?, ?, ?, ?)`,
			c.AccountID, req.Domain, req.Type, docRoot)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		id, _ := result.LastInsertId()

		auditLog(db, r, "domain.create", map[string]interface{}{"domain": req.Domain, "type": req.Type, "account_id": c.AccountID})
		// Write Nginx vhost for the domain with account's PHP version
		var phpSocket string
		db.QueryRow(`SELECT COALESCE(pv.socket_path, '') FROM account_php_version apv
			JOIN php_versions pv ON pv.id = apv.php_version_id
			WHERE apv.account_id = ?`, c.AccountID).Scan(&phpSocket)
		if err := writeNginxVhost(sanitizeDomain(req.Domain), docRoot, "", phpSocket); err != nil {
			log.Printf("[DOMAINS] Failed to write vhost for %s: %v", req.Domain, err)
			// Domain is in DB but vhost failed — report warning to user
			jsonResp(w, 201, map[string]interface{}{
				"id": id, "domain": req.Domain, "type": req.Type,
				"doc_root": docRoot, "status": "created",
				"warning": "Domain added but nginx vhost could not be created. Check server logs.",
			})
			return
		}
		reloadNginx()

		jsonResp(w, 201, map[string]interface{}{
			"id": id, "domain": req.Domain, "type": req.Type,
			"doc_root": docRoot, "status": "created",
		})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		// Capture domain name before deleting
		var delDomain string
		db.QueryRow("SELECT domain FROM domains WHERE id = ? AND account_id = ?", id, c.AccountID).Scan(&delDomain)
		if delDomain == "" {
			jsonError(w, 404, "domain not found")
			return
		}

		auditLog(db, r, "domain.delete", map[string]interface{}{"domain": delDomain, "account_id": c.AccountID})
		db.Exec("DELETE FROM domains WHERE id = ? AND account_id = ?", id, c.AccountID)

		// Only remove vhost if no other active account uses this domain
		var activeCount int
		db.QueryRow(`SELECT COUNT(*) FROM domains d
			JOIN accounts a ON a.id = d.account_id
			WHERE d.domain = ? AND a.status = 'active'`, delDomain).Scan(&activeCount)
		needsReload := false
		if activeCount == 0 {
			if err := removeNginxVhost(sanitizeDomain(delDomain)); err != nil {
				log.Printf("[DOMAINS] Failed to remove vhost for %s: %v", delDomain, err)
			} else {
				log.Printf("[DOMAINS] Removed vhost for %s (no more active owners)", delDomain)
				needsReload = true
			}
		}
		if needsReload {
			reloadNginx()
		}

		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}

// pmaProxyHandler creates a reverse proxy to phpMyAdmin, protected by one-time tokens.
// phpMyAdmin runs on localhost and is NEVER exposed directly to the internet.
func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return len(s) > 0
}

func setPmaAuthHeaders(r *http.Request, database *sql.DB, sessionKey string) {
	var dbUser, dbPassword string
	database.QueryRow("SELECT COALESCE(db_user,''), COALESCE(db_password,'') FROM pma_tokens WHERE session_key = ?", sessionKey).Scan(&dbUser, &dbPassword)
	if dbUser != "" && dbPassword != "" {
		// Authenticate as the specific database user so phpMyAdmin only shows
		// databases that this user has privileges on (one per child account).
		r.SetBasicAuth(dbUser, dbPassword)
	} else {
		// Fallback: should not happen during normal flow, but prevents a
		// full server-wide database listing by refusing credentials.
		r.SetBasicAuth("", "")
	}
}

func pmaProxyHandler(database *sql.DB) http.HandlerFunc {
	pmaTarget := os.Getenv("OWP_PHPMYADMIN_PORT")
	if pmaTarget == "" {
		pmaTarget = "http://127.0.0.1:8080"
	}
	target, _ := url.Parse(pmaTarget)
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Clean up expired tokens periodically
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			database.Exec("DELETE FROM pma_tokens WHERE expires_at < datetime('now')")
		}
	}()

	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/pma")

		// 1. Check for valid session cookie (allows phpMyAdmin internal navigation)
		if cookie, cookieErr := r.Cookie("pma_session"); cookieErr == nil && cookie != nil {
			var count int
			database.QueryRow("SELECT COUNT(*) FROM pma_tokens WHERE session_key = ? AND account_id > 0",
				cookie.Value).Scan(&count)
			if count > 0 {
				// Strip token prefix from URL path (64 hex chars) for session-based access
				cleanPath := path
				if len(path) > 1 {
					firstSlash := strings.Index(path[1:], "/")
					var firstSeg string
					if firstSlash >= 0 {
						firstSeg = path[1 : firstSlash+1]
					} else {
						firstSeg = path[1:]
					}
					if len(firstSeg) == 64 && isHex(firstSeg) {
						if firstSlash >= 0 {
							cleanPath = path[firstSlash+1:]
						} else {
							cleanPath = "/"
						}
					}
				}
				if cleanPath == "" || cleanPath == "/" {
					cleanPath = "/index.php"
				}
				r.URL.Path = cleanPath
				// Auto-login: inject DB credentials via HTTP Basic Auth
				setPmaAuthHeaders(r, database, cookie.Value)
				proxy.ServeHTTP(w, r)
				return
			}
		}

		// 2. No valid session — extract and validate one-time token from URL
		parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)
		if len(parts) == 0 || parts[0] == "" {
			http.Error(w, "missing token", http.StatusForbidden)
			return
		}
		token := parts[0]

		var id, used int
		var sessionKey, expiresAt string
		err := database.QueryRow(`SELECT id, used, session_key, expires_at FROM pma_tokens
			WHERE token_hash = ? AND expires_at > datetime('now')`, token).Scan(&id, &used, &sessionKey, &expiresAt)
		if err != nil {
			http.Error(w, "invalid or expired token", http.StatusForbidden)
			return
		}
		if used == 1 {
			http.Error(w, "token already used — get a new link from the control panel", http.StatusForbidden)
			return
		}

		// Mark token as used (one-time enforcement)
		database.Exec("UPDATE pma_tokens SET used = 1 WHERE id = ?", id)

		// Issue session cookie for subsequent requests
		http.SetCookie(w, &http.Cookie{
			Name:     "pma_session",
			Value:    sessionKey,
			Path:     "/pma/",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   3600,
		})

		// Proxy to phpMyAdmin, stripping the token prefix
		rest := "/"
		if len(parts) > 1 {
			rest = "/" + parts[1]
		}
		if rest == "/" || rest == "" {
			rest = "/index.php"
		}
		r.URL.Path = rest
		// Auto-login: inject credentials on first access too
		setPmaAuthHeaders(r, database, sessionKey)
		proxy.ServeHTTP(w, r)
	}
}

// --- Server Settings ---

func ticketRoutes(r chi.Router, db *sql.DB) {
	const maxSubjectLen = 200
	const maxMessageLen = 50000
	const defaultPageSize = 50

	// List tickets with pagination
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		limit := defaultPageSize
		offset := 0
		if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
			limit = l
		}
		if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
			offset = o
		}

		args := []interface{}{limit, offset}
		whereClause := ""
		if c.Scope == "child" {
			whereClause = "WHERE t.account_id = ?"
			args = append([]interface{}{c.AccountID}, args...)
		}

		rows, err := db.Query(`SELECT t.id, t.account_id, COALESCE(a.username,'') as username,
			t.subject, t.status, t.created_at, t.updated_at,
			(SELECT COUNT(*) FROM ticket_messages WHERE ticket_id = t.id) as msg_count
			FROM support_tickets t LEFT JOIN accounts a ON t.account_id = a.id `+whereClause+` 
			ORDER BY t.updated_at DESC LIMIT ? OFFSET ?`, args...)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		tickets := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id, accID, msgCount int
			var subject, status, created, updated, username string
			if err := rows.Scan(&id, &accID, &username, &subject, &status, &created, &updated, &msgCount); err != nil {
				continue
			}
			tickets = append(tickets, map[string]interface{}{
				"id": id, "account_id": accID, "username": username, "subject": subject, "status": status,
				"created_at": created, "updated_at": updated, "message_count": msgCount,
			})
		}
		if err := rows.Err(); err != nil {
			jsonResp(w, 200, tickets)
			return
		}

		// Get total count for pagination
		var total int
		countQuery := "SELECT COUNT(*) FROM support_tickets t " + whereClause
		countArgs := args[:len(args)-2] // strip limit/offset
		db.QueryRow(countQuery, countArgs...).Scan(&total)

		jsonResp(w, 200, map[string]interface{}{
			"tickets": tickets,
			"total":   total,
			"limit":   limit,
			"offset":  offset,
		})
	})

	// Create ticket
	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		var req struct {
			Subject string `json:"subject"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		req.Subject = strings.TrimSpace(req.Subject)
		req.Message = strings.TrimSpace(req.Message)
		if req.Subject == "" || req.Message == "" {
			jsonError(w, 400, "subject and message required")
			return
		}
		if len(req.Subject) > maxSubjectLen {
			jsonError(w, 400, "subject too long (max 200 characters)")
			return
		}
		if len(req.Message) > maxMessageLen {
			jsonError(w, 400, "message too long (max 50000 characters)")
			return
		}

		result, err := db.Exec(`INSERT INTO support_tickets (account_id, subject) VALUES (?, ?)`, c.AccountID, req.Subject)
		if err != nil {
			jsonError(w, 500, "failed to create ticket")
			return
		}
		ticketID, err := result.LastInsertId()
		if err != nil || ticketID == 0 {
			jsonError(w, 500, "failed to create ticket")
			return
		}
		_, err = db.Exec(`INSERT INTO ticket_messages (ticket_id, sender_type, sender_id, message) VALUES (?, 'user', ?, ?)`,
			ticketID, c.AccountID, req.Message)
		if err != nil {
			db.Exec("DELETE FROM support_tickets WHERE id = ?", ticketID)
			jsonError(w, 500, "failed to create ticket message")
			return
		}
		jsonResp(w, 201, map[string]interface{}{"id": ticketID, "status": "open"})
	})

	// Get ticket messages with pagination
	r.Get("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		id, err := strconv.Atoi(chi.URLParam(r, "id"))
		if err != nil || id <= 0 {
			jsonError(w, 400, "invalid ticket id")
			return
		}

		// Verify ticket exists and check ownership
		var accID int
		err = db.QueryRow("SELECT account_id FROM support_tickets WHERE id = ?", id).Scan(&accID)
		if err != nil {
			jsonError(w, 404, "ticket not found")
			return
		}
		if c.Scope == "child" && accID != c.AccountID {
			jsonError(w, 403, "access denied")
			return
		}

		limit := 200
		offset := 0
		if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 500 {
			limit = l
		}
		if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
			offset = o
		}

		rows, err := db.Query(`SELECT m.id, m.sender_type, m.sender_id,
			CASE WHEN m.sender_type='user' THEN COALESCE(a.username,'(deleted)')
			     WHEN m.sender_type='admin' THEN COALESCE(ad.username,'(deleted)')
			     ELSE '' END as sender_name,
			m.message, m.created_at
			FROM ticket_messages m
			LEFT JOIN accounts a ON m.sender_type='user' AND m.sender_id=a.id
			LEFT JOIN admins ad ON m.sender_type='admin' AND m.sender_id=ad.id
			WHERE m.ticket_id = ? ORDER BY m.created_at ASC LIMIT ? OFFSET ?`, id, limit, offset)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		msgs := make([]map[string]interface{}, 0)
		for rows.Next() {
			var mid, sid int
			var stype, sname, msg, created string
			if err := rows.Scan(&mid, &stype, &sid, &sname, &msg, &created); err != nil {
				continue
			}
			msgs = append(msgs, map[string]interface{}{"id": mid, "sender_type": stype, "sender_id": sid, "sender_name": sname, "message": msg, "created_at": created})
		}
		if err := rows.Err(); err != nil {
			jsonResp(w, 200, msgs)
			return
		}

		var total int
		db.QueryRow("SELECT COUNT(*) FROM ticket_messages WHERE ticket_id = ?", id).Scan(&total)

		jsonResp(w, 200, map[string]interface{}{
			"messages": msgs,
			"total":    total,
			"limit":    limit,
			"offset":   offset,
		})
	})

	// Reply to ticket
	r.Post("/{id}/reply", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		id, err := strconv.Atoi(chi.URLParam(r, "id"))
		if err != nil || id <= 0 {
			jsonError(w, 400, "invalid ticket id")
			return
		}

		var ownerID int
		err = db.QueryRow("SELECT account_id FROM support_tickets WHERE id = ?", id).Scan(&ownerID)
		if err != nil {
			jsonError(w, 404, "ticket not found")
			return
		}
		if c.Scope == "child" && ownerID != c.AccountID {
			jsonError(w, 403, "access denied")
			return
		}

		var req struct{ Message string `json:"message"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		req.Message = strings.TrimSpace(req.Message)
		if req.Message == "" {
			jsonError(w, 400, "message cannot be empty")
			return
		}
		if len(req.Message) > maxMessageLen {
			jsonError(w, 400, "message too long")
			return
		}

		senderType := "user"
		senderID := c.AccountID
		newStatus := "open"
		if c.Scope == "parent" {
			senderType = "admin"
			senderID = c.UserID
			newStatus = "replied"
		}

		_, err = db.Exec(`INSERT INTO ticket_messages (ticket_id, sender_type, sender_id, message) VALUES (?, ?, ?, ?)`,
			id, senderType, senderID, req.Message)
		if err != nil {
			jsonError(w, 500, "failed to send reply")
			return
		}
		db.Exec("UPDATE support_tickets SET status = ?, updated_at = datetime('now') WHERE id = ?", newStatus, id)
		jsonResp(w, 200, map[string]string{"status": newStatus})
	})

	// Update ticket status (admin only)
	r.Put("/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil || c.Scope != "parent" {
			jsonError(w, 403, "access denied")
			return
		}
		id, err := strconv.Atoi(chi.URLParam(r, "id"))
		if err != nil || id <= 0 {
			jsonError(w, 400, "invalid ticket id")
			return
		}
		var req struct{ Status string `json:"status"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		validStatuses := map[string]bool{"open": true, "closed": true, "replied": true, "pending": true}
		if !validStatuses[req.Status] {
			jsonError(w, 400, "invalid status")
			return
		}
		result, err := db.Exec("UPDATE support_tickets SET status = ?, updated_at = datetime('now') WHERE id = ?", req.Status, id)
		if err != nil {
			jsonError(w, 500, "failed to update status")
			return
		}
		if n, _ := result.RowsAffected(); n == 0 {
			jsonError(w, 404, "ticket not found")
			return
		}
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	// Delete ticket (admin only)
	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil || c.Scope != "parent" {
			jsonError(w, 403, "access denied")
			return
		}
		id, err := strconv.Atoi(chi.URLParam(r, "id"))
		if err != nil || id <= 0 {
			jsonError(w, 400, "invalid ticket id")
			return
		}

		// Verify ticket exists
		var exists int
		db.QueryRow("SELECT COUNT(*) FROM support_tickets WHERE id = ?", id).Scan(&exists)
		if exists == 0 {
			jsonError(w, 404, "ticket not found")
			return
		}

		// CASCADE should handle messages, but ensure with explicit delete
		db.Exec("DELETE FROM ticket_messages WHERE ticket_id = ?", id)
		db.Exec("DELETE FROM support_tickets WHERE id = ?", id)
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	// Delete individual message (admin only)
	r.Delete("/messages/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil || c.Scope != "parent" {
			jsonError(w, 403, "access denied")
			return
		}
		id, err := strconv.Atoi(chi.URLParam(r, "id"))
		if err != nil || id <= 0 {
			jsonError(w, 400, "invalid message id")
			return
		}
		result, err := db.Exec("DELETE FROM ticket_messages WHERE id = ?", id)
		if err != nil {
			jsonError(w, 500, "failed to delete message")
			return
		}
		if n, _ := result.RowsAffected(); n == 0 {
			jsonError(w, 404, "message not found")
			return
		}
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	// Edit message (admin only)
	r.Put("/messages/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil || c.Scope != "parent" {
			jsonError(w, 403, "access denied")
			return
		}
		id, err := strconv.Atoi(chi.URLParam(r, "id"))
		if err != nil || id <= 0 {
			jsonError(w, 400, "invalid message id")
			return
		}
		var req struct{ Message string `json:"message"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		req.Message = strings.TrimSpace(req.Message)
		if req.Message == "" {
			jsonError(w, 400, "message cannot be empty")
			return
		}
		if len(req.Message) > maxMessageLen {
			jsonError(w, 400, "message too long")
			return
		}
		result, err := db.Exec("UPDATE ticket_messages SET message = ? WHERE id = ?", req.Message, id)
		if err != nil {
			jsonError(w, 500, "failed to update message")
			return
		}
		if n, _ := result.RowsAffected(); n == 0 {
			jsonError(w, 404, "message not found")
			return
		}
		jsonResp(w, 200, map[string]string{"status": "edited"})
	})
}

func settingsRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query("SELECT key_name, value FROM server_config ORDER BY key_name")
		if err != nil {
			jsonResp(w, 200, map[string]string{})
			return
		}
		defer rows.Close()
		cfg := make(map[string]string)
		for rows.Next() {
			var k, v string
			rows.Scan(&k, &v)
			cfg[k] = v
		}
		if err := rows.Err(); err != nil {
			log.Printf("[SETTINGS] rows iteration error: %v", err)
			jsonResp(w, 200, map[string]string{})
			return
		}
		jsonResp(w, 200, cfg)
	})

	r.Put("/", func(w http.ResponseWriter, r *http.Request) {
		var updates map[string]string
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		for k, v := range updates {
			db.Exec(`INSERT OR REPLACE INTO server_config (key_name, value, updated_at) VALUES (?, ?, datetime('now'))`, k, v)
		}
		jsonResp(w, 200, map[string]string{"status": "saved"})
	})

	r.Get("/upload-limit", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		var defaultLimit, maxLimit int
		db.QueryRow("SELECT COALESCE((SELECT value FROM server_config WHERE key_name = 'default_upload_limit_mb'), '2048')").Scan(&defaultLimit)
		db.QueryRow("SELECT COALESCE((SELECT value FROM server_config WHERE key_name = 'max_upload_limit_mb'), '5120')").Scan(&maxLimit)

		// Check per-account override
		var accountOverride sql.NullInt64
		if c != nil && c.AccountID > 0 {
			db.QueryRow("SELECT disk_used_mb FROM accounts WHERE id = ?", c.AccountID).Scan(&accountOverride)
		}

		jsonResp(w, 200, map[string]interface{}{
			"default_limit_mb": defaultLimit,
			"max_limit_mb":     maxLimit,
			"note":             "Child users can request up to max_limit_mb",
		})
	})
}

// ========== MAIN ==========

func main() {
	dbPath := os.Getenv("OWP_DB_PATH")
	if dbPath == "" {
		dbPath = "./openwebpanel.db"
	}

	database, err := initDB(dbPath)
	if err != nil {
		log.Fatalf("Database init failed: %v", err)
	}
	defer database.Close()

	// Clean up orphaned Nginx vhosts (domains no longer in an active account)
	activeDomains := make(map[string]bool)
	rows, err := database.Query(`SELECT DISTINCT d.domain FROM domains d
		JOIN accounts a ON a.id = d.account_id WHERE a.status = 'active'`)
	if err == nil {
		for rows.Next() {
			var d string
			rows.Scan(&d)
			activeDomains[d] = true
		}
		if err := rows.Err(); err != nil {
			log.Printf("[MAIN] active domains rows iteration error: %v", err)
		}
		rows.Close()
	}
	if entries, err := os.ReadDir(vhostDir); err == nil {
		for _, e := range entries {
			name := strings.TrimSuffix(e.Name(), ".conf")
			if !activeDomains[name] {
				os.Remove(vhostDir + e.Name())
				log.Printf("Removed orphaned vhost: %s", name)
			}
		}
	}

	// Backfill primary domain entries for existing accounts that don't have one
	accRows, err := database.Query(`SELECT id, domain, home_dir FROM accounts`)
	if err == nil {
			type accInfo struct {
				id      int
				domain  string
				homeDir string
			}
			var accounts []accInfo
			for accRows.Next() {
				var a accInfo
				accRows.Scan(&a.id, &a.domain, &a.homeDir)
				accounts = append(accounts, a)
			}
			if err := accRows.Err(); err != nil {
				log.Printf("[MAIN] accounts backfill rows iteration error: %v", err)
			}
			accRows.Close()

			for _, a := range accounts {
				var count int
				database.QueryRow("SELECT COUNT(*) FROM domains WHERE account_id = ? AND type = 'primary'", a.id).Scan(&count)
				if count == 0 {
					database.Exec(`INSERT INTO domains (account_id, domain, type, doc_root) VALUES (?, ?, 'primary', ?)`,
						a.id, a.domain, a.homeDir+"/public_html")
				}
			}
		}

	jwtSecret := os.Getenv("OWP_JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("OWP_JWT_SECRET environment variable is required - set a random 48+ character string")
	}
	jwtManager := auth.NewJWTManager(jwtSecret, 900, 604800)

	corsHandler := cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:*", "http://127.0.0.1:*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	})

	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, 200, map[string]string{"status": "ok", "version": "0.1.0"})
	}

	// ==================== ADMIN ROUTER (:9000) ====================
	adminRouter := chi.NewRouter()
	adminRouter.Use(middleware.Logger)
	adminRouter.Use(middleware.Recoverer)
	adminRouter.Use(middleware.RealIP)
	adminRouter.Use(corsHandler)

	adminRouter.Get("/healthz", healthHandler)

	adminRouter.Route("/api/v1/auth", func(r chi.Router) {
		r.Get("/captcha", captchaHandler(database))
		authRoutes(r, database, jwtManager)
	})

	adminRouter.Route("/api/v1/packages", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		packageRoutes(r, database)
	})

	adminRouter.Route("/api/v1/accounts", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		accountRoutes(r, database)
	})

	adminRouter.Route("/api/v1/stats", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		statsRoutes(r, database)
	})

	adminRouter.Route("/api/v1/server", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		serverRoutes(r)
	})

	adminRouter.Route("/api/v1/settings", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		settingsRoutes(r, database)
	})

	adminRouter.Route("/api/v1/php-versions", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		phpVersionRoutes(r, database)
	})

	adminRouter.Route("/api/v1/bandwidth", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		bandwidthRoutes(r, database)
	})

	adminRouter.Route("/api/v1/submissions", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		submissionRoutes(r, database)
	})

	adminRouter.Route("/api/v1/emails", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		adminEmailRoutes(r, database)
	})

	adminRouter.Route("/api/v1/notifications", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			rows, err := database.Query(`SELECT id, COALESCE(account_id,0), title, message, created_at FROM notifications ORDER BY created_at DESC`)
			if err != nil { jsonResp(w, 200, []interface{}{}); return }
			defer rows.Close()
			type Notification struct {
				ID        int    `json:"id"`
				AccountID int    `json:"account_id"`
				Title     string `json:"title"`
				Message   string `json:"message"`
				CreatedAt string `json:"created_at"`
			}
			notifs := make([]Notification, 0)
			for rows.Next() {
				var n Notification
				rows.Scan(&n.ID, &n.AccountID, &n.Title, &n.Message, &n.CreatedAt)
				notifs = append(notifs, n)
			}
			if err := rows.Err(); err != nil {
				log.Printf("[NOTIFICATIONS] rows iteration error: %v", err)
				jsonResp(w, 200, []interface{}{})
				return
			}
			jsonResp(w, 200, notifs)
		})
		r.Post("/", func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				AccountID *int   `json:"account_id"`
				Title     string `json:"title"`
				Message   string `json:"message"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonError(w, 400, "invalid body"); return
			}
			if req.Title == "" || req.Message == "" {
				jsonError(w, 400, "title and message required"); return
			}
			result, err := database.Exec("INSERT INTO notifications (account_id, title, message) VALUES (?, ?, ?)",
				req.AccountID, req.Title, req.Message)
			if err != nil {
				jsonError(w, 500, err.Error()); return
			}
			id, _ := result.LastInsertId()
			auditLog(database, r, "notification.create", map[string]interface{}{"id": id, "account_id": req.AccountID})
			jsonResp(w, 200, map[string]interface{}{"id": id, "status": "created"})
		})
		r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			database.Exec("DELETE FROM notifications WHERE id = ?", id)
			database.Exec("DELETE FROM notification_reads WHERE notification_id = ?", id)
			jsonResp(w, 200, map[string]string{"status": "deleted"})
		})
	})

	adminRouter.Route("/api/v1/tickets", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		ticketRoutes(r, database)
	})

	adminRouter.Route("/api/v1/security/ips", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil || c.Role != "root" {
				jsonError(w, 403, "access denied")
				return
			}
			rows, err := database.Query(`SELECT id, ip_address, COALESCE(reason,''), COALESCE(blocked_by,''), failed_attempts, created_at, updated_at FROM blocked_ips ORDER BY updated_at DESC`)
			if err != nil { jsonResp(w, 200, []interface{}{}); return }
			defer rows.Close()
			type BlockedIP struct {
				ID             int    `json:"id"`
				IPAddress      string `json:"ip_address"`
				Reason         string `json:"reason"`
				BlockedBy      string `json:"blocked_by"`
				FailedAttempts int    `json:"failed_attempts"`
				CreatedAt      string `json:"created_at"`
				UpdatedAt      string `json:"updated_at"`
			}
			ips := make([]BlockedIP, 0)
			for rows.Next() {
				var b BlockedIP
				rows.Scan(&b.ID, &b.IPAddress, &b.Reason, &b.BlockedBy, &b.FailedAttempts, &b.CreatedAt, &b.UpdatedAt)
				ips = append(ips, b)
			}
			if err := rows.Err(); err != nil {
				log.Printf("[SECURITY] blocked IPs rows iteration error: %v", err)
				jsonResp(w, 200, []interface{}{})
				return
			}
			jsonResp(w, 200, ips)
		})
		r.Post("/{id}/unblock", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil || c.Role != "root" {
				jsonError(w, 403, "access denied")
				return
			}
			id := chi.URLParam(r, "id")
			var ipAddress string
			err := database.QueryRow("SELECT ip_address FROM blocked_ips WHERE id = ?", id).Scan(&ipAddress)
			if err != nil {
				jsonError(w, 404, "blocked IP not found")
				return
			}
			database.Exec("DELETE FROM blocked_ips WHERE id = ?", id)
			database.Exec("DELETE FROM login_attempts WHERE ip_address = ?", ipAddress)
			auditLog(database, r, "security.unblock_ip", map[string]interface{}{"ip": ipAddress})
			log.Printf("[SECURITY] IP %s unblocked by admin %s", ipAddress, c.Username)
			jsonResp(w, 200, map[string]string{"status": "unblocked", "ip_address": ipAddress})
		})
	})

	adminRouter.Route("/api/v1/security/access-log", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil || c.Role != "root" {
				jsonError(w, 403, "access denied"); return
			}
			limit := 100; offset := 0
			if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 500 { limit = l }
			if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 { offset = o }
			rows, err := database.Query(`SELECT id, username, ip_address, success, COALESCE(user_agent,''), created_at FROM login_attempts ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
			if err != nil { jsonResp(w, 200, []interface{}{}); return }
			defer rows.Close()
			type Attempt struct {
				ID        int    `json:"id"`
				Username  string `json:"username"`
				IPAddress string `json:"ip_address"`
				Success   bool   `json:"success"`
				UserAgent string `json:"user_agent"`
				CreatedAt string `json:"created_at"`
			}
			attempts := make([]Attempt, 0)
			for rows.Next() {
				var a Attempt
				var success int
				rows.Scan(&a.ID, &a.Username, &a.IPAddress, &success, &a.UserAgent, &a.CreatedAt)
				a.Success = success == 1
				attempts = append(attempts, a)
			}
			if err := rows.Err(); err != nil {
				log.Printf("[SECURITY] login attempts rows iteration error: %v", err)
				jsonResp(w, 200, []interface{}{}); return
			}
			jsonResp(w, 200, attempts)
		})
		r.Get("/count", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil || c.Role != "root" {
				jsonError(w, 403, "access denied"); return
			}
			var total, blocked int
			database.QueryRow("SELECT COUNT(*) FROM login_attempts").Scan(&total)
			database.QueryRow("SELECT COUNT(*) FROM blocked_ips").Scan(&blocked)
			jsonResp(w, 200, map[string]int{"total_attempts": total, "blocked_ips": blocked})
		})
	})

	adminRouter.Get("/.well-known/acme-challenge/{token}", acmeChallengeHandler)

	adminRouter.Route("/pma", func(r chi.Router) {
		r.Handle("/*", pmaProxyHandler(database))
	})

	adminRouter.With(authMw(jwtManager, "parent")).Post("/api/v1/server/shutdown", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil || c.Role != "root" {
			jsonError(w, 403, "only root admins can shutdown the server")
			return
		}
		jsonResp(w, 200, map[string]string{"status": "shutting_down"})
		log.Println("[SHUTDOWN] Initiated by admin:", c.Username)
		go func() {
			time.Sleep(500 * time.Millisecond)
			os.Exit(0)
		}()
	})

	// Admin static files
	adminStaticDir := os.Getenv("OWP_ADMIN_STATIC_DIR")
	if adminStaticDir == "" {
		adminStaticDir = "./web/dist/admin"
	}
	adminRouter.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			jsonError(w, 404, "not found")
			return
		}
		cleanPath := filepath.Clean(r.URL.Path)
		filePath := filepath.Join(adminStaticDir, cleanPath)
		if !strings.HasPrefix(filePath, filepath.Clean(adminStaticDir)) {
			http.ServeFile(w, r, adminStaticDir+"/admin.html")
			return
		}
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			http.ServeFile(w, r, filePath)
			return
		}
		http.ServeFile(w, r, adminStaticDir+"/admin.html")
	})

	// ==================== CHILD ROUTER (:9001) ====================
	childRouter := chi.NewRouter()
	childRouter.Use(middleware.Logger)
	childRouter.Use(middleware.Recoverer)
	childRouter.Use(middleware.RealIP)
	childRouter.Use(corsHandler)

	childRouter.Get("/healthz", healthHandler)

	childRouter.Route("/api/v1/child/auth", func(r chi.Router) {
		r.Get("/captcha", captchaHandler(database))
		childAuthRoutes(r, database, jwtManager)
	})

	childRouter.Route("/api/v1/child/account", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil {
				jsonError(w, 401, "unauthorized")
				return
			}
			var username, domain, email, homeDir, status, pkgName string
			var diskMB, bandwidthMB, ramLimitMB, maxDB, maxEmail, maxFTP, maxDomains, maxSubdomains, diskUsed, bwUsed, ramUsed int
			var ip sql.NullString
			err := database.QueryRow(`SELECT a.username, a.domain, a.email, a.home_dir, a.status, a.ip_address,
				p.name, p.disk_mb, p.bandwidth_mb,
				CASE WHEN a.ram_limit_mb > 0 THEN a.ram_limit_mb ELSE COALESCE(p.ram_limit_mb, 0) END,
				p.max_db, p.max_email, p.max_ftp, p.max_domains, p.max_subdomains,
				COALESCE(a.disk_used_mb, 0), COALESCE(a.bandwidth_used_mb, 0), COALESCE(a.ram_used_mb, 0)
				FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`,
				c.AccountID).Scan(&username, &domain, &email, &homeDir, &status, &ip,
				&pkgName, &diskMB, &bandwidthMB, &ramLimitMB, &maxDB, &maxEmail, &maxFTP, &maxDomains, &maxSubdomains,
				&diskUsed, &bwUsed, &ramUsed)
			if err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			var dbCount, domainCount, addonCount, subdomainCount, emailCount int
			database.QueryRow("SELECT COUNT(*) FROM child_databases WHERE account_id = ?", c.AccountID).Scan(&dbCount)
			database.QueryRow("SELECT COUNT(*) FROM domains WHERE account_id = ? AND type != 'subdomain'", c.AccountID).Scan(&domainCount)
			database.QueryRow("SELECT COUNT(*) FROM domains WHERE account_id = ? AND type = 'addon'", c.AccountID).Scan(&addonCount)
			database.QueryRow("SELECT COUNT(*) FROM domains WHERE account_id = ? AND type = 'subdomain'", c.AccountID).Scan(&subdomainCount)
			database.QueryRow("SELECT COUNT(*) FROM email_accounts WHERE account_id = ?", c.AccountID).Scan(&emailCount)
			sharedIP := ip.String
			if !ip.Valid || sharedIP == "" {
				sharedIP = getSharedIP()
			}
			ramWarning := ramLimitWarningHandler(database, c.AccountID)
			jsonResp(w, 200, map[string]interface{}{
				"username": username, "domain": domain, "email": email, "home_dir": homeDir,
				"status": status, "shared_ip": sharedIP, "package_name": pkgName,
				"disk_limit_mb": diskMB, "disk_used_mb": diskUsed,
				"bandwidth_limit_mb": bandwidthMB, "bandwidth_used_mb": bwUsed,
				"ram_limit_mb": ramLimitMB, "ram_used_mb": ramUsed, "ram_warning": ramWarning,
				"max_databases": maxDB, "databases_used": dbCount,
				"max_email": maxEmail, "emails_used": emailCount,
				"max_ftp": maxFTP, "max_domains": maxDomains, "total_domains": domainCount,
				"addon_domains": addonCount, "subdomains": subdomainCount, "max_subdomains": maxSubdomains,
			})
		})
	})

	childRouter.Route("/api/v1/child/files", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		childFileRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/databases", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		childDbRoutes(r, database, jwtManager)
	})

	childRouter.Route("/api/v1/child/domains", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		childDomainRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/notifications", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil { jsonError(w, 401, "unauthorized"); return }
			rows, err := database.Query(`SELECT n.id, n.title, n.message, n.created_at,
				CASE WHEN nr.read_at IS NOT NULL THEN 1 ELSE 0 END as is_read
				FROM notifications n
				LEFT JOIN notification_reads nr ON nr.notification_id = n.id AND nr.account_id = ?
				WHERE n.account_id IS NULL OR n.account_id = ?
				ORDER BY n.created_at DESC LIMIT 50`, c.AccountID, c.AccountID)
			if err != nil { jsonResp(w, 200, []interface{}{}); return }
			defer rows.Close()
			type ChildNotif struct {
				ID        int    `json:"id"`
				Title     string `json:"title"`
				Message   string `json:"message"`
				CreatedAt string `json:"created_at"`
				IsRead    bool   `json:"is_read"`
			}
			notifs := make([]ChildNotif, 0)
			for rows.Next() {
				var n ChildNotif
				var isRead int
				rows.Scan(&n.ID, &n.Title, &n.Message, &n.CreatedAt, &isRead)
				n.IsRead = isRead == 1
				notifs = append(notifs, n)
			}
			if err := rows.Err(); err != nil {
				log.Printf("[NOTIFICATIONS] child rows iteration error: %v", err)
				jsonResp(w, 200, []interface{}{}); return
			}
			jsonResp(w, 200, notifs)
		})
		r.Post("/{id}/read", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil { jsonError(w, 401, "unauthorized"); return }
			id := chi.URLParam(r, "id")
			database.Exec("INSERT OR IGNORE INTO notification_reads (notification_id, account_id) VALUES (?, ?)", id, c.AccountID)
			jsonResp(w, 200, map[string]string{"status": "read"})
		})
		r.Post("/read-all", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil { jsonError(w, 401, "unauthorized"); return }
			database.Exec(`INSERT OR IGNORE INTO notification_reads (notification_id, account_id)
				SELECT id, ? FROM notifications WHERE account_id IS NULL OR account_id = ?`, c.AccountID, c.AccountID)
			jsonResp(w, 200, map[string]string{"status": "all_read"})
		})
		r.Get("/unread-count", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil { jsonError(w, 401, "unauthorized"); return }
			var count int
			database.QueryRow(`SELECT COUNT(*) FROM notifications n
				LEFT JOIN notification_reads nr ON nr.notification_id = n.id AND nr.account_id = ?
				WHERE (n.account_id IS NULL OR n.account_id = ?) AND nr.read_at IS NULL`,
				c.AccountID, c.AccountID).Scan(&count)
			jsonResp(w, 200, map[string]int{"count": count})
		})
	})

	childRouter.Route("/api/v1/child/bandwidth", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		childBandwidthRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/cms", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		cmsRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/ssl", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		certRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/emails", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		childEmailRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/cron", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		childCronRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/backups", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		childBackupRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/dns", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		childDNSRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/ftp", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		childFTPRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/ssh", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		childSSHKeyRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/tokens", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		childTokenRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/redirects", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		redirectRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/hotlink", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		hotlinkRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/stats", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		childStatsRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/errors", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		childErrorRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/php-versions", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		childPhpVersionRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/tickets", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), trackBandwidth(database))
		ticketRoutes(r, database)
	})

	// Child static files (served on :9001)
	childStaticDir := os.Getenv("OWP_CHILD_STATIC_DIR")
	if childStaticDir == "" {
		childStaticDir = "./web/dist/child"
	}
	childRouter.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			jsonError(w, 404, "not found")
			return
		}
		cleanPath := filepath.Clean(r.URL.Path)
		filePath := filepath.Join(childStaticDir, cleanPath)
		if !strings.HasPrefix(filePath, filepath.Clean(childStaticDir)) {
			http.ServeFile(w, r, childStaticDir+"/child.html")
			return
		}
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			http.ServeFile(w, r, filePath)
			return
		}
		http.ServeFile(w, r, childStaticDir+"/child.html")
	})

	adminListenAddr := os.Getenv("OWP_ADMIN_LISTEN")
	if adminListenAddr == "" {
		adminListenAddr = ":9000"
	}
	childListenAddr := os.Getenv("OWP_CHILD_LISTEN")
	if childListenAddr == "" {
		childListenAddr = ":9001"
	}

	// Start SMTP server for incoming mail (port 2525, iptables redirects 25->2525)
	go startSMTPServer(database)

	// Start cron job runner (evaluates every 30s)
	go startCronRunner(database)

	// Start CAPTCHA session cleanup (every 5 minutes)
	go func() {
		captchaGen := captcha.New(database, captcha.DefaultConfig())
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			captchaGen.Cleanup()
		}
	}()

	// Start SSL certificate renewal checker (daily)
	startCertRenewal(database)

	// Start Nginx vhost log bandwidth collector (every 5 min)
	startNginxBandwidthCollector(database)

	// Start RAM usage tracker (every 60 sec)
	startRAMTracker(database)

	// Periodic cleanup: remove suspended accounts after configured days
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		for range ticker.C {
			var days int
			database.QueryRow("SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = 'suspend_auto_remove_days'), 7)").Scan(&days)
			result, _ := database.Exec(`DELETE FROM accounts WHERE status = 'suspended' AND updated_at < datetime('now', '-' || ? || ' days')`, days)
			if n, _ := result.RowsAffected(); n > 0 {
				log.Printf("Auto-removed %d suspended accounts (older than %d days)", n, days)
			}
		}
	}()

	// Periodic trash cleanup: remove expired trash records and their files
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		for range ticker.C {
			rows, err := database.Query("SELECT id, trash_path FROM file_trash WHERE expires_at < datetime('now')")
			if err != nil {
				log.Printf("Trash cleanup query error: %v", err)
				continue
			}
			for rows.Next() {
				var id int
				var tp string
				if err := rows.Scan(&id, &tp); err == nil {
					os.RemoveAll(tp)
				}
			}
			if err := rows.Err(); err != nil {
				log.Printf("[TRASH] cleanup rows iteration error: %v", err)
			}
			rows.Close()
			database.Exec("DELETE FROM file_trash WHERE expires_at < datetime('now')")
		}
	}()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("Received signal %v, shutting down...", sig)
		os.Exit(0)
	}()

	adminPass := os.Getenv("OWP_ADMIN_PASSWORD")
	if adminPass == "" {
		adminPass = "admin123"
	}
	if adminPass == "admin123" {
		log.Println("DEFAULT PASSWORD in use. Set OWP_ADMIN_PASSWORD env var for security.")
	} else {
		log.Println("Admin login configured via OWP_ADMIN_PASSWORD.")
	}
	hash, err := auth.HashPassword(adminPass)
	if err == nil {
		database.Exec("UPDATE admins SET password_hash = ? WHERE username = 'admin'", hash)
	}

	// Sync nginx vhosts for all domains for all active accounts
	syncNginxVhosts(database)

	// Start both HTTP servers
	go func() {
		log.Printf("[CHILD] Child Panel HTTP server on %s (db: %s)", childListenAddr, dbPath)
		if err := http.ListenAndServe(childListenAddr, childRouter); err != nil {
			log.Printf("[CHILD] Child server error: %v", err)
		}
	}()

	log.Printf("[ADMIN] Admin Panel HTTP server on %s (db: %s)", adminListenAddr, dbPath)
	if err := http.ListenAndServe(adminListenAddr, adminRouter); err != nil {
		log.Printf("[ADMIN] Admin server listen error (non-fatal): %v", err)
		log.Println("[ADMIN] ListenAndServe returned - the service will continue running.")
		select {}
	}
}
