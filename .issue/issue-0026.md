# OWP-0026 — Bandwidth recount on boot re-reads entire log
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: On restart bandwidth collector resumed from end-1MB, recounting and double-counting traffic on boot.
- **Solution**: bandwidth.go persists a high-water mark bw_progress.<domain> = inode:offset in server_config, seeding at stored offset and persisting after each poll; rotation resets to 0.
- **Files**: cmd/parentd/bandwidth.go
- **Verify**: go build passes
- **Keywords**: bandwidth, log follow, restart, high-water
