package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func childSSHKeyRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		rows, err := db.Query(`SELECT id, name, public_key, fingerprint, type, authorized, created_at
			FROM ssh_keys WHERE account_id = ? ORDER BY created_at DESC`, c.AccountID)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
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
			rows.Scan(&k.ID, &k.Name, &k.PublicKey, &k.Fingerprint, &k.Type, &authorized, &k.CreatedAt)
			k.Authorized = authorized == 1
			k.CreatedAt = strings.Replace(k.CreatedAt, "T", " ", 1)
			if len(k.CreatedAt) > 19 {
				k.CreatedAt = k.CreatedAt[:19]
			}
			keys = append(keys, k)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[SSH] rows iteration error: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		jsonResp(w, 200, keys)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		var req struct {
			Name      string `json:"name"`
			PublicKey string `json:"public_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		if req.Name == "" || req.PublicKey == "" {
			jsonError(w, 400, "name and public_key are required")
			return
		}

		// Derive fingerprint from key
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
			jsonError(w, 500, err.Error())
			return
		}
		id, _ := result.LastInsertId()
		auditLog(db, r, "ssh_key.create", map[string]interface{}{"id": id, "name": req.Name})

		jsonResp(w, 201, map[string]interface{}{"id": id, "fingerprint": fingerprint, "status": "created"})
	})

	r.Post("/{id}/authorize", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))

		var ownerID int
		var publicKey string
		err := db.QueryRow("SELECT account_id, public_key FROM ssh_keys WHERE id = ?", id).Scan(&ownerID, &publicKey)
		if err != nil || ownerID != c.AccountID {
			jsonError(w, 404, "key not found")
			return
		}

		// Write to account's ~/.ssh/authorized_keys
		var homeDir string
		db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", c.AccountID).Scan(&homeDir)
		if homeDir != "" {
			sshDir := homeDir + "/.ssh"
			authFile := sshDir + "/authorized_keys"
			os.MkdirAll(sshDir, 0700)
			data, _ := os.ReadFile(authFile)
			if !strings.Contains(string(data), publicKey) {
				f, _ := os.OpenFile(authFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
				f.WriteString(publicKey + "\n")
				f.Close()
			}
		}

		db.Exec("UPDATE ssh_keys SET authorized = 1 WHERE id = ?", id)
		auditLog(db, r, "ssh_key.authorize", map[string]interface{}{"id": id, "name": ""})
		jsonResp(w, 200, map[string]string{"status": "authorized"})
	})

	r.Post("/{id}/deauthorize", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))

		var ownerID int
		var publicKey string
		err := db.QueryRow("SELECT account_id, public_key FROM ssh_keys WHERE id = ?", id).Scan(&ownerID, &publicKey)
		if err != nil || ownerID != c.AccountID {
			jsonError(w, 404, "key not found")
			return
		}

		// Remove from authorized_keys
		var homeDir string
		db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", c.AccountID).Scan(&homeDir)
		if homeDir != "" {
			authFile := homeDir + "/.ssh/authorized_keys"
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
		auditLog(db, r, "ssh_key.deauthorize", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deauthorized"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))

		// Remove from authorized_keys if present
		var ownerID int
		var publicKey string
		err := db.QueryRow("SELECT account_id, public_key FROM ssh_keys WHERE id = ?", id).Scan(&ownerID, &publicKey)
		if err == nil && ownerID == c.AccountID && publicKey != "" {
			var homeDir string
			db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", c.AccountID).Scan(&homeDir)
			if homeDir != "" {
				authFile := homeDir + "/.ssh/authorized_keys"
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
			jsonError(w, 500, err.Error())
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			jsonError(w, 404, "key not found")
			return
		}
		auditLog(db, r, "ssh_key.delete", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}
