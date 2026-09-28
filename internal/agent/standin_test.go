package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/gateway"
)

func TestClaudeStandIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	write := func(s string) {
		if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"env":{"ANTHROPIC_BASE_URL":"` + gateway.URL() + `","ANTHROPIC_MODEL":"a/main","ANTHROPIC_DEFAULT_HAIKU_MODEL":"a/small","ANTHROPIC_DEFAULT_OPUS_MODEL":"a/big"}}`)
	for asked, want := range map[string]string{
		"claude-haiku-4-5-20251001": "a/small",
		"claude-opus-5-5":           "a/big",
		"claude-sonnet-5":           "a/main", // a tier without a model follows the main one
		"gpt-5":                     "a/main",
	} {
		if got := claudeStandIn(path, asked); got != want {
			t.Errorf("%s: %q, want %q", asked, got, want)
		}
	}
	// Claude Code on Anthropic's own endpoint: nothing stands in
	write(`{"env":{"ANTHROPIC_MODEL":"a/main"}}`)
	if got := claudeStandIn(path, "claude-haiku-4-5"); got != "" {
		t.Errorf("not routed: %q", got)
	}
	if got := StandIn("codex", "claude-haiku-4-5"); got != "" {
		t.Errorf("another agent: %q", got)
	}
}
