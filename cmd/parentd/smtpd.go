package main

import (
	"bytes"
	"crypto/tls"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"
	"golang.org/x/crypto/bcrypt"

	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
)

const smtpPort = 2525

const (
	smtpMaxAuthFailures   = 10
	smtpAuthWindowMinutes = 10
)

// smtpAuthAttempts tracks consecutive failed SMTP AUTH attempts per client IP
// to throttle password brute-forcing.
type smtpAuthAttempts struct {
	count   int
	firstAt time.Time
}

type SMTPBackend struct {
	db *sql.DB
	// mu guards authMu's map
	authMu    sync.Mutex
	authState map[string]*smtpAuthAttempts
}

func (b *SMTPBackend) NewSession(c *gosmtp.Conn) (gosmtp.Session, error) {
	return &SMTPSession{db: b.db, logger: logging.NewDefault("smtp"), backend: b, remoteIP: remoteIP(c)}, nil
}

func remoteIP(c *gosmtp.Conn) string {
	if c == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(c.Conn().RemoteAddr().String())
	if err != nil {
		return c.Conn().RemoteAddr().String()
	}
	return host
}

type SMTPSession struct {
	db         *sql.DB
	logger     *logging.Logger
	backend    *SMTPBackend
	remoteIP   string
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
	if s.backend != nil {
		if err := s.backend.ipThrottled(s.remoteIP); err != nil {
			return err
		}
	}
	var hash string
	var emailAccountID int
	err := s.db.QueryRow("SELECT id, password_hash FROM email_accounts WHERE email = ?", username).Scan(&emailAccountID, &hash)
	if err != nil {
		if s.backend != nil {
			s.backend.ipFail(s.remoteIP, username)
		}
		return fmt.Errorf("authentication failed")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		if s.backend != nil {
			s.backend.ipFail(s.remoteIP, username)
		}
		return fmt.Errorf("authentication failed")
	}
	// Block SMTP sending when the hosting account has emails disabled
	// (read-only mode keeps the inbox readable via the panel API).
	var hostingAccountID int
	if err := s.db.QueryRow("SELECT account_id FROM email_accounts WHERE id = ?", emailAccountID).Scan(&hostingAccountID); err == nil {
		if !featureEnabledFor(s.db, hostingAccountID, "emails") {
			s.logger.Infof("SMTP auth refused for %s: emails disabled for account %d", username, hostingAccountID)
			return fmt.Errorf("email access is disabled for this account")
		}
	}
	if s.backend != nil {
		s.backend.ipSuccess(s.remoteIP)
	}
	s.authedUser = username
	s.logger.Infof("Authenticated %s", username)
	return nil
}

// ipFail records a failed SMTP AUTH for the given client IP.
// ipFail records a failed SMTP AUTH attempt for the given client IP.
func (b *SMTPBackend) ipFail(ip, username string) {
	if b == nil {
		return
	}
	b.authMu.Lock()
	defer b.authMu.Unlock()
	log.Printf("[SMTP] auth failure from %s user %s", ip, username)
	if b.authState == nil {
		b.authState = map[string]*smtpAuthAttempts{}
	}
	st, ok := b.authState[ip]
	now := time.Now()
	if !ok || now.Sub(st.firstAt) > smtpAuthWindowMinutes*time.Minute {
		b.authState[ip] = &smtpAuthAttempts{count: 1, firstAt: now}
		return
	}
	st.count++
}

// ipSuccess clears the failure counter for the given client IP after success.
func (b *SMTPBackend) ipSuccess(ip string) {
	b.authMu.Lock()
	delete(b.authState, ip)
	b.authMu.Unlock()
}

// ipThrottled reports whether the client IP has exceeded the SMTP AUTH failure
// budget within the window, refusing further auth attempts.
func (b *SMTPBackend) ipThrottled(ip string) error {
	b.authMu.Lock()
	defer b.authMu.Unlock()
	st, ok := b.authState[ip]
	if !ok {
		return nil
	}
	if time.Since(st.firstAt) > smtpAuthWindowMinutes*time.Minute {
		delete(b.authState, ip)
		return nil
	}
	if st.count >= smtpMaxAuthFailures {
		return fmt.Errorf("too many authentication attempts - try again later")
	}
	return nil
}

func (s *SMTPSession) Mail(from string, opts *gosmtp.MailOptions) error {
	from = strings.TrimSpace(strings.Trim(from, "<>"))
	if from == "" {
		return fmt.Errorf("missing sender")
	}
	if _, err := mail.ParseAddress(from); err != nil {
		return fmt.Errorf("invalid sender address")
	}
	s.from = from
	return nil
}

func (s *SMTPSession) Rcpt(to string, opts *gosmtp.RcptOptions) error {
	to = strings.TrimSpace(strings.Trim(to, "<>"))
	if to == "" {
		return fmt.Errorf("missing recipient")
	}
	if _, err := mail.ParseAddress(to); err != nil {
		return fmt.Errorf("invalid recipient address")
	}
	s.to = append(s.to, to)
	return nil
}

func (s *SMTPSession) Data(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read message data: %w", err)
	}

	if s.authedUser != "" && len(raw) > 0 {
		trackSMTPBandwidth(s.db, s.authedUser, int64(len(raw)))
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
	}

	bodyText, bodyHTML := extractBody(raw, msg)

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	messageID := fmt.Sprintf("<%d.%x@localhost>", time.Now().UnixNano(), time.Now().UnixNano())

	// Tenant isolation: without SMTP AUTH this server only accepts mail for
	// local mailboxes (inbound). It must never act as an open relay to
	// arbitrary remote recipients.
	if s.authedUser == "" {
		for _, rcpt := range s.to {
			rcpt = strings.TrimSpace(strings.Trim(rcpt, "<>"))
			if rcpt == "" {
				continue
			}
			var localID int
			if err := s.db.QueryRow("SELECT id FROM email_accounts WHERE email = ?", rcpt).Scan(&localID); err != nil {
				return fmt.Errorf("relay denied: %s is not a local mailbox", rcpt)
			}
		}
	}

	// Anti-spoofing: an authenticated sender may only send from their own
	// address, not from arbitrary addresses.
	if s.authedUser != "" && !strings.EqualFold(strings.TrimSpace(s.from), s.authedUser) {
		return fmt.Errorf("sender %s not allowed for authenticated user %s", s.from, s.authedUser)
	}

	for _, rcpt := range s.to {
		rcpt = strings.TrimSpace(rcpt)
		if rcpt == "" {
			continue
		}
		rcpt = strings.Trim(rcpt, "<>")

		var acctID int
		err := s.db.QueryRow("SELECT id FROM email_accounts WHERE email = ?", rcpt).Scan(&acctID)
		if err == nil {
			_, storeErr := s.db.Exec(`INSERT INTO email_messages
				(email_account_id, folder, from_addr, to_addr, subject, body_text, body_html, flags, message_id, received_at)
				VALUES (?, 'INBOX', ?, ?, ?, ?, ?, '', ?, ?)`,
				acctID, fromAddr, rcpt, subject, bodyText, bodyHTML, messageID, now)
			if storeErr != nil {
				s.logger.Errorf("Local delivery error for %s: %v", rcpt, storeErr)
			} else {
				s.logger.Infof("Delivered to local %s (acct %d)", rcpt, acctID)

				var forwardTo string
				s.db.QueryRow("SELECT forward_to FROM email_accounts WHERE id = ? AND forward_to != ''", acctID).Scan(&forwardTo)
				if forwardTo != "" {
					s.logger.Infof("Forwarding from %s to %s", rcpt, forwardTo)
					for _, fwd := range strings.Split(forwardTo, ",") {
						fwd = strings.TrimSpace(fwd)
						if fwd != "" {
							if err := deliverRemote(s.db, fromAddr, fwd, raw); err != nil {
								s.logger.Errorf("Forward delivery to %s failed: %v", fwd, err)
							}
						}
					}
				}
			}
		} else if strings.Contains(rcpt, "@") {
			if err := deliverRemote(s.db, fromAddr, rcpt, raw); err != nil {
				s.logger.Errorf("Remote delivery to %s failed: %v", rcpt, err)
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
		var auth sasl.Client
		if relayUser != "" {
			auth = sasl.NewPlainClient("", relayUser, relayPass)
		}
		return gosmtp.SendMail(addr, auth, from, []string{to}, bytes.NewReader(raw))
	}

	domain := to[strings.LastIndex(to, "@")+1:]
	// Never deliver to internal/loopback/link-local destinations: a tenant
	// could otherwise use the panel server as an SMTP probing relay into the
	// private network.
	if isBlockedMailTarget(domain) {
		return fmt.Errorf("delivery to %s not allowed", domain)
	}
	mxRecords, err := net.LookupMX(domain)
	if err != nil || len(mxRecords) == 0 {
		addrs, err := net.LookupHost(domain)
		if err != nil || len(addrs) == 0 {
			return fmt.Errorf("cannot resolve %s", domain)
		}
		if isBlockedMailTarget(addrs[0]) {
			return fmt.Errorf("delivery to %s not allowed", addrs[0])
		}
		addr := net.JoinHostPort(addrs[0], "25")
		return smtp.SendMail(addr, nil, from, []string{to}, raw)
	}

	for _, mx := range mxRecords {
		addrs, err := net.LookupHost(mx.Host)
		if err != nil || len(addrs) == 0 {
			continue
		}
		blocked := false
		for _, a := range addrs {
			if ip := net.ParseIP(a); ip != nil && isBlockedIP(ip) {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		addr := net.JoinHostPort(mx.Host, "25")
		return smtp.SendMail(addr, nil, from, []string{to}, raw)
	}
	return fmt.Errorf("no usable MX for %s", domain)
}

// isBlockedMailTarget reports whether the given hostname or IP must never be a
// direct SMTP delivery target: loopback, private/link-local networks and bare
// IP literals are all potential SSRF paths through the panel server.
func isBlockedMailTarget(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return true
	}
	// IP literals are never valid external mail destinations for MX delivery.
	if ip := net.ParseIP(h); ip != nil {
		return isBlockedIP(ip)
	}
	// Resolve and check any A/AAAA addresses for the name (e.g. a DNS name
	// pointing at a loopback/private address).
	addrs, err := net.LookupIP(h)
	if err != nil {
		return false
	}
	for _, ip := range addrs {
		if isBlockedIP(ip) {
			return true
		}
	}
	return false
}

// isBlockedIP reports whether an IP must never be a direct SMTP delivery
// target: loopback, private/link-local/unspecified addresses are all potential
// SSRF paths through the panel server.
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func startSMTPServer(db *sql.DB) {
	l := logging.NewDefault("smtp")
	be := &SMTPBackend{db: db, authState: map[string]*smtpAuthAttempts{}}
	srv := gosmtp.NewServer(be)
	srv.Domain = "localhost"
	srv.ReadTimeout = 60 * time.Second
	srv.WriteTimeout = 60 * time.Second
	srv.MaxMessageBytes = 25 * 1024 * 1024
	srv.MaxRecipients = 50

	// SMTP AUTH is only permitted after a STARTTLS handshake. Mailbox
	// credentials must never cross the network in cleartext.
	srv.AllowInsecureAuth = false

	// Load or generate a TLS certificate so STARTTLS is available.
	if tlsConf := smtpTLSConfig(l); tlsConf != nil {
		srv.TLSConfig = tlsConf
	} else {
		l.Warnf("Could not load/generate SMTP TLS certificate — STARTTLS disabled (auth will be refused)")
	}

	smtpListenPort := os.Getenv("OWP_SMTP_PORT")
	if smtpListenPort == "" {
		smtpListenPort = fmt.Sprintf("%d", smtpPort)
	}
	addr := fmt.Sprintf(":%s", smtpListenPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		l.Errorf("Cannot listen on %s: %v (incoming mail disabled)", addr, err)
		return
	}
	l.Infof("Listening on %s", addr)
	if err := srv.Serve(listener); err != nil {
		l.Errorf("Server error: %v", err)
	}
}

func smtpTLSConfig(l *logging.Logger) *tls.Config {
	sslDir := os.Getenv("OWP_HOMES_BASE")
	if sslDir == "" {
		sslDir = "./homes/"
	}
	sslDir = strings.TrimSuffix(sslDir, "/") + "/ssl/"

	certFile := os.Getenv("OWP_SMTP_TLS_CERT")
	keyFile := os.Getenv("OWP_SMTP_TLS_KEY")
	if certFile == "" || keyFile == "" {
		certFile = sslDir + "owp_smtp.crt"
		keyFile = sslDir + "owp_smtp.key"
	}
	if cert, err := tls.LoadX509KeyPair(certFile, keyFile); err == nil {
		return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}

	// If the dedicated SMTP self-signed cert is missing, fall back to any
	// existing tenant self-signed cert so SMTP STARTTLS (and thus AUTH) keeps
	// working. The cert is only used to encrypt the channel.
	if matches, err := filepath.Glob(sslDir + "*.crt"); err == nil {
		for _, caf := range matches {
			keyf := strings.TrimSuffix(caf, ".crt") + ".key"
			if cert, err := tls.LoadX509KeyPair(caf, keyf); err == nil {
				if l != nil {
					l.Infof("SMTP TLS: falling back to %s", caf)
				}
				return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
			}
		}
	}
	if l != nil {
		l.Warnf("Could not load SMTP TLS certificate — STARTTLS disabled (auth will be refused)")
	}
	return nil
}

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

func buildRawEmail(from, to, subject, bodyText, bodyHTML string) []byte {
	// Strip CR/LF from header field values to prevent header injection through
	// attacker-controlled addresses/subjects.
	sanitizeHeader := func(s string) string {
		s = strings.ReplaceAll(s, "\r", "")
		s = strings.ReplaceAll(s, "\n", "")
		return strings.TrimSpace(s)
	}
	from = sanitizeHeader(from)
	to = sanitizeHeader(to)
	subject = sanitizeHeader(subject)
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
