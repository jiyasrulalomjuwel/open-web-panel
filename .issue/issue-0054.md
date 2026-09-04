# OWP-0054 — childd panic on stat error in file listing
- **Severity**: Low | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: childd ignored the e.Info() error and could pass a nil FileInfo, panicking on type switch.
- **Solution**: childd logs the error and skips the entry instead of panicking.
- **Files**: cmd/childd/main.go
- **Verify**: go build passes
- **Keywords**: childd, stat, panic
