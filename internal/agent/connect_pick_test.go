package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/provider"
)

// The 「接入」 switch keeps an agent on the model it was on, now through
// magpie: Claude Code on its opus alias goes to the Claude Opus magpie
// serves, not to the first model of the first provider.
func TestConnectKeepsTheModel(t *testing.T) {
	home, _ := codexHome(t, "", "")
	if err := provider.Save(provider.Provider{ID: "aaa", Name: "AAA", Chat: "https://a.example/v1", Key: "k", Models: []string{"first", "claude-opus-5-5"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, path, `{"model": "opus"}`)
	c := claude(home)
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	if m, _ := edit.GetJSON(path, "model"); m != "aaa/claude-opus-5-5" {
		t.Fatalf("connected on %q:\n%s", m, readFile(path))
	}
}

// Gemini CLI is connected through its model (its first field is how it
// signs in), and Disconnect leaves its settings.json as they were, with
// no "model": {} behind.
func TestGeminiConnectRoundTrip(t *testing.T) {
	home, _ := codexHome(t, "", "")
	if err := provider.Save(provider.Provider{ID: "aaa", Name: "AAA", Chat: "https://a.example/v1", Key: "k", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".gemini")
	const was = `{"security": {"auth": {"selectedType": "oauth-personal"}}}`
	writeFile(t, filepath.Join(dir, "settings.json"), was)
	g := gemini(home)
	if err := g.Connect(); err != nil {
		t.Fatal(err)
	}
	if !g.Wired() {
		t.Fatalf("not connected:\n%s", readFile(filepath.Join(dir, "settings.json")))
	}
	if err := g.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if v, ok := edit.GetJSON(filepath.Join(dir, "settings.json"), "model"); ok {
		t.Fatalf("model left: %q", v)
	}
	if a, _ := edit.GetJSON(filepath.Join(dir, "settings.json"), "security.auth.selectedType"); a != "oauth-personal" {
		t.Fatalf("sign-in %q", a)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
		if k, _ := edit.GetEnvFile(filepath.Join(dir, ".env"), "GEMINI_API_KEY"); k != "" {
			t.Fatalf("magpie's key left: %q", k)
		}
	}
}

// Of the providers serving the model the agent is on, Connect takes the
// account it is signed in to, then a subscription, over a relay listed
// first; on no model of its own, a subscription's before the first.
func TestConnectPrefersTheSubscription(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	for _, c := range []struct {
		cur  string
		opts []Option
		want string
	}{
		{"claude-opus-5-5", []Option{
			{Value: "group/auto", Ref: "group/auto", Group: RoutingGroups},
			{Value: "relay/claude-opus-5-5[1m]", Ref: "relay/claude-opus-5-5[1m]", Group: "Relay"},
			{Value: "other/claude-opus-5-5", Ref: "other/claude-opus-5-5", Group: "Other", sub: true},
			{Value: "claude/claude-opus-5-5", Ref: "claude/claude-opus-5-5", Group: "Claude", sub: true, own: true},
		}, "claude/claude-opus-5-5"},
		{"opus", []Option{
			{Value: "relay/claude-opus-5-5[1m]", Ref: "relay/claude-opus-5-5", Group: "Relay"},
			{Value: "claude/claude-opus-5-5[1m]", Ref: "claude/claude-opus-5-5", Group: "X"},
		}, "claude/claude-opus-5-5[1m]"},
		{"claude-opus-5-5", []Option{
			{Value: "relay/claude-opus-5-5", Ref: "relay/claude-opus-5-5", Group: "Relay"},
			{Value: "other/claude-opus-5-5", Ref: "other/claude-opus-5-5", Group: "Other", sub: true},
		}, "other/claude-opus-5-5"},
		{"", []Option{
			{Value: "stepfun/step-1", Ref: "stepfun/step-1", Group: "StepFun"},
			{Value: "claude/claude-sonnet-5-5", Ref: "claude/claude-sonnet-5-5", Group: "Claude", sub: true},
		}, "claude/claude-sonnet-5-5"},
		{"", []Option{
			{Value: "stepfun/step-1", Ref: "stepfun/step-1", Group: "StepFun"},
		}, "stepfun/step-1"},
	} {
		v := c.cur
		a := &Agent{ID: "x", Name: "X", Fields: []Field{{Key: "model",
			Get:     func() string { return v },
			Set:     func(s string) error { v = s; return nil },
			Options: func(map[string]string) []Option { return c.opts },
		}}}
		if err := a.Connect(); err != nil {
			t.Fatal(err)
		}
		if v != c.want {
			t.Errorf("on %q: connected on %q, want %q", c.cur, v, c.want)
		}
	}
}

// Codex signed in with ChatGPT keeps its own last pick when connected (the
// owner: 让 Codex 记住上次的选择): magpie's models join its list by the base
// URL and its model stays; a model picked beside them keeps it connected,
// and Disconnect leaves Codex on that pick, magpie taken out.
func TestCodexConnectKeepsItsPick(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`,
		"model = \"gpt-5.5\"\nmodel_reasoning_effort = \"xhigh\"\n")
	cx := codex(home)
	if err := cx.Connect(); err != nil {
		t.Fatal(err)
	}
	cfg := read()
	if !cx.Wired() || !strings.Contains(cfg, `model = "gpt-5.5"`) || !strings.Contains(cfg, `openai_base_url = "`+codexGatewayURL()+`"`) ||
		!strings.Contains(cfg, "[model_providers.magpie]") || strings.Contains(cfg, "model_provider =") {
		t.Fatalf("connected (wired %v):\n%s", cx.Wired(), cfg)
	}
	if d := cx.Drift(); d != nil {
		t.Fatalf("drift: %+v", d)
	}
	// Sync, the failover's turn included, leaves it connected
	if err := cx.Sync(); err != nil || !cx.Wired() {
		t.Fatalf("after Sync (%v):\n%s", err, read())
	}
	// picked in Codex's /model, as Codex writes it
	if err := edit.SetTOMLTop(filepath.Join(home, ".codex", "config.toml"), edit.KV{Path: "model", Value: "gpt-5.4"}); err != nil {
		t.Fatal(err)
	}
	if !cx.Wired() {
		t.Fatalf("a pick of its own disconnected it:\n%s", read())
	}
	if err := cx.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if cfg = read(); cx.Wired() || !strings.Contains(cfg, `model = "gpt-5.4"`) || strings.Contains(cfg, "openai_base_url") {
		t.Fatalf("disconnected:\n%s", cfg)
	}
	// connected again, on that pick still
	if err := cx.Connect(); err != nil || !cx.Wired() || !strings.Contains(read(), `model = "gpt-5.4"`) {
		t.Fatalf("again (%v):\n%s", err, read())
	}
}
