//go:build !windows

package provider

import "golang.org/x/sys/unix"

// zcodeOSRelease is the kernel's release, as Node's os.release() gives it
// (25.2.0 on macOS 26), "" when it can't be told.
func zcodeOSRelease() string {
	var u unix.Utsname
	if unix.Uname(&u) != nil {
		return ""
	}
	return unix.ByteSliceToString(u.Release[:])
}
