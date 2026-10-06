package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

// A web page the user listed in Settings (#1051, SkyAerope: a preflight
// from http://localhost:3000 for /v1/models was answered 404 with no CORS
// headers) has its preflight answered and reads the reply, its calls with
// a gateway key; a page not listed gets no CORS headers, as before, nor
// does any page while none is listed; agents, which send no Origin, are
// untouched.
func TestCORSOrigins(t *testing.T) {
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
	}))
	defer up.Close()
	if err := provider.Save(provider.Provider{ID: "plan", Name: "Plan", Key: "upstream-secret", Chat: up.URL + "/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	secret, err := access.Update("add-key", access.Change{Name: "Web app"})
	if err != nil {
		t.Fatal(err)
	}
	h := New().Handler()
	// from this computer, as a browser on it calls 127.0.0.1:3425
	do := func(method, path, origin, key string, hdr map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		var body io.Reader
		if method == "POST" {
			body = strings.NewReader(chatReq)
		}
		r := httptest.NewRequest(method, path, body)
		r.RemoteAddr = "127.0.0.1:50123"
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	preflight := map[string]string{"Access-Control-Request-Method": "GET", "Access-Control-Request-Headers": "authorization, content-type"}

	// none listed: the reporter's preflight, as before
	if w := do("OPTIONS", "/v1/models", "http://localhost:3000", "", preflight); w.Header().Get("Access-Control-Allow-Origin") != "" || w.Code == http.StatusNoContent {
		t.Fatalf("none listed: %d %v", w.Code, w.Header())
	}

	s := settings.Load()
	if s.CORSOrigins, err = settings.CleanOrigins([]string{"http://LOCALHOST:3000/", "https://app.example.com:443"}); err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}

	w := do("OPTIONS", "/v1/models", "http://localhost:3000", "", map[string]string{
		"Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "authorization, content-type, x-app, bad name;",
		"Access-Control-Request-Private-Network": "true"})
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" ||
		!strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "POST") ||
		w.Header().Get("Access-Control-Allow-Headers") != "authorization, content-type, x-app" ||
		w.Header().Get("Access-Control-Allow-Private-Network") != "true" || w.Header().Get("Vary") != "Origin" {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}
	// the call itself, with a gateway key: answered and readable
	if w := do("GET", "/v1/models", "http://localhost:3000", secret, nil); w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("models: %d %v", w.Code, w.Header())
	}
	if w := do("POST", "/v1/chat/completions", "https://app.example.com", secret, nil); w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" || !strings.Contains(w.Body.String(), "ok") {
		t.Fatalf("chat: %d %v %s", w.Code, w.Header(), w.Body)
	}
	// without one, or with a key that isn't a gateway key, it is refused —
	// readably, so the page can say why
	for _, key := range []string{"", "magpie", "sk-magpie-not-a-key"} {
		w := do("POST", "/v1/chat/completions", "http://localhost:3000", key, nil)
		if w.Code != http.StatusUnauthorized || w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" || !strings.Contains(w.Body.String(), "gateway key") {
			t.Errorf("key %q: %d %v %s", key, w.Code, w.Header(), w.Body)
		}
	}
	// a key turned off is no key
	keys, _ := access.List()
	access.Update("off-key", access.Change{Key: keys[len(keys)-1].ID})
	if w := do("GET", "/v1/models", "http://localhost:3000", secret, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("a key turned off: %d", w.Code)
	}
	access.Update("on-key", access.Change{Key: keys[len(keys)-1].ID})

	// another page: no CORS headers, whatever it sends; a port or scheme
	// apart is another origin
	for _, o := range []string{"http://localhost:3001", "https://localhost:3000", "http://evil.example", "null"} {
		if w := do("OPTIONS", "/v1/models", o, "", preflight); w.Header().Get("Access-Control-Allow-Origin") != "" || w.Code == http.StatusNoContent {
			t.Errorf("%s preflight: %d %v", o, w.Code, w.Header())
		}
		if w := do("GET", "/v1/models", o, secret, nil); w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: %v", o, w.Header())
		}
	}
	// an agent: no Origin, no key, as before
	if w := do("POST", "/v1/chat/completions", "", "", nil); w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("agent: %d %v", w.Code, w.Header())
	}
}
