package main

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
)

func childCronRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		rows, err := db.Query(`SELECT id, command, schedule, COALESCE(description,''),
			enabled, COALESCE(last_run_at,''), created_at
			FROM cron_jobs WHERE account_id = ? ORDER BY created_at DESC`, c.AccountID)
		if err != nil {
			getLogger(r).Errorf("cron query: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		type CronJob struct {
			ID          int    `json:"id"`
			Command     string `json:"command"`
			Schedule    string `json:"schedule"`
			Description string `json:"description"`
			Enabled     bool   `json:"enabled"`
			LastRunAt   string `json:"last_run_at"`
			CreatedAt   string `json:"created_at"`
		}
		jobs := make([]CronJob, 0)
		for rows.Next() {
			var j CronJob
			var enabled int
			if err := rows.Scan(&j.ID, &j.Command, &j.Schedule, &j.Description, &enabled, &j.LastRunAt, &j.CreatedAt); err != nil {
				getLogger(r).Warnf("scan cron row: %v", err)
				continue
			}
			j.Enabled = enabled == 1
			jobs = append(jobs, j)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("cron rows iteration error: %v", err)
		}
		jsonResp(w, 200, jobs)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		var req struct {
			Command     string `json:"command"`
			Schedule    string `json:"schedule"`
			Description string `json:"description"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		if req.Command == "" {
			writeAppError(w, r, apperrors.Validation("command is required"))
			return
		}
		if req.Schedule == "" {
			writeAppError(w, r, apperrors.Validation("schedule is required"))
			return
		}
		if !isValidCronExpr(req.Schedule) {
			writeAppError(w, r, apperrors.Validation("invalid cron expression (expected: 'min hour dom mon dow')"))
			return
		}
		if len(req.Command) > 1000 {
			writeAppError(w, r, apperrors.Validation("command too long (max 1000 chars)"))
			return
		}

		if isRAMExceeded(db, c.AccountID) {
			writeAppError(w, r, apperrors.QuotaExceeded("RAM", int64(getRAMLimit(db, c.AccountID))))
			return
		}

		tx, err := db.Begin()
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		defer tx.Rollback()

		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM cron_jobs WHERE account_id = ?", c.AccountID).Scan(&count); err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		if count >= 20 {
			writeAppError(w, r, apperrors.QuotaExceeded("cron jobs", 20))
			return
		}

		result, err := tx.Exec(`INSERT INTO cron_jobs (account_id, command, schedule, description, enabled)
			VALUES (?, ?, ?, ?, 1)`, c.AccountID, req.Command, req.Schedule, req.Description)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		if err := tx.Commit(); err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		id, _ := result.LastInsertId()
		snippet := req.Command
		if len(snippet) > 50 {
			snippet = snippet[:50]
		}
		auditLog(db, r, "cron.create", logging.Fields{"id": id, "command": snippet})
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
		var req struct {
			Command     string `json:"command"`
			Schedule    string `json:"schedule"`
			Description string `json:"description"`
			Enabled     *bool  `json:"enabled"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}

		var ownerID int
		if err := db.QueryRow("SELECT account_id FROM cron_jobs WHERE id = ?", id).Scan(&ownerID); err != nil || ownerID != c.AccountID {
			writeAppError(w, r, apperrors.NotFound("cron job", id))
			return
		}

		if err := withDBRetry(func() error {
			if req.Command != "" {
				if len(req.Command) > 1000 {
					return apperrors.Validation("command too long")
				}
				_, e := db.Exec("UPDATE cron_jobs SET command = ? WHERE id = ?", req.Command, id)
				if e != nil {
					return e
				}
			}
			if req.Schedule != "" {
				if !isValidCronExpr(req.Schedule) {
					return apperrors.Validation("invalid cron expression")
				}
				_, e := db.Exec("UPDATE cron_jobs SET schedule = ? WHERE id = ?", req.Schedule, id)
				if e != nil {
					return e
				}
			}
			if req.Enabled != nil {
				v := 0
				if *req.Enabled {
					v = 1
				}
				_, e := db.Exec("UPDATE cron_jobs SET enabled = ? WHERE id = ?", v, id)
				if e != nil {
					return e
				}
			}
			if req.Description != "" {
				_, e := db.Exec("UPDATE cron_jobs SET description = ? WHERE id = ?", req.Description, id)
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

		auditLog(db, r, "cron.update", logging.Fields{"id": id})
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
		result, err := db.Exec("DELETE FROM cron_jobs WHERE id = ? AND account_id = ?", id, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			writeAppError(w, r, apperrors.NotFound("cron job", id))
			return
		}
		auditLog(db, r, "cron.delete", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	r.Post("/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
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
		_, execErr := db.Exec("UPDATE cron_jobs SET enabled = CASE WHEN enabled THEN 0 ELSE 1 END WHERE id = ? AND account_id = ?",
			id, c.AccountID)
		if execErr != nil {
			writeAppError(w, r, apperrors.Database(execErr))
			return
		}
		jsonResp(w, 200, map[string]string{"status": "toggled"})
	})

	r.Get("/presets", func(w http.ResponseWriter, r *http.Request) {
		presets := []map[string]string{
			{"label": "Every minute", "schedule": "* * * * *"},
			{"label": "Every 5 minutes", "schedule": "*/5 * * * *"},
			{"label": "Every 15 minutes", "schedule": "*/15 * * * *"},
			{"label": "Every 30 minutes", "schedule": "*/30 * * * *"},
			{"label": "Every hour", "schedule": "0 * * * *"},
			{"label": "Twice daily", "schedule": "0 0,12 * * *"},
			{"label": "Daily at midnight", "schedule": "0 0 * * *"},
			{"label": "Weekly (Sunday midnight)", "schedule": "0 0 * * 0"},
			{"label": "Monthly (1st at midnight)", "schedule": "0 0 1 * *"},
		}
		jsonResp(w, 200, presets)
	})
}

func isValidCronExpr(expr string) bool {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return false
	}
	ranges := [][2]int{
		{0, 59},
		{0, 23},
		{1, 31},
		{1, 12},
		{0, 7},
	}
	for i, part := range parts {
		if part == "*" || part == "*/1" {
			continue
		}
		if strings.HasPrefix(part, "*/") {
			n, err := strconv.Atoi(part[2:])
			if err != nil || n < 1 || n > ranges[i][1] {
				return false
			}
			continue
		}
		for _, seg := range strings.Split(part, ",") {
			if seg == "*" {
				continue
			}
			if strings.Contains(seg, "-") {
				ends := strings.SplitN(seg, "-", 2)
				if len(ends) != 2 {
					return false
				}
				s, err1 := strconv.Atoi(ends[0])
				e, err2 := strconv.Atoi(ends[1])
				if err1 != nil || err2 != nil || s < ranges[i][0] || e > ranges[i][1] || s > e {
					return false
				}
			} else {
				n, err := strconv.Atoi(seg)
				if err != nil || n < ranges[i][0] || n > ranges[i][1] {
					return false
				}
			}
		}
	}
	return true
}

func startCronRunner(db *sql.DB) {
	go func() {
		cronLogger := logging.NewDefault("cron")
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			now := time.Now()
			rows, err := db.Query(`SELECT id, account_id, command, schedule, description,
				COALESCE(last_run_at,'') FROM cron_jobs WHERE enabled = 1`)
			if err != nil {
				cronLogger.Errorf("cron runner query: %v", err)
				continue
			}
			for rows.Next() {
				var id, accountID int
				var command, schedule, description, lastRun string
				if err := rows.Scan(&id, &accountID, &command, &schedule, &description, &lastRun); err != nil {
					cronLogger.Warnf("scan cron runner row: %v", err)
					continue
				}
			// Deduplicate: the runner wakes every 30s but a schedule like
			// "* * * * *" matches at both the :00 and :30 tick in the same
			// minute. Skip a job that already ran within this minute.
			if cronMatches(now, schedule) && !ranInSameMinute(lastRun, now) {
				// Skip jobs for non-active accounts and accounts with cron
				// disabled, mirroring the backup scheduler behavior.
				var status string
				if err := db.QueryRow("SELECT status FROM accounts WHERE id = ?", accountID).Scan(&status); err != nil || status != "active" {
					continue
				}
				if !featureEnabledFor(db, accountID, "cron") {
					continue
				}
				db.Exec("UPDATE cron_jobs SET last_run_at = datetime('now') WHERE id = ?", id)
					go func(jobID, acctID int, cmd string) {
						// Tenant isolation: run the job inside the account's own
						// container. Never execute tenant-supplied commands on the
						// host as the panel user (that is full host RCE).
						container := getContainerForAccount(db, acctID)
						if container == "" {
							cronLogger.Errorf("job %d failed: no container for account %d", jobID, acctID)
							return
						}
						output, err := runCmd("docker", "exec", container, "sh", "-c", cmd)
						if err != nil {
							cronLogger.Errorf("job %d failed: %v\n  Output: %s", jobID, err, output)
						}
					}(id, accountID, command)
				}
			}
			if err := rows.Err(); err != nil {
				cronLogger.Errorf("cron runner rows iteration error: %v", err)
			}
			rows.Close()
		}
	}()
}

// ranInSameMinute reports whether the job's last run (stored as a UTC datetime
// string) fell within the same minute as `now`. Used to prevent a 30-second
// ticker from firing a sub-hourly schedule twice.
func ranInSameMinute(lastRun string, now time.Time) bool {
	if lastRun == "" {
		return false
	}
	var t time.Time
	if strings.HasSuffix(lastRun, "Z") {
		t, _ = time.Parse("2006-01-02 15:04:05Z", lastRun)
	} else {
		t, _ = time.ParseInLocation("2006-01-02 15:04:05", lastRun, time.UTC)
	}
	if t.IsZero() {
		return false
	}
	return t.UTC().Format("2006-01-02 15:04") == now.Format("2006-01-02 15:04")
}

func cronMatches(t time.Time, expr string) bool {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return false
	}
	values := []int{t.Minute(), t.Hour(), t.Day(), int(t.Month()), int(t.Weekday())}
	for i, part := range parts {
		if part == "*" {
			continue
		}
		if strings.HasPrefix(part, "*/") {
			n, err := strconv.Atoi(part[2:])
			if err != nil || n == 0 {
				return false
			}
			if values[i]%n != 0 {
				return false
			}
			continue
		}
		matched := false
		for _, seg := range strings.Split(part, ",") {
			if strings.Contains(seg, "-") {
				ends := strings.SplitN(seg, "-", 2)
				s, _ := strconv.Atoi(ends[0])
				e, _ := strconv.Atoi(ends[1])
				if i == 4 {
					if s == 7 {
						s = 0
					}
					if e == 7 {
						e = 0
					}
				}
				if values[i] >= s && values[i] <= e {
					matched = true
					break
				}
			} else {
				n, _ := strconv.Atoi(seg)
				if i == 4 && n == 7 {
					n = 0
				}
				if values[i] == n {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
