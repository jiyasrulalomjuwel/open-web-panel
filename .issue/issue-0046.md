# OWP-0046 — Audit log non-UTC timestamps and wrong actor for child scope
- **Severity**: Low | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Audit wrote local-time and always labeled actor admin even for child-scope actions.
- **Solution**: audit.go and handlers.go use UTC; auditLog detects Scope=='child' → actor_type account + AccountID.
- **Files**: internal/shared/audit/audit.go, cmd/parentd/handlers.go
- **Verify**: go build passes
- **Keywords**: audit, utc, actor, child
