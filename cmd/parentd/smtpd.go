package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"
	"golang.org/x/crypto/bcrypt"
)

const smtpPort = 2525

// ── SMTP Backend ──

type SMTPBackend struct {
	db *sql.DB
}

func (b *SMTPBackend) NewSession(c *gosmtp.Conn) (gosmtp.Session, error) {
	return &SMTPSession{db: b.db}, nil
}

// ── SMTP Session ──

type SMTPSession struct {
	db         *sql.DB
	authedUser string
	from       string
	to         []string
}

func (s *SMTPSession) AuthMechanisms() []string {
	return []string{"PLAIN"}
}

func (s *SMTPSession) Auth(mech string) (sasl.Server, error) {
	switch mech {
	case "PLAIN":
		return sasl.NewPlainServer(func(identity, username, password string) error {
			return s.authenticate(username, password)
		}), nil
	default:
		return nil, fmt.Errorf("unsupported auth mechanism: %s", mech)
	}
}

func (s *SMTPSession) authenticate(username, password string) error {
	var hash string
	err := s.db.QueryRow("SELECT password_hash FROM email_accounts WHERE email = ?", username).Scan(&hash)
	if err != nil {
		return fmt.Errorf("authentication failed")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return fmt.Errorf("authentication failed")
	}
	s.authedUser = username
	return nil
}

func (s *SMTPSession) Mail(from string, opts *gosmtp.MailOptions) error {
	s.from = from
	return nil
}

func (s *SMTPSession) Rcpt(to string, opts *gosmtp.RcptOptions) error {
	s.to = append(s.to, to)
	return nil
}

func (s *SMTPSession) Data(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read message data: %w", err)
	}

	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		msg = nil
	}

	subject := ""
	fromAddr := s.from
	if msg != nil {
		if h := msg.Header.Get("Subject"); h != "" {
			subject = decodeHeader(h)
		}
		if h := msg.Header.Get("From"); h != "" && fromAddr == "" {
			fromAddr = h
		}
		_ = msg.Header.Get("To")
	}

	bodyText, bodyHTML := extractBody(raw, msg)

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	messageID := fmt.Sprintf("<%d.%x@localhost>", time.Now().UnixNano(), time.Now().UnixNano())

	// Process each recipient
	for _, rcpt := range s.to {
		rcpt = strings.TrimSpace(rcpt)
		if rcpt == "" {
			continue
		}
		rcpt = strings.Trim(rcpt, "<>")

		// Check if recipient is a local email account
		var acctID int
		err := s.db.QueryRow("SELECT id FROM email_accounts WHERE email = ?", rcpt).Scan(&acctID)
		if err == nil {
			// Local delivery — store in INBOX
			_, storeErr := s.db.Exec(`INSERT INTO email_messages
				(email_account_id, folder, from_addr, to_addr, subject, body_text, body_html, flags, message_id, received_at)
				VALUES (?, 'INBOX', ?, ?, ?, ?, ?, '', ?, ?)`,
				acctID, fromAddr, rcpt, subject, bodyText, bodyHTML, messageID, now)
			if storeErr != nil {
				log.Printf("[SMTP] Local delivery error for %s: %v", rcpt, storeErr)
			} else {
				log.Printf("[SMTP] Delivered to local %s (acct %d)", rcpt, acctID)

				// Handle forwarding
				var forwardTo string
				s.db.QueryRow("SELECT forward_to FROM email_accounts WHERE id = ? AND forward_to != ''", acctID).Scan(&forwardTo)
				if forwardTo != "" {
					log.Printf("[SMTP] Forwarding from %s to %s", rcpt, forwardTo)
					for _, fwd := range strings.Split(forwardTo, ",") {
						fwd = strings.TrimSpace(fwd)
						if fwd != "" {
							if err := deliverRemote(s.db, fromAddr, fwd, raw); err != nil {
								log.Printf("[SMTP] Forward delivery to %s failed: %v", fwd, err)
							}
						}
					}
				}
			}
		} else if strings.Contains(rcpt, "@") {
			// Remote address — relay outbound
			if err := deliverRemote(s.db, fromAddr, rcpt, raw); err != nil {
				log.Printf("[SMTP] Remote delivery to %s failed: %v", rcpt, err)
			}
		}
	}

	return nil
}

func (s *SMTPSession) Reset() {
	s.from = ""
	s.to = nil
}

func (s *SMTPSession) Logout() error {
	return nil
}

// ── Public delivery function (used by both SMTP server and API send endpoint) ──

func deliverRemote(db *sql.DB, from, to string, raw []byte) error {
	if from == "" {
		from = "postmaster@localhost"
	}

	var relayHost, relayPort, relayUser, relayPass string
	db.QueryRow("SELECT COALESCE((SELECT value FROM server_config WHERE key_name = 'smtp_relay_host'), '')").Scan(&relayHost)
	db.QueryRow("SELECT COALESCE((SELECT value FROM server_config WHERE key_name = 'smtp_relay_port'), '587')").Scan(&relayPort)
	db.QueryRow("SELECT COALESCE((SELECT value FROM server_config WHERE key_name = 'smtp_relay_username'), '')").Scan(&relayUser)
	db.QueryRow("SELECT COALESCE((SELECT value FROM server_config WHERE key_name = 'smtp_relay_password'), '')").Scan(&relayPass)

	if relayHost != "" {
		addr := net.JoinHostPort(relayHost, relayPort)
		log.Printf("[SMTP] Relaying %s -> %s via %s", from, to, addr)

		var auth sasl.Client
		if relayUser != "" {
			auth = sasl.NewPlainClient("", relayUser, relayPass)
		}

		// Use go-smtp's SendMail which supports STARTTLS
		return gosmtp.SendMail(addr, auth, from, []string{to}, bytes.NewReader(raw))
	}

	// Direct delivery via standard net/smtp
	domain := to[strings.LastIndex(to, "@")+1:]
	mxRecords, err := net.LookupMX(domain)
	if err != nil || len(mxRecords) == 0 {
		log.Printf("[SMTP] No MX records for %s, trying A record", domain)
		addrs, err := net.LookupHost(domain)
		if err != nil || len(addrs) == 0 {
			return fmt.Errorf("cannot resolve %s", domain)
		}
		addr := net.JoinHostPort(addrs[0], "25")
		return smtp.SendMail(addr, nil, from, []string{to}, raw)
	}

	addr := net.JoinHostPort(mxRecords[0].Host, "25")
	log.Printf("[SMTP] Delivering %s -> %s via MX %s", from, to, addr)
	return smtp.SendMail(addr, nil, from, []string{to}, raw)
}

// ── Start SMTP server ──

func startSMTPServer(db *sql.DB) {
	be := &SMTPBackend{db: db}
	srv := gosmtp.NewServer(be)
	srv.Domain = "localhost"
	srv.ReadTimeout = 60 * time.Second
	srv.WriteTimeout = 60 * time.Second
	srv.MaxMessageBytes = 25 * 1024 * 1024
	srv.MaxRecipients = 50
	srv.AllowInsecureAuth = true

	// Start on OWP_SMTP_PORT env var (default 2525), then try 25 as fallback
	smtpListenPort := os.Getenv("OWP_SMTP_PORT")
	if smtpListenPort == "" {
		smtpListenPort = fmt.Sprintf("%d", smtpPort)
	}
	addr := fmt.Sprintf(":%s", smtpListenPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("[SMTP] Cannot listen on %s: %v (incoming mail disabled)", addr, err)
		return
	}
	log.Printf("[SMTP] Listening on %s", addr)
	if err := srv.Serve(listener); err != nil {
		log.Printf("[SMTP] Server error: %v", err)
	}
}

// ── Helpers ──

func decodeHeader(h string) string {
	dec := textproto.TrimBytes([]byte(h))
	h = strings.TrimSpace(string(dec))
	if strings.Contains(h, "=?") {
		if addrs, err := mail.ParseAddressList(h); err == nil && len(addrs) > 0 {
			return addrs[0].Name + " <" + addrs[0].Address + ">"
		}
	}
	return h
}

func extractBody(raw []byte, msg *mail.Message) (text, html string) {
	if msg == nil {
		text = string(raw)
		return
	}

	contentType := msg.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "text/plain") {
		b, _ := io.ReadAll(msg.Body)
		text = string(b)
		return
	}

	b, _ := io.ReadAll(msg.Body)
	bodyStr := string(b)

	if strings.Contains(contentType, "multipart/alternative") || strings.Contains(contentType, "multipart/mixed") {
		parts := strings.Split(bodyStr, "--")
		for _, part := range parts {
			pl := strings.ToLower(part)
			if strings.Contains(pl, "content-type: text/plain") {
				if idx := strings.Index(part, "\n\n"); idx != -1 {
					text = cleanup(part[idx+2:])
				}
			}
			if strings.Contains(pl, "content-type: text/html") {
				if idx := strings.Index(part, "\n\n"); idx != -1 {
					html = cleanup(part[idx+2:])
				}
			}
		}
	}

	if text == "" && html == "" {
		text = bodyStr
	}
	return
}

func cleanup(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "\n--"); i != -1 {
		s = s[:i]
	}
	if i := strings.Index(s, "\nContent-"); i != -1 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// Build a raw RFC 822 email from parts (used by API send endpoint)
func buildRawEmail(from, to, subject, bodyText, bodyHTML string) []byte {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("From: %s\r\n", from))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", to))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	if bodyHTML != "" && bodyText != "" {
		boundary := fmt.Sprintf("=_%x", time.Now().UnixNano())
		buf.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
		buf.WriteString("\r\n--" + boundary + "\r\n")
		buf.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n\r\n")
		buf.WriteString(bodyText + "\r\n")
		buf.WriteString("\r\n--" + boundary + "\r\n")
		buf.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n\r\n")
		buf.WriteString(bodyHTML + "\r\n")
		buf.WriteString("\r\n--" + boundary + "--\r\n")
	} else if bodyHTML != "" {
		buf.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n\r\n")
		buf.WriteString(bodyHTML)
	} else {
		buf.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n\r\n")
		buf.WriteString(bodyText)
	}
	return buf.Bytes()
}
