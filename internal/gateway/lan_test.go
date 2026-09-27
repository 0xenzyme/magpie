package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Shared on the network, a request from another machine needs the key, and
// goes on with the gateway's own token; this computer's need none.
func TestLANGuard(t *testing.T) {
	var got string
	h := lanGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization") + "|" + r.Header.Get("x-api-key")
	}))
	call := func(from string, hdr ...string) int {
		got = ""
		r := httptest.NewRequest("POST", "/v1/messages", nil)
		r.RemoteAddr = from
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	key := "sk-magpie-k"
	none := ""
	t.Cleanup(func() { lanKey.Store(&none) })

	lanKey.Store(&none)
	t.Setenv("MAGPIE_ADDR", "")
	if c := call("192.168.1.9:5000"); c != http.StatusForbidden {
		t.Fatal("unshared, another machine got", c)
	}
	if c := call("127.0.0.1:5000"); c != 200 {
		t.Fatal("loopback got", c)
	}
	t.Setenv("MAGPIE_ADDR", "0.0.0.0:3425")
	if c := call("192.168.1.9:5000"); c != 200 {
		t.Fatal("MAGPIE_ADDR's open gateway got", c)
	}

	lanKey.Store(&key)
	if c := call("192.168.1.9:5000"); c != http.StatusUnauthorized {
		t.Fatal("no key got", c)
	}
	if c := call("192.168.1.9:5000", "Authorization", "Bearer wrong"); c != http.StatusUnauthorized {
		t.Fatal("a wrong key got", c)
	}
	if c := call("192.168.1.9:5000", "x-api-key", key); c != 200 || got != "Bearer magpie|magpie" {
		t.Fatal(c, got)
	}
	if c := call("[::1]:5000", "Authorization", "Bearer anything"); c != 200 || got != "Bearer anything|" {
		t.Fatal("loopback", c, got)
	}
}

// A gateway on every interface is reached here on loopback.
func TestURLOfWildcard(t *testing.T) {
	t.Setenv("MAGPIE_ADDR", "0.0.0.0:3499")
	if u := URL(); u != "http://127.0.0.1:3499" {
		t.Fatal(u)
	}
	t.Setenv("MAGPIE_ADDR", "")
	if u := URL(); u != "http://127.0.0.1:3425" {
		t.Fatal(u)
	}
}
