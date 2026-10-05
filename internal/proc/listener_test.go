package proc

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The process listening on a port is found, with the program it runs: here
// the test's own, on a port of its own.
func TestListeningOn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	pids, err := ListeningOn(context.Background(), ln.Addr().(*net.TCPAddr).Port)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(pids, os.Getpid()) {
		t.Fatalf("listening on the port: %v, not this process (%d)", pids, os.Getpid())
	}
	got, err := Executable(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.Executable()
	if a, b := evalPath(got), evalPath(want); a != b {
		t.Fatalf("program %q, want %q", a, b)
	}
}

func evalPath(p string) string {
	if q, err := filepath.EvalSymlinks(p); err == nil {
		return q
	}
	return p
}

// Only magpie's own programs are taken for magpie.
func TestIsMagpie(t *testing.T) {
	for path, want := range map[string]bool{
		"/Applications/Magpie.app/Contents/MacOS/magpie": true,
		"/usr/local/bin/magpie":                          true,
		`C:\Users\u\AppData\Local\magpie\magpie.exe`:     true,
		`C:\Users\u\AppData\Local\magpie\magpie.exe.old`: true,
		"/home/u/.local/bin/dial":                        true,
		"/opt/magpie-dev":                                true,
		"/usr/bin/python3":                               false,
		"/usr/bin/magpies":                               false,
		"/tmp/other-server":                              false,
		`C:\Windows\System32\svchost.exe`:                false,
		"":                                               false,
	} {
		if IsMagpie(path) != want {
			t.Errorf("IsMagpie(%q) = %v", path, !want)
		}
	}
}
