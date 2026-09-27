package gui

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

// `magpie web` serves the window's page to a browser, for a computer that
// can't show the app — no desktop, WSL, a server reached over SSH — with
// the gateway beside it as the app has. What is the desktop's (the panel,
// the tray, the Dock) does nothing there; a folder is named, not opened.

// webHost is the Windows a browser tab stands for.
type webHost struct{ quit func() }

func (webHost) HidePanel()                   {}
func (webHost) ShowMain(string)              {}
func (h webHost) Quit()                      { h.quit() }
func (webHost) OpenURL(string)               {} // the page opens links itself
func (webHost) Copy(string) bool             { return false }
func (webHost) FitPanel(int, Glide)          {}
func (webHost) TintPanel([4]uint8, int) bool { return false }
func (webHost) OpenFolder(path string) error {
	return errors.New("in the browser magpie can't open folders: it is " + tilde(path))
}

// Web is a started `magpie web`: its address and the link that signs a
// browser in.
type Web struct {
	Addr, Link string
	srv        *http.Server
	quit       chan struct{}
}

// StartWeb serves the page on addr (host:port). Every request needs the
// key the link carries, taken once into a cookie other sites' pages can't
// send with a POST (every change is one), so neither a page elsewhere nor
// anyone else on the network reaches the settings and keys behind it.
func StartWeb(addr string) (*Web, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 16)
	rand.Read(b)
	key := hex.EncodeToString(b)
	w := &Web{Addr: ln.Addr().String(), quit: make(chan struct{})}
	host, port, _ := net.SplitHostPort(w.Addr)
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	var once sync.Once
	quit := func() { once.Do(func() { close(w.quit) }) }
	w.Link = "http://" + net.JoinHostPort(host, port) + "/?k=" + key
	w.srv = &http.Server{
		Handler:           webGuard("magpie_web_"+port, key, Handler(webHost{quit}, startBackend())),
		ReadHeaderTimeout: 30 * time.Second,
	}
	go w.srv.Serve(ln)
	return w, nil
}

// Wait returns once the page's Quit is used.
func (w *Web) Wait() {
	<-w.quit
	time.Sleep(200 * time.Millisecond) // the answer to the Quit gets out
	w.srv.Close()
}

// webGuard lets through requests with the key: in the link's k, which it
// trades for a cookie and a clean address, or in that cookie.
func webGuard(cookie, key string, next http.Handler) http.Handler {
	same := func(v string) bool { return subtle.ConstantTimeCompare([]byte(v), []byte(key)) == 1 }
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query(); q.Has("k") {
			if !same(q.Get("k")) {
				http.Error(rw, "this link's key is not this magpie web's: use the link it printed when it started", http.StatusUnauthorized)
				return
			}
			http.SetCookie(rw, &http.Cookie{Name: cookie, Value: key, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
			q.Del("k")
			u := *r.URL
			u.RawQuery = q.Encode()
			http.Redirect(rw, r, u.RequestURI(), http.StatusSeeOther)
			return
		}
		if c, err := r.Cookie(cookie); err != nil || !same(c.Value) {
			http.Error(rw, "open the link magpie web printed when it started: it carries the key", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(rw, r)
	})
}

// LANAddrs are this computer's addresses on the local network, for the
// links to print when the page is served there.
func LANAddrs() []string {
	var out []string
	as, _ := net.InterfaceAddrs()
	for _, a := range as {
		n, ok := a.(*net.IPNet)
		if !ok || n.IP.IsLoopback() || n.IP.To4() == nil || !n.IP.IsPrivate() {
			continue
		}
		out = append(out, n.IP.String())
	}
	return out
}

// isWeb: the page is served to a browser by `magpie web`.
func isWeb(w Windows) bool { _, ok := w.(webHost); return ok }
