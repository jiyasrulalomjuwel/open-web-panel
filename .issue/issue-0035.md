# OWP-0035 — Email disable-forward is a no-op
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Clearing forward_to didn't persist because the empty value was skipped by an if-branch.
- **Solution**: emails.go explicitly runs UPDATE ... SET forward_to = '' when the new value is empty.
- **Files**: cmd/parentd/emails.go
- **Verify**: go build passes
- **Keywords**: email, forward, disable
