package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// Pi asks each model on the API its provider serves it on, so the gateway
// relays the request as it is: a Responses-only provider's models and GPT
// on OpenAI's API go on openai-responses; a Chat provider's, an Anthropic
// one's and a group's stay on the provider's openai-completions.
func TestPiModelsAskedOnTheirNativeAPI(t *testing.T) {
	home := syncHome(t)
	for _, p := range []provider.Provider{
		{ID: "resp", Name: "Resp", Key: "k", Responses: "http://127.0.0.1:1/v1", Models: []string{"grok-5", "gpt-5.5"}},
		{ID: "openai", Name: "OpenAI", Key: "k", Chat: "https://api.openai.com/v1", Responses: "https://api.openai.com/v1", Models: []string{"gpt-5.5", "whisper-x"}},
		{ID: "anth", Name: "Anth", Key: "k", Anthropic: "http://127.0.0.1:1", Models: []string{"claude-sonnet-5"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := pi(home).Field("model").Set("magpie/resp/grok-5"); err != nil {
		t.Fatal(err)
	}
	var file map[string]any
	if err := json.Unmarshal([]byte(readFile(filepath.Join(home, ".pi", "agent", "models.json"))), &file); err != nil {
		t.Fatal(err)
	}
	pm := file["providers"].(map[string]any)["magpie"].(map[string]any)
	if pm["api"] != "openai-completions" || pm["baseUrl"] != gatewayV1() {
		t.Fatalf("provider: %v", pm)
	}
	apis := map[string]any{}
	for _, raw := range pm["models"].([]any) {
		m := raw.(map[string]any)
		apis[m["id"].(string)] = m["api"]
	}
	for id, want := range map[string]any{
		"resp/grok-5":          "openai-responses",
		"resp/gpt-5.5":         "openai-responses",
		"openai/gpt-5.5":       "openai-responses",
		"openai/whisper-x":     nil,
		"relay/glm-4.6":        nil,
		"anth/claude-sonnet-5": nil,
		"group/auto-gpt-5-5":   nil,
	} {
		got, ok := apis[id]
		if !ok {
			t.Errorf("%s not listed: %v", id, apis)
		} else if got != want {
			t.Errorf("%s: api %v, want %v", id, got, want)
		}
	}
}
