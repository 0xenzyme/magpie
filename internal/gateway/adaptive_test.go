package gateway

import (
	"encoding/json"
	"testing"
)

func TestAdaptiveThinking(t *testing.T) {
	for model, want := range map[string]bool{
		"claude-opus-5-5": true, "claude-opus-5": true, "claude-sonnet-4-6": true,
		"claude-opus-4-7": true, "anthropic.claude-sonnet-4.6-v1": true, "claude-sonnet-5": true,
		"claude-sonnet-4-5-20250929": false, "claude-sonnet-4-20250514": false, "claude-opus-4-1": false,
		"claude-3-7-sonnet-20250219": false, "deepseek-v4": false, "claude-haiku-4-5": false,
	} {
		if got := adaptiveOnly(model); got != want {
			t.Errorf("adaptiveOnly(%q) = %v", model, got)
		}
	}
	var out map[string]any
	json.Unmarshal(buildAnthropic(&Request{Thinking: true, Effort: "xhigh"}, "claude-opus-5-5"), &out)
	if th, _ := json.Marshal(out["thinking"]); string(th) != `{"type":"adaptive"}` {
		t.Errorf("thinking = %s", th)
	}
	if oc, _ := json.Marshal(out["output_config"]); string(oc) != `{"effort":"max"}` {
		t.Errorf("output_config = %s", oc)
	}
	json.Unmarshal(buildAnthropic(&Request{Thinking: true, Effort: "low"}, "claude-sonnet-4-5"), &out)
	if th, _ := json.Marshal(out["thinking"]); string(th) != `{"budget_tokens":4096,"type":"enabled"}` {
		t.Errorf("old model thinking = %s", th)
	}
}

// Z.ai's GLM-5.2 and GLM-5.3 take their effort in output_config, as ZCode
// sends it, fitted to their levels; a budget alone leaves them at theirs
func TestGLMEffortInOutputConfig(t *testing.T) {
	for model, want := range map[string]bool{
		"GLM-5.3": true, "glm-5.3-flash": true, "GLM-5.2": true, "zai/glm-5.3": true,
		"GLM-5-Turbo": false, "glm-5.1": false, "glm-5.30": false, "claude-opus-5-5": false,
	} {
		if got := effortInOutputConfig.MatchString(model); got != want {
			t.Errorf("effortInOutputConfig(%q) = %v", model, got)
		}
	}
	var out map[string]any
	json.Unmarshal(buildAnthropic(&Request{Thinking: true, Effort: "max"}, "GLM-5.3"), &out)
	if oc, _ := json.Marshal(out["output_config"]); string(oc) != `{"effort":"max"}` {
		t.Errorf("output_config = %s", oc)
	}
	out = nil
	json.Unmarshal(buildAnthropic(&Request{Thinking: true, Effort: "high"}, "GLM-5-Turbo"), &out)
	if out["output_config"] != nil {
		t.Errorf("GLM-5-Turbo asked output_config: %v", out["output_config"])
	}

	levels := []string{"low", "high", "max"}
	for body, want := range map[string]string{
		// Claude Code's budget for medium, the nearest of GLM-5.3's
		`{"thinking":{"type":"enabled","budget_tokens":10000}}`:                          `{"output_config":{"effort":"high"},"thinking":{"budget_tokens":10000,"type":"enabled"}}`,
		`{"thinking":{"type":"adaptive"},"output_config":{"effort":"low","format":"x"}}`: `{"output_config":{"effort":"low","format":"x"},"thinking":{"type":"adaptive"}}`,
		`{"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"}}`:            `{"output_config":{"effort":"max"},"thinking":{"type":"adaptive"}}`,
		`{"thinking":{"type":"disabled"}}`:                                               `{"thinking":{"type":"disabled"}}`,
		`{"max_tokens":5}`:                                                               `{"max_tokens":5}`,
	} {
		var v any
		json.Unmarshal(withOutputEffort([]byte(body), levels), &v)
		if got, _ := json.Marshal(v); string(got) != want {
			t.Errorf("%s:\n got  %s\n want %s", body, got, want)
		}
	}
}
