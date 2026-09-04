# OWP-0023 — Hotlink protection page broken in both directions
- **Severity**: High | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: HotlinkProtection.tsx used wrong response shapes (r.id guard, enabled as non-bool) so load/save both failed.
- **Solution**: Fixed bool + array shapes, removed stale r.id guard, save sends {enabled: Boolean(...), allowed_domains: string[]}.
- **Files**: web/src/pages/HotlinkProtection.tsx
- **Verify**: npx tsc --noEmit passes
- **Keywords**: hotlink, referer, frontend
