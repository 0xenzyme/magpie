package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/sessions"
)

// sessionsHome puts the sessions package's fixtures where Claude Code and
// Codex keep their sessions, in a sandbox HOME, their models priced.
func sessionsHome(t *testing.T) {
	t.Helper()
	h := home(t)
	for from, env := range map[string]string{"claude": "CLAUDE_CONFIG_DIR", "codex": "CODEX_HOME"} {
		dir := filepath.Join(h, "sessions", from)
		if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "sessions", "testdata", from))); err != nil {
			t.Fatal(err)
		}
		t.Setenv(env, dir)
	}
	oldZone, oldPrice := time.Local, sessions.PriceOf
	time.Local = time.UTC
	sessions.PriceOf = func(m string) (catalog.Price, bool) {
		switch m {
		case "claude-opus-5-5":
			return catalog.Price{Input: 4, Output: 20, CacheRead: 0.2, CacheWrite: 5}, true
		case "gpt-6-astra":
			return catalog.Price{Input: 10, Output: 50, CacheRead: 1}, true
		}
		return catalog.Price{}, false
	}
	t.Cleanup(func() {
		time.Local, sessions.PriceOf = oldZone, oldPrice
		sessions.Reset()
	})
	sessions.Reset()
}

// The Sessions page shows a range's totals, a chart of its days and its
// top models and folders; a model or a folder picked narrows them.
func TestSessionsPage(t *testing.T) {
	sessionsHome(t)
	m := press(t, model{w: 120, h: 40, srange: 1}, "5", "A")
	if m.page != pageSessions || sessRanges[m.srange].days != 0 {
		t.Fatalf("page %d, range %d", m.page, m.srange)
	}
	v := m.View()
	for _, want := range []string{"5 sessions", " all ", "all models", "all folders", "11.9K tokens", "cache read 25.2K (69% hit)", "active 7m on 2 days",
		"█", "Sep 20", "tokens · a day", "models", "gpt-6-astra", "claude-opus-5-5", "folders", "/work/app", "/work/it's", "M model", "f folder"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q in\n%s", want, v)
		}
	}

	// pick a model: the totals are its, and active time isn't kept by model
	m = press(t, m, "M")
	if m.mode != modePick {
		t.Fatal("no picker")
	}
	m = typeIn(m, "opus")
	m = press(t, m, "enter")
	if m.smodel != "claude-opus-5-5" || m.mode != modeList {
		t.Fatalf("model %q, mode %d", m.smodel, m.mode)
	}
	v = m.View()
	for _, want := range []string{"claude-opus-5-5 · all folders", "active — not kept by model", "folders", "/work/app"} {
		if !strings.Contains(v, want) {
			t.Errorf("opus: missing %q in\n%s", want, v)
		}
	}
	if strings.Contains(v, "gpt-6-astra") {
		t.Errorf("opus: Codex's model still counted\n%s", v)
	}
	// x clears the filters, c charts cost
	m = press(t, m, "x", "c")
	if m.smodel != "" || !strings.Contains(m.View(), "cost · a day") {
		t.Errorf("x, c:\n%s", m.View())
	}
	// today has nothing in the fixtures, and no chart of one day
	m = press(t, m, "t")
	if v := m.View(); !strings.Contains(v, "nothing in this range") || strings.Contains(v, "a day") {
		t.Errorf("today:\n%s", v)
	}
}

// The chart is a column a day with room for gaps, a week a column when
// the days don't fit, eighths of a block for what is between.
func TestSessChart(t *testing.T) {
	day := func(date string, n int) sessions.DayTotal {
		d := sessions.DayTotal{Date: date}
		d.Input = n
		return d
	}
	days := []sessions.DayTotal{day("2026-09-26", 800), day("2026-09-27", 0), day("2026-09-28", 100)}
	ls := sessChart(days, false, 80, 2)
	if len(ls) != 3 {
		t.Fatalf("%d lines: %q", len(ls), ls)
	}
	if ls[0] != "█      800" || ls[1] != "█   ▂  tokens · a day" || ls[2] != "Sep 26" {
		t.Errorf("%q", ls)
	}
	var many []sessions.DayTotal
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	for i := range 200 {
		many = append(many, day(start.AddDate(0, 0, i).Format(time.DateOnly), 1000))
	}
	ls = sessChart(many, false, 60, 4)
	if !strings.Contains(ls[len(ls)-2], "a week") || len([]rune(strings.Fields(ls[3])[0])) != 29 {
		t.Errorf("weeks: %q", ls)
	}
	if sessChart(days[:1], false, 80, 6) != nil {
		t.Error("a chart of one day")
	}
}
