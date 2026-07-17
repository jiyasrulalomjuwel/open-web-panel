package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

var nginxLogRe = regexp.MustCompile(`^(\S+) \S+ \S+ \[([^\]]+)\] "([^"]*)" (\d+) (\d+) "([^"]*)" "([^"]*)"`)

func childStatsRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		// Get all domains for this account
		rows, err := db.Query("SELECT domain FROM domains WHERE account_id = ?", c.AccountID)
		if err != nil {
			jsonResp(w, 200, map[string]interface{}{
				"total_visitors":       0,
				"total_bandwidth_bytes": 0,
				"top_pages":            []map[string]interface{}{},
				"recent_hits":          []map[string]interface{}{},
			})
			return
		}
		defer rows.Close()

		uniqueIPs := make(map[string]bool)
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

			logPath := fmt.Sprintf("%s/%s.access.log", logDir, domain)
			f, err := os.Open(logPath)
			if err != nil {
				continue // skip if no log file
			}

			scanner := bufio.NewScanner(f)
			// Increase buffer for long lines
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

				// Extract path from request line "METHOD /path HTTP/1.1"
				path := "/"
				parts := strings.SplitN(requestLine, " ", 3)
				if len(parts) >= 2 {
					path = parts[1]
				}

				uniqueIPs[ip] = true
				pageHits[path]++
				pageBW[path] += bytes
				totalBandwidth += bytes

				if referer != "-" && referer != "" {
					refHost := extractReferrerHost(referer)
					referrers[refHost]++
				}

				// Extract date (YYYY-MM-DD) from timestamp for daily grouping
				if len(timestamp) >= 10 {
					day := timestamp[:10]
					hitsByDay[day]++
				}

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
			f.Close()
		}
		if err := rows.Err(); err != nil {
			log.Printf("[STATS] rows iteration error: %v", err)
		}

		// Build top pages sorted by hit count
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

		// Keep only the last 50 recent hits
		if len(recentHits) > 50 {
			recentHits = recentHits[len(recentHits)-50:]
		}

				// Convert recentHits to []map for JSON output
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

		// Sort and limit referrers
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

		// Build daily traffic
		type dayStat struct {
			Date  string `json:"date"`
			Hits  int    `json:"hits"`
			Visits int   `json:"visits"`
		}
		var trafficByDay []dayStat
		for day, hits := range hitsByDay {
			trafficByDay = append(trafficByDay, dayStat{Date: day, Hits: hits, Visits: hits})
		}
		sort.Slice(trafficByDay, func(i, j int) bool {
			return trafficByDay[i].Date < trafficByDay[j].Date
		})

		jsonResp(w, 200, map[string]interface{}{
			"total_visitors":        len(uniqueIPs),
			"total_bandwidth_bytes": totalBandwidth,
			"top_pages":             topPages,
			"top_referrers":         topReferrers,
			"traffic_by_day":        trafficByDay,
			"recent_hits":           recentJSON,
		})
	})
}

func extractReferrerHost(referer string) string {
	// Strip protocol and path from referrer URL
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
