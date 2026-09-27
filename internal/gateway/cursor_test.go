package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

func cursorFrame(msg pb) []byte { return connectFrame(msg) }

func cursorEnd(js string) []byte {
	b := connectFrame([]byte(js))
	b[0] = 2
	return b
}

func cursorUpdate(num int, msg pb) []byte {
	return cursorFrame(pb{}.bytes(1, pb{}.bytes(num, msg)))
}

// cursorCallFrame is the server handing the client a call of an MCP tool.
func cursorCallFrame(id uint64, callID, name, city string) []byte {
	arg := pb{}.str(1, "city").bytes(2, pb{}.str(3, city))
	return cursorFrame(pb{}.bytes(2, pb{}.varint(1, id).str(15, "exec-1").
		bytes(11, pb{}.str(1, "magpie-"+name).bytes(2, arg).str(3, callID).str(5, name))))
}

// cursorDecoded runs decode over frames, and is what it said and what it
// sent back.
func cursorDecoded(t *testing.T, blobs map[string][]byte, frames ...[]byte) ([]Event, [][]pbField, int) {
	t.Helper()
	pr, pw := io.Pipe()
	st := &cursorStream{pw: pw, blobs: blobs}
	sent := make(chan [][]pbField)
	go func() {
		var got [][]pbField
		br := bufio.NewReader(pr)
		for {
			f, err := readConnectFrame(br)
			if err != nil {
				sent <- got
				return
			}
			got = append(got, pbFields(f.data))
		}
	}()
	out := make(chan Event, 256)
	st.decode(context.Background(), bufio.NewReader(bytes.NewReader(bytes.Join(frames, nil))), out, nil)
	pw.Close()
	var evs []Event
	for ev := range out {
		evs = append(evs, ev)
	}
	return evs, <-sent, st.status
}

func TestCursorDecodesTextAndCalls(t *testing.T) {
	evs, sent, _ := cursorDecoded(t, map[string][]byte{"k1": []byte("blob")},
		cursorFrame(pb{}.bytes(4, pb{}.varint(1, 7).bytes(2, pb{}.str(1, "k1")))),
		cursorFrame(pb{}.bytes(4, pb{}.varint(1, 8).bytes(3, pb{}.str(1, "k2")))),
		cursorUpdate(4, pb{}.str(1, "Hmm.")),
		cursorUpdate(1, pb{}.str(1, "Let me ")),
		cursorUpdate(1, pb{}.str(1, "look.")),
		cursorUpdate(27, pb{}.varint(1, 2)),
		cursorCallFrame(3, "call_a\nfc_1", "get_weather", "Paris"),
		cursorCallFrame(4, "call_b\nfc_2", "get_weather", "Lima"),
		cursorUpdate(1, pb{}.str(1, "never read")),
	)
	want := `|think:Hmm.|text:Let me look.|call:call_a__fc_1/get_weather|args:{"city":"Paris"}|call:call_b__fc_2/get_weather|args:{"city":"Lima"}|stop:tool`
	if got := said(evs); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	// the blob asked for is given, the one to keep acked
	if len(sent) != 2 || sent[0][0].num != 3 || sent[1][0].num != 3 {
		t.Fatalf("sent %v", sent)
	}
	got := pbFields(sent[0][0].data)
	if got[0].n != 7 || string(pbFields(got[1].data)[0].data) != "blob" {
		t.Fatalf("get_blob answered %v", got)
	}
	if cursorCallID("call_a__fc_1") != "call_a\nfc_1" || cursorCallID("toolu_x__fc_1") != "toolu_x__fc_1" {
		t.Fatal("call ids don't come back")
	}
}

func TestCursorDecodesTheEnd(t *testing.T) {
	evs, _, _ := cursorDecoded(t, nil,
		cursorUpdate(1, pb{}.str(1, "Hi")),
		cursorUpdate(14, pb{}.varint(1, 120).varint(2, 30).varint(3, 7)),
	)
	if got := said(evs); got != "|text:Hi|stop:stop" {
		t.Fatal(got)
	}
	for _, ev := range evs {
		if ev.Kind == KUsage && (ev.Usage.Input != 120 || ev.Usage.Output != 30 || ev.Usage.CacheRead != 7) {
			t.Fatalf("usage %+v", ev.Usage)
		}
	}

	evs, _, status := cursorDecoded(t, nil, cursorEnd(`{"error":{"code":"resource_exhausted","message":"Error","details":[{"debug":{"details":{"title":"Usage limit","detail":"You're out."}}}]}}`))
	if got := said(evs); !strings.HasSuffix(got, "Usage limit: You're out.") || status != 429 {
		t.Fatal(got, status)
	}
}

func TestCursorMessages(t *testing.T) {
	r := &Request{
		System: "Be brief.",
		Tools:  []Tool{{Name: "read", Description: "Read a file", Schema: json.RawMessage(`{"type":"object"}`)}},
		Messages: []Message{
			{Role: "user", Parts: []Part{{Kind: ToolResult, CallID: "orphan", Text: "x"}, {Kind: Text, Text: "look at a"}}},
			{Role: "assistant", Parts: []Part{
				{Kind: Text, Text: "Reading."},
				{Kind: ToolCall, ID: "call_1__fc_1", Name: "read", Args: json.RawMessage(`{"p":"a"}`)},
				{Kind: ToolCall, ID: "c2", Name: "gone"},
			}},
			{Role: "user", Parts: []Part{
				{Kind: ToolResult, CallID: "call_1__fc_1", Text: `{"ok":1}`, IsError: true},
				{Kind: Image, MediaType: "image/png", Data: "AAE="},
				{Kind: Text, Text: "and this"},
			}},
		},
	}
	var got []string
	for _, m := range cursorMessages(r, bridgeTools(r)) {
		got = append(got, string(m))
	}
	lt, gt := `\u003c`, `\u003e` // as encoding/json escapes < and >
	sys := `{"content":"Be brief.\n\n` + lt + "dynamic_tool_catalog" + gt
	if !strings.HasPrefix(got[0], sys) || !strings.Contains(got[0], lt+`tool name=\"read\"`+gt+`\nRead a file\ninput schema: {\"type\":\"object\"}`) {
		t.Fatalf("system %s", got[0])
	}
	want := []string{
		`{"content":[{"text":"look at a","type":"text"}],"role":"user"}`,
		`{"content":[{"text":"Reading.","type":"text"},{"args":{"arguments":{"p":"a"},"namespace":"magpie","toolName":"read"},"toolCallId":"call_1\nfc_1","toolName":"CallDynamicTool","type":"tool-call"},{"args":{"arguments":{},"namespace":"magpie","toolName":"gone"},"toolCallId":"c2","toolName":"CallDynamicTool","type":"tool-call"}],"role":"assistant"}`,
		`{"content":[{"experimental_content":[{"text":"{\"ok\":1}","type":"text"}],"isError":true,"result":{"ok":1},"toolCallId":"call_1\nfc_1","toolName":"CallDynamicTool","type":"tool-result"},{"experimental_content":[{"text":"` + devinNoResult + `","type":"text"}],"isError":true,"result":"` + devinNoResult + `","toolCallId":"c2","toolName":"CallDynamicTool","type":"tool-result"}],"role":"tool"}`,
		`{"content":[{"image":{"__type":"Uint8Array","hex":"0001"},"mimeType":"image/png","type":"image"},{"text":"and this","type":"text"}],"role":"user"}`,
	}
	if strings.Join(got[1:], "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got[1:], "\n"), strings.Join(want, "\n"))
	}
}

func TestBuildCursorRun(t *testing.T) {
	msgs := [][]byte{[]byte(`{"role":"user","content":"hi"}`)}
	tools := []bridgeTool{{Name: "read", InputSchema: json.RawMessage(`{"type":"object","required":["p"]}`)}}
	run, blobs := buildCursorRun(msgs, "hi", tools, "gpt-5.4")
	rr := pbFields(pbFields(run)[0].data)
	var state []pbField
	var models, mcp []string
	for _, f := range rr {
		switch f.num {
		case 1:
			state = pbFields(f.data)
		case 3, 9:
			models = append(models, string(pbFields(f.data)[0].data))
		case 4:
			mcp = append(mcp, string(pbFields(pbFields(f.data)[0].data)[0].data))
		}
	}
	if strings.Join(models, ",") != "gpt-5.4,gpt-5.4" || strings.Join(mcp, ",") != "read" {
		t.Fatalf("models %v, tools %v", models, mcp)
	}
	var ids int
	for _, f := range state {
		switch f.num {
		case 1:
			ids++
			if string(blobs[string(f.data)]) != string(msgs[0]) {
				t.Fatal("the message's blob is missing")
			}
		case 8: // the turn, whose user message names the words
			turn := pbFields(blobs[string(f.data)])
			user := blobs[string(pbFields(turn[0].data)[0].data)]
			if string(pbFields(user)[0].data) != "hi" {
				t.Fatalf("turn %q", user)
			}
		}
	}
	if ids != 1 || len(blobs) != 3 {
		t.Fatalf("%d ids, %d blobs", ids, len(blobs))
	}
	// a schema goes as a google.protobuf.Value too, and reads back the same
	def := pbFields(cursorToolDef(tools[0]))
	b, _ := json.Marshal(pbAny(def[1].data))
	if string(b) != `{"required":["p"],"type":"object"}` {
		t.Fatal(string(b))
	}
}

func TestServeCursor(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var reply []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent.v1.AgentService/Run" || r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("x-cursor-client-type") != "cli" {
			http.Error(w, `{"code":"unauthenticated","message":"no"}`, 401)
			return
		}
		f, err := readConnectFrame(bufio.NewReader(r.Body))
		if err != nil || len(pbFields(f.data)) == 0 {
			http.Error(w, `{"code":"invalid_argument","message":"no run"}`, 400)
			return
		}
		// the Run's body stays open: the reply is flushed, not held until
		// the body is read to its end
		http.NewResponseController(w).EnableFullDuplex()
		w.Header().Set("Content-Type", "application/connect+proto")
		w.Write(reply)
		w.(http.Flusher).Flush()
	}))
	defer up.Close()
	tok, ver, agent := cursorToken, cursorVersion, cursorAgent
	cursorToken = func() (string, error) { return "tok", nil }
	cursorVersion = func() string { return "cli-test" }
	cursorAgent = up.URL
	defer func() { cursorToken, cursorVersion, cursorAgent = tok, ver, agent }()

	serve := func(from provider.Protocol, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		var u Usage
		New().serveCursor(w, httptest.NewRequest("POST", "/", strings.NewReader(body)), from, "auto", []byte(body), &u)
		return w
	}
	reply = bytes.Join([][]byte{
		cursorUpdate(1, pb{}.str(1, "Hello")),
		cursorUpdate(27, pb{}.varint(1, 1)),
		cursorCallFrame(1, "toolu_1", "read", "x"),
	}, nil)
	w := serve(provider.Anthropic, `{"model":"x","max_tokens":10,"tools":[{"name":"read","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hi"}]}`)
	var msg struct {
		Content []struct {
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	json.Unmarshal(w.Body.Bytes(), &msg)
	if w.Code != 200 || len(msg.Content) != 2 || msg.Content[0].Text != "Hello" || msg.Content[1].ID != "toolu_1" ||
		string(msg.Content[1].Input) != `{"city":"x"}` || msg.StopReason != "tool_use" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}

	reply = cursorEnd(`{"error":{"code":"resource_exhausted","message":"out of credits"}}`)
	if w = serve(provider.Chat, `{"model":"x","messages":[{"role":"user","content":"hi"}]}`); w.Code != 429 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
