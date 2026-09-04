# OWP-0048 — RequestID not sanitized before logging/use
- **Severity**: Low | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Client-supplied RequestID could contain arbitrary characters (log injection/CRLF).
- **Solution**: httperror.go sanitizes via regex ^[A-Za-z0-9._:-]+$ and falls back to a generated uuid.
- **Files**: internal/shared/middleware/httperror.go
- **Verify**: go build passes
- **Keywords**: requestid, sanitize, log injection
