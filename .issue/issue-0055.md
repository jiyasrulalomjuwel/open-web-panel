# OWP-0055 — JWT tokens lack issuer/audience enforcement
- **Severity**: Low | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: JWTs were signed without iss/aud and validation didn't check them, so tokens from a sibling service could be accepted.
- **Solution**: jwt.go adds JWTIssuer/JWTAudience constants, sets both on signing, and ValidateToken uses jwt.WithIssuer + jwt.WithAudience; 3 new tests.
- **Files**: internal/shared/auth/jwt.go, internal/shared/auth/jwt_test.go
- **Verify**: go test ./internal/shared/auth/ passes
- **Keywords**: jwt, issuer, audience, validation
