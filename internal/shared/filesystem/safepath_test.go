package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafePath(t *testing.T) {
	base := t.TempDir()

	if err := os.MkdirAll(filepath.Join(base, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "sub", "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "ok.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "nested"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "evil-link")); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}
	if err := os.Symlink(filepath.Join(base, "sub"), filepath.Join(base, "inner-link")); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}
	// Dangling symlinks: the link target (or a component of it) doesn't exist yet.
	if err := os.Symlink(filepath.Join(base, "sub", "future.txt"), filepath.Join(base, "inner-dangling")); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "nested", "secret.txt"), filepath.Join(base, "evil-dangling")); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "nonexistent-dir"), filepath.Join(base, "evil-dangling-dir")); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}

	tests := []struct {
		name     string
		userPath string
		want     string
		wantErr  bool
	}{
		{name: "existing file", userPath: "sub/file.txt", want: filepath.Join(base, "sub", "file.txt")},
		{name: "existing dir", userPath: "sub", want: filepath.Join(base, "sub")},
		{name: "root file", userPath: "ok.txt", want: filepath.Join(base, "ok.txt")},
		{name: "new file in existing dir", userPath: "sub/newfile.txt", want: filepath.Join(base, "sub", "newfile.txt")},
		{name: "new file at base root", userPath: "newfile.txt", want: filepath.Join(base, "newfile.txt")},
		{name: "dot", userPath: ".", want: base},
		{name: "empty", userPath: "", want: base},
		{name: "whitespace trimmed", userPath: "  sub  ", want: filepath.Join(base, "sub")},
		{name: "trailing slash", userPath: "sub/", want: filepath.Join(base, "sub")},
		{name: "symlink inside base", userPath: "inner-link", want: filepath.Join(base, "inner-link")},
		{name: "dangling symlink inside base", userPath: "inner-dangling", want: filepath.Join(base, "inner-dangling")},
		{name: "absolute path inside base", userPath: filepath.Join(base, "sub", "file.txt"), want: filepath.Join(base, "sub", "file.txt")},
		{name: "absolute dir inside base", userPath: filepath.Join(base, "sub"), want: filepath.Join(base, "sub")},
		{name: "absolute path equals base", userPath: base, want: base},
		{name: "parent traversal", userPath: "..", wantErr: true},
		{name: "traversal existing target", userPath: "../", wantErr: true},
		{name: "traversal to outside dir", userPath: "../", wantErr: true},
		{name: "traversal nested", userPath: "sub/../..", wantErr: true},
		{name: "traversal to existing outside dir", userPath: filepath.Join("..", filepath.Base(outside)), wantErr: true},
		{name: "traversal to nonexistent outside file", userPath: filepath.Join("..", filepath.Base(outside), "nested", "secret.txt"), wantErr: true},
		{name: "symlink escape", userPath: "evil-link", wantErr: true},
		{name: "symlink escape nested", userPath: "evil-link/nested/secret.txt", wantErr: true},
		{name: "dangling symlink escape (outside target)", userPath: "evil-dangling", wantErr: true},
		{name: "dangling symlink escape nested", userPath: "evil-dangling/sub", wantErr: true},
		{name: "dangling symlink escape to nonexistent dir", userPath: "evil-dangling-dir", wantErr: true},
		{name: "absolute path", userPath: "/etc/passwd", wantErr: true},
		{name: "absolute traversal", userPath: "/../../etc/passwd", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafePath(base, tt.userPath)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("SafePath(%q) = %q, want error", tt.userPath, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("SafePath(%q) unexpected error: %v", tt.userPath, err)
			}
			if got != tt.want {
				t.Errorf("SafePath(%q) = %q, want %q", tt.userPath, got, tt.want)
			}
		})
	}
}
