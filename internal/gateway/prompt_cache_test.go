package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

// A Codex request sent to Claude is marked for Anthropic's prompt cache:
// its instructions, and its conversation at the last block that can be.
func TestAnthropicPromptCache(t *testing.T) {
	r, err := parseResponses([]byte(`{"model":"m","instructions":"be brief","stream":true,
	  "tools":[{"type":"function","name":"shell","parameters":{"type":"object"}}],
	  "input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"list files"}]},
	    {"type":"function_call","call_id":"c1","name":"shell","arguments":"{\"cmd\":\"ls\"}"},
	    {"type":"function_call_output","call_id":"c1","output":"a.go b.go"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		System []aBlock `json:"system"`
		Tools  []map[string]any
		Msgs   []struct{ Content []aBlock } `json:"messages"`
	}
	b := buildAnthropic(r, "claude-sonnet-5")
	json.Unmarshal(b, &out)
	if len(out.System) != 1 || out.System[0].Text != "be brief" || out.System[0].CacheControl["type"] != "ephemeral" {
		t.Fatalf("system: %s", b)
	}
	last := out.Msgs[len(out.Msgs)-1].Content
	if lb := last[len(last)-1]; lb.Type != "tool_result" || lb.CacheControl["type"] != "ephemeral" {
		t.Fatalf("last block: %s", b)
	}
	if n := strings.Count(string(b), `"cache_control"`); n != 2 {
		t.Fatalf("%d marks: %s", n, b)
	}

	// no system prompt: the tools are marked instead; thinking never is
	r.System = ""
	r.Messages = append(r.Messages, Message{Role: "assistant", Parts: []Part{{Kind: Text, Text: "done"}, {Kind: Thinking, Text: "hm", Signature: "sig"}}})
	b = buildAnthropic(r, "claude-sonnet-5")
	out.System, out.Tools, out.Msgs = nil, nil, nil
	json.Unmarshal(b, &out)
	last = out.Msgs[len(out.Msgs)-1].Content
	if out.System != nil || out.Tools[0]["cache_control"] == nil || last[1].Type != "thinking" || last[1].CacheControl != nil || last[0].CacheControl == nil {
		t.Fatalf("no system: %s", b)
	}
	if n := strings.Count(string(b), `"cache_control"`); n != 2 {
		t.Fatalf("%d marks: %s", n, b)
	}
}
