package provider

// Spending a Codex rate-limit reset by itself, for the accounts the user
// said may: when a request finds the account's weekly window used up and
// no other account can take it, one reset starts the windows again and the
// request goes through. Only the week's window counts — five hours pass on
// their own — and one a week at most, so a week's heavy use doesn't eat
// every reset the account holds. What was spent, and until when that week
// ran, is kept in codex-autoreset.json, so a restart doesn't spend another.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/settings"
)

// CodexAutoReset says whether the Codex account user spends a reset by
// itself once its week is used up.
func CodexAutoReset(user string) bool {
	return user != "" && slices.Contains(settings.Load().CodexAutoReset, strings.ToLower(user))
}

// CodexSignedIn is the ChatGPT account Codex is signed in to now.
func CodexSignedIn() (string, bool) {
	l, ok := liveLogin("codex")
	return l.User, ok
}

// SetCodexAutoReset turns that on or off for user.
func SetCodexAutoReset(user string, on bool) error {
	user = strings.ToLower(strings.TrimSpace(user))
	s := settings.Load()
	s.CodexAutoReset = slices.DeleteFunc(s.CodexAutoReset, func(u string) bool { return u == user })
	if on && user != "" {
		s.CodexAutoReset = append(s.CodexAutoReset, user)
	}
	return settings.Save(s)
}

var autoReset autoResets

type autoResets struct {
	sync.Mutex
	path string // the file read, read again when that changes (a new HOME)
	// spent: by account, the end of the week a reset was spent in, when,
	// and what spending it did — for the requests that found the same week
	// used up while it was spent, which go through on it too
	spent map[string]autoResetSpent
	// next: by account, when to look again after a look that spent none
	next map[string]time.Time
}

type autoResetSpent struct {
	Until time.Time    `json:"until"`
	At    time.Time    `json:"at"`
	Out   ResetOutcome `json:"outcome"`
}

// autoResetWait is how long an account is left before it is looked at
// again: its week not used up, or a reset that couldn't be spent.
var autoResetWait = map[bool]time.Duration{false: time.Minute, true: 10 * time.Minute}

func autoResetPath() string { return filepath.Join(settings.Dir(), "codex-autoreset.json") }

// AutoUseCodexReset spends one of the Codex account user's resets (the one
// Codex is signed in to when "") if the user turned that on for it, its
// weekly window is used up, it holds one, and none was spent by itself in
// this week yet. Code is "" when none was tried; the caller asks again
// only on "reset".
func AutoUseCodexReset(ctx context.Context, user string) (ResetOutcome, error) {
	if user == "" {
		live, ok := liveLogin("codex")
		if !ok {
			return ResetOutcome{}, nil
		}
		user = live.User
	}
	if !CodexAutoReset(user) {
		return ResetOutcome{}, nil
	}
	key := strings.ToLower(user)
	asked := time.Now()
	a := &autoReset
	a.Lock() // one look at a time: those waiting take what it did
	defer a.Unlock()
	a.load()
	now := time.Now()
	if s, ok := a.spent[key]; ok {
		if asked.Before(s.At) {
			// asked while it was being spent, for the same week used up
			return s.Out, nil
		}
		if now.Before(s.Until) {
			return ResetOutcome{}, nil
		}
	}
	if now.Before(a.next[key]) {
		return ResetOutcome{}, nil
	}
	lookCtx := ViaLogin(ctx, "codex", user)
	who, tok, accountID, err := codexUserToken(lookCtx, user)
	if err != nil {
		a.next[key] = now.Add(autoResetWait[true])
		return ResetOutcome{}, err
	}
	_, windows, resets, err := codexWindows(ViaLogin(ctx, "codex", who), tok, accountID)
	if err != nil {
		a.next[key] = now.Add(autoResetWait[true])
		return ResetOutcome{}, err
	}
	week := weekUsedUp(windows, now)
	if week == nil || resets == nil || resets.Count <= 0 {
		a.next[key] = now.Add(autoResetWait[week != nil])
		return ResetOutcome{}, nil
	}
	out, err := UseCodexReset(ctx, who)
	if err != nil || out.Code != "reset" {
		a.next[key] = now.Add(autoResetWait[true])
		return out, err
	}
	a.spent[key] = autoResetSpent{Until: *week, At: time.Now(), Out: out}
	a.save()
	return out, nil
}

// weekUsedUp is when the used-up weekly window of windows ends, nil when
// none is used up: the five hours' being full doesn't count.
func weekUsedUp(windows []QuotaWindow, now time.Time) *time.Time {
	for _, w := range windows {
		if w.Span >= 24*time.Hour && !w.Aside && w.Used >= 100 && w.ResetsAt != nil && w.ResetsAt.After(now) {
			return w.ResetsAt
		}
	}
	return nil
}

// load reads what was spent, once; a.Lock is held.
func (a *autoResets) load() {
	if a.path == autoResetPath() {
		return
	}
	a.path = autoResetPath()
	a.spent, a.next = map[string]autoResetSpent{}, map[string]time.Time{}
	if b, err := os.ReadFile(autoResetPath()); err == nil {
		_ = json.Unmarshal(b, &a.spent)
		if a.spent == nil {
			a.spent = map[string]autoResetSpent{}
		}
	}
}

// save writes it; a.Lock is held.
func (a *autoResets) save() {
	now := time.Now()
	for k, s := range a.spent { // weeks gone by say nothing any more
		if now.After(s.Until) {
			delete(a.spent, k)
		}
	}
	b, _ := json.MarshalIndent(a.spent, "", "  ")
	_ = os.MkdirAll(settings.Dir(), 0o755)
	_ = os.WriteFile(a.path, append(b, '\n'), 0o644)
}
