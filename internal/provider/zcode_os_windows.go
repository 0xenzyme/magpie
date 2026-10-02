package provider

import (
	"strconv"
	"sync"

	"golang.org/x/sys/windows"
)

// zcodeOSRelease is Windows' version, as Node's os.release() gives it
// (10.0.26100).
func zcodeOSRelease() string {
	v := windows.RtlGetVersion()
	return strconv.Itoa(int(v.MajorVersion)) + "." + strconv.Itoa(int(v.MinorVersion)) + "." + strconv.Itoa(int(v.BuildNumber))
}

// zcodeSystemLocale is the user's first display language (zh-CN).
var zcodeSystemLocale = sync.OnceValue(func() string {
	if l, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME); err == nil && len(l) > 0 {
		return l[0]
	}
	return ""
})
