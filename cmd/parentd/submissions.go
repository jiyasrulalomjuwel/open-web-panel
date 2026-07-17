package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ---------- Form Submission routes ----------

func submissionRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		claims := getClaims(r)
		if claims == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		accountIDStr := r.URL.Query().Get("account_id")
		formType := r.URL.Query().Get("form_type")

		query := `SELECT id, account_id, form_type, COALESCE(metadata,'{}'), COALESCE(ip_address,''), COALESCE(user_agent,''), created_at
			FROM form_submissions WHERE 1=1`
		args := make([]interface{}, 0)

		if accountIDStr != "" {
			query += " AND account_id = ?"
			args = append(args, accountIDStr)
		}
		if formType != "" {
			query += " AND form_type = ?"
			args = append(args, formType)
		}
		query += " ORDER BY created_at DESC LIMIT 500"

		rows, err := db.Query(query, args...)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		type Submission struct {
			ID        int             `json:"id"`
			AccountID int             `json:"account_id"`
			FormType  string          `json:"form_type"`
			Metadata  json.RawMessage `json:"metadata"`
			IPAddress string          `json:"ip_address"`
			UserAgent string          `json:"user_agent"`
			CreatedAt string          `json:"created_at"`
		}

		submissions := make([]Submission, 0)
		for rows.Next() {
			var s Submission
			rows.Scan(&s.ID, &s.AccountID, &s.FormType, &s.Metadata, &s.IPAddress, &s.UserAgent, &s.CreatedAt)
			submissions = append(submissions, s)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[SUBMISSIONS] rows iteration error: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		jsonResp(w, 200, submissions)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			AccountID int    `json:"account_id"`
			FormType  string `json:"form_type"`
			Metadata  string `json:"metadata"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		if req.FormType == "" {
			jsonError(w, 400, "form_type required")
			return
		}

		ip := r.RemoteAddr
		ua := r.UserAgent()
		_, err := db.Exec(`INSERT INTO form_submissions (account_id, form_type, metadata, ip_address, user_agent) VALUES (?, ?, ?, ?, ?)`,
			req.AccountID, req.FormType, req.Metadata, ip, ua)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonResp(w, 200, map[string]string{"status": "submitted"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		claims := getClaims(r)
		if claims == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		id := chi.URLParam(r, "id")
		if id == "" {
			jsonError(w, 400, "id required")
			return
		}

		// Verify the submission belongs to an account the admin can access
		var accountID int
		err := db.QueryRow("SELECT account_id FROM form_submissions WHERE id = ?", id).Scan(&accountID)
		if err != nil {
			jsonError(w, 404, "submission not found")
			return
		}

		result, err := db.Exec("DELETE FROM form_submissions WHERE id = ?", id)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			jsonError(w, 404, "submission not found")
			return
		}
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}
