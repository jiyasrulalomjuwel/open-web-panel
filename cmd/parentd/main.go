package main

import (
	"archive/zip"
	"context"
	"crypto/rand"
	_ "crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

// ---------- SQLite init ----------

func initDB(path string) (*sql.DB, error) {
	db, err := sdb.ConnectSQLite(path)
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")

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
			status      TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','closed','replied')),
			created_at  TEXT DEFAULT (datetime('now')),
			updated_at  TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS ticket_messages (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			ticket_id   INTEGER NOT NULL,
			sender_type TEXT NOT NULL CHECK(sender_type IN ('user','admin')),
			sender_id   INTEGER NOT NULL,
			message     TEXT NOT NULL,
			created_at  TEXT DEFAULT (datetime('now'))
		);

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
			status          TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled')),
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
	}
	for _, m := range migrations {
		db.Exec(m)
	}

	seedDefaultData(db)
	return db, nil
}

func seedDefaultData(db *sql.DB) {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM packages").Scan(&count)
	if count == 0 {
		db.Exec(`INSERT INTO packages (name, disk_mb, bandwidth_mb, max_db, max_email, max_ftp, max_domains, max_subdomains, ssh_access, backup_enabled, is_default)
			VALUES ('default', 1000, 10000, 5, 10, 5, 3, 10, 0, 1, 1)`)
		db.Exec(`INSERT INTO packages (name, disk_mb, bandwidth_mb, max_db, max_email, max_ftp, max_domains, max_subdomains, ssh_access, backup_enabled)
			VALUES ('starter', 500, 5000, 2, 5, 2, 1, 5, 0, 1)`)
		db.Exec(`INSERT INTO packages (name, disk_mb, bandwidth_mb, max_db, max_email, max_ftp, max_domains, max_subdomains, ssh_access, backup_enabled)
			VALUES ('premium', 5000, 50000, 20, 50, 20, 10, 50, 1, 1)`)
		log.Println("Seeded 3 hosting packages")
	}

	db.QueryRow("SELECT COUNT(*) FROM admins").Scan(&count)
	if count == 0 {
		hash, _ := auth.HashPassword("admin123")
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
}

// ---------- middleware ----------

func authMw(jwtManager *auth.JWTManager) func(http.Handler) http.Handler {
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
			// Allow token via query param for media/viewer requests
			if tokenStr == "" {
				tokenStr = r.URL.Query().Get("token")
			}
			if tokenStr == "" {
				jsonError(w, 401, "missing authorization")
				return
			}
			claims, err := jwtManager.ValidateToken(tokenStr)
			if err != nil {
				jsonError(w, 401, "invalid or expired token")
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

		var adminID int
		var username, passwordHash, role string
		var lastLogin sql.NullString
		err := db.QueryRow(`SELECT id, username, password_hash, role, last_login_at FROM admins WHERE username = ?`,
			req.Username).Scan(&adminID, &username, &passwordHash, &role, &lastLogin)
		if err != nil {
			jsonError(w, 401, "invalid credentials")
			return
		}
		if !auth.CheckPassword(passwordHash, req.Password) {
			jsonError(w, 401, "invalid credentials")
			return
		}

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

	r.With(authMw(jwtManager)).Get("/me", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		jsonResp(w, 200, map[string]interface{}{
			"id": c.UserID, "username": c.Username, "role": c.Role, "scope": c.Scope,
		})
	})

		// Refresh token endpoint
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

			// Delete old refresh token (rotation)
			db.Exec("DELETE FROM refresh_tokens WHERE token_hash = ?", req.RefreshToken)

			if scope == "parent" {
				var username, role string
				err := db.QueryRow("SELECT username, role FROM admins WHERE id = ?", userID).Scan(&username, &role)
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
			} else {
				var username, status string
				err := db.QueryRow("SELECT username, status FROM accounts WHERE id = ?", userID).Scan(&username, &status)
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
			}
		})
}

// --- Packages ---

func packageRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT id, name, disk_mb, bandwidth_mb, max_db, max_email, max_ftp,
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
			if err := rows.Scan(&p.ID, &p.Name, &p.DiskMB, &p.BandwidthMB, &p.MaxDB, &p.MaxEmail, &p.MaxFTP,
				&p.MaxDomains, &p.MaxSubdomains, &ssh, &backup, &def, &p.CreatedAt, &p.UpdatedAt); err != nil {
				continue
			}
			p.SSHAccess = ssh == 1
			p.BackupEnabled = backup == 1
			p.IsDefault = def == 1
			pkgs = append(pkgs, p)
		}
		jsonResp(w, 200, pkgs)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name          string `json:"name"`
			DiskMB        int    `json:"disk_mb"`
			BandwidthMB   int    `json:"bandwidth_mb"`
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
		result, err := db.Exec(`INSERT INTO packages (name, disk_mb, bandwidth_mb, max_db, max_email,
			max_ftp, max_domains, max_subdomains, ssh_access, backup_enabled)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			req.Name, req.DiskMB, req.BandwidthMB, req.MaxDB, req.MaxEmail,
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
			MaxDB         int    `json:"max_db"`
			MaxEmail      int    `json:"max_email"`
			MaxFTP        int    `json:"max_ftp"`
			MaxDomains    int    `json:"max_domains"`
			MaxSubdomains int    `json:"max_subdomains"`
			SSHAccess     bool   `json:"ssh_access"`
			BackupEnabled bool   `json:"backup_enabled"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		ssh := 0
		if req.SSHAccess {
			ssh = 1
		}
		backup := 0
		if req.BackupEnabled {
			backup = 1
		}
		db.Exec(`UPDATE packages SET name=?, disk_mb=?, bandwidth_mb=?, max_db=?, max_email=?,
			max_ftp=?, max_domains=?, max_subdomains=?, ssh_access=?, backup_enabled=?, updated_at=datetime('now')
			WHERE id=?`,
			req.Name, req.DiskMB, req.BandwidthMB, req.MaxDB, req.MaxEmail, req.MaxFTP,
			req.MaxDomains, req.MaxSubdomains, ssh, backup, id)
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		auditLog(db, r, "package.delete", map[string]interface{}{"id": id})
		db.Exec("DELETE FROM packages WHERE id = ? AND is_default = 0", id)
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}

// --- Accounts ---

func accountRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		statusFilter := r.URL.Query().Get("status")
		query := `SELECT a.id, a.username, a.domain, a.email, a.package_id,
			p.name, a.status, a.home_dir, a.ip_address,
			a.disk_used_mb, a.bandwidth_used_mb,
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
				&a.DiskUsedMB, &a.BandwidthUsedMB, &reason, &a.CreatedAt, &a.UpdatedAt); err != nil {
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

		hash, _ := auth.HashPassword(req.Password)
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

		// Write Nginx vhost for the primary domain
		writeNginxVhost(req.Domain, primaryDocRoot, "")
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
			SuspendedReason string `json:"suspended_reason"`
			CreatedAt       string `json:"created_at"`
			UpdatedAt       string `json:"updated_at"`
		}
		var a Acc
		var ip, reason sql.NullString
		err := db.QueryRow(`SELECT a.id, a.username, a.domain, a.email, a.package_id,
			p.name, a.status, a.home_dir, a.ip_address,
			a.disk_used_mb, a.bandwidth_used_mb, COALESCE(a.suspended_reason, ''), a.created_at, a.updated_at
			FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`, id).Scan(
			&a.ID, &a.Username, &a.Domain, &a.Email, &a.PackageID,
			&a.PackageName, &a.Status, &a.HomeDir, &ip,
			&a.DiskUsedMB, &a.BandwidthUsedMB, &reason, &a.CreatedAt, &a.UpdatedAt)
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
		json.NewDecoder(r.Body).Decode(&req)
		auditLog(db, r, "account.suspend", map[string]interface{}{"id": id, "reason": req.Reason})
		db.Exec("UPDATE accounts SET status='suspended', suspended_reason=?, updated_at=datetime('now') WHERE id=?", req.Reason, id)
		jsonResp(w, 200, map[string]string{"status": "suspended"})
	})

	r.Post("/{id}/reset-password", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct{ Password string `json:"password"` }
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Password) < 8 {
			jsonError(w, 400, "password must be at least 8 characters")
			return
		}
		hash, _ := auth.HashPassword(req.Password)
		db.Exec("UPDATE accounts SET password_hash = ?, updated_at = datetime('now') WHERE id = ?", hash, id)
		auditLog(db, r, "account.password_reset", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "password reset"})
	})

	r.Post("/{id}/unsuspend", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		auditLog(db, r, "account.unsuspend", map[string]interface{}{"id": id})
		db.Exec("UPDATE accounts SET status='active', suspended_reason=NULL, updated_at=datetime('now') WHERE id=?", id)
		jsonResp(w, 200, map[string]string{"status": "active"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		auditLog(db, r, "account.terminate", map[string]interface{}{"id": id})
		db.Exec("UPDATE accounts SET status='terminated', updated_at=datetime('now') WHERE id=?", id)

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
					removeNginxVhost(domain)
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

		jsonResp(w, 200, stats)
	})
}

// --- Server ---

func getSharedIP() string {
	// Try the OWP_SHARED_IP env var first
	if ip := os.Getenv("OWP_SHARED_IP"); ip != "" {
		return ip
	}
	// Try to detect primary network IP
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
			"load_1m":       0.5, "load_5m": 0.7, "load_15m": 0.6,
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
	cmd.Stderr = nil
	out, err := cmd.Output()
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

func syncNginxVhosts(db *sql.DB) {
	// Only write vhosts for active accounts, deduplicated by domain name.
	// When multiple active accounts share the same domain, the most recently
	// added domain record wins (highest id).
	rows, err := db.Query(`SELECT d.domain, d.doc_root FROM domains d
		JOIN accounts a ON a.id = d.account_id
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
	defer rows.Close()
	activeVhosts := make(map[string]bool)
	for rows.Next() {
		var domain, docRoot string
		rows.Scan(&domain, &docRoot)
		os.MkdirAll(docRoot, 0755)
		if err := writeNginxVhost(domain, docRoot, ""); err != nil {
			log.Printf("[NGINX] Failed to write vhost for %s: %v", domain, err)
		} else {
			log.Printf("[NGINX] Synced vhost for %s", domain)
		}
		activeVhosts[domain] = true
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
	if err == nil {
		defer certRows.Close()
		for certRows.Next() {
			var domain string
			certRows.Scan(&domain)
			if activeVhosts[domain] {
				addNginxSSL(domain)
			}
		}
	}
}

func writeNginxVhost(domain, docRoot, accountIP string) error {
	logDir := getNginxLogDir()
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
        fastcgi_pass unix:/run/php/php8.3-fpm.sock;
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
	// Try sudo first (configured during install), fall back to direct
	if out, err := runCmd("sudo", "-n", nginxBin, "-s", "reload"); err != nil {
		runCmd(nginxBin, "-s", "reload")
	} else {
		_ = out
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

		var id int
		var username, passwordHash, status, homeDir string
		err := db.QueryRow(`SELECT id, username, password_hash, status, home_dir FROM accounts WHERE username = ?`, req.Username).Scan(
			&id, &username, &passwordHash, &status, &homeDir)
		if err != nil {
			jsonError(w, 401, "invalid credentials")
			return
		}
		if status != "active" {
			jsonError(w, 403, "account is "+status)
			return
		}
		if !auth.CheckPassword(passwordHash, req.Password) {
			jsonError(w, 401, "invalid credentials")
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

	r.With(authMw(jwtManager)).Get("/me", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		var homeDir string
		db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", c.AccountID).Scan(&homeDir)
		jsonResp(w, 200, map[string]interface{}{
			"id": c.UserID, "username": c.Username, "home_dir": homeDir, "role": c.Role,
		})
	})

	r.With(authMw(jwtManager)).Put("/change-password", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		var req struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		var hash string
		db.QueryRow("SELECT password_hash FROM accounts WHERE id = ?", c.AccountID).Scan(&hash)
		if !auth.CheckPassword(hash, req.CurrentPassword) {
			jsonError(w, 400, "current password is incorrect")
			return
		}
		newHash, _ := auth.HashPassword(req.NewPassword)
		db.Exec("UPDATE accounts SET password_hash = ? WHERE id = ?", newHash, c.AccountID)
		auditLog(db, r, "account.change_password", map[string]interface{}{"account_id": c.AccountID})
		jsonResp(w, 200, map[string]string{"status": "password changed"})
	})

	r.With(authMw(jwtManager)).Get("/upload-limit", func(w http.ResponseWriter, r *http.Request) {
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

	r.With(authMw(jwtManager)).Put("/upload-limit", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		var req struct{ LimitMB int `json:"limit_mb"` }
		json.NewDecoder(r.Body).Decode(&req)
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
}

// --- Child File Manager ---

func childFileRoutes(r chi.Router, db *sql.DB) {
	getHomeDir := func(r *http.Request) string {
		c := getClaims(r)
		if c == nil {
			return ""
		}
		var h string
		db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", c.AccountID).Scan(&h)
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

		var safePath string
		if strings.HasPrefix(userPath, home) {
			safePath = userPath
		} else {
			var err error
			safePath, err = filesystem.SafePath(home, userPath)
			if err != nil {
				jsonError(w, 403, err.Error())
				return
			}
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
			info, _ := e.Info()
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
		json.NewDecoder(r.Body).Decode(&req)
		safe, err := filesystem.SafePath(home, req.Path)
		if err != nil {
			jsonError(w, 403, err.Error())
			return
		}
		if err := os.MkdirAll(safe+"/"+req.Name, 0755); err != nil {
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
		json.NewDecoder(r.Body).Decode(&req)
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
		trashDir := home + "/.trash"
		os.MkdirAll(trashDir, 0700)
		trashPath := trashDir + "/" + filepath.Base(safe)
		// Avoid collisions
		if _, err := os.Stat(trashPath); err == nil {
			trashPath = trashDir + "/" + filepath.Base(safe) + "_" + strconv.FormatInt(time.Now().UnixNano(), 36)
		}
		os.Rename(safe, trashPath)

		// Record in trash table for 30-day auto-cleanup
		isDir := 0
		if info.IsDir() {
			isDir = 1
		}
		db.Exec(`INSERT INTO file_trash (account_id, original_path, trash_path, size_bytes, is_dir, expires_at)
			VALUES (?, ?, ?, ?, ?, datetime('now', '+30 days'))`,
			c.AccountID, safe, trashPath, info.Size(), isDir)

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
		json.NewDecoder(r.Body).Decode(&req)
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
		os.Rename(old, newp)
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
		data, err := os.ReadFile(safe)
		if err != nil {
			jsonError(w, 404, "not found")
			return
		}
		contentType := "text"
		if len(data) > 512*1024 {
			contentType = "large"
		}
		jsonResp(w, 200, map[string]interface{}{
			"path":    r.URL.Query().Get("path"),
			"content": string(data),
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
		var req struct{ Path, Content string }
		json.NewDecoder(r.Body).Decode(&req)
		safe, err := filesystem.SafePath(home, req.Path)
		if err != nil {
			jsonError(w, 403, err.Error())
			return
		}
		os.WriteFile(safe, []byte(req.Content), 0644)
		jsonResp(w, 200, map[string]string{"status": "written"})
	})

	r.Get("/disk-usage", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" {
			jsonError(w, 403, "no home")
			return
		}
		var total int64
		walkDirForSize(home, &total)
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

		// Block dangerous file types that could be used to compromise the
		// server. Web files (PHP, HTML, JS, CSS, images, etc.) are allowed.
		blockedExt := map[string]bool{
			".exe": true, ".msi": true, ".bin": true, ".com": true,
			".scr": true, ".pif": true, ".jar": true,
			".bat": true, ".cmd": true, ".ps1": true, ".psm1": true,
			".psd1": true, ".vbs": true, ".vbe": true, ".jse": true,
			".wsf": true, ".wsh": true, ".msc": true,
			".dll": true, ".so": true, ".dylib": true, ".sys": true, ".drv": true,
			".sh": true, ".bash": true, ".zsh": true, ".ksh": true, ".csh": true,
			".class": true,
		}
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if blockedExt[ext] {
			jsonError(w, 403, "file type \""+ext+"\" is not allowed for security reasons")
			return
		}

		// Enforce per-account upload limit
		c := getClaims(r)
		if c != nil {
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

		safe, err := filesystem.SafePath(home, uploadPath+"/"+header.Filename)
		if err != nil {
			jsonError(w, 403, err.Error())
			return
		}
		dst, err := os.Create(safe)
		if err != nil {
			jsonError(w, 500, "failed to create file: "+err.Error())
			return
		}
		defer dst.Close()

		// Use a 1MB buffer so large files write in fewer syscalls.
		if _, err := io.CopyBuffer(dst, file, make([]byte, 1024*1024)); err != nil {
			os.Remove(safe) // clean up partial file
			jsonError(w, 500, "failed to write file: "+err.Error())
			return
		}

		jsonResp(w, 200, map[string]interface{}{"status": "uploaded", "name": header.Filename})
	})

	// Compress to zip
	r.Post("/compress", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" {
			jsonError(w, 403, "no home")
			return
		}
		var req struct {
			Path        string   `json:"path"`
			ArchiveName string   `json:"archive_name"`
			Files       []string `json:"files"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		safeDir, _ := filesystem.SafePath(home, req.Path)
		zf, _ := os.Create(safeDir + "/" + req.ArchiveName)
		defer zf.Close()
		zw := zip.NewWriter(zf)
		defer zw.Close()
		for _, f := range req.Files {
			src := safeDir + "/" + f
			info, err := os.Stat(src)
			if err != nil { continue }
			if info.IsDir() { addDirToZip(zw, src, f) } else { addFileToZip(zw, src, f) }
		}
		jsonResp(w, 200, map[string]string{"status": "compressed"})
	})

	// Extract zip
	r.Post("/extract", func(w http.ResponseWriter, r *http.Request) {
		home := getHomeDir(r)
		if home == "" { jsonError(w, 403, "no home"); return }
		var req struct{ Path string }
		json.NewDecoder(r.Body).Decode(&req)
		safePath, err := filesystem.SafePath(home, req.Path)
		if err != nil { jsonError(w, 403, err.Error()); return }
		r2, err := zip.OpenReader(safePath)
		if err != nil { jsonError(w, 400, "not a valid zip"); return }
		defer r2.Close()
		dest := filepath.Dir(safePath)
		for _, f := range r2.File {
			fp := filepath.Join(dest, f.Name)
			if f.FileInfo().IsDir() { os.MkdirAll(fp, 0755); continue }
			os.MkdirAll(filepath.Dir(fp), 0755)
			src, _ := f.Open(); dst, _ := os.Create(fp)
			io.Copy(dst, src); src.Close(); dst.Close()
		}
		jsonResp(w, 200, map[string]string{"status": "extracted"})
	})

	// --- Trash ---
	r.Get("/trash", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		rows, _ := db.Query(`SELECT id, original_path, size_bytes, is_dir, deleted_at FROM file_trash
			WHERE account_id = ? AND expires_at > datetime('now') ORDER BY deleted_at DESC`, c.AccountID)
		if rows == nil { jsonResp(w, 200, []interface{}{}); return }
		defer rows.Close()
		items := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id, sz, isDir int; var orig, del string
			rows.Scan(&id, &orig, &sz, &isDir, &del)
			items = append(items, map[string]interface{}{
				"id": id, "original_path": orig, "size_bytes": sz,
				"is_dir": isDir == 1, "deleted_at": del})
		}
		jsonResp(w, 200, items)
	})

	r.Post("/trash/{id}/restore", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r); id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var origPath, trashPath string
		db.QueryRow("SELECT original_path, trash_path FROM file_trash WHERE id = ? AND account_id = ?",
			id, c.AccountID).Scan(&origPath, &trashPath)
		os.Rename(trashPath, origPath)
		db.Exec("DELETE FROM file_trash WHERE id = ?", id)
		jsonResp(w, 200, map[string]string{"status": "restored"})
	})

	r.Delete("/trash/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r); id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var tp string
		db.QueryRow("SELECT trash_path FROM file_trash WHERE id = ? AND account_id = ?", id, c.AccountID).Scan(&tp)
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
		f, err := os.Open(safe)
		if err != nil {
			jsonError(w, 404, "file not found")
			return
		}
		defer f.Close()
		stat, _ := f.Stat()
		http.ServeContent(w, r, filepath.Base(safe), stat.ModTime(), f)
	})
}

func walkDirForSize(root string, total *int64) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if e.IsDir() {
			walkDirForSize(root+"/"+e.Name(), total)
		} else {
			*total += info.Size()
		}
	}
}

func addFileToZip(zw *zip.Writer, filePath, name string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}
	w, _ := zw.Create(name)
	w.Write(data)
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
		jsonResp(w, 200, dbs)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		var req struct {
			DBName string `json:"db_name"`
		}
		json.NewDecoder(r.Body).Decode(&req)
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
			jsonResp(w, 200, users)
		})

		r.Post("/", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil {
				jsonError(w, 401, "unauthorized")
				return
			}
			var req struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}
			json.NewDecoder(r.Body).Decode(&req)
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
			json.NewDecoder(r.Body).Decode(&req)
			db.Exec("UPDATE db_users SET password = ? WHERE id = ? AND account_id = ?", req.Password, id, c.AccountID)
			jsonResp(w, 200, map[string]string{"status": "password updated"})
		})

		r.Post("/{id}/assign", func(w http.ResponseWriter, r *http.Request) {
			_ = getClaims(r)
			uid, _ := strconv.Atoi(chi.URLParam(r, "id"))
			var req struct{ DBID int `json:"db_id"`; Privileges string `json:"privileges"` }
			json.NewDecoder(r.Body).Decode(&req)
			if req.Privileges == "" {
				req.Privileges = "ALL PRIVILEGES"
			}
			db.Exec(`INSERT OR REPLACE INTO db_user_assignments (user_id, db_id, privileges) VALUES (?, ?, ?)`,
				uid, req.DBID, req.Privileges)
			jsonResp(w, 200, map[string]string{"status": "assigned"})
		})

		r.Post("/{id}/unassign", func(w http.ResponseWriter, r *http.Request) {
			_ = getClaims(r)
			uid, _ := strconv.Atoi(chi.URLParam(r, "id"))
			var req struct{ DBID int `json:"db_id"` }
			json.NewDecoder(r.Body).Decode(&req)
			db.Exec("DELETE FROM db_user_assignments WHERE user_id = ? AND db_id = ?", uid, req.DBID)
			jsonResp(w, 200, map[string]string{"status": "unassigned"})
		})

		r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			id, _ := strconv.Atoi(chi.URLParam(r, "id"))
			db.Exec("DELETE FROM db_user_assignments WHERE user_id = ?", id)
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
			jsonResp(w, 200, remotes)
		})

		r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			id, _ := strconv.Atoi(chi.URLParam(r, "id"))
			var req struct{ RemoteAccess string `json:"remote_access"` }
			json.NewDecoder(r.Body).Decode(&req)
			db.Exec("UPDATE child_databases SET remote_access = ? WHERE id = ? AND account_id = ?",
				req.RemoteAccess, id, c.AccountID)
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
		rows, err := db.Query(`SELECT id, domain, type, COALESCE(doc_root,''), COALESCE(ssl_enabled,0), created_at
			FROM domains WHERE account_id = ? ORDER BY type, created_at DESC`, c.AccountID)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		domains := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id, ssl int
			var domain, typ, docRoot, created string
			rows.Scan(&id, &domain, &typ, &docRoot, &ssl, &created)
			domains = append(domains, map[string]interface{}{
				"id": id, "domain": domain, "type": typ,
				"doc_root": docRoot, "ssl_enabled": ssl == 1, "created_at": created,
			})
		}
		jsonResp(w, 200, domains)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		var req struct {
			Domain string `json:"domain"`
			Type   string `json:"type"`
		}
		json.NewDecoder(r.Body).Decode(&req)
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
			cleanDomain := strings.TrimPrefix(req.Domain, "www.")
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
		// Write Nginx vhost for the domain
		writeNginxVhost(req.Domain, docRoot, "")
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

		auditLog(db, r, "domain.delete", map[string]interface{}{"domain": delDomain, "account_id": c.AccountID})
		db.Exec("DELETE FROM domains WHERE id = ? AND account_id = ?", id, c.AccountID)

		// Only remove vhost if no other active account uses this domain
		var activeCount int
		db.QueryRow(`SELECT COUNT(*) FROM domains d
			JOIN accounts a ON a.id = d.account_id
			WHERE d.domain = ? AND a.status = 'active'`, delDomain).Scan(&activeCount)
		if activeCount == 0 {
			removeNginxVhost(delDomain)
			log.Printf("[DOMAINS] Removed vhost for %s (no more active owners)", delDomain)
		}
		reloadNginx()

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
	// Child: list own tickets + create
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		accountFilter := ""
		args := []interface{}{}
		if c.Scope == "child" {
			accountFilter = "WHERE t.account_id = ?"
			args = append(args, c.AccountID)
		}
		rows, err := db.Query(`SELECT t.id, t.account_id, COALESCE(a.username,'') as username,
			t.subject, t.status, t.created_at, t.updated_at,
			(SELECT COUNT(*) FROM ticket_messages WHERE ticket_id = t.id) as msg_count
			FROM support_tickets t LEFT JOIN accounts a ON t.account_id = a.id `+accountFilter+` ORDER BY t.updated_at DESC`, args...)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()
		tickets := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id, accID, msgCount int
			var subject, status, created, updated, username string
			rows.Scan(&id, &accID, &username, &subject, &status, &created, &updated, &msgCount)
			tickets = append(tickets, map[string]interface{}{
				"id": id, "account_id": accID, "username": username, "subject": subject, "status": status,
				"created_at": created, "updated_at": updated, "message_count": msgCount,
			})
		}
		jsonResp(w, 200, tickets)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
			if c == nil { jsonError(w, 401, "unauthorized"); return }
		var req struct{ Subject, Message string }
		json.NewDecoder(r.Body).Decode(&req)
		if req.Subject == "" || req.Message == "" {
			jsonError(w, 400, "subject and message required")
			return
		}
		result, _ := db.Exec(`INSERT INTO support_tickets (account_id, subject) VALUES (?, ?)`, c.AccountID, req.Subject)
		ticketID, _ := result.LastInsertId()
		db.Exec(`INSERT INTO ticket_messages (ticket_id, sender_type, sender_id, message) VALUES (?, 'user', ?, ?)`,
			ticketID, c.AccountID, req.Message)
		jsonResp(w, 201, map[string]interface{}{"id": ticketID, "status": "open"})
	})

	// Get ticket messages
	r.Get("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		// Verify ownership (child) or admin access
		var accID int
		db.QueryRow("SELECT account_id FROM support_tickets WHERE id = ?", id).Scan(&accID)
		if c.Scope == "child" && accID != c.AccountID {
			jsonError(w, 403, "access denied")
			return
		}
		rows, _ := db.Query(`SELECT m.id, m.sender_type, m.sender_id,
			CASE WHEN m.sender_type='user' THEN COALESCE(a.username,'(deleted)')
			     WHEN m.sender_type='admin' THEN COALESCE(ad.username,'(deleted)')
			     ELSE '' END as sender_name,
			m.message, m.created_at
			FROM ticket_messages m
			LEFT JOIN accounts a ON m.sender_type='user' AND m.sender_id=a.id
			LEFT JOIN admins ad ON m.sender_type='admin' AND m.sender_id=ad.id
			WHERE m.ticket_id = ? ORDER BY m.created_at ASC`, id)
		if rows == nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()
		msgs := make([]map[string]interface{}, 0)
		for rows.Next() {
			var mid, sid int
			var stype, sname, msg, created string
			rows.Scan(&mid, &stype, &sid, &sname, &msg, &created)
			msgs = append(msgs, map[string]interface{}{"id": mid, "sender_type": stype, "sender_id": sid, "sender_name": sname, "message": msg, "created_at": created})
		}
		jsonResp(w, 200, msgs)
	})

	// Reply to ticket
	r.Post("/{id}/reply", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct{ Message string `json:"message"` }
		json.NewDecoder(r.Body).Decode(&req)
		senderType := "user"
		senderID := c.AccountID
		if c.Scope == "parent" {
			senderType = "admin"
			senderID = c.UserID
		}
		db.Exec(`INSERT INTO ticket_messages (ticket_id, sender_type, sender_id, message) VALUES (?, ?, ?, ?)`,
			id, senderType, senderID, req.Message)
		db.Exec("UPDATE support_tickets SET status = 'replied', updated_at = datetime('now') WHERE id = ?", id)
		jsonResp(w, 200, map[string]string{"status": "replied"})
	})

	// Update ticket status (admin)
	r.Put("/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct{ Status string `json:"status"` }
		json.NewDecoder(r.Body).Decode(&req)
		db.Exec("UPDATE support_tickets SET status = ?, updated_at = datetime('now') WHERE id = ?", req.Status, id)
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	// Delete ticket (admin)
	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		db.Exec("DELETE FROM ticket_messages WHERE ticket_id = ?", id)
		db.Exec("DELETE FROM support_tickets WHERE id = ?", id)
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	// Delete individual message (admin)
	r.Delete("/messages/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		db.Exec("DELETE FROM ticket_messages WHERE id = ?", id)
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	// Edit message (admin)
	r.Put("/messages/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct{ Message string `json:"message"` }
		json.NewDecoder(r.Body).Decode(&req)
		db.Exec("UPDATE ticket_messages SET message = ? WHERE id = ?", req.Message, id)
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
	rows, _ := database.Query(`SELECT DISTINCT d.domain FROM domains d
		JOIN accounts a ON a.id = d.account_id WHERE a.status = 'active'`)
	if rows != nil {
		for rows.Next() {
			var d string
			rows.Scan(&d)
			activeDomains[d] = true
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
		accRows, _ := database.Query(`SELECT id, domain, home_dir FROM accounts`)
		if accRows != nil {
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
		jwtSecret = "openwebpanel-dev-secret-change-in-production"
	}
	jwtManager := auth.NewJWTManager(jwtSecret, 900, 604800)

	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:*", "http://127.0.0.1:*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, 200, map[string]string{"status": "ok", "version": "0.1.0"})
	})

	// Parent Panel API
	r.Route("/api/v1/auth", func(r chi.Router) {
		authRoutes(r, database, jwtManager)
	})

	r.Route("/api/v1/packages", func(r chi.Router) {
		r.Use(authMw(jwtManager))
		packageRoutes(r, database)
	})

	r.Route("/api/v1/accounts", func(r chi.Router) {
		r.Use(authMw(jwtManager))
		accountRoutes(r, database)
	})

	r.Route("/api/v1/stats", func(r chi.Router) {
		r.Use(authMw(jwtManager))
		statsRoutes(r, database)
	})

	r.Route("/api/v1/server", func(r chi.Router) {
		r.Use(authMw(jwtManager))
		serverRoutes(r)
	})

	r.Route("/api/v1/settings", func(r chi.Router) {
		r.Use(authMw(jwtManager))
		settingsRoutes(r, database)
	})

	// Child Panel API
	r.Route("/api/v1/child/auth", func(r chi.Router) {
		childAuthRoutes(r, database, jwtManager)
	})

	r.Route("/api/v1/child/account", func(r chi.Router) {
		r.Use(authMw(jwtManager), trackBandwidth(database))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			c := getClaims(r)
			if c == nil {
				jsonError(w, 401, "unauthorized")
				return
			}
			var username, domain, email, homeDir, status, pkgName string
			var diskMB, bandwidthMB, maxDB, maxEmail, maxFTP, maxDomains, maxSubdomains, diskUsed, bwUsed int
			var ip sql.NullString
			err := database.QueryRow(`SELECT a.username, a.domain, a.email, a.home_dir, a.status, a.ip_address,
				p.name, p.disk_mb, p.bandwidth_mb, p.max_db, p.max_email, p.max_ftp, p.max_domains, p.max_subdomains,
				COALESCE(a.disk_used_mb, 0), COALESCE(a.bandwidth_used_mb, 0)
				FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`,
				c.AccountID).Scan(&username, &domain, &email, &homeDir, &status, &ip,
				&pkgName, &diskMB, &bandwidthMB, &maxDB, &maxEmail, &maxFTP, &maxDomains, &maxSubdomains,
				&diskUsed, &bwUsed)
			if err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			var dbCount int
			database.QueryRow("SELECT COUNT(*) FROM child_databases WHERE account_id = ?", c.AccountID).Scan(&dbCount)

			sharedIP := ip.String
			if !ip.Valid || sharedIP == "" {
				sharedIP = getSharedIP()
			}

			jsonResp(w, 200, map[string]interface{}{
				"username":        username,
				"domain":          domain,
				"email":           email,
				"home_dir":        homeDir,
				"status":          status,
				"shared_ip":       sharedIP,
				"package_name":    pkgName,
				"disk_limit_mb":   diskMB,
				"disk_used_mb":    diskUsed,
				"bandwidth_limit_mb": bandwidthMB,
				"bandwidth_used_mb":  bwUsed,
				"max_databases":   maxDB,
				"databases_used":  dbCount,
				"max_email":       maxEmail,
				"max_ftp":         maxFTP,
				"max_domains":     maxDomains,
				"max_subdomains":  maxSubdomains,
			})
		})
	})

	r.Route("/api/v1/child/files", func(r chi.Router) {
		r.Use(authMw(jwtManager), trackBandwidth(database))
		childFileRoutes(r, database)
	})

	r.Route("/api/v1/child/databases", func(r chi.Router) {
		r.Use(authMw(jwtManager), trackBandwidth(database))
		childDbRoutes(r, database, jwtManager)
	})

	r.Route("/api/v1/child/domains", func(r chi.Router) {
		r.Use(authMw(jwtManager), trackBandwidth(database))
		childDomainRoutes(r, database)
	})

		// Bandwidth routes
		r.Route("/api/v1/bandwidth", func(r chi.Router) {
			r.Use(authMw(jwtManager))
			bandwidthRoutes(r, database)
		})

		// Submissions routes
		r.Route("/api/v1/submissions", func(r chi.Router) {
			r.Use(authMw(jwtManager))
			submissionRoutes(r, database)
		})

		// Admin email routes
		r.Route("/api/v1/emails", func(r chi.Router) {
			r.Use(authMw(jwtManager))
			adminEmailRoutes(r, database)
		})

		// Child routes
		r.Route("/api/v1/child/bandwidth", func(r chi.Router) {
			r.Use(authMw(jwtManager), trackBandwidth(database))
			childBandwidthRoutes(r, database)
		})

		r.Route("/api/v1/child/cms", func(r chi.Router) {
			r.Use(authMw(jwtManager), trackBandwidth(database))
			cmsRoutes(r, database)
		})

		r.Route("/api/v1/child/ssl", func(r chi.Router) {
			r.Use(authMw(jwtManager), trackBandwidth(database))
			certRoutes(r, database)
		})

		r.Route("/api/v1/child/emails", func(r chi.Router) {
			r.Use(authMw(jwtManager), trackBandwidth(database))
			childEmailRoutes(r, database)
		})

			// Cron job routes
			r.Route("/api/v1/child/cron", func(r chi.Router) {
				r.Use(authMw(jwtManager), trackBandwidth(database))
				childCronRoutes(r, database)
			})

			r.Route("/api/v1/child/backups", func(r chi.Router) {
				r.Use(authMw(jwtManager), trackBandwidth(database))
				childBackupRoutes(r, database)
			})

			r.Route("/api/v1/child/dns", func(r chi.Router) {
				r.Use(authMw(jwtManager), trackBandwidth(database))
				childDNSRoutes(r, database)
			})

			r.Route("/api/v1/child/ftp", func(r chi.Router) {
				r.Use(authMw(jwtManager), trackBandwidth(database))
				childFTPRoutes(r, database)
			})

			r.Route("/api/v1/child/ssh", func(r chi.Router) {
				r.Use(authMw(jwtManager), trackBandwidth(database))
				childSSHKeyRoutes(r, database)
			})

			r.Route("/api/v1/child/tokens", func(r chi.Router) {
				r.Use(authMw(jwtManager), trackBandwidth(database))
				childTokenRoutes(r, database)
			})

			r.Route("/api/v1/child/redirects", func(r chi.Router) {
				r.Use(authMw(jwtManager), trackBandwidth(database))
				redirectRoutes(r, database)
			})
			r.Route("/api/v1/child/hotlink", func(r chi.Router) {
				r.Use(authMw(jwtManager), trackBandwidth(database))
				hotlinkRoutes(r, database)
			})
			r.Route("/api/v1/child/stats", func(r chi.Router) {
				r.Use(authMw(jwtManager), trackBandwidth(database))
				childStatsRoutes(r, database)
			})


		// ACME HTTP-01 challenge handler (for Let's Encrypt)
		r.Get("/.well-known/acme-challenge/{token}", acmeChallengeHandler)

		// Ticket routes
		r.Route("/api/v1/tickets", func(r chi.Router) {
			r.Use(authMw(jwtManager))
			ticketRoutes(r, database)
		})

		r.Route("/api/v1/child/tickets", func(r chi.Router) {
			r.Use(authMw(jwtManager), trackBandwidth(database))
			ticketRoutes(r, database)
		})



	// phpMyAdmin reverse proxy — phpMyAdmin runs on localhost, never exposed directly.
	// All access goes through one-time tokens validated server-side.
	r.Route("/pma", func(r chi.Router) {
		r.Handle("/*", pmaProxyHandler(database))
	})

	// Serve frontend static files in production
	staticDir := os.Getenv("OWP_STATIC_DIR")
	if staticDir != "" {
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				jsonError(w, 404, "not found")
				return
			}
			// SPA fallback
			if _, err := os.Stat(staticDir + r.URL.Path); os.IsNotExist(err) {
				http.ServeFile(w, r, staticDir+"/index.html")
				return
			}
			http.FileServer(http.Dir(staticDir)).ServeHTTP(w, r)
		})
	}

	listenAddr := os.Getenv("OWP_LISTEN")
	if listenAddr == "" {
		listenAddr = ":9000"
	}

	// Start SMTP server for incoming mail (port 2525, iptables redirects 25->2525)
	go startSMTPServer(database)

	// Start cron job runner (evaluates every 30s)
	go startCronRunner(database)

	// Start SSL certificate renewal checker (daily)
	startCertRenewal(database)

	// Start Nginx vhost log bandwidth collector (every 5 min)
	startNginxBandwidthCollector(database)

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

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		os.Exit(0)
	}()

	log.Printf("OpenWebPanel Parent Daemon on %s (db: %s)", listenAddr, dbPath)
	log.Printf("Default login: admin / admin123")

	// Sync nginx vhosts for all domains for all active domains
	syncNginxVhosts(database)

	if err := http.ListenAndServe(listenAddr, r); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
