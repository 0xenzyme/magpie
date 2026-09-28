package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/yetone/magpie/internal/gateway"
)

func utf16le(s string, bom bool) []byte {
	var b []byte
	if bom {
		b = append(b, 0xff, 0xfe)
	}
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

// wsl.exe -l -q speaks UTF-16LE, a BOM or not, or UTF-8 under WSL_UTF8.
func TestParseDistros(t *testing.T) {
	list := "Ubuntu-24.04\r\ndocker-desktop\r\nDebian\r\n\r\n"
	want := []string{"Ubuntu-24.04", "Debian"}
	for name, b := range map[string][]byte{
		"utf16":     utf16le(list, false),
		"utf16 bom": utf16le(list, true),
		"utf8":      []byte(list),
	} {
		if got := parseDistros(b); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %q", name, got)
		}
	}
	if got := parseDistros(nil); len(got) != 0 {
		t.Errorf("none: %q", got)
	}
}

func TestWSLMirrored(t *testing.T) {
	for cfg, want := range map[string]bool{
		"":                                  false,
		"[wsl2]\nnetworkingMode=mirrored\n": true,
		"\ufeff[WSL2]\r\nnetworkingmode = Mirrored # yes\r\n":           true,
		"[wsl2]\nnetworkingMode=\"mirrored\"\n":                         true,
		"[wsl2]\nnetworkingMode=nat\n":                                  false,
		"[wsl2]\n#networkingMode=mirrored\n":                            false,
		"[experimental]\nnetworkingMode=mirrored\n":                     false,
		"[wsl2]\nmemory=8GB\n[experimental]\nnetworkingMode=mirrored\n": false,
	} {
		if got := wslMirrored(cfg); got != want {
			t.Errorf("%q: %v", cfg, got)
		}
	}
}

func TestParseProbe(t *testing.T) {
	d := parseProbe("Ubuntu", "home:/home/me/\ndir:.codex\nroute:default via 172.20.0.1 dev eth0 proto kernel\nns:nameserver 10.255.255.254\n")
	if d == nil || d.Home != "/home/me" || !d.Has["dir:.codex"] || d.Has["bin:codex"] || d.Gateway != "172.20.0.1" {
		t.Fatalf("%+v", d)
	}
	if d := parseProbe("U", "home:/root\nbin:codex\nns:nameserver 172.30.0.1\n"); d == nil || d.Gateway != "172.30.0.1" || !d.Has["bin:codex"] {
		t.Fatalf("resolv.conf: %+v", d)
	}
	if d := parseProbe("U", "sh: not found\n"); d != nil {
		t.Fatalf("no home: %+v", d)
	}
}

// A path magpie opens through \\wsl.localhost is the distro's own path in
// the distro's config.
func TestWSLPaths(t *testing.T) {
	d := distro{Root: `\\wsl.localhost\Ubuntu`}
	if got := d.native(`\\wsl.localhost\Ubuntu\home\me\.codex\magpie-models.json`); got != "/home/me/.codex/magpie-models.json" {
		t.Error(got)
	}
	if got := d.native(`\\WSL.LOCALHOST\Ubuntu\home\me`); got != "/home/me" {
		t.Error(got)
	}
	if got := d.local("/home/me/.codex"); got != `\\wsl.localhost\Ubuntu`+string(filepath.Separator)+filepath.Join("home", "me", ".codex") {
		t.Error(got)
	}
	d.Gateway = "172.20.0.1"
	if got, want := d.base(), "http://172.20.0.1:"+gateway.Port(); got != want {
		t.Errorf("nat: %s", got)
	}
	d.Mirrored = true
	if got := d.base(); got != gateway.URL() {
		t.Errorf("mirrored: %s", got)
	}
	if p := d.place("codex@wsl:U"); p.key("codex.model") != "codex@wsl:U.model" || here("").key("codex.model") != "codex.model" {
		t.Error("stash keys")
	}
}

// fakeDistro is a distro whose / is a temp dir, its $HOME the codexHome.
func fakeDistro(home string, mirrored bool) distro {
	return distro{Name: "Ubuntu-24.04", Root: filepath.Dir(home), Home: "/" + filepath.Base(home),
		Has: map[string]bool{"dir:.codex": true}, Gateway: "172.20.0.1", Mirrored: mirrored}
}

// Codex in a distro under NAT, not signed in: magpie as its provider at the
// Windows host, its catalog named by the distro's path, what was there
// stashed under the distro's agent.
func TestWSLCodexProvider(t *testing.T) {
	home, read := codexHome(t, "", "model = \"gpt-5.5\"\n")
	d := fakeDistro(home, false)
	a := wslCodex(d)
	if a.ID != "codex@wsl:Ubuntu-24.04" || a.Name != "Codex · WSL Ubuntu-24.04" || !a.Detected() || a.Bin != "" {
		t.Fatalf("%+v", a)
	}
	if err := a.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	cfg := read()
	nat := "http://172.20.0.1:" + gateway.Port()
	if !strings.Contains(cfg, `model_catalog_json = "`+d.Home+`/.codex/magpie-models.json"`) ||
		!strings.Contains(cfg, `base_url = "`+nat+`/v1"`) || !strings.Contains(cfg, `model_provider = "magpie"`) {
		t.Fatalf("config:\n%s", cfg)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "magpie-models.json")); err != nil {
		t.Fatal(err)
	}
	if c := a.Check(); c != "" {
		t.Fatal(c)
	}
	if s := stashLoad(); s["codex@wsl:Ubuntu-24.04.model"] != "gpt-5.5" || s["codex.model"] != "" {
		t.Fatalf("stash %v", s)
	}
	if !strings.Contains(a.Notice(), "mirrored") {
		t.Error(a.Notice())
	}
	if err := a.Fields[0].Set("gpt-5.4"); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); strings.Contains(cfg, "model_provider =") || strings.Contains(cfg, "model_catalog_json") {
		t.Fatalf("back:\n%s", cfg)
	}
}

// Signed in and mirrored: the base URL is 127.0.0.1, as on Windows; under
// NAT it is the host, and either is known as magpie's again.
func TestWSLCodexBaseURL(t *testing.T) {
	for _, mirrored := range []bool{true, false} {
		home, read := codexHome(t, `{"OPENAI_API_KEY":"sk-x"}`, "model = \"gpt-5.5\"\n")
		a := wslCodex(fakeDistro(home, mirrored))
		if err := a.Fields[0].Set("fake/m1"); err != nil {
			t.Fatal(err)
		}
		want := gateway.URL()
		if !mirrored {
			want = "http://172.20.0.1:" + gateway.Port()
		}
		if cfg := read(); !strings.Contains(cfg, `openai_base_url = "`+want+gateway.CodexPath+`"`) {
			t.Fatalf("mirrored %v:\n%s", mirrored, cfg)
		}
		if c := a.Check(); c != "" {
			t.Fatalf("mirrored %v: %s", mirrored, c)
		}
	}
}

// Off Windows there are none, and nothing is run.
func TestWSLAgentsElsewhere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("on Windows")
	}
	if len(wslAgents()) != 0 {
		t.Fatal("wsl agents off windows")
	}
}
