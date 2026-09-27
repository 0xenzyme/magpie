package gui

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestCindyLink(t *testing.T) {
	link := CindyLink("http://127.0.0.1:3425")
	rest, ok := strings.CutPrefix(link, "cindy://provider/import?")
	if !ok {
		t.Fatal(link)
	}
	q, err := url.ParseQuery(rest)
	if err != nil || len(q) != 2 || q.Get("v") != "1" {
		t.Fatalf("query %v %v", q, err)
	}
	data := q.Get("data")
	if strings.ContainsAny(data, "=+/") {
		t.Fatalf("not unpadded base64url: %s", data)
	}
	b, err := base64.RawURLEncoding.DecodeString(data)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Kind      string
		Name      string
		Auth      struct{ Method, APIKey string }
		Endpoints []struct {
			Protocol, BaseURL, ModelsURL string
			Targets                      []string
		}
	}
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.Kind != "custom" || p.Auth.Method != "apiKey" || p.Auth.APIKey != "magpie-cindy" || len(p.Endpoints) != 3 {
		t.Fatalf("%s", b)
	}
	want := map[string]string{
		"anthropic-messages claude-code": "http://127.0.0.1:3425",
		"openai-responses codex":         "http://127.0.0.1:3425/v1",
		"openai-chat pi":                 "http://127.0.0.1:3425/v1",
	}
	for _, e := range p.Endpoints {
		k := e.Protocol + " " + strings.Join(e.Targets, ",")
		if want[k] != e.BaseURL || e.ModelsURL != "http://127.0.0.1:3425/v1/models" {
			t.Errorf("endpoint %s: %s models %s", k, e.BaseURL, e.ModelsURL)
		}
		delete(want, k)
	}
	if len(want) != 0 {
		t.Errorf("missing %v", want)
	}
}
