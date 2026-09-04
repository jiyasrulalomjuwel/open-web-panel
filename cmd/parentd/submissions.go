package main

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
)

func submissionRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		claims := getClaims(r)
		if claims == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
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
			writeAppError(w, r, apperrors.Database(err))
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
			if err := rows.Scan(&s.ID, &s.AccountID, &s.FormType, &s.Metadata, &s.IPAddress, &s.UserAgent, &s.CreatedAt); err != nil {
				continue
			}
			submissions = append(submissions, s)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("submission rows iteration error: %v", err)
		}
		jsonResp(w, 200, submissions)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		claims := getClaims(r)
		if claims == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}

		var req struct {
			AccountID int    `json:"account_id"`
			FormType  string `json:"form_type"`
			Metadata  string `json:"metadata"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		if req.FormType == "" {
			writeAppError(w, r, apperrors.Validation("form_type is required"))
			return
		}

		// Admin/parent-scope endpoint: an admin may attribute a submission to any
		// account via account_id (that is the intended feature). The parent authMw
		// plus the claims check above gate this handler to authorized admins.
		// Validate the referenced account exists so we never poison rows against
		// nonexistent accounts.
		var exists int
		if err := db.QueryRow("SELECT COUNT(*) FROM accounts WHERE id = ?", req.AccountID).Scan(&exists); err != nil || exists == 0 {
			writeAppError(w, r, apperrors.Validation("account_id does not exist"))
			return
		}

		ip := getClientIP(r)
		ua := r.UserAgent()
		_, err := db.Exec(`INSERT INTO form_submissions (account_id, form_type, metadata, ip_address, user_agent) VALUES (?, ?, ?, ?, ?)`,
			req.AccountID, req.FormType, req.Metadata, ip, ua)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		jsonResp(w, 200, map[string]string{"status": "submitted"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		claims := getClaims(r)
		if claims == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}

		id := chi.URLParam(r, "id")
		if id == "" {
			writeAppError(w, r, apperrors.Validation("id is required"))
			return
		}

		var accountID int
		err := db.QueryRow("SELECT account_id FROM form_submissions WHERE id = ?", id).Scan(&accountID)
		if err != nil {
			writeAppError(w, r, apperrors.NotFound("submission", id))
			return
		}

		// Scope the delete: if an account_id filter is supplied (same as the GET
		// list accepts), only delete submissions belonging to that account.
		// Otherwise delete by id (superadmin/global).
		deletedByIDOnly := true
		query := "DELETE FROM form_submissions WHERE id = ?"
		args := []interface{}{id}
		if accountIDFilter := r.URL.Query().Get("account_id"); accountIDFilter != "" {
			query += " AND account_id = ?"
			args = append(args, accountIDFilter)
			deletedByIDOnly = false
		}

		result, err := db.Exec(query, args...)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			if !deletedByIDOnly {
				writeAppError(w, r, apperrors.NotFound("submission for account", id))
				return
			}
			writeAppError(w, r, apperrors.NotFound("submission", id))
			return
		}
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}
