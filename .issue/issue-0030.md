# OWP-0030 — SMTP MX lookup SSRF
- **Severity**: Medium | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: SMTP forwarding resolved MX records and dialed them without checking for private/loopback/link-local addresses, enabling SSRF.
- **Solution**: smtpd.go iterates MX candidates and skips blocked IPs via isBlockedIP; errors when no usable MX exists.
- **Files**: cmd/parentd/smtpd.go
- **Verify**: go build passes
- **Keywords**: smtp, mx, ssrf, isBlockedIP
