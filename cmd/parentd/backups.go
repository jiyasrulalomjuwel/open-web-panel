package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
)

// Backup archive layout (always a .tar.gz):
//   home/...               account files (prefix present for files/full backups)
//   databases/<db>.sql     one mysqldump per owned database (database/full backups)

var mysqlUseRe = regexp.MustCompile(`(?i)(?:CREATE DATABASE|USE)\s+` + "`([a-zA-Z0-9_]+)`")

// forbiddenRestoreRe matches statements that must never be allowed through a
// restore pipeline, even for the account's own database dump.
var forbiddenRestoreRe = regexp.MustCompile(`(?i)\b(CREATE\s+USER|ALTER\s+USER|DROP\s+USER|GRANT\s|REVOKE\s|INSTALL\s+PLUGIN|UNINSTALL\s+PLUGIN|CREATE\s+ROLE|DROP\s+ROLE|SET\s+PASSWORD\s+|PASSWORD\s*=)\b`)

// validateSQLDump scans an entire dump file (streaming) and rejects it if it
// contains statements that could escalate privileges (CREATE USER / GRANT / etc.)
// or reference any database other than the one being restored. It returns a
// human-readable rejection reason, or an error if the file could not be read.
func validateSQLDump(r io.Reader, dbName string) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	inBlockComment := false
	for scanner.Scan() {
		line := scanner.Text()
		// Reject MySQL executable comments unconditionally; they are not real
		// comments and can carry privileged SQL. Check the raw line so they are
		// never dropped by the block-comment span handling below.
		if strings.Contains(line, "/*!") {
			return "dump contains executable comment /*!...*/", nil
		}
		// Handle block comments spanning lines.
		if inBlockComment {
			if idx := strings.Index(line, "*/"); idx >= 0 {
				line = line[idx+2:]
				inBlockComment = false
			} else {
				continue
			}
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "/*") {
			if strings.HasPrefix(trimmed, "/*") && !strings.Contains(trimmed, "*/") {
				inBlockComment = true
			}
			continue
		}
		if forbiddenRestoreRe.MatchString(trimmed) {
			return "dump contains forbidden privilege/account statement", nil
		}
		// Any USE/CREATE DATABASE must reference exactly the target database.
		if m := mysqlUseRe.FindStringSubmatch(trimmed); m != nil {
			if !strings.EqualFold(m[1], dbName) {
				return fmt.Sprintf("dump references database %q, expected %q", m[1], dbName), nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", nil
}

// backupExecSem caps how many mysqldump/tar backup and restore jobs run at once
// host-wide. Without it, many accounts hitting their schedules at the same time
// would fork an unbounded number of processes and exhaust the host.
var backupExecSem = make(chan struct{}, 4)

// backupFileAllowed reports whether filePath lives inside this account's own
// backup directory (~/backups). Defense in depth: the file path is read from
// the DB, but never trust a stored path for cross-account safety. The account's
// home directory is taken from the accounts table, never from the stored path.
// Any symlinks in the path are resolved first so a file that has been swapped
// for a link to another tenant's data is rejected.
func backupFileAllowed(db *sql.DB, accountID int, filePath string) bool {
	var homeDir string
	if err := db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", accountID).Scan(&homeDir); err != nil || homeDir == "" {
		return false
	}
	base := filepath.Join(homeDir, "backups") + "/"
	if filePath == "" || !isPathWithin(base, filePath) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		return false
	}
	return isPathWithin(base, resolved)
}

// recoverInterruptedBackups runs at startup. A crash/restart can leave backups
// stuck in pending/running/restoring; those rows would otherwise block every
// subsequent backup, restore and schedule for the account forever. Rows that
// were mid-backup are failed (their partial archive is removed), rows that were
// mid-restore are reverted to completed (the archive is still valid), and
// orphaned staging directories are removed.
func recoverInterruptedBackups(db *sql.DB) {
	rows, err := db.Query("SELECT id, account_id, file_path, status FROM backups WHERE status IN ('pending','running','restoring')")
	if err != nil {
		log.Printf("[BACKUP] recovery query: %v", err)
		return
	}
	var stuck []struct {
		id, accountID int64
		path, status  string
	}
	for rows.Next() {
		var s struct {
			id, accountID int64
			path, status  string
		}
		if rows.Scan(&s.id, &s.accountID, &s.path, &s.status) == nil {
			stuck = append(stuck, s)
		}
	}
	rows.Close()
	for _, s := range stuck {
		switch s.status {
		case "pending", "running":
			db.Exec("UPDATE backups SET status = 'failed', progress = 0, message = 'Interrupted by server restart' WHERE id = ?", s.id)
			if s.path != "" && backupFileAllowed(db, int(s.accountID), s.path) {
				os.Remove(s.path)
			}
		case "restoring":
			// A restore interrupted by a crash cannot be trusted as complete: a
			// partial database import or a half-deployed home would be reported
			// as success. Mark it failed so the operator can re-run the restore.
			db.Exec("UPDATE backups SET status = 'failed', progress = 0, message = 'Restore interrupted by server restart' WHERE id = ?", s.id)
		}
	}
	if n := len(stuck); n > 0 {
		log.Printf("[BACKUP] recovered %d interrupted backup(s) after restart", n)
	}

	// Remove orphaned staging directories left by a crash mid-backup/mid-restore.
	homesBase := getHomesBase()
	if entries, err := os.ReadDir(homesBase); err == nil {
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), ".owp_restore_") {
				os.RemoveAll(homesBase + e.Name())
			}
		}
	}
	// A crash in the middle of deployHome leaves the account home renamed to
	// <home>.owp_restore_bak with the live home missing. Rename it back.
	if accRows, err := db.Query("SELECT home_dir FROM accounts WHERE home_dir != ''"); err == nil {
		for accRows.Next() {
			var homeDir string
			if accRows.Scan(&homeDir) != nil || homeDir == "" {
				continue
			}
			if _, err := os.Stat(homeDir); err != nil {
				bak := homeDir + ".owp_restore_bak"
				if _, statErr := os.Stat(bak); statErr == nil {
					if err := os.Rename(bak, homeDir); err != nil {
						log.Printf("[BACKUP] recovery could not restore home %s: %v", homeDir, err)
					} else {
						log.Printf("[BACKUP] recovery restored home %s from a crashed restore", homeDir)
					}
				}
			}
		}
		accRows.Close()
	}

	// tmp_* staging dirs (backup creation) and any legacy restore_* dirs live
	// inside each account's ~/backups folder.
	if accRows, err := db.Query("SELECT home_dir FROM accounts WHERE home_dir != ''"); err == nil {
		for accRows.Next() {
			var homeDir string
			if accRows.Scan(&homeDir) != nil || homeDir == "" {
				continue
			}
			bakDir := filepath.Join(homeDir, "backups")
			if subs, err := os.ReadDir(bakDir); err == nil {
				for _, s := range subs {
					if s.IsDir() && (strings.HasPrefix(s.Name(), "tmp_") || strings.HasPrefix(s.Name(), "restore_")) {
						os.RemoveAll(bakDir + "/" + s.Name())
					}
				}
			}
		}
		accRows.Close()
	}
}

// migrateBackupLocations relocates archives stored by older versions under the
// host-level <homes>/backups/<accountID>/ directory into the account's own
// ~/backups folder, and updates the stored file_path. The old directories are
// removed once they are empty.
func migrateBackupLocations(db *sql.DB) {
	type row struct {
		id, accountID int64
		path, homeDir string
	}
	var rowsToMigrate []row
	homesBase := getHomesBase()
	oldRoot := homesBase + "backups/"
	rows, err := db.Query(`SELECT b.id, b.account_id, b.file_path, COALESCE(a.home_dir,'')
		FROM backups b JOIN accounts a ON a.id = b.account_id`)
	if err != nil {
		log.Printf("[BACKUP] migration query: %v", err)
		return
	}
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.accountID, &r.path, &r.homeDir) == nil && r.path != "" && r.homeDir != "" && strings.HasPrefix(r.path, oldRoot) {
			rowsToMigrate = append(rowsToMigrate, r)
		}
	}
	rows.Close()

	moved := 0
	for _, r := range rowsToMigrate {
		newDir := filepath.Join(r.homeDir, "backups")
		newPath := filepath.Join(newDir, filepath.Base(r.path))
		os.MkdirAll(newDir, 0755)
		if _, err := os.Stat(r.path); err == nil {
			if _, err := os.Stat(newPath); err != nil {
				if err := os.Rename(r.path, newPath); err != nil {
					log.Printf("[BACKUP] migration move failed for backup %d: %v", r.id, err)
				}
			}
		}
		if _, err := db.Exec("UPDATE backups SET file_path = ? WHERE id = ?", newPath, r.id); err != nil {
			log.Printf("[BACKUP] migration update failed for backup %d: %v", r.id, err)
		}
		moved++
	}
	if moved > 0 {
		log.Printf("[BACKUP] migrated %d backup archive(s) into ~/backups", moved)
	}

	// Remove now-empty legacy account directories under the old location.
	if entries, err := os.ReadDir(oldRoot); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			accDir := oldRoot + e.Name()
			if subs, err := os.ReadDir(accDir); err == nil && len(subs) == 0 {
				os.Remove(accDir)
			}
		}
	}
}

func childBackupRoutes(r chi.Router, db *sql.DB) {
	// GET / -> timeline of backups for the account
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		rows, err := db.Query(`SELECT id, domain, type, file_path, file_size, status,
			COALESCE(backup_notes,''), COALESCE(progress,0), COALESCE(trigger_type,'manual'),
			COALESCE(message,''), created_at
			FROM backups WHERE account_id = ? ORDER BY created_at DESC`, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		defer rows.Close()

		type Backup struct {
			ID       int64  `json:"id"`
			Domain   string `json:"domain"`
			Type     string `json:"type"`
			FileSize int64  `json:"file_size"`
			Status   string `json:"status"`
			Notes    string `json:"notes"`
			Progress int    `json:"progress"`
			Trigger  string `json:"trigger"`
			Message  string `json:"message"`
			Created  string `json:"created_at"`
		}
		backups := make([]Backup, 0)
		for rows.Next() {
			var b Backup
			var filePath string
			if err := rows.Scan(&b.ID, &b.Domain, &b.Type, &filePath, &b.FileSize, &b.Status,
				&b.Notes, &b.Progress, &b.Trigger, &b.Message, &b.Created); err != nil {
				getLogger(r).Warnf("scan backup row: %v", err)
				continue
			}
			// Backups are ordinary files inside ~/backups, so the archive may have
			// been deleted from the File Manager. Reconcile: a completed backup
			// whose file is gone cannot be downloaded or restored.
			if b.Status == "completed" && filePath != "" {
				if _, err := os.Stat(filePath); err != nil {
					b.Status = "failed"
					b.Progress = 0
					b.Message = "Backup file was deleted from ~/backups"
					db.Exec("UPDATE backups SET status = 'failed', progress = 0, message = 'Backup file was deleted from ~/backups' WHERE id = ?", b.ID)
				}
			}
			b.Created = strings.Replace(b.Created, "T", " ", 1)
			if len(b.Created) > 19 {
				b.Created = b.Created[:19]
			}
			backups = append(backups, b)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("backup rows iteration error: %v", err)
		}
		jsonResp(w, 200, backups)
	})

	// GET /summary -> aggregate stats for the account
	r.Get("/summary", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		var total int
		var totalSize int64
		var lastCreated, lastStatus string
		var scheduleCount, enabledSchedule int
		// Only completed backups represent actual backup files stored in ~/backups.
		db.QueryRow("SELECT COUNT(*), COALESCE(SUM(file_size),0) FROM backups WHERE account_id = ? AND status = 'completed'", c.AccountID).Scan(&total, &totalSize)
		db.QueryRow("SELECT COALESCE(created_at,''), COALESCE(status,'') FROM backups WHERE account_id = ? AND status = 'completed' ORDER BY created_at DESC LIMIT 1").Scan(&lastCreated, &lastStatus)
		db.QueryRow("SELECT COUNT(*) FROM backup_schedules WHERE account_id = ?", c.AccountID).Scan(&scheduleCount)
		db.QueryRow("SELECT COUNT(*) FROM backup_schedules WHERE account_id = ? AND enabled = 1", c.AccountID).Scan(&enabledSchedule)
		jsonResp(w, 200, map[string]interface{}{
			"total_backups":     total,
			"total_size":        totalSize,
			"last_backup_at":    lastCreated,
			"last_status":       lastStatus,
			"schedule_count":    scheduleCount,
			"enabled_schedules": enabledSchedule,
		})
	})

	// POST / -> create a manual backup (runs in background with live progress)
	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		var req struct {
			Type  string `json:"type"`
			Notes string `json:"notes"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		if req.Type == "" {
			req.Type = "full"
		}
		if req.Type != "full" && req.Type != "files" && req.Type != "database" {
			writeAppError(w, r, apperrors.Validation("backup type must be one of: full, files, database"))
			return
		}
		if len(req.Notes) > 500 {
			writeAppError(w, r, apperrors.Validation("notes too long (max 500 chars)"))
			return
		}

		if !backupEnabledFor(db, c.AccountID) {
			writeAppError(w, r, apperrors.Forbidden("backup feature is not enabled for your account"))
			return
		}
		if isRAMExceeded(db, c.AccountID) {
			writeAppError(w, r, apperrors.QuotaExceeded("RAM", int64(getRAMLimit(db, c.AccountID))))
			return
		}

		var domain, homeDir string
		if err := db.QueryRow("SELECT COALESCE(domain,''), COALESCE(home_dir,'') FROM accounts WHERE id = ?", c.AccountID).Scan(&domain, &homeDir); err != nil {
			writeAppError(w, r, apperrors.NotFound("account", c.AccountID))
			return
		}
		if homeDir == "" {
			writeAppError(w, r, apperrors.Internal("account home directory is not configured", nil))
			return
		}

		// Guard against concurrent manual backups for the same account. The
		// INSERT is conditional so two racing requests cannot both create a
		// backup (the row starts out as 'pending' until the goroutine flips it
		// to 'running', so the guard must cover pending rows too).
		res, err := db.Exec(`INSERT INTO backups (account_id, domain, type, file_path, status, backup_notes, progress, trigger_type, message)
			SELECT ?, ?, ?, '', 'pending', ?, 0, 'manual', 'Queued'
			WHERE NOT EXISTS (
				SELECT 1 FROM backups WHERE account_id = ? AND status IN ('pending','running','restoring')
			)`, c.AccountID, domain, req.Type, req.Notes, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			writeAppError(w, r, apperrors.Conflict("a backup or restore is already in progress for your account"))
			return
		}
		id, _ := res.LastInsertId()
		auditLog(db, r, "backup.create", logging.Fields{"id": id, "type": req.Type})

		go runBackup(db, id, c.AccountID, req.Type, domain, homeDir, req.Notes)
		jsonResp(w, 201, map[string]interface{}{"id": id, "status": "running", "type": req.Type})
	})

	// GET /{id}/status -> live progress for the UI progress bars
	r.Get("/{id}/status", func(w http.ResponseWriter, r *http.Request) {
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
		var status string
		var progress int
		var message string
		if err := db.QueryRow(`SELECT status, COALESCE(progress,0), COALESCE(message,'') FROM backups WHERE id = ? AND account_id = ?`,
			id, c.AccountID).Scan(&status, &progress, &message); err != nil {
			writeAppError(w, r, apperrors.NotFound("backup", id))
			return
		}
		jsonResp(w, 200, map[string]interface{}{"id": id, "status": status, "progress": progress, "message": message})
	})

	// POST /{id}/download -> stream the backup archive to the user
	r.Post("/{id}/download", func(w http.ResponseWriter, r *http.Request) {
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
		var filePath string
		if err := db.QueryRow("SELECT file_path FROM backups WHERE id = ? AND account_id = ? AND status = 'completed'", id, c.AccountID).Scan(&filePath); err != nil {
			writeAppError(w, r, apperrors.NotFound("backup", id))
			return
		}
		if !backupFileAllowed(db, c.AccountID, filePath) {
			writeAppError(w, r, apperrors.NotFound("backup", id))
			return
		}
		if _, err := os.Stat(filePath); err != nil {
			writeAppError(w, r, apperrors.NotFound("backup file", id))
			return
		}
		auditLog(db, r, "backup.download", logging.Fields{"id": id})
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(filePath)+"\"")
		w.Header().Set("Content-Type", "application/gzip")
		http.ServeFile(w, r, filePath)
	})

	// POST /{id}/restore -> restore files and/or databases from a completed backup
	r.Post("/{id}/restore", func(w http.ResponseWriter, r *http.Request) {
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
		var req struct {
			RestoreFiles     bool `json:"restore_files"`
			RestoreDatabase  bool `json:"restore_database"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		if !req.RestoreFiles && !req.RestoreDatabase {
			writeAppError(w, r, apperrors.Validation("select at least one restore target (files and/or databases)"))
			return
		}

		var filePath string
		var backupType string
		if err := db.QueryRow("SELECT file_path, type FROM backups WHERE id = ? AND account_id = ? AND status = 'completed'", id, c.AccountID).Scan(&filePath, &backupType); err != nil {
			writeAppError(w, r, apperrors.NotFound("backup", id))
			return
		}
		if req.RestoreFiles && backupType == "database" {
			writeAppError(w, r, apperrors.Validation("this backup contains no files to restore"))
			return
		}
		if req.RestoreDatabase && backupType == "files" {
			writeAppError(w, r, apperrors.Validation("this backup contains no databases to restore"))
			return
		}
		if !backupFileAllowed(db, c.AccountID, filePath) {
			writeAppError(w, r, apperrors.NotFound("backup file", id))
			return
		}
		if _, err := os.Stat(filePath); err != nil {
			writeAppError(w, r, apperrors.NotFound("backup file", id))
			return
		}

		var homeDir string
		if err := db.QueryRow("SELECT COALESCE(home_dir,'') FROM accounts WHERE id = ?", c.AccountID).Scan(&homeDir); err != nil || homeDir == "" {
			writeAppError(w, r, apperrors.Internal("account home directory is not configured", err))
			return
		}

		// Atomically claim the backup for restore. This blocks concurrent
		// restores of the same backup as well as restores while a backup is
		// pending/running for the account.
		res, err := db.Exec(`UPDATE backups SET status = 'restoring', progress = 0, message = 'Preparing restore…'
			WHERE id = ? AND account_id = ? AND status = 'completed'
			AND NOT EXISTS (SELECT 1 FROM backups WHERE account_id = ? AND status IN ('pending','running','restoring') AND id != ?)`,
			id, c.AccountID, c.AccountID, id)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		claimed, _ := res.RowsAffected()
		if claimed == 0 {
			writeAppError(w, r, apperrors.Conflict("a backup or restore is already in progress for your account"))
			return
		}
		auditLog(db, r, "backup.restore", logging.Fields{"id": id, "files": req.RestoreFiles, "database": req.RestoreDatabase})
		go runRestore(db, int64(id), c.AccountID, filePath, homeDir, req.RestoreFiles, req.RestoreDatabase)
		jsonResp(w, 200, map[string]interface{}{"id": id, "status": "restoring"})
	})

	// DELETE /{id} -> remove a backup record and its archive file
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
		var filePath string
		var status string
		if err := db.QueryRow("SELECT file_path, status FROM backups WHERE id = ? AND account_id = ?", id, c.AccountID).Scan(&filePath, &status); err != nil {
			writeAppError(w, r, apperrors.NotFound("backup", id))
			return
		}
		if status == "pending" || status == "running" || status == "restoring" {
			writeAppError(w, r, apperrors.Conflict("cannot delete a backup while it is in progress"))
			return
		}
		if !backupFileAllowed(db, c.AccountID, filePath) {
			writeAppError(w, r, apperrors.NotFound("backup", id))
			return
		}
		// Delete the DB row first so a partial failure cannot leave a dead
		// timeline entry; the archive is removed afterwards, best-effort.
		res, execErr := db.Exec("DELETE FROM backups WHERE id = ? AND account_id = ?", id, c.AccountID)
		if execErr != nil {
			writeAppError(w, r, apperrors.Database(execErr))
			return
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			writeAppError(w, r, apperrors.NotFound("backup", id))
			return
		}
		if filePath != "" {
			if err := os.Remove(filePath); err != nil {
				log.Printf("[BACKUP] warning: archive %s could not be removed after delete: %v", filePath, err)
			}
		}
		auditLog(db, r, "backup.delete", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	// ---------- Schedules (automatic backups) ----------

	r.Get("/schedules", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		rows, err := db.Query(`SELECT id, name, backup_type, frequency, time,
			day_of_week, day_of_month, retention, enabled, COALESCE(next_run_at,''),
			COALESCE(last_run_at,''), COALESCE(last_status,''), created_at
			FROM backup_schedules WHERE account_id = ? ORDER BY created_at ASC`, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		defer rows.Close()
		schedules := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id, dow, dom, ret, enabled int
			var name, btype, freq, t, nextRun, lastRun, lastStatus, created string
			if err := rows.Scan(&id, &name, &btype, &freq, &t, &dow, &dom, &ret, &enabled, &nextRun, &lastRun, &lastStatus, &created); err != nil {
				getLogger(r).Warnf("scan schedule row: %v", err)
				continue
			}
			schedules = append(schedules, map[string]interface{}{
				"id": id, "name": name, "backup_type": btype, "frequency": freq, "time": t,
				"day_of_week": dow, "day_of_month": dom, "retention": ret, "enabled": enabled == 1,
				"next_run_at": nextRun, "last_run_at": lastRun, "last_status": lastStatus, "created_at": created,
			})
		}
		jsonResp(w, 200, schedules)
	})

	r.Post("/schedules", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		var req struct {
			Name        string `json:"name"`
			BackupType  string `json:"backup_type"`
			Frequency   string `json:"frequency"`
			Time        string `json:"time"`
			DayOfWeek   int    `json:"day_of_week"`
			DayOfMonth  int    `json:"day_of_month"`
			Retention   int    `json:"retention"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		if err := validateSchedule(req.Name, req.BackupType, req.Frequency, req.Time, req.DayOfWeek, req.DayOfMonth, req.Retention); err != nil {
			writeAppError(w, r, err)
			return
		}
		if !backupEnabledFor(db, c.AccountID) {
			writeAppError(w, r, apperrors.Forbidden("backup feature is not enabled for your account"))
			return
		}
		nextRun := computeNextRun(req.Frequency, req.Time, req.DayOfWeek, req.DayOfMonth)
		// Conditional INSERT enforces the 5-schedule quota atomically so two
		// racing requests cannot both slip past a pre-checked count.
		res, err := db.Exec(`INSERT INTO backup_schedules
			(account_id, name, backup_type, frequency, time, day_of_week, day_of_month, retention, enabled, next_run_at)
			SELECT ?, ?, ?, ?, ?, ?, ?, ?, 1, ?
			WHERE (SELECT COUNT(*) FROM backup_schedules WHERE account_id = ?) < 5`,
			c.AccountID, req.Name, req.BackupType, req.Frequency, req.Time, req.DayOfWeek, req.DayOfMonth, req.Retention, nextRun, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			writeAppError(w, r, apperrors.QuotaExceeded("backup schedules", 5))
			return
		}
		id, _ := res.LastInsertId()
		auditLog(db, r, "backup.schedule.create", logging.Fields{"id": id, "name": req.Name})
		jsonResp(w, 201, map[string]interface{}{"id": id, "status": "created", "next_run_at": nextRun})
	})

	r.Put("/schedules/{id}", func(w http.ResponseWriter, r *http.Request) {
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
		if err := db.QueryRow("SELECT account_id FROM backup_schedules WHERE id = ?", id).Scan(&ownerID); err != nil || ownerID != c.AccountID {
			writeAppError(w, r, apperrors.NotFound("backup schedule", id))
			return
		}
		var req struct {
			Name        string `json:"name"`
			BackupType  string `json:"backup_type"`
			Frequency   string `json:"frequency"`
			Time        string `json:"time"`
			DayOfWeek   *int   `json:"day_of_week"`
			DayOfMonth  *int   `json:"day_of_month"`
			Retention   *int   `json:"retention"`
			Enabled     *bool  `json:"enabled"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		// Fetch current values so partial updates only validate the fields sent.
		var curName, curType, curFreq, curTime string
		var curDow, curDom, curRet int
		if err := db.QueryRow("SELECT name, backup_type, frequency, time, day_of_week, day_of_month, retention FROM backup_schedules WHERE id = ?", id).
			Scan(&curName, &curType, &curFreq, &curTime, &curDow, &curDom, &curRet); err != nil {
			writeAppError(w, r, apperrors.NotFound("backup schedule", id))
			return
		}
		if req.Name == "" {
			req.Name = curName
		}
		if req.BackupType == "" {
			req.BackupType = curType
		}
		if req.Frequency == "" {
			req.Frequency = curFreq
		}
		if req.Time == "" {
			req.Time = curTime
		}
		if req.DayOfWeek == nil {
			req.DayOfWeek = &curDow
		}
		if req.DayOfMonth == nil {
			req.DayOfMonth = &curDom
		}
		if req.Retention == nil {
			req.Retention = &curRet
		}
		dow := *req.DayOfWeek
		dom := *req.DayOfMonth
		ret := *req.Retention
		if err := validateSchedule(req.Name, req.BackupType, req.Frequency, req.Time, dow, dom, ret); err != nil {
			writeAppError(w, r, err)
			return
		}
		nextRun := computeNextRun(req.Frequency, req.Time, dow, dom)
		if req.Enabled != nil {
			enabledVal := 0
			if *req.Enabled {
				enabledVal = 1
			}
			_, err = db.Exec(`UPDATE backup_schedules SET name = ?, backup_type = ?, frequency = ?, time = ?,
				day_of_week = ?, day_of_month = ?, retention = ?, enabled = ?, next_run_at = ? WHERE id = ?`,
				req.Name, req.BackupType, req.Frequency, req.Time, dow, dom, ret, enabledVal, nextRun, id)
		} else {
			_, err = db.Exec(`UPDATE backup_schedules SET name = ?, backup_type = ?, frequency = ?, time = ?,
				day_of_week = ?, day_of_month = ?, retention = ?, next_run_at = ? WHERE id = ?`,
				req.Name, req.BackupType, req.Frequency, req.Time, dow, dom, ret, nextRun, id)
		}
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		auditLog(db, r, "backup.schedule.update", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "updated", "next_run_at": nextRun})
	})

	r.Delete("/schedules/{id}", func(w http.ResponseWriter, r *http.Request) {
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
		res, err := db.Exec("DELETE FROM backup_schedules WHERE id = ? AND account_id = ?", id, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeAppError(w, r, apperrors.NotFound("backup schedule", id))
			return
		}
		auditLog(db, r, "backup.schedule.delete", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}

// ---------- Helpers ----------

func backupEnabledFor(db *sql.DB, accountID int) bool {
	// Per-account override wins; otherwise follow the package default.
	// featureEnabledFor lives in main.go (same package).
	return featureEnabledFor(db, accountID, "backups")
}

func validateSchedule(name, backupType, frequency, t string, dayOfWeek, dayOfMonth, retention int) *apperrors.AppError {
	if name == "" {
		return apperrors.Validation("schedule name is required")
	}
	if len(name) > 60 {
		return apperrors.Validation("schedule name too long (max 60 chars)")
	}
	if backupType != "full" && backupType != "files" && backupType != "database" {
		return apperrors.Validation("backup type must be one of: full, files, database")
	}
	if frequency != "daily" && frequency != "weekly" && frequency != "monthly" {
		return apperrors.Validation("frequency must be one of: daily, weekly, monthly")
	}
	if !regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`).MatchString(t) {
		return apperrors.Validation("time must be in 24h HH:MM format")
	}
	if dayOfWeek < 0 || dayOfWeek > 6 {
		return apperrors.Validation("day of week must be between 0 (Sunday) and 6 (Saturday)")
	}
	if dayOfMonth < 1 || dayOfMonth > 28 {
		return apperrors.Validation("day of month must be between 1 and 28")
	}
	if retention < 1 || retention > 90 {
		return apperrors.Validation("retention must be between 1 and 90 backups")
	}
	return nil
}

// computeNextRun returns the next scheduled run time in UTC (YYYY-MM-DD HH:MM:SS).
func computeNextRun(frequency, t string, dayOfWeek, dayOfMonth int) string {
	parts := strings.SplitN(t, ":", 2)
	hour, _ := strconv.Atoi(parts[0])
	min, _ := strconv.Atoi(parts[1])
	now := time.Now().UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	switch frequency {
	case "daily":
		// already computed
	case "weekly":
		for int(next.Weekday()) != dayOfWeek {
			next = next.AddDate(0, 0, 1)
		}
	case "monthly":
		next = time.Date(next.Year(), next.Month(), dayOfMonth, hour, min, 0, 0, time.UTC)
		if !next.After(now) {
			next = time.Date(next.Year(), next.Month()+1, dayOfMonth, hour, min, 0, 0, time.UTC)
		}
	}
	return next.Format("2006-01-02 15:04:05")
}

// scheduleIsDue reports whether a schedule should run now (within the tick window).
func scheduleIsDue(nextRun string, now time.Time) bool {
	if nextRun == "" {
		return false
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", nextRun, time.UTC)
	if err != nil {
		return false
	}
	return !t.After(now)
}

// startBackupScheduler checks backup schedules every minute and runs due backups.
func startBackupScheduler(db *sql.DB) {
	go func() {
		l := logging.NewDefault("backup")
		ticker := time.NewTicker(45 * time.Second)
		for range ticker.C {
			now := time.Now().UTC()
			rows, err := db.Query(`SELECT s.id, s.account_id, s.name, s.backup_type, s.retention,
				s.frequency, s.time, s.day_of_week, s.day_of_month, s.next_run_at,
				COALESCE(a.domain,''), COALESCE(a.home_dir,'')
				FROM backup_schedules s JOIN accounts a ON a.id = s.account_id
				WHERE s.enabled = 1 AND a.status = 'active'`)
			if err != nil {
				l.Errorf("backup scheduler query: %v", err)
				continue
			}
			var due []struct {
				id, accountID, retention int
				backupType, name          string
				frequency, schedTime      string
				dayOfWeek, dayOfMonth     int
				domain, homeDir           string
			}
			for rows.Next() {
				var d struct {
					id, accountID, retention int
					backupType, name          string
					frequency, schedTime      string
					dayOfWeek, dayOfMonth     int
					domain, homeDir           string
				}
				var nextRun string
				if err := rows.Scan(&d.id, &d.accountID, &d.name, &d.backupType, &d.retention,
					&d.frequency, &d.schedTime, &d.dayOfWeek, &d.dayOfMonth, &nextRun,
					&d.domain, &d.homeDir); err != nil {
					l.Warnf("scan schedule: %v", err)
					continue
				}
				if scheduleIsDue(nextRun, now) {
					due = append(due, d)
				}
			}
			rows.Close()

			for _, d := range due {
				if !backupEnabledFor(db, d.accountID) {
					continue
				}
				if d.homeDir == "" {
					continue
				}
				nextRun := computeNextRun(d.frequency, d.schedTime, d.dayOfWeek, d.dayOfMonth)

				// Atomic claim: the INSERT only succeeds when the account has no
				// backup/restore in flight, so a manual backup and a due schedule
				// can never double-launch for the same account.
				res, err := db.Exec(`INSERT INTO backups (account_id, domain, type, file_path, status, backup_notes, progress, trigger_type, message)
					SELECT ?, ?, ?, '', 'pending', ?, 0, 'schedule', 'Queued (scheduled)'
					WHERE NOT EXISTS (
						SELECT 1 FROM backups WHERE account_id = ? AND status IN ('pending','running','restoring')
					)`,
					d.accountID, d.domain, d.backupType, d.name, d.accountID)
				if err != nil {
					l.Errorf("scheduler insert: %v", err)
					continue
				}
				// Only touch last_run/last_status after the queue decision so a
				// skipped run is never recorded as 'running' with no backup.
				if affected, _ := res.RowsAffected(); affected == 0 {
					db.Exec("UPDATE backup_schedules SET next_run_at = ?, last_run_at = datetime('now'), last_status = 'skipped' WHERE id = ?", nextRun, d.id)
					continue
				}
				db.Exec("UPDATE backup_schedules SET next_run_at = ?, last_run_at = datetime('now'), last_status = 'queued' WHERE id = ?", nextRun, d.id)
				bID, _ := res.LastInsertId()
				l.Infof("Running scheduled backup %q for account %d (backup id %d)", d.name, d.accountID, bID)
				schedName := d.name
				go func(scheduleID int, backupID int64, accountID, retention int, backupType, domain, homeDir, schedName string) {
					runBackup(db, backupID, accountID, backupType, domain, homeDir, "scheduled: "+schedName)
					// Retention: keep only the newest N completed backups for this account.
					pruneBackups(db, accountID, retention)
					db.Exec("UPDATE backup_schedules SET last_status = (SELECT status FROM backups WHERE id = ?) WHERE id = ?", backupID, scheduleID)
				}(d.id, bID, d.accountID, d.retention, d.backupType, d.domain, d.homeDir, schedName)
			}
		}
	}()
}

// pruneBackups deletes older completed backups beyond the retention limit.
func pruneBackups(db *sql.DB, accountID, retention int) {
	if retention <= 0 {
		return
	}
	rows, err := db.Query(`SELECT id, file_path FROM backups
		WHERE account_id = ? AND status = 'completed'
		ORDER BY created_at DESC LIMIT -1 OFFSET ?`, accountID, retention)
	if err != nil {
		return
	}
	defer rows.Close()
	var toDelete []struct {
		id   int64
		path string
	}
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err == nil {
			toDelete = append(toDelete, struct {
				id   int64
				path string
			}{id, path})
		}
	}
	rows.Close()
	for _, b := range toDelete {
		if backupFileAllowed(db, accountID, b.path) {
			os.Remove(b.path)
		}
		db.Exec("DELETE FROM backups WHERE id = ?", b.id)
	}
}

func setBackupState(db *sql.DB, id int64, status string, progress int, message string) {
	db.Exec("UPDATE backups SET status = ?, progress = ?, message = ? WHERE id = ?", status, progress, message, id)
}

// diskFreeBytes returns the free space on the filesystem containing path.
func diskFreeBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// ---------- Backup execution ----------

func runBackup(db *sql.DB, id int64, accountID int, backupType, domain, homeDir, notes string) {
	defer func() {
		if r := recover(); r != nil {
			setBackupState(db, id, "failed", 0, fmt.Sprintf("backup error: %v", r))
		}
	}()
	backupExecSem <- struct{}{}
	defer func() { <-backupExecSem }()
	setBackupState(db, id, "running", 5, "Preparing backup…")

	// Backups are ordinary files stored inside the account's own home, in the
	// ~/backups folder, so they count toward the account's normal disk usage and
	// are managed like any other file in the File Manager. There is no separate
	// backup storage quota.
	if homeDir == "" {
		setBackupState(db, id, "failed", 0, "account home directory is not configured")
		return
	}
	backupDir := filepath.Join(homeDir, "backups") + "/"
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		setBackupState(db, id, "failed", 0, "cannot create backup directory")
		return
	}

	// Preflight: refuse to run if the disk has very little free space, so an
	// unbounded database dump can't fill the host and take every tenant down.
	if avail, perr := diskFreeBytes(backupDir); perr == nil {
		const minFreeForBackup = 512 << 20 // 512 MiB
		if avail < minFreeForBackup {
			setBackupState(db, id, "failed", 0, "not enough free disk space to create a backup")
			return
		}
	}
	ts := time.Now().Format("20060102_150405")
	safeDom := sanitizeDomain(domain)
	if safeDom == "" {
		safeDom = "account" + strconv.Itoa(accountID)
	}
	dest := backupDir + safeDom + "_" + backupType + "_" + ts + ".tar.gz"
	db.Exec("UPDATE backups SET file_path = ? WHERE id = ?", dest, id)

	includeFiles := backupType == "files" || backupType == "full"
	includeDB := backupType == "database" || backupType == "full"

	tmpRoot := backupDir + "tmp_" + strconv.FormatInt(id, 10)
	os.RemoveAll(tmpRoot)
	defer os.RemoveAll(tmpRoot)

	// Stage database dumps first.
	var dumpFiles []string
	var dumpSize int64
	if includeDB {
		setBackupState(db, id, "running", 8, "Dumping databases…")
		dumpDir := tmpRoot + "/databases"
		if err := os.MkdirAll(dumpDir, 0755); err != nil {
			setBackupState(db, id, "failed", 0, "cannot create staging directory")
			return
		}

		var names []string
		rows, qerr := db.Query("SELECT db_name FROM child_databases WHERE account_id = ?", accountID)
		if qerr == nil {
			for rows.Next() {
				var n string
				if rows.Scan(&n) == nil && validMysqlIdent(n) {
					names = append(names, n)
				}
			}
			rows.Close()
		}
		pass := os.Getenv("MYSQL_ROOT_PASSWORD")
		for i, n := range names {
			outPath := filepath.Join(dumpDir, n+".sql")
			// The root password is passed via MYSQL_PWD rather than -p so it never
			// shows up in the process list (`ps` is visible to all users on a
			// shared host).
			cmd := exec.Command("mysqldump", "-uroot", "-h", "127.0.0.1", "--single-transaction", "--databases", n)
			if pass != "" {
				cmd.Env = append(os.Environ(), "MYSQL_PWD="+pass)
			}
			out, ferr := os.Create(outPath)
			if ferr != nil {
				setBackupState(db, id, "failed", 0, "cannot create dump file for "+n)
				return
			}
			cmd.Stdout = out
			var errBuf strings.Builder
			cmd.Stderr = &errBuf
			if rerr := cmd.Run(); rerr != nil {
				out.Close()
				os.Remove(outPath)
				setBackupState(db, id, "failed", 0, "database dump failed for "+n+": "+strings.TrimSpace(errBuf.String()))
				return
			}
			out.Close()
			dumpFiles = append(dumpFiles, outPath)
			if fi, e := os.Stat(outPath); e == nil {
				dumpSize += fi.Size()
			}
			pct := 8 + int(float64(i+1)/float64(len(names))*30)
			if pct > 38 {
				pct = 38
			}
			setBackupState(db, id, "running", pct, "Dumping databases…")
		}
	}

	// Estimate total payload for progress computation. The ~/backups folder is
	// excluded from the archive, so its size is subtracted from the home size.
	var homeSize int64
	if includeFiles && homeDir != "" {
		var totalSize int64
		if out, err := exec.Command("du", "-sb", homeDir).Output(); err == nil {
			if fields := strings.Fields(string(out)); len(fields) > 0 {
				totalSize, _ = strconv.ParseInt(fields[0], 10, 64)
			}
		}
		var backupsSize int64
		if out, err := exec.Command("du", "-sb", filepath.Join(homeDir, "backups")).Output(); err == nil {
			if fields := strings.Fields(string(out)); len(fields) > 0 {
				backupsSize, _ = strconv.ParseInt(fields[0], 10, 64)
			}
		}
		homeSize = totalSize - backupsSize
		if homeSize < 0 {
			homeSize = 0
		}
	}
	totalEst := homeSize + dumpSize
	if totalEst <= 0 {
		totalEst = 1
	}

	setBackupState(db, id, "running", 40, "Archiving files…")

	// Build the archive layout (files under home/, dumps under databases/)
	// from a staging tree of symlinks, dereferenced with tar -h. GNU tar's
	// --transform applies globally to every member, so it cannot be used to
	// give different prefixes to the two file sets.
	staging := tmpRoot + "/tar"
	os.RemoveAll(staging)
	if includeFiles {
		os.MkdirAll(staging+"/home", 0755)
	}
	if includeFiles && homeDir != "" {
		if entries, err := os.ReadDir(homeDir); err == nil {
			for _, e := range entries {
				// Never archive the backups folder itself, or every backup would
				// recursively include all previous backups (and the staging dir).
				if e.Name() == "backups" {
					continue
				}
				src := filepath.Join(homeDir, e.Name())
				// Resolve the entry before it is dereferenced by tar. A symlink
				// pointing outside the account home must not be followed, or an
				// attacker could pull another tenant's (or arbitrary host) files
				// into their own downloadable archive. Entries that resolve
				// outside the home are omitted.
				resolved, rerr := filepath.EvalSymlinks(src)
				if rerr == nil && !isPathWithin(homeDir+"/", resolved) {
					log.Printf("[BACKUP] skipping symlink escaping home: %s", src)
					continue
				}
				if _, serr := os.Stat(src); serr != nil {
					continue
				}
				if err := os.Symlink(src, filepath.Join(staging, "home", e.Name())); err != nil {
					log.Printf("[BACKUP] symlink staging failed for %s: %v", e.Name(), err)
				}
			}
		}
	}
	if len(dumpFiles) > 0 {
		os.MkdirAll(staging+"/databases", 0755)
		for _, df := range dumpFiles {
			if err := os.Symlink(df, filepath.Join(staging, "databases", filepath.Base(df))); err != nil {
				log.Printf("[BACKUP] symlink staging failed for %s: %v", df, err)
			}
		}
	}

	// --ignore-failed-read: one unreadable file in the home (e.g. left over by
	// an external process) must not abort the whole backup.
	cmd := exec.Command("tar", "--ignore-failed-read", "-czhf", dest, "-C", staging, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		setBackupState(db, id, "failed", 0, "failed to start archiving: "+err.Error())
		return
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var runErr error
	finished := false
	for !finished {
		select {
		case err := <-done:
			runErr = err
			finished = true
		case <-time.After(300 * time.Millisecond):
			var sz int64
			if fi, e := os.Stat(dest); e == nil {
				sz = fi.Size()
			}
			pct := 40 + int(float64(sz)/float64(totalEst)*55)
			if pct > 95 {
				pct = 95
			}
			setBackupState(db, id, "running", pct, "Archiving files…")
		}
	}
	if runErr != nil {
		os.Remove(dest)
		setBackupState(db, id, "failed", 0, "archiving failed: "+runErr.Error())
		return
	}

	// Integrity check: fully read the archive (validates gzip + structure).
	setBackupState(db, id, "running", 96, "Verifying archive…")
	if out, err := exec.Command("tar", "-tzf", dest).CombinedOutput(); err != nil {
		os.Remove(dest)
		setBackupState(db, id, "failed", 0, "integrity check failed: "+strings.TrimSpace(string(out)))
		return
	}

	var size int64
	if fi, e := os.Stat(dest); e == nil {
		size = fi.Size()
	}
	db.Exec("UPDATE backups SET status = 'completed', progress = 100, message = 'Backup completed', file_size = ? WHERE id = ?", size, id)
}

// ---------- Restore execution ----------

func runRestore(db *sql.DB, id int64, accountID int, archivePath, homeDir string, restoreFiles, restoreDB bool) {
	defer func() {
		if r := recover(); r != nil {
			setBackupState(db, id, "failed", 0, fmt.Sprintf("restore error: %v", r))
			return
		}
	}()
	backupExecSem <- struct{}{}
	defer func() { <-backupExecSem }()
	setBackupState(db, id, "restoring", 5, "Preparing restore…")

	// Restore staging must live OUTSIDE the home directory: deployHome swaps the
	// home by renaming it aside, so a staging dir inside the home would be moved
	// with it and the swap would fail. A sibling scratch dir is used instead.
	scratchBase := filepath.Dir(homeDir)
	stagingHome := filepath.Join(scratchBase, ".owp_restore_"+strconv.FormatInt(id, 10)+"_home")
	stagingDB := filepath.Join(scratchBase, ".owp_restore_"+strconv.FormatInt(id, 10)+"_db")
	os.RemoveAll(stagingHome)
	os.RemoveAll(stagingDB)
	defer os.RemoveAll(stagingHome)
	defer os.RemoveAll(stagingDB)

	if restoreFiles {
		setBackupState(db, id, "restoring", 12, "Extracting files…")
		if err := os.MkdirAll(stagingHome, 0755); err != nil {
			setBackupState(db, id, "failed", 0, "cannot create restore staging directory")
			return
		}
		if err := extractArchiveEntries(archivePath, "home", stagingHome, true); err != nil {
			setBackupState(db, id, "failed", 0, "failed to extract files: "+err.Error())
			return
		}
	}
	if restoreDB {
		setBackupState(db, id, "restoring", 30, "Extracting databases…")
		if err := os.MkdirAll(stagingDB, 0755); err != nil {
			setBackupState(db, id, "failed", 0, "cannot create database staging directory")
			return
		}
		if err := extractArchiveEntries(archivePath, "databases", stagingDB, true); err != nil {
			setBackupState(db, id, "failed", 0, "failed to extract databases: "+err.Error())
			return
		}
	}

	if restoreFiles {
		setBackupState(db, id, "restoring", 60, "Restoring files…")
		if err := deployHome(stagingHome, homeDir); err != nil {
			setBackupState(db, id, "failed", 0, "failed to restore files: "+err.Error())
			return
		}
	}
	if restoreDB {
		setBackupState(db, id, "restoring", 80, "Restoring databases…")
		if err := restoreDatabases(db, accountID, stagingDB); err != nil {
			setBackupState(db, id, "failed", 0, "failed to restore databases: "+err.Error())
			return
		}
	}

	setBackupState(db, id, "restoring", 100, "Restore completed")
	db.Exec("UPDATE backups SET status = 'completed', progress = 100, message = 'Restore completed' WHERE id = ?", id)
}

// extractArchiveEntries extracts entries under the given prefix from a tar.gz
// archive into destDir. When strip is true, the prefix is removed from entry names.
func extractArchiveEntries(archivePath, prefix, destDir string, strip bool) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if name != prefix && !strings.HasPrefix(name, prefix+"/") {
			continue
		}
		rel := strings.TrimPrefix(name, prefix)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			continue
		}
		target := filepath.Join(destDir, rel)
		if !isPathWithin(destDir, target) {
			log.Printf("[BACKUP] skipping unsafe archive entry: %s", hdr.Name)
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
			os.Chmod(target, os.FileMode(hdr.Mode))
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			out, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
			// Preserve the archived file mode so restored dotfiles/secrets keep
			// their original (often 0600) permissions instead of inheriting umask.
			if hdr.Mode != 0 {
				os.Chmod(target, os.FileMode(hdr.Mode))
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(hdr.Linkname) {
				log.Printf("[BACKUP] skipping absolute symlink: %s -> %s", hdr.Name, hdr.Linkname)
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(target), hdr.Linkname))
			if !isPathWithin(destDir, resolved) {
				log.Printf("[BACKUP] skipping unsafe symlink: %s -> %s", hdr.Name, hdr.Linkname)
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				log.Printf("[BACKUP] symlink create failed: %v", err)
			}
		default:
			log.Printf("[BACKUP] skipping unsupported entry type %d: %s", hdr.Typeflag, hdr.Name)
		}
	}
	return nil
}

// deployHome swaps the account home with the restored content. The existing
// home is renamed aside first and the staged content is renamed into place, so
// a permission failure while removing the old files can never leave the live
// home half-deleted. Cleanup of the old home is best-effort.
func deployHome(stagingDir, homeDir string) error {
	if err := os.MkdirAll(filepath.Dir(homeDir), 0755); err != nil {
		return err
	}
	bak := homeDir + ".owp_restore_bak"
	if _, err := os.Stat(homeDir); err == nil {
		os.RemoveAll(bak)
		if err := os.Rename(homeDir, bak); err != nil {
			return err
		}
	}
	if err := os.Rename(stagingDir, homeDir); err != nil {
		// Roll the original home back.
		if _, statErr := os.Stat(bak); statErr == nil {
			os.Rename(bak, homeDir)
		}
		return err
	}
	// The backups folder is excluded from archives, so the staged home has none.
	// Preserve the account's existing ~/backups (all previous backup archives)
	// into the freshly restored home.
	if _, err := os.Stat(filepath.Join(bak, "backups")); err == nil {
		if err := os.Rename(filepath.Join(bak, "backups"), filepath.Join(homeDir, "backups")); err != nil {
			log.Printf("[BACKUP] warning: could not preserve backups folder across restore: %v", err)
		}
	}
	if err := os.RemoveAll(bak); err != nil {
		log.Printf("[BACKUP] warning: could not fully remove old home backup %s: %v", bak, err)
	}
	return nil
}

// restoreDatabases restores each *.sql dump in stagingDir into MySQL, but only
// for databases that the account actually owns. The db name is taken from the
// dump file and cross-checked against the account's child_databases.
func restoreDatabases(db *sql.DB, accountID int, stagingDir string) error {
	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		return nil
	}
	pass := os.Getenv("MYSQL_ROOT_PASSWORD")
	restored := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		dumpPath := filepath.Join(stagingDir, e.Name())
		dbName := strings.TrimSuffix(e.Name(), ".sql")
		if !validMysqlIdent(dbName) {
			log.Printf("[BACKUP] skipping invalid dump name: %s", e.Name())
			continue
		}
		var owned int
		if err := db.QueryRow("SELECT COUNT(*) FROM child_databases WHERE account_id = ? AND LOWER(db_name) = LOWER(?)", accountID, dbName).Scan(&owned); err != nil || owned == 0 {
			log.Printf("[BACKUP] skipping database not owned by account: %s", dbName)
			continue
		}
		// Stream-validate the whole dump before it can reach MySQL. A backup is
		// a file in the account's own home and can be tampered with, so never
		// pipe it blindly into a privileged mysql client. Reject any statement
		// that could escalate privileges or touch another database.
		f, err := os.Open(dumpPath)
		if err != nil {
			continue
		}
		if reason, err := validateSQLDump(f, dbName); err != nil || reason != "" {
			f.Close()
			if err != nil {
				log.Printf("[BACKUP] restore of %s aborted: %v", dbName, err)
			} else {
				log.Printf("[BACKUP] restore of %s rejected: %s", dbName, reason)
			}
			continue
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			continue
		}

		// Prefer tenant-scoped credentials so the restore can only ever touch
		// the account's own database, even if the dump is hostile.
		dbUser := ""
		if err := db.QueryRow("SELECT db_user FROM child_databases WHERE account_id = ? AND LOWER(db_name) = LOWER(?)", accountID, dbName).Scan(&dbUser); err != nil || dbUser == "" {
			log.Printf("[BACKUP] restore of %s aborted: no tenant-scoped mysql user available", dbName)
			continue
		}
		args := []string{"-h", "127.0.0.1", "-u", dbUser, "--default-character-set=utf8mb4", dbName}

		cmd := exec.Command("mysql", args...)
		if pass != "" {
			cmd.Env = append(os.Environ(), "MYSQL_PWD="+pass)
		}
		if dbUser != "" {
			var uPass string
			if err := db.QueryRow("SELECT password FROM db_users WHERE username = ?", dbUser).Scan(&uPass); err == nil && uPass != "" {
				cmd.Env = append(os.Environ(), "MYSQL_PWD="+uPass)
			}
		}
		var errBuf strings.Builder
		cmd.Stderr = &errBuf
		cmd.Stdin = f
		if err := cmd.Run(); err != nil {
			f.Close()
			return fmt.Errorf("restore of %s failed: %s", dbName, strings.TrimSpace(errBuf.String()))
		}
		f.Close()
		restored++
	}
	if restored == 0 {
		return fmt.Errorf("no databases in this backup could be restored")
	}
	return nil
}
