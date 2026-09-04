# OWP-0010 — dbadmin binds all interfaces
- **Severity**: High | **Status**: Resolved | **Date**: 2026-08-07
- **Root cause**: dbadmin HTTP bind = 0.0.0.0, admin db API reachable on network.
- **Solution**: bind `127.0.0.1:`; added `sanitizeDBName` for `api/tables/`.
- **Files**: `cmd/dbadmin/*`
- **Verify**: go build passes; process binds loopback only.
- **Keywords**: dbadmin, bind, loopback, sanitizeDBName, tables