package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// ── Cron Job Routes (Child Panel) ──

func childCronRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		rows, err := db.Query(`SELECT id, command, schedule, COALESCE(description,''),
			enabled, COALESCE(last_run_at,''), created_at
			FROM cron_jobs WHERE account_id = ? ORDER BY created_at DESC`, c.AccountID)
		if err != nil {
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
			rows.Scan(&j.ID, &j.Command, &j.Schedule, &j.Description,
				&enabled, &j.LastRunAt, &j.CreatedAt)
			j.Enabled = enabled == 1
			jobs = append(jobs, j)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[CRON] rows iteration error: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		jsonResp(w, 200, jobs)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		var req struct {
			Command     string `json:"command"`
			Schedule    string `json:"schedule"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		if req.Command == "" || req.Schedule == "" {
			jsonError(w, 400, "command and schedule required")
			return
		}
		if !isValidCronExpr(req.Schedule) {
			jsonError(w, 400, "invalid cron expression (expected: 'min hour dom mon dow')")
			return
		}
		if len(req.Command) > 1000 {
			jsonError(w, 400, "command too long (max 1000 chars)")
			return
		}

		if isRAMExceeded(db, c.AccountID) {
			jsonError(w, 429, "RAM limit exceeded. New cron jobs are temporarily blocked. Please contact your hosting administrator to upgrade your resource allocation.")
			return
		}

		// Enforce max cron jobs per account (default 20)
		var count int
		db.QueryRow("SELECT COUNT(*) FROM cron_jobs WHERE account_id = ?", c.AccountID).Scan(&count)
		if count >= 20 {
			jsonError(w, 403, "cron job limit reached (max 20)")
			return
		}

		result, err := db.Exec(`INSERT INTO cron_jobs (account_id, command, schedule, description, enabled)
			VALUES (?, ?, ?, ?, 1)`, c.AccountID, req.Command, req.Schedule, req.Description)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		id, _ := result.LastInsertId()
		auditLog(db, r, "cron.create", map[string]interface{}{"id": id, "command": req.Command[0:min(50, len(req.Command))]})

		jsonResp(w, 201, map[string]interface{}{"id": id, "status": "created"})
	})

	r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		var req struct {
			Command     string `json:"command"`
			Schedule    string `json:"schedule"`
			Description string `json:"description"`
			Enabled     *bool  `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}

		// Verify ownership
		var ownerID int
		err := db.QueryRow("SELECT account_id FROM cron_jobs WHERE id = ?", id).Scan(&ownerID)
		if err != nil || ownerID != c.AccountID {
			jsonError(w, 404, "cron job not found")
			return
		}

		if req.Command != "" {
			if len(req.Command) > 1000 {
				jsonError(w, 400, "command too long")
				return
			}
			db.Exec("UPDATE cron_jobs SET command = ? WHERE id = ?", req.Command, id)
		}
		if req.Schedule != "" {
			if !isValidCronExpr(req.Schedule) {
				jsonError(w, 400, "invalid cron expression")
				return
			}
			db.Exec("UPDATE cron_jobs SET schedule = ? WHERE id = ?", req.Schedule, id)
		}
		if req.Enabled != nil {
			v := 0
			if *req.Enabled {
				v = 1
			}
			db.Exec("UPDATE cron_jobs SET enabled = ? WHERE id = ?", v, id)
		}
		if req.Description != "" {
			db.Exec("UPDATE cron_jobs SET description = ? WHERE id = ?", req.Description, id)
		}

		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		result, err := db.Exec("DELETE FROM cron_jobs WHERE id = ? AND account_id = ?", id, c.AccountID)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			jsonError(w, 404, "cron job not found")
			return
		}
		auditLog(db, r, "cron.delete", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	// Toggle enable/disable
	r.Post("/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		db.Exec("UPDATE cron_jobs SET enabled = CASE WHEN enabled THEN 0 ELSE 1 END WHERE id = ? AND account_id = ?",
			id, c.AccountID)
		jsonResp(w, 200, map[string]string{"status": "toggled"})
	})

	// List available cron presets / common schedules
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

// Cron expression validation (basic: 5 fields, each valid range)
func isValidCronExpr(expr string) bool {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return false
	}
	ranges := [][2]int{
		{0, 59}, // minute
		{0, 23}, // hour
		{1, 31}, // day of month
		{1, 12}, // month
		{0, 7},  // day of week
	}
	for i, part := range parts {
		if part == "*" || part == "*/1" {
			continue
		}
		// Allow */N
		if strings.HasPrefix(part, "*/") {
			n, err := strconv.Atoi(part[2:])
			if err != nil || n < 1 || n > ranges[i][1] {
				return false
			}
			continue
		}
		// Allow N,N,N
		for _, seg := range strings.Split(part, ",") {
			if seg == "*" {
				continue
			}
			// Allow N-N
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

// ── Cron Runner ──

func startCronRunner(db *sql.DB) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			now := time.Now()
			rows, err := db.Query(`SELECT id, account_id, command, schedule, description
				FROM cron_jobs WHERE enabled = 1`)
			if err != nil {
				continue
			}
			for rows.Next() {
				var id, accountID int
				var command, schedule, description string
				rows.Scan(&id, &accountID, &command, &schedule, &description)
				if cronMatches(now, schedule) {
					db.Exec("UPDATE cron_jobs SET last_run_at = datetime('now') WHERE id = ?", id)
					go func(cmd string) {
						output, err := runCmd("sh", "-c", cmd)
						if err != nil {
							fmt.Printf("[CRON] Job %d failed: %v\n  Output: %s\n", id, err, output)
						}
					}(command)
				}
			}
			if err := rows.Err(); err != nil {
				log.Printf("[CRON] runner rows iteration error: %v", err)
			}
			rows.Close()
		}
	}()
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
				if values[i] >= s && values[i] <= e {
					matched = true
					break
				}
			} else {
				n, _ := strconv.Atoi(seg)
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
