package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SafePath resolves a user-supplied path relative to a base directory and
// ensures the result doesn't escape the base (path traversal protection).
func SafePath(base, userPath string) (string, error) {
	// Clean the user input
	cleanPath := filepath.Clean(strings.TrimSpace(userPath))
	// If it's an absolute path, strip the leading slash to make it relative
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	if cleanPath == "" {
		cleanPath = "."
	}
	fullPath := filepath.Join(base, cleanPath)
	fullPath = filepath.Clean(fullPath)

	// Resolve symlinks
	realPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			// For new files/dirs that don't exist yet, check the parent
			parentPath := filepath.Dir(fullPath)
			realParent, perr := filepath.EvalSymlinks(parentPath)
			if perr != nil {
				return "", fmt.Errorf("path traversal detected: %w", perr)
			}
			realBase, _ := filepath.EvalSymlinks(base)
			if !strings.HasPrefix(realParent, realBase) {
				return "", fmt.Errorf("path traversal detected")
			}
			return fullPath, nil
		}
		return "", fmt.Errorf("resolve path: %w", err)
	}

	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("resolve base: %w", err)
	}
	if !strings.HasPrefix(realPath, realBase) {
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
