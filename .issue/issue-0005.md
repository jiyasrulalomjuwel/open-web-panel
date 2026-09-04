# OWP-0005 — Hotlink protection domain unvalidated
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-07
- **Root cause**: hotlink allowed-referer domains not validated or ownership-checked.
- **Solution**: validate with `validDomain` (main.go:106) + account owner check; `nginx -t` before reload via `testErrOr`/`or` (main.go:2010).
- **Files**: `cmd/parentd/main.go`, hotlink handler
- **Verify**: go build passes; nginx -t runs before reload.
- **Keywords**: hotlink, referer, domain, validDomain