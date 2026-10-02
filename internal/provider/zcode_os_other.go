//go:build !windows

package provider

import (
	"runtime"
	"strings"
	"sync"

	"golang.org/x/sys/unix"

	"github.com/yetone/magpie/internal/proc"
)

// zcodeOSRelease is the kernel's release, as Node's os.release() gives it
// (25.2.0 on macOS 26), "" when it can't be told.
func zcodeOSRelease() string {
	var u unix.Utsname
	if unix.Uname(&u) != nil {
		return ""
	}
	return unix.ByteSliceToString(u.Release[:])
}

// zcodeSystemLocale is the Mac's region (zh_CN), the locale a terminal is
// given, for magpie opened from Finder with no LANG; "" elsewhere.
var zcodeSystemLocale = sync.OnceValue(func() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	out, err := proc.Command("defaults", "read", "-g", "AppleLocale").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
})
