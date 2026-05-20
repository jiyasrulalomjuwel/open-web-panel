package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func childTokenRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		rows, err := db.Query(`SELECT id, name, token_prefix, permissions, COALESCE(last_used_at,''), expires_at, enabled, created_at
			FROM api_tokens WHERE account_id = ? ORDER BY created_at DESC`, c.AccountID)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		type APIToken struct {
			ID          int      `json:"id"`
			Name        string   `json:"name"`
			TokenPrefix string   `json:"token_prefix"`
			Permissions []string `json:"permissions"`
			LastUsedAt  string   `json:"last_used_at"`
			ExpiresAt   string   `json:"expires_at"`
			Enabled     bool     `json:"enabled"`
			CreatedAt   string   `json:"created_at"`
		}
		tokens := make([]APIToken, 0)
		for rows.Next() {
			var t APIToken
			var perms string
			var enabled int
			rows.Scan(&t.ID, &t.Name, &t.TokenPrefix, &perms, &t.LastUsedAt, &t.ExpiresAt, &enabled, &t.CreatedAt)
			t.Enabled = enabled == 1
			json.Unmarshal([]byte(perms), &t.Permissions)
			t.CreatedAt = strings.Replace(t.CreatedAt, "T", " ", 1)
			if len(t.CreatedAt) > 19 {
				t.CreatedAt = t.CreatedAt[:19]
			}
			tokens = append(tokens, t)
		}
		jsonResp(w, 200, tokens)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		var req struct {
			Name        string   `json:"name"`
			Permissions []string `json:"permissions"`
			ExpiresIn   int      `json:"expires_in_days"` // 0 = never
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Name == "" {
			jsonError(w, 400, "name is required")
			return
		}

		// Generate token: owp_<random>
		raw := make([]byte, 32)
		rand.Read(raw)
		tokenHex := hex.EncodeToString(raw)
		fullToken := "owp_" + tokenHex
		prefix := fullToken[:12] + "..."

		tokenHash := sha256.Sum256([]byte(fullToken))
		hashHex := hex.EncodeToString(tokenHash[:])

		permsJSON := "[]"
		if len(req.Permissions) > 0 {
			b, _ := json.Marshal(req.Permissions)
			permsJSON = string(b)
		}

		var expiresAt *string
		if req.ExpiresIn > 0 {
			t := time.Now().AddDate(0, 0, req.ExpiresIn).Format("2006-01-02 15:04:05")
			expiresAt = &t
		}

		result, err := db.Exec(`INSERT INTO api_tokens (account_id, name, token_hash, token_prefix, permissions, expires_at)
			VALUES (?, ?, ?, ?, ?, ?)`, c.AccountID, req.Name, hashHex, prefix, permsJSON, expiresAt)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		id, _ := result.LastInsertId()
		auditLog(db, r, "api_token.create", map[string]interface{}{"id": id, "name": req.Name})

		jsonResp(w, 201, map[string]interface{}{
			"id":           id,
			"token":        fullToken, // Only returned once on creation
			"token_prefix": prefix,
			"status":       "created",
		})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(chi.URLParam(r, "id"))
		result, err := db.Exec("DELETE FROM api_tokens WHERE id = ? AND account_id = ?", id, c.AccountID)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			jsonError(w, 404, "token not found")
			return
		}
		auditLog(db, r, "api_token.delete", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}
