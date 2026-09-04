package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/openwebcpanel/openwebcpanel/internal/shared/auth"
	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
)

// validateAPIToken resolves a long-lived owp_... token to JWT-style claims.
// Returns nil for unknown, disabled or expired tokens. Admin-scope tokens
// inherit their owner's admin role; account-scope tokens mirror a child login
// (full child access of that account — feature gates still apply downstream).
func validateAPIToken(db *sql.DB, raw string) *auth.Claims {
	if !strings.HasPrefix(raw, "owp_") || len(raw) < 16 {
		return nil
	}
	sum := sha256.Sum256([]byte(raw))
	hashHex := hex.EncodeToString(sum[:])
	var id, accountID, adminID, enabled int
	var scope, expiresAt sql.NullString
	err := db.QueryRow(`SELECT id, COALESCE(account_id,0), COALESCE(admin_id,0),
		COALESCE(scope,'child'), expires_at, enabled
		FROM api_tokens WHERE token_hash = ?`, hashHex).
		Scan(&id, &accountID, &adminID, &scope, &expiresAt, &enabled)
	if err != nil || enabled != 1 {
		return nil
	}
	if expiresAt.Valid && expiresAt.String != "" &&
		expiresAt.String < time.Now().UTC().Format("2006-01-02 15:04:05") {
		return nil
	}
	db.Exec("UPDATE api_tokens SET last_used_at = datetime('now') WHERE id = ?", id)
	if scope.String == "admin" {
		var username, role string
		if err := db.QueryRow("SELECT username, role FROM admins WHERE id = ?", adminID).
			Scan(&username, &role); err != nil {
			return nil
		}
		return &auth.Claims{UserID: adminID, Username: username, Role: role, Scope: "parent"}
	}
	var username, status string
	if err := db.QueryRow("SELECT username, status FROM accounts WHERE id = ?", accountID).
		Scan(&username, &status); err != nil {
		return nil
	}
	return &auth.Claims{UserID: accountID, Username: username, Role: "account", Scope: "child", AccountID: accountID}
}

// mintToken creates an owp_... secret, stores only its SHA256 hash and
// returns the one-time secret alongside the new row id.
func mintToken() (fullToken, prefix, hashHex string) {
	raw := make([]byte, 32)
	rand.Read(raw)
	fullToken = "owp_" + hex.EncodeToString(raw)
	prefix = fullToken[:12] + "..."
	sum := sha256.Sum256([]byte(fullToken))
	hashHex = hex.EncodeToString(sum[:])
	return fullToken, prefix, hashHex
}

// ---------- Admin tokens (parent scope) ----------

func adminTokenRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT t.id, t.name, t.token_prefix,
			COALESCE(a.username,''), COALESCE(t.last_used_at,''), t.expires_at, t.enabled, t.created_at
			FROM api_tokens t LEFT JOIN admins a ON a.id = t.admin_id
			WHERE t.scope = 'admin' ORDER BY t.created_at DESC`)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		defer rows.Close()
		type AdminToken struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			Prefix     string `json:"token_prefix"`
			Owner      string `json:"owner"`
			LastUsedAt string `json:"last_used_at"`
			ExpiresAt  string `json:"expires_at"`
			Enabled    bool   `json:"enabled"`
			CreatedAt  string `json:"created_at"`
		}
		out := make([]AdminToken, 0)
		for rows.Next() {
			var t AdminToken
			var expires sql.NullString
			var enabled int
			if err := rows.Scan(&t.ID, &t.Name, &t.Prefix, &t.Owner, &t.LastUsedAt, &expires, &enabled, &t.CreatedAt); err != nil {
				continue
			}
			if expires.Valid {
				t.ExpiresAt = expires.String
			}
			t.Enabled = enabled == 1
			out = append(out, t)
		}
		jsonResp(w, 200, out)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		var req struct {
			Name      string `json:"name"`
			ExpiresIn int    `json:"expires_in_days"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		if req.Name == "" {
			writeAppError(w, r, apperrors.Validation("name is required"))
			return
		}
		if len(req.Name) > 64 {
			writeAppError(w, r, apperrors.Validation("name must not exceed 64 characters"))
			return
		}
		if req.ExpiresIn < 0 || req.ExpiresIn > 3650 {
			writeAppError(w, r, apperrors.Validation("expires_in_days must be between 0 and 3650"))
			return
		}
		fullToken, prefix, hashHex := mintToken()
		var expiresAt *string
		if req.ExpiresIn > 0 {
			t := time.Now().AddDate(0, 0, req.ExpiresIn).Format("2006-01-02 15:04:05")
			expiresAt = &t
		}
		result, err := db.Exec(`INSERT INTO api_tokens (account_id, admin_id, scope, name, token_hash, token_prefix, expires_at)
			VALUES (0, ?, 'admin', ?, ?, ?, ?)`, c.UserID, req.Name, hashHex, prefix, expiresAt)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		id, _ := result.LastInsertId()
		auditLog(db, r, "api_token.create", map[string]interface{}{"id": id, "name": req.Name, "scope": "admin"})
		// The secret is returned exactly once — only the hash is stored.
		jsonResp(w, 201, map[string]interface{}{
			"id": id, "token": fullToken, "token_prefix": prefix, "status": "created",
		})
	})

	r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := paramInt(r, "id")
		if err != nil {
			writeAppError(w, r, err)
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
		res, execErr := db.Exec("UPDATE api_tokens SET enabled = ? WHERE id = ? AND scope = 'admin'", enabled, id)
		if execErr != nil {
			writeAppError(w, r, apperrors.Database(execErr))
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeAppError(w, r, apperrors.NotFound("token", id))
			return
		}
		auditLog(db, r, "api_token.toggle", map[string]interface{}{"id": id, "enabled": req.Enabled})
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := paramInt(r, "id")
		if err != nil {
			writeAppError(w, r, err)
			return
		}
		res, execErr := db.Exec("DELETE FROM api_tokens WHERE id = ? AND scope = 'admin'", id)
		if execErr != nil {
			writeAppError(w, r, apperrors.Database(execErr))
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeAppError(w, r, apperrors.NotFound("token", id))
			return
		}
		auditLog(db, r, "api_token.delete", map[string]interface{}{"id": id, "scope": "admin"})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}

func childTokenRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		rows, err := db.Query(`SELECT id, name, token_prefix, permissions, COALESCE(last_used_at,''), expires_at, enabled, created_at
			FROM api_tokens WHERE account_id = ? ORDER BY created_at DESC`, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
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
			if err := rows.Scan(&t.ID, &t.Name, &t.TokenPrefix, &perms, &t.LastUsedAt, &t.ExpiresAt, &enabled, &t.CreatedAt); err != nil {
				getLogger(r).Warnf("scan token row: %v", err)
				continue
			}
			t.Enabled = enabled == 1
			json.Unmarshal([]byte(perms), &t.Permissions)
			t.CreatedAt = strings.Replace(t.CreatedAt, "T", " ", 1)
			if len(t.CreatedAt) > 19 {
				t.CreatedAt = t.CreatedAt[:19]
			}
			tokens = append(tokens, t)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("token rows iteration error: %v", err)
		}
		jsonResp(w, 200, tokens)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		var req struct {
			Name        string   `json:"name"`
			Permissions []string `json:"permissions"`
			ExpiresIn   int      `json:"expires_in_days"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		if req.Name == "" {
			writeAppError(w, r, apperrors.Validation("name is required"))
			return
		}
		if len(req.Name) > 64 {
			writeAppError(w, r, apperrors.Validation("name must not exceed 64 characters"))
			return
		}
		if req.ExpiresIn < 0 || req.ExpiresIn > 3650 {
			writeAppError(w, r, apperrors.Validation("expires_in_days must be between 0 and 3650"))
			return
		}

		fullToken, prefix, hashHex := mintToken()

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
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		id, _ := result.LastInsertId()
		auditLog(db, r, "api_token.create", logging.Fields{"id": id, "name": req.Name})

		jsonResp(w, 201, map[string]interface{}{
			"id":           id,
			"token":        fullToken,
			"token_prefix": prefix,
			"status":       "created",
		})
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
		res, execErr := db.Exec("DELETE FROM api_tokens WHERE id = ? AND account_id = ?", id, c.AccountID)
		if execErr != nil {
			writeAppError(w, r, apperrors.Database(execErr))
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeAppError(w, r, apperrors.NotFound("token", id))
			return
		}
		auditLog(db, r, "api_token.delete", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
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
		res, execErr := db.Exec("UPDATE api_tokens SET enabled = ? WHERE id = ? AND account_id = ?", enabled, id, c.AccountID)
		if execErr != nil {
			writeAppError(w, r, apperrors.Database(execErr))
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeAppError(w, r, apperrors.NotFound("token", id))
			return
		}
		auditLog(db, r, "api_token.toggle", logging.Fields{"id": id, "enabled": req.Enabled})
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})
}
