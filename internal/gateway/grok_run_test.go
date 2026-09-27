//go:build !windows

package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/provider"
)

// fakeGrok stands in for the Grok Build CLI: it starts a child of its own,
// as grok does its helpers, says a little, then per FAKEGROK_MODE thinks on
// forever (hang), calls the caller's Bash tool through the MCP callback and
// answers with what it got (tool), or just answers (answer). The pids of it
// and its child go to FAKEGROK_PIDS.
const fakeGrok = `#!/bin/sh
echo $$ >> "$FAKEGROK_PIDS"
sleep 600 >/dev/null 2>&1 &
echo $! >> "$FAKEGROK_PIDS"
echo '{"type":"stream_event","event":{"type":"message_start"}}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"thinking it over"}}}'
case "$FAKEGROK_MODE" in
tool)
  out=$(curl -s --noproxy '*' -X POST -H 'Content-Type: application/json' -d '{"tool_call_id":"call_1","name":"Bash","arguments":{"command":"ls"}}' "$MAGPIE_MCP_CALLBACK")
  case "$out" in *file-a*) said="saw file-a" ;; *) said="no result" ;; esac
  echo '{"type":"stream_event","event":{"type":"message_start"}}'
  echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"'"$said"'"}}}'
  echo '{"type":"result","is_error":false,"usage":{"input_tokens":1,"output_tokens":1}}'
  ;;
answer)
  echo '{"type":"stream_event","event":{"type":"message_start"}}'
  echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"fresh run"}}}'
  echo '{"type":"result","is_error":false,"usage":{"input_tokens":1,"output_tokens":1}}'
  ;;
esac
wait
`

type grokRig struct {
	t    *testing.T
	s    *Server
	url  string
	pids string
}

// newGrokRig serves Grok runs of the fake CLI in a sandbox home, with a
// signed-in account that is only a file.
func newGrokRig(t *testing.T, mode string) *grokRig {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("GROK_HOME", filepath.Join(dir, "usergrok"))
	auth := `{"a":{"key":"k","email":"me@example.com"}}`
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{filepath.Join(dir, "usergrok"), filepath.Join(cache, "magpie", "grok-home", ".grok")} {
		os.MkdirAll(d, 0o700)
		os.WriteFile(filepath.Join(d, "auth.json"), []byte(auth), 0o600)
	}
	bin := filepath.Join(dir, "grok")
	os.WriteFile(bin, []byte(fakeGrok), 0o755)
	old := provider.GrokExecutable
	provider.GrokExecutable = func() string { return bin }
	t.Cleanup(func() { provider.GrokExecutable = old })

	r := &grokRig{t: t, s: New(), pids: filepath.Join(dir, "pids")}
	t.Setenv("FAKEGROK_PIDS", r.pids)
	t.Setenv("FAKEGROK_MODE", mode)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /_magpie/claude-mcp/{token}", r.s.subscription.mcpCall)
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var u Usage
		start := func(ctx context.Context, q *Request) (*subscriptionRun, <-chan Event, error) {
			return r.s.subscription.startGrok(ctx, q, "grok-test", "")
		}
		r.s.serveSubscription(w, req, provider.Anthropic, "Grok", "grok-test", body, &u, start)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("MAGPIE_ADDR", strings.TrimPrefix(srv.URL, "http://"))
	r.url = srv.URL
	t.Cleanup(func() {
		for _, pid := range r.started() {
			_ = syscall.Kill(pid, syscall.SIGKILL) // only ours, should a test fail
		}
	})
	return r
}

const grokTools = `"tools":[{"name":"Bash","description":"run a command","input_schema":{"type":"object","properties":{"command":{"type":"string"}}}}]`

func (r *grokRig) post(ctx context.Context, stream bool, msgs string) *http.Response {
	r.t.Helper()
	body := `{"model":"grok-test","max_tokens":100,"stream":` + strconv.FormatBool(stream) + `,` + grokTools + `,"messages":` + msgs + `}`
	req, _ := http.NewRequestWithContext(ctx, "POST", r.url+"/v1/messages", strings.NewReader(body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	return res
}

func (r *grokRig) ask(msgs string) string {
	r.t.Helper()
	res := r.post(context.Background(), false, msgs)
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		r.t.Fatalf("%d %s", res.StatusCode, b)
	}
	return string(b)
}

// started is every process the fake CLI and its children ran as.
func (r *grokRig) started() []int {
	b, _ := os.ReadFile(r.pids)
	var out []int
	for _, f := range strings.Fields(string(b)) {
		if pid, err := strconv.Atoi(f); err == nil {
			out = append(out, pid)
		}
	}
	return out
}

// allGone waits for every process started to have ended, and for the
// bridge to have let go of its runs.
func (r *grokRig) allGone(within time.Duration) {
	r.t.Helper()
	pids := r.started()
	if len(pids) == 0 {
		r.t.Fatal("the fake grok never ran")
	}
	for end := time.Now().Add(within); ; time.Sleep(20 * time.Millisecond) {
		var alive []int
		for _, pid := range pids {
			if syscall.Kill(pid, 0) == nil {
				alive = append(alive, pid)
			}
		}
		r.s.subscription.mu.Lock()
		runs := len(r.s.subscription.runs)
		r.s.subscription.mu.Unlock()
		if len(alive) == 0 && runs == 0 {
			return
		}
		if time.Now().After(end) {
			r.t.Fatalf("still running: processes %v of %v, %d runs", alive, pids, runs)
		}
	}
}

const grokHi = `[{"role":"user","content":"list the files"}]`

// A caller that goes away mid-reply (Esc, or Claude Code quitting) takes
// the grok answering it with it, and grok's children too.
func TestGrokRunEndsWhenTheCallerLeaves(t *testing.T) {
	r := newGrokRig(t, "hang")
	ctx, cancel := context.WithCancel(context.Background())
	res := r.post(ctx, true, grokHi)
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() && !strings.Contains(sc.Text(), "thinking it over") {
	}
	if len(r.started()) != 2 {
		t.Fatalf("pids %v", r.started())
	}
	cancel()
	res.Body.Close()
	r.allGone(3 * time.Second)
}

// A run parked on tool calls whose results never come ends once it has
// waited parkLongest; results that come after get their answer from a run
// started anew.
func TestParkedGrokRunIsLetGo(t *testing.T) {
	old := parkLongest
	parkLongest = 300 * time.Millisecond
	defer func() { parkLongest = old }()
	r := newGrokRig(t, "tool")
	out := r.ask(grokHi)
	if !strings.Contains(out, `"id":"call_1"`) || !strings.Contains(out, `"stop_reason":"tool_use"`) {
		t.Fatalf("no tool call: %s", out)
	}
	r.allGone(3 * time.Second)

	t.Setenv("FAKEGROK_MODE", "answer")
	late := r.ask(`[{"role":"user","content":"list the files"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"Bash","input":{"command":"ls"}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"file-a"}]}]`)
	if !strings.Contains(late, "fresh run") {
		t.Fatalf("a late result went unanswered: %s", late)
	}
}

// A tool round trip goes back to the grok that made the call, however long
// the tool takes within parkLongest, and that grok and its children end
// once it has answered.
func TestGrokToolRoundTrip(t *testing.T) {
	old := parkLongest
	parkLongest = 5 * time.Second
	defer func() { parkLongest = old }()
	r := newGrokRig(t, "tool")
	out := r.ask(grokHi)
	var first struct {
		Content []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"content"`
	}
	json.Unmarshal([]byte(out), &first)
	if n := len(first.Content); n == 0 || first.Content[n-1].ID != "call_1" {
		t.Fatalf("no tool call: %s", out)
	}
	time.Sleep(time.Second) // the tool runs
	answer := r.ask(`[{"role":"user","content":"list the files"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"Bash","input":{"command":"ls"}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"file-a"}]}]`)
	if !strings.Contains(answer, "saw file-a") || !strings.Contains(answer, `"stop_reason":"end_turn"`) {
		t.Fatalf("answer: %s", answer)
	}
	if n := len(r.started()); n != 2 {
		t.Fatalf("the round trip started another grok: %v", r.started())
	}
	r.allGone(5 * time.Second)
}
