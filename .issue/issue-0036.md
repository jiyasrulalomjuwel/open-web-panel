# OWP-0036 — Redirect type key mismatch
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Redirects frontend sent/read a different JSON key than the backend expects for the redirect type.
- **Solution**: Redirects.tsx sends type and rebuilds the row from the form after create.
- **Files**: web/src/pages/Redirects.tsx
- **Verify**: npx tsc --noEmit passes
- **Keywords**: redirect, type, key
