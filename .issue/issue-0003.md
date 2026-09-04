# OWP-0003 — Cron duplicate execution in same minute
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-07
- **Root cause**: no guard against running same cron job twice within the same wall-clock minute.
- **Solution**: `ranInSameMinute` checks `last_run_at`; query selects `COALESCE(last_run_at,'')` (OWP-0016); fixed cron.go indentation.
- **Files**: `cmd/parentd/cron.go`
- **Verify**: go build passes.
- **Keywords**: cron, dedupe, last_run_at, ranInSameMinute