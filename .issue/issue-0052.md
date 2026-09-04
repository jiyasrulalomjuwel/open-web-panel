# OWP-0052 — start-parent.sh hardcoded path and stale pidfile
- **Severity**: Low | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: start-parent.sh referenced a hardcoded install dir and could fail to start if a stale pidfile existed.
- **Solution**: start-parent.sh resolves BASH_SOURCE dir and removes stale pidfiles before start.
- **Files**: start-parent.sh
- **Verify**: bash -n passes
- **Keywords**: start script, pidfile, BASH_SOURCE
