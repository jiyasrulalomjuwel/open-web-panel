package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// ---------- CMS Installer routes ----------

func cmsRoutes(r chi.Router, db *sql.DB) {
	// Backfill tracking records for existing installed CMS installs that
	// were created before the panel tracked databases (one-time at startup).
	backfillCMSDatabaseTracking(db)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		rows, err := db.Query(`SELECT id, account_id, domain_id, domain, cms_type, version,
			install_path, install_url, COALESCE(db_name,''), COALESCE(db_user,''),
			COALESCE(admin_user,''), COALESCE(admin_email,''), admin_url, status, created_at
			FROM cms_installs WHERE account_id = ? ORDER BY created_at DESC`, c.AccountID)
		if err != nil {
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
			rows.Scan(&inst.ID, &inst.AccountID, &inst.DomainID, &inst.Domain, &inst.CmsType,
				&inst.Version, &inst.InstallPath, &inst.InstallURL, &inst.DbName, &inst.DbUser,
				&inst.AdminUser, &inst.AdminEmail, &inst.AdminURL, &inst.Status, &inst.CreatedAt)
			installs = append(installs, inst)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[CMS] rows iteration error: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		jsonResp(w, 200, installs)
	})

	// Get single install status (for polling)
	r.Get("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			jsonError(w, 400, "invalid id")
			return
		}

		var status, version, adminURL string
		err = db.QueryRow(`SELECT status, COALESCE(version,''), COALESCE(admin_url,'') FROM cms_installs WHERE id = ? AND account_id = ?`, id, c.AccountID).Scan(&status, &version, &adminURL)
		if err != nil {
			jsonError(w, 404, "not found")
			return
		}
		jsonResp(w, 200, map[string]string{
			"id":        idStr,
			"status":    status,
			"version":   version,
			"admin_url": adminURL,
		})
	})

	// Check if a domain has SSL issued (for protocol selection)
	r.Get("/ssl-check/{domain}", func(w http.ResponseWriter, r *http.Request) {
		domain := chi.URLParam(r, "domain")
		var count int
		db.QueryRow(`SELECT COUNT(*) FROM ssl_certs WHERE (domain = ? OR domain = ?) AND status = 'issued' AND expires_at > datetime('now')`,
			domain, "*."+domain).Scan(&count)
		jsonResp(w, 200, map[string]bool{"has_ssl": count > 0})
	})

	// List available WordPress versions (fetched from WP.org API)
	r.Get("/versions", func(w http.ResponseWriter, r *http.Request) {
		versions := fetchWordPressVersions()
		jsonResp(w, 200, versions)
	})

	r.Post("/install", func(w http.ResponseWriter, r *http.Request) {
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
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		if isRAMExceeded(db, c.AccountID) {
			jsonError(w, 429, "RAM limit exceeded. New CMS installations are temporarily blocked. Please contact your hosting administrator to upgrade your resource allocation.")
			return
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		if req.DomainID == 0 || req.Domain == "" || req.CmsType == "" {
			jsonError(w, 400, "domain_id, domain, and cms_type required")
			return
		}
		if req.CmsType != "wordpress" {
			jsonError(w, 400, "only wordpress is supported at this time")
			return
		}
		if req.Version == "" {
			req.Version = "latest"
		}
		if req.Protocol == "" {
			req.Protocol = "http"
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
		var docRoot string
		err := db.QueryRow("SELECT account_id, doc_root FROM domains WHERE id = ? AND account_id = ?", req.DomainID, c.AccountID).Scan(&accountID, &docRoot)
		if err != nil {
			jsonError(w, 404, "domain not found")
			return
		}

		// Look up username for unique prefix — usernames are unique in the
		// system, so prepending guarantees database names never collide.
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
			jsonError(w, 400, "invalid subdirectory")
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

		// Generate DB credentials — prefix with username to guarantee
		// uniqueness across the entire server (usernames are unique).
		dbName := username + "_wp_" + generatePassword(8)
		dbUser := username + "_wu_" + generatePassword(8)
		dbPassword := generatePassword(20)

		result, err := db.Exec(`INSERT INTO cms_installs (account_id, domain_id, domain, cms_type,
			version, install_path, install_url, db_name, db_user, db_password, admin_user,
			admin_password, admin_email, site_name, protocol, install_subdir, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'downloading')`,
			accountID, req.DomainID, req.Domain, req.CmsType, req.Version,
			installPath, installURL, dbName, dbUser, dbPassword,
			req.AdminUser, req.AdminPass, req.AdminEmail,
			req.SiteName, req.Protocol, subdir)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		installID, _ := result.LastInsertId()

		// Async install
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
			jsonError(w, 401, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			jsonError(w, 400, "id required")
			return
		}
		var installPath, dbName, dbUser string
		err := db.QueryRow("SELECT install_path, COALESCE(db_name,''), COALESCE(db_user,'') FROM cms_installs WHERE id = ? AND account_id = ?", id, c.AccountID).Scan(&installPath, &dbName, &dbUser)
		if err != nil {
			jsonError(w, 404, "not found")
			return
		}

		// Clean up panel database tracking records if they exist
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

		db.Exec("DELETE FROM cms_installs WHERE id = ? AND account_id = ?", id, c.AccountID)
		if installPath != "" {
			os.RemoveAll(installPath)
		}
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}

// WordPress version info from API
type wpVersionOffer struct {
	Version     string `json:"version"`
	Download    string `json:"download"`
	Description string `json:"description"`
}

var versionsCache []wpVersionOffer
var versionsCacheTime time.Time

func fetchWordPressVersions() []wpVersionOffer {
	// Return cached versions if fresh (< 5 min)
	if len(versionsCache) > 0 && time.Since(versionsCacheTime) < 5*time.Minute {
		return versionsCache
	}

	// Fetch from WordPress.org API
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

	// Fallback to hardcoded versions if API unavailable
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

func installWordPress(db *sql.DB, installID int64, accountID, domainID int, docRoot, domain, installPath, installURL, version, siteName, adminUser, adminPass, adminEmail string) {
	log.Printf("[CMS] Starting WordPress install for domain %s (installID=%d, version=%s)", domain, installID, version)

	// Build download URL based on version
	downloadURL := "https://wordpress.org/latest.tar.gz"
	if version != "" && version != "latest" {
		downloadURL = fmt.Sprintf("https://wordpress.org/wordpress-%s.tar.gz", version)
	}

	// Download WordPress
	homesBase := os.Getenv("OWP_HOMES_BASE")
	if homesBase == "" {
		homesBase = "./homes/"
	}
	tmpDir := homesBase + "tmp/"
	os.MkdirAll(tmpDir, 0755)
	tarPath := tmpDir + "wordpress.tar.gz"

	db.Exec("UPDATE cms_installs SET status = 'downloading' WHERE id = ?", installID)

	cmd := exec.Command("curl", "-sL", "-o", tarPath, "--connect-timeout", "30", "--max-time", "120",
		downloadURL)
	if output, err := cmd.CombinedOutput(); err != nil {
		db.Exec("UPDATE cms_installs SET status = 'failed' WHERE id = ?", installID)
		log.Printf("[CMS] Download failed for %s: %v, output: %s", downloadURL, err, string(output))
		return
	}

	db.Exec("UPDATE cms_installs SET status = 'extracting' WHERE id = ?", installID)

	// Create install directory and extract
	os.MkdirAll(installPath, 0755)
	extractCmd := exec.Command("tar", "-xzf", tarPath, "-C", installPath, "--strip-components=1")
	if output, err := extractCmd.CombinedOutput(); err != nil {
		db.Exec("UPDATE cms_installs SET status = 'failed' WHERE id = ?", installID)
		log.Printf("[CMS] Extract failed: %v, output: %s", err, string(output))
		return
	}

	db.Exec("UPDATE cms_installs SET status = 'configuring' WHERE id = ?", installID)

	// Generate salts
	salt := generatePassword(64)
	salt2 := generatePassword(64)
	salt3 := generatePassword(64)
	salt4 := generatePassword(64)
	salt5 := generatePassword(64)
	salt6 := generatePassword(64)
	salt7 := generatePassword(64)
	salt8 := generatePassword(64)

	// Read DB credentials from install record
	var dbName, dbUser, dbPassword string
	db.QueryRow("SELECT COALESCE(db_name,''), COALESCE(db_user,''), COALESCE(db_password,'') FROM cms_installs WHERE id = ?", installID).Scan(&dbName, &dbUser, &dbPassword)

	// Create wp-config.php
	wpConfigPath := installPath + "/wp-config.php"
	wpConfig := fmt.Sprintf(`<?php
/**
 * WordPress Configuration — Auto-generated by OpenWebPanel
 */

// ** Database settings **
define('DB_NAME', '%s');
define('DB_USER', '%s');
define('DB_PASSWORD', '%s');
define('DB_HOST', 'localhost');
define('DB_CHARSET', 'utf8');
define('DB_COLLATE', '');

// ** Authentication Unique Keys and Salts **
define('AUTH_KEY',         '%s');
define('SECURE_AUTH_KEY',  '%s');
define('LOGGED_IN_KEY',    '%s');
define('NONCE_KEY',        '%s');
define('AUTH_SALT',        '%s');
define('SECURE_AUTH_SALT', '%s');
define('LOGGED_IN_SALT',   '%s');
define('NONCE_SALT',       '%s');

// ** Table prefix **
$table_prefix = 'wp_';

// ** Site settings **
define('WP_HOME', '%s');
define('WP_SITEURL', '%s');
define('WP_CONTENT_URL', '%s/wp-content');

// ** Debug **
define('WP_DEBUG', false);

/* That's all, stop editing! Happy publishing. */

if ( !defined('ABSPATH') ) define('ABSPATH', __DIR__ . '/');
require_once ABSPATH . 'wp-settings.php';
`,
		dbName, dbUser, dbPassword, salt, salt2, salt3, salt4, salt5, salt6, salt7, salt8,
		installURL, installURL, installURL)

	os.WriteFile(wpConfigPath, []byte(wpConfig), 0644)

	db.Exec("UPDATE cms_installs SET status = 'configuring' WHERE id = ?", installID)

	// Create MySQL database and user
	dbCreateCmd := exec.Command("sudo", "mysql", "-e", fmt.Sprintf(
		"CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;"+
			"CREATE USER IF NOT EXISTS '%s'@'localhost' IDENTIFIED BY '%s';"+
			"GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';FLUSH PRIVILEGES;",
		dbName, dbUser, dbPassword, dbName, dbUser))
	if out, err := dbCreateCmd.CombinedOutput(); err != nil {
		log.Printf("[CMS] DB create failed for %s: %v, output: %s", installURL, err, string(out))
	} else {
		log.Printf("[CMS] MySQL database '%s' and user '%s' created successfully for %s", dbName, dbUser, installURL)
	}

	// Always track the database in panel tables — credentials were already
	// generated and written into wp-config.php, so the panel must reflect them
	// regardless of whether the sudo mysql call above succeeded (it may fail
	// due to sudoers config but the DB could already exist, or WP-CLI may
	// create it later).
	var dbID, userID int64

	dbResult, dbErr := db.Exec(`INSERT INTO child_databases (account_id, db_name, db_user, host)
		VALUES (?, ?, ?, 'localhost')`, accountID, dbName, dbUser)
	if dbErr != nil {
		log.Printf("[CMS] Failed to track database in panel: %v", dbErr)
	} else {
		dbID, _ = dbResult.LastInsertId()
	}

	userResult, userErr := db.Exec(`INSERT INTO db_users (account_id, username, password)
		VALUES (?, ?, ?)`, accountID, dbUser, dbPassword)
	if userErr != nil {
		log.Printf("[CMS] Failed to track database user in panel: %v", userErr)
	} else {
		userID, _ = userResult.LastInsertId()
	}

	if dbID > 0 && userID > 0 {
		_, assignErr := db.Exec(`INSERT OR REPLACE INTO db_user_assignments (user_id, db_id, privileges)
			VALUES (?, ?, 'ALL PRIVILEGES')`, userID, dbID)
		if assignErr != nil {
			log.Printf("[CMS] Failed to assign DB user to database: %v", assignErr)
		}
	}

	// Run WordPress installation via WP-CLI
	wpInstallCmd := exec.Command("wp", "core", "install",
		"--url="+installURL,
		"--title="+siteName,
		"--admin_user="+adminUser,
		"--admin_password="+adminPass,
		"--admin_email="+adminEmail,
		"--skip-email",
		"--allow-root")
	wpInstallCmd.Dir = installPath
	if out, err := wpInstallCmd.CombinedOutput(); err != nil {
		log.Printf("[CMS] WP-CLI install failed for %s: %v, output: %s", installURL, err, string(out))
	} else {
		log.Printf("[CMS] WP-CLI install succeeded for %s", installURL)
	}

	// Auto-issue SSL if protocol is https
	var protocol string
	db.QueryRow("SELECT COALESCE(protocol,'http') FROM cms_installs WHERE id = ?", installID).Scan(&protocol)
	if protocol == "https" {
		log.Printf("[CMS] HTTPS requested — issuing SSL for %s (installID=%d)", domain, installID)

		// Check if SSL cert already exists
		var existingID int
		err := db.QueryRow("SELECT id FROM ssl_certs WHERE domain = ? AND status = 'issued'", domain).Scan(&existingID)
		if err != nil {
			// Insert SSL cert record
			result, err := db.Exec(`INSERT INTO ssl_certs (account_id, domain_id, domain, status, auto_renew)
				VALUES (?, ?, ?, 'issuing', 1)`, accountID, domainID, domain)
			if err == nil {
				certID, _ := result.LastInsertId()
				// Issue Let's Encrypt cert (async — will update vhost and wp-config when done)
				go func(cid int64, dom string) {
					err := issueLetsEncryptCert(db, cid, dom, "www."+dom)
					if err != nil {
						log.Printf("[CMS] SSL issuance failed for %s (certID=%d): %v", dom, cid, err)
						db.Exec("UPDATE ssl_certs SET status = 'failed' WHERE id = ?", cid)
						return
					}
					log.Printf("[CMS] SSL issued for %s (certID=%d) — updating wp-config.php", dom, cid)

					// After SSL issued, update wp-config.php URLs to https
					var installURL string
					db.QueryRow("SELECT install_url FROM cms_installs WHERE id = ?", installID).Scan(&installURL)
					httpsURL := strings.Replace(installURL, "http://", "https://", 1)

					// Update wp-config.php
					var installPath string
					db.QueryRow("SELECT install_path FROM cms_installs WHERE id = ?", installID).Scan(&installPath)
					if installPath != "" {
						wpConfigPath := installPath + "/wp-config.php"
						if data, err := os.ReadFile(wpConfigPath); err == nil {
							content := string(data)
							content = strings.ReplaceAll(content, "http://"+dom, "https://"+dom)
							os.WriteFile(wpConfigPath, []byte(content), 0644)
							log.Printf("[CMS] wp-config.php updated to HTTPS for %s", dom)
						}

						// Update cms_installs record with https admin URL
						adminURL := httpsURL + "/wp-admin"
						db.Exec("UPDATE cms_installs SET admin_url=? WHERE id=?", adminURL, installID)
					}
				}(certID, domain)
			}
		}
	}

	// Generate admin URL
	adminURL := installURL + "/wp-admin"

	// Record the installation details
	db.Exec(`UPDATE cms_installs SET status='installed', version=COALESCE(NULLIF(?,''),'latest'),
		admin_url=?, admin_password=?, admin_email=?, site_name=?
		WHERE id=?`,
		version, adminURL, adminPass, adminEmail, siteName, installID)

	// Clean up
	os.Remove(tarPath)

	log.Printf("[CMS] WordPress %s installed at %s (installID=%d)", version, installURL, installID)
}

// backfillCMSDatabaseTracking creates panel tracking records for any existing
// CMS installs that were installed before the tracking feature was added.
// Without this, databases created by older CMS installs would never appear
// in the child panel's Databases section.
//
// IMPORTANT: must collect all rows FIRST (into a slice) and close the result
// set before issuing any INSERT/UPDATE queries, because SQLite is configured
// with MaxOpenConns(1). Querying while iterating rows would deadlock.
func backfillCMSDatabaseTracking(db *sql.DB) {
	rows, err := db.Query(`SELECT id, account_id, COALESCE(db_name,''), COALESCE(db_user,''), COALESCE(db_password,'')
		FROM cms_installs WHERE status = 'installed'`)
	if err != nil {
		return
	}

	// Drain ALL rows into a slice before doing any writes
	type installDB struct {
		id          int
		accountID   int
		dbName      string
		dbUser      string
		dbPassword  string
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
		log.Printf("[CMS] backfill rows iteration error: %v", err)
	}
	rows.Close()

	for _, rec := range pending {
		// Skip if already tracked
		var count int
		db.QueryRow("SELECT COUNT(*) FROM child_databases WHERE db_name = ?", rec.dbName).Scan(&count)
		if count > 0 {
			continue
		}

		log.Printf("[CMS] Backfilling DB tracking for install %d: %s / %s", rec.id, rec.dbName, rec.dbUser)

		dbResult, dbErr := db.Exec(`INSERT INTO child_databases (account_id, db_name, db_user, host)
			VALUES (?, ?, ?, 'localhost')`, rec.accountID, rec.dbName, rec.dbUser)
		if dbErr != nil {
			log.Printf("[CMS] Backfill: failed to insert child_databases for install %d: %v", rec.id, dbErr)
			continue
		}
		dbID, _ := dbResult.LastInsertId()

		userResult, userErr := db.Exec(`INSERT INTO db_users (account_id, username, password)
			VALUES (?, ?, ?)`, rec.accountID, rec.dbUser, rec.dbPassword)
		if userErr != nil {
			log.Printf("[CMS] Backfill: failed to insert db_users for install %d: %v", rec.id, userErr)
			continue
		}
		userID, _ := userResult.LastInsertId()

		if dbID > 0 && userID > 0 {
			_, assignErr := db.Exec(`INSERT OR REPLACE INTO db_user_assignments (user_id, db_id, privileges)
				VALUES (?, ?, 'ALL PRIVILEGES')`, userID, dbID)
			if assignErr != nil {
				log.Printf("[CMS] Backfill: failed to assign user for install %d: %v", rec.id, assignErr)
			}
		}

		log.Printf("[CMS] Backfilled DB tracking for install %d (dbID=%d, userID=%d)", rec.id, dbID, userID)
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
