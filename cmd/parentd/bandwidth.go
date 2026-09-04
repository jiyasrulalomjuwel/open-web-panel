package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
)

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
				if _, err := db.Exec(`INSERT INTO bandwidth_logs (account_id, bytes_in, bytes_out, logged_at) VALUES (?, ?, ?, ?) ON CONFLICT(account_id, logged_at) DO UPDATE SET bytes_out = bytes_out + ?, bytes_in = bytes_in + ?`,
					c.AccountID, rec.bytesIn, rec.bytes, today, rec.bytes, rec.bytesIn); err != nil {
					getLogger(r).Errorf("bandwidth middleware insert error for account %d: %v", c.AccountID, err)
				}
				if _, err := db.Exec("UPDATE accounts SET bandwidth_used_mb = ROUND((SELECT COALESCE(SUM(bytes_out + bytes_in), 0) FROM bandwidth_logs WHERE account_id = ?) / 1048576.0, 2) WHERE id = ?",
					c.AccountID, c.AccountID); err != nil {
					getLogger(r).Errorf("bandwidth middleware update error for account %d: %v", c.AccountID, err)
				}
			}
		})
	}
}

type logFollower struct {
	file      *os.File
	path      string
	domain    string
	accountID int
	position  int64
	ino       uint64
}

type nginxLogProgress struct {
	ino    uint64
	offset int64
}

func fileInode(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}

func loadNginxLogProgress(db *sql.DB, domain string) (nginxLogProgress, bool) {
	var val string
	if err := db.QueryRow("SELECT value FROM server_config WHERE key_name = ?", "bw_progress."+domain).Scan(&val); err != nil || val == "" {
		return nginxLogProgress{}, false
	}
	parts := strings.SplitN(val, ":", 2)
	if len(parts) != 2 {
		return nginxLogProgress{}, false
	}
	ino, err1 := strconv.ParseUint(parts[0], 10, 64)
	off, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil || off < 0 {
		return nginxLogProgress{}, false
	}
	return nginxLogProgress{ino: ino, offset: off}, true
}

func saveNginxLogProgress(db *sql.DB, domain string, ino uint64, offset int64) error {
	_, err := db.Exec("INSERT OR REPLACE INTO server_config (key_name, value, updated_at) VALUES (?, ?, datetime('now'))",
		"bw_progress."+domain, fmt.Sprintf("%d:%d", ino, offset))
	return err
}

func startNginxBandwidthCollector(db *sql.DB) {
	l := logging.NewDefault("bandwidth")
	go func() {
		time.Sleep(10 * time.Second)

		followers := make(map[string]*logFollower)
		rescanCounter := 0

		rescanLogFiles(db, followers)

		ticker := time.NewTicker(5 * time.Second)
		for range ticker.C {
			rescanCounter++
			if rescanCounter >= 10 {
				rescanLogFiles(db, followers)
				rescanCounter = 0
			}
			pollLogFiles(db, followers)
		}
	}()
	l.Infof("Nginx bandwidth collector started (5 sec poll)")
}

func rescanLogFiles(db *sql.DB, followers map[string]*logFollower) {
	l := logging.NewDefault("bandwidth")
	logDir := getNginxLogDir()
	entries, err := os.ReadDir(logDir)
	if err != nil {
		l.Errorf("cannot read nginx log dir %s: %v", logDir, err)
		return
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".access.log") {
			continue
		}
		domain := strings.TrimSuffix(e.Name(), ".access.log")
		logPath := filepath.Join(logDir, e.Name())

		if existing, ok := followers[logPath]; ok {
			newFi, newStatErr := os.Stat(logPath)
			oldFi, oldStatErr := existing.file.Stat()
			if newStatErr == nil && oldStatErr == nil && !os.SameFile(newFi, oldFi) {
				l.Warnf("Log rotated for %s, switching to new inode", logPath)
				existing.file.Close()
				delete(followers, logPath)
			} else {
				continue
			}
		}

		var accountID int
		err := db.QueryRow("SELECT account_id FROM domains WHERE domain = ?", domain).Scan(&accountID)
		if err != nil || accountID == 0 {
			continue
		}

		f, err := os.Open(logPath)
		if err != nil {
			continue
		}

		fi, err := f.Stat()
		if err != nil {
			f.Close()
			continue
		}
		curIno := fileInode(fi)
		endPos, _ := f.Seek(0, 2)

		var startPos int64
		if p, ok := loadNginxLogProgress(db, domain); ok && p.ino > 0 {
			if p.ino == curIno {
				startPos = p.offset
				if startPos > endPos {
					startPos = endPos
				}
			} else {
				startPos = 0
			}
		} else {
			startPos = endPos
		}

		f.Seek(startPos, 0)

		if err := saveNginxLogProgress(db, domain, curIno, startPos); err != nil {
			l.Warnf("cannot persist progress for %s, will retry next poll: %v", logPath, err)
		}

		followers[logPath] = &logFollower{
			file:      f,
			path:      logPath,
			domain:    domain,
			accountID: accountID,
			position:  startPos,
			ino:       curIno,
		}
		l.Infof("Following log: %s (account %d, starting at pos %d/%d)", logPath, accountID, startPos, endPos)
	}
}

func pollLogFiles(db *sql.DB, followers map[string]*logFollower) {
	l := logging.NewDefault("bandwidth")
	for path, fw := range followers {
		fi, err := fw.file.Stat()
		if err != nil {
			l.Errorf("stat error for %s: %v, removing follower", path, err)
			fw.file.Close()
			delete(followers, path)
			continue
		}

		if fw.position > fi.Size() {
			fw.file.Seek(0, 0)
			fw.position = 0
		}

		scanner := bufio.NewScanner(fw.file)
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

		if err := scanner.Err(); err != nil {
			l.Errorf("scanner error for %s: %v", path, err)
			continue
		}

		pos, err := fw.file.Seek(0, 1)
		if err != nil {
			l.Errorf("seek error for %s: %v, removing follower", path, err)
			fw.file.Close()
			delete(followers, path)
			continue
		}
		fw.position = pos

		if err := saveNginxLogProgress(db, fw.domain, fw.ino, pos); err != nil {
			l.Warnf("cannot persist progress for %s, recount limited to this poll on restart: %v", path, err)
		}

		if totalBytes > 0 && fw.accountID > 0 {
			today := time.Now().Format("2006-01-02")
			if _, err := db.Exec(`INSERT INTO bandwidth_logs (account_id, bytes_in, bytes_out, logged_at) VALUES (?, ?, ?, ?) ON CONFLICT(account_id, logged_at) DO UPDATE SET bytes_out = bytes_out + ?, bytes_in = bytes_in + ?`,
				fw.accountID, 0, totalBytes, today, totalBytes, 0); err != nil {
				l.Errorf("poller insert error for account %d: %v", fw.accountID, err)
			}
			if _, err := db.Exec("UPDATE accounts SET bandwidth_used_mb = ROUND((SELECT COALESCE(SUM(bytes_out + bytes_in), 0) FROM bandwidth_logs WHERE account_id = ?) / 1048576.0, 2) WHERE id = ?",
				fw.accountID, fw.accountID); err != nil {
				l.Errorf("poller update accounts error for account %d: %v", fw.accountID, err)
			}
		}
	}
}

func trackSMTPBandwidth(db *sql.DB, emailAddr string, bytes int64) {
	l := logging.NewDefault("bandwidth")
	var hostAccountID int
	err := db.QueryRow("SELECT account_id FROM email_accounts WHERE email = ?", emailAddr).Scan(&hostAccountID)
	if err != nil || hostAccountID == 0 {
		return
	}
	today := time.Now().Format("2006-01-02")
	if _, err := db.Exec(`INSERT INTO bandwidth_logs (account_id, bytes_in, bytes_out, logged_at) VALUES (?, ?, ?, ?) ON CONFLICT(account_id, logged_at) DO UPDATE SET bytes_out = bytes_out + ?, bytes_in = bytes_in + ?`,
		hostAccountID, bytes, 0, today, 0, bytes); err != nil {
		l.Errorf("SMTP insert error for account %d: %v", hostAccountID, err)
		return
	}
	if _, err := db.Exec("UPDATE accounts SET bandwidth_used_mb = ROUND((SELECT COALESCE(SUM(bytes_out + bytes_in), 0) FROM bandwidth_logs WHERE account_id = ?) / 1048576.0, 2) WHERE id = ?",
		hostAccountID, hostAccountID); err != nil {
		l.Errorf("SMTP update accounts error for account %d: %v", hostAccountID, err)
	}
}

func bandwidthRoutes(r chi.Router, db *sql.DB) {
	r.Get("/summary", func(w http.ResponseWriter, r *http.Request) {
		var totalBytes int64
		db.QueryRow("SELECT COALESCE(SUM(bytes_out + bytes_in), 0) FROM bandwidth_logs WHERE logged_at >= date('now', '-30 days')").Scan(&totalBytes)

		rows, err := db.Query(`SELECT logged_at, SUM(bytes_out) as bytes_out, SUM(bytes_in) as bytes_in
			FROM bandwidth_logs
			WHERE logged_at >= date('now', '-30 days')
			GROUP BY logged_at ORDER BY logged_at DESC`)
		if err != nil {
			getLogger(r).Errorf("bandwidth summary query: %v", err)
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
			if err := rows.Scan(&p.Date, &p.BytesOut, &p.BytesIn); err != nil {
				getLogger(r).Warnf("scan bandwidth day row: %v", err)
				continue
			}
			days = append(days, p)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("bandwidth days rows iteration error: %v", err)
		}

		jsonResp(w, 200, map[string]interface{}{
			"total_bytes": totalBytes,
			"days":        days,
		})
	})

	r.Get("/accounts", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT a.id, a.username, a.domain,
			COALESCE(SUM(bl.bytes_out + bl.bytes_in), 0) as total_bytes,
			COALESCE(CAST(SUM(bl.bytes_out + bl.bytes_in) AS REAL)/1048576.0, 0) as used_mb
			FROM accounts a
			LEFT JOIN bandwidth_logs bl ON bl.account_id = a.id
			GROUP BY a.id ORDER BY total_bytes DESC`)
		if err != nil {
			getLogger(r).Errorf("bandwidth accounts query: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		accounts := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id int
			var username, domain string
			var bytes int64
			var mb float64
			if err := rows.Scan(&id, &username, &domain, &bytes, &mb); err != nil {
				getLogger(r).Warnf("scan bandwidth account row: %v", err)
				continue
			}
			accounts = append(accounts, map[string]interface{}{
				"id": id, "username": username, "domain": domain,
				"bytes": bytes, "used_mb": mb,
			})
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("bandwidth accounts rows iteration error: %v", err)
		}
		jsonResp(w, 200, accounts)
	})
}

func childBandwidthRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
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
			getLogger(r).Errorf("child bandwidth query: %v", err)
			jsonResp(w, 200, map[string]interface{}{
				"total_bytes":   totalBytes,
				"used_mb":       float64(totalBytes) / 1048576.0,
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
			if err := rows.Scan(&p.Date, &p.BytesIn, &p.BytesOut); err != nil {
				getLogger(r).Warnf("scan bandwidth child row: %v", err)
				continue
			}
			days = append(days, p)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("child bandwidth rows iteration error: %v", err)
		}

		jsonResp(w, 200, map[string]interface{}{
			"total_bytes":   totalBytes,
			"used_mb":       float64(totalBytes) / 1048576.0,
			"limit_mb":      limitMB,
			"usage_percent": usagePercent,
			"days":          days,
		})
	})
}
