package provider

// Signing in to a Factory account from magpie: WorkOS's device flow under
// droid's client, as `droid` runs it. magpie shows the code, the user
// confirms it on the page WorkOS names, and the tokens WorkOS then hands
// over are kept for magpie alone.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func startFactorySignIn(s *signInFlow) error {
	ctx, cancel := context.WithCancel(context.Background())
	dctx, dcancel := context.WithTimeout(ctx, 30*time.Second)
	var dc struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
		URI        string `json:"verification_uri"`
		Complete   string `json:"verification_uri_complete"`
		Interval   int    `json:"interval"`
	}
	err := factoryDevice(dctx, &dc)
	dcancel()
	if err != nil {
		cancel()
		return err
	}
	if dc.DeviceCode == "" || dc.UserCode == "" {
		cancel()
		return errors.New("Factory's sign-in gave no device code")
	}
	s.mu.Lock()
	s.st.URL, s.st.Code = firstNonEmpty(dc.Complete, dc.URI), dc.UserCode
	s.stop = cancel
	s.mu.Unlock()
	interval := time.Duration(max(dc.Interval, 1)) * time.Second
	go func() {
		defer cancel()
		fail := func(msg string) { s.finish(SignInState{State: "failed", Error: msg}) }
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
			t, err := factoryAuthenticate(ctx, url.Values{
				"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
				"device_code": {dc.DeviceCode}, "client_id": {factoryClientID}})
			switch {
			case ctx.Err() != nil:
				return
			case err != nil:
				continue // a hiccup: ask again
			case t.Error == "authorization_pending":
				continue
			case t.Error == "slow_down":
				interval += time.Second
				continue
			case t.Error == "expired_token":
				fail("the code expired; start again")
				return
			case t.Error == "access_denied":
				fail("the sign-in was declined")
				return
			case t.Error != "" || t.Access == "":
				fail("Factory: " + firstNonEmpty(strings.TrimSpace(t.Error+" "+t.Desc), "no token came back"))
				return
			}
			user, err := factorySignedInWith(ctx, t)
			if err != nil {
				fail(err.Error())
				return
			}
			s.finish(SignInState{State: "done", User: user, Using: strings.EqualFold(activeOf(factorySide()), user)})
			return
		}
	}()
	return nil
}

// factoryDevice asks WorkOS for a device code.
func factoryDevice(ctx context.Context, v any) error {
	b, code, err := factoryPost(ctx, "/authorize/device", url.Values{"client_id": {factoryClientID}})
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return &factoryStatus{code, "Factory sign-in: " + APIError(b, http.StatusText(code))}
	}
	return json.Unmarshal(b, v)
}

// factorySignedInWith keeps the account WorkOS just signed in: its token put
// in its org, as droid does when the sign-in names none, and where Factory
// serves that org from.
func factorySignedInWith(ctx context.Context, t factoryTokens) (string, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	c := factoryCreds{Access: t.Access, Refresh: t.Refresh, ExpiresAt: factoryExpiry(t.Access),
		Org: t.Org, Email: t.User.Email, UserID: t.User.ID}
	claims := jwtClaims(t.Access)
	c.Org = firstNonEmpty(c.Org, claimString(claims, "org_id"))
	c.Email = firstNonEmpty(c.Email, claimString(claims, "email"))
	c.UserID = firstNonEmpty(c.UserID, claimString(claims, "sub"))
	if c.Org == "" {
		var orgs struct {
			IDs []string `json:"workosOrgIds"`
		}
		if err := factoryGet(ctx, c, "/api/cli/org", nil, &orgs); err != nil {
			return "", err
		}
		if len(orgs.IDs) == 0 {
			return "", errors.New("this account belongs to no Factory organization; finish setting it up at app.factory.ai")
		}
		r, err := factoryRenew(ctx, c.Refresh, orgs.IDs[0])
		if err != nil {
			return "", err
		}
		c.Access, c.ExpiresAt, c.Org = r.Access, factoryExpiry(r.Access), orgs.IDs[0]
		if r.Refresh != "" {
			c.Refresh = r.Refresh
		}
	}
	var who struct {
		UserID string `json:"userId"`
		Email  string `json:"email"`
		Region string `json:"region"`
	}
	if err := factoryGet(ctx, c, "/api/cli/whoami", map[string]string{"X-Factory-Whoami-Extended": "true"}, &who); err != nil {
		return "", err
	}
	c.Region = who.Region
	c.Email = firstNonEmpty(c.Email, who.Email)
	c.UserID = firstNonEmpty(c.UserID, who.UserID)
	user := firstNonEmpty(c.Email, c.UserID)
	if user == "" {
		return "", errors.New("signed in, but Factory didn't say whose account it is; try again")
	}
	auth, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	if err := addSideLogin(savedLogin{Agent: "factory", User: user, Auth: auth}, "", func(savedLogin) {}); err != nil {
		return "", err
	}
	// signed in again: whatever WorkOS refused before is over
	_ = editSideLogin("factory", user, func(ls []savedLogin, i int) ([]savedLogin, error) {
		ls[i].Lapsed = ""
		return ls, nil
	})
	forgetAccountCaches()
	return user, nil
}
