# OWP-0001 — FTP domain ownership not validated
- **Severity**: High | **Status**: Resolved | **Date**: 2026-08-07
- **Root cause**: FTP domain not checked to belong to account before remove (`DEL`).
- **Solution**: Owner check before DELETE + idempotent dedup.
- **Files**: `cmd/parentd/ftp.go`
- **Verify**: go build passes.
- **Keywords**: ftp, ownership, authz