package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseGrokModels(t *testing.T) {
	b := []byte(`{"object":"list","data":[
		{"id":"grok-4.7","name":"Grok 4.7","context_window":500000,"api_backend":"responses","reasoning_efforts":[{"value":"xhigh"},{"value":"high"},{"value":"medium"},{"value":"low"}]},
		{"id":"grok-4.7-build-fast","context_window":256000,"api_backend":"responses"},
		{"id":"grok-old","api_backend":"chat_completions"}]}`)
	ms := parseGrokModels(b)
	if len(ms) != 2 || ms[0].ID != "grok-4.7" || ms[0].Name != "Grok 4.7" || ms[0].Context != 500000 ||
		strings.Join(ms[0].Efforts, ",") != "low,medium,high,xhigh" || ms[1].Name != "grok-4.7-build-fast" || ms[1].Efforts != nil {
		t.Fatalf("models = %+v", ms)
	}
}

// A request is signed with the sign-in of its account's home, as the CLI
// signs its own.
func TestGrokSigns(t *testing.T) {
	var got http.Header
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Write([]byte(`{"data":[{"id":"grok-4.7","api_backend":"responses"}]}`))
	}))
	defer up.Close()
	base := grokBase
	grokBase = up.URL
	defer func() { grokBase = base }()
	home := t.TempDir()
	grokSignedIn(t, home, "me@x.ai")
	acct := &Account{Agent: "grok"}
	grokSigned(acct, home)
	req, _ := http.NewRequest("POST", up.URL+"/responses", nil)
	if err := acct.sign(context.Background(), req, []byte(`{"model":"grok-4.7","prompt_cache_key":"c1"}`)); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "Bearer k-me@x.ai" || req.Header.Get("x-grok-client-version") == "" ||
		req.Header.Get("x-grok-model-override") != "grok-4.7" || req.Header.Get("x-grok-conv-id") != "c1" ||
		!strings.HasPrefix(req.Header.Get("User-Agent"), "grok-shell/") {
		t.Fatalf("headers = %v", req.Header)
	}
	ms, err := grokModels(context.Background(), acct.sign)
	if err != nil || len(ms) != 1 || got.Get("Authorization") != "Bearer k-me@x.ai" {
		t.Fatalf("%v %+v %v", err, ms, got)
	}
}

func TestGrokTokenReadsTheCLIsSignIn(t *testing.T) {
	home := t.TempDir()
	exp := time.Now().Add(2 * time.Hour).UTC()
	auth := map[string]any{"https://auth.x.ai::u1": map[string]any{
		"key": "tok", "email": "me@example.com", "auth_mode": "oidc", "refresh_token": "r",
		"expires_at": exp.Format(time.RFC3339Nano), "oidc_issuer": "https://auth.x.ai"}}
	b, _ := json.Marshal(auth)
	if err := os.WriteFile(filepath.Join(home, "auth.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if u, ok := GrokUser(home); !ok || u != "me@example.com" {
		t.Fatalf("user = %q %v", u, ok)
	}
	c, err := grokAccessToken(home, "", false)
	if err != nil || c.Key != "tok" {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := grokAccessToken(t.TempDir(), "", false); err == nil {
		t.Fatal("no sign-in, yet a token")
	}
}
