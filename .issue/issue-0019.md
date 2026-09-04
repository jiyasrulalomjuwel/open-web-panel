# OWP-0019 — SafePath dangling-symlink escape
- **Severity**: High | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: A dangling symlink under the account home could resolve outside the allowed tree because containment was only checked on the pre-resolution path.
- **Solution**: filesystem.SafePath resolves dangling-symlink targets via Lstat+Readlink and a recursive containment check; inner dangling links still allowed, escaping ones rejected. Added safepath tests.
- **Files**: internal/shared/filesystem/filesystem.go, internal/shared/filesystem/safepath_test.go
- **Verify**: go test ./internal/shared/filesystem/ passes
- **Keywords**: safepath, symlink, escape, containment
