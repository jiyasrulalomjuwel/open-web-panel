package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/auth"
)

func childFTPRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		rows, err := db.Query(`SELECT id, username, domain, directory, quota_mb, status, created_at
			FROM ftp_accounts WHERE account_id = ? ORDER BY username`, c.AccountID)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		type FTPAccount struct {
			ID        int    `json:"id"`
			Username  string `json:"username"`
			Domain    string `json:"domain"`
			Directory string `json:"directory"`
			QuotaMB   int    `json:"quota_mb"`
			Status    string `json:"status"`
			CreatedAt string `json:"created_at"`
		}
		accounts := make([]FTPAccount, 0)
		for rows.Next() {
			var a FTPAccount
			rows.Scan(&a.ID, &a.Username, &a.Domain, &a.Directory, &a.QuotaMB, &a.Status, &a.CreatedAt)
			a.CreatedAt = strings.Replace(a.CreatedAt, "T", " ", 1)
			if len(a.CreatedAt) > 19 {
				a.CreatedAt = a.CreatedAt[:19]
			}
			accounts = append(accounts, a)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[FTP] rows iteration error: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		jsonResp(w, 200, accounts)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		if isRAMExceeded(db, c.AccountID) {
			jsonError(w, 429, "RAM limit exceeded. New FTP account creation is temporarily blocked. Please contact your hosting administrator to upgrade your resource allocation.")
			return
		}

		var req struct {
			Username  string `json:"username"`
			Password  string `json:"password"`
			Domain    string `json:"domain"`
			Directory string `json:"directory"`
			QuotaMB   int    `json:"quota_mb"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		if req.Username == "" || req.Password == "" || req.Domain == "" {
			jsonError(w, 400, "username, password, and domain are required")
			return
		}
		if len(req.Password) < 6 {
			jsonError(w, 400, "password must be at least 6 characters")
			return
		}
		if req.QuotaMB < 0 {
			jsonError(w, 400, "quota must be non-negative")
			return
		}
		if strings.Contains(req.Directory, "..") {
			jsonError(w, 400, "directory must not contain '..'")
			return
		}

		// Check duplicate username
		var exists int
		db.QueryRow("SELECT COUNT(*) FROM ftp_accounts WHERE account_id = ? AND username = ?", c.AccountID, req.Username).Scan(&exists)
		if exists > 0 {
			jsonError(w, 409, "an FTP account with this username already exists")
			return
		}

		// Enforce max FTP accounts (0 = unlimited)
		var current, maxFTP int
		db.QueryRow("SELECT COUNT(*) FROM ftp_accounts WHERE account_id = ?", c.AccountID).Scan(&current)
		db.QueryRow("SELECT max_ftp FROM packages p JOIN accounts a ON a.package_id = p.id WHERE a.id = ?", c.AccountID).Scan(&maxFTP)
		if maxFTP > 0 && current >= maxFTP {
			jsonError(w, 403, "FTP account limit reached")
			return
		}

		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			jsonError(w, 500, "failed to hash password")
			return
		}

		result, err := db.Exec(`INSERT INTO ftp_accounts (account_id, username, password_hash, domain, directory, quota_mb)
			VALUES (?, ?, ?, ?, ?, ?)`, c.AccountID, req.Username, hash, req.Domain, req.Directory, req.QuotaMB)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint") {
				jsonError(w, 409, "an FTP account with this username already exists")
				return
			}
			jsonError(w, 500, err.Error())
			return
		}
		id, _ := result.LastInsertId()
		auditLog(db, r, "ftp.create", map[string]interface{}{"id": id, "username": req.Username, "domain": req.Domain})
		jsonResp(w, 201, map[string]interface{}{"id": id, "status": "created"})
	})

	r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))

		var ownerID int
		err := db.QueryRow("SELECT account_id FROM ftp_accounts WHERE id = ?", id).Scan(&ownerID)
		if err != nil || ownerID != c.AccountID {
			jsonError(w, 404, "FTP account not found")
			return
		}

		var req struct {
			Password  string `json:"password"`
			Directory string `json:"directory"`
			QuotaMB   *int   `json:"quota_mb"`
			Status    string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}

		if req.Password != "" {
			if len(req.Password) < 6 {
				jsonError(w, 400, "password must be at least 6 characters")
				return
			}
			hash, err := auth.HashPassword(req.Password)
			if err != nil {
				jsonError(w, 500, "failed to hash password")
				return
			}
			db.Exec("UPDATE ftp_accounts SET password_hash = ? WHERE id = ?", hash, id)
		}
		if req.Directory != "" {
			if strings.Contains(req.Directory, "..") {
				jsonError(w, 400, "directory must not contain '..'")
				return
			}
			db.Exec("UPDATE ftp_accounts SET directory = ? WHERE id = ?", req.Directory, id)
		}
		if req.QuotaMB != nil {
			if *req.QuotaMB < 0 {
				jsonError(w, 400, "quota must be non-negative")
				return
			}
			db.Exec("UPDATE ftp_accounts SET quota_mb = ? WHERE id = ?", *req.QuotaMB, id)
		}
		if req.Status != "" {
			db.Exec("UPDATE ftp_accounts SET status = ? WHERE id = ?", req.Status, id)
		}

		auditLog(db, r, "ftp.update", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		result, err := db.Exec("DELETE FROM ftp_accounts WHERE id = ? AND account_id = ?", id, c.AccountID)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			jsonError(w, 404, "FTP account not found")
			return
		}
		auditLog(db, r, "ftp.delete", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}
