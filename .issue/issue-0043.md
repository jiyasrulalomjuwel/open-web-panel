# OWP-0043 — Raw filesystem errors leaked to child file endpoints
- **Severity**: Low | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Child file handlers returned raw OS error strings, leaking paths.
- **Solution**: main.go adds safeFileError which logs the real error and returns a sanitized response; child handlers now use it.
- **Files**: cmd/parentd/main.go
- **Verify**: go build passes
- **Keywords**: file, errors, sanitize
