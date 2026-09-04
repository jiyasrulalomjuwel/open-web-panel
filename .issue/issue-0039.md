# OWP-0039 — PHP socket path leak in error
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Child PHP-FPM error response echoed the absolute socket path, leaking server layout.
- **Solution**: php.go logs the real path server-side and returns a generic ServiceUnavailable message.
- **Files**: cmd/parentd/php.go
- **Verify**: go build passes
- **Keywords**: php, socket, path leak
