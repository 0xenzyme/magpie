package gui

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/update"
)

// whatsNew shows what changed once magpie runs a newer release than it last
// did (a Discord user: to see whether their issue was fixed). The version it
// last ran is kept in magpie's own folder; as it starts on a newer one, the
// notes of every release since are asked of the site (where the update feed
// is) the first time a window or magpie web's page asks, and shown once.
// Offline, nothing is shown. Settings can open them again. The notes are
// in the page's language, Chinese or English, where a release has them in
// it (freecss on Discord: they should follow Settings' language).
type whatsNew struct {
	mu      sync.Mutex
	after   string                   // the version last run, "" for the current one's notes alone
	pending bool                     // an upgrade whose notes are still to be shown
	shown   bool                     // a page has shown them
	notes   map[string][]update.Note // by language
}

var news = &whatsNew{}

// started is when this process began, to tell magpie's folder from before
// it apart from one this run just made.
var started = time.Now()

const lastRunFile = "last-version"

// start reads the version last run and puts this one in its place.
func (n *whatsNew) start() {
	dir := settings.Dir()
	b, _ := os.ReadFile(filepath.Join(dir, lastRunFile))
	last := strings.TrimSpace(string(b))
	after, show := update.ShowNotes(last, Version, last == "" && usedBefore(dir))
	n.mu.Lock()
	n.after, n.pending = after, show
	n.mu.Unlock()
	if !update.Released(Version) || last == Version {
		return // a build from source leaves the release it came after
	}
	if err := os.MkdirAll(dir, 0o755); err == nil {
		err = os.WriteFile(filepath.Join(dir, lastRunFile), []byte(Version+"\n"), 0o644)
		if err != nil {
			log.Println("last version:", err)
		}
	}
}

// usedBefore is whether magpie's folder held its files before this run: a
// magpie from before the version was kept, not a fresh install.
func usedBefore(dir string) bool {
	for _, f := range []string{"settings.json", "providers.json"} {
		if fi, err := os.Stat(filepath.Join(dir, f)); err == nil && fi.ModTime().Before(started) {
			return true
		}
	}
	return false
}

type whatsNewJSON struct {
	Show     bool          `json:"show"`
	Current  string        `json:"current"`
	Releases []update.Note `json:"releases"`
}

// get is what a page shows: after an upgrade, the notes since, once (a
// page showing them says so with seen); asked again (all, from Settings),
// those or the current version's alone.
func (n *whatsNew) get(ctx context.Context, all bool, lang string) whatsNewJSON {
	n.mu.Lock()
	after, pending, shown := n.after, n.pending, n.shown
	if !pending {
		after = ""
	}
	notes := n.notes[lang]
	n.mu.Unlock()
	j := whatsNewJSON{Current: Version, Releases: []update.Note{}}
	if ((!pending || shown) && !all) || !update.Released(Version) {
		return j
	}
	if notes == nil {
		notes = fetchNotes(ctx, after, Version, lang)
		if len(notes) > 0 { // kept: they don't change while magpie runs
			n.mu.Lock()
			if n.notes == nil {
				n.notes = map[string][]update.Note{}
			}
			n.notes[lang] = notes
			n.mu.Unlock()
		}
	}
	if len(notes) > 0 {
		j.Releases = notes
	}
	j.Show = pending && !shown && len(j.Releases) > 0
	return j
}

func (n *whatsNew) seen() {
	n.mu.Lock()
	n.shown = true
	n.mu.Unlock()
}

// fetchNotes asks the site for the notes since after; a site without the
// list yet, or one that can't be reached, leaves the update feed, whose
// newest release has its notes, for when that is the current one.
func fetchNotes(ctx context.Context, after, upto, lang string) []update.Note {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	notes, err := update.NotesBetween(ctx, after, upto, lang)
	if err == nil && len(notes) > 0 {
		return notes
	}
	if err != nil {
		log.Println("release notes:", err)
	}
	rel, err := update.LatestIn(ctx, lang)
	if err != nil {
		return nil
	}
	return update.Between([]update.Note{{Version: rel.Version, Notes: rel.Notes, URL: rel.URL}}, after, upto)
}

// pageLang is the language a page asks in, its own (?lang=, zh or en), or
// without one the app's, as the tray menu has it.
func pageLang(r *http.Request) string {
	if l := askedLang(r); l != "" {
		return l
	}
	return trayLang(settings.Load().Lang, systemLang)
}

// askedLang is the language a page names (?lang=): zh for any Chinese, en
// for anything else, "" for none.
func askedLang(r *http.Request) string {
	switch l := strings.ToLower(r.URL.Query().Get("lang")); {
	case strings.HasPrefix(l, "zh"):
		return "zh"
	case l != "":
		return "en"
	}
	return ""
}

func whatsNewRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/whatsnew", func(rw http.ResponseWriter, r *http.Request) {
		writeJSON(rw, news.get(r.Context(), r.URL.Query().Get("all") != "", pageLang(r)))
	})
	mux.HandleFunc("POST /api/whatsnew/seen", func(rw http.ResponseWriter, r *http.Request) {
		news.seen()
		rw.WriteHeader(http.StatusNoContent)
	})
}
