package main

import (
	"crypto/tls"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"

	"github.com/openwebcpanel/openwebcpanel/internal/shared/auth"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
)

// ftpDriver authenticates against ftp_accounts and jails each session to the
// account's confined directory via afero. Previously FTP accounts were
// database rows with no listener at all.
type ftpDriver struct {
	db     *sql.DB
	logger *logging.Logger
}

func ftpPort() string {
	if p := strings.TrimSpace(os.Getenv("OWP_FTP_PORT")); p != "" {
		return p
	}
	return "21"
}

func ftpPasvRange() (int, int) {
	raw := strings.TrimSpace(os.Getenv("OWP_FTP_PASV_RANGE"))
	if raw == "" {
		raw = "40000-40009"
	}
	parts := strings.SplitN(raw, "-", 2)
	if len(parts) != 2 {
		return 40000, 40009
	}
	lo, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	hi, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || lo <= 0 || hi < lo || hi-lo > 100 {
		return 40000, 40009
	}
	return lo, hi
}

// ftpPublicIP is the address advertised for PASV data connections: explicit
// override first, then the shared server IP when it is publicly routable.
func ftpPublicIP() string {
	if ip := strings.TrimSpace(os.Getenv("OWP_FTP_PUBLIC_IP")); ip != "" {
		return ip
	}
	if ip := net.ParseIP(getSharedIP()); ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() {
		return ip.String()
	}
	return ""
}

func (d *ftpDriver) GetSettings() (*ftpserver.Settings, error) {
	lo, hi := ftpPasvRange()
	s := &ftpserver.Settings{
		ListenAddr:                 "0.0.0.0:" + ftpPort(),
		PassiveTransferPortRange:   &ftpserver.PortRange{Start: lo, End: hi},
		PublicHost:                 ftpPublicIP(),
		IdleTimeout:                300,
		ConnectionTimeout:          30,
		DisableMLSD:                false,
		ActiveTransferPortNon20:    true,
	}
	return s, nil
}

func (d *ftpDriver) ClientConnected(cc ftpserver.ClientContext) (string, error) {
	return "OpenWebPanel FTP ready", nil
}

func (d *ftpDriver) ClientDisconnected(cc ftpserver.ClientContext) {}

func (d *ftpDriver) AuthUser(cc ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	var id, accountID int
	var hash, directory, ftpStatus, accStatus, homeDir string
	err := d.db.QueryRow(`SELECT f.id, f.account_id, f.password_hash, f.directory, f.status,
		a.status, a.home_dir
		FROM ftp_accounts f JOIN accounts a ON a.id = f.account_id
		WHERE f.username = ?`, user).Scan(&id, &accountID, &hash, &directory, &ftpStatus, &accStatus, &homeDir)
	if err != nil {
		return nil, fmt.Errorf("authentication failed")
	}
	if !auth.CheckPassword(hash, pass) {
		return nil, fmt.Errorf("authentication failed")
	}
	if ftpStatus != "active" {
		return nil, fmt.Errorf("FTP account is %s", ftpStatus)
	}
	if accStatus != "active" {
		return nil, fmt.Errorf("hosting account is %s", accStatus)
	}
	if !featureEnabledFor(d.db, accountID, "ftp") {
		return nil, fmt.Errorf("FTP access is disabled for this account")
	}
	if homeDir == "" {
		return nil, fmt.Errorf("account home not configured")
	}
	// The stored directory is already confined at create/update time; re-join
	// defensively and refuse anything escaping the home directory.
	base := filepath.Join(homeDir, filepath.Clean("/"+directory))
	if !isPathWithin(homeDir, base) {
		d.logger.Errorf("FTP jail escape refused for %s (dir %q)", user, directory)
		return nil, fmt.Errorf("invalid FTP directory")
	}
	if err := os.MkdirAll(base, 0755); err != nil {
		return nil, fmt.Errorf("FTP home unavailable")
	}
	d.logger.Infof("FTP login %s (account %d)", user, accountID)
	return afero.NewBasePathFs(afero.NewOsFs(), base), nil
}

func (d *ftpDriver) GetTLSConfig() (*tls.Config, error) {
	certFile := strings.TrimSpace(os.Getenv("OWP_FTP_TLS_CERT"))
	keyFile := strings.TrimSpace(os.Getenv("OWP_FTP_TLS_KEY"))
	if certFile == "" || keyFile == "" {
		return nil, nil // plaintext with a startup warning (see below)
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}

// startFTPServer runs the FTP listener in the background. A bind failure is
// logged loudly but never kills the panel (e.g. unprivileged native installs
// should set OWP_FTP_PORT=2121).
func startFTPServer(db *sql.DB) {
	l := logging.NewDefault("ftp")
	driver := &ftpDriver{db: db, logger: l}
	if os.Getenv("OWP_FTP_TLS_CERT") == "" {
		l.Warnf("FTP running without TLS — set OWP_FTP_TLS_CERT/OWP_FTP_TLS_KEY for FTPS")
	}
	server := ftpserver.NewFtpServer(driver)
	go func() {
		l.Infof("FTP listening on :%s (pasv %s)", ftpPort(), os.Getenv("OWP_FTP_PASV_RANGE"))
		if err := server.ListenAndServe(); err != nil {
			log.Printf("[FTP] listener failed on port %s: %v (set OWP_FTP_PORT, e.g. 2121 for unprivileged users)", ftpPort(), err)
		}
	}()
}
