package main

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// ---------- SSL Certificate routes ----------

func certRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		var rows *sql.Rows
		var err error
		if c.Scope == "child" {
			rows, err = db.Query(`SELECT id, account_id, domain_id, domain,
				COALESCE(issuer,''), COALESCE(expires_at,''), auto_renew, status, created_at
				FROM ssl_certs WHERE account_id = ? ORDER BY created_at DESC`, c.AccountID)
		} else {
			rows, err = db.Query(`SELECT id, account_id, domain_id, domain,
				COALESCE(issuer,''), COALESCE(expires_at,''), auto_renew, status, created_at
				FROM ssl_certs ORDER BY created_at DESC`)
		}
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		type Cert struct {
			ID        int    `json:"id"`
			AccountID int    `json:"account_id"`
			DomainID  int    `json:"domain_id"`
			Domain    string `json:"domain"`
			Issuer    string `json:"issuer"`
			ExpiresAt string `json:"expires_at"`
			AutoRenew int    `json:"auto_renew"`
			Status    string `json:"status"`
			CreatedAt string `json:"created_at"`
		}

		certs := make([]Cert, 0)
		for rows.Next() {
			var c Cert
			rows.Scan(&c.ID, &c.AccountID, &c.DomainID, &c.Domain,
				&c.Issuer, &c.ExpiresAt, &c.AutoRenew, &c.Status, &c.CreatedAt)
			certs = append(certs, c)
		}
		jsonResp(w, 200, certs)
	})

	r.Post("/issue", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		var req struct {
			DomainID int    `json:"domain_id"`
			Domain   string `json:"domain"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.DomainID == 0 || req.Domain == "" {
			jsonError(w, 400, "domain_id and domain required")
			return
		}

		var accountID int
		var docRoot string
		err := db.QueryRow("SELECT account_id, doc_root FROM domains WHERE id = ?", req.DomainID).Scan(&accountID, &docRoot)
		if err != nil {
			jsonError(w, 404, "domain not found")
			return
		}

		// Child scope can only issue for their own domains
		if c.Scope == "child" && accountID != c.AccountID {
			jsonError(w, 403, "access denied")
			return
		}

		result, err := db.Exec(`INSERT INTO ssl_certs (account_id, domain_id, domain, status) VALUES (?, ?, ?, 'issuing')`,
			accountID, req.DomainID, req.Domain)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		certID, _ := result.LastInsertId()
		auditLog(db, r, "ssl.issue", map[string]interface{}{"id": certID, "domain": req.Domain})

		// Try Let's Encrypt first, fall back to self-signed
		go func(cid int64, domain string) {
			err := issueLetsEncryptCert(db, cid, domain, "www."+domain)
			if err != nil {
				log.Printf("[SSL] Let's Encrypt failed for %s: %v; falling back to self-signed", domain, err)
				db.Exec("UPDATE ssl_certs SET status = 'issuing' WHERE id = ?", cid)
				issueSelfSignedCert(db, cid, accountID, domain, docRoot)
			}
		}(certID, req.Domain)

		jsonResp(w, 200, map[string]interface{}{
			"id":     certID,
			"status": "issuing",
			"domain": req.Domain,
		})
	})

	r.Post("/custom", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		var req struct {
			DomainID    int    `json:"domain_id"`
			Certificate string `json:"certificate"`
			PrivateKey  string `json:"private_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid request body")
			return
		}
		if req.DomainID == 0 || req.Certificate == "" || req.PrivateKey == "" {
			jsonError(w, 400, "domain_id, certificate, and private_key are required")
			return
		}

		var accountID int
		var domain string
		err := db.QueryRow("SELECT account_id, domain FROM domains WHERE id = ?", req.DomainID).Scan(&accountID, &domain)
		if err != nil {
			jsonError(w, 404, "domain not found")
			return
		}
		if c.Scope == "child" && accountID != c.AccountID {
			jsonError(w, 403, "access denied")
			return
		}

		// Validate certificate and private key
		issuer, expiresAt, err := validateCertKeyPair(req.Certificate, req.PrivateKey, domain)
		if err != nil {
			jsonError(w, 400, err.Error())
			return
		}

		// Delete any existing cert for this domain
		db.Exec("DELETE FROM ssl_certs WHERE domain_id = ?", req.DomainID)

		result, err := db.Exec(`INSERT INTO ssl_certs (account_id, domain_id, domain, certificate, private_key, issuer, expires_at, auto_renew, status) VALUES (?, ?, ?, ?, ?, ?, ?, 0, 'issued')`,
			accountID, req.DomainID, domain, req.Certificate, req.PrivateKey, issuer, expiresAt)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		certID, _ := result.LastInsertId()
		auditLog(db, r, "ssl.custom", map[string]interface{}{"id": certID, "domain": domain})

		// Write cert files for nginx
		sslDir := getHomesBase() + "ssl/"
		os.MkdirAll(sslDir, 0700)
		os.WriteFile(sslDir+domain+".crt", []byte(req.Certificate), 0644)
		os.WriteFile(sslDir+domain+".key", []byte(req.PrivateKey), 0600)

		addNginxSSL(domain)

		jsonResp(w, 200, map[string]interface{}{
			"id":         certID,
			"domain":     domain,
			"issuer":     issuer,
			"expires_at": expiresAt,
			"status":     "issued",
		})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}

		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			jsonError(w, 400, "invalid id")
			return
		}

		if c.Scope == "child" {
			var ownerID int
			err := db.QueryRow("SELECT account_id FROM ssl_certs WHERE id = ?", id).Scan(&ownerID)
			if err != nil || ownerID != c.AccountID {
				jsonError(w, 404, "cert not found")
				return
			}
		}

		var domain string
		db.QueryRow("SELECT domain FROM ssl_certs WHERE id = ?", id).Scan(&domain)
		db.Exec("DELETE FROM ssl_certs WHERE id = ?", id)
		auditLog(db, r, "ssl.delete", map[string]interface{}{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}

func issueSelfSignedCert(db *sql.DB, certID int64, accountID int, domain, docRoot string) {
	log.Printf("[SSL] Issuing self-signed cert for %s (certID=%d)", domain, certID)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		db.Exec("UPDATE ssl_certs SET status = 'failed' WHERE id = ?", certID)
		log.Printf("[SSL] Key generation failed: %v", err)
		return
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(now.Unix()),
		Subject: pkix.Name{
			CommonName:   domain,
			Organization: []string{"OpenWebPanel Self-Signed"},
		},
		NotBefore:             now,
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{domain, "www." + domain},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		db.Exec("UPDATE ssl_certs SET status = 'failed' WHERE id = ?", certID)
		log.Printf("[SSL] Certificate creation failed: %v", err)
		return
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	expiresAt := now.Add(365 * 24 * time.Hour).Format("2006-01-02")
	_, err = db.Exec(`UPDATE ssl_certs SET certificate=?, private_key=?, issuer=?, expires_at=?, status='issued' WHERE id=?`,
		string(certPEM), string(keyPEM), "OpenWebPanel Self-Signed CA", expiresAt, certID)
	if err != nil {
		log.Printf("[SSL] DB update failed: %v", err)
		return
	}

	// Write cert files for nginx
	sslDir := getHomesBase() + "ssl/"
	os.MkdirAll(sslDir, 0700)
	os.WriteFile(sslDir+domain+".crt", certPEM, 0644)
	os.WriteFile(sslDir+domain+".key", keyPEM, 0600)

	// Add SSL config to nginx vhost
	addNginxSSL(domain)

	log.Printf("[SSL] Self-signed certificate issued for %s (certID=%d)", domain, certID)
}

func validateCertKeyPair(certPEM, keyPEM, domain string) (issuer string, expiresAt string, err error) {
	// Parse the first certificate in the PEM bundle
	var parsedCert *x509.Certificate
	rest := []byte(certPEM)
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			parsedCert, err = x509.ParseCertificate(block.Bytes)
			if err == nil {
				break
			}
		}
	}
	if parsedCert == nil {
		return "", "", fmt.Errorf("no valid certificate found in PEM data")
	}

	// Verify the certificate covers the domain
	if !certCoversDomain(parsedCert, domain) {
		return "", "", fmt.Errorf("certificate does not cover domain '%s' (CN or SANs: %v)", domain, append([]string{parsedCert.Subject.CommonName}, parsedCert.DNSNames...))
	}

	// Parse the private key
	keyBlock, _ := pem.Decode([]byte(keyPEM))
	if keyBlock == nil {
		return "", "", fmt.Errorf("no valid private key PEM data")
	}

	var parsedKey interface{}
	switch keyBlock.Type {
	case "RSA PRIVATE KEY":
		parsedKey, err = x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	case "EC PRIVATE KEY":
		parsedKey, err = x509.ParseECPrivateKey(keyBlock.Bytes)
	case "PRIVATE KEY":
		parsedKey, err = x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	default:
		return "", "", fmt.Errorf("unsupported private key type: %s", keyBlock.Type)
	}
	if err != nil {
		return "", "", fmt.Errorf("invalid private key: %w", err)
	}

	// Verify the public key matches
	certPubBytes, err := x509.MarshalPKIXPublicKey(parsedCert.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal certificate public key: %w", err)
	}
	var keyPub crypto.PublicKey
	switch k := parsedKey.(type) {
	case *rsa.PrivateKey:
		keyPub = k.Public()
	case *ecdsa.PrivateKey:
		keyPub = k.Public()
	default:
		return "", "", fmt.Errorf("unsupported private key type")
	}
	keyPubBytes, err := x509.MarshalPKIXPublicKey(keyPub)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal key public key: %w", err)
	}
	if string(certPubBytes) != string(keyPubBytes) {
		return "", "", fmt.Errorf("certificate public key does not match private key")
	}

	// Extract issuer
	issuer = parsedCert.Issuer.CommonName
	if issuer == "" && len(parsedCert.Issuer.Organization) > 0 {
		issuer = parsedCert.Issuer.Organization[0]
	}
	if issuer == "" {
		issuer = "Unknown"
	}

	// Extract expiry
	expiresAt = parsedCert.NotAfter.Format("2006-01-02")

	return issuer, expiresAt, nil
}

func certCoversDomain(cert *x509.Certificate, domain string) bool {
	domain = strings.TrimSuffix(domain, ".")
	domain = strings.ToLower(domain)

	// Check CommonName
	if strings.ToLower(cert.Subject.CommonName) == domain {
		return true
	}

	// Check SANs (including wildcards)
	for _, san := range cert.DNSNames {
		san = strings.TrimSuffix(san, ".")
		san = strings.ToLower(san)
		if san == domain {
			return true
		}
		// Wildcard match: *.example.com matches sub.example.com
		if strings.HasPrefix(san, "*.") {
			wildcardDomain := strings.TrimPrefix(san, "*.")
			if strings.HasSuffix(domain, "."+wildcardDomain) && strings.Count(domain, ".") == strings.Count(wildcardDomain, ".")+1 {
				return true
			}
		}
	}
	return false
}
