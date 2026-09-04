# OWP-0016 — Cron zero-value last_run_at mismatch
- **Severity**: Medium | **Status**: Resolved-with-0016 / part of OWP-0003 | **Date**: 2026-08-07
- **Root cause**: cron query selected raw `last_run_at` which could be SQL NULL/zero value, breaking dedupe comparison.
- **Solution**: select `COALESCE(last_run_at,'')` so dedupe always has a comparable string.
- **Files**: `cmd/parentd/cron.go`
- **Verify**: go build passes.
- **Keywords**: cron, last_run_at, coalesce, null