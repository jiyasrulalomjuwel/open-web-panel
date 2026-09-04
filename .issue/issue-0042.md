# OWP-0042 — isUsernameBlocked prefix match false positives
- **Severity**: Low | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Blocked-username check used LIKE '...%' matching any username with the prefix.
- **Solution**: main.go uses exact reason equality: reason = 'Username blocked: '+username.
- **Files**: cmd/parentd/main.go
- **Verify**: go build passes
- **Keywords**: username, blocked, exact-match
