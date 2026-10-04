package agent

// Cursor Private Inference is a build of Cursor whose agent runs on this
// machine (its cursor-local-agent-runtime extension) and asks a model
// endpoint of the user's rather than Cursor's backend (#299). It is
// installed as Cursor is — the same app name, bundle id and ~/.cursor —
// and tells itself apart only by its product.json ("nameShort": "Cursor
// Private Inference"). The endpoint it takes from a model's own settings,
// else its Open configuration dialog's (kept in the state.vscdb regular
// Cursor shares), else from its environment: CURSOR_LOCAL_AGENT_BASE_URL
// and CURSOR_LOCAL_AGENT_API_KEY. magpie writes nothing of Cursor's: its
// row gives the command that starts the app with those (see Agent.Launch),
// and the gateway's /models tells it, for each model, the APIs it is
// served on (api_types) and its limits (capabilities), which it reads to
// pick Anthropic Messages, Responses or Chat and its context.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/gateway"
)

// CursorLocalID is the agent's id, which its key names (TokenFor).
const CursorLocalID = "cursor-local"

// cursorLocalName is product.json's nameShort for the build.
const cursorLocalName = "Cursor Private Inference"

// cursorLocalApp is where the build is installed: its program, to start,
// and "" when it isn't. A var so tests can point it elsewhere.
var cursorLocalApp = func() string {
	cursorLocalSeen.Lock()
	defer cursorLocalSeen.Unlock()
	if time.Since(cursorLocalSeen.at) > 30*time.Second {
		cursorLocalSeen.app, cursorLocalSeen.at = findCursorLocal(cursorLocalRoots()), time.Now()
	}
	return cursorLocalSeen.app
}

// cursorLocalSeen is where the build was last found, looked for again
// after half a minute: the apps folder is read every time the agents are.
var cursorLocalSeen struct {
	sync.Mutex
	app string
	at  time.Time
}

// cursorLocalRoots are the folders apps are installed in on this system.
func cursorLocalRoots() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return []string{"/Applications", filepath.Join(home, "Applications")}
	case "windows":
		var out []string
		for _, v := range []string{"LOCALAPPDATA", "ProgramFiles"} {
			if d := os.Getenv(v); d != "" {
				out = append(out, filepath.Join(d, "Programs"), d)
			}
		}
		return out
	}
	return []string{"/opt", "/usr/share", "/usr/lib", filepath.Join(home, ".local", "share"), filepath.Join(home, "Applications")}
}

// findCursorLocal is the program of the Cursor Private Inference among the
// apps in roots: one whose product.json names it, a Mac's bundle by its
// Info.plist's CFBundleExecutable, else by the applicationName
// product.json gives (cursor, Cursor.exe).
func findCursorLocal(roots []string) string {
	for _, root := range roots {
		ents, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range ents {
			dir := filepath.Join(root, e.Name())
			if strings.HasSuffix(e.Name(), ".app") {
				res := filepath.Join(dir, "Contents", "Resources", "app")
				if p, ok := cursorLocalProduct(res); ok {
					if exe := plistExecutable(filepath.Join(dir, "Contents", "Info.plist")); exe != "" {
						return filepath.Join(dir, "Contents", "MacOS", exe)
					}
					return filepath.Join(dir, "Contents", "MacOS", p.exe())
				}
				continue
			}
			if p, ok := cursorLocalProduct(filepath.Join(dir, "resources", "app")); ok {
				name := p.exe()
				if runtime.GOOS == "windows" {
					name = p.NameShort + ".exe"
					if !isFile(filepath.Join(dir, name)) {
						name = p.exe() + ".exe"
					}
				}
				return filepath.Join(dir, name)
			}
		}
	}
	return ""
}

type cursorProduct struct {
	NameShort       string `json:"nameShort"`
	ApplicationName string `json:"applicationName"`
}

// exe is the program's name as product.json gives it: Cursor for cursor.
func (p cursorProduct) exe() string {
	n := p.ApplicationName
	if n == "" {
		return "Cursor"
	}
	if runtime.GOOS == "linux" {
		return n
	}
	return strings.ToUpper(n[:1]) + n[1:]
}

// cursorLocalProduct reads the product.json in an app's resources, which
// is the build's when it names it.
func cursorLocalProduct(res string) (cursorProduct, bool) {
	var p cursorProduct
	b, err := os.ReadFile(filepath.Join(res, "product.json"))
	if err != nil || json.Unmarshal(b, &p) != nil {
		return p, false
	}
	return p, strings.EqualFold(strings.TrimSpace(p.NameShort), cursorLocalName)
}

var plistExeRe = regexp.MustCompile(`<key>CFBundleExecutable</key>\s*<string>([^<]+)</string>`)

// plistExecutable is CFBundleExecutable of an XML Info.plist, "" when it
// can't be read.
func plistExecutable(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if m := plistExeRe.FindSubmatch(b); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return ""
}

// CursorLocalLaunch is the command that starts the build at app on the
// gateway at gw, in the shell of this system. Started so, from a shell,
// the app has the variables; opened from the Dock or Finder it has only
// the login shell's.
func CursorLocalLaunch(app, gw string) string {
	key, base := gateway.TokenFor(CursorLocalID), gw+"/v1"
	if runtime.GOOS == "windows" {
		return `$env:CURSOR_LOCAL_AGENT_BASE_URL="` + base + `"; $env:CURSOR_LOCAL_AGENT_API_KEY="` + key + `"; & '` + strings.ReplaceAll(app, "'", "''") + `'`
	}
	return "CURSOR_LOCAL_AGENT_BASE_URL=" + base + " CURSOR_LOCAL_AGENT_API_KEY=" + key + " '" + strings.ReplaceAll(app, "'", `'\''`) + "'"
}

func cursorLocal() *Agent {
	return &Agent{
		ID: CursorLocalID, Name: cursorLocalName, Icon: "cursor", Aliases: []string{"cursor-private-inference"},
		detect: func() bool { return cursorLocalApp() != "" },
		Launch: func() string {
			if app := cursorLocalApp(); app != "" {
				return CursorLocalLaunch(app, gateway.URL())
			}
			return ""
		},
		Notice: func() string {
			return cursorLocalName + " takes magpie's gateway from how it is started: quit it, then start it with the command its row copies. A base URL set in its Open configuration comes first, so leave that empty (or set it to " + gateway.URL() + "/v1 with the key " + gateway.TokenFor(CursorLocalID) + ")."
		},
	}
}
