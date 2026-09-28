package provider

// A WorkBuddy subscription is Tencent's CodeBuddy plan, which WorkBuddy
// (its desktop app, packaged from CodeBuddy Code) signs in to. The plan is
// served on an OpenAI-compatible endpoint under the account's own access
// token — a JWT the account refreshes — with the account's id and domain in
// headers, exactly as WorkBuddy sends them.
//
// WorkBuddy's own account is read, never changed, from its auth store,
// <shared data>/auth/workbuddy-desktop.info (plain JSON while credential
// protection is off, which it is by default); its access and refresh tokens
// and the account are taken as they sit there. Further accounts are signed
// in by magpie with WorkBuddy's own external-link flow (auth/state, then
// auth/token polled until ready, then login/account), and their tokens are
// kept in logins.json. A near-expired token is refreshed as WorkBuddy does,
// and a magpie-signed-in one's refresh is written back beside it.
//
// The plan's models are WorkBuddy's CLI agent's, from its product config
// (workbuddy_models.go); wbModels are them before that is read.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/catalog"
)

// Where WorkBuddy's plan and its sign-in are; vars so tests can point them
// elsewhere.
var (
	wbEndpoint = "https://copilot.tencent.com"
	// wbAppVersion is the plugin version the sign-in page is told.
	wbAppVersion = "2.0.0"
	// wbUAVersion is the WorkBuddy desktop version the User-Agent carries.
	// copilot.tencent.com's gateway rejects a request whose User-Agent it
	// doesn't recognise with code 10085 ("请求不合法"), so every call to it —
	// chat and billing alike — must go out as WorkBuddy/<version>.
	wbUAVersion = "5.5.6"
	// wbPollInterval is how often the sign-in asks whether it is ready.
	wbPollInterval = time.Second
)

// wbModels are the plan's models, WorkBuddy's CLI agent's coding picks, with
// the names and context windows its config gives them.
var wbModels = []catalog.Model{
	{ID: "auto", Name: "Auto", Context: 168_000},
	{ID: "hy4-preview-f", Name: "Hy4 preview", Context: 1_000_000},
	{ID: "hy3", Name: "Hy3", Context: 192_000},
	{ID: "hy3-x", Name: "Hy3-X", Context: 192_000},
	{ID: "deepseek-v4.1-flash", Name: "Deepseek-V4.1-Flash", Context: 1_000_000},
	{ID: "glm-5.3", Name: "GLM-5.3", Context: 1_000_000},
	{ID: "glm-5.3-flash", Name: "GLM-5.3-Flash", Context: 1_000_000},
	{ID: "glm-5.2", Name: "GLM-5.2", Context: 1_000_000},
	{ID: "glm-5.1", Name: "GLM-5.1", Context: 200_000},
	{ID: "glm-5v-turbo", Name: "GLM-5v-Turbo", Context: 200_000},
	{ID: "minimax-m3", Name: "MiniMax-M3", Context: 512_000},
	{ID: "kimi-k3-1", Name: "Kimi-K3", Context: 1_000_000},
	{ID: "kimi-k2.7", Name: "Kimi-K2.7-Code", Context: 256_000},
	{ID: "kimi-k2.6", Name: "Kimi-K2.6", Context: 256_000},
	{ID: "deepseek-v4-pro", Name: "Deepseek-V4-Pro", Context: 1_000_000},
}

// wbCreds is a WorkBuddy account's tokens and where they are served, as the
// auth store and the sign-in name them.
type wbCreds struct {
	UID              string `json:"uid"`
	Access           string `json:"accessToken"`
	Refresh          string `json:"refreshToken"`
	ExpiresAt        int64  `json:"expiresAt"`        // unix ms, when the access token lapses
	RefreshExpiresAt int64  `json:"refreshExpiresAt"` // unix ms, when the refresh token lapses
	Domain           string `json:"domain"`
	TokenType        string `json:"tokenType,omitempty"`
}

// wbAccount is a signed-in WorkBuddy account: which login it is (who, its
// plan, and whether it is the one in use), its tokens, and whether it is
// WorkBuddy's own sign-in (read-only) or one magpie added (in logins.json).
type wbAccount struct {
	Login
	creds wbCreds
	own   bool
}

// wbAPI is the API root for a domain: auth and billing sit under it. The
// account's domain (www.codebuddy.cn) names the site; requests otherwise go
// to the product endpoint.
func wbAPI() string { return strings.TrimRight(wbEndpoint, "/") }

// ---- WorkBuddy's own account --------------------------------------------------

// wbAuthPath is where WorkBuddy keeps the signed-in session, per platform,
// as its file-authentication-storage does: <shared data>/auth/<id>.info.
func wbAuthPath() string {
	home, _ := os.UserHomeDir()
	var base string
	switch runtime.GOOS {
	case "darwin":
		base = filepath.Join(home, "Library", "Application Support", "CodeBuddyExtension")
	case "windows":
		base = filepath.Join(home, "AppData", "Local", "CodeBuddyExtension")
	default:
		base = filepath.Join(home, ".local", "share", "CodeBuddyExtension")
	}
	return filepath.Join(base, "Data", "Public", "auth", "workbuddy-desktop.info")
}

// wbStoredSession is the shape of the auth store's session: the account and
// its tokens. The tokens are plain strings while credential protection is
// off (the default); when it is on they are objects magpie can't read, and
// the account is then taken as not readable.
type wbStoredSession struct {
	Account struct {
		UID         string          `json:"uid"`
		Nickname    json.RawMessage `json:"nickname"`
		PhoneNumber json.RawMessage `json:"phoneNumber"`
	} `json:"account"`
	Auth struct {
		AccessToken      json.RawMessage `json:"accessToken"`
		RefreshToken     json.RawMessage `json:"refreshToken"`
		ExpiresAt        int64           `json:"expiresAt"`
		ExpiresIn        int64           `json:"expiresIn"`
		RefreshExpiresAt int64           `json:"refreshExpiresAt"`
		RefreshExpiresIn int64           `json:"refreshExpiresIn"`
		Domain           string          `json:"domain"`
		TokenType        string          `json:"tokenType"`
	} `json:"auth"`
}

// wbString reads a JSON value that is a plain string, "" for anything else
// (an encrypted-field object magpie doesn't decrypt).
func wbString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// wbOwn is the account WorkBuddy is signed in to and its tokens; ok is
// false when it has none, or they are encrypted at rest.
func wbOwn() (who string, c wbCreds, ok bool) {
	var s wbStoredSession
	if !readJSON(wbAuthPath(), &s) {
		return "", wbCreds{}, false
	}
	access, refresh := wbString(s.Auth.AccessToken), wbString(s.Auth.RefreshToken)
	if s.Account.UID == "" || access == "" {
		return "", wbCreds{}, false
	}
	c = wbCreds{
		UID:              s.Account.UID,
		Access:           access,
		Refresh:          refresh,
		ExpiresAt:        s.Auth.ExpiresAt,
		RefreshExpiresAt: s.Auth.RefreshExpiresAt,
		Domain:           s.Auth.Domain,
		TokenType:        s.Auth.TokenType,
	}
	now := time.Now().UnixMilli()
	if c.ExpiresAt == 0 && s.Auth.ExpiresIn > 0 {
		c.ExpiresAt = now + s.Auth.ExpiresIn*1000
	}
	if c.RefreshExpiresAt == 0 && s.Auth.RefreshExpiresIn > 0 {
		c.RefreshExpiresAt = now + s.Auth.RefreshExpiresIn*1000
	}
	return wbWho(wbString(s.Account.Nickname), wbString(s.Account.PhoneNumber), s.Account.UID), c, true
}

// wbWho names a WorkBuddy account: its nickname, its phone number, or its id.
func wbWho(nickname, phone, id string) string {
	return firstNonEmpty(strings.TrimSpace(nickname), strings.TrimSpace(phone), id, "WorkBuddy")
}

// ---- the accounts -------------------------------------------------------------

func wbSavedCreds(l savedLogin) (wbCreds, bool) {
	var c wbCreds
	if json.Unmarshal(l.Auth, &c) != nil || c.Access == "" || c.UID == "" {
		return wbCreds{}, false
	}
	return c, true
}

// wbLogins is every WorkBuddy account signed in, the first in use first.
func wbLogins() []wbAccount {
	ownUser, own, hasOwn := wbOwn()
	if !hasOwn {
		ownUser = ""
	}
	var out []wbAccount
	for _, l := range sideLogins("workbuddy", ownUser, func(l savedLogin) bool {
		_, ok := wbSavedCreds(l)
		return ok
	}) {
		a := wbAccount{Login: l.Login, own: l.saved.own()}
		if a.own {
			a.creds = own
		} else {
			a.creds, _ = wbSavedCreds(l.saved)
		}
		out = append(out, a)
	}
	return out
}

func wbSide() []sideLogin {
	var out []sideLogin
	for _, a := range wbLogins() {
		out = append(out, sideLogin{Login: a.Login})
	}
	return out
}

func wbLoginList() []Login { return loginsOf(wbSide()) }

func switchWorkBuddyLogin(user string) error {
	return switchSideLogin("workbuddy", user, wbSide())
}

func setWorkBuddyLoginOn(user string, on bool) error {
	return setSideLoginOn("workbuddy", user, on, wbSide())
}

func forgetWorkBuddyLogin(user string) error {
	return forgetSideLogin("workbuddy", user, "WorkBuddy's own sign-in; sign out in WorkBuddy", wbSide(), nil)
}

func workBuddyAccount() (Provider, bool) {
	ls := wbLogins()
	if len(ls) == 0 {
		return Provider{}, false
	}
	return wbProvider(ls[0]), true
}

// workBuddyAlsoOn is the WorkBuddy accounts in use behind the first.
func workBuddyAlsoOn() []Provider {
	var out []Provider
	for _, a := range wbLogins() {
		if !a.Active && a.On {
			out = append(out, wbProvider(a))
		}
	}
	return out
}

func wbProvider(a wbAccount) Provider {
	acct := &Account{Agent: "workbuddy", User: a.User, Plan: a.Plan}
	acct.sign = func(ctx context.Context, req *http.Request, body []byte) error {
		c, err := wbFresh(ctx, a)
		if err != nil {
			return err
		}
		req.Header.Del("Authorization")
		req.Header.Set("Authorization", "Bearer "+c.Access)
		req.Header.Set("X-User-Id", c.UID)
		req.Header.Set("X-Domain", wbDomain(c))
		req.Header.Set("X-Product", "SaaS")
		req.Header.Set("X-IDE-Type", "WorkBuddy")
		req.Header.Set("User-Agent", "WorkBuddy/"+wbUAVersion)
		return nil
	}
	acct.models = func() []catalog.Model { return wbModels }
	acct.fetch = func(ctx context.Context) ([]catalog.Model, error) {
		ms, err := wbFetchModels(ctx, acct.sign)
		if err != nil {
			return nil, err
		}
		return ms, catalog.SaveLive("workbuddy", wbAPI()+"/v2", ms)
	}
	return Provider{ID: "workbuddy", Name: "WorkBuddy", Icon: "workbuddy-color", Chat: wbAPI() + "/v2", Website: "https://www.codebuddy.cn", Account: acct}
}

// wbDomain is the X-Domain a request carries: the account's own domain, or
// the endpoint's authority.
func wbDomain(c wbCreds) string {
	if c.Domain != "" {
		return c.Domain
	}
	if u, err := url.Parse(wbAPI()); err == nil && u.Host != "" {
		return u.Host
	}
	return ""
}

// ---- tokens -------------------------------------------------------------------

// wbTokens caches each account's freshest tokens (by uid), so a token
// refreshed for one request is used by the next; WorkBuddy's own file is
// never written, and a magpie-added account's refresh goes to logins.json.
var wbTokens = struct {
	sync.Mutex
	m map[string]wbCreds
}{m: map[string]wbCreds{}}

// wbFresh is a's tokens, refreshed when the access token is about to lapse.
func wbFresh(ctx context.Context, a wbAccount) (wbCreds, error) {
	wbTokens.Lock()
	c := a.creds
	if cached, ok := wbTokens.m[a.creds.UID]; ok && cached.ExpiresAt >= c.ExpiresAt {
		c = cached
	}
	wbTokens.Unlock()

	if c.Access != "" && !wbNearExpiry(c.ExpiresAt) {
		return c, nil
	}
	if c.Refresh == "" || (c.RefreshExpiresAt > 0 && time.Now().UnixMilli() >= c.RefreshExpiresAt) {
		if c.Access != "" {
			return c, nil // no way to refresh; let the request try what there is
		}
		return wbCreds{}, errors.New("this WorkBuddy account is signed out; sign in again")
	}
	refreshed, err := wbRefresh(ctx, c)
	if err != nil {
		if c.Access != "" {
			return c, nil // a refresh hiccup: the current token may still work
		}
		return wbCreds{}, err
	}
	wbTokens.Lock()
	wbTokens.m[refreshed.UID] = refreshed
	wbTokens.Unlock()
	if !a.own {
		wbSaveCreds(a.User, refreshed)
	}
	return refreshed, nil
}

// wbNearExpiry is true within a minute of a token's end (or when unknown).
func wbNearExpiry(expiresAt int64) bool {
	if expiresAt == 0 {
		return false // the store didn't say; trust it until refused
	}
	return time.Now().UnixMilli() >= expiresAt-60_000
}

// wbRefresh trades a refresh token for a fresh access token, as WorkBuddy's
// auth provider does: POST /v2/plugin/auth/token/refresh with the refresh
// token in a header.
func wbRefresh(ctx context.Context, c wbCreds) (wbCreds, error) {
	var got wbRefreshed
	err := wbCall(ctx, http.MethodPost, wbAPI()+"/v2/plugin/auth/token/refresh", map[string]string{
		"X-Refresh-Token":       c.Refresh,
		"X-Auth-Refresh-Source": "plugin",
		"X-Domain":              wbDomain(c),
	}, map[string]any{}, &got)
	if err != nil {
		return wbCreds{}, fmt.Errorf("WorkBuddy token refresh: %w", err)
	}
	if got.AccessToken == "" {
		return wbCreds{}, errors.New("WorkBuddy gave no refreshed token")
	}
	return wbMergeRefreshed(c, got), nil
}

type wbRefreshed struct {
	AccessToken      string `json:"accessToken"`
	RefreshToken     string `json:"refreshToken"`
	ExpiresAt        int64  `json:"expiresAt"`
	ExpiresIn        int64  `json:"expiresIn"`
	RefreshExpiresAt int64  `json:"refreshExpiresAt"`
	RefreshExpiresIn int64  `json:"refreshExpiresIn"`
	Domain           string `json:"domain"`
	TokenType        string `json:"tokenType"`
}

func wbMergeRefreshed(c wbCreds, got wbRefreshed) wbCreds {
	now := time.Now().UnixMilli()
	out := c
	out.Access = got.AccessToken
	if got.RefreshToken != "" {
		out.Refresh = got.RefreshToken
	}
	if got.Domain != "" {
		out.Domain = got.Domain
	}
	if got.TokenType != "" {
		out.TokenType = got.TokenType
	}
	switch {
	case got.ExpiresAt > 0:
		out.ExpiresAt = got.ExpiresAt
	case got.ExpiresIn > 0:
		out.ExpiresAt = now + got.ExpiresIn*1000
	}
	switch {
	case got.RefreshExpiresAt > 0:
		out.RefreshExpiresAt = got.RefreshExpiresAt
	case got.RefreshExpiresIn > 0:
		out.RefreshExpiresAt = now + got.RefreshExpiresIn*1000
	}
	return out
}

// wbSaveCreds writes a magpie-added account's refreshed tokens back to
// logins.json, so the next run starts from them.
func wbSaveCreds(user string, c wbCreds) {
	_ = editSideLogin("workbuddy", user, func(ls []savedLogin, i int) ([]savedLogin, error) {
		if ls[i].own() {
			return ls, nil // never write WorkBuddy's own file
		}
		auth, err := json.Marshal(c)
		if err != nil {
			return ls, err
		}
		ls[i].Auth = auth
		ls[i].Renewed = time.Now().UTC().Truncate(time.Second)
		return ls, nil
	})
}

// ---- allowance ----------------------------------------------------------------

// wbQuota is a WorkBuddy account's credit allowance, from its resource
// summary: the plan's credits used against what the cycle grants.
func wbQuota(ctx context.Context, a wbAccount) SubscriptionQuota {
	q := SubscriptionQuota{Provider: "workbuddy", Name: "WorkBuddy", Icon: "workbuddy-color", Plan: a.Plan, User: a.User, Windows: []QuotaWindow{}}
	c, err := wbFresh(ctx, a)
	if err != nil {
		q.Error = err.Error()
		return q
	}
	// The resource-summary meter (unlike the older /v2/billing meters) has
	// no /v2 gateway prefix — WorkBuddy asks it at /billing/meter/... on
	// both desktop and web.
	var sum wbResourceSummary
	if err := wbCall(ctx, http.MethodPost, wbAPI()+"/billing/meter/get-user-resource-summary", wbAuthHeaders(c), map[string]any{}, &sum); err != nil {
		q.Error = err.Error()
		return q
	}
	if sum.IsPaidUser {
		q.Plan = firstNonEmpty(a.Plan, "Pro")
	} else {
		q.Plan = firstNonEmpty(a.Plan, "Free")
	}
	var total, used float64
	for _, p := range sum.Packages {
		total += float64(p.CycleTotalCapacity)
		used += float64(p.CycleUsedCapacity)
	}
	if total > 0 {
		w := QuotaWindow{Name: "Credits", Used: 100 * used / total, Display: fmt.Sprintf("%s / %s", compactNumber(used), compactNumber(total))}
		q.Windows = append(q.Windows, w)
	}
	return q
}

type wbResourceSummary struct {
	Packages []struct {
		PackageCode         string `json:"PackageCode"`
		CycleTotalCapacity  wbNum  `json:"CycleTotalCapacity"`
		CycleRemainCapacity wbNum  `json:"CycleRemainCapacity"`
		CycleUsedCapacity   wbNum  `json:"CycleUsedCapacity"`
	} `json:"Packages"`
	SubscriptionPackageCode string `json:"SubscriptionPackageCode"`
	IsPaidUser              bool   `json:"IsPaidUser"`
}

// wbNum is a capacity the billing API sends as a JSON string ("3300",
// "438.88000002"), though it occasionally comes as a bare number; it parses
// either, and an empty or unparseable value reads as 0.
type wbNum float64

func (n *wbNum) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*n = wbNum(f)
	return nil
}

func wbAuthHeaders(c wbCreds) map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + c.Access,
		"X-User-Id":     c.UID,
		"X-Domain":      wbDomain(c),
		"X-Product":     "SaaS",
		"X-IDE-Type":    "WorkBuddy",
	}
}

func wbLoginQuota(ctx context.Context, l Login) SubscriptionQuota {
	for _, a := range wbLogins() {
		if strings.EqualFold(a.User, l.User) {
			return wbQuota(ctx, a)
		}
	}
	return SubscriptionQuota{Provider: "workbuddy", Plan: l.Plan, Windows: []QuotaWindow{}, Error: "not signed in"}
}

// ---- WorkBuddy's API ----------------------------------------------------------

// wbError is a WorkBuddy business error: its code lets a poll loop tell a
// "come back" answer (retry) from a real failure.
type wbError struct {
	code int
	msg  string
}

func (e *wbError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return fmt.Sprintf("error %d", e.code)
}

// WorkBuddy's retry codes: the token or the account isn't ready yet.
const (
	wbRetryToken   = 11217
	wbRetryAccount = 12151
)

// wbCall asks one of WorkBuddy's JSON endpoints, which wrap what they say in
// {code, msg, data}: code 0 is a success. On a non-zero code it returns a
// *wbError carrying it.
func wbCall(ctx context.Context, method, u string, headers map[string]string, body, dst any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "WorkBuddy/"+wbUAVersion)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var env struct {
		Code json.Number     `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(b, &env)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		if code, _ := env.Code.Int64(); code != 0 {
			return &wbError{code: int(code), msg: env.Msg}
		}
		if env.Msg != "" {
			return &wbError{code: res.StatusCode, msg: env.Msg}
		}
		return &accountStatusError{status: res.StatusCode}
	}
	if code, _ := env.Code.Int64(); code != 0 {
		return &wbError{code: int(code), msg: env.Msg}
	}
	if dst == nil || len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	return json.Unmarshal(env.Data, dst)
}

// ---- signing in ---------------------------------------------------------------

// startWorkBuddySignIn is WorkBuddy's own external-link sign-in: the app
// asks for a state and a page, opens the page for the user to sign in, then
// polls for the token and the account.
func startWorkBuddySignIn(s *signInFlow) error {
	ctx, cancel := context.WithCancel(context.Background())
	var state struct {
		State   string `json:"state"`
		AuthURL string `json:"authUrl"`
	}
	stateHeaders := map[string]string{
		"X-No-Authorization":   "true",
		"X-No-User-Id":         "true",
		"X-No-Enterprise-Id":   "true",
		"X-No-Department-Info": "true",
	}
	if err := wbCall(ctx, http.MethodPost, wbAPI()+"/v2/plugin/auth/state?platform=workbuddy", stateHeaders, map[string]any{}, &state); err != nil {
		cancel()
		return fmt.Errorf("WorkBuddy sign-in: %w", err)
	}
	u, err := url.Parse(state.AuthURL)
	if state.State == "" || err != nil || u.Scheme != "https" {
		cancel()
		return errors.New("WorkBuddy gave no sign-in page")
	}
	sid := make([]byte, 16)
	_, _ = rand.Read(sid)
	q := u.Query()
	q.Set("version", wbAppVersion)
	q.Set("loginSessionId", hex.EncodeToString(sid))
	u.RawQuery = q.Encode()
	s.mu.Lock()
	s.st.URL = u.String()
	s.stop = cancel
	s.mu.Unlock()

	go func() {
		defer cancel()
		fail := func(msg string) { s.finish(SignInState{State: "failed", Error: msg}) }
		deadline := time.Now().Add(5 * time.Minute)
		token, ok := wbPoll(ctx, deadline, "/v2/plugin/auth/token?state="+url.QueryEscape(state.State), nil, wbRetryToken, fail)
		if !ok {
			return
		}
		var tok wbRefreshed
		if json.Unmarshal(token, &tok) != nil || tok.AccessToken == "" {
			fail("WorkBuddy gave no token")
			return
		}
		c := wbMergeRefreshed(wbCreds{}, tok)
		acctHeaders := map[string]string{
			"Authorization":      "Bearer " + c.Access,
			"X-Domain":           wbDomain(c),
			"X-No-User-Id":       "true",
			"X-No-Enterprise-Id": "true",
		}
		account, ok := wbPoll(ctx, deadline, "/v2/plugin/login/account?state="+url.QueryEscape(state.State), acctHeaders, wbRetryAccount, fail)
		if !ok {
			return
		}
		var acc struct {
			UID         string `json:"uid"`
			Nickname    string `json:"nickname"`
			PhoneNumber string `json:"phoneNumber"`
		}
		if json.Unmarshal(account, &acc) != nil || acc.UID == "" {
			fail("WorkBuddy gave no account")
			return
		}
		c.UID = acc.UID
		who := wbWho(acc.Nickname, acc.PhoneNumber, acc.UID)
		auth, _ := json.Marshal(c)
		if err := addSideLogin(savedLogin{Agent: "workbuddy", User: who, Auth: auth}, func(savedLogin) {}); err != nil {
			fail(err.Error())
			return
		}
		wbTokens.Lock()
		wbTokens.m[c.UID] = c
		wbTokens.Unlock()
		ownUser, _, hasOwn := wbOwn()
		s.finish(SignInState{State: "done", User: who, Using: hasOwn && strings.EqualFold(ownUser, who)})
	}()
	return nil
}

// wbPoll asks path every second until it answers with data, giving up at
// the deadline. A retry code (the answer isn't ready) waits and asks again;
// any other error ends the sign-in through fail.
func wbPoll(ctx context.Context, deadline time.Time, path string, headers map[string]string, retryCode int, fail func(string)) (json.RawMessage, bool) {
	for {
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(wbPollInterval):
		}
		if time.Now().After(deadline) {
			fail("the sign-in expired; start again")
			return nil, false
		}
		var raw json.RawMessage
		err := wbCall(ctx, http.MethodGet, wbAPI()+path, headers, nil, &raw)
		switch {
		case ctx.Err() != nil:
			return nil, false
		case err == nil && len(raw) > 0 && string(raw) != "null":
			return raw, true
		case err == nil:
			continue // ready, but empty: ask again
		}
		var we *wbError
		if errors.As(err, &we) && we.code == retryCode {
			continue // not ready yet
		}
		var st *accountStatusError
		if errors.As(err, &st) && (st.status == 408 || st.status == 429) {
			continue // a hiccup: ask again
		}
		fail("WorkBuddy sign-in: " + err.Error())
		return nil, false
	}
}
