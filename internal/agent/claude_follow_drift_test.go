package agent

import (
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/provider"
)

// claudeOpusThenSonnet is #1050's Claude Code before the switch: on
// magpie's Opus 5.5, its sonnet tier and its subagents given Sonnet 5.5,
// both 1M models, as magpie's record (applied.json) had them. It answers
// the settings.json.
func claudeOpusThenSonnet(t *testing.T, a *Agent, home string) string {
	t.Helper()
	if err := provider.Save(provider.Provider{ID: "anth", Name: "Anthropic", Chat: "https://api.anthropic.com/v1", Key: "k",
		Models: []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-4-5"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("anth", "https://api.anthropic.com/v1", []catalog.Model{
		{ID: "claude-opus-5-5", Context: 1000000}, {ID: "claude-sonnet-5-5", Context: 1000000}, {ID: "claude-haiku-4-5", Context: 200000},
	}); err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{{"model", "anth/claude-opus-5-5"}, {"sonnet", "anth/claude-sonnet-5-5"}, {"subagent", "anth/claude-sonnet-5-5"}} {
		if err := a.Apply(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	if rec := appliedOf(a.ID).Fields; rec["model"] != "anth/claude-opus-5-5[1m]" || rec["sonnet"] != "anth/claude-sonnet-5-5[1m]" || rec["subagent"] != "anth/claude-sonnet-5-5[1m]" {
		t.Fatalf("not #1050's record: %v", rec)
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// The main model moved onto the Sonnet 5.5 magpie had given the sonnet tier
// and the subagents, in Claude Code's own /model (whose list magpie writes,
// each "· via magpie"): the two now follow the main model, read as empty,
// and run on the model magpie set them to. That isn't "changed outside
// magpie" (#1050, cjyrainbow: Claude Code · WSL Ubuntu 已在 magpie 之外换掉
// claude/claude-sonnet-5-5[1m] while everything still went through magpie).
// The main model taken off magpie still is.
func TestClaudeFollowerOnTheMainModelIsNoDrift(t *testing.T) {
	home, _ := codexHome(t, "", "")
	a := claude(home)
	path := claudeOpusThenSonnet(t, a, home)
	if err := edit.SetJSON(path, edit.KV{Path: "model", Value: "anth/claude-sonnet-5-5[1m]"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Follow(); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string { v, _ := edit.GetJSON(path, "env."+k); return v }
	if v := a.Values(); v["model"] != "anth/claude-sonnet-5-5[1m]" || v["sonnet"] != "" || v["subagent"] != "" ||
		env("ANTHROPIC_DEFAULT_SONNET_MODEL") != "anth/claude-sonnet-5-5[1m]" || env("CLAUDE_CODE_SUBAGENT_MODEL") != "anth/claude-sonnet-5-5[1m]" {
		t.Fatalf("not #1050's state: %v\n%s", v, readFile(path))
	}
	if d := a.Drift(); d != nil {
		t.Fatalf("drift on a tier following magpie's model: %+v", *d)
	}

	// the main model on Anthropic's own: changed outside magpie
	if err := edit.SetJSON(path, edit.KV{Path: "env.ANTHROPIC_BASE_URL", Value: "https://api.anthropic.com"}, edit.KV{Path: "model", Value: "claude-sonnet-5-5"}); err != nil {
		t.Fatal(err)
	}
	if d := a.Drift(); d == nil || d.Kind != "replaced" && d.Kind != "unwired" {
		t.Fatalf("the main model off magpie isn't said: %+v", d)
	}
}

// The same in a WSL distro's Claude Code, the reporter's, running and
// stopped: a stopped one reads what was last seen there.
func TestWSLClaudeFollowerOnTheMainModelIsNoDrift(t *testing.T) {
	root, home := claudeDistroHome(t, `{"model": "opus"}`)
	d := distro{Name: "Ubuntu", Root: root, Home: "/home/me", Has: map[string]bool{"dir:.claude": true},
		Gateway: "172.20.0.1", Mirrored: true, Running: true}
	// what magpie sees there is kept for when the distro stops
	seen := d
	wsl.Lock()
	if wsl.seen == nil {
		wsl.seen = map[string]*distro{}
	}
	wsl.seen["Ubuntu"] = &seen
	wsl.Unlock()
	t.Cleanup(func() { wsl.Lock(); delete(wsl.seen, "Ubuntu"); wsl.Unlock() })
	a := wslAgent(wslKindOf("claude"), d)
	path := claudeOpusThenSonnet(t, a, home)
	if err := edit.SetJSON(path, edit.KV{Path: "model", Value: "anth/claude-sonnet-5-5[1m]"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Follow(); err != nil {
		t.Fatal(err)
	}
	if v := a.Values(); v["model"] != "anth/claude-sonnet-5-5[1m]" || v["sonnet"] != "" || v["subagent"] != "" {
		t.Fatalf("not #1050's state: %v\n%s", v, readFile(path))
	}
	if dr := a.Drift(); dr != nil {
		t.Fatalf("drift on a tier following magpie's model: %+v", *dr)
	}
	d.Running = false
	stopped := wslAgent(wslKindOf("claude"), d)
	if v := stopped.Values(); v["model"] != "anth/claude-sonnet-5-5[1m]" || v["sonnet"] != "" {
		t.Fatalf("stopped: not what was last seen: %v", v)
	}
	if dr := stopped.Drift(); dr != nil {
		t.Fatalf("stopped: drift on a tier following magpie's model: %+v", *dr)
	}
	noOwnClaude(t)
}
