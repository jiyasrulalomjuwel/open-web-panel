package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func childErrorRoutes(r chi.Router, db *sql.DB) {
	r.Get("/recent", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		logDir := getNginxLogDir()
		rows, err := db.Query("SELECT domain FROM domains WHERE account_id = ?", c.AccountID)
		if err != nil {
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
			rows.Scan(&domain)
			logPath := logDir + "/" + domain + ".error.log"
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
			jsonResp(w, 200, []map[string]string{})
			return
		}
		if errors == nil {
			errors = []errorEntry{}
		}
		jsonResp(w, 200, errors)
	})

	// =========================================================================
	// IMPORTANT: Literal routes MUST be registered BEFORE parameterized routes
	// to prevent chi from matching "/custom/stats" against "/custom/{id}".
	// =========================================================================

	// List all custom error pages (optional domain_id query filter)
	r.Get("/custom", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		domainIDStr := r.URL.Query().Get("domain_id")

		var rows *sql.Rows
		var err error
		if domainIDStr != "" {
			domainID, _ := strconv.Atoi(domainIDStr)
			rows, err = db.Query(`SELECT ep.id, ep.domain_id, ep.domain, ep.error_code, ep.action_type,
				ep.action_value, ep.enabled, ep.hit_count, ep.last_triggered_at, ep.language,
				ep.seo_noindex, ep.seo_nofollow, ep.seo_canonical, ep.updated_at
				FROM error_pages ep
				JOIN domains d ON d.id = ep.domain_id
				WHERE d.account_id = ? AND ep.domain_id = ?
				ORDER BY ep.error_code`, c.AccountID, domainID)
		} else {
			rows, err = db.Query(`SELECT ep.id, ep.domain_id, ep.domain, ep.error_code, ep.action_type,
				ep.action_value, ep.enabled, ep.hit_count, ep.last_triggered_at, ep.language,
				ep.seo_noindex, ep.seo_nofollow, ep.seo_canonical, ep.updated_at
				FROM error_pages ep
				JOIN domains d ON d.id = ep.domain_id
				WHERE d.account_id = ?
				ORDER BY ep.domain, ep.error_code`, c.AccountID)
		}
		if err != nil {
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
			jsonResp(w, 200, []map[string]interface{}{})
			return
		}
		if pages == nil {
			pages = []customPage{}
		}
		jsonResp(w, 200, pages)
	})

	// Stats for a domain (transactional, non-cached)
	r.Get("/custom/stats", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		domainIDStr := r.URL.Query().Get("domain_id")
		if domainIDStr == "" {
			jsonError(w, 400, "domain_id required")
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
			totalPages = 0
			enabledPages = 0
			totalHits = 0
			lastTriggered = ""
		}
		jsonResp(w, 200, map[string]interface{}{
			"total_pages":    totalPages,
			"enabled_pages":  enabledPages,
			"total_hits":     totalHits,
			"last_triggered": lastTriggered,
		})
	})

	// Export all error pages for a domain
	r.Get("/custom/export", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		domainIDStr := r.URL.Query().Get("domain_id")
		if domainIDStr == "" {
			jsonError(w, 400, "domain_id required")
			return
		}
		domainID, _ := strconv.Atoi(domainIDStr)

		rows, err := db.Query(`SELECT error_code, content, action_type, action_value, enabled,
			custom_headers, custom_footer, seo_noindex, seo_nofollow, seo_canonical, template, language
			FROM error_pages WHERE domain_id = ? AND domain_id IN (SELECT id FROM domains WHERE account_id = ?)`,
			domainID, c.AccountID)
		if err != nil {
			jsonError(w, 500, err.Error())
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
			jsonError(w, 500, "failed to read all rows")
			return
		}
		jsonResp(w, 200, map[string]interface{}{
			"export_version": "1.0",
			"exported_at":    time.Now().UTC().Format(time.RFC3339),
			"pages":          items,
		})
	})

	// Get all error pages for a specific domain
	r.Get("/custom/by-domain/{domain_id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		domainIDStr := chi.URLParam(r, "domain_id")
		domainID, err := strconv.Atoi(domainIDStr)
		if err != nil || domainID <= 0 {
			jsonError(w, 400, "invalid domain_id")
			return
		}

		// Verify domain belongs to account before querying
		var ownerID int
		err = db.QueryRow("SELECT account_id FROM domains WHERE id = ?", domainID).Scan(&ownerID)
		if err != nil || ownerID != c.AccountID {
			jsonError(w, 404, "domain not found")
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
			jsonResp(w, 200, []domainPage{})
			return
		}
		jsonResp(w, 200, pages)
	})

	// Record a hit
	r.Post("/custom/hit", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		var req struct {
			DomainID  int `json:"domain_id"`
			ErrorCode int `json:"error_code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)
		result, err := db.Exec(`UPDATE error_pages SET hit_count = hit_count + 1, last_triggered_at = ?
			WHERE domain_id = ? AND error_code = ? AND domain_id IN (SELECT id FROM domains WHERE account_id = ?)`,
			now, req.DomainID, req.ErrorCode, c.AccountID)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		n, _ := result.RowsAffected()
		jsonResp(w, 200, map[string]interface{}{
			"status":  "recorded",
			"updated": n > 0,
		})
	})

	// Import error pages for a domain
	r.Post("/custom/import", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		var req struct {
			DomainID int          `json:"domain_id"`
			Pages    []exportItem `json:"pages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}
		if req.DomainID == 0 || len(req.Pages) == 0 {
			jsonError(w, 400, "domain_id and pages required")
			return
		}

		var domainAccountID int
		err := db.QueryRow("SELECT account_id FROM domains WHERE id = ?", req.DomainID).Scan(&domainAccountID)
		if err != nil || domainAccountID != c.AccountID {
			jsonError(w, 403, "domain does not belong to your account")
			return
		}

		var domain string
		err = db.QueryRow("SELECT domain FROM domains WHERE id = ?", req.DomainID).Scan(&domain)
		if err != nil {
			jsonError(w, 500, "failed to resolve domain")
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
		auditLog(db, r, "error_page.import", map[string]interface{}{
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

	// =========================================================================
	// Parameterized routes — registered AFTER all literal routes
	// =========================================================================

	// Get single error page with full content
	r.Get("/custom/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			jsonError(w, 400, "invalid id")
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
		var content, actionType, actionValue, customHeaders, customFooter, seoCanonical, template, lang string
		var enabled, seoNoindex, seoNofollow int
		var hitCount int
		var lastTriggeredAt, createdAt, updatedAt string

		err = db.QueryRow(`SELECT ep.id, ep.domain_id, ep.domain, ep.error_code, ep.content,
			ep.action_type, ep.action_value, ep.enabled, ep.hit_count, ep.last_triggered_at,
			ep.custom_headers, ep.custom_footer, ep.seo_noindex, ep.seo_nofollow, ep.seo_canonical,
			ep.template, ep.language, ep.created_at, ep.updated_at
			FROM error_pages ep
			JOIN domains d ON d.id = ep.domain_id
			WHERE ep.id = ? AND d.account_id = ?`, id, c.AccountID).Scan(
			&p.ID, &p.DomainID, &p.Domain, &p.ErrorCode, &content,
			&actionType, &actionValue, &enabled, &hitCount, &lastTriggeredAt,
			&customHeaders, &customFooter, &seoNoindex, &seoNofollow, &seoCanonical,
			&template, &lang, &createdAt, &updatedAt)
		if err == sql.ErrNoRows {
			jsonError(w, 404, "not found")
			return
		}
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		p.Content = content
		p.ActionType = actionType
		p.ActionValue = actionValue
		p.Enabled = enabled == 1
		p.HitCount = hitCount
		p.LastTriggeredAt = lastTriggeredAt
		p.CustomHeaders = customHeaders
		p.CustomFooter = customFooter
		p.SeoNoindex = seoNoindex == 1
		p.SeoNofollow = seoNofollow == 1
		p.SeoCanonical = seoCanonical
		p.Template = template
		p.Language = lang
		p.CreatedAt = createdAt
		p.UpdatedAt = updatedAt
		jsonResp(w, 200, p)
	})

	// Create or update error page
	r.Put("/custom", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
			return
		}

		if req.DomainID <= 0 {
			jsonError(w, 400, "domain_id is required")
			return
		}

		if req.ActionType == "" {
			req.ActionType = "custom_html"
		}
		if req.Language == "" {
			req.Language = "en"
		}

		var domainAccountID int
		err := db.QueryRow("SELECT account_id FROM domains WHERE id = ?", req.DomainID).Scan(&domainAccountID)
		if err != nil || domainAccountID != c.AccountID {
			jsonError(w, 403, "domain does not belong to your account")
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
				jsonError(w, 500, err.Error())
				return
			}
			n, _ := result.RowsAffected()
			if n == 0 {
				jsonError(w, 404, "error page not found")
				return
			}
		} else {
			var domain string
			err := db.QueryRow("SELECT domain FROM domains WHERE id = ?", req.DomainID).Scan(&domain)
			if err != nil {
				jsonError(w, 500, "failed to resolve domain")
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
				jsonError(w, 500, err.Error())
				return
			}
		}
		auditLog(db, r, "error_page.save", map[string]interface{}{
			"domain_id": req.DomainID, "error_code": req.ErrorCode, "action_type": req.ActionType,
		})
		jsonResp(w, 200, map[string]string{"status": "saved"})
	})

	// Delete error page
	r.Delete("/custom/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			jsonError(w, 400, "invalid id")
			return
		}
		result, err := db.Exec("DELETE FROM error_pages WHERE id = ? AND domain_id IN (SELECT id FROM domains WHERE account_id = ?)", id, c.AccountID)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			jsonError(w, 404, "not found")
			return
		}
		auditLog(db, r, "error_page.delete", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	// Toggle enabled/disabled
	r.Post("/custom/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			jsonError(w, 400, "invalid id")
			return
		}
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid body")
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
			jsonError(w, 500, err.Error())
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			jsonError(w, 404, "not found")
			return
		}
		auditLog(db, r, "error_page.toggle", map[string]interface{}{"id": id, "enabled": req.Enabled})
		jsonResp(w, 200, map[string]string{"status": "updated", "enabled": strconv.FormatBool(req.Enabled)})
	})

	// Reset to default
	r.Post("/custom/{id}/reset", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			jsonError(w, 400, "invalid id")
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
			jsonError(w, 500, err.Error())
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			jsonError(w, 404, "not found")
			return
		}
		auditLog(db, r, "error_page.reset", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "reset"})
	})

	// Test/preview - return rendered content
	r.Post("/custom/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			jsonError(w, 400, "invalid id")
			return
		}
		var content, actionType, actionValue string
		err = db.QueryRow(`SELECT ep.content, ep.action_type, ep.action_value FROM error_pages ep
			JOIN domains d ON d.id = ep.domain_id
			WHERE ep.id = ? AND d.account_id = ?`, id, c.AccountID).Scan(&content, &actionType, &actionValue)
		if err == sql.ErrNoRows {
			jsonError(w, 404, "not found")
			return
		}
		if err != nil {
			jsonError(w, 500, err.Error())
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
