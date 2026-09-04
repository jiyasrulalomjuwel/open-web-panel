# OWP-0004 — getClientIP trusts client-supplied proxy header
- **Severity**: High | **Status**: Resolved | **Date**: 2026-08-07
- **Root cause**: `X-Forwarded-For` trusted unconditionally, allowing spoofed source IP (rate-limit bypass).
- **Solution**: `getClientIP` only uses proxy headers when source is trusted: loopback/private or within `OWP_TRUSTED_PROXIES` CIDR list via `isTrustedProxy`.
- **Files**: `cmd/parentd/main.go`
- **Verify**: go build passes.
- **Keywords**: x-forwarded-for, ip, trust proxy, getClientIP, isTrustedProxy