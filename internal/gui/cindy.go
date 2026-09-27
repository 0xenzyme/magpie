package gui

import (
	"encoding/base64"
	"encoding/json"

	"github.com/yetone/magpie/internal/gateway"
)

// Cindy (github.com/makecindy/cindy) keeps its keys encrypted and takes a
// provider only through its import link, which the user confirms in Cindy:
// cindy://provider/import?v=1&data=<base64url JSON>. magpie's link adds the
// gateway as a custom provider, one endpoint per engine Cindy runs — Claude
// Code on Messages, Codex on Responses, Pi on Chat Completions — each
// listing the gateway's models. The key names Cindy, so its calls are
// Cindy's in usage and the routing view.

// CindyScheme is the one scheme besides http(s) the page may ask to open.
const CindyScheme = "cindy://provider/import?"

type cindyEndpoint struct {
	Protocol  string   `json:"protocol"`
	BaseURL   string   `json:"baseUrl"`
	Targets   []string `json:"targets"`
	ModelsURL string   `json:"modelsUrl"`
}

// CindyLink is the import link for the gateway at url.
func CindyLink(url string) string {
	v1 := url + "/v1"
	models := v1 + "/models"
	payload := struct {
		Kind      string          `json:"kind"`
		Name      string          `json:"name"`
		ID        string          `json:"id"`
		Auth      map[string]any  `json:"auth"`
		Endpoints []cindyEndpoint `json:"endpoints"`
	}{
		Kind: "custom", Name: "Magpie", ID: "magpie",
		Auth: map[string]any{"method": "apiKey", "apiKey": gateway.TokenFor("cindy")},
		Endpoints: []cindyEndpoint{
			{"anthropic-messages", url, []string{"claude-code"}, models},
			{"openai-responses", v1, []string{"codex"}, models},
			{"openai-chat", v1, []string{"pi"}, models},
		},
	}
	b, _ := json.Marshal(payload)
	return CindyScheme + "v=1&data=" + base64.RawURLEncoding.EncodeToString(b)
}
