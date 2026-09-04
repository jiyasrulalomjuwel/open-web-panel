# OWP-0022 — phpMyAdmin frontend ignores backend url and uses admin-only /pma path
- **Severity**: High | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Databases.tsx hardcoded /pma/<token>/ which only exists on the admin router; the backend already returned a proper url for child users.
- **Solution**: Frontend now uses the backend-provided url with /pma/<token>/ as fallback.
- **Files**: web/src/pages/Databases.tsx
- **Verify**: npx tsc --noEmit passes
- **Keywords**: phpmyadmin, pma, sso, url
