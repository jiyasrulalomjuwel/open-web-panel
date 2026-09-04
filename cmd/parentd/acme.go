package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
)

var acmeChallenges sync.Map

// acmeRateLimit guards per-account concurrent/short-interval issuance so a
// single tenant (or a compromised tenant token) can't exhaust the Let's Encrypt
// per-account weekly quota for the whole server.
var acmeIssuanceMu sync.Mutex
var acmeLockoutByAccount = map[int]time.Time{}
const acmeAccountCooldown = time.Hour

func acmeCooldownPassed(accountID int) bool {
	acmeIssuanceMu.Lock()
	defer acmeIssuanceMu.Unlock()
	last, ok := acmeLockoutByAccount[accountID]
	if !ok || time.Since(last) >= acmeAccountCooldown {
		acmeLockoutByAccount[accountID] = time.Now()
		return true
	}
	return false
}

func getOrCreateAccountKey(db *sql.DB) crypto.Signer {
	l := logging.NewDefault("acme")
	var keyPEM string
	err := db.QueryRow("SELECT value FROM server_config WHERE key_name = 'acme_account_key'").Scan(&keyPEM)
	if err == nil && keyPEM != "" {
		block, _ := pem.Decode([]byte(keyPEM))
		if block != nil {
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err == nil {
				if signer, ok := key.(crypto.Signer); ok {
					return signer
				}
			}
		}
		l.Warnf("Stored account key invalid, generating new one")
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		l.Errorf("Generate account key error: %v", err)
		return nil
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		l.Errorf("Marshal account key error: %v", err)
		return nil
	}
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER}))

	db.Exec("INSERT OR REPLACE INTO server_config (key_name, value) VALUES ('acme_account_key', ?)", keyPEM)
	l.Infof("New account key generated")
	return key
}

func ensureACMERegistration(client *acme.Client, db *sql.DB) error {
	l := logging.NewDefault("acme")
	var registered string
	err := db.QueryRow("SELECT value FROM server_config WHERE key_name = 'acme_registered'").Scan(&registered)
	if err == nil && registered == "true" {
		return nil
	}

	_, err = client.Register(context.Background(), &acme.Account{}, func(tosURL string) bool {
		return true
	})
	if err != nil {
		if strings.Contains(err.Error(), "409") || strings.Contains(err.Error(), "already registered") {
			db.Exec("INSERT OR REPLACE INTO server_config (key_name, value) VALUES ('acme_registered', 'true')")
			return nil
		}
		l.Errorf("ACME registration failed: %v", err)
		return apperrors.ExternalAPI("ACME", fmt.Errorf("registration failed: %w", err))
	}

	db.Exec("INSERT OR REPLACE INTO server_config (key_name, value) VALUES ('acme_registered', 'true')")
	l.Infof("Account registered with Let's Encrypt")
	return nil
}

func issueLetsEncryptCert(db *sql.DB, certID int64, domains ...string) error {
	l := logging.NewDefault("acme")
	if len(domains) == 0 {
		return apperrors.Validation("no domains provided")
	}
	primary := domains[0]
	l.Infof("Issuing cert for %v (certID=%d)", domains, certID)

	accountKey := getOrCreateAccountKey(db)
	if accountKey == nil {
		return apperrors.Internal("no ACME account key available", nil)
	}

	// Per-account cooldown to protect the shared Let's Encrypt quota.
	var accountID int
	if err := db.QueryRow("SELECT account_id FROM ssl_certs WHERE id = ?", certID).Scan(&accountID); err != nil {
		accountID = -int(certID)
	}
	if !acmeCooldownPassed(accountID) {
		return apperrors.RateLimit(int(acmeAccountCooldown.Seconds()))
	}
	client := &acme.Client{
		Key:          accountKey,
		DirectoryURL: acme.LetsEncryptURL,
	}
	ctx := context.Background()

	if err := ensureACMERegistration(client, db); err != nil {
		return apperrors.ExternalAPI("ACME", fmt.Errorf("registration failed: %w", err))
	}

	ids := make([]acme.AuthzID, len(domains))
	for i, d := range domains {
		ids[i] = acme.AuthzID{Type: "dns", Value: d}
	}
	order, err := client.AuthorizeOrder(ctx, ids)
	if err != nil {
		return apperrors.ExternalAPI("ACME", fmt.Errorf("authorize order failed: %w", err))
	}

	for _, authzURL := range order.AuthzURLs {
		authz, err := client.GetAuthorization(ctx, authzURL)
		if err != nil {
			return apperrors.ExternalAPI("ACME", fmt.Errorf("get authorization failed: %w", err))
		}
		if authz.Status != acme.StatusPending {
			continue
		}

		var chal *acme.Challenge
		for _, c := range authz.Challenges {
			if c.Type == "http-01" {
				chal = c
				break
			}
		}
		if chal == nil {
			return apperrors.ExternalAPI("ACME", fmt.Errorf("no http-01 challenge for %s", authz.Identifier.Value))
		}

		resp, err := client.HTTP01ChallengeResponse(chal.Token)
		if err != nil {
			return apperrors.ExternalAPI("ACME", fmt.Errorf("challenge response failed: %w", err))
		}
		acmeChallenges.Store(chal.Token, resp)

		if _, err := client.Accept(ctx, chal); err != nil {
			acmeChallenges.Delete(chal.Token)
			return apperrors.ExternalAPI("ACME", fmt.Errorf("accept challenge failed: %w", err))
		}

		if _, err := client.WaitAuthorization(ctx, authz.URI); err != nil {
			acmeChallenges.Delete(chal.Token)
			return apperrors.ExternalAPI("ACME", fmt.Errorf("wait authorization failed: %w", err))
		}
		acmeChallenges.Delete(chal.Token)
		l.Infof("Domain %s authorized", authz.Identifier.Value)
	}

	order, err = client.WaitOrder(ctx, order.URI)
	if err != nil {
		return apperrors.ExternalAPI("ACME", fmt.Errorf("wait order failed: %w", err))
	}

	certKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return apperrors.Internal("cert key generation failed", err)
	}

	csrTemplate := &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: primary},
		DNSNames: domains,
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, certKey)
	if err != nil {
		return apperrors.Internal("CSR creation failed", err)
	}

	certDER, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csrDER, true)
	if err != nil {
		return apperrors.ExternalAPI("ACME", fmt.Errorf("create order cert failed: %w", err))
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER[0]})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(certKey)})

	var fullchain []byte
	fullchain = append(fullchain, certPEM...)
	for _, extra := range certDER[1:] {
		fullchain = append(fullchain, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: extra})...)
	}

	cert, err := x509.ParseCertificate(certDER[0])
	expiresAt := time.Now().Add(90 * 24 * time.Hour)
	if err == nil {
		expiresAt = cert.NotAfter
	}

	_, err = db.Exec(`UPDATE ssl_certs SET certificate=?, private_key=?, issuer=?, expires_at=?, status='issued', last_error='' WHERE id=?`,
		string(fullchain), string(keyPEM), "Let's Encrypt", expiresAt.Format("2006-01-02"), certID)
	if err != nil {
		return apperrors.Internal("failed to store SSL certificate", err)
	}

	certFile := getHomesBase() + "ssl/" + sanitizeFilename(primary) + ".crt"
	keyFile := getHomesBase() + "ssl/" + sanitizeFilename(primary) + ".key"
	os.MkdirAll(getHomesBase()+"ssl/", 0700)
	os.WriteFile(certFile, fullchain, 0644)
	os.WriteFile(keyFile, keyPEM, 0600)

	if err := addNginxSSL(db, primary); err != nil {
		l.Errorf("HTTPS block failed for %s: %v", primary, err)
		db.Exec("UPDATE ssl_certs SET last_error = COALESCE(last_error,'') || ' [nginx: ' || ? || ']' WHERE id = ?", err.Error(), certID)
	} else {
		db.Exec("UPDATE domains SET ssl_enabled = 1 WHERE domain = ?", primary)
		enableForceHTTPSIfDefault(db, primary)
	}

	l.Infof("Certificate issued for %s (certID=%d, expires=%s)", primary, certID, expiresAt.Format("2006-01-02"))
	return nil
}

// owpSSLBegin/owpSSLEnd delimit the panel-managed HTTPS block inside a vhost
// file so it can be rewritten (cert rotation, IP change, static-vs-container
// switch) or removed (cert delete) without touching the :80 server.
const (
	owpSSLBegin = "# OWP-SSL-BEGIN (managed by OpenWebPanel, do not edit)"
	owpSSLEnd   = "# OWP-SSL-END"
	owpHTTPSBegin = "# OWP-FORCE-HTTPS-BEGIN (managed by OpenWebPanel, do not edit)"
	owpHTTPSEnd   = "# OWP-FORCE-HTTPS-END"
)

// forceHTTPSDefault reports whether newly issued certificates should redirect
// HTTP to HTTPS automatically (server_config force_https_default, default on).
func forceHTTPSDefault(db *sql.DB) bool {
	var v string
	if err := db.QueryRow("SELECT value FROM server_config WHERE key_name = 'force_https_default'").Scan(&v); err != nil {
		return true
	}
	return v != "0" && v != "false"
}

// stripForceHTTPSSnippet removes a previously injected redirect block.
func stripForceHTTPSSnippet(content string) string {
	if start := strings.Index(content, owpHTTPSBegin); start >= 0 {
		if end := strings.Index(content[start:], owpHTTPSEnd); end >= 0 {
			return content[:start] + content[start+end+len(owpHTTPSEnd):]
		}
	}
	return content
}

// applyForceHTTPS reconciles the :80 vhost with domains.force_https. The
// redirect lives inside `location /` so the ACME-challenge location (^~,
// higher precedence) keeps serving HTTP validation. Returns an error when
// HTTPS is requested but no certificate files exist yet.
func applyForceHTTPS(db *sql.DB, dom string) error {
	nginxMu.Lock()
	defer nginxMu.Unlock()
	return doApplyForceHTTPS(db, dom)
}

func doApplyForceHTTPS(db *sql.DB, dom string) error {
	l := logging.NewDefault("ssl")
	safe := sanitizeDomain(dom)
	vhostPath := vhostDir + safe + ".conf"
	raw, err := os.ReadFile(vhostPath)
	if err != nil {
		return fmt.Errorf("cannot read vhost %s: %w", vhostPath, err)
	}
	content := stripForceHTTPSSnippet(string(raw))

	var force int
	db.QueryRow(`SELECT COALESCE(MAX(force_https),0) FROM domains d
		JOIN accounts a ON a.id = d.account_id
		WHERE d.domain = ? AND a.status = 'active'`, dom).Scan(&force)
	if force != 1 {
		if content == string(raw) {
			return nil
		}
		if err := writeVhostFile(vhostPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("write vhost: %w", err)
		}
		if err := reloadNginxErr(); err != nil {
			return fmt.Errorf("nginx reload: %w", err)
		}
		l.Infof("HTTPS redirect removed for %s", dom)
		return nil
	}

	safeFile := sanitizeFilename(dom)
	if _, err := os.Stat(getHomesBase() + "ssl/" + safeFile + ".crt"); err != nil {
		return fmt.Errorf("cannot force HTTPS for %s: issue an SSL certificate first", dom)
	}
	if _, err := os.Stat(getHomesBase() + "ssl/" + safeFile + ".key"); err != nil {
		return fmt.Errorf("cannot force HTTPS for %s: issue an SSL certificate first", dom)
	}

	anchor := "    location / {"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		return fmt.Errorf("location / block not found in %s", vhostPath)
	}
	snippet := anchor + "\n" + owpHTTPSBegin + "\n" +
		"        if ($scheme = http) { return 301 https://$host$request_uri; }\n" +
		"        " + owpHTTPSEnd
	content = content[:idx] + snippet + content[idx+len(anchor):]
	if err := writeVhostFile(vhostPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write vhost: %w", err)
	}
	if err := reloadNginxErr(); err != nil {
		return fmt.Errorf("nginx reload: %w", err)
	}
	l.Infof("HTTPS redirect enabled for %s", dom)
	return nil
}

// reapplyManagedBlocks restores the 443 server, force-HTTPS redirect,
// redirect rules and error pages for a domain after a plain :80 rewrite
// (e.g. PHP version switch) wiped them. Safe to call unconditionally: each
// step is a no-op when not applicable.
func reapplyManagedBlocks(db *sql.DB, dom string) {
	l := logging.NewDefault("ssl")
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM ssl_certs s JOIN accounts a ON a.id = s.account_id
		WHERE s.domain = ? AND s.status IN ('issued', 'self-signed') AND a.status = 'active'`, dom).Scan(&n)
	if n > 0 {
		if err := addNginxSSL(db, dom); err != nil {
			l.Errorf("reapply HTTPS block failed for %s: %v", dom, err)
		}
	}
	var force int
	db.QueryRow(`SELECT COALESCE(MAX(d.force_https),0) FROM domains d
		JOIN accounts a ON a.id = d.account_id
		WHERE d.domain = ? AND a.status = 'active'`, dom).Scan(&force)
	if force == 1 {
		if err := applyForceHTTPS(db, dom); err != nil {
			l.Errorf("reapply HTTPS redirect failed for %s: %v", dom, err)
		}
	}
	if err := syncDomainRedirects(db, dom); err != nil {
		l.Errorf("reapply redirects failed for %s: %v", dom, err)
	}
	if err := syncDomainErrorPages(db, dom); err != nil {
		l.Errorf("reapply error pages failed for %s: %v", dom, err)
	}
}

// enableForceHTTPSIfDefault turns the redirect on for domains whose default
// says so. Called after a certificate is installed successfully.
func enableForceHTTPSIfDefault(db *sql.DB, dom string) {
	if !forceHTTPSDefault(db) {
		return
	}
	db.Exec("UPDATE domains SET force_https = 1 WHERE domain = ?", dom)
	if err := applyForceHTTPS(db, dom); err != nil {
		l := logging.NewDefault("ssl")
		l.Errorf("auto HTTPS redirect failed for %s: %v", dom, err)
		db.Exec("UPDATE ssl_certs SET last_error = COALESCE(last_error,'') || ' [https-redirect: ' || ? || ']' WHERE domain = ?", err.Error(), dom)
	}
}

// removeNginxSSL strips the managed 443 block for a domain, deletes its cert
// files and reloads nginx. Used when a certificate is deleted.
func removeNginxSSL(db *sql.DB, dom string) error {
	nginxMu.Lock()
	defer nginxMu.Unlock()
	return doRemoveNginxSSL(db, dom)
}

func doRemoveNginxSSL(db *sql.DB, dom string) error {
	l := logging.NewDefault("ssl")
	safe := sanitizeDomain(dom)
	vhostPath := vhostDir + safe + ".conf"
	if existing, err := os.ReadFile(vhostPath); err == nil {
		updated := stripSSLBlock(stripForceHTTPSSnippet(string(existing)))
		if updated != string(existing) {
			if err := writeVhostFile(vhostPath, []byte(updated), 0644); err != nil {
				return fmt.Errorf("write vhost: %w", err)
			}
		}
	}
	safeFile := sanitizeFilename(dom)
	os.Remove(getHomesBase() + "ssl/" + safeFile + ".crt")
	os.Remove(getHomesBase() + "ssl/" + safeFile + ".key")
	db.Exec("UPDATE domains SET ssl_enabled = 0, force_https = 0 WHERE domain = ?", dom)
	if err := reloadNginxErr(); err != nil {
		return fmt.Errorf("nginx reload: %w", err)
	}
	l.Infof("HTTPS removed for %s", dom)
	return nil
}

// stripSSLBlock removes a previously managed 443 block. Legacy blocks written
// before markers existed are detected by their distinctive header instead.
func stripSSLBlock(content string) string {
	if start := strings.Index(content, owpSSLBegin); start >= 0 {
		if end := strings.Index(content[start:], owpSSLEnd); end >= 0 {
			return content[:start] + content[start+end+len(owpSSLEnd):]
		}
	}
	// Legacy: appended block starting at "\nserver {\n\tlisten 443 ssl;"
	if idx := strings.Index(content, "\nserver {\n\tlisten 443 ssl;"); idx >= 0 {
		return content[:idx] + "\n"
	}
	return content
}

func addNginxSSL(db *sql.DB, dom string) error {
	nginxMu.Lock()
	defer nginxMu.Unlock()
	return doAddNginxSSL(db, dom)
}

func doAddNginxSSL(db *sql.DB, dom string) error {
	l := logging.NewDefault("ssl")
	// Vhost files are keyed by sanitized domain (see syncNginxVhosts); the
	// server_name keeps the real domain. Cert files use sanitizeFilename,
	// matching the self-signed/custom writers.
	safe := sanitizeDomain(dom)
	vhostPath := vhostDir + safe + ".conf"
	existing, err := os.ReadFile(vhostPath)
	if err != nil {
		l.Errorf("Cannot read vhost %s: %v", vhostPath, err)
		return fmt.Errorf("cannot read vhost %s: %w", vhostPath, err)
	}
	content := string(existing)

	safeFile := sanitizeFilename(dom)
	certFile := getHomesBase() + "ssl/" + safeFile + ".crt"
	keyFile := getHomesBase() + "ssl/" + safeFile + ".key"
	if _, err := os.Stat(certFile); err != nil {
		return fmt.Errorf("certificate file missing: %s", certFile)
	}
	if _, err := os.Stat(keyFile); err != nil {
		return fmt.Errorf("key file missing: %s", keyFile)
	}

	containerIP := extractContainerIP(content)
	staticMode := containerIP == ""
	if staticMode {
		l.Infof("No container IP for %s, serving static/PHP-FPM on 443", dom)
	}

	locationBlock := ""
	if staticMode {
		phpFpmSocket := getEnvDefault("PHP_FPM_SOCKET", "/run/php/php8.3-fpm.sock")
		// Mirror the :80 static server (root is inherited from the file-scope
		// root directive extracted below).
		locationBlock = `    location / {
        autoindex on;
        try_files $uri $uri/ /index.php?$args;
    }

    location ~ \.php$ {
        fastcgi_pass unix:` + phpFpmSocket + `;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_param HTTPS on;
        include fastcgi_params;
    }`
	} else {
		locationBlock = `    location / {
	    proxy_pass http://` + containerIP + `;
	    proxy_http_version 1.1;
	    proxy_set_header Upgrade $http_upgrade;
	    proxy_set_header Connection "upgrade";
	    proxy_set_header Host $host;
	    proxy_set_header X-Real-IP $remote_addr;
	    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
	    proxy_set_header X-Forwarded-Proto $scheme;
	    proxy_buffering off;
    }`
	}

	// Inherit the :80 server's root for static mode by re-reading it from the
	// existing vhost; fall back to no root (proxy/containers unaffected).
	rootLine := ""
	if staticMode {
		for _, line := range strings.Split(content, "\n") {
			t := strings.TrimSpace(line)
			if strings.HasPrefix(t, "root ") && strings.HasSuffix(t, ";") {
				rootLine = "    " + t + "\n"
				break
			}
		}
	}

	sslBlock := fmt.Sprintf(`%s
server {
	listen 443 ssl;
	listen [::]:443 ssl;
	server_name %s www.%s;
	client_max_body_size 2048M;

	ssl_certificate %s;
	ssl_certificate_key %s;
	ssl_protocols TLSv1.2 TLSv1.3;
	add_header Strict-Transport-Security "max-age=63072000; includeSubDomains" always;

	access_log `+getNginxLogDir()+`/%s.access.log;
	error_log `+getNginxLogDir()+`/%s.error.log;

%s%s
    location ^~ /.well-known/acme-challenge/ {
	    proxy_pass http://127.0.0.1:9000;
	    proxy_set_header Host $host;
    }
}
%s
`, owpSSLBegin, dom, dom, certFile, keyFile, safe, safe, rootLine, locationBlock, owpSSLEnd)

	var hlEnabled int
	var hlDomains string
	db.QueryRow("SELECT COALESCE(enabled,0), COALESCE(allowed_domains,'') FROM hotlink_protection WHERE account_id IN (SELECT account_id FROM domains WHERE domain = ?)", dom).Scan(&hlEnabled, &hlDomains)
	sslBlock = addHotlinkBlock(sslBlock, hlEnabled, hlDomains)

	content = stripSSLBlock(content)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content = content + sslBlock
	if err := writeVhostFile(vhostPath, []byte(content), 0644); err != nil {
		l.Errorf("Write vhost SSL: %v", err)
		return fmt.Errorf("write vhost: %w", err)
	}

	if err := reloadNginxErr(); err != nil {
		return fmt.Errorf("nginx reload: %w", err)
	}
	db.Exec("UPDATE domains SET ssl_enabled = 1 WHERE domain = ?", dom)
	l.Infof("HTTPS enabled for %s", dom)
	return nil
}

// extractContainerIP returns the upstream of the site's `location /` block.
// It must ignore the ACME-challenge location (which always points at the
// panel on :9000) — otherwise static sites get a 443 block proxying to the
// login page instead of their own files.
func extractContainerIP(content string) string {
	inRoot := false
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "location / {" {
			inRoot = true
			continue
		}
		if inRoot {
			if t == "}" {
				break
			}
			if strings.HasPrefix(t, "proxy_pass http://") {
				val := strings.TrimPrefix(t, "proxy_pass http://")
				val = strings.TrimSuffix(val, ";")
				return strings.TrimSpace(val)
			}
		}
	}
	return ""
}

// renewDueCerts renews Let's Encrypt certs expiring within 30 days. Only rows
// the operator left on auto_renew are touched, and a failure keeps the old
// row (and its served files) intact instead of flipping it to failed.
func renewDueCerts(db *sql.DB) {
	l := logging.NewDefault("acme")
	rows, err := db.Query(`SELECT id, domain FROM ssl_certs
		WHERE status = 'issued' AND issuer = "Let's Encrypt" AND auto_renew = 1
		AND expires_at < datetime('now', '+30 days')`)
	if err != nil {
		l.Errorf("cert renewal query: %v", err)
		return
	}
	type due struct {
		id     int64
		domain string
	}
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.domain); err != nil {
			l.Warnf("scan renewal row: %v", err)
			continue
		}
		list = append(list, d)
	}
	if err := rows.Err(); err != nil {
		l.Errorf("renewal rows iteration error: %v", err)
	}
	rows.Close()
	for _, d := range list {
		www := "www." + d.domain
		var aid int
		db.QueryRow("SELECT account_id FROM ssl_certs WHERE id = ?", d.id).Scan(&aid)
		if !domainOwnedByAccount(db, aid, d.domain) || !domainOwnedByAccount(db, aid, www) {
			www = ""
		}
		l.Infof("Auto-renewing cert for %s (certID=%d)", d.domain, d.id)
		db.Exec("UPDATE ssl_certs SET status = 'issuing' WHERE id = ?", d.id)
		names := []string{d.domain}
		if www != "" {
			names = append(names, www)
		}
		if err := issueLetsEncryptCert(db, d.id, names...); err != nil {
			// Keep serving the previous certificate: revert to issued and
			// record why the renewal failed.
			l.Errorf("Auto-renew failed for %s: %v", d.domain, err)
			db.Exec("UPDATE ssl_certs SET status = 'issued', last_error = ? WHERE id = ?", "renew: "+err.Error(), d.id)
		}
	}
}

func startCertRenewal(db *sql.DB) {
	go func() {
		// Sweep once at boot (a restart may have skipped the daily window),
		// then every 24h.
		renewDueCerts(db)
		ticker := time.NewTicker(24 * time.Hour)
		for range ticker.C {
			renewDueCerts(db)
		}
	}()
}

var acmeTokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func acmeChallengeHandler(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/.well-known/acme-challenge/")
	// Strict validation: ACME tokens are short base64url strings. Rejecting
	// anything else blocks path traversal (e.g. "../../ssl/evil.crt").
	if !acmeTokenRe.MatchString(token) {
		http.NotFound(w, r)
		return
	}
	if val, ok := acmeChallenges.Load(token); ok {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte(val.(string)))
		return
	}
	chalPath := filepath.Join(getHomesBase()+".well-known/acme-challenge/", token)
	if data, err := os.ReadFile(chalPath); err == nil {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(data)
		return
	}
	http.NotFound(w, r)
}
