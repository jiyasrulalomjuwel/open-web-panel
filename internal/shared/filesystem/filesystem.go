package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SafePath resolves a user-supplied path relative to a base directory and
// ensures the result doesn't escape the base (path traversal protection).
//
// userPath may be either home-relative ("public_html", "sub/dir") or an
// absolute path that already lives inside base (e.g. the absolute doc_root
// handed off by the domains page). Absolute paths outside base are treated as
// base-relative and are still contained by the checks below.
func SafePath(base, userPath string) (string, error) {
	cleanPath := filepath.Clean(strings.TrimSpace(userPath))
	if filepath.IsAbs(cleanPath) {
		if rel, err := filepath.Rel(base, cleanPath); err == nil &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			// Already inside base — use it as-is; the containment checks below
			// still run against the real (symlink-resolved) path. Note: we
			// must not pass this through filepath.Join (it would concatenate
			// the two absolute paths).
			return checkContainment(base, filepath.Clean(cleanPath))
		}
		cleanPath = strings.TrimPrefix(cleanPath, "/")
	} else {
		cleanPath = strings.TrimPrefix(cleanPath, "/")
	}
	if cleanPath == "" {
		cleanPath = "."
	}
	fullPath := filepath.Join(base, cleanPath)
	fullPath = filepath.Clean(fullPath)
	return checkContainment(base, fullPath)
}

// checkContainment verifies that fullPath stays inside base (symlink-aware)
// and returns it. Used by SafePath for both relative and absolute inputs.
func checkContainment(base, fullPath string) (string, error) {
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("resolve base: %w", err)
	}
	realBaseWithSep := realBase + string(filepath.Separator)

	// Resolve symlinks
	realPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("resolve path: %w", err)
		}
		// For new files/dirs that don't exist yet, check the parent
		parentPath := filepath.Dir(fullPath)
		realParent, perr := filepath.EvalSymlinks(parentPath)
		if perr != nil {
			return "", fmt.Errorf("path traversal detected: %w", perr)
		}
		if realParent != realBase && !strings.HasPrefix(realParent, realBaseWithSep) {
			return "", fmt.Errorf("path traversal detected")
		}

		// The full path doesn't exist, but its final component may itself be a
		// dangling symlink whose target escapes base. A later write would
		// create the file through the link, outside the account home, so
		// resolve the link target (absolute directly, relative joined onto the
		// real parent) and verify that resolved target stays within base too.
		if fi, lerr := os.Lstat(fullPath); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			linkTarget, rerr := os.Readlink(fullPath)
			if rerr != nil {
				return "", fmt.Errorf("path traversal detected: %w", rerr)
			}
			var resolved string
			if filepath.IsAbs(linkTarget) {
				resolved = filepath.Clean(linkTarget)
			} else {
				resolved = filepath.Join(realParent, linkTarget)
			}
			if _, cerr := checkContainment(base, resolved); cerr != nil {
				return "", fmt.Errorf("path traversal detected")
			}
		}
		return fullPath, nil
	}

	if realPath != realBase && !strings.HasPrefix(realPath, realBaseWithSep) {
		return "", fmt.Errorf("path traversal detected")
	}
	return fullPath, nil
}

// EnsureDir creates a directory if it doesn't exist.
func EnsureDir(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// HumanSize returns a human-readable file size.
func HumanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
