# OWP-0033 — Submissions dead ownership check
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Submissions endpoints checked ownership against a claim that was never populated, leaving them effectively unauthenticated against account ownership.
- **Solution**: submissions.go uses getClaims + account existence validation on POST, and scope-aware 404/DELETE.
- **Files**: cmd/parentd/submissions.go
- **Verify**: go build passes
- **Keywords**: submissions, ownership, claims
