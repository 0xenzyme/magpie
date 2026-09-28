package provider

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// a second look at Claude Code's sign-in serves the last answer at once and
// asks the CLI again behind it (#123)
func TestClaudeIdentityServedWhileAsked(t *testing.T) {
	dir := t.TempDir()
	exe, who := filepath.Join(dir, "claude"), filepath.Join(dir, "who")
	os.WriteFile(who, []byte("a@example.com"), 0o600)
	os.WriteFile(exe, []byte("#!/bin/sh\nsleep 1\nprintf '{\"loggedIn\":true,\"email\":\"%s\",\"subscriptionType\":\"max\"}' \"$(cat "+who+")\"\n"), 0o755)
	old := claudeExecutable
	claudeExecutable = func() string { return exe }
	t.Cleanup(func() { claudeExecutable = old; forgetClaudeStatus() })
	forgetClaudeStatus()

	if u, p, out := claudeIdentity(); u != "a@example.com" || p != "max" || out {
		t.Fatalf("first: %q %q %v", u, p, out)
	}
	os.WriteFile(who, []byte("b@example.com"), 0o600)
	claudeStatusMu.Lock()
	claudeStatusAt = time.Now().Add(-time.Minute)
	claudeStatusMu.Unlock()
	start := time.Now()
	if u, _, _ := claudeIdentity(); u != "a@example.com" {
		t.Fatalf("stale look: %q", u)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("a stale look waited %v for the CLI", d)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if u, _, _ := claudeIdentity(); u == "b@example.com" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the answer behind the last one never came")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
