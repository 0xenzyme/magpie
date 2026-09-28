package provider

// Starting a Claude account's next window as soon as the last one resets,
// as codex_warmup.go does a ChatGPT account's: a Claude window, the 5-hour
// one and the weekly one, starts at the account's first request after it
// resets. The request goes through Claude Code itself, as the gateway's
// Claude Subscription does, so it is Claude Code's own; the gateway, which
// runs Claude Code, says how.

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/settings"
)

func claudeWarmPath() string { return filepath.Join(filepath.Dir(Path()), "claude-warmup.json") }

// claudeWarmUsage is each Claude account's windows, the cached reads the
// Usage page shares; an account whose sign-in lapsed is left out.
func claudeWarmUsage(ctx context.Context) map[string]SubscriptionQuota {
	u := LoginUsage(ctx, "claude")
	for _, l := range Logins("claude") {
		if l.Lapsed != "" {
			delete(u, l.User)
		}
	}
	return u
}

// warmClaudeLogin is a warm-up's send for the Claude accounts: send asks
// Claude Code, with oauth the sign-in of a saved account, "" for the one
// Claude Code is signed in to.
func warmClaudeLogin(send func(ctx context.Context, oauth string) error) func(context.Context, string) error {
	return func(ctx context.Context, user string) error {
		ls := Logins("claude")
		i := slices.IndexFunc(ls, func(l Login) bool { return strings.EqualFold(l.User, user) })
		if i < 0 {
			return fmt.Errorf("no Claude account %q", user)
		}
		oauth := ""
		if !ls[i].Active {
			tok, _, err := savedLoginToken(ctx, "claude", ls[i].User)
			if err != nil {
				return err
			}
			oauth = tok
		}
		return send(ctx, oauth)
	}
}

// ClaudeWarmed is when magpie last started a window of each Claude account,
// by user as kept (lower-cased).
func ClaudeWarmed() map[string]time.Time { return codexWarmedIn(claudeWarmPath()) }

// KeepClaudeWindowsWarm starts the Claude accounts' windows as they reset,
// while settings say to, each with one request send makes.
func KeepClaudeWindowsWarm(ctx context.Context, send func(ctx context.Context, oauth string) error) {
	w := codexWarmer{path: claudeWarmPath(), now: time.Now, usage: claudeWarmUsage, send: warmClaudeLogin(send)}
	keepWarm(ctx, "claude", w, func() (string, string) { s := settings.Load(); return s.ClaudeWarmup, s.ClaudeWarmAt })
}
