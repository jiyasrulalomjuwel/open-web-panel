package main

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// ---------- bandwidth tracking middleware ----------

type bwRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *bwRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *bwRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

func trackBandwidth(db *sql.DB) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &bwRecorder{ResponseWriter: w, status: 200}
			next.ServeHTTP(rec, r)
			c := getClaims(r)
			if c != nil && c.Scope == "child" && c.AccountID > 0 {
				today := time.Now().Format("2006-01-02")
				db.Exec(`INSERT INTO bandwidth_logs (account_id, bytes_in, bytes_out, logged_at) VALUES (?, ?, ?, ?) ON CONFLICT(account_id, logged_at) DO UPDATE SET bytes_out = bytes_out + ?, bytes_in = bytes_in + ?`,
					c.AccountID, 0, rec.bytes, today, rec.bytes, 0)
				db.Exec("UPDATE accounts SET bandwidth_used_mb = (SELECT COALESCE(SUM(bytes_out)/1048576, 0) FROM bandwidth_logs WHERE account_id = ?) WHERE id = ?",
					c.AccountID, c.AccountID)
			}
		})
	}
}

// ---------- parent bandwidth routes ----------

func bandwidthRoutes(r chi.Router, db *sql.DB) {
	// /api/v1/bandwidth/summary — total usage + daily chart
	r.Get("/summary", func(w http.ResponseWriter, r *http.Request) {
		var totalBytes int64
		db.QueryRow("SELECT COALESCE(SUM(bytes_out), 0) FROM bandwidth_logs").Scan(&totalBytes)

		rows, err := db.Query(`SELECT logged_at, SUM(bytes_out) as total
			FROM bandwidth_logs
			WHERE logged_at >= date('now', '-30 days')
			GROUP BY logged_at ORDER BY logged_at DESC`)
		if err != nil {
			jsonResp(w, 200, map[string]interface{}{"total_bytes": totalBytes, "days": []interface{}{}})
			return
		}
		defer rows.Close()

		type dayPoint struct {
			Date    string `json:"date"`
			BytesIn int64  `json:"bytes_in"`
			BytesOut int64 `json:"bytes_out"`
		}
		days := make([]dayPoint, 0)
		for rows.Next() {
			var p dayPoint
			rows.Scan(&p.Date, &p.BytesOut)
			days = append(days, p)
		}

		jsonResp(w, 200, map[string]interface{}{
			"total_bytes": totalBytes,
			"days":        days,
		})
	})

	// /api/v1/bandwidth/accounts — per-account breakdown
	r.Get("/accounts", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT a.id, a.username, a.domain,
			COALESCE(SUM(bl.bytes_out), 0) as total_bytes,
			COALESCE(SUM(bl.bytes_out)/1048576, 0) as used_mb
			FROM accounts a
			LEFT JOIN bandwidth_logs bl ON bl.account_id = a.id
			GROUP BY a.id ORDER BY total_bytes DESC`)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		accounts := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id int
			var username, domain string
			var bytes, mb int64
			rows.Scan(&id, &username, &domain, &bytes, &mb)
			accounts = append(accounts, map[string]interface{}{
				"id": id, "username": username, "domain": domain,
				"bytes": bytes, "used_mb": mb,
			})
		}
		jsonResp(w, 200, accounts)
	})
}

func parseInt(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

// ---------- child bandwidth routes ----------

func childBandwidthRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		var totalBytes int64
		db.QueryRow("SELECT COALESCE(SUM(bytes_out), 0) FROM bandwidth_logs WHERE account_id = ?", c.AccountID).Scan(&totalBytes)

		var limitMB int
		db.QueryRow("SELECT p.bandwidth_mb FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?", c.AccountID).Scan(&limitMB)

		usagePercent := 0.0
		if limitMB > 0 {
			usagePercent = float64(totalBytes) / float64(limitMB*1048576) * 100
		}

		rows, err := db.Query(`SELECT logged_at, bytes_out FROM bandwidth_logs WHERE account_id = ? ORDER BY logged_at DESC LIMIT 60`, c.AccountID)
		if err != nil {
			jsonResp(w, 200, map[string]interface{}{
				"total_bytes":   totalBytes,
				"used_mb":       totalBytes / 1048576,
				"limit_mb":      limitMB,
				"usage_percent": usagePercent,
				"days":          []interface{}{},
			})
			return
		}
		defer rows.Close()

		type dayPoint struct {
			Date    string `json:"date"`
			BytesIn int64  `json:"bytes_in"`
			BytesOut int64 `json:"bytes_out"`
		}
		days := make([]dayPoint, 0)
		for rows.Next() {
			var p dayPoint
			rows.Scan(&p.Date, &p.BytesOut)
			days = append(days, p)
		}

		jsonResp(w, 200, map[string]interface{}{
			"total_bytes":   totalBytes,
			"used_mb":       totalBytes / 1048576,
			"limit_mb":      limitMB,
			"usage_percent": usagePercent,
			"days":          days,
		})
	})
}
