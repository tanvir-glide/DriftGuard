package resources

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	tests := []struct {
		name string
		mode os.FileMode
	}{
		{name: "keeps 0600", mode: 0o600},
		{name: "keeps 0640", mode: 0o640},
		{name: "keeps 0644", mode: 0o644},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "f")

			err := os.WriteFile(path, []byte("old"), tt.mode)
			if err != nil {
				t.Fatal(err)
			}

			// WriteFile is subject to umask; pin the mode explicitly
			err = os.Chmod(path, tt.mode)
			if err != nil {
				t.Fatal(err)
			}

			err = WriteFileAtomic(path, []byte("new"))
			if err != nil {
				t.Fatalf("WriteFileAtomic: %v", err)
			}

			got, _ := os.ReadFile(path)
			if string(got) != "new" {
				t.Errorf("content = %q, want %q", got, "new")
			}

			info, _ := os.Stat(path)
			if info.Mode().Perm() != tt.mode {
				t.Errorf("mode = %v, want %v", info.Mode().Perm(), tt.mode)
			}

			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Errorf("leftover files in dir: %v", entries)
			}
		})
	}
}

func TestWriteFileAtomic_FollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")

	err := os.WriteFile(target, []byte("old"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	err = os.Symlink(target, link)
	if err != nil {
		t.Fatal(err)
	}

	err = WriteFileAtomic(link, []byte("new"))
	if err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	info, _ := os.Lstat(link)
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink was replaced by a regular file")
	}

	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Errorf("target content = %q, want %q", got, "new")
	}
}

func TestWriteFileAtomic_MissingFile(t *testing.T) {
	err := WriteFileAtomic(filepath.Join(t.TempDir(), "nope"), []byte("x"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
