package provider

import (
	"testing"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/plugin"
)

// movedGrok is Grok moved onto its plugin, which lists grok-new.
func movedGrok(t *testing.T) (plugin.Provider, Provider) {
	t.Helper()
	claudeHome(t)
	if err := setMigration("grok", func(m *Migration) { m.State = MovePlugin }); err != nil {
		t.Fatal(err)
	}
	pp := plugin.Provider{ID: "grok", Name: "Grok", NPM: "@ai-sdk/openai", Models: []plugin.Model{{ID: "grok-new", Name: "Grok New", NPM: "@ai-sdk/openai"}}}
	return pp, pluginProvider(pp, pluginLogin{})
}

// A moved provider's models are its plugin's: the list the built-in last
// fetched, still kept under the same id, isn't them.
func TestMovedProviderModelsArePlugins(t *testing.T) {
	_, p := movedGrok(t)
	if err := catalog.SaveLive("grok", "https://cli-chat-proxy.grok.com/v1", []catalog.Model{{ID: "grok-old", Provider: "grok"}}); err != nil {
		t.Fatal(err)
	}
	if p.ID != "grok" || !p.IsPlugin() {
		t.Fatalf("moved grok = %+v", p)
	}
	var ids []string
	for _, m := range p.Available() {
		ids = append(ids, m.ID)
	}
	if len(ids) != 1 || ids[0] != "grok-new" {
		t.Fatalf("moved grok's models = %v, want the plugin's [grok-new]", ids)
	}
	if _, ok := p.Fetched(); ok {
		t.Error("moved grok shows the built-in's list as fetched")
	}
}
