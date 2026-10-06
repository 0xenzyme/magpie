package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// relayOfEvery is a relay that answers on Chat, Responses and Anthropic's
// Messages alike, streamed when asked, and records the path and body of
// each request.
func relayOfEvery(t *testing.T) (*httptest.Server, func() (paths []string, bodies []string)) {
	t.Helper()
	var mu sync.Mutex
	var paths, bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		paths, bodies = append(paths, r.URL.Path), append(bodies, string(b))
		mu.Unlock()
		stream := streamOf(b)
		switch {
		case r.URL.Path == "/v1/messages" && stream:
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"x\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"+
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"+
				"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"+
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		case r.URL.Path == "/v1/messages":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"x","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
		case r.URL.Path == "/v1/responses" && stream:
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"i\",\"output_index\":0,\"content_index\":0,\"delta\":\"ok\"}\n\n"+
				"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"x\",\"output\":[{\"type\":\"message\",\"id\":\"i\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
		case r.URL.Path == "/v1/responses":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"r","object":"response","status":"completed","model":"x","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
		case stream:
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}]}\n\n"+
				"data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"x\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n")
		default:
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"c","object":"chat.completion","model":"x","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() ([]string, []string) {
		mu.Lock()
		defer mu.Unlock()
		p, b := paths, bodies
		paths, bodies = nil, nil
		return p, b
	}
}

// A Claude model at a relay with an Anthropic URL beside its OpenAI one: a
// request on Messages goes on as it is, cache_control and all, and one on
// an API the relay doesn't serve is translated to Messages, not Chat, where
// cache_control is lost and every turn is billed uncached (#997;
// ReturnTrue on Discord: 0% cache hits until the OpenAI URL was left
// empty). One on an API the relay serves is relayed as the agent asked.
// The two-endpoint relay (an OpenAI URL and an Anthropic one) and the
// one-endpoint relay that speaks all three are both covered.
func TestClaudeGoesOnMessagesWhereTheRelayHasThem(t *testing.T) {
	srv, seen := relayOfEvery(t)
	for _, c := range []struct {
		name      string
		p         provider.Provider
		responses string // where Codex's Responses request goes
	}{
		{"OpenAI and Anthropic URLs", provider.Provider{Chat: srv.URL + "/v1", Anthropic: srv.URL}, "/v1/messages"},
		{"one URL, three APIs", provider.Provider{Chat: srv.URL + "/v1", Responses: srv.URL + "/v1", Anthropic: srv.URL}, "/v1/responses"},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			c.p.ID, c.p.Name, c.p.Key, c.p.Models = "relay", "Relay", "k", []string{"claude-sonnet-4-5"}
			if err := provider.Save(c.p); err != nil {
				t.Fatal(err)
			}
			for _, r := range []struct{ path, body string }{
				{"/v1/messages", `{"model":"relay/claude-sonnet-4-5","max_tokens":10,"stream":true,"system":[{"type":"text","text":"you are","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hi"}]}`},
				{"/v1/responses", `{"model":"relay/claude-sonnet-4-5","stream":true,"input":"hi"}`},
			} {
				seen()
				if code, b := post(t, r.path, r.body); code != 200 {
					t.Fatalf("%s: %d %s", r.path, code, b)
				}
				paths, bodies := seen()
				want := "/v1/messages"
				if r.path == "/v1/responses" {
					want = c.responses
				}
				if len(paths) != 1 || paths[0] != want {
					t.Fatalf("%s for a Claude model went to %v, want %s", r.path, paths, want)
				}
				if r.path == "/v1/messages" && !strings.Contains(bodies[0], `"cache_control"`) {
					t.Errorf("cache_control lost on the way: %s", bodies[0])
				}
			}
			// Chat for Claude is relayed as Chat: the agent asked it so
			seen()
			if code, b := post(t, "/v1/chat/completions", `{"model":"relay/claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`); code != 200 {
				t.Fatalf("chat: %d %s", code, b)
			}
			if paths, _ := seen(); len(paths) != 1 || paths[0] != "/v1/chat/completions" {
				t.Errorf("chat for Claude went to %v, want it relayed as /v1/chat/completions", paths)
			}
		})
	}
}

// Every API the client spoke is relayed on the same API when the provider
// serves it, for any model: Codex's Responses on Responses, Claude Code's
// Messages on Messages, Chat on Chat (#997: "自动好像只会走chat").
func TestAutoRelaysOnTheClientsOwnAPI(t *testing.T) {
	srv, seen := relayOfEvery(t)
	fresh(t)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: srv.URL + "/v1", Responses: srv.URL + "/v1", Anthropic: srv.URL,
		Models: []string{"glm-5", "gpt-5.1", "claude-sonnet-4-5"}}); err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"glm-5", "gpt-5.1", "claude-sonnet-4-5"} {
		for _, r := range []struct{ path, body string }{
			{"/v1/messages", `{"model":"relay/` + model + `","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`},
			{"/v1/responses", `{"model":"relay/` + model + `","input":"hi"}`},
			{"/v1/chat/completions", `{"model":"relay/` + model + `","messages":[{"role":"user","content":"hi"}]}`},
		} {
			seen()
			if code, b := post(t, r.path, r.body); code != 200 {
				t.Fatalf("%s %s: %d %s", model, r.path, code, b)
			}
			if paths, _ := seen(); len(paths) != 1 || paths[0] != r.path {
				t.Errorf("%s on %s went to %v", model, r.path, paths)
			}
		}
	}
}
