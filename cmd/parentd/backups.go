package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func childBackupRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		rows, err := db.Query(`SELECT id, domain, type, file_path, file_size, status, COALESCE(backup_notes,''), created_at
			FROM backups WHERE account_id = ? ORDER BY created_at DESC`, c.AccountID)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		type Backup struct {
			ID       int    `json:"id"`
			Domain   string `json:"domain"`
			Type     string `json:"type"`
			FilePath string `json:"file_path"`
			FileSize int64  `json:"file_size"`
			Status   string `json:"status"`
			Notes    string `json:"notes"`
			Created  string `json:"created_at"`
		}
		backups := make([]Backup, 0)
		for rows.Next() {
			var b Backup
			rows.Scan(&b.ID, &b.Domain, &b.Type, &b.FilePath, &b.FileSize, &b.Status, &b.Notes, &b.Created)
			b.Created = strings.Replace(b.Created, "T", " ", 1)
			if len(b.Created) > 19 {
				b.Created = b.Created[:19]
			}
			backups = append(backups, b)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[BACKUPS] rows iteration error: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		jsonResp(w, 200, backups)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		var req struct {
			Type   string `json:"type"`
			Domain string `json:"domain"`
			Notes  string `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		if req.Type == "" {
			req.Type = "full"
		}
		if req.Domain == "" {
			// Fall back to account's primary domain
			var domain string
			db.QueryRow("SELECT domain FROM accounts WHERE id = ?", c.AccountID).Scan(&domain)
			req.Domain = domain
		}

		if isRAMExceeded(db, c.AccountID) {
			jsonError(w, 429, "RAM limit exceeded. New backups are temporarily blocked. Please contact your hosting administrator to upgrade your resource allocation.")
			return
		}

		var homeDir string
		err := db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", c.AccountID).Scan(&homeDir)
		if err != nil {
			jsonError(w, 500, "account not found")
			return
		}

		backupDir := getHomesBase() + "backups/" + strconv.Itoa(c.AccountID) + "/"
		os.MkdirAll(backupDir, 0755)

		safeDomain := strings.ReplaceAll(req.Domain, "..", "")
		safeDomain = strings.ReplaceAll(safeDomain, "/", "")
		safeDomain = strings.ReplaceAll(safeDomain, "\\", "")
		safeType := strings.ReplaceAll(req.Type, "..", "")
		safeType = strings.ReplaceAll(safeType, "/", "")
		safeType = strings.ReplaceAll(safeType, "\\", "")
		ts := time.Now().Format("20060102_150405")
		filename := safeDomain + "_" + safeType + "_" + ts + ".tar.gz"
		destPath := backupDir + filename

		result, err := db.Exec(`INSERT INTO backups (account_id, domain, type, file_path, status, backup_notes)
			VALUES (?, ?, ?, ?, 'running', ?)`, c.AccountID, req.Domain, req.Type, destPath, req.Notes)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		backupID, _ := result.LastInsertId()
		auditLog(db, r, "backup.create", map[string]interface{}{"id": backupID, "domain": req.Domain, "type": req.Type})

		go func(id int64, hDir, dest, backupType string) {
			var err error
			switch backupType {
			case "database":
				outFile, ferr := os.Create(dest)
				if ferr != nil {
					db.Exec("UPDATE backups SET status = 'failed' WHERE id = ?", id)
					return
				}
				cmd := exec.Command("mysqldump", "--all-databases")
				cmd.Stdout = outFile
				cmd.Stderr = os.Stderr
				if err = cmd.Run(); err != nil {
					outFile.Close()
					os.Remove(dest)
					db.Exec("UPDATE backups SET status = 'failed' WHERE id = ?", id)
					return
				}
				outFile.Close()
			default:
				parent := filepath.Dir(hDir)
				base := filepath.Base(hDir)
				cmd := exec.Command("tar", "-czf", dest, "-C", parent, base)
				if err = cmd.Run(); err != nil {
					db.Exec("UPDATE backups SET status = 'failed' WHERE id = ?", id)
					return
				}
			}
			var size int64
			if fi, err := os.Stat(dest); err == nil {
				size = fi.Size()
			}
			result, err := db.Exec("UPDATE backups SET status = 'completed', file_size = ? WHERE id = ?", size, id)
			if err != nil {
				log.Printf("[BACKUPS] Failed to update backup status: %v", err)
				return
			}
			affected, _ := result.RowsAffected()
			if affected == 0 {
				log.Printf("[BACKUPS] No backup row found to update for id %d", id)
			}
		}(backupID, homeDir, destPath, req.Type)

		jsonResp(w, 201, map[string]interface{}{"id": backupID, "status": "running", "file": filename})
	})

	r.Get("/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var filePath string
		err := db.QueryRow("SELECT file_path FROM backups WHERE id = ? AND account_id = ? AND status = 'completed'", id, c.AccountID).Scan(&filePath)
		if err != nil {
			jsonError(w, 404, "backup not found or not ready")
			return
		}
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(filePath)+"\"")
		w.Header().Set("Content-Type", "application/gzip")
		http.ServeFile(w, r, filePath)
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var filePath string
		err := db.QueryRow("SELECT file_path FROM backups WHERE id = ? AND account_id = ?", id, c.AccountID).Scan(&filePath)
		if err != nil {
			jsonError(w, 404, "backup not found")
			return
		}
		os.Remove(filePath)
		result, err := db.Exec("DELETE FROM backups WHERE id = ? AND account_id = ?", id, c.AccountID)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			jsonError(w, 404, "backup not found")
			return
		}
		auditLog(db, r, "backup.delete", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}
