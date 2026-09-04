package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
)

type phpDownload struct {
	ID       int    `json:"id"`
	Version  string `json:"version"`
	Status   string `json:"status"`
	Progress string `json:"progress"`
	Error    string `json:"error,omitempty"`
	cancel   context.CancelFunc
	mu       sync.Mutex
}

var (
	dlMu        sync.Mutex
	dlMap       = make(map[int]*phpDownload)
	phpPkgNames = []string{"fpm", "cli", "common", "mysql", "curl", "gd", "mbstring", "xml", "zip"}
	versionRe   = regexp.MustCompile(`^\d+\.\d+$`)
)

func phpPkgs(version string) []string {
	out := make([]string, len(phpPkgNames))
	for i, p := range phpPkgNames {
		out[i] = fmt.Sprintf("php%s-%s", version, p)
	}
	return out
}

func setDl(id int, d *phpDownload) {
	dlMu.Lock()
	if d == nil {
		delete(dlMap, id)
	} else {
		dlMap[id] = d
	}
	dlMu.Unlock()
}

func getDl(id int) *phpDownload {
	dlMu.Lock()
	defer dlMu.Unlock()
	return dlMap[id]
}

type phpDownloadView struct {
	ID       int    `json:"id"`
	Version  string `json:"version"`
	Status   string `json:"status"`
	Progress string `json:"progress"`
	Error    string `json:"error,omitempty"`
}

func getAllDls() []phpDownloadView {
	dlMu.Lock()
	defer dlMu.Unlock()
	out := make([]phpDownloadView, 0, len(dlMap))
	for _, d := range dlMap {
		d.mu.Lock()
		out = append(out, phpDownloadView{
			ID:       d.ID,
			Version:  d.Version,
			Status:   d.Status,
			Progress: d.Progress,
			Error:    d.Error,
		})
		d.mu.Unlock()
	}
	return out
}

var aptTimeout = 60 * time.Minute

func phpVersionRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT id, version, socket_path, status, created_at, updated_at
			FROM php_versions ORDER BY version DESC`)
		if err != nil {
			getLogger(r).Errorf("php versions query: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		type PhpVersion struct {
			ID         int    `json:"id"`
			Version    string `json:"version"`
			SocketPath string `json:"socket_path"`
			Status     string `json:"status"`
			CreatedAt  string `json:"created_at"`
			UpdatedAt  string `json:"updated_at"`
		}
		versions := make([]PhpVersion, 0)
		for rows.Next() {
			var v PhpVersion
			if err := rows.Scan(&v.ID, &v.Version, &v.SocketPath, &v.Status, &v.CreatedAt, &v.UpdatedAt); err != nil {
				getLogger(r).Warnf("scan php version row: %v", err)
				continue
			}
			versions = append(versions, v)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("php versions rows iteration error: %v", err)
		}
		jsonResp(w, 200, versions)
	})

	r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := paramInt(r, "id")
		if err != nil || id <= 0 {
			writeAppError(w, r, apperrors.Validation("invalid id"))
			return
		}
		var req struct {
			Version    *string `json:"version"`
			SocketPath *string `json:"socket_path"`
			Status     *string `json:"status"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}

		validStatuses := map[string]bool{"not_installed": true, "downloaded": true, "activated": true}
		fields := []string{}
		args := []interface{}{}
		if req.Version != nil {
			if !versionRe.MatchString(*req.Version) {
				writeAppError(w, r, apperrors.Validation("invalid version format (must be like 8.3)"))
				return
			}
			fields = append(fields, "version = ?")
			args = append(args, *req.Version)
		}
		if req.SocketPath != nil {
			fields = append(fields, "socket_path = ?")
			args = append(args, *req.SocketPath)
		}
		if req.Status != nil {
			if !validStatuses[*req.Status] {
				writeAppError(w, r, apperrors.Validation("invalid status (must be not_installed, downloaded, or activated)"))
				return
			}
			fields = append(fields, "status = ?")
			args = append(args, *req.Status)
		}
		if len(fields) == 0 {
			writeAppError(w, r, apperrors.Validation("no fields to update"))
			return
		}
		fields = append(fields, "updated_at = datetime('now')")
		args = append(args, id)
		query := fmt.Sprintf("UPDATE php_versions SET %s WHERE id = ?", strings.Join(fields, ", "))
		_, err = db.Exec(query, args...)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		auditLog(db, r, "php_version.update", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := paramInt(r, "id")
		if err != nil || id <= 0 {
			writeAppError(w, r, apperrors.Validation("invalid id"))
			return
		}
		_, err = db.Exec("DELETE FROM php_versions WHERE id = ?", id)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		auditLog(db, r, "php_version.delete", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	r.Post("/{id}/uninstall", func(w http.ResponseWriter, r *http.Request) {
		id, err := paramInt(r, "id")
		if err != nil || id <= 0 {
			writeAppError(w, r, apperrors.Validation("invalid id"))
			return
		}
		var version string
		err = db.QueryRow("SELECT version FROM php_versions WHERE id = ?", id).Scan(&version)
		if err != nil {
			writeAppError(w, r, apperrors.NotFound("PHP version", id))
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), aptTimeout)
		defer cancel()
		args := append([]string{"-n", "apt-get", "purge", "-y"}, phpPkgs(version)...)
		output, err := runApt(ctx, args)
		if err != nil {
			writeAppError(w, r, apperrors.Internal(fmt.Sprintf("uninstall failed: %s", string(output)), err))
			return
		}
		db.Exec(`UPDATE php_versions SET status = 'not_installed', socket_path = '', updated_at = datetime('now') WHERE id = ?`, id)
		auditLog(db, r, "php_version.uninstall", logging.Fields{"id": id, "version": version})
		jsonResp(w, 200, map[string]string{"status": "uninstalled"})
	})

	r.Post("/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		id, err := paramInt(r, "id")
		if err != nil || id <= 0 {
			writeAppError(w, r, apperrors.Validation("invalid id"))
			return
		}
		dl := getDl(id)
		if dl != nil {
			dl.mu.Lock()
			active := dl.Status == "queued" || dl.Status == "downloading"
			dl.mu.Unlock()
			if active {
				writeAppError(w, r, apperrors.Conflict("download already in progress for this version"))
				return
			}
		}
		var version, currentStatus string
		err = db.QueryRow("SELECT version, status FROM php_versions WHERE id = ?", id).Scan(&version, &currentStatus)
		if err != nil {
			writeAppError(w, r, apperrors.NotFound("PHP version", id))
			return
		}
		if currentStatus != "not_installed" {
			writeAppError(w, r, apperrors.Validation("version must be not_installed to download"))
			return
		}
		ensurePhpRepo()
		ctx, cancel := context.WithTimeout(context.Background(), aptTimeout)
		d := &phpDownload{
			ID:       id,
			Version:  version,
			Status:   "queued",
			Progress: "Waiting for package lock...",
			cancel:   cancel,
		}
		setDl(id, d)
		go runDownload(ctx, db, d, version)
		jsonResp(w, 200, map[string]interface{}{"status": "queued", "id": id})
	})

	r.Post("/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		id, err := paramInt(r, "id")
		if err != nil || id <= 0 {
			writeAppError(w, r, apperrors.Validation("invalid id"))
			return
		}
		dl := getDl(id)
		if dl == nil {
			writeAppError(w, r, apperrors.NotFound("active download", id))
			return
		}
		dl.mu.Lock()
		if dl.Status != "queued" && dl.Status != "downloading" {
			dl.mu.Unlock()
			writeAppError(w, r, apperrors.Validation("download is not active"))
			return
		}
		dl.Status = "cancelled"
		dl.Progress = "Cancelled"
		dl.mu.Unlock()
		dl.cancel()
		jsonResp(w, 200, map[string]string{"status": "cancelled"})
	})

	r.Get("/downloads/status", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, 200, map[string]interface{}{"downloads": getAllDls()})
	})

	r.Post("/{id}/activate", func(w http.ResponseWriter, r *http.Request) {
		id, err := paramInt(r, "id")
		if err != nil || id <= 0 {
			writeAppError(w, r, apperrors.Validation("invalid id"))
			return
		}
		var currentStatus string
		err = db.QueryRow("SELECT status FROM php_versions WHERE id = ?", id).Scan(&currentStatus)
		if err != nil {
			writeAppError(w, r, apperrors.NotFound("PHP version", id))
			return
		}
		if currentStatus != "downloaded" {
			writeAppError(w, r, apperrors.Validation("version must be downloaded first before activating"))
			return
		}
		_, err = db.Exec(`UPDATE php_versions SET status = 'activated', updated_at = datetime('now') WHERE id = ?`, id)
		if err != nil {
			writeAppError(w, r, apperrors.Internal("activate failed", err))
			return
		}
		auditLog(db, r, "php_version.activate", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "activated"})
	})

	r.Post("/{id}/deactivate", func(w http.ResponseWriter, r *http.Request) {
		id, err := paramInt(r, "id")
		if err != nil || id <= 0 {
			writeAppError(w, r, apperrors.Validation("invalid id"))
			return
		}
		var currentStatus string
		err = db.QueryRow("SELECT status FROM php_versions WHERE id = ?", id).Scan(&currentStatus)
		if err != nil {
			writeAppError(w, r, apperrors.NotFound("PHP version", id))
			return
		}
		if currentStatus != "activated" {
			writeAppError(w, r, apperrors.Validation("only activated versions can be deactivated"))
			return
		}
		_, err = db.Exec(`UPDATE php_versions SET status = 'downloaded', updated_at = datetime('now') WHERE id = ?`, id)
		if err != nil {
			writeAppError(w, r, apperrors.Internal("deactivate failed", err))
			return
		}
		auditLog(db, r, "php_version.deactivate", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deactivated"})
	})

	r.Post("/{id}/update", func(w http.ResponseWriter, r *http.Request) {
		id, err := paramInt(r, "id")
		if err != nil || id <= 0 {
			writeAppError(w, r, apperrors.Validation("invalid id"))
			return
		}
		var version, currentStatus string
		err = db.QueryRow("SELECT version, status FROM php_versions WHERE id = ?", id).Scan(&version, &currentStatus)
		if err != nil {
			writeAppError(w, r, apperrors.NotFound("PHP version", id))
			return
		}
		if currentStatus != "activated" {
			writeAppError(w, r, apperrors.Validation("only activated versions can be updated"))
			return
		}
		ensurePhpRepo()
		ctx, cancel := context.WithTimeout(context.Background(), aptTimeout)
		defer cancel()
		args := append([]string{"-n", "apt-get", "install", "--only-upgrade", "-y"}, phpPkgs(version)...)
		output, err := runApt(ctx, args)
		if err != nil {
			writeAppError(w, r, apperrors.Internal(fmt.Sprintf("update failed: %s", string(output)), err))
			return
		}
		db.Exec(`UPDATE php_versions SET updated_at = datetime('now') WHERE id = ?`, id)
		auditLog(db, r, "php_version.update", logging.Fields{"id": id, "version": version})
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})
}

func runDownload(ctx context.Context, db *sql.DB, d *phpDownload, version string) {
	dl := getDl(d.ID)
	if dl == nil {
		return
	}

	socketPath := fmt.Sprintf("/run/php/php%s-fpm.sock", version)
	sudoArgs := append([]string{"-n", "apt-get", "install", "-y"}, phpPkgs(version)...)

	for attempt := 0; attempt < 13; attempt++ {
		select {
		case <-ctx.Done():
			dl.mu.Lock()
			dl.Status = "cancelled"
			dl.Progress = "Cancelled"
			dl.mu.Unlock()
			return
		default:
		}

		dl.mu.Lock()
		dl.Status = "downloading"
		dl.Progress = "Running apt-get install..."
		dl.mu.Unlock()

		cmd := exec.CommandContext(ctx, "sudo", sudoArgs...)
		output, err := cmd.CombinedOutput()
		if err == nil {
			db.Exec(`UPDATE php_versions SET status = 'downloaded', socket_path = ?, updated_at = datetime('now') WHERE id = ?`, socketPath, d.ID)
			dl.mu.Lock()
			dl.Status = "completed"
			dl.Progress = "Installation complete"
			dl.mu.Unlock()
			go func() {
				time.Sleep(30 * time.Second)
				dl.mu.Lock()
				if dl.cancel != nil {
					dl.cancel()
				}
				dl.mu.Unlock()
				setDl(d.ID, nil)
			}()
			return
		}

		if ctx.Err() != nil {
			dl.mu.Lock()
			dl.Status = "cancelled"
			dl.Progress = "Cancelled"
			dl.mu.Unlock()
			return
		}

		outStr := string(output)
		if _, ok := err.(*exec.ExitError); ok && strings.Contains(outStr, "dpkg") && strings.Contains(outStr, "lock") {
			dl.mu.Lock()
			dl.Progress = fmt.Sprintf("Waiting for package lock (attempt %d/12)...", attempt+1)
			dl.mu.Unlock()
			select {
			case <-ctx.Done():
				dl.mu.Lock()
				dl.Status = "cancelled"
				dl.mu.Unlock()
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		lines := strings.Split(strings.TrimSpace(outStr), "\n")
		progress := ""
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			if line != "" && !strings.HasPrefix(line, "Reading ") && !strings.HasPrefix(line, "Building ") &&
				!strings.HasPrefix(line, "Need to get") && !strings.HasPrefix(line, "After this") &&
				!strings.Contains(line, "upgraded") {
				progress = line
				break
			}
		}
		if progress == "" {
			progress = outStr
		}
		dl.mu.Lock()
		dl.Status = "failed"
		dl.Progress = progress
		dl.Error = outStr
		dl.mu.Unlock()
		return
	}

	dl.mu.Lock()
	dl.Status = "failed"
	dl.Error = "Timed out waiting for package lock"
	dl.mu.Unlock()
}

func ensurePhpRepo() {
	l := logging.NewDefault("php")
	entries, err := os.ReadDir("/etc/apt/sources.list.d/")
	if err != nil {
		l.Errorf("ensurePhpRepo: cannot read sources list dir: %v", err)
		return
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "sury") && strings.Contains(e.Name(), "php") {
			return
		}
	}

	codenameBytes, err := exec.Command("lsb_release", "-sc").Output()
	if err != nil {
		l.Errorf("ensurePhpRepo: cannot determine codename: %v", err)
		return
	}
	codename := strings.TrimSpace(string(codenameBytes))
	if codename == "" {
		l.Errorf("ensurePhpRepo: empty codename from lsb_release")
		return
	}

	if out, err := exec.Command("sudo", "-n", "curl", "-sSLo", "/usr/share/keyrings/deb.sury.org-php.gpg",
		"https://packages.sury.org/php/apt.gpg").CombinedOutput(); err != nil {
		l.Errorf("ensurePhpRepo: failed to install GPG key: %s", string(out))
		return
	}

	repoLine := fmt.Sprintf("deb [signed-by=/usr/share/keyrings/deb.sury.org-php.gpg] https://packages.sury.org/php/ %s main", codename)
	if out, err := exec.Command("sudo", "-n", "sh", "-c",
		fmt.Sprintf("echo '%s' > /etc/apt/sources.list.d/sury-php.list", repoLine)).CombinedOutput(); err != nil {
		l.Errorf("ensurePhpRepo: failed to write sources list: %s", string(out))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if out, err := runApt(ctx, []string{"apt-get", "update"}); err != nil {
		l.Errorf("ensurePhpRepo: apt-get update failed: %s", string(out))
	}
}

func runApt(ctx context.Context, args []string) ([]byte, error) {
	sudoArgs := append([]string{"-n"}, args...)
	cmd := exec.CommandContext(ctx, "sudo", sudoArgs...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return output, nil
	}
	if strings.Contains(string(output), "dpkg") && strings.Contains(string(output), "lock") {
		for i := 0; i < 12; i++ {
			select {
			case <-ctx.Done():
				return output, ctx.Err()
			case <-time.After(5 * time.Second):
			}
			cmd = exec.CommandContext(ctx, "sudo", sudoArgs...)
			output, err = cmd.CombinedOutput()
			if err == nil {
				return output, nil
			}
			if !strings.Contains(string(output), "dpkg") || !strings.Contains(string(output), "lock") {
				break
			}
		}
	}
	return output, err
}

func childPhpVersionRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT id, version, socket_path, status, created_at, updated_at
			FROM php_versions WHERE status = 'activated' ORDER BY version DESC`)
		if err != nil {
			getLogger(r).Errorf("child php versions query: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		type PhpVersion struct {
			ID         int    `json:"id"`
			Version    string `json:"version"`
			SocketPath string `json:"socket_path"`
			Status     string `json:"status"`
			CreatedAt  string `json:"created_at"`
			UpdatedAt  string `json:"updated_at"`
		}
		versions := make([]PhpVersion, 0)
		for rows.Next() {
			var v PhpVersion
			if err := rows.Scan(&v.ID, &v.Version, &v.SocketPath, &v.Status, &v.CreatedAt, &v.UpdatedAt); err != nil {
				getLogger(r).Warnf("scan child php row: %v", err)
				continue
			}
			versions = append(versions, v)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("child php rows iteration error: %v", err)
		}
		jsonResp(w, 200, versions)
	})

	r.Get("/current", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		var phpVersionID int
		var version, socketPath, status string
		err := db.QueryRow(`SELECT pv.id, pv.version, pv.socket_path, pv.status
			FROM account_php_version apv
			JOIN php_versions pv ON pv.id = apv.php_version_id
			WHERE apv.account_id = ?`, c.AccountID).Scan(&phpVersionID, &version, &socketPath, &status)
		if err != nil {
			defaultSocket := getEnvDefault("PHP_FPM_SOCKET", "/run/php/php8.3-fpm.sock")
			jsonResp(w, 200, map[string]interface{}{
				"id":          0,
				"version":     "",
				"socket_path": defaultSocket,
				"status":      "default",
			})
			return
		}
		jsonResp(w, 200, map[string]interface{}{
			"id":          phpVersionID,
			"version":     version,
			"socket_path": socketPath,
			"status":      status,
		})
	})

	r.Put("/select", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		var req struct {
			PhpVersionID int `json:"php_version_id"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		if req.PhpVersionID <= 0 {
			writeAppError(w, r, apperrors.Validation("invalid php_version_id"))
			return
		}
		var version, socketPath string
		err := db.QueryRow(`SELECT version, socket_path FROM php_versions WHERE id = ? AND status = 'activated'`,
			req.PhpVersionID).Scan(&version, &socketPath)
		if err != nil {
			writeAppError(w, r, apperrors.BadRequest("PHP version not available or not activated"))
			return
		}
		if _, err := os.Stat(socketPath); os.IsNotExist(err) {
			getLogger(r).Errorf("PHP-FPM socket not found at %s", socketPath)
			writeAppError(w, r, apperrors.ServiceUnavailable("PHP-FPM socket is not available"))
			return
		}
		_, err = db.Exec(`INSERT INTO account_php_version (account_id, php_version_id, updated_at)
			VALUES (?, ?, datetime('now'))
			ON CONFLICT(account_id) DO UPDATE SET php_version_id = ?, updated_at = datetime('now')`,
			c.AccountID, req.PhpVersionID, req.PhpVersionID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}

		var containerIP string
		db.QueryRow("SELECT COALESCE(container_ip, '') FROM docker_containers WHERE account_id = ?", c.AccountID).Scan(&containerIP)
		rows, err := db.Query(`SELECT domain, doc_root FROM domains WHERE account_id = ?`, c.AccountID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var domain, docRoot string
				if err := rows.Scan(&domain, &docRoot); err != nil {
					continue
				}
				writeNginxVhost(sanitizeDomain(domain), docRoot, containerIP, 0, "")
				// The :80 rewrite wipes managed blocks — restore 443 + redirect.
				reapplyManagedBlocks(db, domain)
			}
			if err := rows.Err(); err != nil {
				getLogger(r).Errorf("php vhost sync rows iteration error: %v", err)
			}
		}
		reloadNginx()
		auditLog(db, r, "php_version.select", logging.Fields{
			"account_id": c.AccountID,
			"version":    version,
		})
		jsonResp(w, 200, map[string]string{"status": "switched", "version": version})
	})
}
