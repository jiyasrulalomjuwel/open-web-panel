package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	_ "github.com/mattn/go-sqlite3"

	"github.com/openwebcpanel/openwebcpanel/internal/shared/auth"
	sdb "github.com/openwebcpanel/openwebcpanel/internal/shared/db"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/docker"
	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/filesystem"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/middleware"
	)

func getClaims(r *http.Request) *auth.Claims {
	return middleware.GetClaims(r)
}

// columnExists reports whether a column exists on a SQLite table.
func columnExists(db *sql.DB, table, column string) bool {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil && name == column {
			return true
		}
	}
	return false
}

func setClaims(r *http.Request, claims *auth.Claims) context.Context {
	return context.WithValue(r.Context(), middleware.ClaimsKey, claims)
}

// ---------- JSON helpers ----------

func jsonResp(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[JSON] encode error (status %d): %v", status, err)
	}
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	// Never leak raw database/OS error strings to clients on 5xx responses.
	// The original message is still surfaced in jsonResp only for 4xx; for
	// 5xx we substitute a generic message (the raw string is logged instead).
	if status >= 500 {
		log.Printf("[HTTP %d] %s", status, msg)
		msg = "an internal error occurred"
	}
	jsonResp(w, status, map[string]string{"error": msg})
}

// ---------- Domain sanitization ----------

func sanitizeDomain(domain string) string {
	d := strings.ReplaceAll(domain, "..", "")
	d = strings.ReplaceAll(d, "/", "")
	d = strings.ReplaceAll(d, "\\", "")
	return d
}

// injectManagedServerBlock inserts snippet into every top-level server block
// whose server_name matches dom (bare or www). Any previous block delimited
// by begin/end markers is removed first (all occurrences). An empty snippet
// therefore cleanly removes the feature. Brace tracking is intentionally
// naive: our vhost files are machine-generated with one brace per line.
func injectManagedServerBlock(content, dom, begin, end, snippet string) string {
	// Strip existing blocks.
	for {
		s := strings.Index(content, begin)
		if s < 0 {
			break
		}
		e := strings.Index(content[s:], end)
		if e < 0 {
			break
		}
		content = content[:s] + content[s+e+len(end):]
	}
	snippet = strings.TrimSpace(snippet)
	if snippet == "" {
		return content
	}
	block := begin + "\n" + snippet + "\n" + end + "\n"
	var out strings.Builder
	depth := 0
	for _, line := range strings.SplitAfter(content, "\n") {
		out.WriteString(line)
		t := strings.TrimSpace(line)
		if strings.HasSuffix(t, "{") {
			depth++
		}
		if depth == 1 && strings.HasPrefix(t, "server_name ") && serverNameMatches(t, dom) {
			// Every matching server (:80 and :443) gets the block so
			// behavior is consistent on both.
			out.WriteString(block)
		}
		// Count closes on the line (naive: lines rarely mix).
		if strings.HasPrefix(t, "}") {
			depth--
			if depth < 0 {
				depth = 0
			}
		}
	}
	return out.String()
}

// serverNameMatches reports whether a server_name directive line names dom
// (bare or www-prefixed; trailing semicolon tolerated).
func serverNameMatches(line, dom string) bool {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "server_name"))
	rest = strings.TrimSuffix(rest, ";")
	d := strings.ToLower(strings.TrimSuffix(dom, "."))
	for _, tok := range strings.Fields(rest) {
		t := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(tok, ";"), "."))
		if t == d || t == "www."+d || d == "www."+strings.TrimPrefix(t, "www.") {
			return true
		}
	}
	return false
}
// in our own database. Only then is it safe to show the parked page: any
// other Host (panel IP/hostname, unknown names) keeps the previous behavior
// so administrators can never lock themselves out of the login.
func parkedHostFor(r *http.Request, db *sql.DB) string {
	host := strings.ToLower(strings.TrimSpace(r.Host))
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" || db == nil {
		return ""
	}
	bare := strings.TrimPrefix(host, "www.")
	var n int
	// Active accounts only: suspended/terminated domains intentionally have no
	// vhost, and their visitors get the same parked explanation.
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains d
		JOIN accounts a ON a.id = d.account_id
		WHERE a.status = 'active' AND (LOWER(d.domain) = ? OR LOWER(d.domain) = ?)`,
		host, bare).Scan(&n); err != nil || n == 0 {
		return ""
	}
	return host
}

// validDomain reports whether s is a syntactically valid hostname (no paths,
// no config metacharacters, no traversal). This is the only format allowed for
// domain registration, which also prevents nginx config injection.
var domainLabelRe = regexp.MustCompile(`(?i)^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func validDomain(s string) bool {
	s = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if !domainLabelRe.MatchString(label) {
			return false
		}
	}
	return true
}

// ---------- SQLite init ----------

func initDB(path string) (*sql.DB, error) {
	db, err := sdb.ConnectSQLite(path)
	if err != nil {
		return nil, err
	}
	// The SQLite database holds tenant secrets (db passwords, admin hashes,
	// refresh tokens). Restrict it to the panel user so it's not world-readable.
	if !strings.HasPrefix(path, ":memory:") {
		os.Chmod(path, 0600)
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
		files_enabled   INTEGER NOT NULL DEFAULT 1,
		emails_enabled  INTEGER NOT NULL DEFAULT 1,
		ftp_enabled     INTEGER NOT NULL DEFAULT 1,
		db_enabled      INTEGER NOT NULL DEFAULT 1,
		cron_enabled    INTEGER NOT NULL DEFAULT 1,
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
		suspended_at        TEXT DEFAULT '',
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
		force_https INTEGER NOT NULL DEFAULT 0,
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
			last_error  TEXT DEFAULT '',
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
			type            TEXT NOT NULL DEFAULT 'full' CHECK(type IN ('full','files','database')),
			file_path       TEXT NOT NULL,
			file_size       INTEGER DEFAULT 0,
			status          TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','completed','failed','restoring')),
			backup_notes    TEXT DEFAULT '',
			progress        INTEGER DEFAULT 0,
			trigger_type    TEXT NOT NULL DEFAULT 'manual' CHECK(trigger_type IN ('manual','schedule')),
			message         TEXT DEFAULT '',
			created_at      TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS backup_schedules (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id    INTEGER NOT NULL,
			name          TEXT NOT NULL DEFAULT 'Daily Backup',
			backup_type   TEXT NOT NULL DEFAULT 'full' CHECK(backup_type IN ('full','files','database')),
			frequency     TEXT NOT NULL DEFAULT 'daily' CHECK(frequency IN ('daily','weekly','monthly')),
			time          TEXT NOT NULL DEFAULT '02:00',
			day_of_week   INTEGER NOT NULL DEFAULT 0,
			day_of_month  INTEGER NOT NULL DEFAULT 1,
			retention     INTEGER NOT NULL DEFAULT 7,
			enabled       INTEGER NOT NULL DEFAULT 1,
			next_run_at   TEXT,
			last_run_at   TEXT,
			last_status   TEXT DEFAULT '',
			created_at    TEXT DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_backups_account ON backups(account_id, created_at);
		CREATE INDEX IF NOT EXISTS idx_backup_schedules_account ON backup_schedules(account_id);

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
			scope           TEXT NOT NULL DEFAULT 'child',
			admin_id        INTEGER NOT NULL DEFAULT 0,
			created_at      TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS redirects (
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

	CREATE TABLE IF NOT EXISTS k8s_diagnostics (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		node        TEXT NOT NULL,
		check_name  TEXT NOT NULL,
		status      TEXT NOT NULL DEFAULT '',
		detail      TEXT DEFAULT '',
		created_at  TEXT DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_k8s_diagnostics_node ON k8s_diagnostics(node, created_at);

	CREATE TABLE IF NOT EXISTS k8s_node_metrics (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		node        TEXT NOT NULL,
		role        TEXT NOT NULL DEFAULT 'worker',
		cpu_used    TEXT NOT NULL DEFAULT '',
		cpu_pct     REAL NOT NULL DEFAULT 0,
		mem_used    TEXT NOT NULL DEFAULT '',
		mem_pct     REAL NOT NULL DEFAULT 0,
		disk_used   TEXT NOT NULL DEFAULT '',
		disk_pct    REAL NOT NULL DEFAULT 0,
		pods        INTEGER NOT NULL DEFAULT 0,
		pods_cap    INTEGER NOT NULL DEFAULT 0,
		created_at  TEXT DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_k8s_node_metrics_node ON k8s_node_metrics(node, created_at);

	CREATE TABLE IF NOT EXISTS login_attempts (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		username    TEXT NOT NULL,
		ip_address  TEXT NOT NULL,
		success     INTEGER NOT NULL DEFAULT 0,
		user_agent  TEXT DEFAULT '',
		created_at  TEXT DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_login_attempts_ip ON login_attempts(ip_address	);

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

	CREATE TABLE IF NOT EXISTS docker_containers (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		account_id      INTEGER NOT NULL UNIQUE,
		container_id    TEXT NOT NULL DEFAULT '',
		container_name  TEXT NOT NULL DEFAULT '',
		status          TEXT NOT NULL DEFAULT 'created' CHECK(status IN ('created','running','stopped','paused','removed')),
		cpu_limit       REAL NOT NULL DEFAULT 0,
		ram_limit_mb    INTEGER NOT NULL DEFAULT 0,
		storage_limit_gb INTEGER NOT NULL DEFAULT 0,
		last_synced_at  TEXT DEFAULT '',
		created_at      TEXT DEFAULT (datetime('now')),
		updated_at      TEXT DEFAULT (datetime('now')),
		FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
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
		"ALTER TABLE docker_containers ADD COLUMN container_ip TEXT DEFAULT ''",
		// API tokens: scope distinguishes admin vs account tokens; admin_id
		// owns admin-scope tokens (account_id stays 0 for those rows).
		"ALTER TABLE api_tokens ADD COLUMN scope TEXT DEFAULT 'child'",
		"ALTER TABLE api_tokens ADD COLUMN admin_id INTEGER DEFAULT 0",
		// SSL truth: which error forced a self-signed fallback, if any.
		"ALTER TABLE ssl_certs ADD COLUMN last_error TEXT DEFAULT ''",
		// HTTPS enforcement: per-domain redirect flag + global default for new certs.
		"ALTER TABLE domains ADD COLUMN force_https INTEGER DEFAULT 0",
		// Suspended-account auto-purge timestamp.
		"ALTER TABLE accounts ADD COLUMN suspended_at TEXT DEFAULT ''",
		// Cluster registry retired in favor of the K3s API (Phase K3s-native).
		"DROP TABLE IF EXISTS cluster_nodes",
		"DROP TABLE IF EXISTS cluster_join_codes",
		"ALTER TABLE accounts DROP COLUMN node_id",
		// Per-feature access gates (V1: files, emails, ftp, databases, cron).
		// Backups reuses the existing backup_enabled package column.
		"ALTER TABLE packages ADD COLUMN files_enabled INTEGER DEFAULT 1",
		"ALTER TABLE packages ADD COLUMN emails_enabled INTEGER DEFAULT 1",
		"ALTER TABLE packages ADD COLUMN ftp_enabled INTEGER DEFAULT 1",
		"ALTER TABLE packages ADD COLUMN db_enabled INTEGER DEFAULT 1",
		"ALTER TABLE packages ADD COLUMN cron_enabled INTEGER DEFAULT 1",
		// Performance indexes for high-traffic lookup tables.
		"CREATE INDEX IF NOT EXISTS idx_accounts_status ON accounts(status)",
		"CREATE INDEX IF NOT EXISTS idx_accounts_package_id ON accounts(package_id)",
		"CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id)",
		"CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log(created_at)",
		"CREATE INDEX IF NOT EXISTS idx_login_attempts_ip ON login_attempts(ip_address)",
		"CREATE INDEX IF NOT EXISTS idx_blocked_ips_ip_address ON blocked_ips(ip_address)",
		"CREATE INDEX IF NOT EXISTS idx_domains_account_id ON domains(account_id)",
		"CREATE INDEX IF NOT EXISTS idx_child_databases_account_id ON child_databases(account_id)",
		"CREATE INDEX IF NOT EXISTS idx_db_users_account_id ON db_users(account_id)",
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
		adminUser := os.Getenv("OWP_ADMIN_USERNAME")
		if adminUser == "" {
			adminUser = "admin"
		}
		hash, err := auth.HashPassword(adminPass)
		if err != nil {
			log.Fatalf("Failed to hash admin password: %v", err)
		}
		db.Exec(`INSERT INTO admins (username, password_hash, role) VALUES (?, ?, 'root')`, adminUser, hash)
	}

	// Seed default server config if empty
	var cfgCount int
	db.QueryRow("SELECT COUNT(*) FROM server_config").Scan(&cfgCount)
	if cfgCount == 0 {
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('suspend_auto_remove_days', '7')`)
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('default_upload_limit_mb', '2048')`)
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('max_upload_limit_mb', '5120')`)
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('site_name', 'OpenWebPanel')`)
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('force_https_default', '1')`)
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('smtp_relay_host', '')`)
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('smtp_relay_port', '587')`)
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('smtp_relay_username', '')`)
		db.Exec(`INSERT INTO server_config (key_name, value) VALUES ('smtp_relay_password', '')`)
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

	// Migration: add progress/trigger/message columns to backups (older schema)
	if !columnExists(db, "backups", "progress") {
		db.Exec("ALTER TABLE backups ADD COLUMN progress INTEGER DEFAULT 0")
	}
	if !columnExists(db, "backups", "trigger_type") {
		db.Exec("ALTER TABLE backups ADD COLUMN trigger_type TEXT NOT NULL DEFAULT 'manual'")
	}
	if !columnExists(db, "backups", "message") {
		db.Exec("ALTER TABLE backups ADD COLUMN message TEXT DEFAULT ''")
	}
	// Migration: legacy 'partial' backup type is treated as 'files'
	db.Exec(`UPDATE backups SET type = 'files' WHERE type = 'partial'`)

	// Migration: rebuild the backups table when it still carries the old CHECK
	// constraints (type without 'files', status without 'restoring'). SQLite
	// cannot alter CHECK constraints, so the table is rebuilt and copied.
	backupsDDL := ""
	db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='backups'").Scan(&backupsDDL)
	if backupsDDL != "" && (!strings.Contains(backupsDDL, "'files'") || !strings.Contains(backupsDDL, "'restoring'")) {
		tx, err := db.Begin()
		if err != nil {
			log.Printf("[MIGRATE] begin backups rebuild: %v", err)
		} else {
			rebuild := []string{
				`CREATE TABLE backups_new (
					id              INTEGER PRIMARY KEY AUTOINCREMENT,
					account_id      INTEGER NOT NULL,
					domain          TEXT NOT NULL,
					type            TEXT NOT NULL DEFAULT 'full' CHECK(type IN ('full','files','database')),
					file_path       TEXT NOT NULL,
					file_size       INTEGER DEFAULT 0,
					status          TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','completed','failed','restoring')),
					backup_notes    TEXT DEFAULT '',
					progress        INTEGER DEFAULT 0,
					trigger_type    TEXT NOT NULL DEFAULT 'manual' CHECK(trigger_type IN ('manual','schedule')),
					message         TEXT DEFAULT '',
					created_at      TEXT DEFAULT (datetime('now'))
				)`,
				`INSERT INTO backups_new (id, account_id, domain, type, file_path, file_size, status, backup_notes, created_at, progress, trigger_type, message)
					SELECT id, account_id, domain, type, file_path, COALESCE(file_size,0), status, COALESCE(backup_notes,''), COALESCE(created_at, datetime('now')), COALESCE(progress,0), COALESCE(trigger_type,'manual'), COALESCE(message,'') FROM backups`,
				`DROP TABLE backups`,
				`ALTER TABLE backups_new RENAME TO backups`,
				`CREATE INDEX IF NOT EXISTS idx_backups_account ON backups(account_id, created_at)`,
			}
			ok := true
			for _, q := range rebuild {
				if _, err := tx.Exec(q); err != nil {
					log.Printf("[MIGRATE] backups rebuild step failed: %v", err)
					tx.Rollback()
					ok = false
					break
				}
			}
			if ok {
				if err := tx.Commit(); err != nil {
					log.Printf("[MIGRATE] backups rebuild commit: %v", err)
				} else {
					log.Printf("[MIGRATE] rebuilt backups table with updated CHECK constraints")
				}
			}
		}
	}

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

// tokenAuthDB lets authMw fall back to API-token validation (see tokens.go).
// Set once in main() after initDB; the panel uses a single SQLite handle.
var tokenAuthDB *sql.DB

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
				// Fall back to long-lived API tokens (owp_...). Scope is still
				// enforced below, so a token can never exceed its owner's access.
				if tokenAuthDB != nil && strings.HasPrefix(tokenStr, "owp_") {
					if tc := validateAPIToken(tokenAuthDB, tokenStr); tc != nil {
						claims = tc
					}
				}
			}
			if claims == nil {
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

// --- Auth ---

func authRoutes(r chi.Router, db *sql.DB, jwtManager *auth.JWTManager) {
	r.Post("/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}

		clientIP := getClientIP(r)

		if isUsernameBlocked(db, req.Username) {
			recordLoginAttempt(db, req.Username, clientIP, r.UserAgent(), false)
			jsonError(w, 429, "account temporarily locked due to too many failed attempts")
			return
		}

		if isIPBlocked(db, clientIP) {
			recordLoginAttempt(db, req.Username, clientIP, r.UserAgent(), false)
			jsonError(w, 429, "too many failed login attempts - your IP has been temporarily blocked")
			return
		}

		var adminID int
		var username, passwordHash, role string
		var lastLogin sql.NullString
		err := db.QueryRow(`SELECT id, username, password_hash, role, last_login_at FROM admins WHERE username = ?`,
			req.Username).Scan(&adminID, &username, &passwordHash, &role, &lastLogin)
		if err != nil {
			// Run a timing-safe dummy bcrypt so nonexistent usernames take the
			// same time as a wrong password, defeating username enumeration.
			auth.CheckPasswordTimingSafe("", req.Password)
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
			adminID, hashRefreshToken(tokens.RefreshToken))

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

	// Update own admin profile (username and/or password). Sessions stay valid;
	// password changes do not revoke existing tokens by design (short-lived).
	r.With(authMw(jwtManager, "parent")).Put("/me", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" && req.Password == "" {
			jsonError(w, 400, "nothing to update")
			return
		}
		if req.Username != "" {
			if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{2,30}$`).MatchString(req.Username) {
				jsonError(w, 400, "invalid username: must be 3-30 chars, alphanumeric, hyphens, underscores")
				return
			}
			var taken int
			db.QueryRow("SELECT COUNT(*) FROM admins WHERE username = ? AND id != ?", req.Username, c.UserID).Scan(&taken)
			if taken > 0 {
				jsonError(w, 409, "username already taken")
				return
			}
			if _, err := db.Exec("UPDATE admins SET username = ? WHERE id = ?", req.Username, c.UserID); err != nil {
				jsonError(w, 500, "failed to update username")
				return
			}
		}
		if req.Password != "" {
			if len(req.Password) < 8 {
				jsonError(w, 400, "password must be at least 8 characters")
				return
			}
			hash, err := auth.HashPassword(req.Password)
			if err != nil {
				jsonError(w, 500, "failed to hash password")
				return
			}
			if _, err := db.Exec("UPDATE admins SET password_hash = ? WHERE id = ?", hash, c.UserID); err != nil {
				jsonError(w, 500, "failed to update password")
				return
			}
		}
		var username, role string
		db.QueryRow("SELECT username, role FROM admins WHERE id = ?", c.UserID).Scan(&username, &role)
		auditLog(db, r, "admin.profile_update", map[string]interface{}{"id": c.UserID})
		jsonResp(w, 200, map[string]interface{}{"id": c.UserID, "username": username, "role": role})
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
		hashedToken := hashRefreshToken(req.RefreshToken)
		err := db.QueryRow(`SELECT user_id, scope FROM refresh_tokens WHERE token_hash = ? AND expires_at > datetime('now')`,
			hashedToken).Scan(&userID, &scope)
		if err != nil {
			jsonError(w, 401, "invalid or expired refresh token")
			return
		}
		if scope != "parent" {
			jsonError(w, 401, "invalid or expired refresh token")
			return
		}

		// Delete old refresh token (rotation)
		db.Exec("DELETE FROM refresh_tokens WHERE token_hash = ?", hashedToken)

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
			userID, hashRefreshToken(tokens.RefreshToken))
		jsonResp(w, 200, tokens)
	})
}

// --- Packages ---

func packageRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT id, name, disk_mb, bandwidth_mb, ram_limit_mb, max_db, max_email, max_ftp,
			max_domains, max_subdomains, ssh_access, backup_enabled,
			COALESCE(files_enabled, 1), COALESCE(emails_enabled, 1), COALESCE(ftp_enabled, 1),
			COALESCE(db_enabled, 1), COALESCE(cron_enabled, 1),
			is_default, created_at, updated_at
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
			FilesEnabled  bool   `json:"files_enabled"`
			EmailsEnabled bool   `json:"emails_enabled"`
			FTPEnabled    bool   `json:"ftp_enabled"`
			DBEnabled     bool   `json:"db_enabled"`
			CronEnabled   bool   `json:"cron_enabled"`
			IsDefault     bool   `json:"is_default"`
			CreatedAt     string `json:"created_at"`
			UpdatedAt     string `json:"updated_at"`
		}
		pkgs := make([]Pkg, 0)
		for rows.Next() {
			var p Pkg
			var ssh, backup, files, emails, ftp, dben, cron, def int
			if err := rows.Scan(&p.ID, &p.Name, &p.DiskMB, &p.BandwidthMB, &p.RamLimitMB, &p.MaxDB, &p.MaxEmail, &p.MaxFTP,
				&p.MaxDomains, &p.MaxSubdomains, &ssh, &backup, &files, &emails, &ftp, &dben, &cron, &def, &p.CreatedAt, &p.UpdatedAt); err != nil {
				continue
			}
			p.SSHAccess = ssh == 1
			p.BackupEnabled = backup == 1
			p.FilesEnabled = files == 1
			p.EmailsEnabled = emails == 1
			p.FTPEnabled = ftp == 1
			p.DBEnabled = dben == 1
			p.CronEnabled = cron == 1
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
			FilesEnabled  *bool  `json:"files_enabled"`
			EmailsEnabled *bool  `json:"emails_enabled"`
			FTPEnabled    *bool  `json:"ftp_enabled"`
			DBEnabled     *bool  `json:"db_enabled"`
			CronEnabled   *bool  `json:"cron_enabled"`
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
		// New feature gates default to enabled when omitted (backward compatible).
		boolInt := func(b *bool) int {
			if b != nil && !*b {
				return 0
			}
			return 1
		}
		auditLog(db, r, "package.create", map[string]interface{}{"name": req.Name})
		result, err := db.Exec(`INSERT INTO packages (name, disk_mb, bandwidth_mb, ram_limit_mb, max_db, max_email,
			max_ftp, max_domains, max_subdomains, ssh_access, backup_enabled,
			files_enabled, emails_enabled, ftp_enabled, db_enabled, cron_enabled)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			req.Name, req.DiskMB, req.BandwidthMB, req.RamLimitMB, req.MaxDB, req.MaxEmail,
			req.MaxFTP, req.MaxDomains, req.MaxSubdomains, ssh, backup,
			boolInt(req.FilesEnabled), boolInt(req.EmailsEnabled), boolInt(req.FTPEnabled),
			boolInt(req.DBEnabled), boolInt(req.CronEnabled))
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
			FilesEnabled  *bool  `json:"files_enabled"`
			EmailsEnabled *bool  `json:"emails_enabled"`
			FTPEnabled    *bool  `json:"ftp_enabled"`
			DBEnabled     *bool  `json:"db_enabled"`
			CronEnabled   *bool  `json:"cron_enabled"`
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
		boolInt := func(b *bool) int {
			if b != nil && !*b {
				return 0
			}
			return 1
		}
		result, err := db.Exec(`UPDATE packages SET name=?, disk_mb=?, bandwidth_mb=?, ram_limit_mb=?, max_db=?, max_email=?,
			max_ftp=?, max_domains=?, max_subdomains=?, ssh_access=?, backup_enabled=?,
			files_enabled=?, emails_enabled=?, ftp_enabled=?, db_enabled=?, cron_enabled=?,
			updated_at=datetime('now')
			WHERE id=?`,
			req.Name, req.DiskMB, req.BandwidthMB, req.RamLimitMB, req.MaxDB, req.MaxEmail, req.MaxFTP,
			req.MaxDomains, req.MaxSubdomains, ssh, backup,
			boolInt(req.FilesEnabled), boolInt(req.EmailsEnabled), boolInt(req.FTPEnabled),
			boolInt(req.DBEnabled), boolInt(req.CronEnabled), id)
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

// purgeAccountByID performs the full irreversible wipe shared by the manual
// Delete button and the suspended-account auto-purger. Returns per-category
// counts, or sql.ErrNoRows when the account does not exist.
func purgeAccountByID(db *sql.DB, r *http.Request, id int) (map[string]int, error) {
	var username, homeDir string
	if err := db.QueryRow("SELECT username, home_dir FROM accounts WHERE id = ?", id).Scan(&username, &homeDir); err != nil {
		return nil, sql.ErrNoRows
	}
	deleted := map[string]int{}
	del := func(table string) {
		if res, err := db.Exec(fmt.Sprintf("DELETE FROM %s WHERE account_id = ?", table), id); err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				deleted[table] += int(n)
			}
		}
	}

	// Docker container first so nothing executes mid-purge.
	var containerName string
	db.QueryRow("SELECT container_name FROM docker_containers WHERE account_id = ?", id).Scan(&containerName)
	if containerName != "" {
		if err := docker.RemoveContainer(containerName); err != nil {
			log.Printf("[PURGE] container remove failed for account %d: %v", id, err)
		} else {
			deleted["containers"] = 1
		}
	}
	del("docker_containers")

	// Real MySQL databases + users (root CLI, identifiers validated).
	if pass := os.Getenv("MYSQL_ROOT_PASSWORD"); pass != "" {
		if mysql, err := exec.LookPath("mysql"); err == nil {
			rows, err := db.Query("SELECT db_name FROM child_databases WHERE account_id = ?", id)
			var dbs []string
			if err == nil {
				for rows.Next() {
					var n string
					if rows.Scan(&n) == nil && validMysqlIdent(n) {
						dbs = append(dbs, n)
					}
				}
				rows.Close()
			}
			urows, err := db.Query("SELECT username FROM db_users WHERE account_id = ?", id)
			var users []string
			if err == nil {
				for urows.Next() {
					var u string
					if urows.Scan(&u) == nil && validMysqlIdent(u) {
						users = append(users, u)
					}
				}
				urows.Close()
			}
			var stmts []string
			for _, n := range dbs {
				stmts = append(stmts, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", n))
			}
			for _, u := range users {
				stmts = append(stmts, fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", u))
				stmts = append(stmts, fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost'", u))
			}
			if len(stmts) > 0 {
				cmd := exec.Command(mysql, "-h", "127.0.0.1", "-uroot", "-e", strings.Join(stmts, "; "))
				cmd.Env = append(os.Environ(), "MYSQL_PWD="+pass)
				if out, err := cmd.CombinedOutput(); err != nil {
					log.Printf("[PURGE] mysql drop failed for account %d: %v %s", id, err, out)
				} else {
					deleted["mysql_databases"] = len(dbs)
					deleted["mysql_users"] = len(users)
				}
			}
		}
	}
	del("db_user_assignments")
	del("db_users")
	del("child_databases")

	// Mail, FTP, cron, backups, schedules.
	db.Exec(`DELETE FROM email_messages WHERE email_account_id IN (SELECT id FROM email_accounts WHERE account_id = ?)`, id)
	del("email_accounts")
	del("ftp_accounts")
	del("cron_jobs")
	del("backups")
	del("backup_schedules")

	// SSL rows + files + nginx blocks.
	var certDomains []string
	if rows, err := db.Query("SELECT DISTINCT domain FROM ssl_certs WHERE account_id = ?", id); err == nil {
		for rows.Next() {
			var d string
			if rows.Scan(&d) == nil {
				certDomains = append(certDomains, d)
			}
		}
		rows.Close()
	}
	del("ssl_certs")
	for _, d := range certDomains {
		if err := removeNginxSSL(db, d); err != nil {
			log.Printf("[PURGE] HTTPS teardown failed for %s: %v", d, err)
		}
	}

	// Redirects, error pages, domains, vhosts.
	del("redirects")
	del("error_pages")
	var domains []string
	if rows, err := db.Query("SELECT domain FROM domains WHERE account_id = ?", id); err == nil {
		for rows.Next() {
			var d string
			if rows.Scan(&d) == nil {
				domains = append(domains, d)
			}
		}
		rows.Close()
	}
	del("domains")
	for _, d := range domains {
		var activeCount int
		db.QueryRow(`SELECT COUNT(*) FROM domains d JOIN accounts a ON a.id = d.account_id
			WHERE d.domain = ? AND a.status = 'active'`, d).Scan(&activeCount)
		if activeCount == 0 {
			removeNginxVhost(sanitizeDomain(d))
		}
	}
	reloadNginx()

	// Tickets, notifications, tokens, bandwidth, per-account overrides.
	db.Exec(`DELETE FROM ticket_messages WHERE ticket_id IN (SELECT id FROM support_tickets WHERE account_id = ?)`, id)
	del("support_tickets")
	del("notifications")
	db.Exec(`DELETE FROM notification_reads WHERE account_id = ?`, id)
	del("api_tokens")
	del("bandwidth_logs")
	del("file_trash")
	db.Exec(`DELETE FROM refresh_tokens WHERE user_id = ? AND scope = 'child'`, id)
	if res, err := db.Exec(`DELETE FROM server_config WHERE key_name LIKE '%\_' || ? ESCAPE '\'`, id); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			deleted["server_config"] = int(n)
		}
	}

	// Home directory last (contained, never empty/root).
	if homeDir != "" && homeDir != "/" {
		if clean, err := filepath.Abs(homeDir); err == nil && clean != "/" && strings.HasPrefix(clean, "/") {
			if err := os.RemoveAll(clean); err != nil {
				log.Printf("[PURGE] home remove failed for account %d (%s): %v", id, clean, err)
			} else {
				deleted["home_dir"] = 1
			}
		} else {
			log.Printf("[PURGE] refusing to remove suspicious home for account %d: %q", id, homeDir)
		}
	}

	// The account row itself + login-attempt noise.
	db.Exec(`DELETE FROM login_attempts WHERE username = (SELECT username FROM accounts WHERE id = ?)`, id)
	if res, err := db.Exec("DELETE FROM accounts WHERE id = ?", id); err != nil {
		return nil, err
	} else if n, _ := res.RowsAffected(); n == 0 {
		return nil, sql.ErrNoRows
	}
	deleted["accounts"] = 1
	if r != nil {
		auditLog(db, r, "account.purge", map[string]interface{}{"id": id, "username": username, "deleted": deleted})
	} else {
		log.Printf("[PURGE] auto-purged suspended account %d (%s): %v", id, username, deleted)
	}
	return deleted, nil
}

func accountRoutes(r chi.Router, db *sql.DB, jwtManager *auth.JWTManager) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		statusFilter := r.URL.Query().Get("status")
		query := `SELECT a.id, a.username, a.domain, a.email, a.package_id,
			p.name, a.status, a.home_dir, a.ip_address,
			a.disk_used_mb, COALESCE((SELECT CAST(SUM(bytes_out + bytes_in) AS REAL)/1048576.0 FROM bandwidth_logs WHERE account_id = a.id), 0),
			a.ram_used_mb, a.ram_limit_mb,
			COALESCE(a.suspended_reason, ''), COALESCE(a.suspended_at, ''), a.created_at, a.updated_at
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
			ID              int     `json:"id"`
			Username        string  `json:"username"`
			Domain          string  `json:"domain"`
			Email           string  `json:"email"`
			PackageID       int     `json:"package_id"`
			PackageName     string  `json:"package_name"`
			Status          string  `json:"status"`
			HomeDir         string  `json:"home_dir"`
			IPAddress       string  `json:"ip_address"`
			DiskUsedMB      float64 `json:"disk_used_mb"`
			BandwidthUsedMB float64 `json:"bandwidth_used_mb"`
			RamUsedMB       float64 `json:"ram_used_mb"`
			RamLimitMB      float64 `json:"ram_limit_mb"`
			SuspendedReason string  `json:"suspended_reason"`
			SuspendedAt     string  `json:"suspended_at"`
			PurgeAt         string  `json:"purge_at"`
			PurgeInDays     int     `json:"purge_in_days"`
			CreatedAt       string  `json:"created_at"`
			UpdatedAt       string  `json:"updated_at"`
		}
		graceDays := suspendAutoRemoveDays(db)
		accs := make([]Acc, 0)
		for rows.Next() {
			var a Acc
			var ip, reason, suspAt sql.NullString
			if err := rows.Scan(&a.ID, &a.Username, &a.Domain, &a.Email, &a.PackageID,
				&a.PackageName, &a.Status, &a.HomeDir, &ip,
				&a.DiskUsedMB, &a.BandwidthUsedMB, &a.RamUsedMB, &a.RamLimitMB, &reason, &suspAt, &a.CreatedAt, &a.UpdatedAt); err != nil {
				continue
			}
			if ip.Valid {
				a.IPAddress = ip.String
			}
			if reason.Valid {
				a.SuspendedReason = reason.String
			}
			if suspAt.Valid {
				a.SuspendedAt = suspAt.String
			}
			a.PurgeInDays = -1
			if a.Status == "suspended" && a.SuspendedAt != "" && graceDays > 0 {
				if t, err := time.ParseInLocation("2006-01-02 15:04:05", a.SuspendedAt, time.UTC); err == nil {
					purgeAt := t.AddDate(0, 0, graceDays)
					a.PurgeAt = purgeAt.Format("2006-01-02 15:04:05")
					days := int(purgeAt.Sub(time.Now().UTC()).Hours() / 24)
					if days < 0 {
						days = 0
					}
					a.PurgeInDays = days
				}
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

		if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{2,30}$`).MatchString(req.Username) {
			jsonError(w, 400, "invalid username: must be 3-30 chars, alphanumeric, hyphens, underscores")
			return
		}

		// Verify the package exists before creating account
		var pkgCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM packages WHERE id = ?", req.PackageID).Scan(&pkgCount); err != nil {
			jsonError(w, 500, "database error checking package")
			return
		}
		if pkgCount == 0 {
			jsonError(w, 400, "selected package does not exist")
			return
		}

		var existing int
		if err := db.QueryRow("SELECT COUNT(*) FROM accounts WHERE username = ?", req.Username).Scan(&existing); err != nil {
			jsonError(w, 500, "database error checking username")
			return
		}
		if existing > 0 {
			jsonError(w, 409, "username already exists")
			return
		}

		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			jsonError(w, 500, "failed to hash password")
			return
		}
		homeDir := filepath.Join(getHomesBase(), req.Username)

		os.MkdirAll(homeDir, 0755)
		os.MkdirAll(filepath.Join(homeDir, "public_html"), 0755)
		os.MkdirAll(filepath.Join(homeDir, ".owp"), 0700)

		// Create a default index.html for the account's website
		safeDomain := html.EscapeString(req.Domain)
		indexContent := fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>Welcome to %s</title>
<style>body{font-family:Arial;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#f0f4f8}
.card{background:#fff;padding:3rem;border-radius:12px;box-shadow:0 4px 24px rgba(0,0,0,.1);text-align:center;max-width:500px}
h1{color:#1a1a2e;margin-bottom:.5rem}p{color:#555}</style></head>
<body><div class="card"><h1>%s</h1><p>Site hosted by OpenWebPanel</p></div></body></html>`, safeDomain, safeDomain)
		os.WriteFile(filepath.Join(homeDir, "public_html", "index.html"), []byte(indexContent), 0644)

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
		primaryDocRoot := filepath.Join(homeDir, "public_html")
		db.Exec(`INSERT INTO domains (account_id, domain, type, doc_root) VALUES (?, ?, 'primary', ?)`,
			id, req.Domain, primaryDocRoot)

		// Write Nginx vhost for the primary domain (fallback to fastcgi until container is ready)
		if err := writeNginxVhost(sanitizeDomain(req.Domain), primaryDocRoot, "", 0, ""); err != nil {
			log.Printf("[ACCOUNTS] Account created but vhost write failed for %s: %v", req.Domain, err)
			jsonResp(w, 201, map[string]interface{}{
				"id": id, "username": req.Username, "status": "active",
				"warning": "Account created but nginx vhost could not be created. Check server logs.",
			})
			return
		}
		reloadNginx()

		// Create Docker container for this account
		accID := int(id)
		go func(accID int, username, homeDir string, pkgID int) {
			var pkgRAM, perAccRAM int
			db.QueryRow("SELECT COALESCE(ram_limit_mb, 0) FROM packages WHERE id = ?", pkgID).Scan(&pkgRAM)
			db.QueryRow("SELECT COALESCE(ram_limit_mb, 0) FROM accounts WHERE id = ?", accID).Scan(&perAccRAM)
			ram := perAccRAM
			if ram == 0 {
				ram = pkgRAM
			}
			cpu := getEffectiveCPULimit(db, accID)
			info, err := docker.ProvisionAccount(accID, username, homeDir, ram, cpu)
			if err != nil {
				log.Printf("[DOCKER] Failed to create container for %s: %v", username, err)
				return
			}
			db.Exec(`INSERT INTO docker_containers (account_id, container_id, container_name, status,
				cpu_limit, ram_limit_mb, storage_limit_gb)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				accID, info.ID, info.Name, info.Status, cpu, ram, 0)
			log.Printf("[DOCKER] Container %s created for account %s", info.Name, username)
		}(accID, req.Username, homeDir, req.PackageID)

		jsonResp(w, 201, map[string]interface{}{"id": id, "username": req.Username, "status": "active"})
	})

	r.Get("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		type Acc struct {
			ID              int     `json:"id"`
			Username        string  `json:"username"`
			Domain          string  `json:"domain"`
			Email           string  `json:"email"`
			PackageID       int     `json:"package_id"`
			PackageName     string  `json:"package_name"`
			Status          string  `json:"status"`
			HomeDir         string  `json:"home_dir"`
			IPAddress       string  `json:"ip_address"`
			DiskUsedMB      float64 `json:"disk_used_mb"`
			BandwidthUsedMB float64 `json:"bandwidth_used_mb"`
			RamUsedMB       float64 `json:"ram_used_mb"`
			SuspendedReason string  `json:"suspended_reason"`
			SuspendedAt     string  `json:"suspended_at"`
			PurgeAt         string  `json:"purge_at"`
			PurgeInDays     int     `json:"purge_in_days"`
			CreatedAt       string  `json:"created_at"`
			UpdatedAt       string  `json:"updated_at"`
		}
		var a Acc
		var ip, reason, suspAt sql.NullString
		err := db.QueryRow(`SELECT a.id, a.username, a.domain, a.email, a.package_id,
			p.name, a.status, a.home_dir, a.ip_address,
			a.disk_used_mb, COALESCE((SELECT CAST(SUM(bytes_out + bytes_in) AS REAL)/1048576.0 FROM bandwidth_logs WHERE account_id = a.id), 0),
			a.ram_used_mb, COALESCE(a.suspended_reason, ''), COALESCE(a.suspended_at, ''), a.created_at, a.updated_at
			FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`, id).Scan(
			&a.ID, &a.Username, &a.Domain, &a.Email, &a.PackageID,
			&a.PackageName, &a.Status, &a.HomeDir, &ip,
			&a.DiskUsedMB, &a.BandwidthUsedMB, &a.RamUsedMB, &reason, &suspAt, &a.CreatedAt, &a.UpdatedAt)
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
		if suspAt.Valid {
			a.SuspendedAt = suspAt.String
		}
		a.PurgeInDays = -1
		if a.Status == "suspended" && a.SuspendedAt != "" {
			if days := suspendAutoRemoveDays(db); days > 0 {
				if t, err := time.ParseInLocation("2006-01-02 15:04:05", a.SuspendedAt, time.UTC); err == nil {
					purgeAt := t.AddDate(0, 0, days)
					a.PurgeAt = purgeAt.Format("2006-01-02 15:04:05")
					d := int(purgeAt.Sub(time.Now().UTC()).Hours() / 24)
					if d < 0 {
						d = 0
					}
					a.PurgeInDays = d
				}
			}
		}
		jsonResp(w, 200, a)
	})

	// GET /{id}/resources — aggregated per-account resource lists for the
	// admin detail view (read-only across domains, databases, emails, FTP,
	// cron and backups).
	r.Get("/{id}/resources", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var exists int
		if err := db.QueryRow("SELECT COUNT(*) FROM accounts WHERE id = ?", id).Scan(&exists); err != nil || exists == 0 {
			jsonError(w, 404, "account not found")
			return
		}
		queryAll := func(q string) []map[string]interface{} {
			rows, err := db.Query(q, id)
			if err != nil {
				return []map[string]interface{}{}
			}
			defer rows.Close()
			cols, err := rows.Columns()
			if err != nil {
				return []map[string]interface{}{}
			}
			out := make([]map[string]interface{}, 0)
			for rows.Next() {
				vals := make([]interface{}, len(cols))
				ptrs := make([]interface{}, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					continue
				}
				row := map[string]interface{}{}
				for i, c := range cols {
					switch v := vals[i].(type) {
					case []byte:
						row[c] = string(v)
					default:
						row[c] = v
					}
				}
				out = append(out, row)
			}
			return out
		}
		var backupTotal, scheduleTotal, cronTotal int
		db.QueryRow("SELECT COUNT(*) FROM backups WHERE account_id = ?", id).Scan(&backupTotal)
		db.QueryRow("SELECT COUNT(*) FROM backup_schedules WHERE account_id = ?", id).Scan(&scheduleTotal)
		db.QueryRow("SELECT COUNT(*) FROM cron_jobs WHERE account_id = ?", id).Scan(&cronTotal)
		jsonResp(w, 200, map[string]interface{}{
			"domains":   queryAll(`SELECT id, domain, type, doc_root, COALESCE(ssl_enabled,0) AS ssl_enabled, created_at FROM domains WHERE account_id = ? ORDER BY domain`),
			"databases": queryAll(`SELECT id, db_name, db_user, COALESCE(host,'') AS host, COALESCE(size_mb,0) AS size_mb, COALESCE(remote_access,'') AS remote_access FROM child_databases WHERE account_id = ? ORDER BY db_name`),
			"db_users":  queryAll(`SELECT id, username, created_at FROM db_users WHERE account_id = ? ORDER BY username`),
			"emails":    queryAll(`SELECT id, email, COALESCE(forward_to,'') AS forward_to, quota_mb, send_limit, send_used, status, created_at FROM email_accounts WHERE account_id = ? ORDER BY email`),
			"ftp":       queryAll(`SELECT id, username, domain, directory, COALESCE(quota_mb,0) AS quota_mb, status, created_at FROM ftp_accounts WHERE account_id = ? ORDER BY username`),
			"cron_total":    cronTotal,
			"backup_total":  backupTotal,
			"schedule_total": scheduleTotal,
		})
	})

	// GET /{id}/activity — recent audit trail for one account (admin detail
	// view). Matches audit rows whose details JSON references the account id.
	r.Get("/{id}/activity", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		rows, err := db.Query(`SELECT id, actor_type, actor_id, action, COALESCE(details,'{}'),
			COALESCE(ip_address,''), created_at FROM audit_log
			WHERE CAST(json_extract(details, '$.id') AS INTEGER) = ?
			   OR CAST(json_extract(details, '$.account_id') AS INTEGER) = ?
			ORDER BY id DESC LIMIT 50`, id, id)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()
		type Entry struct {
			ID        int    `json:"id"`
			ActorType string `json:"actor_type"`
			ActorID   int    `json:"actor_id"`
			Action    string `json:"action"`
			Details   string `json:"details"`
			IPAddress string `json:"ip_address"`
			CreatedAt string `json:"created_at"`
		}
		out := make([]Entry, 0)
		for rows.Next() {
			var e Entry
			if err := rows.Scan(&e.ID, &e.ActorType, &e.ActorID, &e.Action, &e.Details, &e.IPAddress, &e.CreatedAt); err != nil {
				continue
			}
			out = append(out, e)
		}
		jsonResp(w, 200, out)
	})

	r.Post("/{id}/suspend", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct{ Reason string `json:"reason"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		auditLog(db, r, "account.suspend", map[string]interface{}{"id": id, "reason": req.Reason})
		result, err := db.Exec("UPDATE accounts SET status='suspended', suspended_reason=?, suspended_at=datetime('now'), updated_at=datetime('now') WHERE id=?", req.Reason, id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			jsonError(w, 404, "account not found")
			return
		}
		// Stop the Docker container for this account
		var containerName string
		db.QueryRow("SELECT container_name FROM docker_containers WHERE account_id = ?", id).Scan(&containerName)
		if containerName != "" {
			if err := docker.StopContainer(containerName); err != nil {
				log.Printf("[DOCKER] Failed to stop container %s: %v", containerName, err)
			} else {
				db.Exec("UPDATE docker_containers SET status = 'stopped', updated_at = datetime('now') WHERE account_id = ?", id)
				log.Printf("[DOCKER] Container %s stopped for suspended account %d", containerName, id)
			}
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
		result, err := db.Exec("UPDATE accounts SET status='active', suspended_reason=NULL, suspended_at='', updated_at=datetime('now') WHERE id=?", id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			jsonError(w, 404, "account not found")
			return
		}
		// Restart the Docker container for this account
		var containerName string
		db.QueryRow("SELECT container_name FROM docker_containers WHERE account_id = ?", id).Scan(&containerName)
		if containerName != "" {
			if err := docker.StartContainer(containerName); err != nil {
				log.Printf("[DOCKER] Failed to start container %s: %v", containerName, err)
			} else {
				db.Exec("UPDATE docker_containers SET status = 'running', updated_at = datetime('now') WHERE account_id = ?", id)
				log.Printf("[DOCKER] Container %s started for unsuspended account %d", containerName, id)
			}
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

		// Remove the Docker container for this account
		var containerName string
		db.QueryRow("SELECT container_name FROM docker_containers WHERE account_id = ?", id).Scan(&containerName)
		if containerName != "" {
			if err := docker.RemoveContainer(containerName); err != nil {
				log.Printf("[DOCKER] Failed to remove container %s: %v", containerName, err)
			} else {
				db.Exec("DELETE FROM docker_containers WHERE account_id = ?", id)
				log.Printf("[DOCKER] Container %s removed for terminated account %d", containerName, id)
			}
		}

		jsonResp(w, 200, map[string]string{"status": "terminated"})
	})

	// DELETE /{id}/purge — irreversibly delete an account and ALL of its
	// resources: home files, MySQL databases/users, mail, FTP, cron, backups,
	// SSL, redirects, error pages, domains, tickets, tokens, vhosts and
	// containers. Returns per-category counts.
	r.Delete("/{id}/purge", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		deleted, err := purgeAccountByID(db, r, id)
		if err == sql.ErrNoRows {
			jsonError(w, 404, "account not found")
			return
		}
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonResp(w, 200, map[string]interface{}{"status": "purged", "deleted": deleted})
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
		// Sync Docker container RAM limit
		var containerName string
		db.QueryRow("SELECT container_name FROM docker_containers WHERE account_id = ?", id).Scan(&containerName)
		if containerName != "" {
			cpu := getEffectiveCPULimit(db, id)
			if err := docker.UpdateResourceLimits(containerName, req.LimitMB, cpu); err != nil {
				log.Printf("[DOCKER] Failed to update RAM limit for container %s: %v", containerName, err)
			} else {
				db.Exec("UPDATE docker_containers SET ram_limit_mb = ?, cpu_limit = ?, updated_at = datetime('now') WHERE account_id = ?",
					req.LimitMB, cpu, id)
				log.Printf("[DOCKER] Resource limits synced for container %s (RAM: %d MB, CPU: %.2f)", containerName, req.LimitMB, cpu)
			}
		}
		jsonResp(w, 200, map[string]string{"status": "updated", "limit_mb": fmt.Sprintf("%d", req.LimitMB)})
	})

	// GET /{id}/resource-limits — package defaults, per-account overrides and
	// the effective values that apply to the account.
	r.Get("/{id}/resource-limits", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var pkgDisk, pkgBW, pkgRAM, pkgDB, pkgEmail, pkgFTP, pkgDomains, pkgSubdomains, pkgSSH int
		var pkgFiles, pkgEmails, pkgFTPF, pkgDBF, pkgBackups, pkgCron int
		if err := db.QueryRow(`SELECT p.disk_mb, p.bandwidth_mb, COALESCE(p.ram_limit_mb, 0), p.max_db,
			p.max_email, p.max_ftp, p.max_domains, p.max_subdomains, p.ssh_access,
			COALESCE(p.files_enabled, 1), COALESCE(p.emails_enabled, 1), COALESCE(p.ftp_enabled, 1),
			COALESCE(p.db_enabled, 1), COALESCE(p.backup_enabled, 1), COALESCE(p.cron_enabled, 1)
			FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`, id).Scan(
			&pkgDisk, &pkgBW, &pkgRAM, &pkgDB, &pkgEmail, &pkgFTP, &pkgDomains, &pkgSubdomains, &pkgSSH,
			&pkgFiles, &pkgEmails, &pkgFTPF, &pkgDBF, &pkgBackups, &pkgCron); err != nil {
			jsonError(w, 404, "account not found")
			return
		}
		override := func(key string, fallback int) int {
			var v int
			db.QueryRow("SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = ?", key).Scan(&v)
			if v <= 0 {
				return fallback
			}
			return v
		}
		var accRAM, usedRAM int
		db.QueryRow("SELECT COALESCE(ram_limit_mb,0) FROM accounts WHERE id = ?", id).Scan(&accRAM)
		db.QueryRow("SELECT COALESCE(ram_used_mb,0) FROM accounts WHERE id = ?", id).Scan(&usedRAM)
		effRAM := accRAM
		if effRAM <= 0 {
			effRAM = pkgRAM
		}
		effCPU := getEffectiveCPULimit(db, id)
		var cpuOv float64
		if err := db.QueryRow("SELECT CAST(value AS REAL) FROM server_config WHERE key_name = ?", fmt.Sprintf("cpu_limit_%d", id)).Scan(&cpuOv); err == nil && cpuOv > 0 {
			effCPU = cpuOv
		}
		effDisk := override(fmt.Sprintf("disk_limit_%d", id), pkgDisk)
		effBW := override(fmt.Sprintf("bandwidth_limit_%d", id), pkgBW)
		effDB := override(fmt.Sprintf("max_db_%d", id), pkgDB)
		effEmail := override(fmt.Sprintf("max_email_%d", id), pkgEmail)
		effFTP := override(fmt.Sprintf("max_ftp_%d", id), pkgFTP)
		effDomains := override(fmt.Sprintf("max_domains_%d", id), pkgDomains)
		effSub := override(fmt.Sprintf("max_subdomains_%d", id), pkgSubdomains)
		effSSH := getSSHAccess(db, id, pkgSSH)
		// Per-feature effective values (-1 = package default).
		effFeatures := map[string]bool{}
		pkgFeatures := map[string]int{
			"files": pkgFiles, "emails": pkgEmails, "ftp": pkgFTPF,
			"db": pkgDBF, "backups": pkgBackups, "cron": pkgCron,
		}
		ovFeatures := map[string]int{}
		for _, f := range []string{"files", "emails", "ftp", "db", "backups", "cron"} {
			ovFeatures[f] = getFeatureOverride(db, id, f)
			eff := pkgFeatures[f] == 1
			if ovFeatures[f] != -1 {
				eff = ovFeatures[f] == 1
			}
			effFeatures[f] = eff
		}
		var defaultUpload, maxUpload, perUpload int
		db.QueryRow("SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = 'default_upload_limit_mb'), 2048)").Scan(&defaultUpload)
		db.QueryRow("SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = 'max_upload_limit_mb'), 5120)").Scan(&maxUpload)
		db.QueryRow("SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = 'upload_limit_' || ?), 0)", id).Scan(&perUpload)
		effUpload := defaultUpload
		if perUpload > 0 {
			effUpload = perUpload
		}
		jsonResp(w, 200, map[string]interface{}{
			"package": map[string]interface{}{
				"disk_mb": pkgDisk, "bandwidth_mb": pkgBW, "ram_limit_mb": pkgRAM,
				"cpu_limit": getEffectiveCPULimit(db, id), "max_db": pkgDB, "max_email": pkgEmail,
				"max_ftp": pkgFTP, "max_domains": pkgDomains, "max_subdomains": pkgSubdomains,
				"ssh_access": pkgSSH, "upload_limit_mb": defaultUpload,
				"files_enabled": pkgFiles == 1, "emails_enabled": pkgEmails == 1,
				"ftp_enabled": pkgFTPF == 1, "db_enabled": pkgDBF == 1,
				"backups_enabled": pkgBackups == 1, "cron_enabled": pkgCron == 1,
			},
			"overrides": map[string]interface{}{
				"cpu_limit":       effCPU,
				"disk_mb":         effDisk,
				"bandwidth_mb":    effBW,
				"ram_limit_mb":    effRAM,
				"max_db":          effDB,
				"max_email":       effEmail,
				"max_ftp":         effFTP,
				"max_domains":     effDomains,
				"max_subdomains":  effSub,
				"ssh_access":      effSSH,
				"upload_limit_mb": effUpload,
				"feature_files":   ovFeatures["files"],
				"feature_emails":  ovFeatures["emails"],
				"feature_ftp":     ovFeatures["ftp"],
				"feature_db":      ovFeatures["db"],
				"feature_backups": ovFeatures["backups"],
				"feature_cron":    ovFeatures["cron"],
			},
			"effective": map[string]interface{}{
				"cpu_limit": effCPU, "disk_mb": effDisk, "bandwidth_mb": effBW, "ram_limit_mb": effRAM,
				"max_db": effDB, "max_email": effEmail, "max_ftp": effFTP, "max_domains": effDomains,
				"max_subdomains": effSub, "ssh_access": effSSH, "upload_limit_mb": effUpload,
				"ram_used_mb": usedRAM,
				"features": effFeatures,
			},
		})
	})

	// PUT /{id}/resource-limits — set per-account overrides. A value of 0 means
	// "use the package default" for that resource. RAM is stored on the account
	// row (and synced to the Docker container); the rest live in server_config.
	r.Put("/{id}/resource-limits", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct {
			CPULimit      float64 `json:"cpu_limit"`
			DiskMB        int     `json:"disk_mb"`
			BandwidthMB   int     `json:"bandwidth_mb"`
			RAMLimitMB    int     `json:"ram_limit_mb"`
			MaxDB         int     `json:"max_db"`
			MaxEmail      int     `json:"max_email"`
			MaxFTP        int     `json:"max_ftp"`
			MaxDomains    int     `json:"max_domains"`
			MaxSubdomains int     `json:"max_subdomains"`
			SSHAccess     int     `json:"ssh_access"`
			UploadLimitMB int     `json:"upload_limit_mb"`
			FeatureFiles  *int    `json:"feature_files"`
			FeatureEmails *int    `json:"feature_emails"`
			FeatureFTP    *int    `json:"feature_ftp"`
			FeatureDB     *int    `json:"feature_db"`
			FeatureBackup *int    `json:"feature_backups"`
			FeatureCron   *int    `json:"feature_cron"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		if req.SSHAccess < -1 || req.SSHAccess > 1 {
			writeAppError(w, r, apperrors.Validation("ssh_access must be -1, 0, or 1"))
			return
		}
		// Tri-state feature gates: nil = leave unchanged, -1 = inherit package, 0/1 = force.
		for name, ptr := range map[string]*int{
			"files": req.FeatureFiles, "emails": req.FeatureEmails, "ftp": req.FeatureFTP,
			"db": req.FeatureDB, "backups": req.FeatureBackup, "cron": req.FeatureCron,
		} {
			if ptr != nil && *ptr < -1 || ptr != nil && *ptr > 1 {
				writeAppError(w, r, apperrors.Validation(fmt.Sprintf("feature_%s must be -1, 0, or 1", name)))
				return
			}
		}
		var exists int
		if err := db.QueryRow("SELECT COUNT(*) FROM accounts WHERE id = ?", id).Scan(&exists); err != nil || exists == 0 {
			jsonError(w, 404, "account not found")
			return
		}
		for k, v := range map[string]interface{}{
			"cpu_limit_%d":    req.CPULimit,
			"disk_limit_%d":   req.DiskMB,
			"bandwidth_limit_%d": req.BandwidthMB,
			"max_db_%d":       req.MaxDB,
			"max_email_%d":    req.MaxEmail,
			"max_ftp_%d":      req.MaxFTP,
			"max_domains_%d":  req.MaxDomains,
			"max_subdomains_%d": req.MaxSubdomains,
			"ssh_access_%d":   req.SSHAccess,
			"upload_limit_%d": req.UploadLimitMB,
		} {
			key := fmt.Sprintf(k, id)
			db.Exec(`INSERT OR REPLACE INTO server_config (key_name, value, updated_at) VALUES (?, ?, datetime('now'))`,
				key, fmt.Sprintf("%v", v))
		}
		// Tri-state feature overrides (only when explicitly provided).
		for name, ptr := range map[string]*int{
			"files": req.FeatureFiles, "emails": req.FeatureEmails, "ftp": req.FeatureFTP,
			"db": req.FeatureDB, "backups": req.FeatureBackup, "cron": req.FeatureCron,
		} {
			if ptr == nil {
				continue
			}
			db.Exec(`INSERT OR REPLACE INTO server_config (key_name, value, updated_at) VALUES (?, ?, datetime('now'))`,
				fmt.Sprintf("feature_%s_%d", name, id), strconv.Itoa(*ptr))
		}
		// RAM is a column on accounts (kept for backward compat with old endpoints)
		db.Exec("UPDATE accounts SET ram_limit_mb = ?, updated_at = datetime('now') WHERE id = ?", req.RAMLimitMB, id)
		// Sync the Docker container with the effective RAM/CPU limits
		var containerName string
		db.QueryRow("SELECT container_name FROM docker_containers WHERE account_id = ?", id).Scan(&containerName)
		if containerName != "" {
			cpu := req.CPULimit
			if cpu <= 0 {
				cpu = getEffectiveCPULimit(db, id)
			}
			if err := docker.UpdateResourceLimits(containerName, req.RAMLimitMB, cpu); err != nil {
				log.Printf("[DOCKER] Failed to sync limits for container %s: %v", containerName, err)
			} else {
				db.Exec("UPDATE docker_containers SET ram_limit_mb = ?, cpu_limit = ?, updated_at = datetime('now') WHERE account_id = ?",
					req.RAMLimitMB, cpu, id)
			}
		}
		auditLog(db, r, "account.resource_limits", map[string]interface{}{
			"id": id, "cpu": req.CPULimit, "disk_mb": req.DiskMB, "bandwidth_mb": req.BandwidthMB,
			"ram_mb": req.RAMLimitMB, "max_db": req.MaxDB, "max_email": req.MaxEmail, "max_ftp": req.MaxFTP,
			"max_domains": req.MaxDomains, "max_subdomains": req.MaxSubdomains, "ssh": req.SSHAccess,
			"feature_files": req.FeatureFiles, "feature_emails": req.FeatureEmails,
			"feature_ftp": req.FeatureFTP, "feature_db": req.FeatureDB,
			"feature_backups": req.FeatureBackup, "feature_cron": req.FeatureCron,
		})
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

 	// PUT /{id}/package — change the account's hosting package and re-provision
	// the Docker container so the new RAM/CPU limits apply.
	r.Put("/{id}/package", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct {
			PackageID int `json:"package_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		var pkgCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM packages WHERE id = ?", req.PackageID).Scan(&pkgCount); err != nil || pkgCount == 0 {
			jsonError(w, 400, "selected package does not exist")
			return
		}
		result, err := db.Exec("UPDATE accounts SET package_id = ?, updated_at = datetime('now') WHERE id = ?", req.PackageID, id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			jsonError(w, 404, "account not found")
			return
		}
		auditLog(db, r, "account.package_change", map[string]interface{}{"id": id, "package_id": req.PackageID})
		// Re-sync container limits from the new package (respecting any
		// per-account RAM override set by the resource-limits endpoint).
		var containerName string
		db.QueryRow("SELECT container_name FROM docker_containers WHERE account_id = ?", id).Scan(&containerName)
		if containerName != "" {
			ram := getEffectiveRAMLimit(db, id)
			cpu := getEffectiveCPULimit(db, id)
			if err := docker.UpdateResourceLimits(containerName, ram, cpu); err != nil {
				log.Printf("[DOCKER] Failed to update limits after package change for container %s: %v", containerName, err)
			} else {
				db.Exec("UPDATE docker_containers SET ram_limit_mb = ?, cpu_limit = ?, updated_at = datetime('now') WHERE account_id = ?",
					ram, cpu, id)
				log.Printf("[DOCKER] Container %s re-provisioned for new package (RAM: %d MB, CPU: %.2f)", containerName, ram, cpu)
			}
		}
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	// PUT /{id} — edit editable account info (email, domain). Username is not
	// editable because it is tied to the system user, home directory, Docker
	// container name and MySQL database prefixes.
	r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct {
			Email  string `json:"email"`
			Domain string `json:"domain"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		if req.Email != "" && !strings.Contains(req.Email, "@") {
			jsonError(w, 400, "invalid email address")
			return
		}
		if req.Domain != "" {
			if !validDomain(req.Domain) {
				writeAppError(w, r, apperrors.Validation("invalid domain"))
				return
			}
			var inUse int
			if err := db.QueryRow(`SELECT COUNT(*) FROM accounts WHERE domain = ? AND id != ?`, req.Domain, id).Scan(&inUse); err != nil && err != sql.ErrNoRows {
				jsonError(w, 500, "failed to check domain")
				return
			}
			if inUse > 0 {
				jsonError(w, 409, "this domain is already in use by another account")
				return
			}
		}
		var oldDomain string
		db.QueryRow("SELECT domain FROM accounts WHERE id = ?", id).Scan(&oldDomain)
		result, err := db.Exec(`UPDATE accounts SET email = COALESCE(NULLIF(?, ''), email),
			domain = COALESCE(NULLIF(?, ''), domain), updated_at = datetime('now') WHERE id = ?`,
			req.Email, req.Domain, id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			jsonError(w, 404, "account not found")
			return
		}
		// Keep the primary domain row + nginx vhost in sync when the domain changes
		if req.Domain != "" && req.Domain != oldDomain && oldDomain != "" {
			db.Exec("UPDATE domains SET domain = ? WHERE account_id = ? AND type = 'primary'", req.Domain, id)
			var docRoot string
			db.QueryRow("SELECT doc_root FROM domains WHERE account_id = ? AND type = 'primary'", id).Scan(&docRoot)
			removeNginxVhost(sanitizeDomain(oldDomain))
			if err := writeNginxVhost(sanitizeDomain(req.Domain), docRoot, "", 0, ""); err != nil {
				log.Printf("[ACCOUNTS] Failed to rewrite vhost for %s after edit: %v", req.Domain, err)
			}
			reloadNginx()
		}
		auditLog(db, r, "account.edit", map[string]interface{}{"id": id, "email": req.Email, "domain": req.Domain})
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	// POST /{id}/login-as — mint a child-scoped token pair for the account so an
	// admin can drop into the Site Panel without needing the account password.
	r.Post("/{id}/login-as", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var username string
		var status string
		if err := db.QueryRow("SELECT username, status FROM accounts WHERE id = ?", id).Scan(&username, &status); err != nil {
			jsonError(w, 404, "account not found")
			return
		}
		if status != "active" {
			jsonError(w, 403, "account is "+status)
			return
		}
		claims := &auth.Claims{
			UserID:    id,
			Username:  username,
			Role:      "account",
			Scope:     "child",
			AccountID: id,
		}
		tokens, err := jwtManager.GenerateTokenPair(claims)
		if err != nil {
			jsonError(w, 500, "token generation failed")
			return
		}
		db.Exec(`INSERT INTO refresh_tokens (user_id, token_hash, scope, expires_at) VALUES (?, ?, 'child', datetime('now', '+7 days'))`,
			id, hashRefreshToken(tokens.RefreshToken))
		auditLog(db, r, "account.login_as", map[string]interface{}{"id": id, "username": username})
		jsonResp(w, 200, map[string]interface{}{
			"access_token":  tokens.AccessToken,
			"refresh_token": tokens.RefreshToken,
			"expires_in":    tokens.ExpiresIn,
			"user":          map[string]interface{}{"id": id, "username": username, "role": "account", "home_dir": ""},
		})
	})
}

// --- Stats ---

func statsRoutes(r chi.Router, db *sql.DB) {
	r.Get("/overview", func(w http.ResponseWriter, r *http.Request) {
		stats := make(map[string]interface{})
		var count int

		db.QueryRow("SELECT COUNT(*) FROM accounts WHERE status = 'active'").Scan(&count)
		stats["active_accounts"] = count
		db.QueryRow("SELECT COUNT(*) FROM accounts WHERE status = 'suspended'").Scan(&count)
		stats["suspended_accounts"] = count
		db.QueryRow("SELECT COUNT(*) FROM accounts WHERE status = 'pending'").Scan(&count)
		stats["pending_accounts"] = count
		db.QueryRow("SELECT COALESCE(CAST(SUM(disk_used_mb) AS INTEGER), 0) FROM accounts").Scan(&count)
		stats["total_disk_used_mb"] = count
		var bwSum float64
		db.QueryRow("SELECT COALESCE(CAST(SUM(bytes_out + bytes_in) AS REAL)/1048576.0, 0) FROM bandwidth_logs").Scan(&bwSum)
		stats["total_bandwidth_used_mb"] = bwSum
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
	nginxBin    = getEnvDefault("NGINX_BIN", "/usr/sbin/nginx")
	nginxConf   = getEnvDefault("NGINX_CONF", nginxPrefix+"/nginx.conf")
	// nginxMu serializes all vhost file writes (sync sweeps vs per-request
	// SSL add/remove) so concurrent writers can't interleave duplicate or
	// half-written 443 blocks. Callers inside a held section must use the
	// do* variants below.
	nginxMu sync.Mutex
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
	domain           string
	docRoot          string
	containerIP      string
	hotlinkEnabled   int
	hotlinkDomains   string
}

func syncNginxVhosts(db *sql.DB) {
	nginxMu.Lock()
	defer nginxMu.Unlock()
	os.MkdirAll(vhostDir, 0755)
	// Only write vhosts for active accounts, deduplicated by domain name.
	// When multiple active accounts share the same domain, the most recently
	// added domain record wins (highest id).
	rows, err := db.Query(`SELECT d.domain, d.doc_root, COALESCE(dc.container_ip, ''),
		COALESCE(hp.enabled, 0), COALESCE(hp.allowed_domains, '')
		FROM domains d
		JOIN accounts a ON a.id = d.account_id
		LEFT JOIN docker_containers dc ON dc.account_id = a.id
		LEFT JOIN hotlink_protection hp ON hp.account_id = a.id
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
		if err := rows.Scan(&v.domain, &v.docRoot, &v.containerIP, &v.hotlinkEnabled, &v.hotlinkDomains); err != nil {
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
		if err := writeNginxVhost(safeDomain, v.docRoot, v.containerIP, v.hotlinkEnabled, v.hotlinkDomains); err != nil {
			log.Printf("[NGINX] Failed to write vhost for %s: %v", v.domain, err)
		} else {
			log.Printf("[NGINX] Synced vhost for %s (container_ip=%s)", v.domain, v.containerIP)
		}
		// Key by the on-disk (sanitized) filename so the stale-file sweep
		// below never deletes a live vhost whose name was sanitized.
		activeVhosts[safeDomain] = true
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

	// Restore SSL server blocks for domains with live certs (only for active vhosts).
	// Both real ('issued') and fallback ('self-signed') rows restore port 443:
	// a browser warning is strictly better than a refused connection.
	certRows, err := db.Query("SELECT DISTINCT domain FROM ssl_certs WHERE status IN ('issued', 'self-signed')")
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
		if activeVhosts[sanitizeDomain(domain)] {
			if err := doAddNginxSSL(db, domain); err != nil {
				log.Printf("[NGINX] HTTPS restore failed for %s: %v", domain, err)
			}
		}
	}
	// Re-apply HTTP->HTTPS redirects for domains that enforce them.
	forceRows, err := db.Query(`SELECT DISTINCT d.domain FROM domains d
		JOIN accounts a ON a.id = d.account_id
		WHERE a.status = 'active' AND d.force_https = 1`)
	if err == nil {
		var forceDomains []string
		for forceRows.Next() {
			var domain string
			forceRows.Scan(&domain)
			forceDomains = append(forceDomains, domain)
		}
		forceRows.Close()
		for _, domain := range forceDomains {
			if activeVhosts[sanitizeDomain(domain)] {
				if err := doApplyForceHTTPS(db, domain); err != nil {
					log.Printf("[NGINX] HTTPS redirect restore failed for %s: %v", domain, err)
				}
			}
		}
	}
	// Re-apply redirect + error-page snippets (plain :80 rewrites wipe them).
	for _, v := range vhosts {
		if err := doSyncDomainRedirects(db, v.domain); err != nil {
			log.Printf("[NGINX] redirect restore failed for %s: %v", v.domain, err)
		}
		if err := doSyncDomainErrorPages(db, v.domain); err != nil {
			log.Printf("[NGINX] error-page restore failed for %s: %v", v.domain, err)
		}
	}
	reloadNginx()
}

func addHotlinkBlock(cfg string, enabled int, domains string) string {
if enabled == 0 || domains == "" {
		return cfg
	}
	extensions := "(jpg|jpeg|png|gif|webp|svg|ico|css|js|mp4|avi|mov|pdf|zip|ttf|otf|woff|woff2|eot)"
	refs := "none blocked server_names"
	for _, d := range strings.Split(domains, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			refs += " ~." + d
		}
	}
	block := fmt.Sprintf(`
    location ~* \.%s$ {
        valid_referers %s;
        if ($invalid_referer) { return 403; }
    }
`, extensions, refs)
	if idx := strings.LastIndex(cfg, "location ^~ /.well-known"); idx >= 0 {
		cfg = cfg[:idx] + "\n" + block + "\n" + cfg[idx:]
	} else {
		cfg += "\n" + block + "\n"
	}
	return cfg
}

func writeVhostFile(path string, data []byte, perm os.FileMode) error {
	err := os.WriteFile(path, data, perm)
	if err != nil && os.IsPermission(err) {
		os.Remove(path)
		err = os.WriteFile(path, data, perm)
	}
	return err
}

func writeNginxVhost(domain, docRoot string, containerIP string, hotlinkEnabled int, hotlinkDomains string) error {
	logDir := getNginxLogDir()
	os.MkdirAll(logDir, 0755)

	if containerIP == "" {
		phpFpmSocket := getEnvDefault("PHP_FPM_SOCKET", "/run/php/php8.3-fpm.sock")
		cfg := fmt.Sprintf(`# OpenWebPanel -- %s
server {
    listen 80;
    listen [::]:80;
    server_name %s www.%s;
    client_max_body_size 2048M;
    root %s;
    index index.html index.htm index.php;

    access_log `+logDir+`/%s.access.log;
    error_log `+logDir+`/%s.error.log;

    location / {
        autoindex on;
        try_files $uri $uri/ /index.php?$args;
    }

    location ~ \.php$ {
        fastcgi_pass unix:`+phpFpmSocket+`;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        include fastcgi_params;
    }

    location ^~ /.well-known/acme-challenge/ {
        proxy_pass http://127.0.0.1:9000;
        proxy_set_header Host $host;
    }

    location ~ /\.owp {
        deny all;
    }
}
`, domain, domain, domain, docRoot, domain, domain)
		cfg = addHotlinkBlock(cfg, hotlinkEnabled, hotlinkDomains)
		return writeVhostFile(vhostDir+domain+".conf", []byte(cfg), 0644)
	}

	cfg := fmt.Sprintf(`# OpenWebPanel -- %s
server {
    listen 80;
    listen [::]:80;
    server_name %s www.%s;
    client_max_body_size 2048M;

    access_log `+logDir+`/%s.access.log;
    error_log `+logDir+`/%s.error.log;

    location / {
        proxy_pass http://%s;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
    }

    location ^~ /.well-known/acme-challenge/ {
        proxy_pass http://127.0.0.1:9000;
        proxy_set_header Host $host;
    }
}
`, domain, domain, domain, domain, domain, containerIP)
		cfg = addHotlinkBlock(cfg, hotlinkEnabled, hotlinkDomains)
	return writeVhostFile(vhostDir+domain+".conf", []byte(cfg), 0644)
}

func removeNginxVhost(domain string) error {
	return os.Remove(vhostDir + sanitizeDomain(domain) + ".conf")
}

func reloadNginx() {
	if err := reloadNginxErr(); err != nil {
		log.Printf("[NGINX] reload failed: %v", err)
	}
}

// reloadNginxErr reloads nginx, returning the failure instead of only logging
// it so SSL/vhost flows can surface the real error to the operator.
func reloadNginxErr() error {
	// Test the configuration before touching the running process so a bad
	// vhost (e.g. a crafted hotlink/rewrite block) can never take nginx down.
	if out, err := runCmd("sudo", "-n", nginxBin, "-t"); err != nil {
		// fall back to testing without sudo (nginx may be reachable directly)
		if out2, err2 := runCmd(nginxBin, "-t"); err2 != nil {
			log.Printf("[NGINX] config test failed, skipping reload: %v\n%s", testErrOr(err, err2), or(out, out2))
			return fmt.Errorf("nginx config test failed: %w", testErrOr(err, err2))
		}
	}
	if out, err := runCmd("sudo", "-n", nginxBin, "-s", "reload"); err != nil {
		log.Printf("[NGINX] sudo -n reload failed: %v\n%s", err, out)
		if out2, err2 := runCmd(nginxBin, "-s", "reload"); err2 != nil {
			log.Printf("[NGINX] Direct reload also failed: %v\n%s", err2, out2)
			return fmt.Errorf("nginx reload failed: %w", testErrOr(err, err2))
		}
		log.Printf("[NGINX] Reloaded successfully")
		return nil
	}
	log.Printf("[NGINX] Reloaded successfully")
	return nil
}

func testErrOr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
const maxLoginAttempts = 5
const maxUsernameAttempts = 10

func isIPBlocked(db *sql.DB, ipAddress string) bool {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM blocked_ips WHERE ip_address = ?", ipAddress).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

func isUsernameBlocked(db *sql.DB, username string) bool {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM blocked_ips WHERE reason = ?`,
		"Username blocked: "+username).Scan(&count)
	return count > 0
}

func recordLoginAttempt(db *sql.DB, username, ipAddress, userAgent string, success bool) {
	db.Exec(`INSERT INTO login_attempts (username, ip_address, success, user_agent) VALUES (?, ?, ?, ?)`,
		username, ipAddress, boolToInt(success), userAgent)
	if !success {
		var ipFailedCount int
		db.QueryRow(`SELECT COUNT(*) FROM login_attempts 
			WHERE ip_address = ? AND success = 0 
			AND created_at > datetime('now', '-15 minutes')`, ipAddress).Scan(&ipFailedCount)
		if ipFailedCount >= maxLoginAttempts {
			db.Exec(`INSERT OR REPLACE INTO blocked_ips (ip_address, reason, blocked_by, failed_attempts, updated_at) 
				VALUES (?, 'Exceeded maximum failed login attempts', 'system', ?, datetime('now'))`,
				ipAddress, ipFailedCount)
			log.Printf("[SECURITY] IP %s blocked: %d failed login attempts in 15 minutes", ipAddress, ipFailedCount)
		}

		var userFailedCount int
		db.QueryRow(`SELECT COUNT(*) FROM login_attempts 
			WHERE username = ? AND success = 0 
			AND created_at > datetime('now', '-15 minutes')`, username).Scan(&userFailedCount)
		if userFailedCount >= maxUsernameAttempts {
			db.Exec(`INSERT OR REPLACE INTO blocked_ips (ip_address, reason, blocked_by, failed_attempts, updated_at) 
				VALUES (?, ?, 'system', ?, datetime('now'))`,
				"username:"+username, "Username blocked: "+username, userFailedCount)
			log.Printf("[SECURITY] Username %s blocked: %d failed login attempts in 15 minutes", username, userFailedCount)
		}
	}
}

func hashRefreshToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
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
	// Only trust X-Forwarded-For / X-Real-IP when the direct peer is a trusted
	// reverse proxy (nginx runs on loopback). Otherwise a client could spoof
	// these headers to bypass per-IP brute-force lockouts and rate limiting.
	if isTrustedProxy(host) {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
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
	}
	return host
}

// isTrustedProxy reports whether a direct peer address may supply forwarded
// client-IP headers. nginx proxies from loopback; additional trusted proxies
// can be listed in OWP_TRUSTED_PROXIES (comma-separated IPs or CIDRs).
func isTrustedProxy(peer string) bool {
	ip := net.ParseIP(peer)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return true
	}
	for _, cidrStr := range strings.Split(os.Getenv("OWP_TRUSTED_PROXIES"), ",") {
		cidrStr = strings.TrimSpace(cidrStr)
		if cidrStr == "" {
			continue
		}
		if strings.Contains(cidrStr, "/") {
			if _, ipNet, err := net.ParseCIDR(cidrStr); err == nil && ipNet.Contains(ip) {
				return true
			}
		} else if other := net.ParseIP(cidrStr); other != nil && other.Equal(ip) {
			return true
		}
	}
	return false
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
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}

		clientIP := getClientIP(r)

		if isUsernameBlocked(db, req.Username) {
			recordLoginAttempt(db, req.Username, clientIP, r.UserAgent(), false)
			jsonError(w, 429, "account temporarily locked due to too many failed attempts")
			return
		}

		if isIPBlocked(db, clientIP) {
			recordLoginAttempt(db, req.Username, clientIP, r.UserAgent(), false)
			jsonError(w, 429, "too many failed login attempts - your IP has been temporarily blocked")
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
			id, hashRefreshToken(tokens.RefreshToken))

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

		hashedToken := hashRefreshToken(req.RefreshToken)
		var userID int
		var scope string
		err := db.QueryRow(`SELECT user_id, scope FROM refresh_tokens WHERE token_hash = ? AND expires_at > datetime('now')`,
			hashedToken).Scan(&userID, &scope)
		if err != nil {
			jsonError(w, 401, "invalid or expired refresh token")
			return
		}
		if scope != "child" {
			jsonError(w, 401, "invalid or expired refresh token")
			return
		}

		db.Exec("DELETE FROM refresh_tokens WHERE token_hash = ?", hashedToken)

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
			userID, hashRefreshToken(tokens.RefreshToken))
		jsonResp(w, 200, tokens)
	})
}

// isPathWithin reports whether target is base or a descendant of base.
func isPathWithin(base, target string) bool {
	b := filepath.Clean(base)
	t := filepath.Clean(target)
	if t == b {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(t, b+sep)
}

// pathForResponse converts a resolved absolute filesystem path back into the
// home-relative form the File Manager UI uses for navigation and breadcrumbs.
// The home root is represented as "/".
func pathForResponse(home, resolved string) string {
	rel, err := filepath.Rel(home, resolved)
	if err != nil {
		return "/"
	}
	if rel == "." || rel == "" {
		return "/"
	}
	rel = filepath.ToSlash(rel)
	return "/" + strings.TrimPrefix(rel, "/")
}

// fsErrorMessage returns a client-safe message for a filesystem error. Client
// caused path-traversal rejections are preserved; anything else (which may
// embed server-side paths) is reduced to a generic message.
func fsErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	if strings.Contains(err.Error(), "path traversal detected") {
		return err.Error()
	}
	return "filesystem operation failed"
}

// safeFileError logs the real filesystem error and writes a sanitized response
// so server paths never leak to the client.
func safeFileError(w http.ResponseWriter, r *http.Request, status int, err error) {
	if err == nil {
		return
	}
	msg := fsErrorMessage(err)
	if msg == "filesystem operation failed" {
		getLogger(r).Errorf("filesystem operation failed: %v", err)
	}
	jsonError(w, status, msg)
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
			safeFileError(w, r, 403, err)
			return
		}

		entries, err := os.ReadDir(safePath)
		if err != nil {
			jsonError(w, 404, "directory not found")
			return
		}

		type FileEntry struct {
			Name      string `json:"name"`
			Type      string `json:"type"`
			Size      string `json:"size"`
			ModTime   string `json:"mod_time"`
			Perm      string `json:"perm"`
			Owner     string `json:"owner"`
			Group     string `json:"group"`
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
			owner, group := getOwnerGroupFromInfo(info)
			res = append(res, FileEntry{
				Name:      e.Name(),
				Type:      typ,
				Size:      sz,
				ModTime:   info.ModTime().Format(time.RFC3339),
				Perm:      info.Mode().String(),
				Owner:     owner,
				Group:     group,
			})
		}
		jsonResp(w, 200, map[string]interface{}{
			"path":    pathForResponse(home, safePath),
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
			safeFileError(w, r, 403, err)
			return
		}
		if err := os.MkdirAll(filepath.Join(safe, req.Name), 0755); err != nil {
			safeFileError(w, r, 500, err)
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
			safeFileError(w, r, 403, err)
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
		relPath, relErr := filepath.Rel(home, safe)
		if relErr != nil {
			getLogger(r).Errorf("failed to compute relative path: %v", relErr)
			jsonError(w, 500, "failed to compute relative path")
			return
		}
		var dbErr error
		_, dbErr = db.Exec(`INSERT INTO file_trash (account_id, original_path, trash_path, size_bytes, is_dir, expires_at)
			VALUES (?, ?, ?, ?, ?, datetime('now', '+30 days'))`,
			c.AccountID, relPath, trashPath, info.Size(), isDir)
		if dbErr != nil {
			getLogger(r).Errorf("failed to record trash entry: %v", dbErr)
			jsonError(w, 500, "failed to record trash entry")
			return
		}

		if err := os.Rename(safe, trashPath); err != nil {
			// Roll back the DB entry since the file move failed
			db.Exec("DELETE FROM file_trash WHERE account_id = ? AND trash_path = ?", c.AccountID, trashPath)
			safeFileError(w, r, 500, err)
			return
		}

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
			safeFileError(w, r, 403, err)
			return
		}
		newp, err := filesystem.SafePath(home, req.NewPath)
		if err != nil {
			safeFileError(w, r, 403, err)
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
			// Cross-device fallback: copy and delete
			if err := copyRecursive(old, newp); err != nil {
				safeFileError(w, r, 500, err)
				return
			}
			if err := os.RemoveAll(old); err != nil {
				safeFileError(w, r, 500, err)
				return
			}
			jsonResp(w, 200, map[string]string{"status": "renamed"}); return
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
			safeFileError(w, r, 403, err)
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
			safeFileError(w, r, 403, err)
			return
		}
		rel, rerr := filepath.Rel(home, safe)
		if rerr == nil && (strings.HasPrefix(rel, ".owp") || strings.HasPrefix(rel, ".trash") ||
			rel == ".owp" || rel == ".trash") {
			jsonError(w, 403, "writing to protected directories is not allowed")
			return
		}
		if err := os.MkdirAll(filepath.Dir(safe), 0755); err != nil {
			safeFileError(w, r, 500, err)
			return
		}
		if err := os.WriteFile(safe, []byte(req.Content), 0644); err != nil {
			safeFileError(w, r, 500, err)
			return
		}
		if cl := getClaims(r); cl != nil {
			touchDiskUsage(db, cl.AccountID)
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
			safeFileError(w, r, 403, err)
			return
		}
		tmpPath := safe + ".tmp"
		dst, err := os.Create(tmpPath)
		if err != nil {
			safeFileError(w, r, 500, err)
			return
		}
		defer os.Remove(tmpPath)

		if _, err := io.CopyBuffer(dst, file, make([]byte, 1024*1024)); err != nil {
			dst.Close()
			safeFileError(w, r, 500, err)
			return
		}
		if err := dst.Close(); err != nil {
			safeFileError(w, r, 500, err)
			return
		}
		if err := os.Rename(tmpPath, safe); err != nil {
			safeFileError(w, r, 500, err)
			return
		}
		os.Chmod(safe, 0644)

		touchDiskUsage(db, c.AccountID)
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
			safeFileError(w, r, 403, err)
			return
		}
		archiveName := filepath.Base(req.ArchiveName)
		archivePath := filepath.Join(safeDir, archiveName)
		zf, err := os.Create(archivePath)
		if err != nil {
			safeFileError(w, r, 500, err)
			return
		}
		os.Chmod(archivePath, 0644)
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
		var req struct {
			Path        string `json:"path"`
			Destination string `json:"destination"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body"); return
		}
		safePath, err := filesystem.SafePath(home, req.Path)
		if err != nil { safeFileError(w, r, 403, err); return }
		r2, err := zip.OpenReader(safePath)
		if err != nil { jsonError(w, 400, "not a valid zip"); return }
		defer r2.Close()
		// Pre-scan: reject password-protected ZIPs and check limits
		var totalFiles, totalBytes int64
		for _, f := range r2.File {
			if f.Flags&0x1 != 0 {
				jsonError(w, 400, "cannot extract password-protected zip"); return
			}
			totalFiles++
			if totalFiles > 10000 {
				jsonError(w, 400, "zip contains too many entries (max 10000)"); return
			}
			if !f.FileInfo().IsDir() {
				totalBytes += int64(f.UncompressedSize64)
				if totalBytes > 1<<30 {
					jsonError(w, 400, "zip uncompressed size exceeds limit (max 1GB)"); return
				}
			}
		}
		dest := filepath.Dir(safePath)
		if req.Destination != "" {
			safeDest, err := filesystem.SafePath(home, req.Destination)
			if err != nil { safeFileError(w, r, 403, err); return }
			dest = safeDest
		}
		cleanDest := filepath.Clean(dest)
		realHome, herr := filepath.EvalSymlinks(home)
		if herr != nil {
			jsonError(w, 500, "cannot resolve home"); return
		}
		realHomeSep := realHome + string(filepath.Separator)
		for _, f := range r2.File {
			// Prevent zip slip: reject absolute paths and path traversal
			if filepath.IsAbs(f.Name) {
				log.Printf("Rejecting absolute path entry in zip: %s", f.Name)
				continue
			}
			fp := filepath.Join(cleanDest, f.Name)
			if !strings.HasPrefix(fp, cleanDest+string(filepath.Separator)) && fp != cleanDest {
				log.Printf("Rejecting zip slip entry: %s -> %s", f.Name, fp)
				continue
			}
			if f.FileInfo().IsDir() { os.MkdirAll(fp, 0755); continue }
			os.MkdirAll(filepath.Dir(fp), 0755)
			// Ensure no symlinked ancestor redirects writes outside the home.
			if realParent, rpErr := filepath.EvalSymlinks(filepath.Dir(fp)); rpErr != nil {
				log.Printf("extract: cannot resolve parent %s: %v", filepath.Dir(fp), rpErr)
				continue
			} else if realParent != realHome && !strings.HasPrefix(realParent, realHomeSep) {
				log.Printf("extract: rejecting symlink escape: %s -> %s", fp, realParent)
				continue
			}
			src, err := f.Open()
			if err != nil { log.Printf("Failed to open zip entry %s: %v", f.Name, err); continue }
			dst, err := os.Create(fp)
			if err != nil { src.Close(); log.Printf("Failed to create %s: %v", fp, err); continue }
			if _, err := io.Copy(dst, src); err != nil {
				log.Printf("Failed to write %s: %v", fp, err)
			}
			src.Close(); dst.Close()
			os.Chmod(fp, 0644)
		}
		touchDiskUsage(db, c.AccountID)
		jsonResp(w, 200, map[string]string{"status": "extracted"})
	})

	// Move files/folders to a destination directory
	r.Post("/move", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" { jsonError(w, 403, "no home"); return }
		var req struct {
			Paths       []string `json:"paths"`
			Destination string   `json:"destination"`
			Overwrite   bool     `json:"overwrite"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body"); return
		}
		if len(req.Paths) == 0 {
			jsonError(w, 400, "no files specified"); return
		}
		if req.Destination == "" {
			jsonError(w, 400, "destination required"); return
		}
		safeDest, err := filesystem.SafePath(home, req.Destination)
		if err != nil { safeFileError(w, r, 403, err); return }
		destInfo, err := os.Stat(safeDest)
		if err != nil || !destInfo.IsDir() {
			jsonError(w, 400, "destination must be an existing directory"); return
		}
		// Ensure destination isn't inside protected directories
		relDest, _ := filepath.Rel(home, safeDest)
		relDest = filepath.ToSlash(relDest)
		if strings.HasPrefix(relDest, ".owp") || strings.HasPrefix(relDest, ".trash") {
			jsonError(w, 403, "cannot move to protected directories"); return
		}
		// Sort paths so children are processed before parents, preventing
		// parent-moves from invalidating child source paths during flatten/unwrap.
		sorted := make([]string, len(req.Paths))
		copy(sorted, req.Paths)
		sort.Slice(sorted, func(i, j int) bool {
			return strings.Count(sorted[i], "/") > strings.Count(sorted[j], "/")
		})
		var failures []string
		for _, p := range sorted {
			safeSrc, err := filesystem.SafePath(home, p)
			if err != nil { failures = append(failures, p+": "+fsErrorMessage(err)); continue }
			if _, err := os.Stat(safeSrc); err != nil {
				failures = append(failures, p+": source not found"); continue
			}
			relSrc, _ := filepath.Rel(home, safeSrc)
			relSrc = filepath.ToSlash(relSrc)
			if strings.HasPrefix(relSrc, ".owp") || strings.HasPrefix(relSrc, ".trash") {
				failures = append(failures, p+": cannot move protected files"); continue
			}
			destPath := filepath.Join(safeDest, filepath.Base(safeSrc))
			if _, err := os.Stat(destPath); err == nil {
				if req.Overwrite {
					if err := os.RemoveAll(destPath); err != nil {
						failures = append(failures, p+": "+fsErrorMessage(err)); continue
					}
				} else {
					failures = append(failures, p+": destination already exists"); continue
				}
			}
			if err := os.Rename(safeSrc, destPath); err != nil {
				// Fall back to copy+delete for cross-device moves (EXDEV) or
				// other cases where rename is not possible.
				if copyErr := copyRecursive(safeSrc, destPath); copyErr != nil {
					failures = append(failures, p+": "+fsErrorMessage(err)); continue
				}
				os.RemoveAll(safeSrc)
			}
		}
		if len(failures) > 0 {
			jsonResp(w, 200, map[string]interface{}{"status": "partial", "failures": failures})
		} else {
			jsonResp(w, 200, map[string]string{"status": "moved"})
		}
	})

	r.Post("/copy", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" { jsonError(w, 403, "no home"); return }
		var req struct {
			Paths       []string `json:"paths"`
			Destination string   `json:"destination"`
			Overwrite   bool     `json:"overwrite"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body"); return
		}
		if len(req.Paths) == 0 {
			jsonError(w, 400, "no files specified"); return
		}
		if req.Destination == "" {
			jsonError(w, 400, "destination required"); return
		}
		safeDest, err := filesystem.SafePath(home, req.Destination)
		if err != nil { safeFileError(w, r, 403, err); return }
		destInfo, err := os.Stat(safeDest)
		if err != nil || !destInfo.IsDir() {
			jsonError(w, 400, "destination must be an existing directory"); return
		}
		relDest, _ := filepath.Rel(home, safeDest)
		relDest = filepath.ToSlash(relDest)
		if strings.HasPrefix(relDest, ".owp") || strings.HasPrefix(relDest, ".trash") {
			jsonError(w, 403, "cannot copy to protected directories"); return
		}
		var failures []string
		for _, p := range req.Paths {
			safeSrc, err := filesystem.SafePath(home, p)
			if err != nil { failures = append(failures, p+": "+fsErrorMessage(err)); continue }
			relSrc, _ := filepath.Rel(home, safeSrc)
			relSrc = filepath.ToSlash(relSrc)
			if strings.HasPrefix(relSrc, ".owp") || strings.HasPrefix(relSrc, ".trash") {
				failures = append(failures, p+": cannot copy protected files"); continue
			}
			destPath := filepath.Join(safeDest, filepath.Base(safeSrc))
			if _, err := os.Stat(destPath); err == nil {
				if req.Overwrite {
					if err := os.RemoveAll(destPath); err != nil {
						failures = append(failures, p+": "+fsErrorMessage(err)); continue
					}
				} else {
					failures = append(failures, p+": destination already exists"); continue
				}
			}
			if err := copyRecursive(safeSrc, destPath); err != nil {
				failures = append(failures, p+": "+fsErrorMessage(err)); continue
			}
		}
		if len(failures) > 0 {
			jsonResp(w, 200, map[string]interface{}{"status": "partial", "failures": failures})
		} else {
			jsonResp(w, 200, map[string]string{"status": "copied"})
		}
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
		// Revalidate both paths are inside the account home before touching FS.
		if !isPathWithin(home, trashPath) {
			jsonError(w, 400, "invalid trash path"); return
		}
		origFull, err := filesystem.SafePath(home, origPath)
		if err != nil {
			jsonError(w, 400, "invalid restore path"); return
		}
		if err := os.Rename(trashPath, origFull); err != nil {
			// Cross-device fallback: copy and delete
			if err := copyRecursive(trashPath, origFull); err != nil {
				safeFileError(w, r, 500, err)
				return
			}
			os.RemoveAll(trashPath)
		}
		db.Exec("DELETE FROM file_trash WHERE id = ?", id)
		touchDiskUsage(db, c.AccountID)
		jsonResp(w, 200, map[string]string{"status": "restored"})
	})

	r.Delete("/trash/{id}", func(w http.ResponseWriter, r *http.Request) {
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
		var tp string
		err = db.QueryRow("SELECT trash_path FROM file_trash WHERE id = ? AND account_id = ?", id, c.AccountID).Scan(&tp)
		if err != nil {
			jsonError(w, 404, "trash entry not found"); return
		}
		if !isPathWithin(home, tp) {
			jsonError(w, 400, "invalid trash path"); return
		}
		os.RemoveAll(tp); db.Exec("DELETE FROM file_trash WHERE id = ?", id)
		touchDiskUsage(db, c.AccountID)
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	r.Post("/trash/empty", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		home := getHomeDir(r)
		if home == "" {
			jsonError(w, 403, "no home"); return
		}
		rows, qErr := db.Query("SELECT id, trash_path FROM file_trash WHERE account_id = ?", c.AccountID)
		if qErr != nil {
			jsonError(w, 500, "failed to query trash"); return
		}
		defer rows.Close()
		for rows.Next() {
			var id int
			var tp string
			if err := rows.Scan(&id, &tp); err != nil {
				continue
			}
			if !isPathWithin(home, tp) {
				continue
			}
			os.RemoveAll(tp)
		}
		db.Exec("DELETE FROM file_trash WHERE account_id = ?", c.AccountID)
		touchDiskUsage(db, c.AccountID)
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
			safeFileError(w, r, 403, err)
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

	// --- File stat (detailed info) ---
	r.Get("/stat", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" { jsonError(w, 403, "no home"); return }
		filePath := r.URL.Query().Get("path")
		if filePath == "" { jsonError(w, 400, "path required"); return }
		safe, err := filesystem.SafePath(home, filePath)
		if err != nil { safeFileError(w, r, 403, err); return }
		info, err := os.Lstat(safe)
		if err != nil { jsonError(w, 404, "not found"); return }
		owner, group := getOwnerGroupFromInfo(info)
		jsonResp(w, 200, map[string]interface{}{
			"name":       filepath.Base(safe),
			"size":       info.Size(),
			"mod_time":   info.ModTime().Format(time.RFC3339),
			"perm":       info.Mode().String(),
			"perm_octal": fmt.Sprintf("%o", info.Mode().Perm()),
			"owner":      owner,
			"group":      group,
			"is_dir":     info.IsDir(),
		})
	})

	// --- Permission management (chmod) ---
	r.Post("/chmod", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" { jsonError(w, 403, "no home"); return }
		var req struct {
			Path string `json:"path"`
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil { jsonError(w, 400, "invalid body"); return }
		if req.Path == "" || req.Mode == "" { jsonError(w, 400, "path and mode required"); return }
		safe, err := filesystem.SafePath(home, req.Path)
		if err != nil { safeFileError(w, r, 403, err); return }
		rel, _ := filepath.Rel(home, safe)
		if strings.HasPrefix(rel, ".") || strings.HasPrefix(rel, "..") { jsonError(w, 403, "access denied"); return }
		mode64, err := strconv.ParseInt(req.Mode, 8, 32)
		if err != nil { jsonError(w, 400, "invalid mode format (use octal, e.g. 0644)"); return }
		if mode64 < 0 || mode64 > 07777 { jsonError(w, 400, "mode out of range"); return }
		if err := os.Chmod(safe, os.FileMode(mode64)); err != nil {
			safeFileError(w, r, 500, err)
			return
		}
		jsonResp(w, 200, map[string]string{"status": "ok", "mode": fmt.Sprintf("%04o", mode64)})
	})

	// --- Change owner/group ---
	r.Post("/owner", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" { jsonError(w, 403, "no home"); return }
		var req struct {
			Path  string `json:"path"`
			Owner string `json:"owner"`
			Group string `json:"group"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil { jsonError(w, 400, "invalid body"); return }
		if req.Path == "" || (req.Owner == "" && req.Group == "") { jsonError(w, 400, "path and owner/group required"); return }
		safe, err := filesystem.SafePath(home, req.Path)
		if err != nil { safeFileError(w, r, 403, err); return }
		rel, _ := filepath.Rel(home, safe)
		if strings.HasPrefix(rel, ".") || strings.HasPrefix(rel, "..") { jsonError(w, 403, "access denied"); return }
		uid := -1
		gid := -1
		if req.Owner != "" {
			u, err := user.Lookup(req.Owner)
			if err != nil { jsonError(w, 400, "unknown owner: "+req.Owner); return }
			uid, _ = strconv.Atoi(u.Uid)
		}
		if req.Group != "" {
			g, err := user.LookupGroup(req.Group)
			if err != nil { jsonError(w, 400, "unknown group: "+req.Group); return }
			gid, _ = strconv.Atoi(g.Gid)
		}
		if err := os.Chown(safe, uid, gid); err != nil {
			safeFileError(w, r, 500, err)
			return
		}
		jsonResp(w, 200, map[string]string{"status": "ok"})
	})

	// --- File search ---
	r.Get("/search", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" { jsonError(w, 403, "no home"); return }
		query := strings.TrimSpace(r.URL.Query().Get("query"))
		if query == "" { jsonError(w, 400, "query required"); return }
		searchType := r.URL.Query().Get("type")
		if searchType == "" { searchType = "filename" }
		recursive := r.URL.Query().Get("recursive") != "false"
		startPath := r.URL.Query().Get("path")
		if startPath == "" { startPath = "/" }
		rootPath, err := filesystem.SafePath(home, startPath)
		if err != nil { safeFileError(w, r, 403, err); return }

		type SearchResult struct {
			Name     string `json:"name"`
			Path     string `json:"path"`
			Type     string `json:"type"`
			Size     int64  `json:"size"`
			ModTime  string `json:"mod_time"`
			Match    string `json:"match,omitempty"`
			Line     int    `json:"line,omitempty"`
		}
		results := make([]SearchResult, 0)
		qLower := strings.ToLower(query)
		var mu sync.Mutex
		walkFn := func(path string, d os.DirEntry, err error) error {
			if err != nil { return nil }
			relPath, _ := filepath.Rel(rootPath, path)
			if relPath == "." { return nil }
			if strings.HasPrefix(d.Name(), ".") { return nil }
			if !recursive && strings.Count(relPath, string(filepath.Separator)) > 0 { return nil }

			info, err := d.Info()
			if err != nil { return nil }

			// Skip symlinks: they may point outside the account home (escape).
			if info.Mode()&os.ModeSymlink != 0 { return nil }

			if searchType == "filename" || searchType == "both" {
				if strings.Contains(strings.ToLower(d.Name()), qLower) {
					relFull, _ := filepath.Rel(home, path)
					mu.Lock()
					results = append(results, SearchResult{
						Name: d.Name(), Path: "/" + filepath.ToSlash(relFull),
						Type: func() string { if d.IsDir() { return "dir" }; return "file" }(),
						Size: info.Size(), ModTime: info.ModTime().Format(time.RFC3339),
					})
					mu.Unlock()
					if len(results) >= 500 { return filepath.SkipAll }
				}
			}
			if (searchType == "content" || searchType == "both") && !d.IsDir() {
				if info.Size() > 5*1024*1024 { return nil }
				data, err := os.ReadFile(path)
				if err != nil { return nil }
				if bytes.Contains(bytes.ToLower(data), []byte(qLower)) {
					relFull, _ := filepath.Rel(home, path)
					lines := strings.Split(string(data), "\n")
					for i, line := range lines {
						if strings.Contains(strings.ToLower(line), qLower) {
							mu.Lock()
							results = append(results, SearchResult{
								Name: d.Name(), Path: "/" + filepath.ToSlash(relFull),
								Type: "file", Size: info.Size(),
								ModTime: info.ModTime().Format(time.RFC3339),
								Match:   strings.TrimSpace(line), Line: i + 1,
							})
							mu.Unlock()
							if len(results) >= 200 { break }
						}
					}
					if len(results) >= 500 { return filepath.SkipAll }
				}
			}
			return nil
		}

		if recursive {
			filepath.WalkDir(rootPath, walkFn)
		} else {
			entries, err := os.ReadDir(rootPath)
			if err == nil {
				for _, e := range entries {
					walkFn(filepath.Join(rootPath, e.Name()), e, nil)
				}
			}
		}
		jsonResp(w, 200, map[string]interface{}{"results": results, "total": len(results)})
	})


	// --- Image manipulation ---
	r.Post("/image/resize", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" { jsonError(w, 403, "no home"); return }
		var req struct {
			Path   string `json:"path"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil { jsonError(w, 400, "invalid body"); return }
		if req.Path == "" || req.Width <= 0 || req.Height <= 0 { jsonError(w, 400, "path, width, height required"); return }
		safe, err := filesystem.SafePath(home, req.Path)
		if err != nil { safeFileError(w, r, 403, err); return }
		if err := processImage(safe, func(img image.Image) (image.Image, string, error) {
			return resizeImage(img, req.Width, req.Height), filepath.Ext(safe), nil
		}); err != nil { safeFileError(w, r, 500, err); return }
		jsonResp(w, 200, map[string]string{"status": "resized", "width": strconv.Itoa(req.Width), "height": strconv.Itoa(req.Height)})
	})
	r.Post("/image/rotate", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" { jsonError(w, 403, "no home"); return }
		var req struct {
			Path    string `json:"path"`
			Degrees int    `json:"degrees"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil { jsonError(w, 400, "invalid body"); return }
		if req.Path == "" { jsonError(w, 400, "path required"); return }
		if req.Degrees%90 != 0 { jsonError(w, 400, "degrees must be a multiple of 90"); return }
		safe, err := filesystem.SafePath(home, req.Path)
		if err != nil { safeFileError(w, r, 403, err); return }
		if err := processImage(safe, func(img image.Image) (image.Image, string, error) {
			return rotateImage(img, req.Degrees), filepath.Ext(safe), nil
		}); err != nil { safeFileError(w, r, 500, err); return }
		jsonResp(w, 200, map[string]string{"status": "rotated", "degrees": strconv.Itoa(req.Degrees)})
	})
	r.Post("/image/crop", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" { jsonError(w, 403, "no home"); return }
		var req struct {
			Path   string `json:"path"`
			X      int    `json:"x"`
			Y      int    `json:"y"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil { jsonError(w, 400, "invalid body"); return }
		if req.Path == "" || req.Width <= 0 || req.Height <= 0 { jsonError(w, 400, "path, width, height required"); return }
		safe, err := filesystem.SafePath(home, req.Path)
		if err != nil { safeFileError(w, r, 403, err); return }
		if err := processImage(safe, func(img image.Image) (image.Image, string, error) {
			return cropImage(img, req.X, req.Y, req.Width, req.Height), filepath.Ext(safe), nil
		}); err != nil { safeFileError(w, r, 500, err); return }
		jsonResp(w, 200, map[string]string{"status": "cropped"})
	})

	// --- Download multiple files as ZIP ---
	r.Post("/download-multiple", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" { jsonError(w, 403, "no home"); return }
		var req struct{ Paths []string `json:"paths"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil { jsonError(w, 400, "invalid body"); return }
		if len(req.Paths) == 0 { jsonError(w, 400, "no files specified"); return }

		pr, pw := io.Pipe()
		zw := zip.NewWriter(pw)
		errCh := make(chan error, 1)
		go func() {
			defer pw.Close()
			for _, p := range req.Paths {
				safe, err := filesystem.SafePath(home, p)
				if err != nil { continue }
				rel, _ := filepath.Rel(home, safe)
				info, err := os.Lstat(safe)
				if err != nil { continue }
				if info.IsDir() {
					addDirToZip(zw, safe, filepath.ToSlash(rel))
				} else {
					addFileToZip(zw, safe, filepath.ToSlash(rel))
				}
			}
			errCh <- zw.Close()
		}()
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=\"download.zip\"")
		io.Copy(w, pr)
		<-errCh
	})
}

func getOwnerGroupFromInfo(info os.FileInfo) (string, string) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", ""
	}
	owner := strconv.Itoa(int(stat.Uid))
	group := strconv.Itoa(int(stat.Gid))
	if u, err := user.LookupId(owner); err == nil {
		owner = u.Username
	}
	if g, err := user.LookupGroupId(group); err == nil {
		group = g.Name
	}
	return owner, group
}

func getFileOwnerGroup(path string) (string, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", "", err
	}
	owner, group := getOwnerGroupFromInfo(info)
	return owner, group, nil
}

// --- Image manipulation helpers ---

func processImage(path string, fn func(image.Image) (image.Image, string, error)) error {
	info, err := os.Lstat(path)
	if err != nil { return fmt.Errorf("file not found: %w", err) }
	if info.IsDir() { return fmt.Errorf("not an image file") }
	if info.Size() > 50*1024*1024 { return fmt.Errorf("file too large (max 50MB)") }

	ext := strings.ToLower(filepath.Ext(path))
	var src image.Image
	switch ext {
	case ".jpg", ".jpeg":
		f, err := os.Open(path)
		if err != nil { return err }
		defer f.Close()
		src, err = jpeg.Decode(f)
		if err != nil { return fmt.Errorf("invalid JPEG: %w", err) }
	case ".png":
		f, err := os.Open(path)
		if err != nil { return err }
		defer f.Close()
		src, err = png.Decode(f)
		if err != nil { return fmt.Errorf("invalid PNG: %w", err) }
	default:
		return fmt.Errorf("unsupported image format: %s (only JPEG and PNG supported)", ext)
	}

	dst, outExt, err := fn(src)
	if err != nil { return err }
	if outExt == "" { outExt = ext }

	backupPath := path + ".bak"
	if err := os.Rename(path, backupPath); err != nil { return fmt.Errorf("backup failed: %w", err) }

	writeErr := writeImage(path, dst, outExt)
	if writeErr != nil {
		os.Rename(backupPath, path)
		return writeErr
	}
	os.Remove(backupPath)
	return nil
}

func writeImage(path string, img image.Image, ext string) error {
	f, err := os.Create(path)
	if err != nil { return err }
	defer f.Close()
	switch ext {
	case ".jpg", ".jpeg":
		return jpeg.Encode(f, img, &jpeg.Options{Quality: 90})
	case ".png":
		return png.Encode(f, img)
	}
	return png.Encode(f, img)
}

func resizeImage(img image.Image, width, height int) image.Image {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW == 0 || srcH == 0 { return img }
	
	// Maintain aspect ratio if only one dimension is provided
	if width <= 0 && height > 0 {
		width = srcW * height / srcH
	}
	if height <= 0 && width > 0 {
		height = srcH * width / srcW
	}
	if width <= 0 { width = srcW }
	if height <= 0 { height = srcH }

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			srcX := float64(x) * float64(srcW) / float64(width)
			srcY := float64(y) * float64(srcH) / float64(height)
			// Bilinear interpolation
			x0, y0 := int(math.Floor(srcX)), int(math.Floor(srcY))
			x1, y1 := x0+1, y0+1
			if x1 >= srcW { x1 = srcW - 1 }
			if y1 >= srcH { y1 = srcH - 1 }
			xf := srcX - float64(x0)
			yf := srcY - float64(y0)
			c00 := img.At(x0, y0)
			c10 := img.At(x1, y0)
			c01 := img.At(x0, y1)
			c11 := img.At(x1, y1)
			r00, g00, b00, a00 := c00.RGBA()
			r10, g10, b10, a10 := c10.RGBA()
			r01, g01, b01, a01 := c01.RGBA()
			r11, g11, b11, a11 := c11.RGBA()
			r := uint8(((1-xf)*(1-yf)*float64(r00) + xf*(1-yf)*float64(r10) + (1-xf)*yf*float64(r01) + xf*yf*float64(r11)) / 256.0 / 65535.0 * 255.0)
			g := uint8(((1-xf)*(1-yf)*float64(g00) + xf*(1-yf)*float64(g10) + (1-xf)*yf*float64(g01) + xf*yf*float64(g11)) / 256.0 / 65535.0 * 255.0)
			b := uint8(((1-xf)*(1-yf)*float64(b00) + xf*(1-yf)*float64(b10) + (1-xf)*yf*float64(b01) + xf*yf*float64(b11)) / 256.0 / 65535.0 * 255.0)
			a := uint8(((1-xf)*(1-yf)*float64(a00) + xf*(1-yf)*float64(a10) + (1-xf)*yf*float64(a01) + xf*yf*float64(a11)) / 256.0 / 65535.0 * 255.0)
			dst.Set(x, y, color.RGBA{R: r, G: g, B: b, A: a})
		}
	}
	return dst
}

func rotateImage(img image.Image, degrees int) image.Image {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	switch ((degrees % 360) + 360) % 360 {
	case 90:
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(h-1-y, x, img.At(x, y))
			}
		}
		return dst
	case 180:
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(w-1-x, h-1-y, img.At(x, y))
			}
		}
		return dst
	case 270:
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(y, w-1-x, img.At(x, y))
			}
		}
		return dst
	default:
		return img
	}
}

func cropImage(img image.Image, x, y, w, h int) image.Image {
	bounds := img.Bounds()
	if x < 0 { x = 0 }
	if y < 0 { y = 0 }
	if x+w > bounds.Dx() { w = bounds.Dx() - x }
	if y+h > bounds.Dy() { h = bounds.Dy() - y }
	if w <= 0 || h <= 0 { return img }
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			dst.Set(dx, dy, img.At(x+dx, y+dy))
		}
	}
	return dst
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
		if strings.HasPrefix(e.Name(), ".") {
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

// getAccountOverride reads a per-account override from server_config. Returns
// fallback when no override (or a <= 0 value) is stored.
func getAccountOverride(db *sql.DB, accountID int, key string, fallback int) int {
	var v int
	if err := db.QueryRow("SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = ?",
		fmt.Sprintf("%s_%d", key, accountID)).Scan(&v); err != nil || v <= 0 {
		return fallback
	}
	return v
}

// getSSHAccess reports whether the account may use SSH. Per-account override
// semantics: -1 (or unset) = follow the package, 0 = force off, 1 = force on.
func getSSHAccess(db *sql.DB, accountID int, pkgDefault int) int {
	var v int
	if err := db.QueryRow("SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = ?",
		fmt.Sprintf("ssh_access_%d", accountID)).Scan(&v); err != nil || v == -1 {
		return pkgDefault
	}
	return v
}

// ---------- Per-feature access gates (V1: files, emails, ftp, db, backups, cron) ----------
//
// Package columns hold the default (1 = allowed, 0 = blocked); per-account
// overrides live in server_config as feature_<name>_<id> with -1 (or unset) =
// follow the package, 0 = force blocked, 1 = force allowed. Backups reuses the
// existing backup_enabled package column. Unknown feature names are allowed
// (fail-open) so a typo can never lock every user out.
var featurePackageColumns = map[string]string{
	"files":   "files_enabled",
	"emails":  "emails_enabled",
	"ftp":     "ftp_enabled",
	"db":      "db_enabled",
	"backups": "backup_enabled",
	"cron":    "cron_enabled",
}

// getFeatureOverride returns the per-account override for a feature:
// -1 = inherit package default (or unset/invalid), 0 = force blocked, 1 = force allowed.
func getFeatureOverride(db *sql.DB, accountID int, feature string) int {
	var v int
	if err := db.QueryRow("SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = ?",
		fmt.Sprintf("feature_%s_%d", feature, accountID)).Scan(&v); err != nil {
		return -1
	}
	if v != 0 && v != 1 {
		return -1
	}
	return v
}

// featurePackageDefault reads the package default for a feature (1 = allowed).
func featurePackageDefault(db *sql.DB, accountID int, feature string) int {
	col, ok := featurePackageColumns[feature]
	if !ok {
		return 1
	}
	var v int
	// Column may not exist on DBs created before the migration; treat a query
	// error as allowed so upgrades never lock users out.
	if err := db.QueryRow(fmt.Sprintf(`SELECT COALESCE(p.%s, 1) FROM accounts a
		JOIN packages p ON a.package_id = p.id WHERE a.id = ?`, col), accountID).Scan(&v); err != nil {
		return 1
	}
	return v
}

// featureEnabledFor reports whether the account may use a feature.
func featureEnabledFor(db *sql.DB, accountID int, feature string) bool {
	if ov := getFeatureOverride(db, accountID, feature); ov != -1 {
		return ov == 1
	}
	return featurePackageDefault(db, accountID, feature) == 1
}

// requireActiveAccount rejects child requests from non-active accounts. This
// closes the suspend bypass where a live JWT kept working after suspension.
func requireActiveAccount(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil {
				jsonError(w, 401, "unauthorized")
				return
			}
			var status string
			if err := db.QueryRow("SELECT status FROM accounts WHERE id = ?", c.AccountID).Scan(&status); err != nil {
				jsonError(w, 403, "account not found or inactive")
				return
			}
			if status != "active" {
				jsonError(w, 403, fmt.Sprintf("account is %s", status))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireFeature rejects child requests when the feature is disabled for the
// account. Unknown features fail open (see featurePackageColumns).
func requireFeature(db *sql.DB, feature string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil {
				jsonError(w, 401, "unauthorized")
				return
			}
			if !featureEnabledFor(db, c.AccountID, feature) {
				jsonError(w, 403, fmt.Sprintf("%s access is disabled for your account", feature))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireEmailsReadOnly enforces email read-only mode: reads (GET) and flag
// updates (PATCH, e.g. mark read/starred) stay allowed, while everything that
// creates, changes or sends mail is blocked when emails are disabled.
func requireEmailsReadOnly(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodPatch {
				next.ServeHTTP(w, r)
				return
			}
			requireFeature(db, "emails")(next).ServeHTTP(w, r)
		})
	}
}

func getEffectiveCPULimit(db *sql.DB, accountID int) float64 {
	// Per-account CPU override wins when set (stored as float cores)
	var override float64
	if err := db.QueryRow("SELECT CAST(value AS REAL) FROM server_config WHERE key_name = ?",
		fmt.Sprintf("cpu_limit_%d", accountID)).Scan(&override); err == nil && override > 0 {
		return override
	}
	var pkgRAM int
	db.QueryRow(`SELECT COALESCE(p.ram_limit_mb, 0) FROM accounts a
		JOIN packages p ON a.package_id = p.id WHERE a.id = ?`, accountID).Scan(&pkgRAM)
	// Derive CPU limit proportionally: 1 CPU core per 1024MB RAM, min 0.5
	if pkgRAM <= 0 {
		return 0
	}
	cpu := float64(pkgRAM) / 1024.0
	if cpu < 0.5 {
		return 0.5
	}
	if cpu > 8 {
		return 8
	}
	return cpu
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
				var containerName string
				db.QueryRow("SELECT container_name FROM docker_containers WHERE account_id = ? AND status = 'running'", id).Scan(&containerName)
				if containerName != "" {
					if info, err := docker.GetContainerInfo(containerName); err == nil {
						mb += info.RAMUsageMB
					}
				}
				if _, err := db.Exec("UPDATE accounts SET ram_used_mb = ? WHERE id = ?", mb, id); err != nil {
					log.Printf("[RAM] Failed to update ram_used_mb for account %d (%s): %v", id, username, err)
				}
			}
		}
	}()
}

const resourceSafetyBufferMB = 50

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

func resourceLimitWarningHandler(db *sql.DB, accountID int) string {
	var diskUsed, diskLimit, ramUsed, ramLimit int
	var bwUsed, bwLimit float64
	db.QueryRow(`SELECT COALESCE(a.disk_used_mb, 0), COALESCE(p.disk_mb, 0),
		COALESCE((SELECT CAST(SUM(bytes_out + bytes_in) AS REAL)/1048576.0 FROM bandwidth_logs WHERE account_id = a.id), 0), COALESCE(p.bandwidth_mb, 0),
		COALESCE(a.ram_used_mb, 0), CASE WHEN a.ram_limit_mb > 0 THEN a.ram_limit_mb ELSE COALESCE(p.ram_limit_mb, 0) END
		FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`, accountID).Scan(
		&diskUsed, &diskLimit, &bwUsed, &bwLimit, &ramUsed, &ramLimit)
	// Honor per-account overrides set by the admin (0 = package default)
	diskLimit = getAccountOverride(db, accountID, "disk_limit", diskLimit)
	bwLimit = float64(getAccountOverride(db, accountID, "bandwidth_limit", int(bwLimit)))

	var warnings []string
	if diskLimit > 0 && diskUsed >= (diskLimit*95/100) {
		warnings = append(warnings, "You have nearly reached your disk limit. Please upgrade your plan or contact support.")
	}
	if bwLimit > 0 && bwUsed >= (bwLimit*95/100) {
		warnings = append(warnings, "You have nearly reached your bandwidth limit. Please upgrade your plan or contact support.")
	}
	if ramLimit > 0 && ramUsed >= (ramLimit*95/100) {
		warnings = append(warnings, "You have nearly reached your RAM limit. Please upgrade your plan or contact support.")
	}

	if len(warnings) > 0 {
		return strings.Join(warnings, " ")
	}
	return ""
}

// touchDiskUsage recomputes one account's disk usage shortly after a file
// mutation so the UI reflects uploads/deletes within seconds instead of at
// the next 5-minute tracker tick. Runs async and never blocks the request;
// a du success is authoritative (writes even 0), a du failure changes nothing.
func touchDiskUsage(db *sql.DB, accountID int) {
	go func() {
		time.Sleep(2 * time.Second)
		var homeDir string
		if err := db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", accountID).Scan(&homeDir); err != nil || homeDir == "" {
			return
		}
		var mb int
		var containerName string
		db.QueryRow("SELECT container_name FROM docker_containers WHERE account_id = ? AND status = 'running'", accountID).Scan(&containerName)
		if containerName != "" {
			if info, err := docker.GetContainerInfo(containerName); err == nil && info.DiskUsageMB > 0 {
				mb = int(info.DiskUsageMB)
			}
		}
		if mb <= 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			out, err := exec.CommandContext(ctx, "du", "-sk", homeDir).Output()
			cancel()
			if err != nil {
				return
			}
			fields := strings.Fields(string(out))
			if len(fields) == 0 {
				return
			}
			kb, err := strconv.Atoi(fields[0])
			if err != nil {
				return
			}
			mb = (kb + 512) / 1024
		}
		db.Exec("UPDATE accounts SET disk_used_mb = ? WHERE id = ?", mb, accountID)
	}()
}

func startDiskTracker(db *sql.DB) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			rows, err := db.Query("SELECT id, home_dir FROM accounts WHERE status = 'active'")
			if err != nil {
				log.Printf("[DISK] Failed to query accounts: %v", err)
				continue
			}
			for rows.Next() {
				var id int
				var homeDir string
				if err := rows.Scan(&id, &homeDir); err != nil {
					continue
				}
				var mb int
				var containerName string
				db.QueryRow("SELECT container_name FROM docker_containers WHERE account_id = ? AND status = 'running'", id).Scan(&containerName)
				if containerName != "" {
					if info, err := docker.GetContainerInfo(containerName); err == nil && info.DiskUsageMB > 0 {
						mb = int(info.DiskUsageMB)
					}
				}
	if mb <= 0 && homeDir != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				out, err := exec.CommandContext(ctx, "du", "-sk", homeDir).Output()
				cancel()
				if err == nil {
					fields := strings.Fields(string(out))
					if len(fields) > 0 {
						if kb, err := strconv.Atoi(fields[0]); err == nil {
							mb = (kb + 512) / 1024
						}
					}
				}
			}
				if mb > 0 {
					db.Exec("UPDATE accounts SET disk_used_mb = ? WHERE id = ?", mb, id)
				}
			}
			rows.Close()
		}
	}()
}

func suspendIfLimitExceeded(db *sql.DB, id int, username string) {
	var diskUsed, diskLimit, ramUsed, ramLimit int
	var bwUsed, bwLimit float64
	row := db.QueryRow(`SELECT COALESCE(a.disk_used_mb, 0), COALESCE(p.disk_mb, 0),
		COALESCE((SELECT CAST(SUM(bytes_out + bytes_in) AS REAL)/1048576.0 FROM bandwidth_logs WHERE account_id = a.id), 0), COALESCE(p.bandwidth_mb, 0),
		COALESCE(a.ram_used_mb, 0), CASE WHEN a.ram_limit_mb > 0 THEN a.ram_limit_mb ELSE COALESCE(p.ram_limit_mb, 0) END
		FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`, id)
	if err := row.Scan(&diskUsed, &diskLimit, &bwUsed, &bwLimit, &ramUsed, &ramLimit); err != nil {
		log.Printf("[SUSPEND] Query error for account %d (%s): %v", id, username, err)
		return
	}

	bufferLimit := func(limit int) int {
		b := resourceSafetyBufferMB
		if b > limit/2 {
			b = limit / 2
		}
		if b < 0 {
			b = 0
		}
		return b
	}
	reasons := []string{}
	if diskLimit > 0 && diskUsed >= diskLimit-bufferLimit(diskLimit) {
		reasons = append(reasons, fmt.Sprintf("Disk (%d MB) exceeded limit (%d MB)", diskUsed, diskLimit))
	}
	if bwLimit > 0 && bwUsed >= bwLimit-float64(bufferLimit(int(bwLimit))) {
		reasons = append(reasons, fmt.Sprintf("Bandwidth (%.2f MB) exceeded limit (%.0f MB)", bwUsed, bwLimit))
	}
	if ramLimit > 0 && ramUsed >= ramLimit-bufferLimit(ramLimit) {
		reasons = append(reasons, fmt.Sprintf("RAM (%d MB) exceeded limit (%d MB)", ramUsed, ramLimit))
	}

	if len(reasons) > 0 {
		reason := "Auto-suspended: " + strings.Join(reasons, "; ")
		log.Printf("[SUSPEND] Suspending account %s (ID: %d): %s", username, id, reason)
		db.Exec("UPDATE accounts SET status='suspended', suspended_reason=?, suspended_at=datetime('now'), updated_at=datetime('now') WHERE id=?", reason, id)
		var containerName string
		db.QueryRow("SELECT container_name FROM docker_containers WHERE account_id = ?", id).Scan(&containerName)
		if containerName != "" {
			if err := docker.StopContainer(containerName); err != nil {
				log.Printf("[SUSPEND] Failed to stop container %s: %v", containerName, err)
			} else {
				db.Exec("UPDATE docker_containers SET status = 'stopped', updated_at = datetime('now') WHERE account_id = ?", id)
			}
		}
	}
}

// suspendAutoRemoveDays reads the grace period (days) after which suspended
// accounts are permanently purged. <=0 disables auto-purge.
func suspendAutoRemoveDays(db *sql.DB) int {
	var v int
	if err := db.QueryRow("SELECT CAST(value AS INTEGER) FROM server_config WHERE key_name = 'suspend_auto_remove_days'").Scan(&v); err != nil {
		return 7
	}
	return v
}

// startSuspendedPurger permanently deletes suspended accounts past their grace
// period (hourly). Uses the same purge routine as the manual Delete button.
func startSuspendedPurger(db *sql.DB) {
	go func() {
		time.Sleep(60 * time.Second)
		ticker := time.NewTicker(60 * time.Minute)
		for range ticker.C {
			days := suspendAutoRemoveDays(db)
			if days <= 0 {
				continue
			}
			// Backfill: accounts suspended before suspended_at existed inherit
			// their last update time so they purge on schedule too.
			db.Exec(`UPDATE accounts SET suspended_at = updated_at
				WHERE status = 'suspended' AND COALESCE(suspended_at, '') = ''`)
			rows, err := db.Query(`SELECT id, username FROM accounts WHERE status = 'suspended'
				AND COALESCE(suspended_at, '') != ''
				AND datetime(suspended_at, '+' || ? || ' days') <= datetime('now')`, days)
			if err != nil {
				log.Printf("[PURGE] auto-purge query failed: %v", err)
				continue
			}
			var ids []int
			var names []string
			for rows.Next() {
				var id int
				var username string
				if rows.Scan(&id, &username) == nil {
					ids = append(ids, id)
					names = append(names, username)
				}
			}
			rows.Close()
			for i, id := range ids {
				log.Printf("[PURGE] auto-purging suspended account %s (ID: %d, grace %dd exceeded)", names[i], id, days)
				if _, err := purgeAccountByID(db, nil, id); err != nil {
					log.Printf("[PURGE] auto-purge failed for account %d: %v", id, err)
				}
			}
		}
	}()
}

func startAutoSuspender(db *sql.DB) {
	go func() {
		time.Sleep(30 * time.Second)
		ticker := time.NewTicker(60 * time.Second)
		for range ticker.C {
			type act struct {
				id   int
				name string
			}
			var accounts []act
			rows, err := db.Query("SELECT id, username FROM accounts WHERE status = 'active'")
			if err != nil {
				log.Printf("[SUSPEND] Failed to query active accounts: %v", err)
				continue
			}
			for rows.Next() {
				var id int
				var username string
				if err := rows.Scan(&id, &username); err != nil {
					continue
				}
				accounts = append(accounts, act{id, username})
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				log.Printf("[SUSPEND] Rows iteration error: %v", err)
			}

			for _, a := range accounts {
				suspendIfLimitExceeded(db, a.id, a.name)
			}
		}
	}()
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
	if _, err := io.Copy(w, f); err != nil {
		log.Printf("addFileToZip: failed to copy %s: %v", filePath, err)
	}
}

func addDirToZip(zw *zip.Writer, dirPath, baseName string) {
	filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			log.Printf("addDirToZip: walk error at %s: %v", path, err)
			return nil
		}
		// Skip symlinks: they may point outside the account home (escape).
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, relErr := filepath.Rel(dirPath, path)
		if relErr != nil {
			log.Printf("addDirToZip: rel error for %s: %v", path, relErr)
			return nil
		}
		if d.IsDir() {
			if rel != "." {
				if _, err := zw.Create(baseName + "/" + rel + "/"); err != nil {
					log.Printf("addDirToZip: failed to create dir entry %s: %v", rel, err)
				}
			}
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			log.Printf("addDirToZip: failed to read %s: %v", path, readErr)
			return nil
		}
		w, createErr := zw.Create(baseName + "/" + rel)
		if createErr != nil {
			log.Printf("addDirToZip: failed to create entry %s: %v", rel, createErr)
			return nil
		}
		if _, writeErr := w.Write(data); writeErr != nil {
			log.Printf("addDirToZip: failed to write %s: %v", rel, writeErr)
		}
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
		if !validMysqlIdent(req.DBName) {
			jsonError(w, 400, "invalid database name (letters, numbers and underscores only)")
			return
		}

		// Enforce max_db limit from the hosting package (or per-account override)
		var maxDB int
		err := db.QueryRow(`SELECT p.max_db FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`,
			c.AccountID).Scan(&maxDB)
		if err != nil {
			jsonError(w, 500, "failed to load package limits")
			return
		}
		maxDB = getAccountOverride(db, c.AccountID, "max_db", maxDB)

		// Count + insert must be one transaction so concurrent create requests
		// can't exceed max_db.
		tx, err := db.Begin()
		if err != nil {
			jsonError(w, 500, "failed to load package limits")
			return
		}
		var currentDB int
		tx.QueryRow("SELECT COUNT(*) FROM child_databases WHERE account_id = ?", c.AccountID).Scan(&currentDB)
		if currentDB >= maxDB {
			tx.Rollback()
			jsonError(w, 403, fmt.Sprintf("database limit reached (%d/%d)", currentDB, maxDB))
			return
		}

		prefixedName := c.Username + "_" + req.DBName
		result, err := tx.Exec(`INSERT INTO child_databases (account_id, db_name, db_user) VALUES (?, ?, ?)`,
			c.AccountID, prefixedName, c.Username+"_u")
		if err != nil {
			tx.Rollback()
			jsonError(w, 500, err.Error())
			return
		}
		if err := tx.Commit(); err != nil {
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
			if !validMysqlIdent(req.Username) {
				jsonError(w, 400, "invalid username (letters, numbers and underscores only)")
				return
			}
			if len(req.Password) < 8 {
				jsonError(w, 400, "password must be at least 8 characters")
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
			if c == nil {
				jsonError(w, 401, "unauthorized")
				return
			}
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
			var ownerCheck int
			err := db.QueryRow("SELECT 1 FROM db_users WHERE id = ? AND account_id = ?", uid, c.AccountID).Scan(&ownerCheck)
			if err != nil {
				jsonError(w, 404, "database user not found")
				return
			}
			err = db.QueryRow("SELECT 1 FROM child_databases WHERE id = ? AND account_id = ?", req.DBID, c.AccountID).Scan(&ownerCheck)
			if err != nil {
				jsonError(w, 404, "database not found")
				return
			}
			if req.Privileges == "" {
				req.Privileges = "ALL PRIVILEGES"
			}
			if _, err := db.Exec(`INSERT OR REPLACE INTO db_user_assignments (user_id, db_id, privileges) VALUES (?, ?, ?)`,
				uid, req.DBID, req.Privileges); err != nil {
				jsonError(w, 500, err.Error())
				return
			}
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
			var ownerCheck int
			err := db.QueryRow("SELECT 1 FROM db_users WHERE id = ? AND account_id = ?", uid, c.AccountID).Scan(&ownerCheck)
			if err != nil {
				jsonError(w, 404, "database user not found")
				return
			}
			err = db.QueryRow("SELECT 1 FROM child_databases WHERE id = ? AND account_id = ?", req.DBID, c.AccountID).Scan(&ownerCheck)
			if err != nil {
				jsonError(w, 404, "database not found")
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
			var ownerCheck int
			if err := db.QueryRow("SELECT 1 FROM db_users WHERE id = ? AND account_id = ?", id, c.AccountID).Scan(&ownerCheck); err != nil {
				jsonError(w, 404, "database user not found")
				return
			}
			if _, err := db.Exec("DELETE FROM db_user_assignments WHERE user_id IN (SELECT id FROM db_users WHERE id = ? AND account_id = ?)", id, c.AccountID); err != nil {
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
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct {
			RemoteAccess string `json:"remote_access"`
			Enabled      bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		host := "localhost"
		if req.Enabled || req.RemoteAccess == "%" {
			host = "%"
		}
		var dbName, dbUser string
		if err := db.QueryRow("SELECT db_name, db_user FROM child_databases WHERE id = ? AND account_id = ?", id, c.AccountID).Scan(&dbName, &dbUser); err != nil {
			jsonError(w, 404, "database not found")
			return
		}
		var dbPassword string
		db.QueryRow("SELECT COALESCE(password,'') FROM db_users WHERE username = ? AND account_id = ?", dbUser, c.AccountID).Scan(&dbPassword)

		mysqlRootPass := os.Getenv("MYSQL_ROOT_PASSWORD")
		if mysqlRootPass == "" {
			jsonError(w, 500, "remote access unavailable: MySQL is not configured")
			return
		}
		if !validMysqlIdent(dbName) || !validMysqlIdent(dbUser) {
			jsonError(w, 500, "invalid database identifiers")
			return
		}
		dbPassword = mysqlEscape(dbPassword)
		var sql string
		if host == "%" {
			sql = fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY '%s'; GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%%'; FLUSH PRIVILEGES;", dbUser, dbPassword, dbName, dbUser)
		} else {
			sql = fmt.Sprintf("REVOKE ALL PRIVILEGES ON `%s`.* FROM '%s'@'%%'; DROP USER IF EXISTS '%s'@'%%'; FLUSH PRIVILEGES;", dbName, dbUser, dbUser)
		}
		// Run mysql via MYSQL_PWD so the root password never appears in the
		// process list, which is readable by any local user on a shared host.
		mysqlCmd := exec.Command("mysql", "-u", "root", "-h", "127.0.0.1", "-e", sql)
		mysqlCmd.Env = append(os.Environ(), "MYSQL_PWD="+mysqlRootPass)
		if out, err := mysqlCmd.CombinedOutput(); err != nil {
			log.Printf("[REMOTE] mysql error: %v, output: %s", err, string(out))
			jsonError(w, 500, "failed to update remote access on MySQL")
			return
		}
		result, err := db.Exec("UPDATE child_databases SET remote_access = ? WHERE id = ? AND account_id = ?", host, id, c.AccountID)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			jsonError(w, 404, "database not found")
			return
		}
		jsonResp(w, 200, map[string]string{"status": "updated", "remote_access": host})
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
		rows, err := db.Query(`SELECT d.id, d.domain, d.type, COALESCE(d.doc_root,''), COALESCE(d.ssl_enabled,0), COALESCE(d.force_https,0), d.created_at,
			COALESCE((
				SELECT
					CASE
						WHEN SUM(CASE WHEN s.status = 'issued' AND COALESCE(s.expires_at,'') > datetime('now') THEN 1 ELSE 0 END) > 0 THEN 'issued'
						WHEN SUM(CASE WHEN s.status = 'self-signed' THEN 1 ELSE 0 END) > 0 THEN 'self-signed'
						WHEN SUM(CASE WHEN s.status = 'issuing' THEN 1 ELSE 0 END) > 0 THEN 'issuing'
						WHEN SUM(CASE WHEN s.status = 'issued' THEN 1 ELSE 0 END) > 0 THEN 'expired'
						WHEN SUM(CASE WHEN s.status = 'failed' THEN 1 ELSE 0 END) > 0 THEN 'failed'
						ELSE 'none'
					END
				FROM ssl_certs s WHERE s.domain = d.domain AND s.account_id = d.account_id
			), 'none') AS ssl_status
			FROM domains d WHERE d.account_id = ? ORDER BY d.type, d.created_at DESC`, c.AccountID)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		domains := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id, ssl, force int
			var domain, typ, docRoot, created, certStatus string
			rows.Scan(&id, &domain, &typ, &docRoot, &ssl, &force, &created, &certStatus)
			domains = append(domains, map[string]interface{}{
				"id": id, "domain": domain, "type": typ,
				"doc_root": docRoot, "ssl_enabled": ssl == 1, "force_https": force == 1,
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
		if !validDomain(req.Domain) {
			jsonError(w, 400, "invalid domain name")
			return
		}
		if req.Type == "" {
			req.Type = "addon"
		}
		switch req.Type {
		case "primary", "addon", "parked", "subdomain":
		default:
			jsonError(w, 400, "invalid domain type")
			return
		}
		// Cross-account isolation: a domain may only be registered once, by its
		// owning active account. This prevents one tenant from hijacking or
		// overwriting another tenant's domain/vhost.
		var inUse int
		if err := db.QueryRow(`SELECT COUNT(*) FROM domains d JOIN accounts a ON a.id = d.account_id WHERE LOWER(d.domain) = LOWER(?) AND a.status = 'active'`,
			req.Domain).Scan(&inUse); err == nil && inUse > 0 {
			jsonError(w, 409, "this domain is already in use on this server")
			return
		}

		// Enforce limits from package (separate checks for domains vs subdomains).
		// The two count queries and the INSERT must be one transaction so
		// concurrent requests can't exceed max_domains / max_subdomains.
		var maxDomains, maxSubdomains, currentDomains, currentSubdomains int
		err := db.QueryRow(`SELECT p.max_domains, p.max_subdomains FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`,
			c.AccountID).Scan(&maxDomains, &maxSubdomains)
		if err != nil {
			jsonError(w, 500, "failed to load limits")
			return
		}
		maxDomains = getAccountOverride(db, c.AccountID, "max_domains", maxDomains)
		maxSubdomains = getAccountOverride(db, c.AccountID, "max_subdomains", maxSubdomains)
		tx, err := db.Begin()
		if err != nil {
			jsonError(w, 500, "failed to load limits")
			return
		}
		// Count non-subdomain domains (addon, parked, primary)
		tx.QueryRow("SELECT COUNT(*) FROM domains WHERE account_id = ? AND type != 'subdomain'", c.AccountID).Scan(&currentDomains)
		// Count subdomains separately
		tx.QueryRow("SELECT COUNT(*) FROM domains WHERE account_id = ? AND type = 'subdomain'", c.AccountID).Scan(&currentSubdomains)

		if req.Type == "subdomain" {
			if currentSubdomains >= maxSubdomains {
				tx.Rollback()
				jsonError(w, 403, fmt.Sprintf("subdomain limit reached (%d/%d)", currentSubdomains, maxSubdomains))
				return
			}
		} else {
			if currentDomains >= maxDomains {
				tx.Rollback()
				jsonError(w, 403, fmt.Sprintf("domain limit reached (%d/%d)", currentDomains, maxDomains))
				return
			}
		}

		homeDir := getHomeDir(c.AccountID)
		// Primary uses public_html; addon/subdomain get own dir at home level
		var docRoot string
		if req.Type == "primary" {
			docRoot = filepath.Join(homeDir, "public_html")
		} else {
			safeDomain := sanitizeDomain(req.Domain)
			cleanDomain := strings.TrimPrefix(safeDomain, "www.")
			docRoot = filepath.Join(homeDir, cleanDomain)
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

		result, err := tx.Exec(`INSERT INTO domains (account_id, domain, type, doc_root) VALUES (?, ?, ?, ?)`,
			c.AccountID, req.Domain, req.Type, docRoot)
		if err != nil {
			tx.Rollback()
			jsonError(w, 500, err.Error())
			return
		}
		if err := tx.Commit(); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		id, _ := result.LastInsertId()

		auditLog(db, r, "domain.create", map[string]interface{}{"domain": req.Domain, "type": req.Type, "account_id": c.AccountID})
		var containerIP string
		db.QueryRow("SELECT COALESCE(container_ip, '') FROM docker_containers WHERE account_id = ?", c.AccountID).Scan(&containerIP)
		if err := writeNginxVhost(sanitizeDomain(req.Domain), docRoot, containerIP, 0, ""); err != nil {
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

	r.Put("/{id}/https", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct {
			Force bool `json:"force"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		var dom string
		if err := db.QueryRow("SELECT domain FROM domains WHERE id = ? AND account_id = ?",
			id, c.AccountID).Scan(&dom); err != nil || dom == "" {
			jsonError(w, 404, "domain not found")
			return
		}
		if req.Force {
			var n int
			db.QueryRow(`SELECT COUNT(*) FROM ssl_certs s JOIN accounts a ON a.id = s.account_id
				WHERE s.domain = ? AND s.status IN ('issued', 'self-signed') AND a.status = 'active'`, dom).Scan(&n)
			if n == 0 {
				jsonError(w, 400, "issue an SSL certificate for this domain first")
				return
			}
		}
		forceInt := 0
		if req.Force {
			forceInt = 1
		}
		db.Exec("UPDATE domains SET force_https = ? WHERE id = ? AND account_id = ?",
			forceInt, id, c.AccountID)
		if err := applyForceHTTPS(db, dom); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		auditLog(db, r, "domain.https", map[string]interface{}{"id": id, "domain": dom, "force": req.Force})
		jsonResp(w, 200, map[string]interface{}{"status": "updated", "force_https": req.Force})
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
			// No owner left: drop its certificates too, or nginx keeps
			// serving 443 for a deleted domain.
			db.Exec("DELETE FROM ssl_certs WHERE domain = ?", delDomain)
			db.Exec("DELETE FROM redirects WHERE domain_id IN (SELECT id FROM domains WHERE domain = ?)", delDomain)
			db.Exec(`DELETE FROM error_pages WHERE domain_id IN (SELECT id FROM domains WHERE domain = ?)`, delDomain)
			if err := removeNginxSSL(db, delDomain); err != nil {
				log.Printf("[DOMAINS] HTTPS teardown for %s: %v", delDomain, err)
			} else {
				needsReload = true
			}
		}
		if needsReload {
			reloadNginx()
		}

		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}

// validMysqlIdent reports whether s is safe to use as a MySQL database/user identifier.
func validMysqlIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

// mysqlEscape escapes a string for safe use inside a single-quoted MySQL literal.
func mysqlEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "''")
	return s
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

		// One-time enforcement made atomic: claim the token in a single
		// conditional UPDATE so two racing requests cannot both use it.
		res, err := database.Exec(`UPDATE pma_tokens SET used = 1
			WHERE token_hash = ? AND used = 0 AND expires_at > datetime('now')`, token)
		if err != nil {
			http.Error(w, "invalid token", http.StatusForbidden)
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			http.Error(w, "token already used — get a new link from the control panel", http.StatusForbidden)
			return
		}

		var sessionKey string
		if err := database.QueryRow("SELECT session_key FROM pma_tokens WHERE token_hash = ?", token).Scan(&sessionKey); err != nil {
			http.Error(w, "invalid token", http.StatusForbidden)
			return
		}

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

func notifyTicketEvent(db *sql.DB, ticketID, accountID int, event string) {
	var subject string
	if err := db.QueryRow("SELECT subject FROM support_tickets WHERE id = ?", ticketID).Scan(&subject); err != nil {
		log.Printf("[TICKET] notify: failed to query subject for ticket %d: %v", ticketID, err)
		return
	}
	var title, msg string
	switch event {
	case "replied_user":
		title = "New Reply from User"
		msg = fmt.Sprintf("User replied to ticket #%d: %s", ticketID, subject)
	case "replied_admin":
		title = "New Reply from Staff"
		msg = fmt.Sprintf("Staff replied to ticket #%d: %s", ticketID, subject)
	default:
		return
	}
	if _, err := db.Exec("INSERT INTO notifications (account_id, title, message) VALUES (?, ?, ?)", accountID, title, msg); err != nil {
		log.Printf("[TICKET] notify: failed to insert notification for ticket %d: %v", ticketID, err)
	}

	if event == "replied_admin" {
		var email string
		if err := db.QueryRow("SELECT email FROM accounts WHERE id = ?", accountID).Scan(&email); err != nil {
			log.Printf("[TICKET] notify: no email for account %d: %v", accountID, err)
		} else if email != "" {
			raw := buildRawEmail("noreply@localhost", email, title, msg, "")
			if err := deliverRemote(db, "noreply@localhost", email, raw); err != nil {
				log.Printf("[TICKET] notify: email delivery failed for %s: %v", email, err)
			}
		}
	}
}

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
			jsonError(w, 500, "failed to query tickets")
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
			jsonResp(w, 200, map[string]interface{}{
				"tickets": tickets,
				"total":   0,
				"limit":   limit,
				"offset":  offset,
			})
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

	// Create ticket (child accounts only)
	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		if c.Scope != "child" {
			jsonError(w, 403, "only child accounts can create tickets")
			return
		}
		var count int
		db.QueryRow("SELECT COUNT(*) FROM support_tickets WHERE account_id = ? AND created_at > datetime('now', '-1 hour')", c.AccountID).Scan(&count)
		if count >= 10 {
			jsonError(w, 429, "too many tickets created in the last hour")
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
		db.Exec("INSERT INTO notifications (account_id, title, message) VALUES (NULL, 'New Support Ticket', ?)",
			fmt.Sprintf("New ticket #%d: %s", ticketID, req.Subject))
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
			jsonError(w, 500, "failed to query messages")
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
			jsonResp(w, 200, map[string]interface{}{
				"messages": msgs,
				"total":    0,
				"limit":    limit,
				"offset":   offset,
			})
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

		tx, err := db.Begin()
		if err != nil {
			jsonError(w, 500, "failed to start transaction")
			return
		}
		defer tx.Rollback()

		_, err = tx.Exec(`INSERT INTO ticket_messages (ticket_id, sender_type, sender_id, message) VALUES (?, ?, ?, ?)`,
			id, senderType, senderID, req.Message)
		if err != nil {
			jsonError(w, 500, "failed to send reply")
			return
		}
		_, err = tx.Exec("UPDATE support_tickets SET status = ?, updated_at = datetime('now') WHERE id = ?", newStatus, id)
		if err != nil {
			jsonError(w, 500, "failed to update ticket")
			return
		}
		if err := tx.Commit(); err != nil {
			jsonError(w, 500, "failed to commit reply")
			return
		}
		event := "replied_user"
		if senderType == "admin" {
			event = "replied_admin"
		}
		notifyTicketEvent(db, id, ownerID, event)
		jsonResp(w, 200, map[string]string{"status": newStatus})
	})

	// Update ticket status (admin only; account owners may close/reopen own)
	r.Put("/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil || (c.Scope != "parent" && c.Scope != "child") {
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
		if c.Scope == "child" {
			// Owners may only close or reopen their own tickets.
			if req.Status != "closed" && req.Status != "open" {
				jsonError(w, 403, "you can only close or reopen your own tickets")
				return
			}
			var owner int
			if err := db.QueryRow("SELECT account_id FROM support_tickets WHERE id = ?", id).Scan(&owner); err != nil || owner != c.AccountID {
				jsonError(w, 404, "ticket not found")
				return
			}
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

		var deletedSubject string
		db.QueryRow("SELECT subject FROM support_tickets WHERE id = ?", id).Scan(&deletedSubject)
		db.Exec("INSERT INTO notifications (account_id, title, message) VALUES (NULL, 'Ticket Deleted', ?)",
			fmt.Sprintf("Ticket #%d was deleted: %s", id, deletedSubject))

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
		var originalMsg string
		var ticketID int
		db.QueryRow("SELECT message, ticket_id FROM ticket_messages WHERE id = ?", id).Scan(&originalMsg, &ticketID)
		result, err := db.Exec("UPDATE ticket_messages SET message = ? WHERE id = ?", req.Message, id)
		if err != nil {
			jsonError(w, 500, "failed to update message")
			return
		}
		if n, _ := result.RowsAffected(); n == 0 {
			jsonError(w, 404, "message not found")
			return
		}
		log.Printf("[TICKET] Message %d in ticket %d edited: %d chars → %d chars", id, ticketID, len(originalMsg), len(req.Message))
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

// --- Docker Containers ---

type DBContainer struct {
	ID             int     `json:"db_id"`
	AccountID      int     `json:"account_id"`
	ContainerID    string  `json:"container_id"`
	ContainerName  string  `json:"container_name"`
	Status         string  `json:"status"`
	CPULimit       float64 `json:"cpu_limit"`
	RAMLimitMB     int     `json:"ram_limit_mb"`
	StorageLimitGB int     `json:"storage_limit_gb"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

type ContainerResult struct {
	DockerInfo  *docker.ContainerInfo `json:"docker_info"`
	DBInfo      *DBContainer          `json:"db_info"`
	AccountID   int                   `json:"account_id"`
	AccountName string                `json:"account_name"`
	HasDocker   bool                  `json:"has_docker"`
	DockerError string                `json:"docker_error,omitempty"`
}

func syncDockerContainers(db *sql.DB) {
	containers, err := docker.ListContainers()
	if err != nil {
		log.Printf("[DOCKER] Failed to list containers for sync: %v", err)
		return
	}
	for _, c := range containers {
		_, err := db.Exec(`INSERT INTO docker_containers (account_id, container_id, container_name, status,
			cpu_limit, ram_limit_mb, storage_limit_gb, container_ip, last_synced_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
			ON CONFLICT(account_id) DO UPDATE SET
				container_id = excluded.container_id,
				container_name = excluded.container_name,
				status = excluded.status,
				cpu_limit = excluded.cpu_limit,
				ram_limit_mb = excluded.ram_limit_mb,
				storage_limit_gb = excluded.storage_limit_gb,
				container_ip = excluded.container_ip,
				last_synced_at = datetime('now'),
				updated_at = datetime('now')`,
			c.AccountID, c.ID, c.Name, c.Status,
			c.CPULimit, c.RAMLimitMB, c.StorageLimitGB, c.ContainerIP)
		if err != nil {
			log.Printf("[DOCKER] Sync failed for container %s: %v", c.Name, err)
		}
	}
}

func provisionAllAccounts(db *sql.DB) {
	log.Println("[DOCKER] Starting retroactive container provisioning for all accounts...")
	rows, err := db.Query(`SELECT a.id, a.username, a.home_dir,
		COALESCE(a.ram_limit_mb, COALESCE(p.ram_limit_mb, 0)) as ram_limit,
		p.ram_limit_mb as pkg_ram
		FROM accounts a JOIN packages p ON a.package_id = p.id
		WHERE a.status NOT IN ('terminated')`)
	if err != nil {
		log.Printf("[DOCKER] Failed to query accounts for provisioning: %v", err)
		return
	}
	defer rows.Close()
	type provisionTarget struct {
		ID       int
		Username string
		HomeDir  string
		RAMLimit int
	}
	var targets []provisionTarget
	for rows.Next() {
		var t provisionTarget
		var perAccRam, pkgRam int
		if err := rows.Scan(&t.ID, &t.Username, &t.HomeDir, &perAccRam, &pkgRam); err != nil {
			continue
		}
		t.RAMLimit = perAccRam
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DOCKER] provisioning rows iteration error: %v", err)
	}

	provisioned := 0
	skipped := 0
	errors := 0
	for _, t := range targets {
		if docker.ContainerExists(docker.ContainerName(t.ID, t.Username)) {
			skipped++
			continue
		}
		cpuLimit := float64(t.RAMLimit) / 1024.0
		if cpuLimit < 0.5 {
			cpuLimit = 0.5
		}
		if cpuLimit > 8 {
			cpuLimit = 8
		}
		_, err := docker.ProvisionAccount(t.ID, t.Username, t.HomeDir, t.RAMLimit, cpuLimit)
		if err != nil {
			log.Printf("[DOCKER] Failed to provision container for account %s (ID: %d): %v", t.Username, t.ID, err)
			errors++
			continue
		}
		// Update or insert DB record
		containerName := docker.ContainerName(t.ID, t.Username)
		info, _ := docker.GetContainerInfo(containerName)
		if info != nil {
			db.Exec(`INSERT INTO docker_containers (account_id, container_id, container_name, status,
				cpu_limit, ram_limit_mb, storage_limit_gb, last_synced_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
				ON CONFLICT(account_id) DO UPDATE SET
					container_id = excluded.container_id,
					container_name = excluded.container_name,
					status = excluded.status,
					cpu_limit = excluded.cpu_limit,
					ram_limit_mb = excluded.ram_limit_mb,
					storage_limit_gb = excluded.storage_limit_gb,
					last_synced_at = datetime('now'),
					updated_at = datetime('now')`,
				t.ID, info.ID, info.Name, info.Status, cpuLimit, t.RAMLimit, 0)
		}
		provisioned++
		log.Printf("[DOCKER] Provisioned container for account %s (ID: %d)", t.Username, t.ID)
	}
	log.Printf("[DOCKER] Retroactive provisioning complete: %d provisioned, %d skipped, %d errors", provisioned, skipped, errors)
}

// ========== HELPERS ==========

func copyRecursive(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if srcInfo.IsDir() {
		if err := os.MkdirAll(dst, 0755); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			srcPath := filepath.Join(src, entry.Name())
			dstPath := filepath.Join(dst, entry.Name())
			if err := copyRecursive(srcPath, dstPath); err != nil {
				return err
			}
		}
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return os.Chmod(dst, 0644)
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

	// Share the handle with the API-token fallback in authMw.
	tokenAuthDB = database

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
						a.id, a.domain, filepath.Join(a.homeDir, "public_html"))
				}
			}
		}

	jwtSecret := os.Getenv("OWP_JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("OWP_JWT_SECRET environment variable is required - set a random 48+ character string")
	}
	jwtManager := auth.NewJWTManager(jwtSecret, 900, 604800)

	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			database.Exec("DELETE FROM refresh_tokens WHERE expires_at <= datetime('now')")
			database.Exec("DELETE FROM blocked_ips WHERE updated_at <= datetime('now', '-24 hours')")
		}
	}()

	corsHandler := cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:*", "http://127.0.0.1:*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	})

	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, 200, map[string]string{"status": "ok", "version": "2.1.0"})
	}

	mainLogger := logging.NewDefault("parentd")

	// ==================== ADMIN ROUTER (:9000) ====================
	adminRouter := chi.NewRouter()
	adminRouter.Use(middleware.RequestID)
	adminRouter.Use(middleware.Recoverer)
	adminRouter.Use(middleware.RequestLogger(mainLogger))
	adminRouter.Use(corsHandler)
	adminRouter.Use(middleware.ValidateContentType)
	adminRouter.Use(middleware.SecurityHeaders)
	adminRouter.Use(middleware.HSTSMiddleware(63072000, true))
	adminRouter.Use(middleware.BodyLimit(2 << 30))

	adminRouter.Get("/healthz", healthHandler)

	adminRouter.Route("/api/v1/auth", func(r chi.Router) {
		authRoutes(r, database, jwtManager)
	})

	adminRouter.Route("/api/v1/packages", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		packageRoutes(r, database)
	})

	adminRouter.Route("/api/v1/accounts", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		accountRoutes(r, database, jwtManager)
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

	adminRouter.Route("/api/v1/api-tokens", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		adminTokenRoutes(r, database)
	})

	adminRouter.Route("/api/v1/nodes", func(r chi.Router) {
		r.Use(authMw(jwtManager, "parent"))
		k8sRoutes(r, database)
		k8sDepsRoutes(r, database)
		k8sHealthRoutes(r, database)
		k8sNodesRoutes(r, database)
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
		// A request for a hosted domain should never reach the panel: it
		// means nginx has no vhost for that Host (missing file, skipped
		// reload, inactive account). Serve an explicit parked page instead of
		// the admin login so visitors never mistake it for the site.
		if host := parkedHostFor(r, database); host != "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `<!doctype html><html><head><title>Site not configured</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>body{font-family:system-ui,sans-serif;display:flex;min-height:90vh;align-items:center;justify-content:center;background:#f8fafc;color:#334155;margin:0}.box{text-align:center;max-width:420px;padding:24px}h1{font-size:20px;margin:0 0 8px}p{font-size:14px;color:#64748b}</style>
</head><body><div class="box"><h1>Site not configured</h1>
<p>The domain <strong>%s</strong> is registered here but its web server
configuration is missing. The site owner should check the domain's vhost or
contact the administrator.</p></div></body></html>`, html.EscapeString(host))
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
	childRouter.Use(middleware.RequestID)
	childRouter.Use(middleware.Recoverer)
	childRouter.Use(middleware.RequestLogger(mainLogger))
	childRouter.Use(corsHandler)
	childRouter.Use(middleware.ValidateContentType)
	childRouter.Use(middleware.SecurityHeaders)
	childRouter.Use(middleware.HSTSMiddleware(63072000, true))
	childRouter.Use(middleware.BodyLimit(2 << 30))

	childRouter.Get("/healthz", healthHandler)

	childRouter.Route("/api/v1/child/auth", func(r chi.Router) {
		childAuthRoutes(r, database, jwtManager)
	})

	childRouter.Route("/api/v1/child/account", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), trackBandwidth(database))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil {
				jsonError(w, 401, "unauthorized")
				return
			}
			var username, domain, email, homeDir, status, pkgName string
			var diskMB, bandwidthMB, ramLimitMB, maxDB, maxEmail, maxFTP, maxDomains, maxSubdomains int
			var diskUsed, ramUsed int
			var bwUsed float64
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
			// Apply per-account admin overrides (server_config keys, 0 = package default)
			diskMB = getAccountOverride(database, c.AccountID, "disk_limit", diskMB)
			bandwidthMB = getAccountOverride(database, c.AccountID, "bandwidth_limit", bandwidthMB)
			maxDB = getAccountOverride(database, c.AccountID, "max_db", maxDB)
			maxEmail = getAccountOverride(database, c.AccountID, "max_email", maxEmail)
			maxFTP = getAccountOverride(database, c.AccountID, "max_ftp", maxFTP)
			maxDomains = getAccountOverride(database, c.AccountID, "max_domains", maxDomains)
			maxSubdomains = getAccountOverride(database, c.AccountID, "max_subdomains", maxSubdomains)
			var dbCount, domainCount, addonCount, subdomainCount, emailCount, ftpCount int
			database.QueryRow("SELECT COUNT(*) FROM child_databases WHERE account_id = ?", c.AccountID).Scan(&dbCount)
			database.QueryRow("SELECT COUNT(*) FROM domains WHERE account_id = ? AND type != 'subdomain'", c.AccountID).Scan(&domainCount)
			database.QueryRow("SELECT COUNT(*) FROM domains WHERE account_id = ? AND type = 'addon'", c.AccountID).Scan(&addonCount)
			database.QueryRow("SELECT COUNT(*) FROM domains WHERE account_id = ? AND type = 'subdomain'", c.AccountID).Scan(&subdomainCount)
			database.QueryRow("SELECT COUNT(*) FROM email_accounts WHERE account_id = ?", c.AccountID).Scan(&emailCount)
			database.QueryRow("SELECT COUNT(*) FROM ftp_accounts WHERE account_id = ?", c.AccountID).Scan(&ftpCount)
			sharedIP := ip.String
			if !ip.Valid || sharedIP == "" {
				sharedIP = getSharedIP()
			}
			// Override disk_used_mb and ram_used_mb with live Docker stats if container exists
			var containerName string
			database.QueryRow("SELECT container_name FROM docker_containers WHERE account_id = ?", c.AccountID).Scan(&containerName)
			if containerName != "" {
				if info, err := docker.GetContainerInfo(containerName); err == nil && info.Status == "running" {
					if info.DiskUsageMB > 0 {
						diskUsed = int(info.DiskUsageMB)
					}
					if info.RAMUsageMB > 0 {
						ramUsed = info.RAMUsageMB
					}
				}
			}

		// Override bandwidth_used_mb with live float sum from bandwidth_logs
		var liveBWMB float64
		database.QueryRow("SELECT COALESCE(CAST(SUM(bytes_out + bytes_in) AS REAL)/1048576.0, 0) FROM bandwidth_logs WHERE account_id = ?", c.AccountID).Scan(&liveBWMB)
		bwUsed = liveBWMB

		resourceWarning := resourceLimitWarningHandler(database, c.AccountID)

		features := map[string]bool{}
		for _, f := range []string{"files", "emails", "ftp", "db", "backups", "cron"} {
			features[f] = featureEnabledFor(database, c.AccountID, f)
		}

		jsonResp(w, 200, map[string]interface{}{
			"username": username, "domain": domain, "email": email, "home_dir": homeDir,
			"status": status, "shared_ip": sharedIP, "package_name": pkgName,
			"disk_limit_mb": diskMB, "disk_used_mb": diskUsed,
			"bandwidth_limit_mb": bandwidthMB, "bandwidth_used_mb": bwUsed,
			"ram_limit_mb": ramLimitMB, "ram_used_mb": ramUsed, "resource_warning": resourceWarning,
			"max_databases": maxDB, "databases_used": dbCount,
			"max_email": maxEmail, "emails_used": emailCount,
			"max_ftp": maxFTP, "ftp_used": ftpCount,
			"max_domains": maxDomains, "total_domains": domainCount,
			"addon_domains": addonCount, "subdomains": subdomainCount, "max_subdomains": maxSubdomains,
			"features": features,
		})
		})
	})

	childRouter.Route("/api/v1/child/account/docker-stats", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil {
				jsonError(w, 401, "unauthorized")
				return
			}
			var containerName string
			err := database.QueryRow("SELECT container_name FROM docker_containers WHERE account_id = ?", c.AccountID).Scan(&containerName)
			if err != nil || containerName == "" {
				jsonResp(w, 200, map[string]interface{}{
					"ram_usage_mb": 0, "disk_usage_mb": 0,
					"network_rx_bytes": 0, "network_tx_bytes": 0,
					"cpu_cores_usage": 0, "ram_limit_mb": 0,
					"has_container": false,
				})
				return
			}
			info, err := docker.GetContainerInfo(containerName)
			if err != nil {
				jsonResp(w, 200, map[string]interface{}{
					"ram_usage_mb": 0, "disk_usage_mb": 0,
					"network_rx_bytes": 0, "network_tx_bytes": 0,
					"cpu_cores_usage": 0, "ram_limit_mb": 0,
					"has_container": true, "error": err.Error(),
				})
				return
			}
			jsonResp(w, 200, map[string]interface{}{
				"ram_usage_mb":     info.RAMUsageMB,
				"disk_usage_mb":    info.DiskUsageMB,
				"network_rx_bytes": info.NetworkRXBytes,
				"network_tx_bytes": info.NetworkTXBytes,
				"cpu_cores_usage":  info.CPUCoresUsage,
				"ram_limit_mb":     info.RAMLimitMB,
				"cpu_limit":        info.CPULimit,
				"status":           info.Status,
				"has_container":    true,
			})
		})
	})

	childRouter.Route("/api/v1/child/files", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), requireFeature(database, "files"), trackBandwidth(database))
		childFileRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/databases", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), requireFeature(database, "db"), trackBandwidth(database))
		childDbRoutes(r, database, jwtManager)
	})

	childRouter.Route("/api/v1/child/domains", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), trackBandwidth(database))
		childDomainRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/notifications", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database))
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
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), trackBandwidth(database))
		childBandwidthRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/cms", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), trackBandwidth(database))
		cmsRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/ssl", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), trackBandwidth(database))
		certRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/emails", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), requireEmailsReadOnly(database), trackBandwidth(database))
		childEmailRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/cron", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), requireFeature(database, "cron"), trackBandwidth(database))
		childCronRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/backups", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), requireFeature(database, "backups"), trackBandwidth(database))
		childBackupRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/dns", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), trackBandwidth(database))
		childDNSRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/ftp", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), requireFeature(database, "ftp"), trackBandwidth(database))
		childFTPRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/ssh", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), trackBandwidth(database))
		childSSHKeyRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/tokens", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), trackBandwidth(database))
		childTokenRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/redirects", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), trackBandwidth(database))
		redirectRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/hotlink", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), trackBandwidth(database))
		hotlinkRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/stats", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), trackBandwidth(database))
		childStatsRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/errors", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), trackBandwidth(database))
		childErrorRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/php-versions", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database), trackBandwidth(database))
		childPhpVersionRoutes(r, database)
	})

	childRouter.Route("/api/v1/child/tickets", func(r chi.Router) {
		r.Use(authMw(jwtManager, "child"), requireActiveAccount(database))
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
		adminListenAddr = "127.0.0.1:9000"
	}
	childListenAddr := os.Getenv("OWP_CHILD_LISTEN")
	if childListenAddr == "" {
		childListenAddr = "127.0.0.1:9001"
	}

	// Start SMTP server for incoming mail (port 2525, iptables redirects 25->2525)
	go startSMTPServer(database)

	// Start FTP server for ftp_accounts (port 21 by default, OWP_FTP_PORT)
	go startFTPServer(database)

	// Start cron job runner (evaluates every 30s)
	go startCronRunner(database)

	// Reset backups left in flight by a previous crash/restart so the affected
	// accounts are not blocked from new backups, restores and schedules forever.
	recoverInterruptedBackups(database)

	// Move any archives stored by older versions under the host-level backups
	// directory into each account's ~/backups folder.
	migrateBackupLocations(database)

	// Start automatic backup scheduler (evaluates every 45s)
	go startBackupScheduler(database)

	// Start SSL certificate renewal checker (daily)
	startCertRenewal(database)

	// Start Nginx vhost log bandwidth collector (5s poll, rescan every 50s)
	startNginxBandwidthCollector(database)

	// Start RAM usage tracker (every 60 sec)
	startRAMTracker(database)

	// Start disk usage tracker (every 5 min)
	startDiskTracker(database)

	// Start K3s worker health checker (every 5 min, autofix on)
	startK8sHealthChecker(database)

	// Start K3s metrics snapshots (every 30s, SQLite history for UI)
	startK8sMetricsCollector(database)

	// Start auto-suspender for resource limit enforcement (every 60 sec)
	startAutoSuspender(database)

	// Start suspended-account auto-purger (hourly, grace from settings)
	startSuspendedPurger(database)

	// Docker initialization
	go func() {
		if err := docker.EnsureInstalled(); err != nil {
			log.Printf("[DOCKER] Docker not available: %v", err)
			syncNginxVhosts(database)
			return
		}
		if err := docker.EnsureNetwork(); err != nil {
			log.Printf("[DOCKER] Failed to create owp-network: %v", err)
		}
		if err := docker.EnsureImage(); err != nil {
			log.Printf("[DOCKER] Failed to build child image: %v", err)
		}
		syncDockerContainers(database)
		log.Println("[DOCKER] Manager initialized and containers synced")
		// Retroactively provision containers for accounts that don't have them
		provisionAllAccounts(database)
		// Sync vhosts after containers are in the DB with correct IPs
		syncNginxVhosts(database)
	}()

	// Periodic Docker container sync (every 5 minutes)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			if err := docker.EnsureInstalled(); err != nil {
				continue
			}
			syncDockerContainers(database)
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

	adminServer := &http.Server{
		Addr:         adminListenAddr,
		Handler:      adminRouter,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	childServer := &http.Server{
		Addr:         childListenAddr,
		Handler:      childRouter,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("[CHILD] Child Panel HTTP server on %s", childListenAddr)
		if err := childServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[CHILD] Child server error: %v", err)
		}
	}()

	go func() {
		log.Printf("[ADMIN] Admin Panel HTTP server on %s", adminListenAddr)
		if err := adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[ADMIN] Admin server error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("Received signal %v, shutting down...", sig)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	childServer.Shutdown(shutdownCtx)
	adminServer.Shutdown(shutdownCtx)
	log.Println("Servers stopped")
}
