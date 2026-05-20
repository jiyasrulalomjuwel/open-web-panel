package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

// ── Child Email Routes ──

func childEmailRoutes(r chi.Router, db *sql.DB) {
	// Count email accounts for this child account
	r.Get("/count", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		var count int
		db.QueryRow("SELECT COUNT(*) FROM email_accounts WHERE account_id = ?", c.AccountID).Scan(&count)
		jsonResp(w, 200, map[string]int{"count": count})
	})

	// List email accounts for this child account
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		rows, err := db.Query(`SELECT e.id, e.account_id, e.domain_id, e.email, e.forward_to,
			e.quota_mb, e.send_limit, e.send_used, e.send_reset_date, e.status, e.created_at,
			d.domain as domain_name
			FROM email_accounts e JOIN domains d ON e.domain_id = d.id
			WHERE e.account_id = ? ORDER BY e.created_at DESC`, c.AccountID)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()
		type EmailAcct struct {
			ID            int    `json:"id"`
			AccountID     int    `json:"account_id"`
			DomainID      int    `json:"domain_id"`
			Email         string `json:"email"`
			ForwardTo     string `json:"forward_to"`
			QuotaMB       int    `json:"quota_mb"`
			SendLimit     int    `json:"send_limit"`
			SendUsed      int    `json:"send_used"`
			SendResetDate string `json:"send_reset_date"`
			Status        string `json:"status"`
			CreatedAt     string `json:"created_at"`
			DomainName    string `json:"domain_name"`
		}
		accts := make([]EmailAcct, 0)
		for rows.Next() {
			var a EmailAcct
			rows.Scan(&a.ID, &a.AccountID, &a.DomainID, &a.Email, &a.ForwardTo,
				&a.QuotaMB, &a.SendLimit, &a.SendUsed, &a.SendResetDate, &a.Status, &a.CreatedAt, &a.DomainName)
			accts = append(accts, a)
		}
		jsonResp(w, 200, accts)
	})

	// Create email account
	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		var req struct {
			DomainID   int    `json:"domain_id"`
			LocalPart  string `json:"local_part"`
			Password   string `json:"password"`
			ForwardTo  string `json:"forward_to"`
			QuotaMB    int    `json:"quota_mb"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.DomainID == 0 || req.LocalPart == "" || req.Password == "" {
			jsonError(w, 400, "domain_id, local_part, and password required")
			return
		}

		// Check domain ownership
		var domainName string
		err := db.QueryRow("SELECT domain FROM domains WHERE id = ? AND account_id = ?",
			req.DomainID, c.AccountID).Scan(&domainName)
		if err != nil {
			jsonError(w, 404, "domain not found")
			return
		}

		// Check email limit from package
		var pkgMax, currentCount int
		db.QueryRow(`SELECT p.max_email FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`,
			c.AccountID).Scan(&pkgMax)
		db.QueryRow("SELECT COUNT(*) FROM email_accounts WHERE account_id = ?", c.AccountID).Scan(&currentCount)
		if currentCount >= pkgMax {
			jsonError(w, 400, fmt.Sprintf("Email limit reached (%d/%d). Upgrade your package.", currentCount, pkgMax))
			return
		}

		email := req.LocalPart + "@" + domainName
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			jsonError(w, 500, "failed to hash password")
			return
		}

		if req.QuotaMB == 0 {
			req.QuotaMB = 100
		}

		// Reset send_used if date changed
		sendLimit := 25
		sendUsed := 0
		today := time.Now().Format("2006-01-02")

		result, err := db.Exec(`INSERT INTO email_accounts
			(account_id, domain_id, email, password_hash, forward_to, quota_mb, send_limit, send_used, send_reset_date)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.AccountID, req.DomainID, email, string(hash), req.ForwardTo, req.QuotaMB,
			sendLimit, sendUsed, today)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		id, _ := result.LastInsertId()
		jsonResp(w, 201, map[string]interface{}{"id": id, "email": email, "status": "created"})
	})

	// Update email account (forwarding, password, etc)
	r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		id := chi.URLParam(r, "id")
		var req struct {
			Password  string `json:"password"`
			ForwardTo string `json:"forward_to"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if req.Password != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
			if err == nil {
				db.Exec("UPDATE email_accounts SET password_hash = ? WHERE id = ? AND account_id = ?",
					string(hash), id, c.AccountID)
			}
		}
		if req.ForwardTo != "" {
			db.Exec("UPDATE email_accounts SET forward_to = ? WHERE id = ? AND account_id = ?",
				req.ForwardTo, id, c.AccountID)
		}
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	// Delete email account
	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		id := chi.URLParam(r, "id")
		db.Exec("DELETE FROM email_messages WHERE email_account_id IN (SELECT id FROM email_accounts WHERE id = ? AND account_id = ?)", id, c.AccountID)
		db.Exec("DELETE FROM email_accounts WHERE id = ? AND account_id = ?", id, c.AccountID)
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	// Get inbox messages for an email account
	r.Get("/{id}/inbox", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		id := chi.URLParam(r, "id")
		folder := r.URL.Query().Get("folder")
		if folder == "" {
			folder = "INBOX"
		}

		// Verify ownership
		var ownerID int
		db.QueryRow("SELECT account_id FROM email_accounts WHERE id = ?", id).Scan(&ownerID)
		if ownerID != c.AccountID {
			jsonError(w, 403, "access denied")
			return
		}

		rows, err := db.Query(`SELECT id, folder, from_addr, to_addr, subject,
			flags, received_at FROM email_messages
			WHERE email_account_id = ? AND folder = ?
			ORDER BY received_at DESC LIMIT 50`, id, folder)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()
		msgs := make([]map[string]interface{}, 0)
		for rows.Next() {
			var mid int
			var fld, from, to, subj, flags, received string
			rows.Scan(&mid, &fld, &from, &to, &subj, &flags, &received)
			isSeen := strings.Contains(flags, "\\Seen")
			msgs = append(msgs, map[string]interface{}{
				"id": mid, "folder": fld, "from": from, "to": to,
				"subject": subj, "seen": isSeen, "flags": flags,
				"received_at": received,
			})
		}
		jsonResp(w, 200, msgs)
	})

	// Read a single message
	r.Get("/{id}/messages/{mid}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		id := chi.URLParam(r, "id")
		mid := chi.URLParam(r, "mid")

		var ownerID int
		db.QueryRow("SELECT account_id FROM email_accounts WHERE id = ?", id).Scan(&ownerID)
		if ownerID != c.AccountID {
			jsonError(w, 403, "access denied")
			return
		}

		var msgID int
		var fld, from, to, subj, bodyText, bodyHTML, flags, received string
		err := db.QueryRow(`SELECT id, folder, from_addr, to_addr, subject,
			body_text, body_html, flags, received_at FROM email_messages
			WHERE id = ? AND email_account_id = ?`, mid, id).Scan(
			&msgID, &fld, &from, &to, &subj, &bodyText, &bodyHTML, &flags, &received)
		if err != nil {
			jsonError(w, 404, "message not found")
			return
		}

		// Mark as seen
		if !strings.Contains(flags, "\\Seen") {
			newFlags := flags + " \\Seen"
			db.Exec("UPDATE email_messages SET flags = ? WHERE id = ?", strings.TrimSpace(newFlags), mid)
		}

		jsonResp(w, 200, map[string]interface{}{
			"id": msgID, "folder": fld, "from": from, "to": to,
			"subject": subj, "body_text": bodyText, "body_html": bodyHTML,
			"flags": flags, "received_at": received,
		})
	})

	// Update message flags (seen, flagged, etc)
	r.Patch("/{id}/messages/{mid}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		id := chi.URLParam(r, "id")
		mid := chi.URLParam(r, "mid")
		var req struct{ Flags string `json:"flags"` }
		json.NewDecoder(r.Body).Decode(&req)

		var ownerID int
		db.QueryRow("SELECT account_id FROM email_accounts WHERE id = ?", id).Scan(&ownerID)
		if ownerID != c.AccountID {
			jsonError(w, 403, "access denied")
			return
		}
		db.Exec("UPDATE email_messages SET flags = ? WHERE id = ? AND email_account_id = ?",
			req.Flags, mid, id)
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	// Delete a message
	r.Delete("/{id}/messages/{mid}", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		id := chi.URLParam(r, "id")
		mid := chi.URLParam(r, "mid")
		var ownerID int
		db.QueryRow("SELECT account_id FROM email_accounts WHERE id = ?", id).Scan(&ownerID)
		if ownerID != c.AccountID {
			jsonError(w, 403, "access denied")
			return
		}
		db.Exec("DELETE FROM email_messages WHERE id = ? AND email_account_id = ?", mid, id)
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	// Send email (checks daily limit)
	r.Post("/{id}/send", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		id := chi.URLParam(r, "id")
		var req struct {
			To      string `json:"to"`
			Subject string `json:"subject"`
			Body    string `json:"body"`
			BodyHTML string `json:"body_html"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.To == "" {
			jsonError(w, 400, "recipient (to) required")
			return
		}

		var ownerID, sendLimit, sendUsed int
		var email, sendResetDate string
		err := db.QueryRow("SELECT account_id, email, send_limit, send_used, send_reset_date FROM email_accounts WHERE id = ?", id).
			Scan(&ownerID, &email, &sendLimit, &sendUsed, &sendResetDate)
		if err != nil || ownerID != c.AccountID {
			jsonError(w, 403, "access denied")
			return
		}

		// Reset counter if new day
		today := time.Now().Format("2006-01-02")
		if sendResetDate != today {
			sendUsed = 0
			db.Exec("UPDATE email_accounts SET send_used = 0, send_reset_date = ? WHERE id = ?", today, id)
		}

		// Check daily limit
		if sendUsed >= sendLimit {
			jsonError(w, 429, fmt.Sprintf("Daily sending limit reached (%d/%d). Contact admin to increase limit.", sendUsed, sendLimit))
			return
		}

		// Store sent message
		msgID := generateMessageID(email)
		_, err = db.Exec(`INSERT INTO email_messages
			(email_account_id, folder, from_addr, to_addr, subject, body_text, body_html, flags, message_id)
			VALUES (?, 'Sent', ?, ?, ?, ?, ?, '\\Seen', ?)`,
			id, email, req.To, req.Subject, req.Body, req.BodyHTML, msgID)
		if err != nil {
			jsonError(w, 500, "failed to store message")
			return
		}

		// Increment send counter
		db.Exec("UPDATE email_accounts SET send_used = send_used + 1 WHERE id = ?", id)

		// Actually deliver via SMTP (relay or direct)
		go func() {
			raw := buildRawEmail(email, req.To, req.Subject, req.Body, req.BodyHTML)
			if err := deliverRemote(db, email, req.To, raw); err != nil {
				log.Printf("[EMAIL] Delivery failed %s -> %s: %v", email, req.To, err)
			} else {
				log.Printf("[EMAIL] Delivered %s -> %s", email, req.To)
			}
		}()

		log.Printf("[EMAIL] Sent from %s to %s (subject: %s)", email, req.To, req.Subject)
		jsonResp(w, 200, map[string]interface{}{
			"status": "sent",
			"to":     req.To,
			"subject": req.Subject,
		})
	})

	// DNS configuration info
	r.Get("/dns", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		rows, err := db.Query("SELECT domain FROM domains WHERE account_id = ?", c.AccountID)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()
		type DNSInfo struct {
			Domain     string `json:"domain"`
			MxRecord   string `json:"mx_record"`
			SpfRecord  string `json:"spf_record"`
			DkimRecord string `json:"dkim_record"`
			ServerIP   string `json:"server_ip"`
			MailHost   string `json:"mail_host"`
		}
		configs := make([]DNSInfo, 0)
		for rows.Next() {
			var d string
			rows.Scan(&d)
			serverIP := getOutboundIP()

			configs = append(configs, DNSInfo{
				Domain:     d,
				MxRecord:   fmt.Sprintf("mail.%s", d),
				SpfRecord:  fmt.Sprintf("v=spf1 mx ~all"),
				DkimRecord: "Set up DKIM signing on mail server",
				ServerIP:   serverIP,
				MailHost:   fmt.Sprintf("mail.%s", d),
			})
		}
		jsonResp(w, 200, configs)
	})
}

// ── Admin Email Routes ──

func adminEmailRoutes(r chi.Router, db *sql.DB) {
	// List all email accounts across all users
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT e.id, e.account_id, e.domain_id, e.email, e.forward_to,
			e.quota_mb, e.send_limit, e.send_used, e.status, e.created_at,
			a.username as account_username, d.domain as domain_name
			FROM email_accounts e
			JOIN accounts a ON e.account_id = a.id
			JOIN domains d ON e.domain_id = d.id
			ORDER BY e.created_at DESC`)
		if err != nil {
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()
		type EmailAcct struct {
			ID              int    `json:"id"`
			AccountID       int    `json:"account_id"`
			DomainID        int    `json:"domain_id"`
			Email           string `json:"email"`
			ForwardTo       string `json:"forward_to"`
			QuotaMB         int    `json:"quota_mb"`
			SendLimit       int    `json:"send_limit"`
			SendUsed        int    `json:"send_used"`
			Status          string `json:"status"`
			CreatedAt       string `json:"created_at"`
			AccountUsername string `json:"account_username"`
			DomainName      string `json:"domain_name"`
		}
		accts := make([]EmailAcct, 0)
		for rows.Next() {
			var a EmailAcct
			rows.Scan(&a.ID, &a.AccountID, &a.DomainID, &a.Email, &a.ForwardTo,
				&a.QuotaMB, &a.SendLimit, &a.SendUsed, &a.Status, &a.CreatedAt,
				&a.AccountUsername, &a.DomainName)
			accts = append(accts, a)
		}
		jsonResp(w, 200, accts)
	})

	// Update email limits
	r.Put("/{id}/limits", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req struct {
			SendLimit int `json:"send_limit"`
			QuotaMB   int `json:"quota_mb"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.SendLimit > 0 {
			db.Exec("UPDATE email_accounts SET send_limit = ? WHERE id = ?", req.SendLimit, id)
		}
		if req.QuotaMB > 0 {
			db.Exec("UPDATE email_accounts SET quota_mb = ? WHERE id = ?", req.QuotaMB, id)
		}
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	// Delete email account (admin)
	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		db.Exec("DELETE FROM email_messages WHERE email_account_id = ?", id)
		db.Exec("DELETE FROM email_accounts WHERE id = ?", id)
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}

func generateMessageID(domain string) string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("<%x.%d@%s>", b, time.Now().UnixNano(), domain)
}

// getOutboundIP returns the server's preferred outbound IP address
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "0.0.0.0"
	}
	defer conn.Close()
	addr := conn.LocalAddr().(*net.UDPAddr)
	return addr.IP.String()
}
