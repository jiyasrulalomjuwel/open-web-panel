# OWP-0021 — SSH authorize silent no-op on first key
- **Severity**: High | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: When no authorized_keys file existed yet, appending silently failed so the first authorized key never took effect; installed flag was wrong.
- **Solution**: ssh.go always ensures authorized_keys is created and appended; the installed flag gates authorized=1 correctly.
- **Files**: cmd/parentd/ssh.go
- **Verify**: go build passes
- **Keywords**: ssh, authorized_keys, first key
