package tui

// The sessions page: what the agents' sessions (Claude Code's, Codex's,
// OpenCode's, Pi's) spent, day by
// day, as the app's Sessions view shows it — a range, a model and a folder
// to narrow it to, the totals and a chart of the days.

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yetone/magpie/internal/agent"
	"github.com/yetone/magpie/internal/sessions"
	"github.com/yetone/magpie/internal/usage"
)

// sessRange is a range the page offers: its name and days (0 for all).
type sessRange struct {
	name string
	days int
}

var sessRanges = []sessRange{{"today", 1}, {"7 days", 7}, {"30 days", 30}, {"90 days", 90}, {"all", 0}}

const allModels, allFolders = "all models", "all folders"

// sessFilterMsg is a model or folder picked, "" for every one.
type sessFilterMsg struct {
	folder bool
	value  string
}

func (m *model) reloadSessions() {
	m.sstats = sessions.StatsFor(sessRanges[m.srange].days)
}

func (m model) updateSessions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(sessRanges)
	switch msg.String() {
	case "l", "right":
		m.srange = (m.srange + 1) % n
	case "h", "left":
		m.srange = (m.srange + n - 1) % n
	case "t":
		m.srange = 0
	case "w":
		m.srange = 1
	case "m":
		m.srange = 2
	case "A":
		m.srange = n - 1
	case "c":
		m.scost = !m.scost
		return m, nil
	case "M":
		m.openSessPick(false)
		return m, nil
	case "f":
		m.openSessPick(true)
		return m, nil
	case "x":
		m.smodel, m.sfolder = "", ""
		return m, nil
	default:
		return m, nil
	}
	m.reloadSessions()
	return m, nil
}

// openSessPick picks a model, or a folder, of those the range has under
// the other filter.
func (m *model) openSessPick(folder bool) {
	r := m.sstats.Sum(m.smodel, m.sfolder)
	all, shares, crumb := allModels, r.Models, "model"
	if folder {
		all, shares, crumb = allFolders, r.Folders, "folder"
	}
	items := []agent.Option{{Value: all}}
	for _, s := range shares {
		if s.Name != "" {
			items = append(items, agent.Option{Value: s.Name, Note: fmtTokens(s.Spent()) + " tokens"})
		}
	}
	m.pk = picker{
		crumbs: []string{"sessions", crumb},
		input:  newInput("filter"),
		items:  items,
		onPick: func(v string) tea.Cmd {
			if v == all {
				v = ""
			}
			return func() tea.Msg { return sessFilterMsg{folder, v} }
		},
	}
	m.pk.refilter()
	m.mode = modePick
	m.back = modeList
}

func (m model) viewSessions() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	b.WriteString(strings.Join(sessLines(m.sstats, m.srange, m.smodel, m.sfolder, m.scost, m.w-len(pad)-2, m.h), "\n"))
	return strings.TrimRight(b.String(), "\n")
}

// sessLines is the page below its header: the ranges and filters, the
// totals, the chart and the top models and folders, each line indented.
func sessLines(st sessions.Stats, rng int, model, folder string, byCost bool, width, height int) []string {
	var out []string
	add := func(s string) { out = append(out, pad+"  "+s) }
	var tabs []string
	for i, r := range sessRanges {
		if i == rng {
			tabs = append(tabs, sPill.Render(r.name))
		} else {
			tabs = append(tabs, sMuted.Render(" "+r.name+" "))
		}
	}
	filter := func(v, all string) string {
		if v == "" {
			return sMuted.Render(all)
		}
		return sNameOn.Render(v)
	}
	add(strings.Join(tabs, " ") + "    " + filter(model, allModels) + sFaint.Render(" · ") + filter(tildePath(folder), allFolders))
	add("")

	r := st.Sum(model, folder)
	if r.Spent() == 0 && r.CacheRead == 0 {
		if model != "" || folder != "" {
			add(sMuted.Render("no session matches · x clears the filters"))
		} else {
			add(sMuted.Render("nothing in this range · Claude Code's, Codex's, OpenCode's and Pi's sessions on this computer show up here"))
		}
		return out
	}
	c := sFaint.Render("no price")
	if r.Cost > 0 {
		c = sOK.Render(fmtCost(usage.Totals{Cost: r.Cost, Unpriced: len(r.Unpriced)}))
	}
	hit := ""
	if p := r.Input + r.CacheRead; r.CacheRead > 0 && p > 0 {
		hit = sMuted.Render(fmt.Sprintf(" (%d%% hit)", 100*r.CacheRead/p))
	}
	active := sessions.Duration(r.Active)
	if r.Active >= 0 {
		active += sMuted.Render(fmt.Sprintf(" on %d day%s", r.DaysUsed, plural(r.DaysUsed)))
	} else {
		active += sMuted.Render(" not kept by model")
	}
	add(sName.Render(fmtTokens(r.Spent())+" tokens") + sMuted.Render(" · ") + c + sMuted.Render(" · cache read ") + sText.Render(fmtTokens(r.CacheRead)) + hit + sMuted.Render(" · active ") + sText.Render(active))
	add(sMuted.Render("in " + fmtTokens(r.Input) + "  out " + fmtTokens(r.Output) + "  cache write " + fmtTokens(r.CacheWrite)))
	add("")

	chart := sessChart(r.Days, byCost, width, 6)
	for _, l := range chart {
		add(l)
	}
	if len(chart) > 0 {
		add("")
	}

	room := max(2, (height-18-len(chart))/2)
	table := func(head string, ss []sessions.Share, name func(string) string) {
		if len(ss) == 0 {
			return
		}
		add(sFaint.Render(head))
		w := 0
		for _, s := range ss[:min(len(ss), room)] {
			w = max(w, lipgloss.Width(name(s.Name)))
		}
		total := 0
		for _, s := range ss {
			total += s.Spent()
		}
		for i, s := range ss {
			if i == room {
				add(sFaint.Render(fmt.Sprintf("and %d more · f and M pick one", len(ss)-room)))
				break
			}
			sc := sFaint.Render("no price")
			if s.Cost > 0 {
				sc = sOK.Render(fmtCost(usage.Totals{Cost: s.Cost}))
			}
			add(sText.Render(padRight(name(s.Name), w)) + "  " + sMuted.Render(fmt.Sprintf("%3.0f%%", 100*float64(s.Spent())/float64(max(1, total)))) + "  " + sText.Render(padRight(fmtTokens(s.Spent()), 7)) + "  " + sc)
		}
		add("")
	}
	if model == "" {
		table("models", r.Models, func(s string) string { return s })
	}
	if folder == "" {
		table("folders", r.Folders, func(s string) string {
			if s == "" {
				return "(no folder)"
			}
			return trunc(tildePath(s), 48)
		})
	}
	return out
}

// sessChart is the days as a bar chart of block characters, height rows
// tall and at most width wide: a column a day, or a week (or more) when
// the days don't fit; its peak on the right, its first and last day under
// it. Nothing for a range of one day.
func sessChart(days []sessions.DayTotal, byCost bool, width, height int) []string {
	if len(days) < 2 || height < 1 {
		return nil
	}
	const label = 10 // room for the peak on the right
	cols := max(8, width-label)
	step := 1
	for (len(days)+step-1)/step > cols {
		step++
	}
	if step > 1 && step < 7 && (len(days)+6)/7 <= cols {
		step = 7
	}
	type bucket struct {
		date  string
		value float64
	}
	var bs []bucket
	for i := 0; i < len(days); i += step {
		b := bucket{date: days[i].Date}
		for _, d := range days[i:min(len(days), i+step)] {
			if byCost {
				b.value += d.Cost
			} else {
				b.value += float64(d.Spent())
			}
		}
		bs = append(bs, b)
	}
	peak := 0.0
	for _, b := range bs {
		peak = max(peak, b.value)
	}
	if peak == 0 {
		return nil
	}
	// a gap between the columns while there is room for it
	gap := ""
	if 2*len(bs) <= cols {
		gap = " "
	}
	fill := []rune(" ▁▂▃▄▅▆▇█")
	lines := make([]string, height)
	for row := range height {
		level := height - 1 - row
		var sb strings.Builder
		for i, b := range bs {
			if i > 0 {
				sb.WriteString(gap)
			}
			eighths := int(b.value * float64(height*8) / peak)
			if b.value > 0 {
				eighths = max(1, eighths)
			}
			sb.WriteRune(fill[min(8, max(0, eighths-level*8))])
		}
		lines[row] = sCursor.Render(sb.String())
	}
	top := fmtTokens(int(peak))
	if byCost {
		top = fmtCost(usage.Totals{Cost: peak})
	}
	lines[0] += "  " + sFaint.Render(top)
	unit := "a day"
	if step == 7 {
		unit = "a week"
	} else if step > 1 {
		unit = fmt.Sprintf("%d days", step)
	}
	lines[height-1] += "  " + sFaint.Render(map[bool]string{false: "tokens", true: "cost"}[byCost]+" · "+unit)
	span := len(bs) + (len(bs)-1)*len(gap)
	first, last := shortDate(bs[0].date), shortDate(bs[len(bs)-1].date)
	under := first
	if n := span - len(first) - len(last); n > 0 {
		under += strings.Repeat(" ", n) + last
	}
	return append(lines, sFaint.Render(under))
}

func shortDate(d string) string {
	t, err := time.ParseInLocation(time.DateOnly, d, time.Local)
	if err != nil {
		return d
	}
	return t.Format("Jan 2")
}

func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}
