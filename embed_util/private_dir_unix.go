//go:build !windows

package embed_util

import (
	"fmt"
	"os"
	"syscall"
)

// ensurePrivateDir fails unless dir is a real directory owned by the effective user, and removes group and other
// write permission, which a umask such as 002 leaves on newly created directories.
func ensurePrivateDir(dir string) error {
	st, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", dir, err)
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !st.IsDir() || !ok || int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("refusing to extract to %s: not a directory owned by the current user", dir)
	}
	if st.Mode().Perm()&0o022 != 0 {
		err = os.Chmod(dir, st.Mode().Perm()&^0o022)
		if err != nil {
			return fmt.Errorf("failed to restrict permissions of %s: %w", dir, err)
		}
	}
	return nil
}
