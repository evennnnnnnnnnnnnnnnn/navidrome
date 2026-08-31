//go:build !unix || aix || solaris

package core

import "errors"

// openNoFollow is a no-op where the platform has no O_NOFOLLOW. Windows has no symlink
// semantics for this to defend against in the same way, and the earlier Lstat check
// still rejects links.
const openNoFollow = 0

// errFsInfoUnsupported explains why the trash pre-flight is skipped. Callers treat it
// as "cannot tell", never as "does not fit": the check is a safety net, and refusing
// every delete on a platform without statfs would be worse than the problem it solves.
var errFsInfoUnsupported = errors.New("filesystem capacity information is not available on this platform")

func fsFreeBytesOf(string) (uint64, error) { return 0, errFsInfoUnsupported }

func fsDeviceIDOf(string) (uint64, error) { return 0, errFsInfoUnsupported }
