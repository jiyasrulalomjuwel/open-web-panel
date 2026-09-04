package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
)

var nginxLogRe = regexp.MustCompile(`^(\S+) \S+ \S+ \[([^\]]+)\] "([^"]*)" (\d+) (\d+) "([^"]*)" "([^"]*)"`)

const (
	// statsTotalsWindowDays bounds the aggregated metrics (visitors,
	// bandwidth, page hits, referrers, per-day hits) to the trailing window
	// so historical log entries never inflate the totals.
	statsTotalsWindowDays = 30
	// statsRecentDays bounds the recent_hits list to the trailing window.
	statsRecentDays = 7
	// statsRecentMax caps the number of recent hits returned.
	statsRecentMax = 50
)

// nginxLogTimeLayout matches nginx's $time_local format, e.g.
// 09/Aug/2026:10:30:00 +0000.
var nginxLogTimeLayout = "02/Jan/2006:15:04:05 -0700"

func parseLogLineTime(ts string) (time.Time, bool) {
	t, err := time.Parse(nginxLogTimeLayout, ts)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func withinWindow(t time.Time, days int) bool {
	return time.Since(t) <= time.Duration(days)*24*time.Hour
}

// logDayKey derives the day from the log line's local time portion, keeping
// nginx's server-local time semantics.
func logDayKey(t time.Time) string {
	return t.Format("2006-01-02")
}

func childStatsRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}

		rows, err := db.Query("SELECT domain FROM domains WHERE account_id = ?", c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		defer rows.Close()

		uniqueIPs := make(map[string]bool)
		uniqueIPsByDay := make(map[string]map[string]struct{})
		pageHits := make(map[string]int)
		pageBW := make(map[string]int64)
		hitsByDay := make(map[string]int)
		referrers := make(map[string]int)
		var totalBandwidth int64
		type logHit struct {
			Timestamp string `json:"timestamp"`
			IP        string `json:"ip"`
			Path      string `json:"path"`
			Status    int    `json:"status"`
			Bytes     int64  `json:"bytes"`
			Referer   string `json:"referer"`
			UserAgent string `json:"user_agent"`
		}
		var recentHits []logHit

		logDir := getNginxLogDir()

		for rows.Next() {
			var domain string
			rows.Scan(&domain)

			// Domains come from the DB but old/invalid rows could contain path
			// separators. Refuse anything that isn't a well-formed domain name.
			if !validDomain(domain) {
				continue
			}
			logPath := fmt.Sprintf("%s/%s.access.log", logDir, domain)
			f, err := os.Open(logPath)
			if err != nil {
				continue
			}

			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
			for scanner.Scan() {
				line := scanner.Text()
				m := nginxLogRe.FindStringSubmatch(line)
				if m == nil {
					continue
				}

				ip := m[1]
				timestamp := m[2]
				requestLine := m[3]
				statusStr := m[4]
				bytesStr := m[5]
				referer := m[6]
				userAgent := m[7]

				status, _ := strconv.Atoi(statusStr)
				bytes, _ := strconv.ParseInt(bytesStr, 10, 64)

				path := "/"
				parts := strings.SplitN(requestLine, " ", 3)
				if len(parts) >= 2 {
					path = parts[1]
				}

				logTime, ok := parseLogLineTime(timestamp)
				if !ok {
					continue
				}

				if withinWindow(logTime, statsRecentDays) {
					recentHits = append(recentHits, logHit{
						Timestamp: timestamp,
						IP:        ip,
						Path:      path,
						Status:    status,
						Bytes:     bytes,
						Referer:   referer,
						UserAgent: userAgent,
					})
				}

				if !withinWindow(logTime, statsTotalsWindowDays) {
					continue
				}
				day := logDayKey(logTime)

				uniqueIPs[ip] = true
				pageHits[path]++
				pageBW[path] += bytes
				totalBandwidth += bytes

				if referer != "-" && referer != "" {
					refHost := extractReferrerHost(referer)
					referrers[refHost]++
				}

				hitsByDay[day]++
				if uniqueIPsByDay[day] == nil {
					uniqueIPsByDay[day] = make(map[string]struct{})
				}
				uniqueIPsByDay[day][ip] = struct{}{}
			}
			f.Close()
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("stats rows iteration error: %v", err)
		}

		type pageStat struct {
			Path  string `json:"path"`
			Hits  int    `json:"hits"`
			Bytes int64  `json:"bytes"`
		}
		var topPages []pageStat
		for p, hits := range pageHits {
			topPages = append(topPages, pageStat{Path: p, Hits: hits, Bytes: pageBW[p]})
		}
		sort.Slice(topPages, func(i, j int) bool {
			return topPages[i].Hits > topPages[j].Hits
		})
		if len(topPages) > 10 {
			topPages = topPages[:10]
		}

		if len(recentHits) > statsRecentMax {
			recentHits = recentHits[len(recentHits)-statsRecentMax:]
		}

		recentJSON := make([]map[string]interface{}, len(recentHits))
		for i, h := range recentHits {
			recentJSON[i] = map[string]interface{}{
				"timestamp":  h.Timestamp,
				"ip":         h.IP,
				"path":       h.Path,
				"status":     h.Status,
				"bytes":      h.Bytes,
				"referer":    h.Referer,
				"user_agent": h.UserAgent,
			}
		}

		type refStat struct {
			Referer string `json:"referer"`
			Hits    int    `json:"hits"`
		}
		var topReferrers []refStat
		for ref, hits := range referrers {
			topReferrers = append(topReferrers, refStat{Referer: ref, Hits: hits})
		}
		sort.Slice(topReferrers, func(i, j int) bool {
			return topReferrers[i].Hits > topReferrers[j].Hits
		})
		if len(topReferrers) > 10 {
			topReferrers = topReferrers[:10]
		}

		type dayStat struct {
			Date   string `json:"date"`
			Hits   int    `json:"hits"`
			Visits int    `json:"visits"`
		}
		var trafficByDay []dayStat
		for day, hits := range hitsByDay {
			trafficByDay = append(trafficByDay, dayStat{Date: day, Hits: hits, Visits: len(uniqueIPsByDay[day])})
		}
		sort.Slice(trafficByDay, func(i, j int) bool {
			return trafficByDay[i].Date < trafficByDay[j].Date
		})

		jsonResp(w, 200, map[string]interface{}{
			"total_visitors":         len(uniqueIPs),
			"total_bandwidth_bytes":  totalBandwidth,
			"top_pages":              topPages,
			"top_referrers":          topReferrers,
			"traffic_by_day":         trafficByDay,
			"recent_hits":            recentJSON,
		})
	})
}

func extractReferrerHost(referer string) string {
	ref := referer
	if strings.HasPrefix(ref, "https://") {
		ref = ref[8:]
	} else if strings.HasPrefix(ref, "http://") {
		ref = ref[7:]
	}
	if idx := strings.Index(ref, "/"); idx > 0 {
		ref = ref[:idx]
	}
	if idx := strings.Index(ref, "?"); idx > 0 {
		ref = ref[:idx]
	}
	if ref == "" {
		return "direct"
	}
	return ref
}
