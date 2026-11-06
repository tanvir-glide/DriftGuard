package resources

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// WriteFileAtomic replaces the file at path with data. It writes a temp
// file in the same directory, syncs it, then renames it over the target, so
// readers see either the old or the new content and never a partial write.
// Permissions and ownership of the existing file are preserved. The file
// must already exist. A symlink is followed, so the link itself survives
func WriteFileAtomic(path string, data []byte) (err error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", path, err)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("stat %s: %w", resolved, err)
	}

	dir := filepath.Dir(resolved)

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(resolved)+".driftguard-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}

	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	err = tmp.Chmod(info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		err = tmp.Chown(int(st.Uid), int(st.Gid))
		if err != nil {
			return fmt.Errorf("chown temp file: %w", err)
		}
	}

	_, err = tmp.Write(data)
	if err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	err = tmp.Sync()
	if err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}

	err = tmp.Close()
	if err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	err = os.Rename(tmp.Name(), resolved)
	if err != nil {
		return fmt.Errorf("rename over %s: %w", resolved, err)
	}

	return syncDir(dir)
}

// syncDir flushes the directory entry so the rename survives a crash
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open dir %s: %w", dir, err)
	}
	defer d.Close()

	err = d.Sync()
	if err != nil {
		return fmt.Errorf("sync dir %s: %w", dir, err)
	}

	return nil
}
