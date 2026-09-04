# OWP-0038 — Accounts pagination desync
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Accounts page could request a page beyond totalPages after deletions/filtering, returning empty.
- **Solution**: Accounts.tsx clamps page to Math.max(1, totalPages) via useEffect.
- **Files**: web/src/pages/Accounts.tsx
- **Verify**: npx tsc --noEmit passes
- **Keywords**: accounts, pagination, clamp
