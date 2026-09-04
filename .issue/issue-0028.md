# OWP-0028 — nginx vhost injection via account domain update
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: PUT /accounts/{id} accepted arbitrary domain strings which were interpolated into nginx vhost files, allowing config injection.
- **Solution**: main.go runs validDomain() before persisting a domain update.
- **Files**: cmd/parentd/main.go
- **Verify**: go build passes; validDomain allows real domains
- **Keywords**: nginx, vhost, injection, validDomain
