# OWP-0047 — Rate limiter keys on spoofable client IP
- **Severity**: Low | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Rate limiting keyed on a header-derived IP that a client could spoof to reset limits.
- **Solution**: httperror.go keys on net.SplitHostPort(RemoteAddr) and ignores X-Forwarded-For; tests updated.
- **Files**: internal/shared/middleware/httperror.go, internal/shared/middleware/ratelimiter_test.go
- **Verify**: go test ./internal/shared/middleware/ passes
- **Keywords**: ratelimit, ip, spoof
