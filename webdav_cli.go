package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/davsync"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/settings"
)

// WebDAV sync from the terminal: what Settings › WebDAV sync does, through
// the same davsync.Configure, Now, Off and Dismiss.

const webdavUsage = `usage:
  magpie webdav                           whether WebDAV sync is on, where to, and how the last sync went
  magpie webdav on <address> [user=…] [keys=no] [agents=no] [library=no]
                                          keep providers, settings, profiles, agents' models and the library
                                          the same on every computer through a WebDAV folder (a folder named
                                          magpie is made in it), and sync at once; asks for the password
                                          (with a user) and the passphrase
  magpie webdav set k=v…                  change it: address, user, keys, agents, library (yes|no);
                                          password= and passphrase= ask for a new one
  magpie webdav now                       sync now (the gateway does every 3 minutes, while it runs)
  magpie webdav dismiss                   clear what the last sync said it replaced
  magpie webdav off                       turn it off; the file on the server stays

  passphrase  the same on every computer: the file is sealed with it on this one, and the server only
              ever sees it sealed. Keep it: without it the file can't be opened. Never the password:
              the server is sent that
  password    asked for, unechoed, never given on the command line, as the passphrase isn't. The one saved
              is only sent to the server and user it was given for: change either and it is asked for
              again; user= alone for a server that asks for no sign-in. An app password where the server
              has them
  keys        no: providers go without their API keys, and each computer keeps its own
  first sync  on a computer that had its own setup, each part that differs becomes the server's; what
              was here is kept in the sync folder beside magpie's files, and magpie webdav says so

  Piped in, the password (when asked) is the first line of stdin and the passphrase the next.

  e.g. magpie webdav on https://dav.jianguoyun.com/dav/ user=me@example.com
       magpie webdav set agents=no library=no
`

// webdavCmd is magpie webdav ….
func webdavCmd(args []string) error {
	if len(args) == 0 || args[0] == "show" {
		return webdavShow()
	}
	switch args[0] {
	case "on":
		return webdavSet(args[1:], true)
	case "set":
		return webdavSet(args[1:], false)
	case "now":
		return webdavNow()
	case "dismiss":
		if err := davsync.Dismiss(); err != nil {
			return err
		}
		return webdavShow()
	case "off":
		if err := davsync.Off(); err != nil {
			return err
		}
		fmt.Println("WebDAV sync is off; the file on the server stays")
		return nil
	case "help", "-h", "--help":
		fmt.Print(webdavUsage)
		return nil
	}
	return fmt.Errorf("unknown webdav command %q\n\n%s", args[0], webdavUsage)
}

// webdavSet turns sync on (on), or changes it (set): what is not given
// stays as it was.
func webdavSet(args []string, on bool) error {
	c, was := davsync.Load()
	if !was {
		if !on {
			return errors.New("WebDAV sync is off: magpie webdav on <address> [user=…] turns it on")
		}
		c = davsync.Config{Keys: true, Agents: true}
	}
	askPass, askPhrase := false, c.Passphrase == ""
	address := false
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if !ok || strings.Contains(k, "://") { // the address, bare, = in it or not
			if address {
				return fmt.Errorf("one address, not %q too\n\n%s", a, webdavUsage)
			}
			k, v, address = "address", a, true
		}
		switch k {
		case "address":
			c.URL = v
		case "user":
			c.User = v
		case "password", "passphrase": // not in the shell's history, nor in ps
			if v != "" {
				return fmt.Errorf("the %s is asked for, not given: %s= alone", k, k)
			}
			if k == "password" {
				askPass = true
			} else {
				askPhrase = true
			}
		case "keys", "agents", "library":
			if v != "yes" && v != "no" {
				return fmt.Errorf("%s=yes|no, not %q", k, v)
			}
			yes := v == "yes"
			switch k {
			case "keys":
				c.Keys = yes
			case "agents":
				c.Agents = yes
			default:
				c.Library = &yes
			}
		default:
			return fmt.Errorf("unknown field %q (address, user, password, passphrase, keys, agents, library)", k)
		}
	}
	if strings.TrimSpace(c.URL) == "" {
		return fmt.Errorf("no address\n\n%s", webdavUsage)
	}
	// a wrong one said before any secret is typed
	if err := davsync.CheckAddress(c.URL); err != nil {
		return err
	}
	// the password saved goes only to the server and user it was given
	// for: Configure keeps it there, and says when one has to be typed
	c.Password = ""
	if kept, needed := davsync.SavedPassword(c); needed || c.User != "" && !kept {
		askPass = true
	}
	var err error
	if askPass {
		host := c.URL
		if u, err := url.Parse(c.URL); err == nil && u.Host != "" {
			host = u.Host
		}
		if c.Password, err = secret("password", "Password for "+host+": ", false); err != nil {
			return err
		}
	}
	if askPhrase {
		if c.Passphrase, err = passphrase("Passphrase (the same on every computer): ", true); err != nil {
			return err
		}
	}
	if err := davsync.Configure(c); err != nil {
		return err
	}
	// and sync at once, so a wrong address or password shows now
	if err := webdavNow(); err != nil {
		fmt.Fprintln(os.Stderr, muted.Render("It is on: magpie webdav set k=v… changes it, magpie webdav off turns it off."))
		return err
	}
	return nil
}

// webdavNow syncs once, then says how it went.
func webdavNow() error {
	if _, ok := davsync.Load(); !ok {
		return errors.New("WebDAV sync is off: magpie webdav on <address> [user=…] turns it on")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := davsync.Now(ctx); err != nil {
		return fmt.Errorf("couldn't sync: %w", err)
	}
	return webdavShow()
}

// webdavShow is sync as Settings shows it: never the secrets.
func webdavShow() error {
	v := davsync.Status()
	if !v.On {
		fmt.Println("WebDAV sync is off")
		fmt.Println(muted.Render("magpie webdav on <address> keeps providers, settings, profiles, agents' models and the library the same on every computer (magpie webdav help)"))
		return nil
	}
	var who []string
	if v.User != "" {
		who = append(who, v.User)
	}
	if v.PasswordSet {
		who = append(who, "password saved")
	}
	fmt.Println(bold.Render("WebDAV sync"), v.URL, muted.Render(strings.Join(who, " · ")))

	what := []string{"providers without their API keys"}
	if v.Keys {
		what[0] = "providers with their API keys"
	}
	what = append(what, "settings", "profiles")
	if v.Agents {
		what = append(what, "agents' models")
	}
	if v.Library {
		what = append(what, "library")
	}
	fmt.Println("  syncs", strings.Join(what, ", "))

	switch {
	case v.Error != "":
		fmt.Println("  couldn't sync:", v.Error)
	case v.Last.IsZero():
		fmt.Println("  " + muted.Render("not synced yet"))
	default:
		fmt.Println(" ", green.Render("✓"), "synced", syncWhen(v.Last))
	}
	if n := v.Notice; n != nil {
		if len(n.Here) > 0 {
			fmt.Println("  Replaced here by newer ones from another computer:", partNames(n.Here))
		}
		if len(n.There) > 0 {
			fmt.Println("  Replaced on the server by this computer's newer ones:", partNames(n.There))
		}
		dir := n.Saved
		if dir == "" {
			dir = filepath.Join(settings.Dir(), "sync")
		}
		fmt.Println(muted.Render("  The copies replaced are kept in " + tilde(dir) + " (magpie restore opens one; magpie webdav dismiss clears this)"))
	}
	if !gateway.Running() {
		fmt.Println(muted.Render("  No gateway runs here: nothing syncs by itself until one does (the app, magpie web or magpie serve), every 3 minutes; magpie webdav now syncs once"))
	}
	return nil
}

func syncWhen(t time.Time) string {
	if y, m, d := time.Now().Date(); t.Year() == y && t.Month() == m && t.Day() == d {
		return t.Format("at 15:04")
	}
	return t.Format("2006-01-02 15:04")
}

// partNames are davsync's parts, as Settings names them.
func partNames(ps []string) string {
	names := map[string]string{"agents": "agents' models"}
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p
		if n, ok := names[p]; ok {
			out[i] = n
		}
	}
	return strings.Join(out, ", ")
}
