package provider

// Starting a ChatGPT account's next window as soon as the last one resets.
// A Codex window starts at the account's first request after it resets, not
// at the reset: an account that sits idle for a day after its week ends has
// its next week end a day later too. So, while the setting is on, whichever
// magpie runs the gateway looks at each account's windows every few minutes
// and, when one has started over — its reset time passed, or its use fell
// back (OpenAI resetting everyone's limits early) — and nothing has used it
// since, sends that account one tiny request, as the gateway sends any.
// Once per reset: what it saw and did is kept in codex-warmup.json, so a
// restart doesn't send it again.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/settings"
)

const (
	// codexWarmEvery is how often the windows are looked at.
	codexWarmEvery = 5 * time.Minute
	// warmSlack is how far into a window it is still taken as unused: an
	// idle window's reset is always a whole window from now.
	warmSlack = 10 * time.Minute
	// warmTries is how many failed warm-ups one reset gets.
	warmTries = 3
)

// warmWindow is one window of an account as last seen, and when magpie
// last started it.
type warmWindow struct {
	ResetsAt time.Time `json:"resetsAt,omitzero"`
	Used     float64   `json:"used"`
	Warmed   time.Time `json:"warmed,omitzero"`
	// Pending is a reset whose warm-up failed, tried again next time
	// until warmTries have failed.
	Pending bool `json:"pending,omitempty"`
	Failed  int  `json:"failed,omitempty"`
}

// warmState is every account's windows, by lower-cased user and window name.
type warmState map[string]map[string]warmWindow

func codexWarmPath() string { return filepath.Join(filepath.Dir(Path()), "codex-warmup.json") }

func readWarmState(path string) warmState {
	st := warmState{}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &st)
	}
	return st
}

// CodexWarm is what came of looking at one account: the windows it was
// sent a request for, and why that failed.
type CodexWarm struct {
	User    string   `json:"user"`
	Windows []string `json:"windows"`
	Err     string   `json:"error,omitempty"`
}

// codexWarmer is the warm-up with what it reads and sends given, for tests;
// Claude's (claude_warmup.go) is one too.
type codexWarmer struct {
	path  string
	now   func() time.Time
	usage func(context.Context) map[string]SubscriptionQuota // by user
	send  func(ctx context.Context, user string) error
}

// warmNow reads each account's windows, the weekly ones or with which
// "all" the 5-hour ones too, sends a request to each account one of them
// has started over on, and keeps what it saw.
func (c codexWarmer) warmNow(ctx context.Context, which string) []CodexWarm {
	st := readWarmState(c.path)
	now := c.now()
	usage := c.usage(ctx)
	users := make([]string, 0, len(usage))
	for u := range usage {
		users = append(users, u)
	}
	slices.Sort(users)
	var out []CodexWarm
	changed := false
	for _, user := range users {
		q := usage[user]
		if q.Error != "" {
			continue // nothing known: what was seen stands
		}
		key := strings.ToLower(user)
		prev, next := st[key], map[string]warmWindow{}
		var due []string
		for _, w := range q.Windows {
			if w.Span <= 0 || w.Aside || w.Model != "" || w.Span < 24*time.Hour && which != "all" {
				continue
			}
			cur := asOf(w, now)
			p, seen := prev[w.Name]
			n := warmWindow{Used: cur.Used, Warmed: p.Warmed}
			if cur.ResetsAt != nil {
				n.ResetsAt = *cur.ResetsAt
			}
			if warmDue(p, seen, cur, now) {
				due = append(due, w.Name)
				// kept as it was until a request goes, so it is due again
				n = p
			}
			next[w.Name] = n
		}
		if len(due) > 0 {
			r := CodexWarm{User: user, Windows: due}
			err := c.send(ctx, user)
			for _, name := range due {
				n, p := next[name], prev[name]
				switch {
				case err == nil:
					n = windowSeen(q, name, now)
					n.Warmed = now
				case p.Failed+1 >= warmTries: // given up on this reset
					n = windowSeen(q, name, now)
				default:
					n.Pending, n.Failed = true, p.Failed+1
				}
				next[name] = n
			}
			if err != nil {
				r.Err = err.Error()
			}
			out = append(out, r)
		}
		if !mapsEqual(prev, next) {
			st[key], changed = next, true
		}
	}
	if changed {
		if b, err := json.MarshalIndent(st, "", "  "); err == nil {
			if err := writePrivate(c.path, append(b, '\n')); err != nil {
				log.Printf("codex warm-up: %v", err)
			}
		}
	}
	return out
}

// windowSeen is window name of q as now, as warmNow keeps it.
func windowSeen(q SubscriptionQuota, name string, now time.Time) warmWindow {
	for _, w := range q.Windows {
		if w.Name == name {
			cur := asOf(w, now)
			n := warmWindow{Used: cur.Used}
			if cur.ResetsAt != nil {
				n.ResetsAt = *cur.ResetsAt
			}
			return n
		}
	}
	return warmWindow{}
}

func mapsEqual(a, b map[string]warmWindow) bool {
	if len(a) != len(b) {
		return false
	}
	for k, x := range a {
		y, ok := b[k]
		if !ok || !x.ResetsAt.Equal(y.ResetsAt) || x.Used != y.Used || !x.Warmed.Equal(y.Warmed) || x.Failed != y.Failed || x.Pending != y.Pending {
			return false
		}
	}
	return true
}

// asOf is w as of now, its reset made absolute: a window read before a
// reset that has since passed (a cached read, or one kept through a
// failed fetch) has started over, from nothing.
func asOf(w QuotaWindow, now time.Time) QuotaWindow {
	if w.ResetsAt == nil && w.ResetSecs > 0 {
		t := now.Add(time.Duration(w.ResetSecs) * time.Second)
		w.ResetsAt = &t
	}
	if w.ResetsAt != nil && !now.Before(*w.ResetsAt) {
		w.Used, w.ResetsAt = 0, nil
	}
	return w
}

// warmDue says whether a window seen as p (seen false the first time) and
// now as cur wants starting: it is unused, and has started over since p —
// or is seen for the first time. Use falling back is a reset whatever the
// window says, OpenAI resetting everyone's limits early among them. One
// whose warm-up failed is still due.
func warmDue(p warmWindow, seen bool, cur QuotaWindow, now time.Time) bool {
	switch {
	case seen && p.Pending:
		return true
	case !seen:
		return idle(cur, now)
	case p.Used-cur.Used >= 1:
		return true
	case !p.ResetsAt.IsZero() && !now.Before(p.ResetsAt):
		return idle(cur, now)
	}
	return false
}

// idle says whether a window (as of now) hasn't started: nothing used,
// and its reset a whole window away, as the backend reports one not yet
// begun — or not known.
func idle(w QuotaWindow, now time.Time) bool {
	return w.Used == 0 && (w.ResetsAt == nil || w.ResetsAt.Sub(now) >= w.Span-warmSlack)
}

// codexWarmUsage is each ChatGPT account's windows, the cached reads the
// Usage page and the switch to an account with room share; an account
// whose sign-in lapsed is left out.
func codexWarmUsage(ctx context.Context) map[string]SubscriptionQuota {
	u := LoginUsage(ctx, "codex")
	for _, l := range Logins("codex") {
		if l.Lapsed != "" {
			delete(u, l.User)
		}
	}
	return u
}

// warmCodexLogin sends the ChatGPT account user one tiny request, with its
// own sign-in: Codex's for the account Codex is on, logins.json's else.
func warmCodexLogin(ctx context.Context, user string) error {
	ls := Logins("codex")
	i := slices.IndexFunc(ls, func(l Login) bool { return strings.EqualFold(l.User, user) })
	if i < 0 {
		return fmt.Errorf("no ChatGPT account %q", user)
	}
	l := ls[i]
	token := func(ctx context.Context) (string, string, error) { return savedLoginToken(ctx, "codex", l.User) }
	if l.Active {
		token = func(ctx context.Context) (string, string, error) { return codexToken(ctx, codexAuthPath()) }
	}
	model, effort, err := warmModel(l.User)
	if err != nil {
		return err
	}
	return warmCodex(ctx, codexSign(token), model, effort)
}

// warmModel is the model a warm-up asks, and at what effort: the account's
// smallest (a mini) or else the first it lists, at low.
func warmModel(user string) (model, effort string, err error) {
	ms, _, ok := catalog.Live(accountModels("codex", user))
	if !ok {
		ms, _, _ = catalog.Live("codex")
	}
	if len(ms) == 0 {
		ms = catalog.Codex()
	}
	if len(ms) == 0 {
		return "", "", errors.New("no Codex model is known yet")
	}
	m := ms[0]
	if i := slices.IndexFunc(ms, func(m catalog.Model) bool { return strings.Contains(m.ID, "mini") }); i >= 0 {
		m = ms[i]
	}
	switch {
	case slices.Contains(m.Efforts, "low"):
		effort = "low"
	case len(m.Efforts) > 0:
		effort = m.Efforts[0]
	}
	return m.ID, effort, nil
}

// warmCodex sends one "hi" to the ChatGPT backend as a Codex account's
// requests go (codexBody: Codex's instructions, not stored), and reads the
// reply to its end.
func warmCodex(ctx context.Context, sign func(context.Context, *http.Request, []byte) error, model, effort string) error {
	in := map[string]any{"model": model, "input": "hi"}
	if effort != "" {
		in["reasoning"] = map[string]any{"effort": effort}
	}
	b, _ := json.Marshal(in)
	body := codexBody(b)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, CodexBase+"/responses", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := sign(ctx, req, body); err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(msg)))
	}
	_, err = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	return err
}

// CodexWarmed is when magpie last started a window of each ChatGPT
// account, by user as kept (lower-cased).
func CodexWarmed() map[string]time.Time { return codexWarmedIn(codexWarmPath()) }

func codexWarmedIn(path string) map[string]time.Time {
	out := map[string]time.Time{}
	for user, ws := range readWarmState(path) {
		for _, w := range ws {
			if w.Warmed.After(out[user]) {
				out[user] = w.Warmed
			}
		}
	}
	return out
}

// KeepCodexWindowsWarm starts the ChatGPT accounts' windows as they reset,
// while settings say to, two minutes after it starts and every
// codexWarmEvery after that, until ctx ends.
func KeepCodexWindowsWarm(ctx context.Context) {
	w := codexWarmer{path: codexWarmPath(), now: time.Now, usage: codexWarmUsage, send: warmCodexLogin}
	keepWarm(ctx, "codex", w, func() string { return settings.Load().CodexWarmup })
}

// keepWarm runs w while which (the setting) says to: two minutes after it
// starts and every codexWarmEvery after that, until ctx ends.
func keepWarm(ctx context.Context, name string, w codexWarmer, which func() string) {
	t := time.NewTimer(2 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if which := which(); which != "" {
			c, cancel := context.WithTimeout(ctx, 2*time.Minute)
			for _, r := range w.warmNow(c, which) {
				if r.Err != "" {
					log.Printf("%s warm-up: %s's %s window: %s", name, r.User, strings.Join(r.Windows, ", "), r.Err)
				} else {
					log.Printf("%s warm-up: started %s's %s window", name, r.User, strings.Join(r.Windows, ", "))
				}
			}
			cancel()
		}
		t.Reset(codexWarmEvery)
	}
}
