package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yetone/magpie/internal/update"
)

// Before an update is put in, the page asks what changed (Hu9956, #844):
// every release after this one up to the update's, newest first, without
// their Install sections; the site's list out of reach, the update's own
// notes; no update, none.
func TestUpdateNotesRoute(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after") != "0.1.604" || r.URL.Query().Get("upto") != "0.1.607" {
			t.Errorf("asked %s", r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode(map[string]any{"releases": []update.Note{
			{Version: "0.1.607", Notes: "- seven\n\n### Install\n\nlinks"},
			{Version: "0.1.606", Notes: "- six"},
			{Version: "0.1.605", Notes: "- five"},
			{Version: "0.1.604", Notes: "- four"},
		}})
	}))
	defer feed.Close()
	t.Setenv("MAGPIE_NOTES_FEED", feed.URL)
	t.Setenv("MAGPIE_UPDATE_FEED", "http://127.0.0.1:1/")
	oldV, oldState, oldLatest, oldIn := Version, updates.state, updates.latest, updates.notesIn
	defer func() { Version, updates.state, updates.latest, updates.notesIn = oldV, oldState, oldLatest, oldIn }()
	Version = "0.1.604"
	updates.mu.Lock()
	updates.state, updates.latest, updates.notesIn = "ready", &update.Release{Version: "0.1.607", Notes: "- seven only"}, "en"
	updates.mu.Unlock()
	mux := http.NewServeMux()
	updateRoutes(mux, nil)
	get := func() (out struct {
		Releases []update.Note `json:"releases"`
		Error    string        `json:"error"`
	}) {
		rw := httptest.NewRecorder()
		mux.ServeHTTP(rw, httptest.NewRequest("GET", "/api/update/notes?lang=en", nil))
		if rw.Code != 200 {
			t.Fatalf("%d %s", rw.Code, rw.Body)
		}
		json.Unmarshal(rw.Body.Bytes(), &out)
		return out
	}
	j := get()
	var vs []string
	for _, n := range j.Releases {
		vs = append(vs, n.Version)
	}
	if len(vs) != 3 || vs[0] != "0.1.607" || vs[2] != "0.1.605" || j.Releases[0].Notes != "- seven" {
		t.Fatalf("between: %+v", j)
	}

	// the site's list out of reach: the update's own notes
	t.Setenv("MAGPIE_NOTES_FEED", "http://127.0.0.1:1/")
	if j := get(); len(j.Releases) != 1 || j.Releases[0].Version != "0.1.607" || j.Releases[0].Notes != "- seven only" {
		t.Fatalf("fallback: %+v", j)
	}

	// no update: no notes
	updates.mu.Lock()
	updates.state, updates.latest = "latest", nil
	updates.mu.Unlock()
	if j := get(); len(j.Releases) != 0 || j.Error != "" {
		t.Fatalf("none: %+v", j)
	}
}
