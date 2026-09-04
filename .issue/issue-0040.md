# OWP-0040 — CMS installs no timeout or dedupe
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: CMS installs could hang forever and double-submit concurrently, wasting resources.
- **Solution**: cms.go runs mysql/curl/tar/wp under a 10-minute context timeout and dedupes via cmsInstallsInFlight + DB status check returning Conflict.
- **Files**: cmd/parentd/cms.go
- **Verify**: go build passes
- **Keywords**: cms, timeout, dedupe, concurrency
