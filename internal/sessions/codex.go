package sessions

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Codex writes a rollout file per session (a session picked up again later
// may go on in another, named with the same thread id): a session_meta line,
// a turn_context per turn naming the model, and token_count events with the
// running total and the last call's usage. Its input counts include the
// cached tokens.

type cxLine struct {
	Type    string `json:"type"`
	Payload struct {
		Type    string `json:"type"`
		ID      string `json:"id"`
		Cwd     string `json:"cwd"`
		Model   string `json:"model"`
		Role    string `json:"role"`
		Message string `json:"message"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Settings struct {
			Model string `json:"model"`
			Cwd   string `json:"cwd"`
		} `json:"thread_settings"`
		Info *struct {
			Total *cxUsage `json:"total_token_usage"`
			Last  *cxUsage `json:"last_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

type cxUsage struct {
	Input      int `json:"input_tokens"`
	Cached     int `json:"cached_input_tokens"`
	CacheWrite int `json:"cache_write_input_tokens"`
	Output     int `json:"output_tokens"`
}

// raw is the usage in Codex's own terms, input with the cache in it.
func (u cxUsage) raw() Tokens {
	return Tokens{Input: u.Input, Output: u.Output, CacheRead: u.Cached, CacheWrite: u.CacheWrite}
}

// spent is raw usage with the cache taken out of the input.
func spent(t Tokens) Tokens {
	t.Input -= t.CacheRead
	if t.Input < 0 {
		t.Input = 0
	}
	return t
}

var (
	cxMeta     = []byte(`"type":"session_meta"`)
	cxTurn     = []byte(`"type":"turn_context"`)
	cxCount    = []byte(`"type":"token_count"`)
	cxSettings = []byte(`"type":"thread_settings_applied"`)
	cxUserMsg  = []byte(`"type":"user_message"`)
	cxUserRole = []byte(`"role":"user"`)
)

func codexLine(s *state, b []byte, _ bool) {
	s.saw(tsAt(b, false))
	want := s.ID == "" && bytes.Contains(b, cxMeta) ||
		bytes.Contains(b, cxTurn) || bytes.Contains(b, cxCount) || bytes.Contains(b, cxSettings) ||
		s.Title == "" && (bytes.Contains(b, cxUserMsg) || bytes.Contains(b, cxUserRole))
	if !want {
		return
	}
	var l cxLine
	if json.Unmarshal(b, &l) != nil {
		return
	}
	p := &l.Payload
	switch {
	case l.Type == "session_meta":
		if s.ID == "" {
			s.ID = p.ID
		}
		if s.Cwd == "" {
			s.Cwd = p.Cwd
		}
	case l.Type == "turn_context":
		if p.Model != "" {
			s.Model = p.Model
		}
		if s.Cwd == "" {
			s.Cwd = p.Cwd
		}
	case l.Type == "event_msg" && p.Type == "thread_settings_applied":
		if p.Settings.Model != "" {
			s.Model = p.Settings.Model
		}
	case l.Type == "event_msg" && p.Type == "user_message":
		if s.Title == "" {
			s.Title = cxPrompt(p.Message)
		}
		if s.Title == "" && s.First == "" {
			s.First = untagged(p.Message)
		}
	case l.Type == "response_item" && p.Type == "message" && p.Role == "user":
		for _, c := range p.Content {
			if s.Title == "" && (c.Type == "input_text" || c.Type == "text") {
				s.Title = cxPrompt(c.Text)
			}
		}
	case l.Type == "event_msg" && p.Type == "token_count":
		if p.Info == nil || p.Info.Total == nil {
			return
		}
		total := p.Info.Total.raw()
		var d Tokens
		switch prev := s.Total; {
		case prev != nil && total.Input >= prev.Input && total.Output >= prev.Output && total.CacheRead >= prev.CacheRead:
			// the same total told again adds nothing
			d = total
			d.sub(*prev)
			d.CacheWrite = max(0, d.CacheWrite)
		case p.Info.Last != nil:
			// the first count in the file (whose total may run on from an
			// earlier file), or a total that started over
			d = p.Info.Last.raw()
		default:
			d = total
		}
		s.Total = &total
		s.use(s.Model, spent(d))
	}
}

// cxPrompt is the words of a prompt, or "" for what Codex put in itself
// (the environment, AGENTS.md, instructions).
func cxPrompt(text string) string {
	t := strings.TrimSpace(text)
	if t == "" || strings.HasPrefix(t, "<") || strings.HasPrefix(t, "# AGENTS.md") {
		return ""
	}
	return title(t)
}
