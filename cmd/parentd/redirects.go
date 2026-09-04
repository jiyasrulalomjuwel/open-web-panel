package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/validator"
)

const (
	owpRedirectBegin = "# OWP-REDIRECTS-BEGIN (managed by OpenWebPanel, do not edit)"
	owpRedirectEnd   = "# OWP-REDIRECTS-END"
)

// validRedirectSource allows only absolute paths without nginx-special
// characters, so rows can be rendered as exact-match locations verbatim.
func validRedirectSource(s string) error {
	if !strings.HasPrefix(s, "/") {
		return fmt.Errorf("source_path must start with /")
	}
	if strings.ContainsAny(s, " \t\r\n;{}#") {
		return fmt.Errorf("source_path contains invalid characters")
	}
	if len(s) > 500 {
		return fmt.Errorf("source_path too long")
	}
	return nil
}

// validRedirectTarget allows absolute paths or http(s) URLs, nothing that can
// break out of the `return` directive.
func validRedirectTarget(s string) error {
	if strings.ContainsAny(s, " \t\r\n;{}") {
		return fmt.Errorf("target_url contains invalid characters")
	}
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return nil
	}
	return fmt.Errorf("target_url must be an absolute path or http(s) URL")
}

// syncDomainRedirects rebuilds the redirect snippet for one domain from its
// active rows and injects it into every matching server block (:80 + :443).
// An empty rule set cleanly removes the block.
func syncDomainRedirects(db *sql.DB, dom string) error {
	nginxMu.Lock()
	defer nginxMu.Unlock()
	return doSyncDomainRedirects(db, dom)
}

func doSyncDomainRedirects(db *sql.DB, dom string) error {
	rows, err := db.Query(`SELECT source_path, target_url, redirect_type FROM redirects r
		JOIN accounts a ON a.id = r.account_id
		JOIN domains d ON d.id = r.domain_id
		WHERE d.domain = ? AND a.status = 'active'
		AND COALESCE(r.status, 'active') = 'active'
		ORDER BY source_path`, dom)
	if err != nil {
		return err
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var src, dst, typ string
		if err := rows.Scan(&src, &dst, &typ); err != nil {
			continue
		}
		if validRedirectSource(src) != nil || validRedirectTarget(dst) != nil {
			continue
		}
		if typ != "301" && typ != "302" {
			typ = "301"
		}
		fmt.Fprintf(&b, "    location = %s { return %s %s; }\n", src, typ, dst)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	safe := sanitizeDomain(dom)
	vhostPath := vhostDir + safe + ".conf"
	raw, err := os.ReadFile(vhostPath)
	if err != nil {
		// No vhost (yet): sync restores it later.
		return nil
	}
	updated := injectManagedServerBlock(string(raw), dom, owpRedirectBegin, owpRedirectEnd, b.String())
	if updated == string(raw) {
		return nil
	}
	if err := writeVhostFile(vhostPath, []byte(updated), 0644); err != nil {
		return err
	}
	return reloadNginxErr()
}

func redirectRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		rows, err := db.Query(`SELECT id, account_id, domain_id, source_path, target_url, redirect_type, status, created_at
			FROM redirects WHERE account_id = ? ORDER BY created_at DESC`, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
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
			if err := rows.Scan(&rdr.ID, &rdr.AccountID, &rdr.DomainID, &rdr.SourcePath, &rdr.TargetURL, &rdr.RedirectType, &rdr.Status, &rdr.CreatedAt); err != nil {
				getLogger(r).Warnf("scan redirect row: %v", err)
				continue
			}
			redirects = append(redirects, rdr)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("redirect rows iteration error: %v", err)
		}
		jsonResp(w, 200, redirects)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		var req struct {
			DomainID     int    `json:"domain_id"`
			SourcePath   string `json:"source_path"`
			TargetURL    string `json:"target_url"`
			RedirectType string `json:"type"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}

		validation := validator.New().
			Add("domain_id", validator.Required(), validator.Min(1)).
			Add("source_path", validator.Required(), validator.MaxLength(500)).
			Add("target_url", validator.Required(), validator.MaxLength(2000))
		data, _ := toMap(req)
		if err := validation.Validate(data); err != nil {
			writeAppError(w, r, err)
			return
		}

		if req.RedirectType == "" {
			req.RedirectType = "301"
		}
		if req.RedirectType != "301" && req.RedirectType != "302" {
			writeAppError(w, r, apperrors.Validation("type must be 301 or 302"))
			return
		}

		var domainName string
		err := db.QueryRow("SELECT domain FROM domains WHERE id = ? AND account_id = ?",
			req.DomainID, c.AccountID).Scan(&domainName)
		if err != nil {
			writeAppError(w, r, apperrors.NotFound("domain", req.DomainID))
			return
		}
		if err := validRedirectSource(req.SourcePath); err != nil {
			writeAppError(w, r, apperrors.Validation(err.Error()))
			return
		}
		if err := validRedirectTarget(req.TargetURL); err != nil {
			writeAppError(w, r, apperrors.Validation(err.Error()))
			return
		}

		result, err := db.Exec(`INSERT INTO redirects (account_id, domain_id, source_path, target_url, redirect_type)
			VALUES (?, ?, ?, ?, ?)`, c.AccountID, req.DomainID, req.SourcePath, req.TargetURL, req.RedirectType)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		id, _ := result.LastInsertId()
		auditLog(db, r, "redirect.create", map[string]interface{}{"id": id, "source": req.SourcePath, "target": req.TargetURL})
		if err := syncDomainRedirects(db, domainName); err != nil {
			getLogger(r).Warnf("redirect nginx sync failed for %s: %v", domainName, err)
		}
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
		var domainName string
		if err := db.QueryRow(`SELECT r.account_id, d.domain FROM redirects r
			JOIN domains d ON d.id = r.domain_id WHERE r.id = ?`, id).Scan(&ownerID, &domainName); err != nil || ownerID != c.AccountID {
			writeAppError(w, r, apperrors.NotFound("redirect", id))
			return
		}

		var req struct {
			SourcePath   string `json:"source_path"`
			TargetURL    string `json:"target_url"`
			RedirectType string `json:"type"`
			Status       string `json:"status"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}

		if req.RedirectType != "" && req.RedirectType != "301" && req.RedirectType != "302" {
			writeAppError(w, r, apperrors.Validation("type must be 301 or 302"))
			return
		}

		if req.SourcePath != "" {
			if err := validRedirectSource(req.SourcePath); err != nil {
				writeAppError(w, r, apperrors.Validation(err.Error()))
				return
			}
		}
		if req.TargetURL != "" {
			if err := validRedirectTarget(req.TargetURL); err != nil {
				writeAppError(w, r, apperrors.Validation(err.Error()))
				return
			}
		}
		if req.Status != "" && req.Status != "active" && req.Status != "disabled" {
			writeAppError(w, r, apperrors.Validation("status must be active or disabled"))
			return
		}

		if err := withDBRetry(func() error {
			if req.SourcePath != "" {
				if _, e := db.Exec("UPDATE redirects SET source_path = ? WHERE id = ?", req.SourcePath, id); e != nil {
					return e
				}
			}
			if req.TargetURL != "" {
				if _, e := db.Exec("UPDATE redirects SET target_url = ? WHERE id = ?", req.TargetURL, id); e != nil {
					return e
				}
			}
			if req.RedirectType != "" {
				if _, e := db.Exec("UPDATE redirects SET redirect_type = ? WHERE id = ?", req.RedirectType, id); e != nil {
					return e
				}
			}
			if req.Status != "" {
				if _, e := db.Exec("UPDATE redirects SET status = ? WHERE id = ?", req.Status, id); e != nil {
					return e
				}
			}
			return nil
		}); err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}

		auditLog(db, r, "redirect.update", logging.Fields{"id": id})
		if err := syncDomainRedirects(db, domainName); err != nil {
			getLogger(r).Warnf("redirect nginx sync failed for %s: %v", domainName, err)
		}
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
		var domainName string
		var ownerID int
		if err := db.QueryRow(`SELECT r.account_id, d.domain FROM redirects r
			JOIN domains d ON d.id = r.domain_id WHERE r.id = ?`, id).Scan(&ownerID, &domainName); err != nil || ownerID != c.AccountID {
			writeAppError(w, r, apperrors.NotFound("redirect", id))
			return
		}
		res, execErr := db.Exec("DELETE FROM redirects WHERE id = ? AND account_id = ?", id, c.AccountID)
		if execErr != nil {
			writeAppError(w, r, apperrors.Database(execErr))
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeAppError(w, r, apperrors.NotFound("redirect", id))
			return
		}
		auditLog(db, r, "redirect.delete", logging.Fields{"id": id})
		if err := syncDomainRedirects(db, domainName); err != nil {
			getLogger(r).Warnf("redirect nginx sync failed for %s: %v", domainName, err)
		}
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}
