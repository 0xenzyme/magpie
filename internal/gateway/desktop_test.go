package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// Claude Desktop asks for anthropic/<id>: served by the model with that id
func TestAnthropicPrefixedModel(t *testing.T) {
	f := &fake{reply: sse(
		`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"m1","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)}
	setup(t, provider.Anthropic, f)
	code, body := post(t, "/v1/messages", `{"model":"anthropic/fake/m1","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || f.calls != 1 {
		t.Fatalf("anthropic/fake/m1: %d %s, %d calls", code, body, f.calls)
	}
	var sent struct{ Model string }
	if json.Unmarshal(f.got, &sent) != nil || sent.Model != "m1" {
		t.Fatalf("sent upstream as %q", f.got)
	}
	if code, body := post(t, "/v1/messages", `{"model":"anthropic/nobody/m1","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`); code != 404 {
		t.Fatalf("unknown: %d %s", code, body)
	}
	for _, id := range []string{"fake/m1", "anthropic/fake/m1"} {
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models/"+id, nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"fake/m1"`) {
			t.Fatalf("GET /v1/models/%s: %d %s", id, rec.Code, rec.Body)
		}
	}
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, httptest.NewRequest("HEAD", "/api/hello", nil))
	if rec.Code != 200 {
		t.Fatalf("HEAD /api/hello: %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("x-api-key", Token+"-claude-desktop")
	rec = httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"id":"anthropic/fake/m1"`) {
		t.Fatalf("Claude Desktop's list: %s", rec.Body)
	}
	if claudeLooking("devin/claude-opus-5-5") != "devin/claude-opus-5-5" {
		t.Fatal("a Claude id was prefixed")
	}
	if got := unprefixed("claude-sonnet-4-5"); got != "claude-sonnet-4-5" {
		t.Fatalf("unprefixed changed %q", got)
	}
}
