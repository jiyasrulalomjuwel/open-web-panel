# OWP-0011 — ACME issuance no rate limit
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-07
- **Root cause**: letsencrypt cert issuance per account unbounded → spurious certs/rate hits.
- **Solution**: per-account 1h cooldown via `acmeCooldownPassed` + `acmeLockoutByAccount`, guarded by `sync.Mutex`.
- **Files**: `cmd/parentd/acme.go`
- **Verify**: go build passes.
- **Keywords**: acme, letsencrypt, rate, cooldown, acmeCooldownPassed