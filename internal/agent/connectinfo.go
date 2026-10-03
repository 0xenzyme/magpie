package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/proc"
)

// What the Agents page says beside an agent's 「接入」 switch (the owner's
// design): before it is on, what the agent runs on now — its own
// subscription, sign-in or providers — so the user can tell what turning
// it on touches; once on, whether copies of it already running still have
// the list they started with.

// Source is what an agent not connected to magpie runs on now: "sub" (a
// Claude subscription), "chatgpt" (a ChatGPT sign-in), "key" (an API key
// of its own), "providers:N" (N providers of its own); "" when unknown.
func (a *Agent) Source() string {
	if a.WSL != "" || a.Path == "" {
		return ""
	}
	dir := filepath.Dir(a.Path)
	switch a.ID {
	case "claude":
		// signed in to claude.ai: ~/.claude.json (or the one in
		// CLAUDE_CONFIG_DIR) has the account
		for _, p := range []string{filepath.Join(dir, ".claude.json"), filepath.Join(filepath.Dir(dir), ".claude.json")} {
			if v, _ := edit.GetJSON(p, "oauthAccount.accountUuid"); v != "" {
				return "sub"
			}
		}
		if v, _ := edit.GetJSON(a.Path, "env.ANTHROPIC_API_KEY"); v != "" {
			return "key"
		}
		if v, _ := edit.GetJSON(a.Path, "apiKeyHelper"); v != "" {
			return "key"
		}
	case "codex":
		if codexChatGPT(dir) {
			return "chatgpt"
		}
		if v, _ := edit.GetJSON(filepath.Join(dir, "auth.json"), "OPENAI_API_KEY"); v != "" {
			return "key"
		}
	case "opencode", "mimocode":
		// the providers in its config, and those signed in through it
		n := map[string]bool{}
		var cfg map[string]json.RawMessage
		if v, _ := edit.GetJSON(a.Path, "provider"); json.Unmarshal([]byte(v), &cfg) == nil {
			for k := range cfg {
				n[k] = true
			}
		}
		home, _ := os.UserHomeDir()
		var auth map[string]json.RawMessage
		if b, err := os.ReadFile(filepath.Join(home, ".local", "share", a.ID, "auth.json")); err == nil && json.Unmarshal(b, &auth) == nil {
			for k := range auth {
				n[k] = true
			}
		}
		delete(n, magpieID)
		if len(n) > 0 {
			return "providers:" + strconv.Itoa(len(n))
		}
	}
	return ""
}

// startsWith is, for an agent that reads magpie's models only as it
// starts, how to find its processes (patterns for pgrep -f) and the files
// whose change they missed.
func (a *Agent) startsWith() (pats []string, files []string) {
	switch a.ID {
	case "codex":
		return []string{`(^|/)codex( |$)`}, []string{a.Path, filepath.Join(filepath.Dir(a.Path), "magpie-models.json")}
	case "claude":
		return []string{`(^|/)claude( |$)`}, []string{a.Path}
	}
	return nil, nil
}

// Stale is how many copies of the agent are running that started before
// magpie last changed what it reads at start: they still have the list
// they started with until reopened. Zero where it can't be told (Windows).
func (a *Agent) Stale() int {
	pats, files := a.startsWith()
	if len(pats) == 0 || a.WSL != "" || runtime.GOOS == "windows" {
		return 0
	}
	var changed time.Time
	for _, f := range files {
		if st, err := os.Stat(f); err == nil && st.ModTime().After(changed) {
			changed = st.ModTime()
		}
	}
	if changed.IsZero() {
		return 0
	}
	n := 0
	for _, pat := range pats {
		out, _ := proc.Command("pgrep", "-f", pat).Output()
		pids := strings.Fields(string(out))
		if len(pids) == 0 {
			continue
		}
		out, _ = proc.Command("ps", "-o", "etime=", "-p", strings.Join(pids, ",")).Output()
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if up, ok := elapsed(l); ok && time.Now().Add(-up).Before(changed.Add(-time.Second)) {
				n++
			}
		}
	}
	return n
}

// elapsed reads ps's etime, [[dd-]hh:]mm:ss.
func elapsed(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	var days int
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, false
		}
		days, s = n, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	secs := 0
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, false
		}
		secs = secs*60 + n
	}
	return time.Duration(days)*24*time.Hour + time.Duration(secs)*time.Second, true
}
