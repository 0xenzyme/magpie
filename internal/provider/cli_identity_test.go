package provider

import (
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestCLIIdentityKeptAcrossStarts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var asked atomic.Int32
	answer := func() (string, string, bool) { asked.Add(1); return "me@example.com", "Pro", true }
	exe := func() string { return "/bin/sh" }

	// never asked before: the first look waits for the CLI, and keeps it
	first := &cliIdentity{name: "x", exe: exe, ask: answer}
	if u, p, ok := first.get(); !ok || u != "me@example.com" || p != "Pro" || asked.Load() != 1 {
		t.Fatalf("first: %q %q %v, asked %d", u, p, ok, asked.Load())
	}
	if _, err := os.Stat(identityPath()); err != nil {
		t.Fatal(err)
	}

	// a magpie started again serves the kept one at once, however long the
	// CLI takes, and asks it behind that
	release := make(chan struct{})
	slow := func() (string, string, bool) { <-release; return "other@example.com", "Ultra", true }
	again := &cliIdentity{name: "x", exe: exe, ask: slow}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if u, p, ok := again.get(); !ok || u != "me@example.com" || p != "Pro" {
			t.Errorf("kept: %q %q %v", u, p, ok)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a start with a kept answer waited for the CLI")
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if u, _, _ := again.get(); u == "other@example.com" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the answer behind the kept one never came")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if k := readIdentities()["x"]; k.User != "other@example.com" || k.Plan != "Ultra" {
		t.Fatalf("kept after the refresh: %+v", k)
	}

	// after a sign-in or out, the next look waits for the CLI again
	again.forget()
	again.ask = func() (string, string, bool) { return "", "", false }
	if _, _, ok := again.get(); ok {
		t.Fatal("forget served the old answer")
	}
	if k := readIdentities()["x"]; k.OK {
		t.Fatalf("signed out, still kept: %+v", k)
	}
}

func TestCLIIdentityKeptIgnoredWithoutTheCLI(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	(&cliIdentity{name: "x", exe: func() string { return "/bin/sh" }, ask: func() (string, string, bool) { return "me@example.com", "", true }}).get()
	gone := &cliIdentity{name: "x", exe: func() string { return "" }, ask: func() (string, string, bool) { return "", "", false }}
	if u, _, ok := gone.get(); ok || u != "" {
		t.Fatalf("a removed CLI still served %q", u)
	}
}
