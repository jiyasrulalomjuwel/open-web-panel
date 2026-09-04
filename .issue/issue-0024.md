# OWP-0024 — Webmail message detail wiped on refresh
- **Severity**: High | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Webmail.tsx lost the selected-message detail whenever the poll/refresh cycle re-rendered.
- **Solution**: Webmail.tsx preserves detail state on refresh.
- **Files**: web/src/pages/Webmail.tsx
- **Verify**: npx tsc --noEmit passes
- **Keywords**: webmail, detail, refresh
