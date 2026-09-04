package main

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/validator"
)

var validDNSTypes = map[string]bool{"A": true, "AAAA": true, "CNAME": true, "MX": true, "TXT": true, "NS": true, "SRV": true, "SOA": true}

func childDNSRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		domain := r.URL.Query().Get("domain")
		if domain == "" {
			db.QueryRow("SELECT domain FROM accounts WHERE id = ?", c.AccountID).Scan(&domain)
		}

		rows, err := db.Query(`SELECT id, domain, type, name, value, priority, ttl, enabled, created_at
			FROM dns_records WHERE account_id = ? AND domain = ? ORDER BY type, name`, c.AccountID, domain)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
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
			if err := rows.Scan(&rec.ID, &rec.Domain, &rec.Type, &rec.Name, &rec.Value, &rec.Priority, &rec.TTL, &enabled, &rec.CreatedAt); err != nil {
				getLogger(r).Warnf("scan dns row: %v", err)
				continue
			}
			rec.Enabled = enabled == 1
			rec.CreatedAt = strings.Replace(rec.CreatedAt, "T", " ", 1)
			if len(rec.CreatedAt) > 19 {
				rec.CreatedAt = rec.CreatedAt[:19]
			}
			records = append(records, rec)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("dns rows iteration error: %v", err)
		}
		jsonResp(w, 200, records)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
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
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}

		validation := validator.New().
			Add("domain", validator.Required(), validator.Domain()).
			Add("type", validator.Required()).
			Add("name", validator.Required(), validator.MaxLength(255)).
			Add("value", validator.Required(), validator.MaxLength(1000))
		data, _ := toMap(req)
		if err := validation.Validate(data); err != nil {
			writeAppError(w, r, err)
			return
		}

		if !validDNSTypes[req.Type] {
			writeAppError(w, r, apperrors.Validation("invalid DNS record type, must be one of: A, AAAA, CNAME, MX, TXT, NS, SRV, SOA"))
			return
		}
		if req.TTL == 0 {
			req.TTL = 3600
		}
		if req.TTL < 60 || req.TTL > 86400 {
			writeAppError(w, r, apperrors.Validation("TTL must be between 60 and 86400 seconds"))
			return
		}

		// Tenant isolation: DNS records may only be created for domains the
		// requesting account actually owns. Otherwise any account could claim
		// DNS authority over arbitrary domains.
		var owned int
		if err := db.QueryRow("SELECT COUNT(*) FROM domains WHERE account_id = ? AND LOWER(domain) = LOWER(?)",
			c.AccountID, req.Domain).Scan(&owned); err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		if owned == 0 {
			writeAppError(w, r, apperrors.Validation("domain must belong to your account"))
			return
		}

		result, err := db.Exec(`INSERT INTO dns_records (account_id, domain, type, name, value, priority, ttl)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, c.AccountID, req.Domain, req.Type, req.Name, req.Value, req.Priority, req.TTL)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		id, _ := result.LastInsertId()
		auditLog(db, r, "dns.create", map[string]interface{}{"id": id, "domain": req.Domain, "type": req.Type, "name": req.Name})
		jsonResp(w, 201, map[string]interface{}{"id": id, "status": "created"})
	})

	r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		id, err := paramInt(r, "id")
		if err != nil {
			writeAppError(w, r, err)
			return
		}

		var ownerID int
		if err := db.QueryRow("SELECT account_id FROM dns_records WHERE id = ?", id).Scan(&ownerID); err != nil || ownerID != c.AccountID {
			writeAppError(w, r, apperrors.NotFound("DNS record", id))
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
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}

		// Re-apply create-time validation on updates: an update handler that
		// skips these checks lets a client store arbitrary type/value/TTL data.
		if req.Type != "" && !validDNSTypes[req.Type] {
			writeAppError(w, r, apperrors.Validation("invalid DNS record type, must be one of: A, AAAA, CNAME, MX, TXT, NS, SRV, SOA"))
			return
		}
		if req.Name != "" && len(req.Name) > 255 {
			writeAppError(w, r, apperrors.Validation("name too long (max 255)"))
			return
		}
		if req.Value != "" && len(req.Value) > 1000 {
			writeAppError(w, r, apperrors.Validation("value too long (max 1000)"))
			return
		}
		if req.TTL != nil && (*req.TTL < 60 || *req.TTL > 86400) {
			writeAppError(w, r, apperrors.Validation("TTL must be between 60 and 86400 seconds"))
			return
		}

		if err := withDBRetry(func() error {
			var e error
			if req.Type != "" {
				_, e = db.Exec("UPDATE dns_records SET type = ? WHERE id = ?", req.Type, id)
			}
			if e == nil && req.Name != "" {
				_, e = db.Exec("UPDATE dns_records SET name = ? WHERE id = ?", req.Name, id)
			}
			if e == nil && req.Value != "" {
				_, e = db.Exec("UPDATE dns_records SET value = ? WHERE id = ?", req.Value, id)
			}
			if e == nil && req.Priority != nil {
				_, e = db.Exec("UPDATE dns_records SET priority = ? WHERE id = ?", *req.Priority, id)
			}
			if e == nil && req.TTL != nil {
				_, e = db.Exec("UPDATE dns_records SET ttl = ? WHERE id = ?", *req.TTL, id)
			}
			if e == nil && req.Enabled != nil {
				v := boolToInt(*req.Enabled)
				_, e = db.Exec("UPDATE dns_records SET enabled = ? WHERE id = ?", v, id)
			}
			return e
		}); err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}

		auditLog(db, r, "dns.update", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		id, err := paramInt(r, "id")
		if err != nil {
			writeAppError(w, r, err)
			return
		}
		res, execErr := db.Exec("DELETE FROM dns_records WHERE id = ? AND account_id = ?", id, c.AccountID)
		if execErr != nil {
			writeAppError(w, r, apperrors.Database(execErr))
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeAppError(w, r, apperrors.NotFound("DNS record", id))
			return
		}
		auditLog(db, r, "dns.delete", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}
