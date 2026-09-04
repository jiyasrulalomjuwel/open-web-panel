# OWP-0014 — DB file mode too permissive
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-07
- **Root cause**: openwebpanel.db created with default (0644) perms → world-readable secrets/hashes.
- **Solution**: `chmod 0600` in `initDB` (skip `:memory:`).
- **Files**: `cmd/parentd/db.go` (`initDB`)
- **Verify**: go build passes; db file mode 0600.
- **Keywords**: db, chmod, 0600, permissions, initDB