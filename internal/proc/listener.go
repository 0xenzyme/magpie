package proc

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
)

// The process listening on a port, and its program: how the GUI finds the
// older magpie that keeps the gateway's port and quits it, when the user
// asks it to (leslie_luo on Discord: the page said an older magpie served
// the gateway, with no way to find it and quit it).

// ErrDenied is a process this user may not end: another user's, or one
// run as administrator.
var ErrDenied = errors.New("not allowed to end it")

// ListeningOn is the ids of the processes listening on TCP port, that this
// user can see: lsof on a Mac, /proc on Linux, Get-NetTCPConnection on
// Windows. Another user's process may be missing.
func ListeningOn(ctx context.Context, port int) ([]int, error) { return listeningOn(ctx, port) }

// Executable is the path of the program process pid runs, as the system
// says it: the one it was started from, also when that file has been
// replaced since (an update).
func Executable(pid int) (string, error) {
	p, err := executable(pid)
	if err != nil {
		return "", err
	}
	// Linux names a replaced file so
	return strings.TrimSuffix(p, " (deleted)"), nil
}

// Terminate asks process pid to end: SIGTERM on Unix, which a magpie quits
// on; on Windows it is ended at once (nothing else reaches a tray app).
// ErrDenied when this user may not.
func Terminate(pid int) error { return terminate(pid) }

// Kill ends process pid at once. ErrDenied when this user may not.
func Kill(pid int) error { return kill(pid) }

// IsMagpie says whether the program at path is a magpie: magpie, its
// magpie-* builds or dial, its name before; with .exe, and Windows' .old
// for one replaced by an update while it ran.
func IsMagpie(path string) bool {
	if path == "" {
		return false
	}
	n := strings.ToLower(filepath.Base(filepath.Clean(strings.ReplaceAll(path, `\`, "/"))))
	n = strings.TrimSuffix(n, ".old")
	n = strings.TrimSuffix(n, ".new")
	n = strings.TrimSuffix(n, ".exe")
	for _, name := range []string{"magpie", "dial"} {
		if n == name || strings.HasPrefix(n, name+"-") || strings.HasPrefix(n, name+"_") {
			return true
		}
	}
	return false
}
