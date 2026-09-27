package gui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebGuard(t *testing.T) {
	h := webGuard("c", "key", http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { rw.WriteHeader(http.StatusTeapot) }))
	do := func(r *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	if c := do(httptest.NewRequest("GET", "/api/state", nil)).Code; c != http.StatusUnauthorized {
		t.Fatalf("no key: %d", c)
	}
	if c := do(httptest.NewRequest("GET", "/?k=nope", nil)).Code; c != http.StatusUnauthorized {
		t.Fatalf("wrong key: %d", c)
	}
	rec := do(httptest.NewRequest("GET", "/?k=key&view=settings", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/?view=settings" {
		t.Fatalf("link: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	ck := rec.Result().Cookies()
	if len(ck) != 1 || ck[0].Value != "key" || !ck[0].HttpOnly || ck[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie: %+v", ck)
	}
	r := httptest.NewRequest("POST", "/api/settings", nil)
	r.AddCookie(ck[0])
	if c := do(r).Code; c != http.StatusTeapot {
		t.Fatalf("with cookie: %d", c)
	}
	r = httptest.NewRequest("POST", "/api/settings", nil)
	r.AddCookie(&http.Cookie{Name: "c", Value: "other"})
	if c := do(r).Code; c != http.StatusUnauthorized {
		t.Fatalf("wrong cookie: %d", c)
	}
}
