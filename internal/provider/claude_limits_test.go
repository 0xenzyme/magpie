package provider

import (
	"context"
	"testing"
	"time"
)

// What Claude Code says as it answers is the account's allowance, without
// the usage endpoint, which keeps the windows it alone tells.
func TestNoteClaudeLimits(t *testing.T) {
	old := claudeBase
	claudeBase = "http://127.0.0.1:1" // nothing answers there
	defer func() { claudeBase = old }()
	reset := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	claudeUsage.Lock()
	claudeUsage.m = map[string]claudeUsageEntry{"kev@example.com": {at: time.Now().Add(-time.Hour), ws: []QuotaWindow{
		{Name: "7 days · Opus", Used: 30, Model: "opus"}, {Name: "5 hours", Used: 5},
	}}}
	claudeUsage.Unlock()
	defer func() { claudeUsage.Lock(); claudeUsage.m = nil; claudeUsage.Unlock() }()

	NoteClaudeLimits("Kev@example.com", []ClaudeLimit{{Kind: "five_hour", Used: 0.42, ResetsAt: reset.Unix()}, {Kind: "seven_day", Used: 0.1}, {Kind: "overage", Used: 1}})
	ws, err := claudeWindows(context.Background(), "kev@example.com", "tok")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, w := range ws {
		names = append(names, w.Name)
	}
	if len(ws) != 3 || ws[0].Name != "5 hours" || ws[0].Used != 42 || !ws[0].ResetsAt.Equal(reset) || ws[0].Span != 5*time.Hour || ws[1].Name != "7 days" || ws[1].Used != 10 || ws[2].Name != "7 days · Opus" {
		t.Fatalf("windows: %v %+v", names, ws)
	}

	// past the cache, the usage endpoint failing leaves what was heard
	claudeUsage.Lock()
	e := claudeUsage.m["kev@example.com"]
	e.at = time.Now().Add(-10 * time.Minute)
	claudeUsage.m["kev@example.com"] = e
	claudeUsage.Unlock()
	if ws, err := claudeWindows(context.Background(), "kev@example.com", "tok"); err != nil || len(ws) != 3 {
		t.Fatalf("after the endpoint failed: %v %v", ws, err)
	}
	// heard long ago, the failure is told
	claudeUsage.Lock()
	e.heard = time.Now().Add(-2 * time.Hour)
	claudeUsage.m["kev@example.com"] = e
	claudeUsage.Unlock()
	if _, err := claudeWindows(context.Background(), "kev@example.com", "tok"); err == nil {
		t.Fatal("an old word hid the endpoint's failure")
	}
}
