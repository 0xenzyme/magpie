package main

import (
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/settings"
)

// The serve banner says the gateway takes any key only from this machine
// while that is true: MAGPIE_ADDR on every interface (a Docker image) and
// not shared from Settings is open to anyone who reaches it.
func TestKeyNote(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, c := range []struct{ addr, want string }{
		{"", "only listens on localhost"},
		{"127.0.0.1:3426", "only listens on localhost"},
		{"localhost:3426", "only listens on localhost"},
		{"[::1]:3426", "only listens on localhost"},
		{"0.0.0.0:3425", "anyone who reaches it"},
		{":3425", "anyone who reaches it"},
		{"192.168.1.5:3425", "anyone who reaches it"},
	} {
		t.Setenv("MAGPIE_ADDR", c.addr)
		if n := keyNote(); !strings.Contains(n, c.want) {
			t.Errorf("MAGPIE_ADDR=%q: %q", c.addr, n)
		}
	}
	if err := settings.Save(settings.Settings{LAN: true, LANKey: "sk-magpie-k"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGPIE_ADDR", "0.0.0.0:3425")
	if n := keyNote(); !strings.Contains(n, "Share on local network") {
		t.Error("shared:", n)
	}
}
