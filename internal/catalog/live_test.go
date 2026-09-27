package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEndpointAPIs(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want string
	}{
		{[]string{"/messages"}, "anthropic"},
		{[]string{"/v1/messages", "/chat/completions", "ws:/responses", "/responses"}, "anthropic,chat,responses"},
		{[]string{"/v1/chat/completions/", "/chat/completions"}, "chat"},
		{[]string{"/embeddings"}, ""},
		{nil, ""},
	} {
		if got := strings.Join(EndpointAPIs(c.in), ","); got != c.want {
			t.Errorf("%v: %q", c.in, got)
		}
	}
}

// A list that tells each model's context window (Command Code, OpenRouter)
// has it kept; an odd value is only left out.
func TestFetchContextLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Write([]byte(`{"object":"list","data":[{"id":"claude-sonnet-5","name":"Claude Sonnet 5","context_length":1000000,"supported_endpoints":["/messages"]},{"id":"kimi-k3","context_length":"lots"},{"id":"glm-5"}]}`))
	}))
	defer srv.Close()
	ms, _, err := FetchAt(context.Background(), srv.URL+"/v1", "k", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, m := range ms {
		got[m.ID] = m.Context
	}
	if len(got) != 3 || got["claude-sonnet-5"] != 1000000 || got["kimi-k3"] != 0 || got["glm-5"] != 0 {
		t.Errorf("contexts %v", got)
	}
}
