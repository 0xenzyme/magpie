package gateway

import (
	"net/http"
	"slices"
	"strings"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

// corsGuard lets the web pages the user listed in Settings › Gateway
// (settings.CORSOrigins, #1051) call the gateway from a browser: their
// preflight is answered, and their calls get the headers that let the page
// read the reply. Such a call carries an enabled gateway key — a page is
// not an agent the user started, and on loopback a call needs no key, so
// without one any script served at that origin (another dev server on
// the same port) could spend the user's subscriptions. A page not listed
// gets no CORS headers, as before, and a request with no Origin (every
// agent's) passes untouched.
func corsGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || !corsAllowed(origin) {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Add("Vary", "Origin")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			if asked := corsHeaders(r.Header.Get("Access-Control-Request-Headers")); asked != "" {
				h.Set("Access-Control-Allow-Headers", asked)
			}
			// Chrome asks before a public page reaches this computer
			if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
				h.Set("Access-Control-Allow-Private-Network", "true")
			}
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// lanGuard, in front of Handler on the real server, has already
		// taken the key and put magpie's own token in its place
		keyed := access.Caller(r.Context()).KeyID != "" ||
			slices.ContainsFunc(callerKeys(r), func(k string) bool { _, ok := access.Authenticate(k); return ok })
		if !keyed {
			writeError(w, provider.Chat, http.StatusUnauthorized, "a web page calls magpie with a gateway key: create one in magpie's Gateway page and send it as Authorization: Bearer <key> (or x-api-key)")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// corsAllowed: origin is one the user listed.
func corsAllowed(origin string) bool {
	saved := settings.Load().CORSOrigins
	if len(saved) == 0 {
		return false
	}
	// as saved from Settings, or as written into settings.json by hand
	list, _ := settings.CleanOrigins(saved)
	o, err := settings.CleanOrigins([]string{origin})
	return err == nil && len(o) == 1 && slices.Contains(list, o[0])
}

// corsHeaders is a preflight's Access-Control-Request-Headers as the
// answer allows them: the header names asked for, nothing else.
func corsHeaders(asked string) string {
	var out []string
	for _, f := range strings.Split(asked, ",") {
		f = strings.TrimSpace(f)
		if f != "" && !strings.ContainsFunc(f, func(c rune) bool {
			return !(c == '-' || c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z')
		}) {
			out = append(out, f)
		}
	}
	return strings.Join(out, ", ")
}
