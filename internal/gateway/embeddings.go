package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/usage"
)

// retrieveTimeout bounds an embeddings or rerank call: a vendor answers
// one in seconds, a long batch in a minute or two.
const retrieveTimeout = 3 * time.Minute

// retrieve serves the retrieval APIs (#765): POST /v1/embeddings in
// OpenAI's shape and POST /v1/rerank in the shape Cohere, Jina and Voyage
// share. Neither is a chat, so neither is translated: the body goes to the
// model's provider at its OpenAI-style base + path as the agent sent it,
// the model id the provider's own, and the vendor's answer comes back as
// it said it. The call is recorded and its tokens counted like a turn's.
func (s *Server) retrieve(path, operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, admitted := s.requestBody(w, r, provider.Chat)
		if !admitted {
			return
		}
		start := time.Now()
		var req map[string]json.RawMessage
		if json.Unmarshal(body, &req) != nil || req == nil {
			writeError(w, provider.Chat, 400, "the body isn't a JSON object")
			return
		}
		var asked string
		json.Unmarshal(req["model"], &asked)
		if asked == "" {
			writeError(w, provider.Chat, 400, "name the model: \"model\" is a magpie model id, as /v1/models lists them")
			return
		}
		who := callerOf(r)
		call := Call{Time: start, From: provider.Chat, Agent: who.agent, Via: who.via, Model: asked}
		usage.Saw(agentOf(r))
		p, model, ok := provider.Resolve(asked)
		if !ok {
			msg := fmt.Sprintf("magpie knows no model %q", asked)
			if off, isOff := provider.SwitchedOff(asked); isOff {
				msg = switchedOff(off, asked)
			}
			call.Status, call.Error = 404, msg
			writeError(w, provider.Chat, 404, msg)
			s.record(call)
			return
		}
		call.Provider, call.To = p.ID, provider.Chat
		fail := func(code int, msg string) {
			call.Status, call.Error, call.Millis = code, msg, time.Since(start).Milliseconds()
			writeError(w, provider.Chat, code, msg)
			s.record(call)
		}
		base := strings.TrimRight(p.Base(provider.Chat), "/")
		if base == "" {
			fail(400, fmt.Sprintf("%s has no OpenAI-style API for %s", p.Name, path))
			return
		}
		req["model"], _ = json.Marshal(model)
		out, _ := json.Marshal(req)
		var done func()
		w, out, done = redacted(w, out)
		defer done()
		ctx, cancel := context.WithTimeout(r.Context(), retrieveTimeout)
		defer cancel()
		b, code, err := s.retrieveFrom(ctx, p, base+path, out)
		call.Millis = time.Since(start).Milliseconds()
		call.Status = code
		in := retrievedTokens(b)
		call.Usage.Input = in
		providerKeyID, providerKeyName := "", ""
		if p.Account == nil && p.Key != "" {
			providerKeyID, providerKeyName = provider.KeyID(p.Key), p.KeyName
		}
		appendUsage(r, usage.Record{Operation: operation, Time: start, Agent: call.Agent, Via: call.Via, Provider: p.ID, Host: p.Where(), Model: model, Requested: asked, ProviderKeyID: providerKeyID, ProviderKeyName: providerKeyName, ProviderAccount: accountOf(p),
			Input: in, Millis: call.Millis, Status: code, Session: sessionOf(r.Header)})
		if err != nil {
			call.Error = err.Error()
			s.record(call)
			writeError(w, provider.Chat, code, err.Error())
			return
		}
		s.record(call)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		w.Write(b)
	}
}

// retrieveFrom posts body to url as the provider signs its requests, and
// reads the answer; a failure's code is the vendor's, with its message.
func (s *Server) retrieveFrom(ctx context.Context, p provider.Provider, url string, body []byte) ([]byte, int, error) {
	ctx = p.Via(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 500, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if p.IsRemoteMagpie() {
		passOnCaller(ctx, req)
	}
	if err := p.Sign(ctx, req, provider.Chat, body); err != nil {
		return nil, 502, err
	}
	res, err := p.Do(s.client, req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 504, fmt.Errorf("%s didn't answer in %s", p.Name, retrieveTimeout)
		}
		return nil, 502, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 256<<20))
	if err != nil {
		return nil, 502, err
	}
	if res.StatusCode >= 300 {
		hint := ""
		if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusMethodNotAllowed {
			hint = " — " + p.Name + " may not serve this API"
		}
		return b, res.StatusCode, fmt.Errorf("%s: %d %s%s%s", provider.HostOf(url), res.StatusCode, http.StatusText(res.StatusCode), vendorSaid(vendorMessage(b)), hint)
	}
	return b, res.StatusCode, nil
}

// retrievedTokens is what an answer says it counted: OpenAI's
// prompt_tokens, Jina's and Voyage's total_tokens, or Cohere's billed
// input tokens.
func retrievedTokens(b []byte) int {
	var a struct {
		Usage struct {
			Prompt int `json:"prompt_tokens"`
			Input  int `json:"input_tokens"`
			Total  int `json:"total_tokens"`
		} `json:"usage"`
		Meta struct {
			Billed struct {
				Input int `json:"input_tokens"`
			} `json:"billed_units"`
		} `json:"meta"`
	}
	if json.Unmarshal(b, &a) != nil {
		return 0
	}
	for _, n := range []int{a.Usage.Prompt, a.Usage.Input, a.Usage.Total, a.Meta.Billed.Input} {
		if n > 0 {
			return n
		}
	}
	return 0
}
