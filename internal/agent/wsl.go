package agent

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/proc"
)

// Codex installed in a WSL distro reads its config there, not in Windows'
// home — so does the Codex app's WSL connection. On Windows magpie lists the
// distros (wsl.exe -l -q), asks each once for its $HOME and whether Codex is
// there, and edits the files through \\wsl.localhost\<distro>. Each is an
// agent of its own, codex@wsl:<distro>. The gateway it is pointed at is
// 127.0.0.1 when WSL shares Windows' network (networkingMode=mirrored in
// .wslconfig); under NAT it is Windows as WSL sees it, which reaches the
// gateway only while that listens beyond loopback.

// place is where an agent lives: its home as magpie opens it, how a path
// there is spelt in the agent's own config, and the gateway as it reaches
// it. here(home) is this machine's.
type place struct {
	home  string
	id    string              // the agent's id when not its own: its stash keys go under it
	spell func(string) string // a path under home as the agent names it; nil: as is
	base  func() string       // the gateway's URL from the agent; nil: gateway.URL
}

func here(home string) place { return place{home: home} }

func (p place) gw() string {
	if p.base != nil {
		return p.base()
	}
	return gateway.URL()
}

func (p place) v1() string       { return p.gw() + "/v1" }
func (p place) codexURL() string { return p.gw() + gateway.CodexPath }

// host is the gateway's host as the agent reaches it.
func (p place) host() string {
	if p.base == nil {
		return "127.0.0.1"
	}
	h, _, err := net.SplitHostPort(strings.TrimPrefix(p.base(), "http://"))
	if err != nil {
		return "127.0.0.1"
	}
	return h
}

// native is a path under home as the agent names it in its config.
func (p place) native(path string) string {
	if p.spell == nil {
		return path
	}
	return p.spell(path)
}

// key is a stash key of this machine's agent ("codex.model") as this
// place's agent's.
func (p place) key(k string) string {
	if p.id == "" {
		return k
	}
	_, rest, _ := strings.Cut(k, ".")
	return p.id + "." + rest
}

// distro is one WSL distro, as probed.
type distro struct {
	Name     string
	Home     string          // $HOME inside it, e.g. /home/me
	Root     string          // where magpie opens its / from, e.g. \\wsl.localhost\Ubuntu
	Has      map[string]bool // "dir:.codex", "bin:codex": what the probe found
	Gateway  string          // the Windows host as the distro reaches it, when not mirrored
	Mirrored bool
}

// local is a path inside the distro as magpie opens it.
func (d distro) local(linux string) string {
	return d.Root + strings.ReplaceAll(linux, "/", string(filepath.Separator))
}

// native is a path magpie opens as the distro spells it.
func (d distro) native(local string) string {
	rel := local
	if len(local) >= len(d.Root) && strings.EqualFold(local[:len(d.Root)], d.Root) {
		rel = local[len(d.Root):]
	}
	rel = strings.ReplaceAll(rel, `\`, "/")
	if !strings.HasPrefix(rel, "/") {
		rel = "/" + rel
	}
	return rel
}

// base is the gateway's URL from inside the distro.
func (d distro) base() string {
	if d.Mirrored || d.Gateway == "" {
		return gateway.URL()
	}
	return "http://" + net.JoinHostPort(d.Gateway, gateway.Port())
}

func (d distro) place(id string) place {
	return place{home: d.local(d.Home), id: id, spell: d.native, base: d.base}
}

// wslCodex is Codex in a distro: Codex's own reading and writing, at the
// distro's home, with the distro's way to the gateway.
func wslCodex(d distro) *Agent {
	id := "codex@wsl:" + d.Name
	a := codexIn(d.place(id))
	a.ID, a.Name, a.Aliases, a.Bin, a.UA, a.WSL = id, "Codex · WSL "+d.Name, nil, "", nil, d.Name
	a.detect = func() bool { return d.Has["dir:.codex"] || d.Has["bin:codex"] }
	// its requests carry Codex's User-Agent and are counted as Codex's
	// on Windows, so a prompt with none of "its" own seen isn't a bypass
	a.LastUsed = nil
	if reached := a.Reached; reached != nil {
		a.Reached = func(since time.Time) (time.Time, string, bool) {
			at, to, refused := reached(since)
			if sameHost(to, d.base()) {
				to = gateway.URL() // the gateway, however WSL reaches it
			}
			return at, to, refused
		}
	}
	a.Notice = func() string {
		if !d.Mirrored {
			return "WSL " + d.Name + " isn't in mirrored networking, so its Codex can't reach magpie on 127.0.0.1 and was pointed at Windows (" + d.base() +
				"), which answers only while the gateway listens beyond loopback and Windows' firewall lets WSL in. " +
				"Set networkingMode=mirrored under [wsl2] in %UserProfile%\\.wslconfig and run wsl --shutdown, then pick the model again."
		}
		return "Codex in WSL " + d.Name + " builds its model list at start-up — restart it (and the Codex app's WSL connection) to see this."
	}
	return a
}

// wslAgents are the agents in this machine's WSL distros; none off Windows.
func wslAgents() []*Agent {
	if runtime.GOOS != "windows" {
		return nil
	}
	var out []*Agent
	for _, d := range wslDistros() {
		// only where Codex is: magpie writes nothing into a distro without it
		if a := wslCodex(d); a.Detected() {
			out = append(out, a)
		}
	}
	return out
}

// The distro list is asked for again after a while; a distro is probed once
// (which starts it), and again only after a probe that failed.
var wsl struct {
	sync.Mutex
	at     time.Time
	names  []string
	probed map[string]*distro
	failed map[string]time.Time
}

const (
	wslListAge  = time.Minute
	wslRetryAge = 10 * time.Minute
)

func wslDistros() []distro {
	wsl.Lock()
	defer wsl.Unlock()
	if time.Since(wsl.at) > wslListAge {
		wsl.names = wslList()
		wsl.at = time.Now()
	}
	if wsl.probed == nil {
		wsl.probed, wsl.failed = map[string]*distro{}, map[string]time.Time{}
	}
	mirrored := wslMirrored(wslConfig())
	var out []distro
	for _, n := range wsl.names {
		d := wsl.probed[n]
		if d == nil {
			if t, ok := wsl.failed[n]; ok && time.Since(t) < wslRetryAge {
				continue
			}
			if d = wslProbe(n); d == nil {
				wsl.failed[n] = time.Now()
				continue
			}
			wsl.probed[n] = d
		}
		c := *d
		c.Mirrored = mirrored
		out = append(out, c)
	}
	return out
}

func wslCommand(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := proc.CommandContext(ctx, "wsl.exe", args...)
	// wsl.exe speaks UTF-16 unless told otherwise; either is read
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	return cmd.Output()
}

// wslList is the installed distros' names.
func wslList() []string {
	b, err := wslCommand(10*time.Second, "-l", "-q")
	if err != nil {
		return nil
	}
	return parseDistros(b)
}

// wslProbeScript prints the distro's home, what of Codex it has, and its
// default route (the Windows host under NAT).
const wslProbeScript = `echo "home:$HOME"; [ -d "$HOME/.codex" ] && echo dir:.codex; ` +
	`command -v codex >/dev/null 2>&1 && echo bin:codex; ` +
	`ip route show default 2>/dev/null | head -n1 | sed 's/^/route:/'; ` +
	`grep -m1 '^nameserver' /etc/resolv.conf 2>/dev/null | sed 's/^/ns:/'; true`

func wslProbe(name string) *distro {
	b, err := wslCommand(30*time.Second, "-d", name, "-e", "sh", "-lc", wslProbeScript)
	if err != nil {
		return nil
	}
	d := parseProbe(name, string(b))
	if d == nil {
		return nil
	}
	d.Root = `\\wsl.localhost\` + name
	if _, err := os.Stat(d.Root + `\`); err != nil {
		d.Root = `\\wsl$\` + name // before Windows 11 / WSL 0.50
	}
	return d
}

// parseProbe reads wslProbeScript's output.
func parseProbe(name, out string) *distro {
	d := &distro{Name: name, Has: map[string]bool{}}
	var ns string
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		switch k, v, _ := strings.Cut(l, ":"); k {
		case "home":
			d.Home = strings.TrimRight(v, "/")
		case "dir", "bin":
			d.Has[l] = true
		case "route":
			// default via 172.20.0.1 dev eth0 …
			if f := strings.Fields(v); len(f) >= 3 && f[1] == "via" && net.ParseIP(f[2]) != nil {
				d.Gateway = f[2]
			}
		case "ns":
			if f := strings.Fields(v); len(f) >= 2 && net.ParseIP(f[1]) != nil {
				ns = f[1]
			}
		}
	}
	if !strings.HasPrefix(d.Home, "/") {
		return nil
	}
	if d.Gateway == "" {
		d.Gateway = ns
	}
	return d
}

// parseDistros reads wsl.exe -l -q: UTF-16LE (with or without a BOM), or
// UTF-8 under WSL_UTF8. Docker Desktop's own distros are left out.
func parseDistros(b []byte) []string {
	s := decodeWSL(b)
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(strings.Trim(l, "\x00\ufeff\r"))
		if l == "" || strings.HasPrefix(strings.ToLower(l), "docker-desktop") {
			continue
		}
		out = append(out, l)
	}
	return out
}

func decodeWSL(b []byte) string {
	utf16le := len(b) >= 2 && (b[0] == 0xff && b[1] == 0xfe || b[1] == 0 && b[0] != 0)
	if !utf16le {
		return string(b)
	}
	if b[0] == 0xff && b[1] == 0xfe {
		b = b[2:]
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}

// wslConfig is %UserProfile%\.wslconfig, "" if there is none.
func wslConfig() string {
	home, _ := os.UserHomeDir()
	b, _ := os.ReadFile(filepath.Join(home, ".wslconfig"))
	return string(b)
}

// wslMirrored reports whether a .wslconfig puts WSL 2 in mirrored
// networking: networkingMode=mirrored under [wsl2].
func wslMirrored(cfg string) bool {
	section, mirrored := "", false
	sc := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(cfg, "\ufeff")))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || l[0] == '#' || l[0] == ';' {
			continue
		}
		if strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]") {
			section = strings.ToLower(strings.TrimSpace(l[1 : len(l)-1]))
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok || section != "wsl2" || !strings.EqualFold(strings.TrimSpace(k), "networkingMode") {
			continue
		}
		if i := strings.IndexAny(v, "#;"); i >= 0 {
			v = v[:i]
		}
		mirrored = strings.EqualFold(strings.Trim(strings.TrimSpace(v), `"`), "mirrored")
	}
	return mirrored
}
