package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `magpie claude default` takes magpie out and puts back the model Claude
// Code was on before it, as the Agents page's Disconnect does, the user's
// own lines kept (__jingling on X: it took their model away with
// magpie's); a field's own default still leaves the agent as installed.
func TestAgentDefaultPutsBackTheModelBefore(t *testing.T) {
	groupsHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	path := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte("{\n  // mine\n  \"model\": \"opus\",\n  \"theme\": \"dark\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	read := func() string {
		b, _ := os.ReadFile(path)
		return string(b)
	}
	if err := run([]string{"claude", "a/m"}); err != nil {
		t.Fatal(err)
	}
	if s := read(); !strings.Contains(s, `"model": "a/m"`) || !strings.Contains(s, "ANTHROPIC_BASE_URL") {
		t.Fatalf("not wired:\n%s", s)
	}
	if err := run([]string{"claude", "default"}); err != nil {
		t.Fatal(err)
	}
	s := read()
	if !strings.Contains(s, `"model": "opus"`) || strings.Contains(s, "ANTHROPIC_BASE_URL") || !strings.Contains(s, "// mine") || !strings.Contains(s, `"theme": "dark"`) {
		t.Fatalf("default didn't put back what was there:\n%s", s)
	}
	if err := run([]string{"claude", "model", "default"}); err != nil {
		t.Fatal(err)
	}
	if s := read(); strings.Contains(s, `"model"`) {
		t.Fatalf("the model field's default leaves Claude Code as installed:\n%s", s)
	}
}
