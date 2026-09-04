package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
)

func childSSHKeyRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		rows, err := db.Query(`SELECT id, name, public_key, fingerprint, type, authorized, created_at
			FROM ssh_keys WHERE account_id = ? ORDER BY created_at DESC`, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		defer rows.Close()

		type SSHKey struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			PublicKey   string `json:"public_key"`
			Fingerprint string `json:"fingerprint"`
			Type        string `json:"type"`
			Authorized  bool   `json:"authorized"`
			CreatedAt   string `json:"created_at"`
		}
		keys := make([]SSHKey, 0)
		for rows.Next() {
			var k SSHKey
			var authorized int
			if err := rows.Scan(&k.ID, &k.Name, &k.PublicKey, &k.Fingerprint, &k.Type, &authorized, &k.CreatedAt); err != nil {
				getLogger(r).Warnf("scan ssh key row: %v", err)
				continue
			}
			k.Authorized = authorized == 1
			k.CreatedAt = strings.Replace(k.CreatedAt, "T", " ", 1)
			if len(k.CreatedAt) > 19 {
				k.CreatedAt = k.CreatedAt[:19]
			}
			keys = append(keys, k)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("ssh key rows iteration error: %v", err)
		}
		jsonResp(w, 200, keys)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		var req struct {
			Name      string `json:"name"`
			PublicKey string `json:"public_key"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		if req.Name == "" || req.PublicKey == "" {
			writeAppError(w, r, apperrors.Validation("name and public_key are required"))
			return
		}
		if len(req.Name) > 64 {
			writeAppError(w, r, apperrors.Validation("name must not exceed 64 characters"))
			return
		}

		parts := strings.Fields(req.PublicKey)
		fingerprint := ""
		keyType := "ssh-rsa"
		if len(parts) >= 2 {
			keyType = parts[0]
			if decoded, err := base64.StdEncoding.DecodeString(parts[1]); err == nil {
				h := sha256.Sum256(decoded)
				fingerprint = "SHA256:" + base64.StdEncoding.EncodeToString(h[:])
			}
		}

		result, err := db.Exec(`INSERT INTO ssh_keys (account_id, name, public_key, fingerprint, type)
			VALUES (?, ?, ?, ?, ?)`, c.AccountID, req.Name, req.PublicKey, fingerprint, keyType)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		id, _ := result.LastInsertId()

		jsonResp(w, 201, map[string]interface{}{"id": id, "fingerprint": fingerprint, "status": "created"})
	})

	r.Post("/{id}/authorize", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		id, atoiErr := strconv.Atoi(chi.URLParam(r, "id"))
		if atoiErr != nil {
			writeAppError(w, r, apperrors.Validation("invalid id"))
			return
		}

		if !sshAccessForAccount(db, c.AccountID) {
			writeAppError(w, r, apperrors.Forbidden("SSH access is not enabled on your package"))
			return
		}

		var ownerID int
		var publicKey string
		err := db.QueryRow("SELECT account_id, public_key FROM ssh_keys WHERE id = ?", id).Scan(&ownerID, &publicKey)
		if err != nil || ownerID != c.AccountID {
			writeAppError(w, r, apperrors.NotFound("SSH key", id))
			return
		}

		var homeDir string
		db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", c.AccountID).Scan(&homeDir)
		installed := false
		if homeDir != "" {
			sshDir := filepath.Join(homeDir, ".ssh")
			authFile := filepath.Join(sshDir, "authorized_keys")
			if err := os.MkdirAll(sshDir, 0700); err == nil {
				data, readErr := os.ReadFile(authFile)
				if readErr != nil && !os.IsNotExist(readErr) {
					getLogger(r).Errorf("failed to read %s: %v", authFile, readErr)
				} else if strings.Contains(string(data), publicKey) {
					installed = true
				} else {
					f, openErr := os.OpenFile(authFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
					if openErr == nil {
						if _, writeErr := f.WriteString(publicKey + "\n"); writeErr == nil {
							installed = true
						} else {
							getLogger(r).Errorf("failed to write key to %s: %v", authFile, writeErr)
						}
						f.Close()
					}
				}
			}
		}
		if installed {
			db.Exec("UPDATE ssh_keys SET authorized = 1 WHERE id = ?", id)
		}
		auditLog(db, r, "ssh_key.authorize", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "authorized"})
	})

	r.Post("/{id}/deauthorize", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))

		var ownerID int
		var publicKey string
		err := db.QueryRow("SELECT account_id, public_key FROM ssh_keys WHERE id = ?", id).Scan(&ownerID, &publicKey)
		if err != nil || ownerID != c.AccountID {
			writeAppError(w, r, apperrors.NotFound("SSH key", id))
			return
		}

		var homeDir string
		db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", c.AccountID).Scan(&homeDir)
		if homeDir != "" {
			authFile := filepath.Join(homeDir, ".ssh", "authorized_keys")
			if data, err := os.ReadFile(authFile); err == nil {
				lines := strings.Split(string(data), "\n")
				filtered := make([]string, 0, len(lines))
				for _, line := range lines {
					if strings.TrimSpace(line) != "" && !strings.Contains(line, publicKey) {
						filtered = append(filtered, line)
					}
				}
				os.WriteFile(authFile, []byte(strings.Join(filtered, "\n")), 0600)
			}
		}

		db.Exec("UPDATE ssh_keys SET authorized = 0 WHERE id = ?", id)
		auditLog(db, r, "ssh_key.deauthorize", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deauthorized"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		id, atoiErr := strconv.Atoi(chi.URLParam(r, "id"))
		if atoiErr != nil {
			writeAppError(w, r, apperrors.Validation("invalid id"))
			return
		}

		var ownerID int
		var publicKey string
		err := db.QueryRow("SELECT account_id, public_key FROM ssh_keys WHERE id = ?", id).Scan(&ownerID, &publicKey)
		if err == nil && ownerID == c.AccountID && publicKey != "" {
			var homeDir string
			db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", c.AccountID).Scan(&homeDir)
			if homeDir != "" {
				authFile := filepath.Join(homeDir, ".ssh", "authorized_keys")
				if data, err := os.ReadFile(authFile); err == nil {
					lines := strings.Split(string(data), "\n")
					filtered := make([]string, 0, len(lines))
					for _, line := range lines {
						if strings.TrimSpace(line) != "" && !strings.Contains(line, publicKey) {
							filtered = append(filtered, line)
						}
					}
					os.WriteFile(authFile, []byte(strings.Join(filtered, "\n")), 0600)
				}
			}
		}

		result, err := db.Exec("DELETE FROM ssh_keys WHERE id = ? AND account_id = ?", id, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			writeAppError(w, r, apperrors.NotFound("SSH key", id))
			return
		}
		auditLog(db, r, "ssh_key.delete", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}

// sshAccessForAccount reports whether the account's package grants SSH access.
func sshAccessForAccount(db *sql.DB, accountID int) bool {
	var enabled int = 1
	db.QueryRow(`SELECT COALESCE(p.ssh_access, 1) FROM packages p
		JOIN accounts a ON a.package_id = p.id WHERE a.id = ?`, accountID).Scan(&enabled)
	// Per-account override: -1/unset follows the package, 0 forces off, 1 forces on
	return getSSHAccess(db, accountID, enabled) == 1
}
