# OWP-0009 — SMTP auth no rate limiting
- **Severity**: High | **Status**: Resolved | **Date**: 2026-08-07
- **Root cause**: `SMTPBackend` accepted unlimited AUTH attempts → password brute force via 2525.
- **Solution**: throttle — 10 fails/10min per IP (`ipFail`/`ipSuccess`/`ipThrottled`), `authMu sync.Mutex`, `authState` map; `NewSession` passes `remoteIP`; over-limit → `Blocked by ipThrottled`.
- **Files**: `cmd/parentd/smtpd.go`
- **Verify**: go build passes; journal shows `Listening on :2525`.
- **Keywords**: smtp, auth, throttle, bruteforce, ipThrottled, smtpAuthAttempts