# Regression Check — 2026-08-07

Deployed: `/opt/openwebpanel/app` (parentd active, childd container namespace).
Source: `/tmp/opencode/owp-src`.

## Result: PASS — no regressions

| ID | Fix | Where | Verified |
|----|-----|-------|----------|
| OWP-0001 | FTP domain ownership | ftp.go:114 | source |
| OWP-0002 | SSH ssh_access enforcement | ssh.go:121 | source |
| OWP-0003 | Cron dedup ranInSameMinute | cron.go:326 | source |
| OWP-0004 | Trusted proxy getClientIP | main.go:2127 | source |
| OWP-0005 | Hotlink validDomain | main.go:113 | source |
| OWP-0006 | DNS update validation | dns handler | source |
| OWP-0007 | jsonError masks 5xx | main.go:94 | source |
| OWP-0008 | Login timing dummy hash | shared/auth/jwt.go:111 | source |
| OWP-0009 | SMTP auth throttle | smtpd.go:36,90 | source |
| OWP-0010 | dbadmin loopback + sanitize | cmd/dbadmin | source |
| OWP-0011 | ACME 1h cooldown | acme.go:33 | source |
| OWP-0012 | Backup disk preflight | backups.go:996 | source |
| OWP-0013 | File write .owp/.trash block | main.go | source |
| OWP-0014 | DB file 0600 | main.go:136 + runtime | **runtime: 0600 on /opt/openwebpanel/data/openwebpanel.db** |
| OWP-0015 | SMTP STARTTLS cert fallback | smtpd.go | **runtime: journal "SMTP TLS: falling back to ...juwel.collage.edu.pl.crt", Listening on :2525** |
| OWP-0016 | Cron COALESCE last_run_at | cron.go:24,311 | source |

## Runtime smoke tests
- `openwebpanel.service` = active; nginx -t + reload clean at each startup.
- Child login (kept test account `password`/`password`) → access token OK.
- `GET /child/account` returns `disk_limit_mb`, `bandwidth_used_mb` (frontend M15 fix confirmed against live API).
- Admin bundle served (`admin-DqHX8_pO.js`), `ip-api.com` removed (0 hits), `disk_limit_mb` present in child bundle.
- Login throttle live: repeated bad admin logins → HTTP 429 (OWP-0009-adjacent login lockout working).

## Notes
- Deployed `smtpd.go` is byte-identical to source (diff PASS).
- `/opt/openwebpanel/app/openwebpanel.db` (0644, unused leftover) is NOT opened by the running process; the live DB is `/opt/openwebpanel/data/openwebpanel.db` (0600). Leftover can be removed but is harmless.
- Admin login password is unknown (customer credential) — not brute-forced; deliberately left alone.
