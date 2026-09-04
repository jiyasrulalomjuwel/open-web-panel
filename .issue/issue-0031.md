# OWP-0031 — wp-config.php world-readable
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: CMS installs wrote wp-config.php with default perms, exposing DB credentials.
- **Solution**: cms.go writes wp-config with 0600 at both write sites.
- **Files**: cmd/parentd/cms.go
- **Verify**: go build passes
- **Keywords**: wp-config, permissions, 0600
