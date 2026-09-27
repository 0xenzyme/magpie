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

// An agent whose User-Agent is only the AI SDK's (Alma) is known by the
// token it was given; the others still by their User-Agent.
func TestAgentOf(t *testing.T) {
	for _, c := range []struct{ auth, key, ua, want string }{
		{"Bearer " + TokenFor("alma"), "", "ai-sdk/openai/2.0.52 ai-sdk/provider-utils/3.0.12 runtime/node.js/v22", "alma"},
		{"", TokenFor("alma"), "ai-sdk/anthropic/2.0.1", "alma"},
		{"Bearer " + Token, "", "claude-cli/2.1.0 (external, cli)", "claude-cli"},
		{"Bearer " + Token, "", "ai-sdk/openai/2.0.52", "ai-sdk"},
		{"Bearer " + Token + "-", "", "codex_cli_rs/0.40.0", "codex_cli_rs"},
	} {
		r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		if c.auth != "" {
			r.Header.Set("Authorization", c.auth)
		}
		if c.key != "" {
			r.Header.Set("x-api-key", c.key)
		}
		r.Header.Set("User-Agent", c.ua)
		if got := agentOf(r); got != c.want {
			t.Errorf("%q %q %q: %q, want %q", c.auth, c.key, c.ua, got, c.want)
		}
	}
}
