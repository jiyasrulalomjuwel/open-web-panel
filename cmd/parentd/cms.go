package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/validator"
)

const cmsCmdTimeout = 10 * time.Minute

var (
	cmsInstallMu        sync.Mutex
	cmsInstallsInFlight = make(map[int64]struct{})
)

func cmsRoutes(r chi.Router, db *sql.DB) {
	backfillCMSDatabaseTracking(db)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		rows, err := db.Query(`SELECT id, account_id, domain_id, domain, cms_type, version,
			install_path, install_url, COALESCE(db_name,''), COALESCE(db_user,''),
			COALESCE(admin_user,''), COALESCE(admin_email,''), admin_url, status, created_at
			FROM cms_installs WHERE account_id = ? ORDER BY created_at DESC`, c.AccountID)
		if err != nil {
			getLogger(r).Errorf("cms list query: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		type CMSInstall struct {
			ID          int    `json:"id"`
			AccountID   int    `json:"account_id"`
			DomainID    int    `json:"domain_id"`
			Domain      string `json:"domain"`
			CmsType     string `json:"cms_type"`
			Version     string `json:"version"`
			InstallPath string `json:"install_path"`
			InstallURL  string `json:"install_url"`
			DbName      string `json:"db_name"`
			DbUser      string `json:"db_user"`
			AdminUser   string `json:"admin_user"`
			AdminEmail  string `json:"admin_email"`
			AdminURL    string `json:"admin_url"`
			Status      string `json:"status"`
			CreatedAt   string `json:"created_at"`
		}

		installs := make([]CMSInstall, 0)
		for rows.Next() {
			var inst CMSInstall
			if err := rows.Scan(&inst.ID, &inst.AccountID, &inst.DomainID, &inst.Domain, &inst.CmsType,
				&inst.Version, &inst.InstallPath, &inst.InstallURL, &inst.DbName, &inst.DbUser,
				&inst.AdminUser, &inst.AdminEmail, &inst.AdminURL, &inst.Status, &inst.CreatedAt); err != nil {
				getLogger(r).Warnf("scan cms row: %v", err)
				continue
			}
			installs = append(installs, inst)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("cms rows iteration error: %v", err)
		}
		jsonResp(w, 200, installs)
	})

	r.Get("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		id, err := paramInt(r, "id")
		if err != nil {
			writeAppError(w, r, err)
			return
		}

		var status, version, adminURL string
		err = db.QueryRow(`SELECT status, COALESCE(version,''), COALESCE(admin_url,'') FROM cms_installs WHERE id = ? AND account_id = ?`, id, c.AccountID).Scan(&status, &version, &adminURL)
		if err != nil {
			writeAppError(w, r, apperrors.NotFound("CMS install", id))
			return
		}
		jsonResp(w, 200, map[string]interface{}{
			"id":        id,
			"status":    status,
			"version":   version,
			"admin_url": adminURL,
		})
	})

	r.Get("/ssl-check/{domain}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		domain := chi.URLParam(r, "domain")
		var count int
		db.QueryRow(`SELECT COUNT(*) FROM ssl_certs s JOIN domains d ON d.id = s.domain_id
			WHERE d.account_id = ? AND LOWER(d.domain) = LOWER(?) AND s.status = 'issued' AND s.expires_at > datetime('now')`,
			c.AccountID, domain).Scan(&count)
		jsonResp(w, 200, map[string]bool{"has_ssl": count > 0})
	})

	r.Get("/versions", func(w http.ResponseWriter, r *http.Request) {
		versions := fetchWordPressVersions()
		jsonResp(w, 200, versions)
	})

	r.Post("/install", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}

		if isRAMExceeded(db, c.AccountID) {
			writeAppError(w, r, apperrors.QuotaExceeded("RAM", int64(getRAMLimit(db, c.AccountID))))
			return
		}

		var req struct {
			DomainID      int    `json:"domain_id"`
			Domain        string `json:"domain"`
			CmsType       string `json:"cms_type"`
			Version       string `json:"version"`
			Protocol      string `json:"protocol"`
			InstallSubdir string `json:"install_subdir"`
			SiteName      string `json:"site_name"`
			AdminUser     string `json:"admin_user"`
			AdminPass     string `json:"admin_password"`
			AdminEmail    string `json:"admin_email"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}

		validation := validator.New().
			Add("domain_id", validator.Required()).
			Add("cms_type", validator.Required())
		data, _ := toMap(req)
		if err := validation.Validate(data); err != nil {
			writeAppError(w, r, err)
			return
		}
		if req.Domain == "" {
			writeAppError(w, r, apperrors.Validation("domain is required"))
			return
		}
		if req.CmsType != "wordpress" {
			writeAppError(w, r, apperrors.Validation("only wordpress is supported at this time"))
			return
		}
		if req.Version == "" {
			req.Version = "latest"
		}
		if req.Protocol == "" {
			req.Protocol = "http"
		}
		if req.Protocol != "http" && req.Protocol != "https" {
			writeAppError(w, r, apperrors.Validation("protocol must be http or https"))
			return
		}
		if req.SiteName == "" {
			req.SiteName = req.Domain
		}
		if req.AdminPass == "" {
			req.AdminPass = generatePassword(16)
		}
		if req.AdminUser == "" {
			req.AdminUser = "admin_" + generatePassword(6)
		}

		var accountID int
		var docRoot, dbDomain string
		err := db.QueryRow("SELECT account_id, doc_root, domain FROM domains WHERE id = ? AND account_id = ?", req.DomainID, c.AccountID).Scan(&accountID, &docRoot, &dbDomain)
		if err != nil {
			writeAppError(w, r, apperrors.NotFound("domain", req.DomainID))
			return
		}
		// The install domain is taken from the owned domain record only. The
		// client-supplied domain string is ignored to prevent a child from
		// installing to, or requesting a certificate for, another tenant's domain.
		req.Domain = dbDomain

		var username string
		db.QueryRow("SELECT username FROM accounts WHERE id = ?", accountID).Scan(&username)
		if username == "" {
			username = fmt.Sprintf("u%d", accountID)
		}

		subdir := req.InstallSubdir
		if subdir != "" && subdir[0] == '/' {
			subdir = subdir[1:]
		}
		if strings.Contains(subdir, "..") {
			writeAppError(w, r, apperrors.Validation("invalid subdirectory"))
			return
		}
		installPath := docRoot
		if subdir != "" {
			installPath = docRoot + "/" + subdir
		}
		installURL := req.Protocol + "://" + req.Domain
		if subdir != "" {
			installURL += "/" + subdir
		}

		dbName := username + "_wp_" + generatePassword(8)
		dbUser := username + "_wu_" + generatePassword(8)
		dbPassword := generatePassword(20)

		var inProgress int
		if err := db.QueryRow(`SELECT COUNT(*) FROM cms_installs WHERE account_id = ? AND install_path = ?
			AND status IN ('downloading','configuring','extracting')`, accountID, installPath).Scan(&inProgress); err == nil && inProgress > 0 {
			writeAppError(w, r, apperrors.Conflict("an install is already in progress for this path"))
			return
		}

		result, err := db.Exec(`INSERT INTO cms_installs (account_id, domain_id, domain, cms_type,
			version, install_path, install_url, db_name, db_user, db_password, admin_user,
			admin_password, admin_email, site_name, protocol, install_subdir, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'downloading')`,
			accountID, req.DomainID, req.Domain, req.CmsType, req.Version,
			installPath, installURL, dbName, dbUser, dbPassword,
			req.AdminUser, req.AdminPass, req.AdminEmail,
			req.SiteName, req.Protocol, subdir)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		installID, _ := result.LastInsertId()

		cmsInstallMu.Lock()
		if _, dup := cmsInstallsInFlight[installID]; dup {
			cmsInstallMu.Unlock()
			writeAppError(w, r, apperrors.Conflict("an install is already in progress for this path"))
			return
		}
		cmsInstallsInFlight[installID] = struct{}{}
		cmsInstallMu.Unlock()

		go installWordPress(db, installID, accountID, req.DomainID, docRoot, req.Domain,
			installPath, installURL, req.Version, req.SiteName,
			req.AdminUser, req.AdminPass, req.AdminEmail)

		jsonResp(w, 200, map[string]interface{}{
			"id":          installID,
			"status":      "downloading",
			"url":         installURL,
			"admin_user":  req.AdminUser,
			"admin_email": req.AdminEmail,
		})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		id, err := paramInt(r, "id")
		if err != nil {
			writeAppError(w, r, err)
			return
		}

		var installPath, dbName, dbUser string
		err = db.QueryRow("SELECT install_path, COALESCE(db_name,''), COALESCE(db_user,'') FROM cms_installs WHERE id = ? AND account_id = ?", id, c.AccountID).Scan(&installPath, &dbName, &dbUser)
		if err != nil {
			writeAppError(w, r, apperrors.NotFound("CMS install", id))
			return
		}

		if dbName != "" {
			var dbID int
			if err := db.QueryRow("SELECT id FROM child_databases WHERE db_name = ?", dbName).Scan(&dbID); err == nil {
				db.Exec("DELETE FROM db_user_assignments WHERE db_id = ?", dbID)
				db.Exec("DELETE FROM child_databases WHERE id = ?", dbID)
			}
		}
		if dbUser != "" {
			db.Exec("DELETE FROM db_user_assignments WHERE user_id IN (SELECT id FROM db_users WHERE username = ?)", dbUser)
			db.Exec("DELETE FROM db_users WHERE username = ?", dbUser)
		}

		_, execErr := db.Exec("DELETE FROM cms_installs WHERE id = ? AND account_id = ?", id, c.AccountID)
		if execErr != nil {
			writeAppError(w, r, apperrors.Database(execErr))
			return
		}
		if installPath != "" {
			var homeDir string
			db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", c.AccountID).Scan(&homeDir)
			if homeDir != "" && isPathWithin(homeDir, installPath) {
				os.RemoveAll(installPath)
			} else {
				getLogger(r).Warnf("cms.delete refused to remove path outside home: %s", installPath)
			}
		}
		auditLog(db, r, "cms.delete", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}

type wpVersionOffer struct {
	Version     string `json:"version"`
	Download    string `json:"download"`
	Description string `json:"description"`
}

var versionsCache []wpVersionOffer
var versionsCacheTime time.Time

func fetchWordPressVersions() []wpVersionOffer {
	if len(versionsCache) > 0 && time.Since(versionsCacheTime) < 5*time.Minute {
		return versionsCache
	}

	type wpAPIResponse struct {
		Offers []struct {
			Version     string `json:"version"`
			Download    string `json:"download"`
			Description string `json:"description"`
		} `json:"offers"`
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.wordpress.org/core/version-check/1.7/")
	if err == nil && resp != nil {
		defer resp.Body.Close()
		var apiResp wpAPIResponse
		if err := json.NewDecoder(resp.Body).Decode(&apiResp); err == nil && len(apiResp.Offers) > 0 {
			versions := make([]wpVersionOffer, 0)
			for _, o := range apiResp.Offers {
				versions = append(versions, wpVersionOffer{
					Version:     o.Version,
					Download:    o.Download,
					Description: o.Description,
				})
			}
			versionsCache = versions
			versionsCacheTime = time.Now()
			return versions
		}
	}

	fallback := []wpVersionOffer{
		{Version: "6.9.4", Download: "https://wordpress.org/wordpress-6.9.4.tar.gz"},
		{Version: "6.9.3", Download: "https://wordpress.org/wordpress-6.9.3.tar.gz"},
		{Version: "6.9.2", Download: "https://wordpress.org/wordpress-6.9.2.tar.gz"},
		{Version: "6.9.1", Download: "https://wordpress.org/wordpress-6.9.1.tar.gz"},
		{Version: "6.9", Download: "https://wordpress.org/wordpress-6.9.tar.gz"},
		{Version: "6.8.1", Download: "https://wordpress.org/wordpress-6.8.1.tar.gz"},
		{Version: "6.8", Download: "https://wordpress.org/wordpress-6.8.tar.gz"},
		{Version: "6.7.2", Download: "https://wordpress.org/wordpress-6.7.2.tar.gz"},
		{Version: "6.7.1", Download: "https://wordpress.org/wordpress-6.7.1.tar.gz"},
		{Version: "6.6.2", Download: "https://wordpress.org/wordpress-6.6.2.tar.gz"},
	}
	versionsCache = fallback
	versionsCacheTime = time.Now()
	return fallback
}

func getContainerForAccount(db *sql.DB, accountID int) string {
	var name string
	db.QueryRow("SELECT container_name FROM docker_containers WHERE account_id = ? AND status = 'running' ORDER BY id DESC LIMIT 1", accountID).Scan(&name)
	if name != "" {
		return name
	}
	prefix := fmt.Sprintf("owp_%d_", accountID)
	out, _ := exec.Command("docker", "ps", "--format", "{{.Names}}").CombinedOutput()
	for _, n := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(n, prefix) {
			return n
		}
	}
	return ""
}

func containerPathFromHost(hostPath string) string {
	if strings.HasPrefix(hostPath, "/opt/openwebpanel/app/homes/") || strings.HasPrefix(hostPath, "/opt/openwebpanel/homes/") {
		idx := strings.Index(hostPath, "/homes/")
		if idx >= 0 {
			tail := hostPath[idx+len("/homes/"):]
			parts := strings.SplitN(tail, "/", 2)
			if len(parts) == 2 {
				return "/home/user/" + parts[1]
			}
		}
	}
	return hostPath
}

func runWPCLIInContainer(containerName, containerPath, installURL, siteName, adminUser, adminPass, adminEmail string) (string, error) {
	script := fmt.Sprintf(
		`if ! command -v wp >/dev/null 2>&1; then
			if ! command -v php >/dev/null 2>&1; then
				phpPath=$(find /usr/bin /usr/local/bin -name 'php*' -type f 2>/dev/null | head -1)
				[ -n "$phpPath" ] && ln -sf "$phpPath" /usr/local/bin/php
			fi
			if ! php -m 2>/dev/null | grep -q phar; then
				apk add php83-phar 2>/dev/null || true
			fi
			curl -sLo /usr/local/bin/wp https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar 2>/dev/null && chmod +x /usr/local/bin/wp
		fi
		wp core install \
			--url=%s \
			--title=%s \
			--admin_user=%s \
			--admin_password=%s \
			--admin_email=%s \
			--path=%s \
			--skip-email \
			--allow-root 2>&1`,
		shq(installURL), shq(siteName), shq(adminUser), shq(adminPass), shq(adminEmail), shq(containerPath))

	ctx, cancel := context.WithTimeout(context.Background(), cmsCmdTimeout)
	out, err := exec.CommandContext(ctx, "docker", "exec", containerName, "sh", "-c", script).CombinedOutput()
	cancel()
	return string(out), err
}

func shq(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, c := range s {
		if c == '\'' {
			b.WriteString(`'\''`)
		} else {
			b.WriteRune(c)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// phpe escapes a value for safe inclusion inside a single-quoted PHP string
// literal (e.g. in generated wp-config.php). It prevents single-quote breakout
// which could otherwise enable PHP code injection.
func phpe(s string) string {
	return strings.ReplaceAll(s, "'", `\'`)
}

func installWordPress(db *sql.DB, installID int64, accountID, domainID int, docRoot, domain, installPath, installURL, version, siteName, adminUser, adminPass, adminEmail string) {
	l := logging.NewDefault("cms")
	l.Infof("Starting WordPress install for domain %s (installID=%d, version=%s)", domain, installID, version)
	defer func() {
		cmsInstallMu.Lock()
		delete(cmsInstallsInFlight, installID)
		cmsInstallMu.Unlock()
	}()

	var dbName, dbUser, dbPassword string
	db.QueryRow("SELECT COALESCE(db_name,''), COALESCE(db_user,''), COALESCE(db_password,'') FROM cms_installs WHERE id = ?", installID).Scan(&dbName, &dbUser, &dbPassword)

	db.Exec("UPDATE cms_installs SET status = 'configuring' WHERE id = ?", installID)

	mysqlRootPass := os.Getenv("MYSQL_ROOT_PASSWORD")
	if mysqlRootPass == "" {
		db.Exec("UPDATE cms_installs SET status = 'failed' WHERE id = ?", installID)
		l.Errorf("MYSQL_ROOT_PASSWORD not set — cannot create database for %s", installURL)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), cmsCmdTimeout)
	dbCreateCmd := exec.CommandContext(ctx, "mysql", "-u", "root", "-e", fmt.Sprintf(
		"CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;"+
			"CREATE USER IF NOT EXISTS '%s'@'172.18.0.%%' IDENTIFIED BY '%s';"+
			"GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'172.18.0.%%';FLUSH PRIVILEGES;",
		dbName, dbUser, dbPassword, dbName, dbUser))
	dbCreateCmd.Env = append(os.Environ(), "MYSQL_PWD="+mysqlRootPass)
	out, err := dbCreateCmd.CombinedOutput()
	cancel()
	if err != nil {
		db.Exec("UPDATE cms_installs SET status = 'failed' WHERE id = ?", installID)
		l.Errorf("DB create failed for %s: %v, output: %s", installURL, err, string(out))
		return
	}
	l.Infof("MySQL database '%s' and user '%s' created successfully for %s", dbName, dbUser, installURL)

	var dbID, userID int64

	dbResult, dbErr := db.Exec(`INSERT INTO child_databases (account_id, db_name, db_user, host)
		VALUES (?, ?, ?, 'localhost')`, accountID, dbName, dbUser)
	if dbErr != nil {
		l.Warnf("Failed to track database in panel: %v", dbErr)
	} else {
		dbID, _ = dbResult.LastInsertId()
	}

	userResult, userErr := db.Exec(`INSERT INTO db_users (account_id, username, password)
		VALUES (?, ?, ?)`, accountID, dbUser, dbPassword)
	if userErr != nil {
		l.Warnf("Failed to track database user in panel: %v", userErr)
	} else {
		userID, _ = userResult.LastInsertId()
	}

	if dbID > 0 && userID > 0 {
		_, assignErr := db.Exec(`INSERT OR REPLACE INTO db_user_assignments (user_id, db_id, privileges)
			VALUES (?, ?, 'ALL PRIVILEGES')`, userID, dbID)
		if assignErr != nil {
			l.Warnf("Failed to assign DB user to database: %v", assignErr)
		}
	}

	downloadURL := "https://wordpress.org/latest.tar.gz"
	if version != "" && version != "latest" {
		downloadURL = fmt.Sprintf("https://wordpress.org/wordpress-%s.tar.gz", version)
	}

	homesBase := os.Getenv("OWP_HOMES_BASE")
	if homesBase == "" {
		homesBase = "./homes/"
	}
	tmpDir := homesBase + "tmp/"
	os.MkdirAll(tmpDir, 0755)
	tarPath := tmpDir + "wordpress.tar.gz"

	db.Exec("UPDATE cms_installs SET status = 'downloading' WHERE id = ?", installID)

	downloadCtx, downloadCancel := context.WithTimeout(context.Background(), cmsCmdTimeout)
	cmd := exec.CommandContext(downloadCtx, "curl", "-sL", "-o", tarPath, "--connect-timeout", "30", "--max-time", "120",
		downloadURL)
	output, err := cmd.CombinedOutput()
	downloadCancel()
	if err != nil {
		db.Exec("UPDATE cms_installs SET status = 'failed' WHERE id = ?", installID)
		l.Errorf("Download failed for %s: %v, output: %s", downloadURL, err, string(output))
		return
	}

	db.Exec("UPDATE cms_installs SET status = 'extracting' WHERE id = ?", installID)

	os.MkdirAll(installPath, 0755)
	extractCtx, extractCancel := context.WithTimeout(context.Background(), cmsCmdTimeout)
	extractCmd := exec.CommandContext(extractCtx, "tar", "-xzf", tarPath, "-C", installPath, "--strip-components=1")
	extractOutput, extractErr := extractCmd.CombinedOutput()
	extractCancel()
	if extractErr != nil {
		db.Exec("UPDATE cms_installs SET status = 'failed' WHERE id = ?", installID)
		l.Errorf("Extract failed: %v, output: %s", extractErr, string(extractOutput))
		return
	}

	salt := generatePassword(64)
	salt2 := generatePassword(64)
	salt3 := generatePassword(64)
	salt4 := generatePassword(64)
	salt5 := generatePassword(64)
	salt6 := generatePassword(64)
	salt7 := generatePassword(64)
	salt8 := generatePassword(64)

	wpConfigPath := installPath + "/wp-config.php"
	wpConfig := fmt.Sprintf(`<?php
define('DB_NAME', '%s');
define('DB_USER', '%s');
define('DB_PASSWORD', '%s');
define('DB_HOST', '172.18.0.1');
define('DB_CHARSET', 'utf8');
define('DB_COLLATE', '');
define('AUTH_KEY',         '%s');
define('SECURE_AUTH_KEY',  '%s');
define('LOGGED_IN_KEY',    '%s');
define('NONCE_KEY',        '%s');
define('AUTH_SALT',        '%s');
define('SECURE_AUTH_SALT', '%s');
define('LOGGED_IN_SALT',   '%s');
define('NONCE_SALT',       '%s');
$table_prefix = 'wp_';
define('WP_HOME', '%s');
define('WP_SITEURL', '%s');
define('WP_CONTENT_URL', '%s/wp-content');
define('WP_DEBUG', false);

if (isset($_SERVER['HTTP_X_FORWARDED_PROTO']) && $_SERVER['HTTP_X_FORWARDED_PROTO'] === 'https') {
    $_SERVER['HTTPS'] = 'on';
}

if ( !defined('ABSPATH') ) define('ABSPATH', __DIR__ . '/');
require_once ABSPATH . 'wp-settings.php';
`,
		phpe(dbName), phpe(dbUser), phpe(dbPassword), phpe(salt), phpe(salt2), phpe(salt3), phpe(salt4), phpe(salt5), phpe(salt6), phpe(salt7), phpe(salt8),
		phpe(installURL), phpe(installURL), phpe(installURL))

	os.WriteFile(wpConfigPath, []byte(wpConfig), 0600)

	containerName := getContainerForAccount(db, accountID)
	containerPath := containerPathFromHost(installPath)

	if containerName != "" {
		l.Infof("Running wp-cli install inside container %s (path=%s)", containerName, containerPath)
		wpOut, err := runWPCLIInContainer(containerName, containerPath, installURL, siteName, adminUser, adminPass, adminEmail)
		if err != nil || !strings.Contains(wpOut, "Success:") {
			l.Errorf("WP-CLI install in container %s failed: %v, output: %s", containerName, err, wpOut)
		} else {
			l.Infof("WP-CLI install succeeded in container %s", containerName)
		}
	} else {
		l.Warnf("No running container found for account %d, attempting host wp-cli", accountID)
		wpCtx, wpCancel := context.WithTimeout(context.Background(), cmsCmdTimeout)
		wpInstallCmd := exec.CommandContext(wpCtx, "wp", "core", "install",
			"--url="+installURL,
			"--title="+siteName,
			"--admin_user="+adminUser,
			"--admin_password="+adminPass,
			"--admin_email="+adminEmail,
			"--skip-email",
			"--allow-root")
		wpInstallCmd.Dir = installPath
		wpOut, wpErr := wpInstallCmd.CombinedOutput()
		wpCancel()
		if wpErr != nil {
			l.Errorf("WP-CLI install failed for %s: %v, output: %s", installURL, wpErr, string(wpOut))
		} else {
			l.Infof("WP-CLI install succeeded for %s", installURL)
		}
	}

	var protocol string
	db.QueryRow("SELECT COALESCE(protocol,'http') FROM cms_installs WHERE id = ?", installID).Scan(&protocol)
	if protocol == "https" {
		l.Infof("HTTPS requested — issuing SSL for %s (installID=%d)", domain, installID)

		var existingID int
		err := db.QueryRow("SELECT id FROM ssl_certs WHERE domain = ? AND status = 'issued'", domain).Scan(&existingID)
		if err != nil {
			result, err := db.Exec(`INSERT INTO ssl_certs (account_id, domain_id, domain, status, auto_renew)
				VALUES (?, ?, ?, 'issuing', 1)`, accountID, domainID, domain)
			if err == nil {
				certID, _ := result.LastInsertId()
				go func(cid int64, dom string) {
					sslLogger := logging.NewDefault("cms")
					wwwDomain := "www." + dom
					if !domainOwnedByAccount(db, accountID, wwwDomain) {
						wwwDomain = ""
					}
					names := []string{dom}
					if wwwDomain != "" {
						names = append(names, wwwDomain)
					}
					err := issueLetsEncryptCert(db, cid, names...)
					if err != nil {
						sslLogger.Errorf("SSL issuance failed for %s (certID=%d): %v", dom, cid, err)
						db.Exec("UPDATE ssl_certs SET status = 'failed' WHERE id = ?", cid)
						return
					}
					sslLogger.Infof("SSL issued for %s (certID=%d)", dom, cid)

					var installURL string
					db.QueryRow("SELECT install_url FROM cms_installs WHERE id = ?", installID).Scan(&installURL)
					httpsURL := strings.Replace(installURL, "http://", "https://", 1)

					var installPath string
					db.QueryRow("SELECT install_path FROM cms_installs WHERE id = ?", installID).Scan(&installPath)
					if installPath != "" {
						wpConfigPath := installPath + "/wp-config.php"
						if data, err := os.ReadFile(wpConfigPath); err == nil {
							content := string(data)
							content = strings.ReplaceAll(content, "http://"+dom, "https://"+dom)
							os.WriteFile(wpConfigPath, []byte(content), 0600)
							sslLogger.Infof("wp-config.php updated to HTTPS for %s", dom)
						}

						adminURL := httpsURL + "/wp-admin"
						db.Exec("UPDATE cms_installs SET admin_url=? WHERE id=?", adminURL, installID)
					}
				}(certID, domain)
			}
		}
	}

	adminURL := installURL + "/wp-admin"

	db.Exec(`UPDATE cms_installs SET status='installed', version=COALESCE(NULLIF(?,''),'latest'),
		admin_url=?, admin_password=?, admin_email=?, site_name=?
		WHERE id=?`,
		version, adminURL, adminPass, adminEmail, siteName, installID)

	os.Remove(tarPath)

	l.Infof("WordPress %s installed at %s (installID=%d)", version, installURL, installID)
}

func backfillCMSDatabaseTracking(db *sql.DB) {
	l := logging.NewDefault("cms")
	rows, err := db.Query(`SELECT id, account_id, COALESCE(db_name,''), COALESCE(db_user,''), COALESCE(db_password,'')
		FROM cms_installs WHERE status = 'installed'`)
	if err != nil {
		return
	}

	type installDB struct {
		id         int
		accountID  int
		dbName     string
		dbUser     string
		dbPassword string
	}
	var pending []installDB
	for rows.Next() {
		var rec installDB
		if err := rows.Scan(&rec.id, &rec.accountID, &rec.dbName, &rec.dbUser, &rec.dbPassword); err != nil {
			continue
		}
		if rec.dbName == "" || rec.dbUser == "" {
			continue
		}
		pending = append(pending, rec)
	}
	if err := rows.Err(); err != nil {
		l.Errorf("backfill rows iteration error: %v", err)
	}
	rows.Close()

	for _, rec := range pending {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM child_databases WHERE db_name = ?", rec.dbName).Scan(&count)
		if count > 0 {
			continue
		}

		l.Infof("Backfilling DB tracking for install %d: %s / %s", rec.id, rec.dbName, rec.dbUser)

		dbResult, dbErr := db.Exec(`INSERT INTO child_databases (account_id, db_name, db_user, host)
			VALUES (?, ?, ?, 'localhost')`, rec.accountID, rec.dbName, rec.dbUser)
		if dbErr != nil {
			l.Errorf("Backfill: failed to insert child_databases for install %d: %v", rec.id, dbErr)
			continue
		}
		dbID, _ := dbResult.LastInsertId()

		userResult, userErr := db.Exec(`INSERT INTO db_users (account_id, username, password)
			VALUES (?, ?, ?)`, rec.accountID, rec.dbUser, rec.dbPassword)
		if userErr != nil {
			l.Errorf("Backfill: failed to insert db_users for install %d: %v", rec.id, userErr)
			continue
		}
		userID, _ := userResult.LastInsertId()

		if dbID > 0 && userID > 0 {
			_, assignErr := db.Exec(`INSERT OR REPLACE INTO db_user_assignments (user_id, db_id, privileges)
				VALUES (?, ?, 'ALL PRIVILEGES')`, userID, dbID)
			if assignErr != nil {
				l.Errorf("Backfill: failed to assign user for install %d: %v", rec.id, assignErr)
			}
		}

		l.Infof("Backfilled DB tracking for install %d (dbID=%d, userID=%d)", rec.id, dbID, userID)
	}
}

func generatePassword(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[n.Int64()]
	}
	return string(b)
}
