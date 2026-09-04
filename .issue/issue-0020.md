# OWP-0020 — ACME queries wrong ssl_certificates table
- **Severity**: High | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: ACME handler read/wrote ssl_certificates but the schema table is ssl_certs, so issuance/storage silently failed.
- **Solution**: acme.go now uses ssl_certs and a lookup-failure sentinel (-int(certID)) so missing certs are detected instead of producing wrong data.
- **Files**: cmd/parentd/acme.go
- **Verify**: go build passes
- **Keywords**: acme, letsencrypt, ssl_certs
