package profile

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/library"
)

// A profile's details are its fields by agent, in the order magpie lists the
// agents and their fields, under the agents' names and the fields' labels,
// with what the library gives each; an empty quiet field is left out, an
// agent magpie no longer knows comes last by id, and nothing that reads as a
// key or a token is in them (#467).
func TestDetails(t *testing.T) {
	sandbox(t)
	p := Profile{
		Fields: map[string]string{
			"codex.effort":  "high",
			"codex.model":   "gpt-5",
			"codex.login":   "", // quiet: follows the model
			"claude.model":  "opus",
			"claude.haiku":  "",
			"gone.model":    "m",
			"gone.apiKey":   "plain-looking",
			"codex.extra":   "sk-live-abcdef",
			"claude.custom": strings.Repeat("a1B2", 10),
		},
		Library: &library.Setup{
			MCP:    map[string][]string{"github": {"claude", "codex"}, "off": {}},
			Skills: map[string][]string{"review": {"claude"}},
			Instructions: library.SetupInstructions{
				Shared: "be brief", Agents: []string{"codex"},
			},
		},
	}
	gs := Details(p)
	var ids []string
	for _, g := range gs {
		ids = append(ids, g.ID)
	}
	ci, xi := slices.Index(ids, "claude"), slices.Index(ids, "codex")
	if ci < 0 || xi < 0 || ids[len(ids)-1] != "gone" || len(ids) != 3 {
		t.Fatalf("groups %v, want claude and codex in magpie's order, then gone", ids)
	}
	claude, codex, gone := gs[ci], gs[xi], gs[2]
	if claude.Name != "Claude Code" || codex.Name != "Codex" || gone.Name != "gone" {
		t.Errorf("names %q %q %q", claude.Name, codex.Name, gone.Name)
	}

	item := func(g Group, key string) (Item, bool) {
		for _, it := range g.Fields {
			if it.Key == key {
				return it, true
			}
		}
		return Item{}, false
	}
	if it, _ := item(codex, "model"); it.Value != "gpt-5" || it.Label != "model" {
		t.Errorf("codex model %+v", it)
	}
	if codex.Fields[0].Key != "model" {
		t.Errorf("codex's fields in its order, model first: %+v", codex.Fields)
	}
	if _, ok := item(codex, "login"); ok {
		t.Error("an empty quiet field is left out")
	}
	if _, ok := item(claude, "haiku"); ok {
		t.Error("an empty tier is left out")
	}
	for _, c := range []struct {
		g   Group
		key string
	}{{codex, "extra"}, {claude, "custom"}, {gone, "apiKey"}} {
		it, ok := item(c.g, c.key)
		if !ok || !it.Hidden || it.Value != "" {
			t.Errorf("%s.%s must be there, hidden and empty: %+v", c.g.ID, c.key, it)
		}
	}

	if !slices.Equal(claude.Servers, []string{"github"}) || !slices.Equal(claude.Skills, []string{"review"}) || claude.Instructions {
		t.Errorf("claude's library %+v", claude)
	}
	if !slices.Equal(codex.Servers, []string{"github"}) || len(codex.Skills) != 0 || !codex.Instructions {
		t.Errorf("codex's library %+v", codex)
	}

	b, _ := json.Marshal(gs)
	for _, s := range []string{"sk-live", "a1B2a1B2", "plain-looking", "be brief"} {
		if strings.Contains(string(b), s) {
			t.Errorf("details carry %q: %s", s, b)
		}
	}
}
