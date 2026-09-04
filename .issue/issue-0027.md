# OWP-0027 — Stats parser walks entire access log forever
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Stats reading did a full log parse without a window, growing unbounded over time.
- **Solution**: stats.go now computes a 30-day totals window and 7-day recent hits with distinct-IP visits; date is YYYY-MM-DD.
- **Files**: cmd/parentd/stats.go
- **Verify**: go build passes; frontend renders
- **Keywords**: stats, access log, window
