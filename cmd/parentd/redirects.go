package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func redirectRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		rows, err := db.Query(`SELECT id, account_id, domain_id, source_path, target_url, redirect_type, status, created_at
			FROM redirects WHERE account_id = ? ORDER BY created_at DESC`, c.AccountID)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		type Redirect struct {
			ID           int    `json:"id"`
			AccountID    int    `json:"account_id"`
			DomainID     int    `json:"domain_id"`
			SourcePath   string `json:"source_path"`
			TargetURL    string `json:"target_url"`
			RedirectType string `json:"redirect_type"`
			Status       string `json:"status"`
			CreatedAt    string `json:"created_at"`
		}
		redirects := make([]Redirect, 0)
		for rows.Next() {
			var rdr Redirect
			rows.Scan(&rdr.ID, &rdr.AccountID, &rdr.DomainID, &rdr.SourcePath, &rdr.TargetURL, &rdr.RedirectType, &rdr.Status, &rdr.CreatedAt)
			redirects = append(redirects, rdr)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[REDIRECTS] rows iteration error: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		jsonResp(w, 200, redirects)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		var req struct {
			DomainID     int    `json:"domain_id"`
			SourcePath   string `json:"source_path"`
			TargetURL    string `json:"target_url"`
			RedirectType string `json:"type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		if req.DomainID == 0 || req.SourcePath == "" || req.TargetURL == "" {
			jsonError(w, 400, "domain_id, source_path, and target_url required")
			return
		}
		if req.RedirectType == "" {
			req.RedirectType = "301"
		}
		if req.RedirectType != "301" && req.RedirectType != "302" {
			jsonError(w, 400, "type must be 301 or 302")
			return
		}

		// Verify domain ownership
		var domainName string
		err := db.QueryRow("SELECT domain FROM domains WHERE id = ? AND account_id = ?",
			req.DomainID, c.AccountID).Scan(&domainName)
		if err != nil {
			jsonError(w, 404, "domain not found")
			return
		}

		result, err := db.Exec(`INSERT INTO redirects (account_id, domain_id, source_path, target_url, redirect_type)
			VALUES (?, ?, ?, ?, ?)`, c.AccountID, req.DomainID, req.SourcePath, req.TargetURL, req.RedirectType)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		id, _ := result.LastInsertId()
		auditLog(db, r, "redirect.create", map[string]interface{}{"id": id, "source": req.SourcePath, "target": req.TargetURL})
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
		err := db.QueryRow("SELECT account_id FROM redirects WHERE id = ?", id).Scan(&ownerID)
		if err != nil || ownerID != c.AccountID {
			jsonError(w, 404, "redirect not found")
			return
		}

		var req struct {
			SourcePath   string `json:"source_path"`
			TargetURL    string `json:"target_url"`
			RedirectType string `json:"type"`
			Status       string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}

		if req.SourcePath != "" {
			db.Exec("UPDATE redirects SET source_path = ? WHERE id = ?", req.SourcePath, id)
		}
		if req.TargetURL != "" {
			db.Exec("UPDATE redirects SET target_url = ? WHERE id = ?", req.TargetURL, id)
		}
		if req.RedirectType != "" {
			if req.RedirectType != "301" && req.RedirectType != "302" {
				jsonError(w, 400, "type must be 301 or 302")
				return
			}
			db.Exec("UPDATE redirects SET redirect_type = ? WHERE id = ?", req.RedirectType, id)
		}
		if req.Status != "" {
			db.Exec("UPDATE redirects SET status = ? WHERE id = ?", req.Status, id)
		}

		auditLog(db, r, "redirect.update", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		result, err := db.Exec("DELETE FROM redirects WHERE id = ? AND account_id = ?", id, c.AccountID)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			jsonError(w, 404, "redirect not found")
			return
		}
		auditLog(db, r, "redirect.delete", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}
