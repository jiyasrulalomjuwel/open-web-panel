package main

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/auth"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/validator"
)

func childFTPRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		rows, err := db.Query(`SELECT id, username, domain, directory, quota_mb, status, created_at
			FROM ftp_accounts WHERE account_id = ? ORDER BY username`, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
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
			if err := rows.Scan(&a.ID, &a.Username, &a.Domain, &a.Directory, &a.QuotaMB, &a.Status, &a.CreatedAt); err != nil {
				getLogger(r).Warnf("scan ftp row: %v", err)
				continue
			}
			a.CreatedAt = strings.Replace(a.CreatedAt, "T", " ", 1)
			if len(a.CreatedAt) > 19 {
				a.CreatedAt = a.CreatedAt[:19]
			}
			accounts = append(accounts, a)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("ftp rows iteration error: %v", err)
		}
		jsonResp(w, 200, accounts)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
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
			Username  string `json:"username"`
			Password  string `json:"password"`
			Domain    string `json:"domain"`
			Directory string `json:"directory"`
			QuotaMB   int    `json:"quota_mb"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}

		validation := validator.New().
			Add("username", validator.Required(), validator.MaxLength(32)).
			Add("password", validator.Required(), validator.MinLength(6)).
			Add("domain", validator.Required(), validator.MaxLength(255))
		data, _ := toMap(req)
		if err := validation.Validate(data); err != nil {
			writeAppError(w, r, err)
			return
		}

		if req.QuotaMB < 0 {
			writeAppError(w, r, apperrors.Validation("quota must be non-negative"))
			return
		}
		confinedDir, ok := confineFTPDir(db, c.AccountID, req.Directory)
		if !ok {
			writeAppError(w, r, apperrors.Validation("directory must be a relative path inside the account home"))
			return
		}
		req.Directory = confinedDir

		var exists int
		db.QueryRow("SELECT COUNT(*) FROM ftp_accounts WHERE account_id = ? AND username = ?", c.AccountID, req.Username).Scan(&exists)
		if exists > 0 {
			writeAppError(w, r, apperrors.Duplicate("FTP account", req.Username))
			return
		}

		if req.Domain != "" {
			var owned int
			db.QueryRow("SELECT COUNT(*) FROM domains WHERE account_id = ? AND LOWER(domain) = LOWER(?)", c.AccountID, req.Domain).Scan(&owned)
			if owned == 0 {
				writeAppError(w, r, apperrors.Validation("domain does not belong to this account"))
				return
			}
		}

		var current, maxFTP int
		db.QueryRow("SELECT COUNT(*) FROM ftp_accounts WHERE account_id = ?", c.AccountID).Scan(&current)
		db.QueryRow("SELECT max_ftp FROM packages p JOIN accounts a ON a.package_id = p.id WHERE a.id = ?", c.AccountID).Scan(&maxFTP)
		maxFTP = getAccountOverride(db, c.AccountID, "max_ftp", maxFTP)
		if maxFTP > 0 && current >= maxFTP {
			writeAppError(w, r, apperrors.QuotaExceeded("FTP accounts", int64(maxFTP)))
			return
		}

		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			writeAppError(w, r, apperrors.Internal("failed to hash password", err))
			return
		}

		res, err := db.Exec(`INSERT INTO ftp_accounts (account_id, username, password_hash, domain, directory, quota_mb)
			VALUES (?, ?, ?, ?, ?, ?)`, c.AccountID, req.Username, hash, req.Domain, req.Directory, req.QuotaMB)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint") {
				writeAppError(w, r, apperrors.Duplicate("FTP account", req.Username))
				return
			}
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		id, _ := res.LastInsertId()
		auditLog(db, r, "ftp.create", logging.Fields{"id": id, "username": req.Username, "domain": req.Domain})
		jsonResp(w, 201, map[string]interface{}{"id": id, "status": "created"})
	})

	r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
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

		var ownerID int
		if err := db.QueryRow("SELECT account_id FROM ftp_accounts WHERE id = ?", id).Scan(&ownerID); err != nil || ownerID != c.AccountID {
			writeAppError(w, r, apperrors.NotFound("FTP account", id))
			return
		}

		var req struct {
			Password  string `json:"password"`
			Directory string `json:"directory"`
			QuotaMB   *int   `json:"quota_mb"`
			Status    string `json:"status"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}

		if err := withDBRetry(func() error {
			if req.Password != "" {
				if len(req.Password) < 6 {
					return apperrors.Validation("password must be at least 6 characters")
				}
				hash, pErr := auth.HashPassword(req.Password)
				if pErr != nil {
					return apperrors.Internal("failed to hash password", pErr)
				}
				_, e := db.Exec("UPDATE ftp_accounts SET password_hash = ? WHERE id = ?", hash, id)
				if e != nil {
					return e
				}
			}
			if req.Directory != "" {
				confinedDir, ok := confineFTPDir(db, c.AccountID, req.Directory)
				if !ok {
					return apperrors.Validation("directory must be a relative path inside the account home")
				}
				_, e := db.Exec("UPDATE ftp_accounts SET directory = ? WHERE id = ?", confinedDir, id)
				if e != nil {
					return e
				}
			}
			if req.QuotaMB != nil {
				if *req.QuotaMB < 0 {
					return apperrors.Validation("quota must be non-negative")
				}
				_, e := db.Exec("UPDATE ftp_accounts SET quota_mb = ? WHERE id = ?", *req.QuotaMB, id)
				if e != nil {
					return e
				}
			}
			if req.Status != "" {
				_, e := db.Exec("UPDATE ftp_accounts SET status = ? WHERE id = ?", req.Status, id)
				if e != nil {
					return e
				}
			}
			return nil
		}); err != nil {
			if appErr, ok := err.(*apperrors.AppError); ok {
				writeAppError(w, r, appErr)
				return
			}
			writeAppError(w, r, apperrors.Database(err))
			return
		}

		auditLog(db, r, "ftp.update", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "updated"})
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
		res, execErr := db.Exec("DELETE FROM ftp_accounts WHERE id = ? AND account_id = ?", id, c.AccountID)
		if execErr != nil {
			writeAppError(w, r, apperrors.Database(execErr))
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeAppError(w, r, apperrors.NotFound("FTP account", id))
			return
		}
		auditLog(db, r, "ftp.delete", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}

// confineFTPDir restricts an FTP account's login directory to a relative path
// inside the account home. Absolute paths, `..` escapes and empty values are
// rejected so a tenant can never point FTP at another tenant's (or the host's)
// files. It returns the validated relative directory.
func confineFTPDir(db *sql.DB, accountID int, directory string) (string, bool) {
	dir := strings.TrimSpace(directory)
	if dir == "" {
		return "/", true
	}
	if strings.HasPrefix(dir, "/") || strings.HasPrefix(dir, "\\") {
		return "", false
	}
	if strings.Contains(dir, "..") {
		return "", false
	}
	if strings.ContainsAny(dir, "\x00\r\n") {
		return "", false
	}
	var homeDir string
	if err := db.QueryRow("SELECT COALESCE(home_dir,'') FROM accounts WHERE id = ?", accountID).Scan(&homeDir); err != nil || homeDir == "" {
		return "", false
	}
	abs := filepath.Join(homeDir, filepath.Clean(dir))
	if !isPathWithin(homeDir+"/", abs) {
		return "", false
	}
	return filepath.Clean(dir), true
}
