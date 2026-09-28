package gateway

import (
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// a model the agent names that magpie doesn't serve goes to the one the
// agent is set to use for it; one magpie serves is sent as asked
func TestStandIn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := &fake{t: t, ctype: "application/json", reply: `{"id":"c1","choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`}
	setup(t, provider.Chat, f)
	var asked []string
	StandIn = func(agent, model string) string { asked = append(asked, model); return "fake/m1" }
	t.Cleanup(func() { StandIn = nil })

	code, body := post(t, "/v1/chat/completions", `{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || modelOf(f.got) != "m1" || !strings.Contains(body, "OK") {
		t.Fatalf("stood in: %d %s, upstream %s", code, body, f.got)
	}
	for _, m := range []string{"m1", "fake/m1", "fake/other"} {
		asked = nil
		if code, body := post(t, "/v1/chat/completions", `{"model":"`+m+`","messages":[{"role":"user","content":"hi"}]}`); code != 200 || len(asked) != 0 {
			t.Fatalf("%s: %d %s, stand-in asked for %v", m, code, body, asked)
		}
	}
	// no stand-in: unknown as before
	StandIn = func(string, string) string { return "" }
	if code, _ := post(t, "/v1/chat/completions", `{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":"hi"}]}`); code != 404 {
		t.Fatalf("without a stand-in: %d", code)
	}
}
