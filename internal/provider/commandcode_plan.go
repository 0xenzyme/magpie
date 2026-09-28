package provider

// A Command Code subscription is a commandcode.ai plan (Pro, GOAT, Max,
// Ultra — every one but Go comes with API access). Signing in to it, as
// `cmd auth login` does, mints an API key for the account; that key is
// served on Command Code's Provider API (/provider/v1: Chat, Responses and
// Anthropic's Messages) and billed against the plan's own credits and its
// 5-hour and weekly limits, not as pay-as-you-go.
//
// The CLI's own account is read, never changed, from ~/.commandcode/
// auth.json. Further accounts are signed in by magpie with the CLI's own
// browser sign-in: commandcode.ai/studio/auth/cli sends the new key to a
// callback on this machine, which answers as the CLI's does; their keys
// are kept in logins.json.
//
// Its id is commandcode-plan: "commandcode" is the keyed preset's, which a
// provider added with a key from Studio may already have.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/catalog"
)

// CommandCodePlanID is the subscription's id, and its sign-in's.
const CommandCodePlanID = "commandcode-plan"

// Where Command Code's API and its sign-in page are; vars so tests can
// point them elsewhere.
var (
	cmdAPI    = "https://api.commandcode.ai"
	cmdStudio = "https://commandcode.ai"
)

// cmdModels are the plan's best-known models, before its list is fetched.
var cmdModels = []catalog.Model{
	{ID: "claude-sonnet-5", Name: "Claude Sonnet 5", Context: 1_000_000},
	{ID: "claude-opus-5-5", Name: "Claude Opus 5.5", Context: 1_000_000},
	{ID: "gpt-6-sol", Name: "GPT-6 Sol", Context: 1_050_000},
	{ID: "deepseek/deepseek-v4-pro", Name: "DeepSeek V4 Pro", Context: 1_000_000},
	{ID: "deepseek/deepseek-v4-flash", Name: "DeepSeek V4 Flash", Context: 1_000_000},
	{ID: "moonshotai/Kimi-K3", Name: "Kimi K3", Context: 1_000_000},
	{ID: "zai-org/GLM-5.3", Name: "GLM-5.3", Context: 1_000_000},
	{ID: "MiniMaxAI/MiniMax-M3", Name: "MiniMax M3", Context: 1_000_000},
}

// cmdAuth is an account's key, as auth.json and the sign-in name it.
type cmdAuth struct {
	APIKey   string `json:"apiKey"`
	UserID   string `json:"userId,omitempty"`
	UserName string `json:"userName,omitempty"`
	KeyName  string `json:"keyName,omitempty"`
}

// ---- the CLI's own account ----------------------------------------------------

func cmdAuthPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".commandcode", "auth.json")
}

// cmdOwn is the account Command Code's CLI is signed in to; ok is false
// when it has none.
func cmdOwn() (who string, a cmdAuth, ok bool) {
	if !readJSON(cmdAuthPath(), &a) || strings.TrimSpace(a.APIKey) == "" {
		return "", cmdAuth{}, false
	}
	return cmdWho(a), a, true
}

// cmdWho names an account: its user name, or its id.
func cmdWho(a cmdAuth) string {
	return firstNonEmpty(a.UserName, a.UserID, "Command Code")
}

// ---- the accounts -------------------------------------------------------------

type cmdLogin struct {
	Login
	auth cmdAuth
}

func cmdSaved(l savedLogin) (cmdAuth, bool) {
	var a cmdAuth
	if json.Unmarshal(l.Auth, &a) != nil || a.APIKey == "" {
		return cmdAuth{}, false
	}
	return a, true
}

// cmdLogins is every Command Code account signed in, the first in use first.
func cmdLogins() []cmdLogin {
	ownUser, own, hasOwn := cmdOwn()
	if !hasOwn {
		ownUser = ""
	}
	var out []cmdLogin
	for _, l := range sideLogins(CommandCodePlanID, ownUser, func(l savedLogin) bool {
		_, ok := cmdSaved(l)
		return ok
	}) {
		a := own
		if !l.saved.own() {
			a, _ = cmdSaved(l.saved)
		}
		out = append(out, cmdLogin{l.Login, a})
	}
	return out
}

func cmdSide() []sideLogin {
	var out []sideLogin
	for _, l := range cmdLogins() {
		out = append(out, sideLogin{Login: l.Login})
	}
	return out
}

func cmdLoginList() []Login { return loginsOf(cmdSide()) }

func switchCommandCodeLogin(user string) error {
	return switchSideLogin(CommandCodePlanID, user, cmdSide())
}

func setCommandCodeLoginOn(user string, on bool) error {
	return setSideLoginOn(CommandCodePlanID, user, on, cmdSide())
}

func forgetCommandCodeLogin(user string) error {
	return forgetSideLogin(CommandCodePlanID, user, "Command Code's own sign-in; sign out with cmd auth logout", cmdSide(), nil)
}

func commandCodeAccount() (Provider, bool) {
	ls := cmdLogins()
	if len(ls) == 0 {
		return Provider{}, false
	}
	return cmdProvider(ls[0].User, ls[0].Plan, ls[0].auth), true
}

// commandCodeAlsoOn is the Command Code accounts in use behind the first.
func commandCodeAlsoOn() []Provider {
	var out []Provider
	for _, l := range cmdLogins() {
		if !l.Active && l.On {
			out = append(out, cmdProvider(l.User, l.Plan, l.auth))
		}
	}
	return out
}

func cmdProvider(who, plan string, a cmdAuth) Provider {
	p := Provider{ID: CommandCodePlanID, Name: "Command Code Plan", Icon: "commandcode",
		Chat: cmdAPI + "/provider/v1", Responses: cmdAPI + "/provider/v1", Anthropic: cmdAPI + "/provider",
		Website: cmdStudio}
	acct := &Account{Agent: CommandCodePlanID, User: who, Plan: plan}
	acct.sign = func(ctx context.Context, req *http.Request, body []byte) error {
		req.Header.Del("Authorization")
		req.Header.Set("Authorization", "Bearer "+a.APIKey)
		req.Header.Set("x-api-key", a.APIKey)
		return nil
	}
	acct.models = func() []catalog.Model { return cmdModels }
	// the plan's list is the Provider API's, with what each model is
	// served on; it is asked as a keyed provider's is
	acct.fetch = func(ctx context.Context) ([]catalog.Model, error) {
		keyed := p
		keyed.Account, keyed.Key = nil, a.APIKey
		return keyed.Fetch(ctx)
	}
	p.Account = acct
	return p
}

// ---- allowance ----------------------------------------------------------------

// cmdPlans names the plans as the CLI does, by their id's start.
var cmdPlans = []struct{ id, name string }{
	{"individual-provider", "Provider"}, {"individual-goat", "GOAT"}, {"individual-ultra", "Ultra"},
	{"individual-max", "Max"}, {"individual-pro", "Pro"}, {"individual-go", "Go"}, {"teams-pro", "Teams Pro"},
}

func cmdPlanName(id string) string {
	id = strings.ReplaceAll(strings.ToLower(id), "_", "-")
	for _, p := range cmdPlans {
		if strings.HasPrefix(id, p.id) {
			return p.name
		}
	}
	return ""
}

type cmdWindow struct {
	Used    any `json:"used"`
	Cap     any `json:"cap"`
	ResetAt any `json:"resetAt"` // unix seconds or ms, or a time
}

type cmdCredits struct {
	Credits struct {
		PlanID    string `json:"planId"`
		Monthly   any    `json:"monthlyCredits"`
		Purchased any    `json:"purchasedCredits"`
		Free      any    `json:"freeCredits"`
	} `json:"credits"`
	Windows *struct {
		FiveHour *cmdWindow `json:"fiveHour"`
		Weekly   *cmdWindow `json:"weekly"`
	} `json:"windowLimits"`
}

// cmdTime reads a reset time: unix seconds or milliseconds, or RFC 3339.
func cmdTime(v any) *time.Time {
	if s, ok := v.(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return &t
		}
	}
	n, ok := number(v)
	if !ok || n <= 0 {
		return nil
	}
	if n < 1e12 {
		n *= 1000
	}
	t := time.UnixMilli(int64(n))
	return &t
}

// cmdQuotaOf makes the credits reply into the plan's windows: the 5-hour
// and weekly limits, and what is left of the month's credits.
func cmdQuotaOf(q SubscriptionQuota, c cmdCredits) SubscriptionQuota {
	if n := cmdPlanName(c.Credits.PlanID); n != "" {
		q.Plan = n
	}
	if c.Windows != nil {
		for _, w := range []struct {
			name string
			span time.Duration
			w    *cmdWindow
		}{{"5 hours", 5 * time.Hour, c.Windows.FiveHour}, {"Weekly", 7 * 24 * time.Hour, c.Windows.Weekly}} {
			if w.w == nil {
				continue
			}
			used, ok1 := number(w.w.Used)
			limit, ok2 := number(w.w.Cap)
			if !ok1 || !ok2 || limit <= 0 {
				continue
			}
			q.Windows = append(q.Windows, QuotaWindow{Name: w.name, Used: min(100, 100*used/limit), Span: w.span,
				ResetsAt: cmdTime(w.w.ResetAt)})
		}
	}
	var left float64
	var known bool
	for _, v := range []any{c.Credits.Monthly, c.Credits.Purchased, c.Credits.Free} {
		if n, ok := number(v); ok {
			left += max(0, n)
			known = true
		}
	}
	if known {
		q.Balance = money("$", left)
	}
	return q
}

func cmdQuota(ctx context.Context, l Login, a cmdAuth) SubscriptionQuota {
	q := SubscriptionQuota{Provider: CommandCodePlanID, Name: "Command Code", Icon: "commandcode", Plan: l.Plan, User: l.User, Windows: []QuotaWindow{}}
	var c cmdCredits
	if err := accountJSON(ctx, cmdAPI+"/alpha/billing/credits", a.APIKey, nil, &c); err != nil {
		q.Error = err.Error()
		return q
	}
	return cmdQuotaOf(q, c)
}

func cmdLoginQuota(ctx context.Context, l Login) SubscriptionQuota {
	for _, c := range cmdLogins() {
		if strings.EqualFold(c.User, l.User) {
			return cmdQuota(ctx, l, c.auth)
		}
	}
	return SubscriptionQuota{Provider: CommandCodePlanID, Plan: l.Plan, Windows: []QuotaWindow{}, Error: "not signed in"}
}

// ---- signing in ---------------------------------------------------------------

// startCommandCodeSignIn is the CLI's browser sign-in: Studio asks the
// user to approve a key for this machine, then posts it (a form, or JSON
// from an older Studio) to the callback, which sends the browser on to a
// page saying it is done.
func startCommandCodeSignIn(s *signInFlow) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	q := url.Values{}
	q.Set("callback", fmt.Sprintf("http://127.0.0.1:%d/callback", port))
	q.Set("state", s.state)
	q.Set("mode", "redirect")
	srv := &http.Server{Handler: http.HandlerFunc(s.commandCodeCallback), ReadHeaderTimeout: 10 * time.Second}
	s.mu.Lock()
	s.st.URL = cmdStudio + "/studio/auth/cli?" + q.Encode()
	s.srv = srv
	s.mu.Unlock()
	go func() { _ = srv.Serve(ln) }()
	return nil
}

// cmdOrigins are the Studio pages that may post to the callback.
var cmdOrigins = []string{"https://commandcode.ai", "https://staging.commandcode.ai"}

func (s *signInFlow) commandCodeCallback(w http.ResponseWriter, r *http.Request) {
	origin := cmdOrigins[0]
	for _, o := range cmdOrigins {
		if r.Header.Get("Origin") == o {
			origin = o
		}
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
	}
	switch {
	case r.Method == http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
		return
	case r.URL.Path == "/callback/complete" && r.Method == http.MethodGet:
		st := s.status()
		if st.State == "done" {
			signInPage(w, true, "You're signed in", fmt.Sprintf("%s is added to magpie. You can close this tab.", st.User))
		} else {
			signInPage(w, false, "Sign-in didn't finish", firstNonEmpty(st.Error, "Start it again from magpie."))
		}
		return
	case r.URL.Path == "/cancel":
		s.finish(SignInState{State: "canceled"})
		w.WriteHeader(http.StatusNoContent)
		return
	case r.URL.Path != "/callback":
		http.NotFound(w, r)
		return
	case r.Method != http.MethodPost:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 10_000))
	var got struct {
		cmdAuth
		State string `json:"state"`
		Error string `json:"error"`
		Desc  string `json:"error_description"`
	}
	isJSON := strings.HasPrefix(strings.TrimSpace(strings.ToLower(r.Header.Get("Content-Type"))), "application/json")
	if isJSON {
		_ = json.Unmarshal(body, &got)
	} else {
		f, _ := url.ParseQuery(string(body))
		got.APIKey, got.UserID, got.UserName, got.KeyName = f.Get("apiKey"), f.Get("userId"), f.Get("userName"), f.Get("keyName")
		got.State, got.Error, got.Desc = f.Get("state"), f.Get("error"), f.Get("error_description")
	}
	answer := func(ok bool, msg string) {
		if isJSON {
			w.Header().Set("Content-Type", "application/json")
			if !ok {
				w.WriteHeader(http.StatusBadRequest)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": ok, "error": msg})
			return
		}
		// as the CLI answers: on to the page that says how it went
		w.Header().Set("Location", "/callback/complete?state="+url.QueryEscape(got.State))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusSeeOther)
	}
	if got.State != s.state {
		// not ours: someone else's page, or a stale tab
		w.WriteHeader(http.StatusForbidden)
		signInPage(w, false, "This link isn't from magpie's sign-in", "Start it again from magpie.")
		return
	}
	if s.status().State != "waiting" {
		answer(false, "this sign-in is over")
		return
	}
	if got.Error != "" {
		msg := firstNonEmpty(got.Desc, got.Error)
		if got.Error == "access_denied" {
			msg = "the sign-in was denied"
		}
		s.finish(SignInState{State: "failed", Error: msg})
		answer(false, msg)
		return
	}
	if got.APIKey == "" {
		answer(false, "Command Code sent back no key")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	who, plan, err := cmdSignedIn(ctx, got.cmdAuth)
	if err != nil {
		s.finish(SignInState{State: "failed", Error: err.Error()})
		answer(false, err.Error())
		return
	}
	a := got.cmdAuth
	a.UserName = who
	auth, _ := json.Marshal(a)
	ownUser, _, hasOwn := cmdOwn()
	if !hasOwn {
		ownUser = ""
	}
	if err := addSideLogin(savedLogin{Agent: CommandCodePlanID, User: who, Plan: plan, Auth: auth}, ownUser, func(savedLogin) {}); err != nil {
		s.finish(SignInState{State: "failed", Error: err.Error()})
		answer(false, err.Error())
		return
	}
	// the browser comes back for its page after this: the server stays up
	// a while for it, where finish would close it within a second
	s.mu.Lock()
	srv := s.srv
	s.srv = nil
	s.mu.Unlock()
	if srv != nil {
		time.AfterFunc(10*time.Second, func() { _ = srv.Close() })
	}
	s.finish(SignInState{State: "done", User: who, Plan: plan, Using: hasOwn && strings.EqualFold(ownUser, who)})
	answer(true, "")
}

// cmdSignedIn checks a new key and names its account, as the CLI does
// with whoami, and reads its plan.
func cmdSignedIn(ctx context.Context, a cmdAuth) (who, plan string, err error) {
	var me struct {
		User struct {
			ID       string `json:"id"`
			UserName string `json:"userName"`
			Name     string `json:"name"`
			Email    string `json:"email"`
		} `json:"user"`
	}
	if err := accountJSON(ctx, cmdAPI+"/alpha/whoami", a.APIKey, nil, &me); err != nil {
		return "", "", fmt.Errorf("Command Code didn't take the new key: %w", err)
	}
	who = firstNonEmpty(me.User.UserName, a.UserName, me.User.Email, me.User.ID, a.UserID)
	if who == "" {
		return "", "", fmt.Errorf("Command Code didn't say which account signed in")
	}
	var c cmdCredits
	if accountJSON(ctx, cmdAPI+"/alpha/billing/credits", a.APIKey, nil, &c) == nil {
		plan = cmdPlanName(c.Credits.PlanID)
	}
	return who, plan, nil
}
