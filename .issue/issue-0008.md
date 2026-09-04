# OWP-0008 — Login timing oracle reveals account existence
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-07
- **Root cause**: missing account returned instantly vs bcrypt verify delay; user enumeration via timing.
- **Solution**: `CheckPasswordTimingSafe` compares against a dummy bcrypt hash when user row not found (`dummyHash` in `internal/shared/auth/jwt.go`), called from main.go login.
- **Files**: `internal/shared/auth/jwt.go`, `cmd/parentd/main.go`
- **Verify**: go build passes.
- **Keywords**: timing, enum, bcrypt, dummy, login, CheckPasswordTimingSafe