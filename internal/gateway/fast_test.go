package gateway

import (
	"strings"
	"testing"
)

// Codex's /fast (service_tier priority) survives a group's routing to a
// ChatGPT account, and goes nowhere else.
func TestResponsesFastTier(t *testing.T) {
	r, err := parseResponses([]byte(`{"model":"group/g","input":"hi","service_tier":"priority"}`))
	if err != nil || !r.Fast {
		t.Fatalf("fast not read: %v %+v", err, r)
	}
	for host, want := range map[string]bool{"chatgpt.com": true, "api.openai.com": true, "openrouter.ai": false, "": false} {
		b := string(buildResponses(r, "gpt-5.5", host, false))
		if got := strings.Contains(b, `"service_tier":"priority"`); got != want {
			t.Errorf("%q: tier sent %v, want %v: %s", host, got, want, b)
		}
	}
	r, _ = parseResponses([]byte(`{"model":"m","input":"hi","service_tier":"flex"}`))
	if r.Fast {
		t.Error("flex read as fast")
	}
}
