package davsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A 403 is told apart: a folder in the address that isn't on the server, one
// the account may only read, and a server that won't say — not all of them a
// wrong password, as they read before.
func TestDAVForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "me" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		path := strings.TrimSuffix(r.URL.Path, "/")
		switch {
		case r.Method == "PROPFIND" && (path == "" || path == "/ro"):
			w.WriteHeader(http.StatusMultiStatus)
		case r.Method == "PROPFIND" && path == "/missing":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/ro/"):
			w.WriteHeader(http.StatusNotFound)
		default: // a Synology: anything else, 403
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	for _, c := range []struct{ dir, pass, want string }{
		{"/missing", "pw", "there is no folder /missing"},
		{"/ro", "pw", "doesn't let this account write in /ro"},
		{"/secret", "pw", "doesn't let this account use /secret"},
		{"/missing", "bad", "user name or password"},
	} {
		d, err := newDAV(Config{URL: srv.URL + c.dir, User: "me", Password: c.pass})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = d.get(ctx)
		if err == nil {
			err = d.put(ctx, []byte("x"), "")
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.dir, err, c.want)
		}
	}
}
