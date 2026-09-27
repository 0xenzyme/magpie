package sessions

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// Claude Code writes one line per event: the prompts as "user" lines, each
// block of a reply as an "assistant" line carrying the whole message's
// usage (so a message's later blocks repeat it), and titles of its own.

type ccLine struct {
	Type        string `json:"type"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	Cwd         string `json:"cwd"`
	SessionID   string `json:"sessionId"`
	AITitle     string `json:"aiTitle"`
	Summary     string `json:"summary"`
	Message     struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			Input      int `json:"input_tokens"`
			Output     int `json:"output_tokens"`
			CacheRead  int `json:"cache_read_input_tokens"`
			CacheWrite int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

var (
	ccAssistant = []byte(`"type":"assistant"`)
	ccUser      = []byte(`"type":"user"`)
	ccTitle     = []byte(`"type":"ai-title"`)
	ccSummary   = []byte(`"type":"summary"`)
	ccCwd       = []byte(`"cwd":"`)
)

func claudeLine(s *state, b []byte, main bool) {
	s.saw(tsAt(b, true))
	want := bytes.Contains(b, ccAssistant) ||
		main && s.Title == "" && bytes.Contains(b, ccUser) ||
		main && (bytes.Contains(b, ccTitle) || bytes.Contains(b, ccSummary)) ||
		s.Cwd == "" && bytes.Contains(b, ccCwd)
	if !want {
		return
	}
	var l ccLine
	if json.Unmarshal(b, &l) != nil {
		return
	}
	if s.Cwd == "" && l.Cwd != "" {
		s.Cwd = l.Cwd
	}
	if s.ID == "" && l.SessionID != "" {
		s.ID = l.SessionID
	}
	switch l.Type {
	case "ai-title":
		if l.AITitle != "" {
			s.Named = title(l.AITitle)
		}
	case "summary":
		if l.Summary != "" && s.Named == "" {
			s.Named = title(l.Summary)
		}
	case "user":
		if main && s.Title == "" && !l.IsMeta && !l.IsSidechain {
			s.Title = ccPrompt(l.Message.Content)
			if s.Title == "" && s.First == "" {
				s.First = untagged(ccText(l.Message.Content))
			}
		}
	case "assistant":
		u := l.Message.Usage
		if u == nil || l.Message.Model == "<synthetic>" {
			return
		}
		t := Tokens{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite}
		if id := l.Message.ID; id != "" && id == s.Msg {
			// another block of the message counted: its usage stands for
			// the whole, the latest word on it
			if m, ok := s.Models[s.MsgModel]; ok {
				m.sub(s.MsgUse)
				s.Models[s.MsgModel] = m
			}
		}
		s.Msg, s.MsgModel, s.MsgUse = l.Message.ID, l.Message.Model, t
		if s.Models == nil {
			s.Models = map[string]Tokens{}
		}
		m := s.Models[l.Message.Model]
		m.add(t)
		s.Models[l.Message.Model] = m
	}
}

// ccText is the text of a user message, "" for a tool's result.
func ccText(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) != nil {
		var blocks []struct{ Type, Text string }
		if json.Unmarshal(content, &blocks) != nil {
			return ""
		}
		var parts []string
		for _, b := range blocks {
			if b.Type == "tool_result" {
				return ""
			}
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		text = strings.Join(parts, " ")
	}
	return strings.TrimSpace(text)
}

var (
	ccCommand = regexp.MustCompile(`<command-name>\s*([^<]*?)\s*</command-name>`)
	ccArgs    = regexp.MustCompile(`<command-args>\s*([^<]*?)\s*</command-args>`)
)

// ccPrompt is the words of a prompt someone typed, or "" for what Claude
// Code put in the conversation itself (tool results, command output).
func ccPrompt(content json.RawMessage) string {
	text := ccText(content)
	if m := ccCommand.FindStringSubmatch(text); m != nil {
		// a slash command: its name and what was typed after it
		cmd := m[1]
		if a := ccArgs.FindStringSubmatch(text); a != nil && a[1] != "" {
			cmd += " " + a[1]
		}
		return title(cmd)
	}
	if text == "" || strings.HasPrefix(text, "<") || strings.HasPrefix(text, "[Request interrupted") || strings.HasPrefix(text, "Caveat:") {
		return ""
	}
	return title(text)
}
