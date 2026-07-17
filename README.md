# OpenWebPanel

A lightweight, self-hosted web hosting control panel built with **Go** and **React**. Manage websites, databases, emails, SSL certificates, FTP accounts, and more — all from a clean web interface.

![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)
![React](https://img.shields.io/badge/React-18.3-61DAFB?logo=react)
![License](https://img.shields.io/badge/license-MIT-blue)

---

## ✨ Features

| Category | Features |
|---|---|
| **Hosting Accounts** | Create/manage accounts with resource limits (disk, bandwidth, databases, email, FTP) |
| **File Manager** | Browser-based file browsing, upload, edit, extract, and management |
| **Database Manager** | MariaDB user databases + integrated phpMyAdmin |
| **Domain Manager** | Addon domains, parked domains, subdomains — auto-Nginx vhost generation |
| **Email** | Email accounts, webmail (Roundcube), built-in SMTP server, forwards |
| **SSL Certificates** | Let's Encrypt auto-renewal, custom SSL upload |
| **CMS Installer** | One-click WordPress and other CMS installations |
| **FTP Accounts** | Per-account isolated FTP access |
| **Bandwidth Monitoring** | Track usage per account via Nginx logs + SMTP |
| **Custom Error Pages** | Per-domain custom 404/500/etc. error pages served by Nginx |
| **Hotlink Protection** | Protect your media from hotlinking |
| **Redirects** | URL redirection management |
| **PHP** | Per-domain PHP-FPM socket support |
| **Backups** | Automated nightly backups |
| **Cron Jobs** | User-managed scheduled tasks |

---

## 🚀 Quick Install

```bash
curl -fsSL https://raw.githubusercontent.com/jiyasrulalomjuwel/open-web-panel/main/install.sh | sudo bash
```

The installer auto-detects your system and provisions everything in **~10–15 minutes**.

### What it sets up

- Nginx (reverse proxy + website vhosts)
- MariaDB (user databases)
- PHP-FPM (configurable version)
- phpMyAdmin
- Go backend (REST API)
- React frontend (admin panel + user panel)
- SMTP server (incoming mail)
- Firewall rules (UFW)
- Watchdog + log rotation + nightly backups

### After Installation

| URL | Description |
|---|---|
| `http://your-server:2086` | **Admin Panel** — manage accounts, packages, settings |
| `http://your-server:2082` | **User Panel** — your hosting clients manage their sites |
| `http://your-server` | Main website (port 80) |

The admin password is **randomly generated** during installation and printed at the end. It is also stored in `/opt/openwebpanel/app/.env`.

### Custom Installation

```bash
# Specify a domain/IP
sudo OWP_DOMAIN=panel.example.com bash install.sh

# Use custom ports
sudo OWP_PANEL_PORT=8443 OWP_USER_PORT=8444 bash install.sh

# Skip firewall or swap
sudo OWP_SKIP_FIREWALL=true OWP_SKIP_SWAP=true bash install.sh
```

---

## 🏗 Architecture

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐
│   Nginx     │────▶│   parentd    │────▶│   SQLite    │
│  (80/2086   │     │  (Go API)    │     │  (Panel DB) │
│   /2082)    │     │   :9000      │     │             │
└─────────────┘     └──────┬───────┘     └─────────────┘
                           │
┌─────────────┐     ┌──────┴───────┐     ┌─────────────┐
│   MariaDB   │◀────│   childd     │     │  React SPA  │
│  (User DBs) │     │  (User File  │     │  (Served by │
│             │     │   Server)    │     │   parentd)  │
└─────────────┘     └──────────────┘     └─────────────┘
```

### Components

- **`parentd`** — Main Go daemon: REST API, SQLite panel database, Nginx vhost management, SMTP server, cron runner, SSL renewal. Runs on port `:9000`.
- **`childd`** — Per-user file server with path traversal protection.
- **Web UI** — React SPA served by `parentd`. Admin panel on port `2086`, user panel on port `2082`.
- **Nginx** — Reverse proxy for the panel UI + serves user websites with PHP-FPM.
- **MariaDB** — MySQL-compatible database for user websites (separate from the panel's SQLite DB).

---

## 📋 Requirements

| Requirement | Minimum |
|---|---|
| **OS** | Ubuntu 20.04+ or Debian 11+ |
| **RAM** | 1 GB (swap auto-created if less) |
| **Disk** | 5 GB+ free |
| **Arch** | x86_64 or ARM64 |
| **Root access** | Required for installation |

### Installed Dependencies

| Dependency | Role | Details |
|---|---|---|
| **Nginx** | Reverse proxy + website vhosts | Must include `/etc/nginx/vhosts/*.conf` in `nginx.conf` |
| **PHP-FPM** | PHP script execution via FastCGI | Socket path set via `PHP_FPM_SOCKET` env var; must match installed version |
| **MariaDB** | User website databases | Separate from panel's SQLite DB |

### Required Nginx Configuration

The panel writes vhost configs to `NGINX_VHOST_DIR` (default `/etc/nginx/vhosts/`). For nginx to serve user websites, add to `nginx.conf`:

```nginx
http {
    # ... existing config ...
    include /etc/nginx/vhosts/*.conf;
}
```

**Permissions:** The panel process must have write access to `NGINX_VHOST_DIR`, and the nginx worker user (`www-data`) must be able to traverse the account home directories. On development setups:

```bash
sudo chown <panel-user>:<panel-user> /etc/nginx/vhosts
sudo usermod -a -G <panel-user-group> www-data   # grants www-data access to homes
sudo mkdir -p /var/log/nginx && sudo chown www-data:www-data /var/log/nginx
```

---

## 🛠 Development

```bash
git clone https://github.com/jiyasrulalomjuwel/open-web-panel.git
cd open-web-panel

# Build backend
go build -o bin/parentd ./cmd/parentd/

# Build frontend
cd web && npm install && npm run build && cd ..

# Run (with env vars)
OWP_DB_PATH=./openwebpanel.db \
OWP_STATIC_DIR=./web/dist \
OWP_LISTEN=:9000 \
sudo -E ./bin/parentd
```

Or use the development script:
```bash
sudo bash dev.sh
```

### Project Structure

```
├── cmd/
│   ├── parentd/          # Main admin panel daemon (Go)
│   ├── childd/           # Per-user file server (Go)
│   └── dbadmin/          # SQLite database admin tool
├── internal/shared/      # Shared libraries
│   ├── auth/             # JWT + bcrypt authentication
│   ├── db/               # SQLite + MariaDB connectors
│   ├── filesystem/       # Safe file operations
│   ├── audit/            # Audit logging
│   ├── config/           # Configuration (unused)
│   ├── logging/          # Logging utilities (unused)
│   └── middleware/       # HTTP middleware (unused)
├── web/                  # React frontend
│   └── src/
│       ├── components/   # Shared UI components + layouts
│       ├── pages/        # 26 page components
│       └── lib/          # API client
├── migrations/           # SQL schema files (MySQL format)
├── deploy/               # systemd units + Docker scripts
├── install.sh            # One-command installer
├── start.sh              # Quick start script
└── dev.sh                # Development launcher
```

---

## 🔧 Management Commands

```bash
# Service management
systemctl status openwebpanel    # Panel status
systemctl restart openwebpanel   # Restart the panel
journalctl -u openwebpanel -f    # Live logs

# Logs
tail -f /opt/openwebpanel/logs/parentd.log
tail -f /opt/openwebpanel/logs/watchdog.log

# Configuration
cat /opt/openwebpanel/app/.env   # Panel environment variables
```

---

## ⚙️ Configuration

All panel configuration is via environment variables (see `.env.example`):

| Variable | Default | Description |
|---|---|---|
| `OWP_LISTEN` | `:9000` | API listen address |
| `OWP_DB_PATH` | `./openwebpanel.db` | SQLite database path |
| `OWP_STATIC_DIR` | `./web/dist` | Frontend static files |
| `OWP_JWT_SECRET` | auto-generated | JWT signing key |
| `OWP_ADMIN_PASSWORD` | randomly generated | Initial admin password |
| `OWP_HOMES_BASE` | `./homes/` | User home directories |
| `OWP_PUBLIC_HOST` | auto-detected | Server public hostname |
| `NGINX_VHOST_DIR` | `/etc/nginx/vhosts` | Nginx vhost configs |
| `NGINX_LOG_DIR` | `/var/log/nginx` | Nginx log directory |
| `OWP_SMTP_PORT` | `2525` | Incoming SMTP port |

---

## 🐳 Docker

The project includes a multi-stage Docker build. Required environment variables must be set:

```bash
# Required: Set strong passwords before starting
export OWP_JWT_SECRET="generate-a-random-64-char-secret"
export OWP_ADMIN_PASSWORD="strong-admin-password"
export MYSQL_ROOT_PASSWORD="strong-mysql-root-password"
export MYSQL_ADMIN_PASSWORD="strong-mysql-admin-password"

make docker-build
make docker-up
```

**Note:** The Dockerfile no longer bundles PHP-FPM configuration or nginx site configs. These must be configured separately or generated at runtime by the panel.

---

## 📄 License

MIT — see [LICENSE](LICENSE) for details.
