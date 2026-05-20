# OpenWebPanel

A lightweight web hosting control panel built with Go and React. Manage websites, databases, emails, DNS, SSL certificates, and more — all from a clean web interface.

![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)
![React](https://img.shields.io/badge/React-18.3-61DAFB?logo=react)
![License](https://img.shields.io/badge/license-MIT-green)

## Features

- **Hosting Accounts** — Create and manage hosting accounts with resource limits
- **File Manager** — Browse, upload, edit, and manage files via browser
- **Database Manager** — MariaDB database creation and phpMyAdmin integration
- **Domain Manager** — Addon/parked/subdomain management with auto-Nginx vhosts
- **Email** — Email accounts, webmail, forwards, and built-in SMTP server
- **DNS Editor** — Manage A, AAAA, CNAME, MX, TXT records
- **SSL Certificates** — Let's Encrypt auto-renewal
- **CMS Installer** — One-click WordPress and other CMS installations
- **FTP Accounts** — Isolated FTP access per account
- **Backups** — Automated backup and restore
- **Bandwidth Monitoring** — Track usage per account
- **Cron Jobs** — User-managed scheduled tasks
- **phpMyAdmin** — Integrated database management
- **One-Click Install** — Single-command Ubuntu/Debian installer

## Quick Start

```bash
curl -fsSL https://raw.githubusercontent.com/jiyasrulalomjuwel/open-web-panel/main/install.sh | sudo bash
```

The installer auto-detects your system and sets up everything in ~10-15 minutes.

### After Install

| URL | Description |
|---|---|
| `http://your-server:2086` | Admin Panel |
| `http://your-server:2082` | User Panel |
| `http://your-server` | Main Site (port 80) |

**Default login:** `admin` / `admin123`

## Architecture

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐
│  Nginx      │────▶│  parentd     │────▶│  SQLite     │
│  (80/2086   │     │  (Go API)    │     │  (Panel DB) │
│   /2082)    │     │  :9000       │     │             │
└─────────────┘     └──────┬───────┘     └─────────────┘
                           │
┌─────────────┐     ┌──────┴───────┐     ┌─────────────┐
│  MariaDB    │◀────│  childd      │     │  React SPA  │
│  (User DBs) │     │  (User File  │     │  (Served by │
│             │     │   Server)    │     │   parentd)  │
└─────────────┘     └──────────────┘     └─────────────┘
```

### Components

- **parentd** — Main daemon: REST API, SQLite panel DB, Nginx vhost management, SMTP server, cron runner, SSL renewal
- **childd** — Per-user file server with path traversal protection
- **Web UI** — React SPA with admin panel (port 2086) and user panel (port 2082)
- **Nginx** — Reverse proxy for the panel and user websites
- **MariaDB** — User hosting databases (separate from the panel's SQLite DB)

## Requirements

- **OS:** Ubuntu 20.04+ or Debian 11+
- **RAM:** 1GB minimum (swap auto-created if less)
- **Disk:** 5GB+ free
- **Arch:** x86_64 or ARM64

## Development

```bash
# Clone the repo
git clone https://github.com/jiyasrulalomjuwel/open-web-panel.git
cd open-web-panel

# Build and run the backend
make dev-backend

# In another terminal, run the frontend
make dev-frontend

# Or use the dev script
sudo bash dev.sh
```

### Project Structure

```
├── cmd/
│   ├── parentd/       # Main admin panel API daemon
│   └── childd/        # Per-user file server
├── internal/
│   ├── parent/        # Admin-specific logic (accounts, packages)
│   │   ├── accounts/
│   │   ├── api/
│   │   └── packages/
│   └── shared/        # Shared libraries
│       ├── audit/     # Audit logging
│       ├── auth/      # JWT + bcrypt authentication
│       ├── config/    # Configuration management
│       ├── db/        # Database connectors (SQLite + MariaDB)
│       ├── filesystem/# Path traversal safe file operations
│       ├── logging/   # Logging utilities
│       └── middleware/# HTTP middleware (auth, CORS, logging)
├── web/               # React frontend
│   └── src/
│       ├── components/# Shared UI components
│       ├── pages/     # Page components
│       └── lib/       # API client library
├── deploy/
│   └── systemd/       # Systemd service files
├── migrations/        # SQL migrations
└── install.sh         # One-command installer
```

## Production Deployment

```bash
# Full installation
sudo bash install.sh

# Or with custom options
sudo OWP_DOMAIN=panel.example.com OWP_PANEL_PORT=2086 bash install.sh
```

### Management Commands

```bash
systemctl status openwebpanel    # Check panel status
journalctl -u openwebpanel -f    # View live logs
systemctl restart openwebpanel   # Restart the panel
tail -f /opt/openwebpanel/logs/watchdog.log  # Watchdog log
```

### Manual Start (for testing)

```bash
sudo bash start.sh
```

## Configuration

All configuration is via environment variables (see `.env.example`):

| Variable | Default | Description |
|---|---|---|
| `OWP_LISTEN` | `:9000` | API listen address |
| `OWP_DB_PATH` | `./openwebpanel.db` | SQLite database path |
| `OWP_STATIC_DIR` | `./web/dist` | Frontend static files |
| `OWP_JWT_SECRET` | auto-generated | JWT signing key |
| `OWP_HOMES_BASE` | `./homes/` | User home directories |
| `OWP_PUBLIC_HOST` | auto-detected | Server public hostname |
| `NGINX_VHOST_DIR` | `/etc/nginx/vhosts` | Nginx vhost configs |

## Docker

```bash
make docker-build
make docker-up
```

## License

MIT
