package main

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
)

func certRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}

		var rows *sql.Rows
		var err error
		if c.Scope == "child" {
			rows, err = db.Query(`SELECT id, account_id, domain_id, domain,
				COALESCE(issuer,''), COALESCE(expires_at,''), auto_renew, status, COALESCE(last_error,''), created_at
				FROM ssl_certs WHERE account_id = ? ORDER BY created_at DESC`, c.AccountID)
		} else {
			rows, err = db.Query(`SELECT id, account_id, domain_id, domain,
				COALESCE(issuer,''), COALESCE(expires_at,''), auto_renew, status, COALESCE(last_error,''), created_at
				FROM ssl_certs ORDER BY created_at DESC`)
		}
		if err != nil {
			getLogger(r).Errorf("ssl cert query: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		type 		Cert struct {
			ID        int    `json:"id"`
			AccountID int    `json:"account_id"`
			DomainID  int    `json:"domain_id"`
			Domain    string `json:"domain"`
			Issuer    string `json:"issuer"`
			ExpiresAt string `json:"expires_at"`
			AutoRenew int    `json:"auto_renew"`
			Status    string `json:"status"`
			LastError string `json:"last_error"`
			CreatedAt string `json:"created_at"`
		}

		certs := make([]Cert, 0)
		for rows.Next() {
			var ct Cert
			if err := rows.Scan(&ct.ID, &ct.AccountID, &ct.DomainID, &ct.Domain,
				&ct.Issuer, &ct.ExpiresAt, &ct.AutoRenew, &ct.Status, &ct.LastError, &ct.CreatedAt); err != nil {
				getLogger(r).Warnf("scan ssl row: %v", err)
				continue
			}
			certs = append(certs, ct)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("ssl rows iteration error: %v", err)
		}
		jsonResp(w, 200, certs)
	})

	r.Post("/issue", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}

		if isRAMExceeded(db, c.AccountID) {
			writeAppError(w, r, apperrors.QuotaExceeded("RAM", int64(getRAMLimit(db, c.AccountID))))
			return
		}

		var req struct {
			DomainID int    `json:"domain_id"`
			Domain   string `json:"domain"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		if req.DomainID == 0 || req.Domain == "" {
			writeAppError(w, r, apperrors.Validation("domain_id and domain are required"))
			return
		}

		var accountID int
		var docRoot, dbDomain string
		err := db.QueryRow("SELECT account_id, doc_root, domain FROM domains WHERE id = ?", req.DomainID).Scan(&accountID, &docRoot, &dbDomain)
		if err != nil {
			writeAppError(w, r, apperrors.NotFound("domain", req.DomainID))
			return
		}

		if c.Scope == "child" && accountID != c.AccountID {
			writeAppError(w, r, apperrors.Forbidden("access denied"))
			return
		}
		// The certificate domain is taken from the owned domain record only.
		// The client-supplied domain string is ignored to prevent issuing a
		// certificate for another tenant's domain.
		domain := dbDomain

		result, err := db.Exec(`INSERT INTO ssl_certs (account_id, domain_id, domain, status) VALUES (?, ?, ?, 'issuing')`,
			accountID, req.DomainID, domain)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		certID, _ := result.LastInsertId()
		auditLog(db, r, "ssl.issue", logging.Fields{"id": certID, "domain": domain})

		go func(cid int64, domain string, aid int, dRoot string) {
			l := logging.NewDefault("ssl")
			wwwDomain := "www." + domain
			if !domainOwnedByAccount(db, aid, domain) || !domainOwnedByAccount(db, aid, wwwDomain) {
				wwwDomain = ""
			}
			names := []string{domain}
			if wwwDomain != "" {
				names = append(names, wwwDomain)
			}
			err := issueLetsEncryptCert(db, cid, names...)
			if err != nil {
				// Never masquerade a fallback as a real certificate: record
				// the LE failure and mark the row self-signed so the UI can
				// tell the operator exactly why HTTPS is not trusted.
				l.Errorf("Let's Encrypt failed for %s: %v; falling back to self-signed", domain, err)
				db.Exec("UPDATE ssl_certs SET status = 'self-signed', last_error = ? WHERE id = ?", err.Error(), cid)
				issueSelfSignedCert(db, cid, aid, domain, dRoot, wwwDomain)
			}
		}(certID, domain, accountID, docRoot)

		jsonResp(w, 200, map[string]interface{}{
			"id":     certID,
			"status": "issuing",
			"domain": req.Domain,
		})
	})

	r.Post("/custom", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}

		var req struct {
			DomainID    int    `json:"domain_id"`
			Certificate string `json:"certificate"`
			PrivateKey  string `json:"private_key"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		if req.DomainID == 0 {
			writeAppError(w, r, apperrors.Validation("domain_id is required"))
			return
		}
		if req.Certificate == "" {
			writeAppError(w, r, apperrors.Validation("certificate is required"))
			return
		}
		if req.PrivateKey == "" {
			writeAppError(w, r, apperrors.Validation("private_key is required"))
			return
		}

		var accountID int
		var domain string
		err := db.QueryRow("SELECT account_id, domain FROM domains WHERE id = ?", req.DomainID).Scan(&accountID, &domain)
		if err != nil {
			writeAppError(w, r, apperrors.NotFound("domain", req.DomainID))
			return
		}
		if c.Scope == "child" && accountID != c.AccountID {
			writeAppError(w, r, apperrors.Forbidden("access denied"))
			return
		}

		issuer, expiresAt, err := validateCertKeyPair(req.Certificate, req.PrivateKey, domain)
		if err != nil {
			writeAppError(w, r, apperrors.BadRequest(err.Error()))
			return
		}

		if _, err := db.Exec("DELETE FROM ssl_certs WHERE domain_id = ?", req.DomainID); err != nil {
			getLogger(r).Warnf("Failed to delete existing cert for domain_id %d: %v", req.DomainID, err)
		}

		result, err := db.Exec(`INSERT INTO ssl_certs (account_id, domain_id, domain, certificate, private_key, issuer, expires_at, auto_renew, status) VALUES (?, ?, ?, ?, ?, ?, ?, 0, 'issued')`,
			accountID, req.DomainID, domain, req.Certificate, req.PrivateKey, issuer, expiresAt)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		certID, _ := result.LastInsertId()
		auditLog(db, r, "ssl.custom", logging.Fields{"id": certID, "domain": domain})

		sslDir := getHomesBase() + "ssl/"
		os.MkdirAll(sslDir, 0700)
		safeDomain := sanitizeFilename(domain)
		os.WriteFile(sslDir+safeDomain+".crt", []byte(req.Certificate), 0644)
		os.WriteFile(sslDir+safeDomain+".key", []byte(req.PrivateKey), 0600)

		if err := addNginxSSL(db, domain); err != nil {
			getLogger(r).Warnf("custom cert stored but HTTPS block failed for %s: %v", domain, err)
			db.Exec("UPDATE ssl_certs SET last_error = ? WHERE id = ?", "nginx: "+err.Error(), certID)
		} else {
			db.Exec("UPDATE domains SET ssl_enabled = 1 WHERE domain = ?", domain)
			enableForceHTTPSIfDefault(db, domain)
		}

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
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}

		id, err := paramInt(r, "id")
		if err != nil {
			writeAppError(w, r, err)
			return
		}

		if c.Scope == "child" {
			var ownerID int
			if err := db.QueryRow("SELECT account_id FROM ssl_certs WHERE id = ?", id).Scan(&ownerID); err != nil || ownerID != c.AccountID {
				writeAppError(w, r, apperrors.NotFound("SSL certificate", id))
				return
			}
		}

		var domain string
		if err := db.QueryRow("SELECT domain FROM ssl_certs WHERE id = ?", id).Scan(&domain); err != nil {
			writeAppError(w, r, apperrors.NotFound("SSL certificate", id))
			return
		}
		result, err := db.Exec("DELETE FROM ssl_certs WHERE id = ?", id)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			writeAppError(w, r, apperrors.NotFound("SSL certificate", id))
			return
		}
		// Tear down the HTTPS block + cert files so nginx stops serving 443
		// for a domain the operator just removed.
		if err := removeNginxSSL(db, domain); err != nil {
			getLogger(r).Warnf("cert row deleted but HTTPS teardown failed for %s: %v", domain, err)
		}
		auditLog(db, r, "ssl.delete", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}

func issueSelfSignedCert(db *sql.DB, certID int64, accountID int, domain, docRoot, wwwDomain string) {
	l := logging.NewDefault("ssl")
	l.Infof("Issuing self-signed cert for %s (certID=%d)", domain, certID)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		db.Exec("UPDATE ssl_certs SET status = 'failed' WHERE id = ?", certID)
		l.Errorf("Key generation failed: %v", err)
		return
	}

	dnsNames := []string{domain}
	if wwwDomain != "" && !containsString(dnsNames, wwwDomain) {
		dnsNames = append(dnsNames, wwwDomain)
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
		DNSNames:              dnsNames,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		db.Exec("UPDATE ssl_certs SET status = 'failed' WHERE id = ?", certID)
		l.Errorf("Certificate creation failed: %v", err)
		return
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	expiresAt := now.Add(365 * 24 * time.Hour).Format("2006-01-02")
	_, err = db.Exec(`UPDATE ssl_certs SET certificate=?, private_key=?, issuer=?, expires_at=?, status='self-signed' WHERE id=?`,
		string(certPEM), string(keyPEM), "OpenWebPanel Self-Signed CA", expiresAt, certID)
	if err != nil {
		l.Errorf("DB update failed: %v", err)
		return
	}

	sslDir := getHomesBase() + "ssl/"
	os.MkdirAll(sslDir, 0700)
	safeDomain := sanitizeFilename(domain)
	os.WriteFile(sslDir+safeDomain+".crt", certPEM, 0644)
	os.WriteFile(sslDir+safeDomain+".key", keyPEM, 0600)

	if err := addNginxSSL(db, domain); err != nil {
		l.Errorf("HTTPS block failed for %s: %v", domain, err)
		db.Exec("UPDATE ssl_certs SET last_error = COALESCE(last_error,'') || ' [nginx: ' || ? || ']' WHERE id = ?", err.Error(), certID)
	} else {
		db.Exec("UPDATE domains SET ssl_enabled = 1 WHERE domain = ?", domain)
		enableForceHTTPSIfDefault(db, domain)
	}

	l.Infof("Self-signed certificate issued for %s (certID=%d)", domain, certID)
}

func validateCertKeyPair(certPEM, keyPEM, domain string) (issuer string, expiresAt string, err error) {
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

	if !certCoversDomain(parsedCert, domain) {
		return "", "", fmt.Errorf("certificate does not cover domain '%s' (CN or SANs: %v)", domain, append([]string{parsedCert.Subject.CommonName}, parsedCert.DNSNames...))
	}

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

	issuer = parsedCert.Issuer.CommonName
	if issuer == "" && len(parsedCert.Issuer.Organization) > 0 {
		issuer = parsedCert.Issuer.Organization[0]
	}
	if issuer == "" {
		issuer = "Unknown"
	}

	expiresAt = parsedCert.NotAfter.Format("2006-01-02")
	return issuer, expiresAt, nil
}

func certCoversDomain(cert *x509.Certificate, domain string) bool {
	domain = strings.TrimSuffix(domain, ".")
	domain = strings.ToLower(domain)

	if strings.ToLower(cert.Subject.CommonName) == domain {
		return true
	}

	for _, san := range cert.DNSNames {
		san = strings.TrimSuffix(san, ".")
		san = strings.ToLower(san)
		if san == domain {
			return true
		}
		if strings.HasPrefix(san, "*.") {
			wildcardDomain := strings.TrimPrefix(san, "*.")
			if strings.HasSuffix(domain, "."+wildcardDomain) && strings.Count(domain, ".") == strings.Count(wildcardDomain, ".")+1 {
				return true
			}
		}
	}
	return false
}

func sanitizeFilename(name string) string {
	s := strings.ReplaceAll(name, "..", "")
	s = strings.ReplaceAll(s, "/", "")
	s = strings.ReplaceAll(s, "\\", "")
	return s
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// domainOwnedByAccount reports whether the given domain is registered to the
// account. This guards issuance of certificates / SANs for names the account
// does not actually control.
func domainOwnedByAccount(db *sql.DB, accountID int, domain string) bool {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM domains WHERE account_id = ? AND LOWER(domain) = LOWER(?)`, accountID, domain).Scan(&count)
	return count > 0
}
