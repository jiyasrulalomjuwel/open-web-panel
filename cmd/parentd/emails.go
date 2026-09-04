package main

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/validator"
)

func childEmailRoutes(r chi.Router, db *sql.DB) {
	r.Get("/count", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		var count int
		db.QueryRow("SELECT COUNT(*) FROM email_accounts WHERE account_id = ?", c.AccountID).Scan(&count)
		jsonResp(w, 200, map[string]int{"count": count})
	})

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		rows, err := db.Query(`SELECT e.id, e.account_id, e.domain_id, e.email, e.forward_to,
			e.quota_mb, e.send_limit, e.send_used, e.send_reset_date, e.status, e.created_at,
			d.domain as domain_name
			FROM email_accounts e JOIN domains d ON e.domain_id = d.id
			WHERE e.account_id = ? ORDER BY e.created_at DESC`, c.AccountID)
		if err != nil {
			getLogger(r).Errorf("email list query: %v", err)
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
			if err := rows.Scan(&a.ID, &a.AccountID, &a.DomainID, &a.Email, &a.ForwardTo,
				&a.QuotaMB, &a.SendLimit, &a.SendUsed, &a.SendResetDate, &a.Status, &a.CreatedAt, &a.DomainName); err != nil {
				getLogger(r).Warnf("scan email row: %v", err)
				continue
			}
			accts = append(accts, a)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("email list rows iteration error: %v", err)
		}
		jsonResp(w, 200, accts)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
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
			DomainID   int    `json:"domain_id"`
			LocalPart  string `json:"local_part"`
			Password   string `json:"password"`
			ForwardTo  string `json:"forward_to"`
			QuotaMB    int    `json:"quota_mb"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}

		validation := validator.New().
			Add("local_part", validator.Required(), validator.MaxLength(64)).
			Add("password", validator.Required(), validator.MinLength(6))
		data, _ := toMap(req)
		if err := validation.Validate(data); err != nil {
			writeAppError(w, r, err)
			return
		}
		if !validLocalPart(req.LocalPart) {
			writeAppError(w, r, apperrors.Validation("local_part contains invalid characters"))
			return
		}
		if req.ForwardTo != "" && !validForwardTo(req.ForwardTo) {
			writeAppError(w, r, apperrors.Validation("forward_to contains invalid address(es)"))
			return
		}
		if req.DomainID == 0 {
			writeAppError(w, r, apperrors.Validation("domain_id is required"))
			return
		}

		var domainName string
		err := db.QueryRow("SELECT domain FROM domains WHERE id = ? AND account_id = ?",
			req.DomainID, c.AccountID).Scan(&domainName)
		if err != nil {
			writeAppError(w, r, apperrors.NotFound("domain", req.DomainID))
			return
		}

		var pkgMax, currentCount int
		db.QueryRow(`SELECT p.max_email FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`,
			c.AccountID).Scan(&pkgMax)
		pkgMax = getAccountOverride(db, c.AccountID, "max_email", pkgMax)
		db.QueryRow("SELECT COUNT(*) FROM email_accounts WHERE account_id = ?", c.AccountID).Scan(&currentCount)
		if currentCount >= pkgMax {
			writeAppError(w, r, apperrors.QuotaExceeded("email accounts", int64(pkgMax)))
			return
		}

		email := req.LocalPart + "@" + domainName
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			writeAppError(w, r, apperrors.Internal("failed to hash password", err))
			return
		}

		if req.QuotaMB == 0 {
			req.QuotaMB = 100
		}

		sendLimit := 25
		sendUsed := 0
		today := time.Now().Format("2006-01-02")

		result, err := db.Exec(`INSERT INTO email_accounts
			(account_id, domain_id, email, password_hash, forward_to, quota_mb, send_limit, send_used, send_reset_date)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.AccountID, req.DomainID, email, string(hash), req.ForwardTo, req.QuotaMB,
			sendLimit, sendUsed, today)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint") {
				writeAppError(w, r, apperrors.Duplicate("email account", email))
				return
			}
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		id, _ := result.LastInsertId()
		auditLog(db, r, "email.create", logging.Fields{"id": id, "email": email})
		jsonResp(w, 201, map[string]interface{}{"id": id, "email": email, "status": "created"})
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
			Password  string `json:"password"`
			ForwardTo string `json:"forward_to"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}

		if err := withDBRetry(func() error {
			if req.Password != "" {
				if len(req.Password) < 6 {
					return apperrors.Validation("password must be at least 6 characters")
				}
				hash, pErr := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
				if pErr != nil {
					return apperrors.Internal("failed to hash password", pErr)
				}
				_, e := db.Exec("UPDATE email_accounts SET password_hash = ? WHERE id = ? AND account_id = ?",
					string(hash), id, c.AccountID)
				if e != nil {
					return e
				}
			}
			if req.ForwardTo != "" {
				if !validForwardTo(req.ForwardTo) {
					return apperrors.Validation("forward_to contains invalid address(es)")
				}
				_, e := db.Exec("UPDATE email_accounts SET forward_to = ? WHERE id = ? AND account_id = ?",
					req.ForwardTo, id, c.AccountID)
				if e != nil {
					return e
				}
			} else {
				_, e := db.Exec("UPDATE email_accounts SET forward_to = '' WHERE id = ? AND account_id = ?",
					id, c.AccountID)
				if e != nil {
					return e
				}
			}
			return nil
		}); err != nil {
			if appErr, ok := err.(*apperrors.AppError); ok {
				writeAppError(w, r, appErr)
				return
			}
			writeAppError(w, r, apperrors.Database(err))
			return
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
		db.Exec("DELETE FROM email_messages WHERE email_account_id IN (SELECT id FROM email_accounts WHERE id = ? AND account_id = ?)", id, c.AccountID)
		result, err := db.Exec("DELETE FROM email_accounts WHERE id = ? AND account_id = ?", id, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			writeAppError(w, r, apperrors.NotFound("email account", id))
			return
		}
		auditLog(db, r, "email.delete", logging.Fields{"id": id})
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	r.Get("/{id}/inbox", func(w http.ResponseWriter, r *http.Request) {
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
		folder := r.URL.Query().Get("folder")
		if folder == "" {
			folder = "INBOX"
		}

		var ownerID int
		db.QueryRow("SELECT account_id FROM email_accounts WHERE id = ?", id).Scan(&ownerID)
		if ownerID != c.AccountID {
			writeAppError(w, r, apperrors.Forbidden("access denied"))
			return
		}

		rows, err := db.Query(`SELECT id, folder, from_addr, to_addr, subject,
			flags, received_at FROM email_messages
			WHERE email_account_id = ? AND folder = ?
			ORDER BY received_at DESC LIMIT 50`, id, folder)
		if err != nil {
			getLogger(r).Errorf("inbox query: %v", err)
			jsonResp(w, 200, []interface{}{})
			return
		}
		defer rows.Close()

		msgs := make([]map[string]interface{}, 0)
		for rows.Next() {
			var mid int
			var fld, from, to, subj, flags, received string
			if err := rows.Scan(&mid, &fld, &from, &to, &subj, &flags, &received); err != nil {
				getLogger(r).Warnf("scan inbox row: %v", err)
				continue
			}
			isSeen := strings.Contains(flags, "\\Seen")
			msgs = append(msgs, map[string]interface{}{
				"id": mid, "folder": fld, "from": from, "to": to,
				"subject": subj, "seen": isSeen, "flags": flags,
				"received_at": received,
			})
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("inbox rows iteration error: %v", err)
		}
		jsonResp(w, 200, msgs)
	})

	r.Get("/{id}/messages/{mid}", func(w http.ResponseWriter, r *http.Request) {
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
		mid, err := paramInt(r, "mid")
		if err != nil {
			writeAppError(w, r, err)
			return
		}

		var ownerID int
		db.QueryRow("SELECT account_id FROM email_accounts WHERE id = ?", id).Scan(&ownerID)
		if ownerID != c.AccountID {
			writeAppError(w, r, apperrors.Forbidden("access denied"))
			return
		}

		var msgID int
		var fld, from, to, subj, bodyText, bodyHTML, flags, received string
		err = db.QueryRow(`SELECT id, folder, from_addr, to_addr, subject,
			body_text, body_html, flags, received_at FROM email_messages
			WHERE id = ? AND email_account_id = ?`, mid, id).Scan(
			&msgID, &fld, &from, &to, &subj, &bodyText, &bodyHTML, &flags, &received)
		if err != nil {
			writeAppError(w, r, apperrors.NotFound("message", mid))
			return
		}

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

	r.Patch("/{id}/messages/{mid}", func(w http.ResponseWriter, r *http.Request) {
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
		mid, err := paramInt(r, "mid")
		if err != nil {
			writeAppError(w, r, err)
			return
		}

		var req struct{ Flags string `json:"flags"` }
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}

		var ownerID int
		db.QueryRow("SELECT account_id FROM email_accounts WHERE id = ?", id).Scan(&ownerID)
		if ownerID != c.AccountID {
			writeAppError(w, r, apperrors.Forbidden("access denied"))
			return
		}
		_, execErr := db.Exec("UPDATE email_messages SET flags = ? WHERE id = ? AND email_account_id = ?",
			req.Flags, mid, id)
		if execErr != nil {
			writeAppError(w, r, apperrors.Database(execErr))
			return
		}
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	r.Delete("/{id}/messages/{mid}", func(w http.ResponseWriter, r *http.Request) {
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
		mid, err := paramInt(r, "mid")
		if err != nil {
			writeAppError(w, r, err)
			return
		}

		var ownerID int
		db.QueryRow("SELECT account_id FROM email_accounts WHERE id = ?", id).Scan(&ownerID)
		if ownerID != c.AccountID {
			writeAppError(w, r, apperrors.Forbidden("access denied"))
			return
		}
		_, execErr := db.Exec("DELETE FROM email_messages WHERE id = ? AND email_account_id = ?", mid, id)
		if execErr != nil {
			writeAppError(w, r, apperrors.Database(execErr))
			return
		}
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})

	r.Post("/{id}/send", func(w http.ResponseWriter, r *http.Request) {
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
			To       string `json:"to"`
			Subject  string `json:"subject"`
			Body     string `json:"body"`
			BodyHTML string `json:"body_html"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}
		if req.To == "" {
			writeAppError(w, r, apperrors.Validation("recipient (to) is required"))
			return
		}
		validatedTo, err := validateRecipientList(req.To)
		if err != nil {
			writeAppError(w, r, apperrors.Validation(err.Error()))
			return
		}

		var ownerID, sendLimit, sendUsed int
		var email, sendResetDate string
		err = db.QueryRow("SELECT account_id, email, send_limit, send_used, send_reset_date FROM email_accounts WHERE id = ?", id).
			Scan(&ownerID, &email, &sendLimit, &sendUsed, &sendResetDate)
		if err != nil || ownerID != c.AccountID {
			writeAppError(w, r, apperrors.Forbidden("access denied"))
			return
		}

		today := time.Now().Format("2006-01-02")
		if sendResetDate != today {
			sendUsed = 0
			db.Exec("UPDATE email_accounts SET send_used = 0, send_reset_date = ? WHERE id = ?", today, id)
		}

if sendUsed >= sendLimit {
			getLogger(r).Warnf("Daily sending limit reached (%d/%d) for account %d", sendUsed, sendLimit, c.AccountID)
		writeAppError(w, r, apperrors.RateLimit(86400))
			return
		}

		// Atomically reserve one send slot so concurrent requests cannot both
		// pass the check above and exceed the daily limit.
		res, err := db.Exec("UPDATE email_accounts SET send_used = send_used + 1 WHERE id = ? AND send_used < ? AND account_id = ?", id, sendLimit, c.AccountID)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			writeAppError(w, r, apperrors.RateLimit(86400))
			return
		}

		msgID := generateMessageID(email)
		_, err = db.Exec(`INSERT INTO email_messages
			(email_account_id, folder, from_addr, to_addr, subject, body_text, body_html, flags, message_id)
			VALUES (?, 'Sent', ?, ?, ?, ?, ?, '\\Seen', ?)`,
			id, email, validatedTo, req.Subject, req.Body, req.BodyHTML, msgID)
		if err != nil {
			db.Exec("UPDATE email_accounts SET send_used = send_used - 1 WHERE id = ?", id)
			writeAppError(w, r, apperrors.Internal("failed to store message", err))
			return
		}

		go func() {
			l := logging.NewDefault("email")
			raw := buildRawEmail(email, validatedTo, req.Subject, req.Body, req.BodyHTML)
			if err := deliverRemote(db, email, validatedTo, raw); err != nil {
				l.Errorf("Delivery failed %s -> %s: %v", email, validatedTo, err)
			} else {
				l.Infof("Delivered %s -> %s", email, validatedTo)
			}
		}()

		getLogger(r).Infof("Sent from %s to %s (subject: %s)", email, validatedTo, req.Subject)
		jsonResp(w, 200, map[string]interface{}{
			"status":  "sent",
			"to":      validatedTo,
			"subject": req.Subject,
		})
	})

	r.Get("/dns", func(w http.ResponseWriter, r *http.Request) {
		c := getClaims(r)
		if c == nil {
			writeAppError(w, r, apperrors.Unauthorized(""))
			return
		}
		rows, err := db.Query("SELECT domain FROM domains WHERE account_id = ?", c.AccountID)
		if err != nil {
			getLogger(r).Errorf("email dns query: %v", err)
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
			if err := rows.Scan(&d); err != nil {
				continue
			}
			serverIP := getOutboundIP()
			configs = append(configs, DNSInfo{
				Domain:     d,
				MxRecord:   fmt.Sprintf("mail.%s", d),
				SpfRecord:  "v=spf1 mx ~all",
				DkimRecord: "Set up DKIM signing on mail server",
				ServerIP:   serverIP,
				MailHost:   fmt.Sprintf("mail.%s", d),
			})
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("email dns rows iteration error: %v", err)
		}
		jsonResp(w, 200, configs)
	})
}

func adminEmailRoutes(r chi.Router, db *sql.DB) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT e.id, e.account_id, e.domain_id, e.email, e.forward_to,
			e.quota_mb, e.send_limit, e.send_used, e.status, e.created_at,
			a.username as account_username, d.domain as domain_name
			FROM email_accounts e
			JOIN accounts a ON e.account_id = a.id
			JOIN domains d ON e.domain_id = d.id
			ORDER BY e.created_at DESC`)
		if err != nil {
			getLogger(r).Errorf("admin email list query: %v", err)
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
			if err := rows.Scan(&a.ID, &a.AccountID, &a.DomainID, &a.Email, &a.ForwardTo,
				&a.QuotaMB, &a.SendLimit, &a.SendUsed, &a.Status, &a.CreatedAt,
				&a.AccountUsername, &a.DomainName); err != nil {
				getLogger(r).Warnf("scan admin email row: %v", err)
				continue
			}
			accts = append(accts, a)
		}
		if err := rows.Err(); err != nil {
			getLogger(r).Errorf("admin email list rows iteration error: %v", err)
		}
		jsonResp(w, 200, accts)
	})

	r.Put("/{id}/limits", func(w http.ResponseWriter, r *http.Request) {
		id, err := paramInt(r, "id")
		if err != nil {
			writeAppError(w, r, err)
			return
		}
		var req struct {
			SendLimit int `json:"send_limit"`
			QuotaMB   int `json:"quota_mb"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeAppError(w, r, err)
			return
		}

		if err := withDBRetry(func() error {
			if req.SendLimit > 0 {
				_, e := db.Exec("UPDATE email_accounts SET send_limit = ? WHERE id = ?", req.SendLimit, id)
				if e != nil {
					return e
				}
			}
			if req.QuotaMB > 0 {
				_, e := db.Exec("UPDATE email_accounts SET quota_mb = ? WHERE id = ?", req.QuotaMB, id)
				if e != nil {
					return e
				}
			}
			return nil
		}); err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		jsonResp(w, 200, map[string]string{"status": "updated"})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := paramInt(r, "id")
		if err != nil {
			writeAppError(w, r, err)
			return
		}
		db.Exec("DELETE FROM email_messages WHERE email_account_id = ?", id)
		result, err := db.Exec("DELETE FROM email_accounts WHERE id = ?", id)
		if err != nil {
			writeAppError(w, r, apperrors.Database(err))
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			writeAppError(w, r, apperrors.NotFound("email account", id))
			return
		}
		jsonResp(w, 200, map[string]string{"status": "deleted"})
	})
}

func generateMessageID(domain string) string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("<%x.%d@%s>", b, time.Now().UnixNano(), domain)
}

// validLocalPart enforces a safe character set for a mailbox local-part,
// preventing header injection (CR/LF/colon) and other malformed addresses.
func validLocalPart(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '-', c == '+', c == '%':
		default:
			return false
		}
	}
	return true
}

// validForwardTo validates a comma-separated list of forwarding addresses.
func validForwardTo(s string) bool {
	for _, part := range strings.Split(s, ",") {
		addr := strings.TrimSpace(part)
		if addr == "" {
			continue
		}
		if _, err := mail.ParseAddress(addr); err != nil {
			return false
		}
	}
	return true
}

// validateRecipientList parses and validates a comma/space separated recipient
// string, rejecting anything that would allow header injection or internal
// delivery. It returns the normalized address list.
func validateRecipientList(s string) (string, error) {
	addrs, err := mail.ParseAddressList(s)
	if err != nil {
		return "", fmt.Errorf("invalid recipient address list")
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("recipient list is empty")
	}
	var parts []string
	for _, a := range addrs {
		addr := strings.TrimSpace(a.Address)
		if addr == "" {
			continue
		}
		if !strings.Contains(addr, "@") {
			return "", fmt.Errorf("invalid recipient address")
		}
		domain := addr[strings.LastIndex(addr, "@")+1:]
		if isBlockedMailTarget(domain) {
			return "", fmt.Errorf("delivery to %s not allowed", domain)
		}
		parts = append(parts, addr)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("recipient list is empty")
	}
	return strings.Join(parts, ", "), nil
}

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "0.0.0.0"
	}
	defer conn.Close()
	addr := conn.LocalAddr().(*net.UDPAddr)
	return addr.IP.String()
}
