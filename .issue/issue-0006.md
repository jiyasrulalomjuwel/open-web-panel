# OWP-0006 — DNS update lacks validation
- **Severity**: High | **Status**: Resolved | **Date**: 2026-08-07
- **Root cause**: update endpoint accepted arbitrary type/name/value/TTL.
- **Solution**: re-validate on update — type whitelist, name<=255, value<=1000, TTL 60..86400.
- **Files**: `cmd/parentd/dns.go` (or main.go dns handler)
- **Verify**: go build passes.
- **Keywords**: dns, validation, ttl, record