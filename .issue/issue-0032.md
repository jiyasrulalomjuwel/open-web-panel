# OWP-0032 — cms isPathWithin args reversed
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: cms delete/install containment passed arguments reversed, so it could allow deleting directories outside the home.
- **Solution**: Call site fixed to isPathWithin(homeDir, installPath).
- **Files**: cmd/parentd/cms.go
- **Verify**: go build passes
- **Keywords**: cms, isPathWithin, containment
