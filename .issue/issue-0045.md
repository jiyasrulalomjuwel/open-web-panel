# OWP-0045 — Unescaped secrets in systemd EnvironmentFile
- **Severity**: Low | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: install scripts wrote JWT/DB/admin secrets into .env without escaping, breaking systemd parsing on special chars.
- **Solution**: env_escape() in stages.sh and esc() in install-docker.sh escape backslash and double-quote for EnvironmentFile.
- **Files**: install/stages.sh, install-docker.sh
- **Verify**: bash -n passes
- **Keywords**: install, secrets, escaping, systemd
