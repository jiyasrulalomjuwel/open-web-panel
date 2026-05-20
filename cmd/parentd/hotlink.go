package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func hotlinkRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		var id int
		var enabled int
		var allowedDomains string
		err := db.QueryRow(`SELECT id, enabled, COALESCE(allowed_domains, '')
			FROM hotlink_protection WHERE account_id = ?`, c.AccountID).Scan(&id, &enabled, &allowedDomains)
		if err != nil {
			// No settings yet — return defaults
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
			jsonError(w, 401, "unauthorized")
			return
		}

		var req struct {
			Enabled        bool     `json:"enabled"`
			AllowedDomains []string `json:"allowed_domains"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		enabled := 0
		if req.Enabled {
			enabled = 1
		}
		domainStr := strings.Join(req.AllowedDomains, ",")

		// Upsert: insert or update
		_, err := db.Exec(`INSERT INTO hotlink_protection (account_id, enabled, allowed_domains)
			VALUES (?, ?, ?)
			ON CONFLICT(account_id) DO UPDATE SET enabled = excluded.enabled, allowed_domains = excluded.allowed_domains,
			updated_at = datetime('now')`, c.AccountID, enabled, domainStr)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}

		auditLog(db, r, "hotlink.update", map[string]interface{}{"enabled": req.Enabled, "domains": req.AllowedDomains})
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})
}
