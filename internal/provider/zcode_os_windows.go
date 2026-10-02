package provider

import (
	"strconv"

	"golang.org/x/sys/windows"
)

// zcodeOSRelease is Windows' version, as Node's os.release() gives it
// (10.0.26100).
func zcodeOSRelease() string {
	v := windows.RtlGetVersion()
	return strconv.Itoa(int(v.MajorVersion)) + "." + strconv.Itoa(int(v.MinorVersion)) + "." + strconv.Itoa(int(v.BuildNumber))
}
