package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// cliIdentity is who an agent's CLI says is signed in (`cursor-agent about`,
// `devin auth status`). Asking takes the CLI seconds, ten at worst, and
// every request of the gateway and every page of the window reads the
// accounts, so after the first answer a stale one is served while a fresh
// one is fetched behind it. The answer is kept on disk too: a magpie just
// started serves the last one at once rather than holding everything for
// the CLIs (#123), and asks them again behind it.
type cliIdentity struct {
	sync.Mutex
	name       string
	exe        func() string
	ask        func() (user, plan string, ok bool)
	at         time.Time
	refreshing bool
	read       bool // the one kept on disk was looked for
	user, plan string
	ok         bool
}

type keptIdentity struct {
	User string `json:"user,omitempty"`
	Plan string `json:"plan,omitempty"`
	OK   bool   `json:"ok"`
}

var identityFile sync.Mutex

func identityPath() string { return filepath.Join(filepath.Dir(Path()), "cli-identity.json") }

func readIdentities() map[string]keptIdentity {
	m := map[string]keptIdentity{}
	if b, err := os.ReadFile(identityPath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func (c *cliIdentity) keep() {
	identityFile.Lock()
	defer identityFile.Unlock()
	m := readIdentities()
	k := keptIdentity{User: c.user, Plan: c.plan, OK: c.ok}
	if m[c.name] == k {
		return
	}
	m[c.name] = k
	if b, err := json.MarshalIndent(m, "", "  "); err == nil {
		_ = writePrivate(identityPath(), append(b, '\n'))
	}
}

func (c *cliIdentity) get() (user, plan string, ok bool) {
	c.Lock()
	defer c.Unlock()
	if c.at.IsZero() && !c.read {
		c.read = true
		identityFile.Lock()
		k, found := readIdentities()[c.name]
		identityFile.Unlock()
		// served now, asked again behind it; a CLI since removed has no one
		if found && c.exe() != "" {
			c.user, c.plan, c.ok = k.User, k.Plan, k.OK
			c.at = time.Unix(1, 0)
		}
	}
	if c.at.IsZero() {
		c.user, c.plan, c.ok = c.ask()
		c.at = time.Now()
		c.keep()
	} else if time.Since(c.at) > time.Minute && !c.refreshing {
		c.refreshing = true
		go func() {
			u, p, ok := c.ask()
			c.Lock()
			c.user, c.plan, c.ok = u, p, ok
			c.at, c.refreshing = time.Now(), false
			c.keep()
			c.Unlock()
		}()
	}
	return c.user, c.plan, c.ok
}

// forget has the next look ask the CLI and wait for it: after a sign-in or
// a sign-out, the last answer is the wrong one.
func (c *cliIdentity) forget() {
	c.Lock()
	c.at, c.read = time.Time{}, true
	c.Unlock()
}
