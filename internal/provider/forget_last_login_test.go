package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

// #874: a subscription signed in to one account of magpie's could never be
// signed out — "magpie uses … first; put another account first", with no
// other account to put first. The only one goes; with another there, the
// first still waits for it to be put first.
func TestForgetOnlyFirstLogin(t *testing.T) {
	claudeHome(t)
	auth := func(r string) json.RawMessage {
		b, _ := json.Marshal(googleAuth{RefreshToken: r})
		return b
	}
	saveLogins(t, savedLogin{Agent: "antigravity", User: "a@x.com", Auth: auth("ra"), On: true, First: true})
	ForgetAccounts()
	if ls := Logins("antigravity"); len(ls) != 1 || !ls[0].Active {
		t.Fatalf("logins before: %+v", ls)
	}
	if err := ForgetAccount("antigravity"); err != nil {
		t.Fatalf("signing out the only account: %v", err)
	}
	ForgetAccounts()
	if ls := Logins("antigravity"); len(ls) != 0 {
		t.Fatalf("still listed: %+v", ls)
	}
	for _, l := range readLogins() {
		if l.Agent == "antigravity" {
			t.Fatalf("logins.json still keeps %+v", l)
		}
	}

	// two accounts: Remove on the first still asks for the other first,
	// and signing the subscription out takes both
	saveLogins(t,
		savedLogin{Agent: "antigravity", User: "a@x.com", Auth: auth("ra"), On: true, First: true},
		savedLogin{Agent: "antigravity", User: "b@x.com", Auth: auth("rb"), On: true})
	ForgetAccounts()
	if err := ForgetLogin("antigravity", "a@x.com"); err == nil || !strings.Contains(err.Error(), "put another account first") {
		t.Fatalf("removing the first of two: %v", err)
	}
	if err := ForgetAccount("antigravity"); err != nil {
		t.Fatalf("signing out both: %v", err)
	}
	ForgetAccounts()
	if ls := Logins("antigravity"); len(ls) != 0 {
		t.Fatalf("still listed: %+v", ls)
	}
}
