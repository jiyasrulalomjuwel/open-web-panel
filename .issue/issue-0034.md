# OWP-0034 — FTP default directory absolute path rejected
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: FTPManager sent the default dir with a leading slash, which the backend rejects.
- **Solution**: FTPManager.tsx uses directory.trim() || username (no leading slash).
- **Files**: web/src/pages/FTPManager.tsx
- **Verify**: npx tsc --noEmit passes
- **Keywords**: ftp, default dir
