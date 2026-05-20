package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func childDNSRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		domain := r.URL.Query().Get("domain")
		if domain == "" {
			db.QueryRow("SELECT domain FROM accounts WHERE id = ?", c.AccountID).Scan(&domain)
		}

		rows, err := db.Query(`SELECT id, domain, type, name, value, priority, ttl, enabled, created_at
			FROM dns_records WHERE account_id = ? AND domain = ? ORDER BY type, name`, c.AccountID, domain)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		type DNSRecord struct {
			ID        int    `json:"id"`
			Domain    string `json:"domain"`
			Type      string `json:"type"`
			Name      string `json:"name"`
			Value     string `json:"value"`
			Priority  int    `json:"priority"`
			TTL       int    `json:"ttl"`
			Enabled   bool   `json:"enabled"`
			CreatedAt string `json:"created_at"`
		}
		records := make([]DNSRecord, 0)
		for rows.Next() {
			var rec DNSRecord
			var enabled int
			rows.Scan(&rec.ID, &rec.Domain, &rec.Type, &rec.Name, &rec.Value, &rec.Priority, &rec.TTL, &enabled, &rec.CreatedAt)
			rec.Enabled = enabled == 1
			rec.CreatedAt = strings.Replace(rec.CreatedAt, "T", " ", 1)
			if len(rec.CreatedAt) > 19 {
				rec.CreatedAt = rec.CreatedAt[:19]
			}
			records = append(records, rec)
		}
		jsonResp(w, 200, records)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		var req struct {
			Domain   string `json:"domain"`
			Type     string `json:"type"`
			Name     string `json:"name"`
			Value    string `json:"value"`
			Priority int    `json:"priority"`
			TTL      int    `json:"ttl"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Domain == "" || req.Type == "" || req.Name == "" || req.Value == "" {
			jsonError(w, 400, "domain, type, name, and value are required")
			return
		}
		validTypes := map[string]bool{"A": true, "AAAA": true, "CNAME": true, "MX": true, "TXT": true, "NS": true, "SRV": true, "SOA": true}
		if !validTypes[req.Type] {
			jsonError(w, 400, "invalid DNS record type")
			return
		}
		if req.TTL == 0 {
			req.TTL = 3600
		}

		result, err := db.Exec(`INSERT INTO dns_records (account_id, domain, type, name, value, priority, ttl)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, c.AccountID, req.Domain, req.Type, req.Name, req.Value, req.Priority, req.TTL)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		id, _ := result.LastInsertId()
		auditLog(db, r, "dns.create", map[string]interface{}{"id": id, "domain": req.Domain, "type": req.Type, "name": req.Name})
		jsonResp(w, 201, map[string]interface{}{"id": id, "status": "created"})
	})

	r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))

		var ownerID int
		err := db.QueryRow("SELECT account_id FROM dns_records WHERE id = ?", id).Scan(&ownerID)
		if err != nil || ownerID != c.AccountID {
			jsonError(w, 404, "record not found")
			return
		}

		var req struct {
			Type     string `json:"type"`
			Name     string `json:"name"`
			Value    string `json:"value"`
			Priority *int   `json:"priority"`
			TTL      *int   `json:"ttl"`
			Enabled  *bool  `json:"enabled"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if req.Type != "" {
			db.Exec("UPDATE dns_records SET type = ? WHERE id = ?", req.Type, id)
		}
		if req.Name != "" {
			db.Exec("UPDATE dns_records SET name = ? WHERE id = ?", req.Name, id)
		}
		if req.Value != "" {
			db.Exec("UPDATE dns_records SET value = ? WHERE id = ?", req.Value, id)
		}
		if req.Priority != nil {
			db.Exec("UPDATE dns_records SET priority = ? WHERE id = ?", *req.Priority, id)
		}
		if req.TTL != nil {
			db.Exec("UPDATE dns_records SET ttl = ? WHERE id = ?", *req.TTL, id)
		}
		if req.Enabled != nil {
			v := 0
			if *req.Enabled {
				v = 1
			}
			db.Exec("UPDATE dns_records SET enabled = ? WHERE id = ?", v, id)
		}

		auditLog(db, r, "dns.update", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		result, err := db.Exec("DELETE FROM dns_records WHERE id = ? AND account_id = ?", id, c.AccountID)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			jsonError(w, 404, "record not found")
			return
		}
		auditLog(db, r, "dns.delete", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}
