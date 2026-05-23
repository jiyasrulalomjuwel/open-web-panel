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
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
)

// Global ACME challenge store (token → key authorization response)
var acmeChallenges sync.Map

// getOrCreateAccountKey loads or creates an ACME account private key
func getOrCreateAccountKey(db *sql.DB) crypto.Signer {
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
		log.Printf("[ACME] Stored account key invalid, generating new one")
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("[ACME] Generate account key: %v", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		log.Fatalf("[ACME] Marshal account key: %v", err)
	}
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER}))

	db.Exec("INSERT OR REPLACE INTO server_config (key_name, value) VALUES ('acme_account_key', ?)", keyPEM)
	log.Printf("[ACME] New account key generated")
	return key
}

// ensureACMERegistration registers the ACME account with Let's Encrypt
func ensureACMERegistration(client *acme.Client, db *sql.DB) error {
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
		return fmt.Errorf("register: %w", err)
	}

	db.Exec("INSERT OR REPLACE INTO server_config (key_name, value) VALUES ('acme_registered', 'true')")
	log.Printf("[ACME] Account registered with Let's Encrypt")
	return nil
}

// issueLetsEncryptCert performs the full ACME HTTP-01 flow for a domain
// domains includes the primary domain and any SANs (e.g. ["gmeil.sbs", "www.gmeil.sbs"])
func issueLetsEncryptCert(db *sql.DB, certID int64, domains ...string) error {
	if len(domains) == 0 {
		return fmt.Errorf("no domains provided")
	}
	primary := domains[0]
	log.Printf("[ACME] Issuing cert for %v (certID=%d)", domains, certID)

	accountKey := getOrCreateAccountKey(db)
	client := &acme.Client{
		Key:          accountKey,
		DirectoryURL: acme.LetsEncryptURL,
	}
	ctx := context.Background()

	if err := ensureACMERegistration(client, db); err != nil {
		return fmt.Errorf("acme registration: %w", err)
	}

	// Step 1: Create order with all domain names
	ids := make([]acme.AuthzID, len(domains))
	for i, d := range domains {
		ids[i] = acme.AuthzID{Type: "dns", Value: d}
	}
	order, err := client.AuthorizeOrder(ctx, ids)
	if err != nil {
		return fmt.Errorf("authorize order: %w", err)
	}

	// Step 2: Fulfill each pending authorization
	for _, authzURL := range order.AuthzURLs {
		authz, err := client.GetAuthorization(ctx, authzURL)
		if err != nil {
			return fmt.Errorf("get authorization: %w", err)
		}
		if authz.Status != acme.StatusPending {
			continue
		}

		// Find HTTP-01 challenge
		var chal *acme.Challenge
		for _, c := range authz.Challenges {
			if c.Type == "http-01" {
				chal = c
				break
			}
		}
		if chal == nil {
			return fmt.Errorf("no http-01 challenge for %s", authz.Identifier.Value)
		}

		// Compute response and store in global map
		resp, err := client.HTTP01ChallengeResponse(chal.Token)
		if err != nil {
			return fmt.Errorf("challenge response: %w", err)
		}
		acmeChallenges.Store(chal.Token, resp)
		defer acmeChallenges.Delete(chal.Token)

		// Accept the challenge
		if _, err := client.Accept(ctx, chal); err != nil {
			return fmt.Errorf("accept challenge: %w", err)
		}

		// Wait for CA to validate
		if _, err := client.WaitAuthorization(ctx, authz.URI); err != nil {
			return fmt.Errorf("wait authorization: %w", err)
		}
		log.Printf("[ACME] Domain %s authorized", authz.Identifier.Value)
	}

	// Step 3: Wait for order to be ready
	order, err = client.WaitOrder(ctx, order.URI)
	if err != nil {
		return fmt.Errorf("wait order: %w", err)
	}

	// Step 4: Generate certificate key and CSR with all domains
	certKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("cert key gen: %w", err)
	}

	csrTemplate := &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: primary},
		DNSNames: domains,
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, certKey)
	if err != nil {
		return fmt.Errorf("csr: %w", err)
	}

	// Step 5: Submit CSR and get certificate
	certDER, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csrDER, true)
	if err != nil {
		return fmt.Errorf("create order cert: %w", err)
	}

	// Encode
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER[0]})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(certKey)})

	// Append fullchain if bundle
	var fullchain []byte
	fullchain = append(fullchain, certPEM...)
	for _, extra := range certDER[1:] {
		fullchain = append(fullchain, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: extra})...)
	}

	// Parse expiry
	cert, err := x509.ParseCertificate(certDER[0])
	expiresAt := time.Now().Add(90 * 24 * time.Hour)
	if err == nil {
		expiresAt = cert.NotAfter
	}

	// Store in DB
	_, err = db.Exec(`UPDATE ssl_certs SET certificate=?, private_key=?, issuer=?, expires_at=?, status='issued' WHERE id=?`,
		string(fullchain), string(keyPEM), "Let's Encrypt", expiresAt.Format("2006-01-02"), certID)
	if err != nil {
		return fmt.Errorf("db update: %w", err)
	}

	// Write cert files for nginx
	certFile := getHomesBase() + "ssl/" + primary + ".crt"
	keyFile := getHomesBase() + "ssl/" + primary + ".key"
	os.MkdirAll(getHomesBase()+"ssl/", 0700)
	os.WriteFile(certFile, fullchain, 0644)
	os.WriteFile(keyFile, keyPEM, 0600)

	// Add SSL config to nginx vhost
	addNginxSSL(primary)

	log.Printf("[ACME] Certificate issued for %s (certID=%d, expires=%s)", primary, certID, expiresAt.Format("2006-01-02"))
	return nil
}

// addNginxSSL augments the domain's vhost with SSL listening on port 443
// Note: parameter is named `dom` to avoid shadowing in the file since `domain` was renamed to `primary` in the caller
func addNginxSSL(dom string) {
	vhostPath := vhostDir + dom + ".conf"
	existing, err := os.ReadFile(vhostPath)
	if err != nil {
		log.Printf("[SSL] Cannot read vhost %s: %v", vhostPath, err)
		return
	}
	content := string(existing)

	// Check if SSL is already configured
	if strings.Contains(content, "listen 443 ssl") {
		return
	}

	certFile := getHomesBase() + "ssl/" + dom + ".crt"
	keyFile := getHomesBase() + "ssl/" + dom + ".key"

	sslBlock := fmt.Sprintf(`
server {
	listen 443 ssl;
	listen [::]:443 ssl;
	server_name %s www.%s;
	root %s;
	index index.html index.htm index.php;

	ssl_certificate %s;
	ssl_certificate_key %s;
	ssl_protocols TLSv1.2 TLSv1.3;

	access_log ` + getNginxLogDir() + `/%s.access.log;
	error_log ` + getNginxLogDir() + `/%s.error.log;

	location / {
	    try_files $uri $uri/ /index.php?$args;
	}

	location ~ \.php$ {
	    fastcgi_pass unix:/run/php/php8.3-fpm.sock;
	    fastcgi_index index.php;
	    fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
	    include fastcgi_params;
	}

	location ^~ /.well-known/acme-challenge/ {
	    proxy_pass http://127.0.0.1:9000;
	    proxy_set_header Host $host;
	}

	location ~ /\. {
	    deny all;
	}

	location ~ /\.owp {
	    deny all;
	}
}
`, dom, dom, extractDocRoot(content), certFile, keyFile, dom, dom)

	// Insert SSL block before the closing of the config
	content = content + sslBlock
	if err := os.WriteFile(vhostPath, []byte(content), 0644); err != nil {
		log.Printf("[SSL] Write vhost SSL: %v", err)
		return
	}

	reloadNginx()
	log.Printf("[SSL] HTTPS enabled for %s", dom)
}

// extractDocRoot extracts the root directive from a nginx vhost config
func extractDocRoot(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "root ") {
			return strings.TrimSuffix(strings.TrimPrefix(line, "root "), ";")
		}
	}
	return "/var/www/html"
}

// startCertRenewal checks certificates daily and renews those expiring within 30 days
func startCertRenewal(db *sql.DB) {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		for range ticker.C {
			rows, err := db.Query(`SELECT id, domain FROM ssl_certs
				WHERE status = 'issued' AND issuer = "Let's Encrypt"
				AND expires_at < datetime('now', '+30 days')`)
			if err != nil {
				continue
			}
			for rows.Next() {
				var id int64
				var domain string
				rows.Scan(&id, &domain)
				log.Printf("[ACME] Auto-renewing cert for %s (certID=%d)", domain, id)
				db.Exec("UPDATE ssl_certs SET status = 'issuing' WHERE id = ?", id)
				if err := issueLetsEncryptCert(db, id, domain, "www."+domain); err != nil {
					log.Printf("[ACME] Auto-renew failed for %s: %v", domain, err)
					db.Exec("UPDATE ssl_certs SET status = 'failed' WHERE id = ?", id)
				}
			}
			rows.Close()
		}
	}()
}

// acmeChallengeHandler serves HTTP-01 challenge responses from the in-memory store
func acmeChallengeHandler(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/.well-known/acme-challenge/")
	if val, ok := acmeChallenges.Load(token); ok {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte(val.(string)))
		return
	}
	// Fall back to filesystem (for manually-placed challenges)
	chalPath := getHomesBase() + ".well-known/acme-challenge/" + token
	if data, err := os.ReadFile(chalPath); err == nil {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(data)
		return
	}
	http.NotFound(w, r)
}
