# OWP-0053 — Cron DOW 7 not normalized and quota check race
- **Severity**: Low | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Sunday as 7 wasn't normalized to 0 in ranges/singles; cron quota count+insert not atomic.
- **Solution**: cron.go normalizes dow 7→0 in single and range matches and wraps quota check+insert in db.Begin().
- **Files**: cmd/parentd/cron.go
- **Verify**: go build passes
- **Keywords**: cron, dow, 7, quota
