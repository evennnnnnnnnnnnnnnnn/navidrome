//go:build unix && !aix && !solaris

package core

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// openNoFollow makes an open refuse a symlink at the final path component, so a file
// swapped for a link between validation and use is rejected instead of followed.
const openNoFollow = syscall.O_NOFOLLOW

// fsFreeBytesOf reports how much space an unprivileged process can still use on the
// filesystem holding path. Bavail rather than Bfree: the blocks reserved for root are
// not ours to spend.
func fsFreeBytesOf(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

// fsDeviceIDOf reports which filesystem path lives on, so a caller can tell a rename
// (which costs no space) from a cross-device copy (which costs the whole file).
func fsDeviceIDOf(path string) (uint64, error) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Dev), nil
}
