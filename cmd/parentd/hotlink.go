package main

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
)

func hotlinkRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}

		var id int
		var enabled int
		var allowedDomains string
		err := db.QueryRow(`SELECT id, enabled, COALESCE(allowed_domains, '')
			FROM hotlink_protection WHERE account_id = ?`, c.AccountID).Scan(&id, &enabled, &allowedDomains)
		if err != nil {
			jsonResp(w, 200, map[string]interface{}{
				"enabled":         false,
				"allowed_domains": []string{},
			})
			return
		}

		domains := []string{}
		if allowedDomains != "" {
			domains = strings.Split(allowedDomains, ",")
		}

		jsonResp(w, 200, map[string]interface{}{
			"enabled":         enabled == 1,
			"allowed_domains": domains,
		})
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}

		var req struct {
			Enabled        bool     `json:"enabled"`
			AllowedDomains []string `json:"allowed_domains"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}

		enabled := 0
		if req.Enabled {
			enabled = 1
		}

		clean := make([]string, 0, len(req.AllowedDomains))
		for _, d := range req.AllowedDomains {
			d = strings.TrimSpace(d)
			d = strings.TrimSuffix(d, ".")
			if d == "" {
				continue
			}
			if !validDomain(d) {
				writeAppError(w, r, apperrors.Validation("invalid allowed domain: "+d))
				return
			}
			var owned int
			db.QueryRow("SELECT COUNT(*) FROM domains WHERE account_id = ? AND LOWER(domain) = LOWER(?)", c.AccountID, d).Scan(&owned)
			if owned == 0 {
				writeAppError(w, r, apperrors.Validation("allowed domain is not owned by this account: "+d))
				return
			}
			clean = append(clean, d)
		}
		domainStr := strings.Join(clean, ",")

		_, err := db.Exec(`INSERT INTO hotlink_protection (account_id, enabled, allowed_domains)
			VALUES (?, ?, ?)
			ON CONFLICT(account_id) DO UPDATE SET enabled = excluded.enabled, allowed_domains = excluded.allowed_domains,
			updated_at = datetime('now')`, c.AccountID, enabled, domainStr)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}

		auditLog(db, r, "hotlink.update", map[string]interface{}{"enabled": req.Enabled, "domains": req.AllowedDomains})
		go syncNginxVhosts(db)
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})
}
