package provider

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A Claude account's windows as its usage endpoint tells them: an unused
// one with no reset, a model's weekly one beside the account's.
func TestClaudeWarmStartsTheAccountsWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude-warmup.json")
	now := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	f := &fakeWarm{now: now, errs: map[string]error{}}
	f.ws = map[string][]QuotaWindow{
		"a@example.com": {
			{Name: "5 hours", Span: fiveHours},
			win("7 days", week, 20, now.Add(2*24*time.Hour)),
			{Name: "7 days · Opus", Span: week, Model: "opus"},
		},
	}
	// weekly only: the 5-hour window is left, the running week noted
	if rs := f.run(t, path, "week"); len(rs) != 0 {
		t.Fatalf("week: %+v", rs)
	}
	rs := f.run(t, path, "all")
	if len(rs) != 1 || strings.Join(rs[0].Windows, ",") != "5 hours" {
		t.Fatalf("all: %+v", rs)
	}
	if !codexWarmedIn(path)["a@example.com"].Equal(now) {
		t.Fatal("not kept")
	}
}
