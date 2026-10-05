package gateway

import (
	"bytes"
	"encoding/json"
	"strings"
)

// upstreamOf is the provider an aggregator's reply (a JSON body, or one
// event of its stream) says answered behind it, "" when it says none.
// OpenRouter names it in a top-level "provider" — a Chat body and every
// chunk of its stream, an Anthropic Messages body — and in a Messages
// stream's message_start, as message.provider (seen 2026-10-05: DeepInfra,
// StreamLake). Its Responses replies name none.
func upstreamOf(b []byte) string {
	if !bytes.Contains(b, []byte(`"provider"`)) {
		return ""
	}
	var v struct {
		Provider json.RawMessage `json:"provider"`
		Message  struct {
			Provider json.RawMessage `json:"provider"`
		} `json:"message"`
		Response struct {
			Provider json.RawMessage `json:"provider"`
		} `json:"response"`
	}
	if json.Unmarshal(b, &v) != nil {
		return ""
	}
	for _, raw := range []json.RawMessage{v.Provider, v.Message.Provider, v.Response.Provider} {
		var s string // a request's provider options echoed back are an object
		if json.Unmarshal(raw, &s) == nil {
			if s = strings.TrimSpace(s); s != "" && len(s) <= 64 {
				return s
			}
		}
	}
	return ""
}
