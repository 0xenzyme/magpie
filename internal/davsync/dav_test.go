package davsync

import (
	"context"
	"io"
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

// OpenList's and Alist's /dav/ lists their storages: a folder can't be made
// there (MKCOL 405, as if it were) and a file can't be put (404). The error
// says to put a storage in the address, naming them.
func TestDAVStorageRoot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/dav/" && r.Header.Get("Depth") == "1":
			w.WriteHeader(http.StatusMultiStatus)
			io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><D:multistatus xmlns:D="DAV:">`+
				`<D:response><D:href>/dav/</D:href><D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop></D:propstat></D:response>`+
				`<D:response><D:href>/dav/local/</D:href><D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop></D:propstat></D:response>`+
				`<D:response><D:href>/dav/aliyun/</D:href><D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop></D:propstat></D:response>`+
				`<D:response><D:href>/dav/readme.txt</D:href><D:propstat><D:prop><D:resourcetype/></D:prop></D:propstat></D:response>`+
				`</D:multistatus>`)
		case r.Method == "MKCOL":
			w.WriteHeader(http.StatusMethodNotAllowed)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	d, err := newDAV(Config{URL: srv.URL + "/dav/"})
	if err != nil {
		t.Fatal(err)
	}
	err = d.put(context.Background(), []byte("x"), "")
	if err == nil || !strings.Contains(err.Error(), "(local, aliyun), like "+srv.URL+"/dav/local") {
		t.Fatalf("%v", err)
	}
}

// A Synology answers 403 for anything under a folder that isn't there yet,
// PROPFIND too: magpie's own folder, before the first sync, is made rather
// than taken for a folder the account can't write in.
func TestDAVForbiddenUntilMade(t *testing.T) {
	made, stored := false, ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(r.URL.Path, "/")
		switch {
		case path == "/data" && r.Method == "PROPFIND":
			w.WriteHeader(http.StatusMultiStatus)
		case path == "/data/magpie" && r.Method == "MKCOL":
			made = true
			w.WriteHeader(http.StatusCreated)
		case strings.HasPrefix(path, "/data/magpie") && made:
			switch r.Method {
			case "PROPFIND":
				w.WriteHeader(http.StatusMultiStatus)
			case http.MethodPut:
				b, _ := io.ReadAll(r.Body)
				stored = string(b)
				w.WriteHeader(http.StatusCreated)
			case http.MethodGet:
				if stored == "" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				io.WriteString(w, stored)
			}
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	d, err := newDAV(Config{URL: srv.URL + "/data"})
	if err != nil {
		t.Fatal(err)
	}
	if data, _, err := d.get(ctx); err != nil || data != nil {
		t.Fatalf("before the first sync: %q %v", data, err)
	}
	if err := d.put(ctx, []byte("x"), ""); err != nil {
		t.Fatal(err)
	}
	if data, _, err := d.get(ctx); err != nil || string(data) != "x" {
		t.Fatalf("after: %q %v", data, err)
	}
}
