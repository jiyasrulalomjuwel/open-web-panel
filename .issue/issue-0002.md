# OWP-0002 — SSH access flag not enforced
- **Severity**: High | **Status**: Resolved | **Date**: 2026-08-07
- **Root cause**: `ssh_access=0` ignored; any account could authorize SSH.
- **Solution**: `sshAccessForAccount` checks `ssh_access` (legacy NULL → COALESCE 1) on `POST /{id}/authorize`.
- **Files**: `cmd/parentd/ssh.go`
- **Verify**: go build passes.
- **Keywords**: ssh, ssh_access, authorize