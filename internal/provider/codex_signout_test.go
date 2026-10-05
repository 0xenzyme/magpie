package provider

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Codex's only account is signed out when it is removed, as codex logout
// does (mamba on Discord); one with another saved is still refused, with a
// SignedInError the GUI says in the reader's language; and ForgetAccount,
// which takes the one in use last, signs Codex out too.
func TestCodexSignOutLastAccount(t *testing.T) {
	home := signIn(t)
	auth := filepath.Join(home, ".codex", "auth.json")
	rememberLogins(true)

	// another saved: the one signed in to is refused, as before
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	err := ForgetLogin("codex", "work@example.com")
	var signedIn *SignedInError
	if !errors.As(err, &signedIn) || signedIn.Agent != "codex" || signedIn.User != "work@example.com" {
		t.Fatalf("the one in use with another saved: %v", err)
	}
	if !strings.Contains(err.Error(), "Codex is signed in to work@example.com now") {
		t.Fatalf("message: %v", err)
	}
	if _, err := os.Stat(auth); err != nil {
		t.Fatalf("auth.json went on a refusal: %v", err)
	}

	// the other forgotten, the one left is signed out
	if err := ForgetLogin("codex", "me@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := ForgetLogin("codex", "work@example.com"); err != nil {
		t.Fatalf("the only account: %v", err)
	}
	if _, err := os.Stat(auth); !os.IsNotExist(err) {
		t.Fatalf("auth.json still there: %v", err)
	}
	if ls := Logins("codex"); len(ls) != 0 {
		t.Fatalf("still listed: %+v", ls)
	}
	rememberLogins(true)
	if ls := Logins("codex"); len(ls) != 0 {
		t.Fatalf("remembered again: %+v", ls)
	}

	// signed in but not saved yet: signed out all the same
	codexSignIn(t, home, "new@example.com", "r-new")
	if err := ForgetLogin("codex", "new@example.com"); err != nil {
		t.Fatalf("unsaved: %v", err)
	}
	if _, err := os.Stat(auth); !os.IsNotExist(err) {
		t.Fatalf("auth.json still there: %v", err)
	}

	// an account not signed in to and not saved is still unknown
	if err := ForgetLogin("codex", "nobody@example.com"); err == nil || errors.As(err, &signedIn) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestForgetAccountSignsCodexOut(t *testing.T) {
	home := signIn(t)
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	if err := ForgetAccount("codex"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("auth.json still there: %v", err)
	}
	if ls := Logins("codex"); len(ls) != 0 {
		t.Fatalf("saved still: %+v", ls)
	}
}

// Claude Code's account in use is never signed out by a removal, even its
// only one: its sign-in may be in the keychain, and magpie serves its
// saved accounts while it is signed out (claudeStandIn).
func TestClaudeInUseStillRefused(t *testing.T) {
	home := claudeHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	shellFakes(t)
	creds := claudeSignIn(t, home, time.Now().Add(time.Hour))
	writeFile(t, filepath.Join(home, ".claude.json"), map[string]any{"oauthAccount": map[string]any{"emailAddress": "cc@example.com"}})
	rememberLogins(true)
	live, ok := liveLogin("claude")
	if !ok {
		t.Fatal("no Claude Code sign-in")
	}
	var signedIn *SignedInError
	if err := ForgetLogin("claude", live.User); !errors.As(err, &signedIn) {
		t.Fatalf("claude in use: %v", err)
	}
	if _, err := os.Stat(creds); err != nil {
		t.Fatalf("credentials went: %v", err)
	}
}
