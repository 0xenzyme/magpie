package gui

import (
	"net/url"
	"testing"
)

// The tray panel's Routing tab opens the window's Routing page on the
// request clicked: the id goes along, and nothing else does.
func TestMainView(t *testing.T) {
	for _, c := range []struct {
		q    url.Values
		want string
	}{
		{url.Values{}, ""},
		{url.Values{"view": {"settings"}}, "settings"},
		{url.Values{"view": {"routing"}}, "routing"},
		{url.Values{"view": {"routing"}, "req": {"42"}}, "routing&req=42"},
		{url.Values{"view": {"settings"}, "req": {"42"}}, "settings"},
		{url.Values{"view": {"routing"}, "req": {"42&view=x"}}, "routing"},
		{url.Values{"view": {"routing"}, "req": {"-1"}}, "routing"},
	} {
		if got := mainView(c.q); got != c.want {
			t.Errorf("%v: %q, want %q", c.q, got, c.want)
		}
	}
}
