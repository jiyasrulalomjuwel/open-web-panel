# OWP-0051 — Stale async responses + polling interval leak in file/SSL pages
- **Severity**: Low | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: FileManager loadDir/openImageDialog could apply stale responses out of order; SSLCertificates poll recreated intervals each render.
- **Solution**: FileManager adds dirReqSeq/previewReqSeq useRef guards; media viewer revokes prior URL; SSLCertificates keys the poll effect on issuing state with stable cleanup.
- **Files**: web/src/pages/FileManager.tsx, web/src/pages/SSLCertificates.tsx
- **Verify**: npx tsc --noEmit passes
- **Keywords**: race, polling, interval, stale
