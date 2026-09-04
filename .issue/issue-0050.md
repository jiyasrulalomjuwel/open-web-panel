# OWP-0050 — Blob URLs not revoked (memory leak) / revoked too early
- **Severity**: Low | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: FileManager and api.ts created object URLs that were never revoked, and download revoked before Firefox/Safari read it.
- **Solution**: closeViewer revokes via setViewerFile updater; downloadFile and api.ts revoke after a 1s setTimeout so browsers complete the fetch.
- **Files**: web/src/pages/FileManager.tsx, web/src/lib/api.ts
- **Verify**: npx tsc --noEmit passes
- **Keywords**: blob, revoke, memory leak, download
