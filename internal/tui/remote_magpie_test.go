package tui

import (
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// A remote magpie is added from the TUI with its address, as the app's
// editor and magpie provider add remote-magpie … url=… add it, and its
// address is changed there later (akic404 on Discord: the TUI had no place
// for it, only the key).
func TestTUIAddsARemoteMagpieWithItsAddress(t *testing.T) {
	home(t)
	m := press(t, model{w: 120, h: 40}, "2", "a")
	if m.mode != modePick {
		t.Fatalf("a opened mode %v, not the vendors", m.mode)
	}
	m = typeIn(m, "remote-magpie")
	m = press(t, m, "enter")
	if m.mode != modeAsk || m.ask.input.Placeholder != "http://192.168.1.20:3425" {
		t.Fatalf("picking Remote magpie asked %q (mode %v), not its address", m.ask.input.Placeholder, m.mode)
	}
	// nothing typed: said to be needed, nothing added
	m = press(t, m, "enter")
	wantFlash(t, m, false, "The other magpie's address is needed")
	if _, err := provider.Find("remote-magpie"); err == nil {
		t.Fatal("added without an address")
	}

	m = press(t, m, "a")
	m = typeIn(m, "remote-magpie")
	m = press(t, m, "enter")
	m = typeIn(m, "127.0.0.1:1")
	m = press(t, m, "enter")
	if m.mode != modeAsk || m.ask.input.Placeholder != "the API key" {
		t.Fatalf("after the address it asked %q (mode %v), not the key", m.ask.input.Placeholder, m.mode)
	}
	m = typeIn(m, "sk-magpie-test")
	m = press(t, m, "enter")
	wantFlash(t, m, true, "added Remote magpie")
	p, err := provider.Find("remote-magpie")
	if err != nil {
		t.Fatal(err)
	}
	if p.Chat != "http://127.0.0.1:1/v1" || p.Responses != "http://127.0.0.1:1/v1" || p.Anthropic != "http://127.0.0.1:1" || p.Key != "sk-magpie-test" {
		t.Fatalf("saved %q %q %q key %q", p.Chat, p.Responses, p.Anthropic, p.Key)
	}

	// w changes its address, the one there now in the line
	m.reloadProviders()
	for i, q := range m.provs {
		if q.ID == "remote-magpie" {
			m.prow = i
		}
	}
	m = press(t, m, "w")
	if m.mode != modeAsk || m.ask.input.Value() != "http://127.0.0.1:1" {
		t.Fatalf("w opened %q (mode %v), not the address now", m.ask.input.Value(), m.mode)
	}
	m = press(t, m, "ctrl+u")
	m = typeIn(m, "http://127.0.0.1:2/v1")
	m = press(t, m, "enter")
	wantFlash(t, m, true, "address")
	p, _ = provider.Find("remote-magpie")
	if p.Chat != "http://127.0.0.1:2/v1" || p.Anthropic != "http://127.0.0.1:2" || p.Key != "sk-magpie-test" {
		t.Fatalf("after w: %q %q key %q", p.Chat, p.Anthropic, p.Key)
	}

	// a vendor of its own address has none to change
	m.reloadProviders()
	for i, q := range m.provs {
		if q.ID == "a" {
			m.prow = i
		}
	}
	m = press(t, m, "w")
	if m.mode == modeAsk {
		t.Fatal("w asked a custom provider's address")
	}
}
