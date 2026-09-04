# OWP-0049 — shq shell quoting emits wrong escape sequence
- **Severity**: Low | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: shq wrote '\'' as '\' which breaks single-quoted shell strings containing quotes.
- **Solution**: cms.go shq now writes '\'' (close-quote, escaped quote, reopen) producing a correct round-trip.
- **Files**: cmd/parentd/cms.go
- **Verify**: go build passes
- **Keywords**: shq, shell, quoting
