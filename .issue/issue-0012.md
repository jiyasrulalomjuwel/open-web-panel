# OWP-0012 — Backups no disk space preflight
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-07
- **Root cause**: backup dump could fill disk and leave partial archives.
- **Solution**: `diskFreeBytes` via `syscall.Statfs`; abort dump if free < MIN (512MB).
- **Files**: `cmd/parentd/backups.go`
- **Verify**: go build passes.
- **Keywords**: backup, disk, statfs, preflight, diskFreeBytes