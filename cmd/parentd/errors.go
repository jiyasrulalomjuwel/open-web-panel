package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
)

const (
	owpErrorPagesBegin = "# OWP-ERRORPAGES-BEGIN (managed by OpenWebPanel, do not edit)"
	owpErrorPagesEnd   = "# OWP-ERRORPAGES-END"
	maxErrorPageBytes  = 256 * 1024
)

// allowedErrorCodes is the set renderable as nginx error_page directives.
var allowedErrorCodes = map[int]bool{
	400: true, 401: true, 403: true, 404: true, 405: true, 408: true,
	500: true, 502: true, 503: true, 504: true,
}

// syncDomainErrorPages renders enabled custom_html error pages for one domain
// into nginx: content files under the account home plus error_page directives
// injected into every matching server block (:80 + :443).
func syncDomainErrorPages(db *sql.DB, dom string) error {
	nginxMu.Lock()
	defer nginxMu.Unlock()
	return doSyncDomainErrorPages(db, dom)
}

func doSyncDomainErrorPages(db *sql.DB, dom string) error {
	rows, err := db.Query(`SELECT ep.error_code, ep.content, ep.domain_id, d.account_id FROM error_pages ep
		JOIN domains d ON d.id = ep.domain_id
		JOIN accounts a ON a.id = ep.account_id
		WHERE d.domain = ? AND a.status = 'active'
		AND COALESCE(ep.enabled, 0) = 1 AND COALESCE(ep.action_type, '') = 'custom_html'`, dom)
	if err != nil {
		return err
	}
	defer rows.Close()
	type page struct {
		code      int
		content   string
		accountID int
	}
	var pages []page
	for rows.Next() {
		var p page
		var content string
		if err := rows.Scan(&p.code, &content, new(int), &p.accountID); err != nil {
			continue
		}
		// Last row wins per code; keep it simple and deterministic.
		if !allowedErrorCodes[p.code] || len(content) == 0 || len(content) > maxErrorPageBytes {
			continue
		}
		p.content = content
		pages = append(pages, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	byCode := map[int]page{}
	for _, p := range pages {
		byCode[p.code] = p
	}
	safe := sanitizeDomain(dom)
	var b strings.Builder
	for code, p := range byCode {
		var homeDir string
		db.QueryRow("SELECT home_dir FROM accounts WHERE id = ?", p.accountID).Scan(&homeDir)
		if homeDir == "" {
			continue
		}
		dir := filepath.Join(homeDir, ".owp-errors", safe)
		if err := os.MkdirAll(dir, 0755); err != nil {
			continue
		}
		fp := filepath.Join(dir, fmt.Sprintf("%d.html", code))
		if err := os.WriteFile(fp, []byte(p.content), 0644); err != nil {
			continue
		}
		uri := fmt.Sprintf("/__owp_err_%d.html", code)
		fmt.Fprintf(&b, "    error_page %d %s;\n", code, uri)
		fmt.Fprintf(&b, "    location = %s { internal; alias %s; }\n", uri, fp)
	}
	vhostPath := vhostDir + safe + ".conf"
	raw, err := os.ReadFile(vhostPath)
	if err != nil {
		return nil // no vhost (yet): sync restores it later
	}
	updated := injectManagedServerBlock(string(raw), dom, owpErrorPagesBegin, owpErrorPagesEnd, b.String())
	if updated == string(raw) {
		return nil
	}
	if err := writeVhostFile(vhostPath, []byte(updated), 0644); err != nil {
		return err
	}
	return reloadNginxErr()
}

// domainForErrorPage resolves the domain name owning an error-page row.
func domainForErrorPage(db *sql.DB, pageID, accountID int) string {
	var dom string
	db.QueryRow(`SELECT d.domain FROM error_pages ep JOIN domains d ON d.id = ep.domain_id
		WHERE ep.id = ? AND ep.domain_id IN (SELECT id FROM domains WHERE account_id = ?)`,
		pageID, accountID).Scan(&dom)
	return dom
}

func childErrorRoutes(r chi.Router, db *sql.DB) {
	r.Get("/recent", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}

		logDir := getNginxLogDir()
		rows, err := db.Query("SELECT domain FROM domains WHERE account_id = ?", c.AccountID)
		if err != nil {
			getLogger(r).Errorf("error page domains query: %v", err)
			jsonResp(w, 200, []map[string]string{})
			return
		}
		defer rows.Close()

		type errorEntry struct {
			Domain string `json:"domain"`
			Line   string `json:"line"`
			Level  string `json:"level"`
			Time   string `json:"time"`
		}
		var errors []errorEntry

		for rows.Next() {
			var domain string
			if err := rows.Scan(&domain); err != nil {
				continue
			}
			domain = sanitizeDomain(domain)
			logPath := filepath.Join(logDir, domain+".error.log")
			data, err := os.ReadFile(logPath)
			if err != nil {
				continue
			}
			lines := strings.Split(string(data), "\n")
			start := 0
			if len(lines) > 50 {
				start = len(lines) - 50
			}
			for _, line := range lines[start:] {
				if line == "" {
					continue
				}
				level := "error"
				if strings.Contains(line, "warn") || strings.Contains(line, "WARN") {
					level = "warn"
				}
				if strings.Contains(line, "critical") || strings.Contains(line, "CRIT") {
					level = "critical"
				}
				timeStr := ""
				if idx := strings.Index(line, "["); idx >= 0 {
					if end := strings.Index(line[idx:], "]"); end >= 0 {
						timeStr = line[idx+1 : idx+end]
					}
				}
				errors = append(errors, errorEntry{
					Domain: domain,
					Line:   line,
					Level:  level,
					Time:   timeStr,
				})
			}
		}
		if rows.Err() != nil {
			getLogger(r).Errorf("error recent rows iteration error: %v", rows.Err())
		}
		if errors == nil {
			errors = []errorEntry{}
		}
		jsonResp(w, 200, errors)
	})

	r.Get("/custom", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		domainIDStr := r.URL.Query().Get("domain_id")
		domainID, _ := strconv.Atoi(domainIDStr)

		var rows *sql.Rows
		var err error
		if domainID > 0 {
			rows, err = db.Query(`SELECT ep.id, ep.domain_id, ep.domain, ep.error_code, ep.action_type,
				ep.action_value, ep.enabled, ep.hit_count, ep.last_triggered_at, ep.language,
				ep.seo_noindex, ep.seo_nofollow, ep.seo_canonical, ep.updated_at
				FROM error_pages ep
				JOIN domains d ON d.id = ep.domain_id
				WHERE d.account_id = ? AND ep.domain_id = ?
				ORDER BY ep.error_code`, c.AccountID, domainID)
		}
		if rows == nil {
			rows, err = db.Query(`SELECT ep.id, ep.domain_id, ep.domain, ep.error_code, ep.action_type,
				ep.action_value, ep.enabled, ep.hit_count, ep.last_triggered_at, ep.language,
				ep.seo_noindex, ep.seo_nofollow, ep.seo_canonical, ep.updated_at
				FROM error_pages ep
				JOIN domains d ON d.id = ep.domain_id
				WHERE d.account_id = ?
				ORDER BY ep.domain, ep.error_code`, c.AccountID)
		}
		if err != nil {
			getLogger(r).Errorf("custom error pages query: %v", err)
			jsonResp(w, 200, []map[string]interface{}{})
			return
		}
		defer rows.Close()

		type customPage struct {
			ID              int    `json:"id"`
			DomainID        int    `json:"domain_id"`
			Domain          string `json:"domain"`
			ErrorCode       int    `json:"error_code"`
			ActionType      string `json:"action_type"`
			ActionValue     string `json:"action_value"`
			Enabled         bool   `json:"enabled"`
			HitCount        int    `json:"hit_count"`
			LastTriggeredAt string `json:"last_triggered_at"`
			Language        string `json:"language"`
			SeoNoindex      bool   `json:"seo_noindex"`
			SeoNofollow     bool   `json:"seo_nofollow"`
			SeoCanonical    string `json:"seo_canonical"`
			UpdatedAt       string `json:"updated_at"`
		}
		pages := make([]customPage, 0)
		for rows.Next() {
			var p customPage
			var enabled, seoNoindex, seoNofollow int
			if err := rows.Scan(&p.ID, &p.DomainID, &p.Domain, &p.ErrorCode, &p.ActionType,
				&p.ActionValue, &enabled, &p.HitCount, &p.LastTriggeredAt, &p.Language,
				&seoNoindex, &seoNofollow, &p.SeoCanonical, &p.UpdatedAt); err != nil {
				continue
			}
			p.Enabled = enabled == 1
			p.SeoNoindex = seoNoindex == 1
			p.SeoNofollow = seoNofollow == 1
			pages = append(pages, p)
		}
		if rows.Err() != nil {
			getLogger(r).Errorf("custom pages rows iteration error: %v", rows.Err())
		}
		if pages == nil {
			pages = []customPage{}
		}
		jsonResp(w, 200, pages)
	})

	r.Get("/custom/stats", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		domainIDStr := r.URL.Query().Get("domain_id")
		if domainIDStr == "" {
			writeAppError(w, r, apperrors.Validation("domain_id required"))
			return
		}
		domainID, _ := strconv.Atoi(domainIDStr)

		var totalPages, enabledPages, totalHits int
		var lastTriggered string
		err := db.QueryRow(`SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN enabled=1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(hit_count), 0),
			COALESCE(MAX(last_triggered_at), '')
			FROM error_pages WHERE domain_id = ? AND domain_id IN (SELECT id FROM domains WHERE account_id = ?)`,
			domainID, c.AccountID).Scan(&totalPages, &enabledPages, &totalHits, &lastTriggered)
		if err != nil {
			getLogger(r).Warnf("custom stats query: %v", err)
		}
		jsonResp(w, 200, map[string]interface{}{
			"total_pages":    totalPages,
			"enabled_pages":  enabledPages,
			"total_hits":     totalHits,
			"last_triggered": lastTriggered,
		})
	})

	r.Get("/custom/export", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		domainIDStr := r.URL.Query().Get("domain_id")
		if domainIDStr == "" {
			writeAppError(w, r, apperrors.Validation("domain_id required"))
			return
		}
		domainID, _ := strconv.Atoi(domainIDStr)

		rows, err := db.Query(`SELECT error_code, content, action_type, action_value, enabled,
			custom_headers, custom_footer, seo_noindex, seo_nofollow, seo_canonical, template, language
			FROM error_pages WHERE domain_id = ? AND domain_id IN (SELECT id FROM domains WHERE account_id = ?)`,
			domainID, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		defer rows.Close()

		type exportItem struct {
			ErrorCode     int    `json:"error_code"`
			Content       string `json:"content"`
			ActionType    string `json:"action_type"`
			ActionValue   string `json:"action_value"`
			Enabled       bool   `json:"enabled"`
			CustomHeaders string `json:"custom_headers"`
			CustomFooter  string `json:"custom_footer"`
			SeoNoindex    bool   `json:"seo_noindex"`
			SeoNofollow   bool   `json:"seo_nofollow"`
			SeoCanonical  string `json:"seo_canonical"`
			Template      string `json:"template"`
			Language      string `json:"language"`
		}
		items := make([]exportItem, 0)
		for rows.Next() {
			var item exportItem
			var enabled, seoNoindex, seoNofollow int
			if err := rows.Scan(&item.ErrorCode, &item.Content, &item.ActionType, &item.ActionValue,
				&enabled, &item.CustomHeaders, &item.CustomFooter,
				&seoNoindex, &seoNofollow, &item.SeoCanonical, &item.Template, &item.Language); err != nil {
				continue
			}
			item.Enabled = enabled == 1
			item.SeoNoindex = seoNoindex == 1
			item.SeoNofollow = seoNofollow == 1
			items = append(items, item)
		}
		if rows.Err() != nil {
			writeAppError(w, r, apperrors.Internal("failed to read all rows", nil))
			return
		}
		jsonResp(w, 200, map[string]interface{}{
			"export_version": "1.0",
			"exported_at":    time.Now().UTC().Format(time.RFC3339),
			"pages":          items,
		})
	})

	r.Get("/custom/by-domain/{domain_id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		domainID, err := paramInt(r, "domain_id")
		if err != nil || domainID <= 0 {
			writeAppError(w, r, apperrors.Validation("invalid domain_id"))
			return
		}

		var ownerID int
		err = db.QueryRow("SELECT account_id FROM domains WHERE id = ?", domainID).Scan(&ownerID)
		if err != nil || ownerID != c.AccountID {
			writeAppError(w, r, apperrors.NotFound("domain", domainID))
			return
		}

		type domainPage struct {
			ID              int    `json:"id"`
			ErrorCode       int    `json:"error_code"`
			ActionType      string `json:"action_type"`
			ActionValue     string `json:"action_value"`
			Enabled         bool   `json:"enabled"`
			HitCount        int    `json:"hit_count"`
			LastTriggeredAt string `json:"last_triggered_at"`
			Content         string `json:"content"`
			CustomHeaders   string `json:"custom_headers"`
			CustomFooter    string `json:"custom_footer"`
			SeoNoindex      bool   `json:"seo_noindex"`
			SeoNofollow     bool   `json:"seo_nofollow"`
			SeoCanonical    string `json:"seo_canonical"`
			Template        string `json:"template"`
			Language        string `json:"language"`
		}

		rows, err := db.Query(`SELECT id, error_code, action_type, action_value, enabled,
			hit_count, last_triggered_at, content, custom_headers, custom_footer,
			seo_noindex, seo_nofollow, seo_canonical, template, language
			FROM error_pages WHERE domain_id = ? ORDER BY error_code`, domainID)
		if err != nil {
			getLogger(r).Errorf("by-domain query: %v", err)
			jsonResp(w, 200, []domainPage{})
			return
		}
		defer rows.Close()

		pages := make([]domainPage, 0)
		for rows.Next() {
			var p domainPage
			var enabled, seoNoindex, seoNofollow int
			if err := rows.Scan(&p.ID, &p.ErrorCode, &p.ActionType, &p.ActionValue, &enabled,
				&p.HitCount, &p.LastTriggeredAt, &p.Content, &p.CustomHeaders, &p.CustomFooter,
				&seoNoindex, &seoNofollow, &p.SeoCanonical, &p.Template, &p.Language); err != nil {
				continue
			}
			p.Enabled = enabled == 1
			p.SeoNoindex = seoNoindex == 1
			p.SeoNofollow = seoNofollow == 1
			pages = append(pages, p)
		}
		if rows.Err() != nil {
			getLogger(r).Errorf("by-domain rows iteration error: %v", rows.Err())
		}
		jsonResp(w, 200, pages)
	})

	r.Post("/custom/hit", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		var req struct {
			DomainID  int `json:"domain_id"`
			ErrorCode int `json:"error_code"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)
		result, err := db.Exec(`UPDATE error_pages SET hit_count = hit_count + 1, last_triggered_at = ?
			WHERE domain_id = ? AND error_code = ? AND domain_id IN (SELECT id FROM domains WHERE account_id = ?)`,
			now, req.DomainID, req.ErrorCode, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		n, _ := result.RowsAffected()
		jsonResp(w, 200, map[string]interface{}{
			"status":  "recorded",
			"updated": n > 0,
		})
	})

	r.Post("/custom/import", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		var req struct {
			DomainID int          `json:"domain_id"`
			Pages    []exportItem `json:"pages"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		if req.DomainID == 0 || len(req.Pages) == 0 {
			writeAppError(w, r, apperrors.Validation("domain_id and pages required"))
			return
		}

		var domainAccountID int
		err := db.QueryRow("SELECT account_id FROM domains WHERE id = ?", req.DomainID).Scan(&domainAccountID)
		if err != nil || domainAccountID != c.AccountID {
			writeAppError(w, r, apperrors.Forbidden("domain does not belong to your account"))
			return
		}

		var domain string
		err = db.QueryRow("SELECT domain FROM domains WHERE id = ?", req.DomainID).Scan(&domain)
		if err != nil {
			writeAppError(w, r, apperrors.Internal("failed to resolve domain", err))
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)
		imported := 0
		var lastErr error

		for _, p := range req.Pages {
			enabled := 0
			if p.Enabled {
				enabled = 1
			}
			seoNoindex := 0
			if p.SeoNoindex {
				seoNoindex = 1
			}
			seoNofollow := 0
			if p.SeoNofollow {
				seoNofollow = 1
			}
			if p.ActionType == "" {
				p.ActionType = "custom_html"
			}
			if p.Language == "" {
				p.Language = "en"
			}
			_, err := db.Exec(`INSERT OR REPLACE INTO error_pages
				(account_id, domain_id, domain, error_code, content, action_type, action_value,
				enabled, custom_headers, custom_footer, seo_noindex, seo_nofollow, seo_canonical,
				template, language, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				c.AccountID, req.DomainID, domain, p.ErrorCode, p.Content,
				p.ActionType, p.ActionValue, enabled,
				p.CustomHeaders, p.CustomFooter, seoNoindex, seoNofollow,
				p.SeoCanonical, p.Template, p.Language, now)
			if err == nil {
				imported++
			} else {
				lastErr = err
			}
		}
		auditLog(db, r, "error_page.import", logging.Fields{
			"domain_id": req.DomainID, "imported": imported, "total": len(req.Pages),
		})
		status := "imported"
		if lastErr != nil {
			status = "partial"
		}
		jsonResp(w, 200, map[string]interface{}{
			"status":   status,
			"imported": imported,
			"total":    len(req.Pages),
			"error":    lastErr != nil,
		})
	})

	r.Get("/custom/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		id, err := paramInt(r, "id")
		if err != nil || id <= 0 {
			writeAppError(w, r, apperrors.Validation("invalid id"))
			return
		}

		type fullPage struct {
			ID              int    `json:"id"`
			DomainID        int    `json:"domain_id"`
			Domain          string `json:"domain"`
			ErrorCode       int    `json:"error_code"`
			Content         string `json:"content"`
			ActionType      string `json:"action_type"`
			ActionValue     string `json:"action_value"`
			Enabled         bool   `json:"enabled"`
			HitCount        int    `json:"hit_count"`
			LastTriggeredAt string `json:"last_triggered_at"`
			CustomHeaders   string `json:"custom_headers"`
			CustomFooter    string `json:"custom_footer"`
			SeoNoindex      bool   `json:"seo_noindex"`
			SeoNofollow     bool   `json:"seo_nofollow"`
			SeoCanonical    string `json:"seo_canonical"`
			Template        string `json:"template"`
			Language        string `json:"language"`
			CreatedAt       string `json:"created_at"`
			UpdatedAt       string `json:"updated_at"`
		}
		var p fullPage
		err = db.QueryRow(`SELECT ep.id, ep.domain_id, ep.domain, ep.error_code, ep.content,
			ep.action_type, ep.action_value, ep.enabled, ep.hit_count, ep.last_triggered_at,
			ep.custom_headers, ep.custom_footer, ep.seo_noindex, ep.seo_nofollow, ep.seo_canonical,
			ep.template, ep.language, ep.created_at, ep.updated_at
			FROM error_pages ep
			JOIN domains d ON d.id = ep.domain_id
			WHERE ep.id = ? AND d.account_id = ?`, id, c.AccountID).Scan(
			&p.ID, &p.DomainID, &p.Domain, &p.ErrorCode, &p.Content,
			&p.ActionType, &p.ActionValue, &p.Enabled, &p.HitCount, &p.LastTriggeredAt,
			&p.CustomHeaders, &p.CustomFooter, &p.SeoNoindex, &p.SeoNofollow, &p.SeoCanonical,
			&p.Template, &p.Language, &p.CreatedAt, &p.UpdatedAt)
		if err == sql.ErrNoRows {
			writeAppError(w, r, apperrors.NotFound("error page", id))
			return
		}
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		jsonResp(w, 200, p)
	})

	r.Put("/custom", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		var req struct {
			ID            *int   `json:"id"`
			DomainID      int    `json:"domain_id"`
			ErrorCode     int    `json:"error_code"`
			Content       string `json:"content"`
			ActionType    string `json:"action_type"`
			ActionValue   string `json:"action_value"`
			Enabled       *bool  `json:"enabled"`
			CustomHeaders string `json:"custom_headers"`
			CustomFooter  string `json:"custom_footer"`
			SeoNoindex    *bool  `json:"seo_noindex"`
			SeoNofollow   *bool  `json:"seo_nofollow"`
			SeoCanonical  string `json:"seo_canonical"`
			Template      string `json:"template"`
			Language      string `json:"language"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		if req.DomainID <= 0 {
			writeAppError(w, r, apperrors.Validation("domain_id is required"))
			return
		}
		if req.ActionType == "" {
			req.ActionType = "custom_html"
		}
		if req.ActionType == "custom_html" {
			if !allowedErrorCodes[req.ErrorCode] {
				writeAppError(w, r, apperrors.Validation("error_code must be one of 400, 401, 403, 404, 405, 408, 500, 502, 503, 504"))
				return
			}
			if len(req.Content) > maxErrorPageBytes {
				writeAppError(w, r, apperrors.Validation("content too large (max 256KB)"))
				return
			}
		}
		if req.Language == "" {
			req.Language = "en"
		}

		var domainAccountID int
		err := db.QueryRow("SELECT account_id FROM domains WHERE id = ?", req.DomainID).Scan(&domainAccountID)
		if err != nil || domainAccountID != c.AccountID {
			writeAppError(w, r, apperrors.Forbidden("domain does not belong to your account"))
			return
		}

		now := time.Now().UTC().Format(time.RFC3339)
		enabled := 1
		if req.Enabled != nil && !*req.Enabled {
			enabled = 0
		}
		seoNoindex := 0
		if req.SeoNoindex != nil && *req.SeoNoindex {
			seoNoindex = 1
		}
		seoNofollow := 0
		if req.SeoNofollow != nil && *req.SeoNofollow {
			seoNofollow = 1
		}

		if req.ID != nil && *req.ID > 0 {
			result, err := db.Exec(`UPDATE error_pages SET
				content=?, error_code=?, domain_id=?, action_type=?, action_value=?,
				enabled=?, custom_headers=?, custom_footer=?, seo_noindex=?, seo_nofollow=?,
				seo_canonical=?, template=?, language=?, updated_at=?
				WHERE id=? AND domain_id IN (SELECT id FROM domains WHERE account_id=?)`,
				req.Content, req.ErrorCode, req.DomainID, req.ActionType, req.ActionValue,
				enabled, req.CustomHeaders, req.CustomFooter, seoNoindex, seoNofollow,
				req.SeoCanonical, req.Template, req.Language, now,
				*req.ID, c.AccountID)
			if err != nil {
				writeAppError(w, r, apperrors.Database(err))
				return
			}
			n, _ := result.RowsAffected()
			if n == 0 {
				writeAppError(w, r, apperrors.NotFound("error page", *req.ID))
				return
			}
		} else {
			var domain string
			err := db.QueryRow("SELECT domain FROM domains WHERE id = ?", req.DomainID).Scan(&domain)
			if err != nil {
				writeAppError(w, r, apperrors.Internal("failed to resolve domain", err))
				return
			}
			_, err = db.Exec(`INSERT OR REPLACE INTO error_pages
				(account_id, domain_id, domain, error_code, content, action_type, action_value,
				enabled, custom_headers, custom_footer, seo_noindex, seo_nofollow, seo_canonical,
				template, language, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				c.AccountID, req.DomainID, domain, req.ErrorCode, req.Content,
				req.ActionType, req.ActionValue, enabled,
				req.CustomHeaders, req.CustomFooter, seoNoindex, seoNofollow,
				req.SeoCanonical, req.Template, req.Language, now)
			if err != nil {
				writeAppError(w, r, apperrors.Database(err))
				return
			}
		}
		auditLog(db, r, "error_page.save", logging.Fields{
			"domain_id": req.DomainID, "error_code": req.ErrorCode, "action_type": req.ActionType,
		})
		var saveDomain string
		db.QueryRow("SELECT domain FROM domains WHERE id = ?", req.DomainID).Scan(&saveDomain)
		if saveDomain != "" {
			if err := syncDomainErrorPages(db, saveDomain); err != nil {
				getLogger(r).Warnf("error-page nginx sync failed for %s: %v", saveDomain, err)
			}
		}
		jsonResp(w, 200, map[string]string{"status": "saved"})
	})

	r.Delete("/custom/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		id, err := paramInt(r, "id")
		if err != nil || id <= 0 {
			writeAppError(w, r, apperrors.Validation("invalid id"))
			return
		}
		var delDomain string
		db.QueryRow(`SELECT d.domain FROM error_pages ep JOIN domains d ON d.id = ep.domain_id
			WHERE ep.id = ? AND ep.domain_id IN (SELECT id FROM domains WHERE account_id = ?)`, id, c.AccountID).Scan(&delDomain)
		result, err := db.Exec("DELETE FROM error_pages WHERE id = ? AND domain_id IN (SELECT id FROM domains WHERE account_id = ?)", id, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			writeAppError(w, r, apperrors.NotFound("error page", id))
			return
		}
		auditLog(db, r, "error_page.delete", logging.Fields{"id": id})
		if delDomain != "" {
			if err := syncDomainErrorPages(db, delDomain); err != nil {
				getLogger(r).Warnf("error-page nginx sync failed for %s: %v", delDomain, err)
			}
		}
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	r.Post("/custom/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		id, err := paramInt(r, "id")
		if err != nil || id <= 0 {
			writeAppError(w, r, apperrors.Validation("invalid id"))
			return
		}
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		enabled := 0
		if req.Enabled {
			enabled = 1
		}
		now := time.Now().UTC().Format(time.RFC3339)
		result, err := db.Exec("UPDATE error_pages SET enabled=?, updated_at=? WHERE id=? AND domain_id IN (SELECT id FROM domains WHERE account_id=?)",
			enabled, now, id, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			writeAppError(w, r, apperrors.NotFound("error page", id))
			return
		}
		auditLog(db, r, "error_page.toggle", logging.Fields{"id": id, "enabled": req.Enabled})
		if dom := domainForErrorPage(db, id, c.AccountID); dom != "" {
			if err := syncDomainErrorPages(db, dom); err != nil {
				getLogger(r).Warnf("error-page nginx sync failed for %s: %v", dom, err)
			}
		}
		jsonResp(w, 200, map[string]interface{}{"status": "updated", "enabled": req.Enabled})
	})

	r.Post("/custom/{id}/reset", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		id, err := paramInt(r, "id")
		if err != nil || id <= 0 {
			writeAppError(w, r, apperrors.Validation("invalid id"))
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)
		result, err := db.Exec(`UPDATE error_pages SET
			content='', action_type='custom_html', action_value='', custom_headers='',
			custom_footer='', seo_noindex=0, seo_nofollow=0, seo_canonical='', template='',
			language='en', updated_at=?
			WHERE id=? AND domain_id IN (SELECT id FROM domains WHERE account_id=?)`,
			now, id, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			writeAppError(w, r, apperrors.NotFound("error page", id))
			return
		}
		auditLog(db, r, "error_page.reset", logging.Fields{"id": id})
		if dom := domainForErrorPage(db, id, c.AccountID); dom != "" {
			if err := syncDomainErrorPages(db, dom); err != nil {
				getLogger(r).Warnf("error-page nginx sync failed for %s: %v", dom, err)
			}
		}
		jsonResp(w, 200, map[string]string{"status": "reset"})
	})

	r.Post("/custom/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		id, err := paramInt(r, "id")
		if err != nil || id <= 0 {
			writeAppError(w, r, apperrors.Validation("invalid id"))
			return
		}
		var content, actionType, actionValue string
		err = db.QueryRow(`SELECT ep.content, ep.action_type, ep.action_value FROM error_pages ep
			JOIN domains d ON d.id = ep.domain_id
			WHERE ep.id = ? AND d.account_id = ?`, id, c.AccountID).Scan(&content, &actionType, &actionValue)
		if err == sql.ErrNoRows {
			writeAppError(w, r, apperrors.NotFound("error page", id))
			return
		}
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		rendered := content
		if actionType == "internal_redirect" || actionType == "external_redirect" {
			rendered = actionValue
		}
		jsonResp(w, 200, map[string]interface{}{
			"content":     rendered,
			"action_type": actionType,
		})
	})
}

type exportItem struct {
	ErrorCode     int    `json:"error_code"`
	Content       string `json:"content"`
	ActionType    string `json:"action_type"`
	ActionValue   string `json:"action_value"`
	Enabled       bool   `json:"enabled"`
	CustomHeaders string `json:"custom_headers"`
	CustomFooter  string `json:"custom_footer"`
	SeoNoindex    bool   `json:"seo_noindex"`
	SeoNofollow   bool   `json:"seo_nofollow"`
	SeoCanonical  string `json:"seo_canonical"`
	Template      string `json:"template"`
	Language      string `json:"language"`
}
