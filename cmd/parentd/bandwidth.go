package main

import (
	"bufio"
	"database/sql"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// ---------- bandwidth tracking middleware ----------

type bwRecorder struct {
	http.ResponseWriter
	status  int
	bytesIn int64
	bytes   int64
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
			if r.Body != nil {
				rec.bytesIn = r.ContentLength
				if rec.bytesIn < 0 {
					rec.bytesIn = 0
				}
			}
			next.ServeHTTP(rec, r)
			c := getClaims(r)
			if c != nil && c.Scope == "child" && c.AccountID > 0 {
				today := time.Now().Format("2006-01-02")
				db.Exec(`INSERT INTO bandwidth_logs (account_id, bytes_in, bytes_out, logged_at) VALUES (?, ?, ?, ?) ON CONFLICT(account_id, logged_at) DO UPDATE SET bytes_out = bytes_out + ?, bytes_in = bytes_in + ?`,
					c.AccountID, rec.bytesIn, rec.bytes, today, rec.bytes, rec.bytesIn)
				db.Exec("UPDATE accounts SET bandwidth_used_mb = (SELECT COALESCE(SUM(bytes_out + bytes_in)/1048576, 0) FROM bandwidth_logs WHERE account_id = ?) WHERE id = ?",
					c.AccountID, c.AccountID)
			}
		})
	}
}

// ---------- Nginx vhost log parser (website visitor bandwidth) ----------

func startNginxBandwidthCollector(db *sql.DB) {
	go func() {
		// Wait a bit for the server to start
		time.Sleep(10 * time.Second)

		// Track last read position per log file
		positions := make(map[string]int64)

		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			collectNginxBandwidth(db, positions)
		}
	}()
	log.Println("[BW] Nginx bandwidth collector started (every 5 min)")
}

func collectNginxBandwidth(db *sql.DB, positions map[string]int64) {
	logDir := getNginxLogDir()
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".access.log") {
			continue
		}
		domain := strings.TrimSuffix(e.Name(), ".access.log")
		logPath := filepath.Join(logDir, e.Name())

		// Look up the account for this domain
		var accountID int
		var docRoot string
		err := db.QueryRow("SELECT account_id, doc_root FROM domains WHERE domain = ?", domain).Scan(&accountID, &docRoot)
		if err != nil {
			continue
		}

		// Open log file and seek to last position
		f, err := os.Open(logPath)
		if err != nil {
			continue
		}

		lastPos := positions[logPath]
		if lastPos > 0 {
			// Check if file has been truncated (position beyond file size)
			if fi, _ := f.Stat(); fi != nil && lastPos > fi.Size() {
				lastPos = 0
			}
			f.Seek(lastPos, 0)
		}

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

		var totalBytes int64
		var linesRead int
		for scanner.Scan() {
			line := scanner.Text()
			m := nginxLogRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			bytesStr := m[5]
			bytesVal, _ := strconv.ParseInt(bytesStr, 10, 64)
			if bytesVal > 0 {
				totalBytes += bytesVal
			}
			linesRead++
		}

		// Record position for next time
		if pos, err := f.Seek(0, 1); err == nil {
			positions[logPath] = pos
		}
		f.Close()

		if totalBytes > 0 && accountID > 0 {
			today := time.Now().Format("2006-01-02")
			db.Exec(`INSERT INTO bandwidth_logs (account_id, bytes_in, bytes_out, logged_at) VALUES (?, ?, ?, ?) ON CONFLICT(account_id, logged_at) DO UPDATE SET bytes_out = bytes_out + ?, bytes_in = bytes_in + ?`,
				accountID, 0, totalBytes, today, totalBytes, 0)
		}
	}
}

// ---------- SMTP bandwidth tracking ----------

func trackSMTPBandwidth(db *sql.DB, emailAddr string, bytes int64) {
	var hostAccountID int
	err := db.QueryRow("SELECT account_id FROM email_accounts WHERE email = ?", emailAddr).Scan(&hostAccountID)
	if err != nil || hostAccountID == 0 {
		return
	}
	today := time.Now().Format("2006-01-02")
	db.Exec(`INSERT INTO bandwidth_logs (account_id, bytes_in, bytes_out, logged_at) VALUES (?, ?, ?, ?) ON CONFLICT(account_id, logged_at) DO UPDATE SET bytes_out = bytes_out + ?, bytes_in = bytes_in + ?`,
		hostAccountID, bytes, 0, today, 0, bytes)
}

// ---------- parent bandwidth routes ----------

func bandwidthRoutes(r chi.Router, db *sql.DB) {
	// /api/v1/bandwidth/summary — total usage + daily chart
	r.Get("/summary", func(w http.ResponseWriter, r *http.Request) {
		var totalBytes int64
		db.QueryRow("SELECT COALESCE(SUM(bytes_out + bytes_in), 0) FROM bandwidth_logs").Scan(&totalBytes)

		rows, err := db.Query(`SELECT logged_at, SUM(bytes_out) as bytes_out, SUM(bytes_in) as bytes_in
			FROM bandwidth_logs
			WHERE logged_at >= date('now', '-30 days')
			GROUP BY logged_at ORDER BY logged_at DESC`)
		if err != nil {
			jsonResp(w, 200, map[string]interface{}{"total_bytes": totalBytes, "days": []interface{}{}})
			return
		}
		defer rows.Close()

		type dayPoint struct {
			Date     string `json:"date"`
			BytesIn  int64  `json:"bytes_in"`
			BytesOut int64  `json:"bytes_out"`
		}
		days := make([]dayPoint, 0)
		for rows.Next() {
			var p dayPoint
			rows.Scan(&p.Date, &p.BytesOut, &p.BytesIn)
			days = append(days, p)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[BANDWIDTH] rows iteration error: %v", err)
			jsonResp(w, 200, map[string]interface{}{"total_bytes": 0, "days": []interface{}{}})
			return
		}

		jsonResp(w, 200, map[string]interface{}{
			"total_bytes": totalBytes,
			"days":        days,
		})
	})

	// /api/v1/bandwidth/accounts — per-account breakdown
	r.Get("/accounts", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT a.id, a.username, a.domain,
			COALESCE(SUM(bl.bytes_out + bl.bytes_in), 0) as total_bytes,
			COALESCE(SUM(bl.bytes_out + bl.bytes_in)/1048576, 0) as used_mb
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
		if err := rows.Err(); err != nil {
			log.Printf("[BANDWIDTH] accounts rows iteration error: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
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
		db.QueryRow("SELECT COALESCE(SUM(bytes_out + bytes_in), 0) FROM bandwidth_logs WHERE account_id = ?", c.AccountID).Scan(&totalBytes)

		var limitMB int
		db.QueryRow("SELECT p.bandwidth_mb FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?", c.AccountID).Scan(&limitMB)

		usagePercent := 0.0
		if limitMB > 0 {
			usagePercent = float64(totalBytes) / float64(limitMB*1048576) * 100
		}

		rows, err := db.Query(`SELECT logged_at, bytes_in, bytes_out FROM bandwidth_logs WHERE account_id = ? ORDER BY logged_at DESC LIMIT 60`, c.AccountID)
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
			Date     string `json:"date"`
			BytesIn  int64  `json:"bytes_in"`
			BytesOut int64  `json:"bytes_out"`
		}
		days := make([]dayPoint, 0)
		for rows.Next() {
			var p dayPoint
			rows.Scan(&p.Date, &p.BytesIn, &p.BytesOut)
			days = append(days, p)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[BANDWIDTH] child rows iteration error: %v", err)
			jsonResp(w, 200, map[string]interface{}{"total_bytes": 0, "used_mb": 0, "limit_mb": 0, "usage_percent": 0.0, "days": []interface{}{}})
			return
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
