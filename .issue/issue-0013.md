# OWP-0013 — File write allows .owp/.trash
- **Severity**: High | **Status**: Resolved | **Date**: 2026-08-07
- **Root cause**: file write endpoint could overwrite internal `.owp`/`.trash` paths (config/backup data) via path traversal.
- **Solution**: block `.owp`/`.trash` targets in `r.Post("/write")`.
- **Files**: `cmd/parentd/main.go`
- **Verify**: go build passes.
- **Keywords**: file, write, .owp, .trash, traversal