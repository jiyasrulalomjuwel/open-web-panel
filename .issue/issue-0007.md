# OWP-0007 — jsonError leaks internal 5xx details
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-07
- **Root cause**: 5xx responses returned raw error strings to client, leaking internals.
- **Solution**: `jsonError` masks 5xx to "an internal error occurred"; raw error logged server-side.
- **Files**: `cmd/parentd/main.go` (`jsonError`)
- **Verify**: go build passes.
- **Keywords**: jsonError, 5xx, mask, leak