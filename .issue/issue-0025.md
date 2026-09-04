# OWP-0025 — DB/domain quota TOCTOU race
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Count-then-insert quota checks were not atomic, allowing quota oversubscription under concurrency.
- **Solution**: main.go wraps DB-count+INSERT and domain/subdomain COUNT+INSERT in db.Begin() transactions.
- **Files**: cmd/parentd/main.go
- **Verify**: go build passes
- **Keywords**: toctou, quota, transaction
