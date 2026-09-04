# OWP-0029 — SSH access tri-state -1/0/1 mishandled
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: ssh_access used -1/0/1 but overrides and checks treated values inconsistently, so 0 (off) could still grant access.
- **Solution**: main.go validates ssh_access in {-1,0,1}; getAccountOverride reads only treat -1/absent as package default, 0 = off, 1 = on.
- **Files**: cmd/parentd/main.go
- **Verify**: go build passes
- **Keywords**: ssh, ssh_access, tri-state
