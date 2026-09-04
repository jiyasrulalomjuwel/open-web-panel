# OpenWebPanel

A lightweight, self-hosted web hosting control panel built with **Go** and **React**. Give every customer their own isolated hosting account — websites, databases, email, SSL, FTP, backups — all from a clean web interface.

![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)
![React](https://img.shields.io/badge/React-18.3-61DAFB?logo=react)
![Docker](https://img.shields.io/badge/Docker-required-2496ED?logo=docker)
![License](https://img.shields.io/badge/license-MIT-blue)

---

## 🚀 Install in one command

Copy, paste on any Linux server as root, done in **~10–15 minutes**:

```bash
curl -fsSL https://raw.githubusercontent.com/jiyasrulalomjuwel/open-web-panel/main/install.sh | sudo bash
```

That's the recommended path — Docker is installed if missing, the image is
built, and the admin password is printed at the end.

### Option B — pure Docker, one command

Same result, no installer script — clone, set two secrets, bring it up:

```bash
git clone https://github.com/jiyasrulalomjuwel/open-web-panel.git && cd open-web-panel && \
  OWP_JWT_SECRET="$(openssl rand -hex 32)" \
  OWP_ADMIN_PASSWORD='Choose-A-Strong-Password' \
  sudo -E docker compose -f docker-compose.prod.yml up -d --build
```

Then open `http://your-server:2086` and log in as `admin` with the password
you chose. Data persists in the `owp_data` and `owp_homes` volumes.

The installer checks for Docker (installing it if missing), asks for your admin
credentials, builds the self-contained image and starts everything. Your admin
password is printed at the end (and saved in `/opt/openwebpanel/.env`).

| What | Where |
|---|---|
| **Admin Panel** — manage accounts, packages, settings | `http://your-server:2086` |
| **User Panel** — your clients manage their sites | `http://your-server:2082` |
| **Websites** | `http://your-server` (ports 80/443) |
| Mail (SMTP) | port 2525 |
| FTP | port 21 |

Requirements: any modern Linux · Docker (auto-installed) · 2 GB RAM · 10 GB+ free disk · x86_64 or ARM64 · root access.

---

## ✨ Feature tour

| Category | What you get |
|---|---|
| **Hosting Accounts** | Create/manage accounts with quotas (disk, bandwidth, RAM, databases, email, FTP) and per-user feature gates |
| **Access Control** | Allow or block Files, Emails, FTP, Databases, Backups, Cron per user — or set defaults per package |
| **Account Details** | Click any user for usage, limits, package, security, resources and full history |
| **File Manager** | Browse, upload, edit, extract, trash/restore — right in the browser |
| **Databases** | MariaDB user databases + one-click phpMyAdmin login |
| **Domains** | Addon, parked and subdomains with auto-generated Nginx configs; secure any domain with one click right after adding it |
| **Email** | Mailboxes, built-in webmail, SMTP server, forwards, daily send limits |
| **SSL / HTTPS** | Free Let's Encrypt certificates, honest status (self-signed is never disguised), one-click HTTPS redirect + HSTS |
| **WordPress Installer** | One-click WordPress with database, admin user and optional SSL |
| **FTP** | Real FTP server with per-account isolated logins |
| **Backups** | Manual + scheduled full/files/database backups with restore |
| **Cron Jobs** | Per-user scheduled commands with presets |
| **Redirects & Error Pages** | Per-domain 301/302 redirects and custom error pages, served by Nginx |
| **Bandwidth & Stats** | Per-account traffic metering and site statistics |
| **API Tokens** | Long-lived keys so your own software can create/suspend accounts and more — with an in-app integration guide |
| **Cluster Nodes** | K3s-powered workers: one-click dependency install, health checks with auto-fix, live CPU/RAM/disk per node |

---

## 📦 How it works

Everything runs inside **one Docker image** — Nginx + PHP-FPM + MariaDB + the Go backend + both web UIs. Nothing is installed on your host except Docker itself, so installs are identical on every machine. Your data lives in two Docker volumes (`owp_data`, `owp_homes`) and survives updates.

```
visitors ──▶ Nginx :80/:443 ──▶ customer sites (PHP-FPM / MariaDB)
admins   ──▶ Nginx :2086 ──▶ parentd :9000 ──▶ SQLite (panel DB)
customers──▶ Nginx :2082 ──▶ parentd :9001 ──▶ per-account homes
```

### Custom install

```bash
# Non-interactive (CI, scripts)
sudo OWP_ADMIN_USERNAME=admin OWP_ADMIN_PASSWORD='secret' bash install.sh

# Custom panel ports / offline bundle / skip firewall
sudo OWP_PANEL_PORT=8443 OWP_BUNDLE_DIR=/data/bundle bash install.sh
sudo OWP_SKIP_FIREWALL=true bash install.sh
```

### Offline / air-gapped install

```bash
# 1. On a connected machine with Docker:
sudo bash bundle/build-bundle.sh     # produces bundle/openwebpanel.tar

# 2. Copy bundle/ over, then on the target:
sudo OWP_BUNDLE_DIR=/path/to/bundle bash install.sh
```

---

## 🔧 Manage

```bash
docker compose -f /opt/openwebpanel/docker-compose.yml ps        # status
docker compose -f /opt/openwebpanel/docker-compose.yml restart   # restart
docker compose -f /opt/openwebpanel/docker-compose.yml logs -f   # live logs
cat /opt/openwebpanel/.env   # secrets (root only)
```

---

## 🛠 Develop

```bash
git clone https://github.com/jiyasrulalomjuwel/open-web-panel.git
cd open-web-panel
go build -o bin/parentd ./cmd/parentd/          # backend (CGO on: sqlite)
cd web && npm install && npm run build:all && cd ..
sudo bash dev.sh                                # backend :9000/:9001 + vite :5173/:5174
```

Project layout: `cmd/parentd` (API, SMTP, FTP, cron, SSL, scheduler) · `cmd/childd` (file server) · `internal/shared` (auth, db, audit, …) · `web/src/{pages,components,lib}` (35 pages, admin + user SPAs) · `deploy/{docker,k3s}` (container + cluster scripts) · `install.sh` (the one-command installer).

---

## 🛟 Troubleshooting

| Symptom | Fix |
|---|---|
| Panel not reachable after install | `docker compose -f /opt/openwebpanel/docker-compose.yml logs --tail 60` — the installer checks `/healthz` before finishing |
| Checksum mismatch | Delete the archive in `bundle/` or `/tmp` and rerun |
| Slow/flaky network | Use the offline bundle path above |
| No compose plugin | `install.sh` installs Docker + the compose plugin automatically |

---

## 📄 License

MIT — see [LICENSE](LICENSE) for details.
