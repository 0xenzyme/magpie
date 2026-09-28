package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGoogle is Google's token endpoint and Code Assist, as far as magpie
// uses them.
type fakeGoogle struct {
	mu        sync.Mutex
	refreshes int
	loads     []map[string]any
	onboards  []map[string]any
	load      string // loadCodeAssist's reply
	onboard   string // onboardUser's reply
	heads     map[string]http.Header
}

func (f *fakeGoogle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.heads == nil {
		f.heads = map[string]http.Header{}
	}
	f.heads[r.URL.Path] = r.Header
	b, _ := io.ReadAll(r.Body)
	var body map[string]any
	json.Unmarshal(b, &body)
	switch {
	case r.URL.Path == "/token":
		f.refreshes++
		form, _ := url.ParseQuery(string(b))
		if form.Get("refresh_token") == "" || form.Get("grant_type") != "refresh_token" {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		io.WriteString(w, `{"access_token":"fresh-token","expires_in":3600}`)
	case strings.HasSuffix(r.URL.Path, ":loadCodeAssist"):
		f.loads = append(f.loads, body)
		io.WriteString(w, f.load)
	case strings.HasSuffix(r.URL.Path, ":onboardUser"):
		f.onboards = append(f.onboards, body)
		io.WriteString(w, f.onboard)
	case strings.HasSuffix(r.URL.Path, "latest-arm64-mac.yml"):
		io.WriteString(w, "version: 3.1.4\npath: x.zip\n")
	default:
		http.NotFound(w, r)
	}
}

func googleSandbox(t *testing.T, f *fakeGoogle) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	oldToken, oldProd, oldDaily, oldVer, oldPoll := googleTokenURL, codeAssistProd, codeAssistDaily, antigravityVersionURL, onboardPoll
	googleTokenURL, codeAssistProd, codeAssistDaily = srv.URL+"/token", srv.URL+"/prod", srv.URL+"/daily"
	antigravityVersionURL, onboardPoll = srv.URL+"/latest-arm64-mac.yml", time.Millisecond
	resetGoogleState()
	t.Cleanup(func() {
		googleTokenURL, codeAssistProd, codeAssistDaily, antigravityVersionURL, onboardPoll = oldToken, oldProd, oldDaily, oldVer, oldPoll
		resetGoogleState()
	})
}

func resetGoogleState() {
	googleState.Lock()
	googleState.tokens = map[string]googleAuth{}
	googleState.projects = map[string]googleProject{}
	googleState.Unlock()
	antigravityVer.Lock()
	antigravityVer.v, antigravityVer.at = "", time.Time{}
	antigravityVer.Unlock()
}

func writeGeminiLogin(t *testing.T, env string) {
	t.Helper()
	dir := geminiDir()
	os.MkdirAll(dir, 0o700)
	creds := `{"access_token":"old","refresh_token":"rt-own","expiry_date":1}`
	os.WriteFile(filepath.Join(dir, "oauth_creds.json"), []byte(creds), 0o600)
	os.WriteFile(filepath.Join(dir, "google_accounts.json"), []byte(`{"active":"me@example.com","old":[]}`), 0o600)
	if env != "" {
		os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600)
	}
}

// Gemini CLI's own sign-in is found, and its expired token is refreshed in
// memory: the file Gemini CLI keeps is left as it was.
func TestGeminiOwnLoginRefreshesInMemory(t *testing.T) {
	f := &fakeGoogle{}
	googleSandbox(t, f)
	writeGeminiLogin(t, "")
	before, _ := os.ReadFile(filepath.Join(geminiDir(), "oauth_creds.json"))
	g, ok := geminiOwnLogin()
	if !ok || g.user != "me@example.com" || !g.own {
		t.Fatalf("own login = %+v, %v", g, ok)
	}
	for i := 0; i < 2; i++ {
		tok, err := g.token(context.Background())
		if err != nil || tok != "fresh-token" {
			t.Fatalf("token = %q, %v", tok, err)
		}
	}
	if f.refreshes != 1 {
		t.Errorf("refreshed %d times, want once", f.refreshes)
	}
	after, _ := os.ReadFile(filepath.Join(geminiDir(), "oauth_creds.json"))
	if !bytes.Equal(before, after) {
		t.Error("Gemini CLI's oauth_creds.json was written")
	}
}

// Only a sign-in Gemini CLI's own OAuth client minted is Gemini CLI's: one
// minted for another client (Antigravity's) can't be refreshed as Gemini
// CLI's and isn't listed; one without an ID token is taken as Gemini CLI's.
func TestGeminiOwnLoginOnlyGeminiCLIsClient(t *testing.T) {
	idToken := func(claims map[string]any) string {
		b, _ := json.Marshal(claims)
		return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
	}
	for _, c := range []struct {
		name, idToken string
		want          bool
	}{
		{"no id token", "", true},
		{"gemini cli", idToken(map[string]any{"aud": geminiApp.clientID, "azp": geminiApp.clientID}), true},
		{"antigravity", idToken(map[string]any{"aud": antigravityApp.clientID, "azp": antigravityApp.clientID}), false},
		{"antigravity aud only", idToken(map[string]any{"aud": antigravityApp.clientID}), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			googleSandbox(t, &fakeGoogle{})
			dir := geminiDir()
			os.MkdirAll(dir, 0o700)
			creds, _ := json.Marshal(map[string]any{"access_token": "old", "refresh_token": "rt-own", "expiry_date": 1, "id_token": c.idToken})
			os.WriteFile(filepath.Join(dir, "oauth_creds.json"), creds, 0o600)
			os.WriteFile(filepath.Join(dir, "google_accounts.json"), []byte(`{"active":"me@example.com","old":[]}`), 0o600)
			_, ok := geminiOwnLogin()
			if ok != c.want {
				t.Fatalf("own login found = %v, want %v", ok, c.want)
			}
			if n := len(googleLoginList("gemini")); n != map[bool]int{true: 1, false: 0}[c.want] {
				t.Errorf("gemini lists %d accounts", n)
			}
			if n := len(googleLoginList("antigravity")); n != 0 {
				t.Errorf("antigravity lists %d accounts", n)
			}
		})
	}
}

// Google turns individuals away from Gemini CLI's sign-in; the account
// then needs a project, and magpie says how to name one.
func TestGeminiNeedsAProject(t *testing.T) {
	f := &fakeGoogle{load: `{"allowedTiers":[{"id":"standard-tier","name":"Gemini Code Assist","userDefinedCloudaicompanionProject":true,"isDefault":true}],
		"ineligibleTiers":[{"reasonCode":"UNSUPPORTED_CLIENT","reasonMessage":"This client is no longer supported","tierId":"free-tier"}]}`}
	googleSandbox(t, f)
	writeGeminiLogin(t, "")
	g, _ := geminiOwnLogin()
	_, err := g.project(context.Background())
	if err == nil || !strings.Contains(err.Error(), "magpie accounts project gemini me@example.com") ||
		!strings.Contains(err.Error(), "no longer supported") || !strings.Contains(err.Error(), "~/.gemini/.env") ||
		!strings.Contains(err.Error(), "add it under Antigravity in magpie") {
		t.Fatalf("err = %v", err)
	}
	if len(f.onboards) != 0 {
		t.Error("onboarded without a project")
	}
}

// With GOOGLE_CLOUD_PROJECT in Gemini CLI's .env the account is onboarded
// to it, as Gemini CLI does.
func TestGeminiProjectFromEnv(t *testing.T) {
	f := &fakeGoogle{
		load:    `{"allowedTiers":[{"id":"standard-tier","name":"Gemini Code Assist","userDefinedCloudaicompanionProject":true,"isDefault":true}]}`,
		onboard: `{"done":true,"response":{"cloudaicompanionProject":{"id":"my-proj","name":"x"}}}`,
	}
	googleSandbox(t, f)
	writeGeminiLogin(t, "# comment\nGOOGLE_CLOUD_PROJECT=\"my-proj\"\n")
	g, _ := geminiOwnLogin()
	p, err := g.project(context.Background())
	if err != nil || p.id != "my-proj" || p.plan != "Gemini Code Assist" {
		t.Fatalf("project = %+v, %v", p, err)
	}
	if f.loads[0]["cloudaicompanionProject"] != "my-proj" || f.onboards[0]["tierId"] != "standard-tier" {
		t.Errorf("load %v onboard %v", f.loads[0], f.onboards[0])
	}
	if ua := f.heads["/prod/v1internal:loadCodeAssist"].Get("User-Agent"); !strings.HasPrefix(ua, "GeminiCLI/") {
		t.Errorf("User-Agent = %q", ua)
	}
}

// An account named in magpie gets its project from `magpie accounts project`.
func TestSetGoogleProject(t *testing.T) {
	f := &fakeGoogle{load: `{"currentTier":{"id":"standard-tier","name":"Gemini Code Assist Standard"}}`}
	googleSandbox(t, f)
	auth := googleAuth{AccessToken: "a", RefreshToken: "rt-2", Expiry: time.Now().Add(time.Hour).UnixMilli()}
	if err := addGoogleLogin("gemini", "work@example.com", "", auth); err != nil {
		t.Fatal(err)
	}
	ls := googleLogins("gemini")
	if len(ls) != 1 {
		t.Fatalf("logins = %+v", ls)
	}
	if _, err := ls[0].acct.project(context.Background()); err == nil {
		t.Fatal("a project was found for an account with none")
	}
	if err := SetGoogleProject("gemini", "work@example.com", " proj-7 "); err != nil {
		t.Fatal(err)
	}
	p, err := googleLogins("gemini")[0].acct.project(context.Background())
	if err != nil || p.id != "proj-7" || p.plan != "Gemini Code Assist Standard" {
		t.Fatalf("project = %+v, %v", p, err)
	}
	if err := SetGoogleProject("gemini", "nobody@example.com", "x"); err == nil {
		t.Error("set a project on an account that isn't there")
	}
}

// Antigravity's account is onboarded on the daily endpoint, asking until
// it is done, and its requests carry what Antigravity sends.
func TestAntigravityProjectAndEnvelope(t *testing.T) {
	f := &fakeGoogle{
		load:    `{"allowedTiers":[{"id":"free-tier","name":"Antigravity","isDefault":true}]}`,
		onboard: `{"done":true,"response":{"cloudaicompanionProject":"ag-proj"}}`,
	}
	googleSandbox(t, f)
	auth := googleAuth{AccessToken: "tok", RefreshToken: "rt-ag", Expiry: time.Now().Add(time.Hour).UnixMilli()}
	if err := addGoogleLogin("antigravity", "ag@example.com", "", auth); err != nil {
		t.Fatal(err)
	}
	p, ok := googleAccountOf("antigravity")
	if !ok || p.Account == nil || p.Base(CodeAssist) != codeAssistDaily {
		t.Fatalf("provider = %+v", p)
	}
	if s := p.Speaks(); len(s) != 1 || s[0] != CodeAssist {
		t.Fatalf("speaks %v", s)
	}
	body := `{"model":"claude-sonnet-4-6","request":{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}}`
	req, _ := http.NewRequest("POST", p.Base(CodeAssist)+"/v1internal:streamGenerateContent?alt=sse", strings.NewReader(body))
	if err := p.Sign(context.Background(), req, CodeAssist, []byte(body)); err != nil {
		t.Fatal(err)
	}
	if f.heads["/prod/v1internal:loadCodeAssist"] == nil || f.heads["/daily/v1internal:onboardUser"] == nil {
		t.Errorf("asked %v", f.heads)
	}
	if f.onboards[0]["tier_id"] != "free-tier" {
		t.Errorf("onboard = %v", f.onboards[0])
	}
	var env map[string]any
	b, _ := io.ReadAll(req.Body)
	json.Unmarshal(b, &env)
	r, _ := env["request"].(map[string]any)
	if env["project"] != "ag-proj" || env["userAgent"] != "antigravity" || env["requestType"] != "agent" ||
		!strings.HasPrefix(env["requestId"].(string), "agent-") || r["sessionId"] == "" || r["sessionId"] == nil {
		t.Errorf("envelope = %s", b)
	}
	if req.Header.Get("Authorization") != "Bearer tok" || !strings.HasPrefix(req.Header.Get("User-Agent"), "antigravity/hub/3.1.4 ") {
		t.Errorf("headers = %v", req.Header)
	}
	// the same conversation keeps its session
	req2, _ := http.NewRequest("POST", "http://x", nil)
	p.Sign(context.Background(), req2, CodeAssist, []byte(body))
	b2, _ := io.ReadAll(req2.Body)
	var env2 map[string]any
	json.Unmarshal(b2, &env2)
	if env2["request"].(map[string]any)["sessionId"] != r["sessionId"] {
		t.Error("session changed between turns")
	}
}
