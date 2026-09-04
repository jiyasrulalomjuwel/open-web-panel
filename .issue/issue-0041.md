# OWP-0041 — Missing DB indexes on hot query columns
- **Severity**: Low | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Several frequently-filtered columns had no indexes, slowing account/domain/audit lookups.
- **Solution**: 9 CREATE INDEX IF NOT EXISTS statements appended to migrations for accounts, refresh_tokens, audit_log, login_attempts, blocked_ips, domains, child_databases, db_users.
- **Files**: cmd/parentd/main.go
- **Verify**: go build passes; migrations apply on existing DB
- **Keywords**: index, migration, performance
