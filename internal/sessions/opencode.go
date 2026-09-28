package sessions

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/provider"
)

// OpenCode keeps a session as rows, not a file of lines: since 1.2 in its
// SQLite database (opencode.db: session, message and part tables, each
// message and part a JSON document), before that as JSON files under
// storage/ — session/<project>/<id>.json, message/<session>/<id>.json and
// part/<message>/<id>.json. The database is made from those files once,
// which stay behind, so where there is a database only it is read. An
// assistant message carries its model and its tokens, input without the
// cache and output without the reasoning. A subagent's work is a session
// of its own whose parent is the session it ran in, and counts there.

// OpenCodeDir is OpenCode's data folder: $XDG_DATA_HOME/opencode, else
// ~/.local/share/opencode — on Windows too, where OpenCode keeps it there.
func OpenCodeDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "opencode")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "opencode")
}

// openCodeDB is OpenCode's database: $OPENCODE_DB (a path in the data
// folder unless absolute), else opencode.db there.
func openCodeDB() string {
	if p := os.Getenv("OPENCODE_DB"); p != "" && p != ":memory:" {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(OpenCodeDir(), p)
	}
	return filepath.Join(OpenCodeDir(), "opencode.db")
}

// ocInfo is a session as OpenCode keeps it, a row or a file.
type ocInfo struct {
	ID        string `json:"id"`
	ParentID  string `json:"parentID"`
	Directory string `json:"directory"`
	Title     string `json:"title"`
	Time      struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
}

// ocMessage is a message, a JSON document in either store.
type ocMessage struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	ModelID string `json:"modelID"`
	Time    struct {
		Created   int64 `json:"created"`
		Completed int64 `json:"completed"`
	} `json:"time"`
	Path *struct {
		Cwd string `json:"cwd"`
	} `json:"path"`
	Tokens *struct {
		Input     int `json:"input"`
		Output    int `json:"output"`
		Reasoning int `json:"reasoning"`
		Cache     struct {
			Read  int `json:"read"`
			Write int `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
}

// ocPart is a part of a message: only its text is looked at, for a title.
type ocPart struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Synthetic bool   `json:"synthetic"`
	Ignored   bool   `json:"ignored"`
}

// ocStore is where OpenCode keeps its sessions: its database or its files.
type ocStore interface {
	info(sid string) (ocInfo, bool)
	messages(sid string) [][]byte // in no set order
	parts(mid string) [][]byte    // in order
}

func ms(n int64) time.Time {
	if n <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(n)
}

// openCodeFiles are OpenCode's sessions, one "file" each: in the database
// its path is the database's and #<session id>, its size the count of its
// messages and its time that of the latest change to one; from the files
// its own file's path, the sum of its messages' sizes and the latest time.
func openCodeFiles() []file {
	if db := openCodeDB(); fileExists(db) {
		return openCodeDBFiles(db)
	}
	return openCodeJSONFiles(filepath.Join(OpenCodeDir(), "storage"))
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// ocRoots gives each session the key of the session it ran under, the one
// at the top: a subagent's subagent counts there too.
func ocRoots(out []file, parent map[string]string) []file {
	for i := range out {
		root := out[i].sid
		for n := 0; parent[root] != "" && n < 64; n++ {
			root = parent[root]
		}
		out[i].key = "opencode:" + root
		out[i].main = parent[out[i].sid] == ""
	}
	return out
}

// ---- the database -------------------------------------------------------------

// dbs are the databases open while List or Stats reads them, closed when
// they are done (closeDBs).
var dbs = map[string]*sql.DB{}

func openDB(path string) *sql.DB {
	if db := dbs[path]; db != nil {
		return db
	}
	db, err := provider.OpenReadOnly(path)
	if err != nil {
		return nil
	}
	db.SetMaxOpenConns(4)
	dbs[path] = db
	return db
}

func closeDBs() {
	for p, db := range dbs {
		db.Close()
		delete(dbs, p)
	}
}

type ocDB struct{ db *sql.DB }

func openCodeDBFiles(path string) []file {
	db := openDB(path)
	if db == nil {
		return nil
	}
	rows, err := db.Query(`SELECT s.id, COALESCE(s.parent_id, ''), s.time_updated, COUNT(m.id), COALESCE(MAX(m.time_updated), 0)
		FROM session s LEFT JOIN message m ON m.session_id = s.id GROUP BY s.id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	store := ocDB{db}
	var out []file
	parent := map[string]string{}
	for rows.Next() {
		var id, par string
		var updated, n, last int64
		if rows.Scan(&id, &par, &updated, &n, &last) != nil {
			continue
		}
		parent[id] = par
		out = append(out, file{agent: "opencode", path: path + "#" + id, sid: id, oc: store, size: n, mod: ms(max(updated, last))})
	}
	return ocRoots(out, parent)
}

func (s ocDB) info(sid string) (ocInfo, bool) {
	var i ocInfo
	err := s.db.QueryRow(`SELECT id, COALESCE(parent_id, ''), directory, title, time_created, time_updated FROM session WHERE id = ?`, sid).
		Scan(&i.ID, &i.ParentID, &i.Directory, &i.Title, &i.Time.Created, &i.Time.Updated)
	return i, err == nil
}

func (s ocDB) rows(query, arg string) [][]byte {
	rows, err := s.db.Query(query, arg)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var b []byte
		if rows.Scan(&b) == nil {
			out = append(out, b)
		}
	}
	return out
}

func (s ocDB) messages(sid string) [][]byte {
	return s.rows(`SELECT data FROM message WHERE session_id = ?`, sid)
}

func (s ocDB) parts(mid string) [][]byte {
	return s.rows(`SELECT data FROM part WHERE message_id = ? ORDER BY id`, mid)
}

// ---- the files ----------------------------------------------------------------

type ocFiles struct{ root string } // storage/

func openCodeJSONFiles(root string) []file {
	infos, _ := filepath.Glob(filepath.Join(root, "session", "*", "*.json"))
	store := ocFiles{root}
	var out []file
	parent := map[string]string{}
	for _, p := range infos {
		b, err := os.ReadFile(p)
		var i ocInfo
		if err != nil || json.Unmarshal(b, &i) != nil || i.ID == "" {
			continue
		}
		f := file{agent: "opencode", path: p, sid: i.ID, oc: store}
		if !stat(&f) {
			continue
		}
		// a message's file is written again as its reply goes on
		f.size = 0
		if es, err := os.ReadDir(filepath.Join(root, "message", i.ID)); err == nil {
			for _, e := range es {
				if fi, err := e.Info(); err == nil && fi.Mode().IsRegular() {
					f.size += fi.Size()
					if fi.ModTime().After(f.mod) {
						f.mod = fi.ModTime()
					}
				}
			}
		}
		parent[i.ID] = i.ParentID
		out = append(out, f)
	}
	return ocRoots(out, parent)
}

func (s ocFiles) info(sid string) (ocInfo, bool) {
	ps, _ := filepath.Glob(filepath.Join(s.root, "session", "*", sid+".json"))
	for _, p := range ps {
		var i ocInfo
		if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &i) == nil {
			return i, true
		}
	}
	return ocInfo{}, false
}

func (s ocFiles) docs(dir string) [][]byte {
	ps, _ := filepath.Glob(filepath.Join(s.root, dir, "*.json"))
	sort.Strings(ps)
	var out [][]byte
	for _, p := range ps {
		if b, err := os.ReadFile(p); err == nil {
			out = append(out, b)
		}
	}
	return out
}

func (s ocFiles) messages(sid string) [][]byte { return s.docs(filepath.Join("message", sid)) }
func (s ocFiles) parts(mid string) [][]byte    { return s.docs(filepath.Join("part", mid)) }

// ---- reading a session --------------------------------------------------------

// parseOpenCode reads a session whole: a message's usage is written in
// place as its reply goes on, so there is no reading on from before.
func parseOpenCode(f file) *state {
	s := &state{Size: f.size, Mod: f.mod.UnixNano()}
	if f.oc == nil {
		return s
	}
	info, ok := f.oc.info(f.sid)
	if !ok {
		return s
	}
	s.ID, s.Cwd = info.ID, info.Directory
	if t := title(info.Title); t != "" && !ocDefaultTitle(t) {
		s.Named = t
	}
	var msgs []ocMessage
	for _, b := range f.oc.messages(f.sid) {
		var m ocMessage
		if json.Unmarshal(b, &m) == nil {
			msgs = append(msgs, m)
		}
	}
	sort.SliceStable(msgs, func(i, j int) bool {
		if msgs[i].Time.Created != msgs[j].Time.Created {
			return msgs[i].Time.Created < msgs[j].Time.Created
		}
		return msgs[i].ID < msgs[j].ID
	})
	s.saw(ms(info.Time.Created), f.main)
	for _, m := range msgs {
		at := ms(m.Time.Created)
		s.saw(at, f.main)
		switch m.Role {
		case "user":
			if f.main && s.Title == "" && s.First == "" {
				ocTitle(s, f.oc.parts(m.ID))
			}
		case "assistant":
			if done := ms(m.Time.Completed); !done.IsZero() {
				s.saw(done, f.main)
				at = done
			}
			if s.Cwd == "" && m.Path != nil {
				s.Cwd = m.Path.Cwd
			}
			if t := m.Tokens; t != nil && m.ModelID != "" {
				s.use(dateOf(at), m.ModelID, Tokens{Input: t.Input, Output: t.Output + t.Reasoning, CacheRead: t.Cache.Read, CacheWrite: t.Cache.Write})
			}
		}
	}
	return s
}

// ocTitle takes a session's title from its first prompt: the words typed,
// not what OpenCode put in beside them (a file read, a command's template).
func ocTitle(s *state, parts [][]byte) {
	var typed, other []string
	for _, b := range parts {
		var p ocPart
		if json.Unmarshal(b, &p) != nil || p.Type != "text" || p.Ignored || strings.TrimSpace(p.Text) == "" {
			continue
		}
		if p.Synthetic {
			other = append(other, p.Text)
		} else {
			typed = append(typed, p.Text)
		}
	}
	if t := strings.Join(typed, " "); strings.HasPrefix(strings.TrimSpace(t), "<") {
		s.First = untagged(t)
	} else {
		s.Title = title(t)
	}
	if s.Title == "" && s.First == "" {
		s.First = untagged(strings.Join(other, " "))
	}
}

// ocDefaultTitle is the title OpenCode gives a session before it names it.
func ocDefaultTitle(t string) bool {
	return strings.HasPrefix(t, "New session - ") || strings.HasPrefix(t, "Child session - ")
}
