# OWP-0037 — Submissions metadata type crash
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: metadata could be an object or string depending on API response; rendering assumed one shape and crashed.
- **Solution**: Submissions.tsx types metadata as Record<string,string|number|boolean|null>|string|null, stringifies for search, renders safely.
- **Files**: web/src/pages/Submissions.tsx
- **Verify**: npx tsc --noEmit passes
- **Keywords**: submissions, metadata, crash
