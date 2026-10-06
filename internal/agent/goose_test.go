package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/testenv"
)

// Unexpanded folder variables must not send a config write into ./~/….
func TestGooseIgnoresTildeConfigHome(t *testing.T) {
	home := t.TempDir()
	testenv.SetHome(t, home)
	work := t.TempDir()
	t.Chdir(work)
	t.Setenv("XDG_CONFIG_HOME", "~/.config")
	t.Setenv("APPDATA", "~/AppData/Roaming")
	a, err := Find("goose")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "goose", "config.yaml")
	if a.Path != want {
		t.Errorf("Goose path = %q, want %q", a.Path, want)
	}
	if err := a.Field("effort").Set("high"); err != nil {
		t.Fatal(err)
	}
	if got, _ := edit.GetYAMLTop(want, "GOOSE_THINKING_EFFORT"); got != "high" {
		t.Errorf("effort at %s = %q, want high", want, got)
	}
	if entries, err := os.ReadDir(work); err != nil || len(entries) != 0 {
		t.Errorf("working directory changed: %v, %v", entries, err)
	}
}

// A goose on PATH that is pressly's migration tool (a Go program) is not the
// Goose agent (Discord: Jun, goose shown without ~/.config/goose); Block's
// goose, not a Go program, is.
func TestGooseDetectSkipsGoGoose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH lookup of a script")
	}
	home := t.TempDir()
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	a := goose(home, filepath.Join(home, ".config"))
	if a.Detected() {
		t.Fatal("detected with no goose at all")
	}

	src := filepath.Join(t.TempDir(), "main.go")
	os.WriteFile(src, []byte("package main\nfunc main() {}\n"), 0o644)
	gobin, err := exec.LookPath("go")
	if err != nil {
		gobin = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	build := exec.Command(gobin, "build", "-o", filepath.Join(bin, "goose"), src)
	build.Env = append(os.Environ(), "PATH="+os.Getenv("PATH"))
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("go build: %v %s", err, out)
	}
	if a.Detected() {
		t.Fatal("a Go goose on PATH was taken for the agent")
	}

	os.WriteFile(filepath.Join(bin, "goose"), []byte("#!/bin/sh\necho 1.9.0\n"), 0o755)
	if !a.Detected() {
		t.Fatal("Block's goose on PATH not detected")
	}

	os.Remove(filepath.Join(bin, "goose"))
	os.MkdirAll(filepath.Join(home, ".config", "goose"), 0o755)
	if !a.Detected() {
		t.Fatal("goose's config dir not detected")
	}
}

// Goose is offered magpie's models, and picking one makes magpie a custom
// provider of Goose's that hands it each model's window and says which
// models Goose can send a thinking level (wani on Discord: Goose's GUI
// offered no thinking level and had the context wrong). A model Goose sends
// no level for (GLM, on an OpenAI-compatible provider) is not said to think.
func TestGooseRunsOnMagpie(t *testing.T) {
	home := syncHome(t)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	if err := provider.Save(provider.Provider{ID: "oa", Name: "OA", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"gpt-5.5"}}); err != nil {
		t.Fatal(err)
	}
	a := goose(home, filepath.Join(home, ".config"))
	f := a.Fields[0]
	var vals []string
	for _, o := range f.Options(map[string]string{}) {
		vals = append(vals, o.Value)
	}
	if !slices.Contains(vals, "magpie/relay/glm-4.6") || !slices.Contains(vals, "magpie/oa/gpt-5.5") {
		t.Fatalf("magpie's models not offered: %v", vals)
	}
	if err := f.Set("magpie/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	if got := f.Get(); got != "magpie/relay/glm-4.6" {
		t.Fatalf("model = %q", got)
	}
	file := gooseProviderPath(a.Path)
	if filepath.Dir(filepath.Dir(file)) != filepath.Dir(a.Path) {
		t.Fatalf("custom provider at %s, config at %s", file, a.Path)
	}
	var p struct {
		Name          string            `json:"name"`
		Engine        string            `json:"engine"`
		BaseURL       string            `json:"base_url"`
		APIKeyEnv     *string           `json:"api_key_env"`
		RequiresAuth  *bool             `json:"requires_auth"`
		DynamicModels *bool             `json:"dynamic_models"`
		Skip          bool              `json:"skip_canonical_filtering"`
		Headers       map[string]string `json:"headers"`
		Models        []struct {
			Name      string `json:"name"`
			Context   int    `json:"context_limit"`
			Reasoning bool   `json:"reasoning"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(readFile(file)), &p); err != nil {
		t.Fatalf("%v: %s", err, readFile(file))
	}
	if p.Name != "magpie" || p.Engine != "openai" || p.BaseURL != gatewayV1() ||
		p.Headers["Authorization"] != "Bearer "+gateway.Token || p.APIKeyEnv == nil || *p.APIKeyEnv != "" ||
		p.RequiresAuth == nil || *p.RequiresAuth || p.DynamicModels == nil || *p.DynamicModels || !p.Skip {
		t.Fatalf("provider: %s", readFile(file))
	}
	think := map[string]bool{}
	window := map[string]int{}
	for _, m := range p.Models {
		think[m.Name], window[m.Name] = m.Reasoning, m.Context
	}
	if window["relay/glm-4.6"] != 204800 {
		t.Fatalf("glm window = %d: %s", window["relay/glm-4.6"], readFile(file))
	}
	if think["relay/glm-4.6"] || !think["oa/gpt-5.5"] {
		t.Fatalf("reasoning: %v", think)
	}
	if c := a.Check(); c != "" {
		t.Fatalf("check: %s", c)
	}
	if err := edit.SetJSON(file, edit.KV{Path: "base_url", Value: "http://elsewhere/v1"}); err != nil {
		t.Fatal(err)
	}
	if a.Check() == "" {
		t.Fatal("a rewritten base_url not noticed")
	}

	// the catalog as it is now reaches the file
	writeFile(t, catalog.CachePath(), `{"zai":{"models":{"glm-4.6":{"id":"glm-4.6","name":"GLM-4.6","limit":{"context":131072}}}}}`)
	catalog.Reset()
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(file), `"context_limit": 131072`) || a.Check() != "" {
		t.Fatalf("not synced: %s", readFile(file))
	}

	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("custom provider left behind: %v", err)
	}
	if f.Get() != "" {
		t.Fatalf("model left: %q", f.Get())
	}
}

// Goose's own providers are offered only when this goose is set up with
// them (#987, EZN7L2C3: a Goose with only magpie listed OpenRouter and its
// hundreds of models, which magpie's show/hide list can't hide): one its
// providers map marks configured, one a legacy <name>_configured marker
// names, the one it uses, and one whose key is in the environment.
func TestGooseOffersOnlyItsConfiguredProviders(t *testing.T) {
	home := syncHome(t)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	for _, k := range gooseKeys {
		t.Setenv(k, "")
	}
	os.WriteFile(catalog.CachePath(), []byte(`{"zai":{"models":{"glm-4.6":{"id":"glm-4.6","name":"GLM-4.6"}}},`+
		`"openrouter":{"name":"OpenRouter","models":{"x/a":{"id":"x/a","name":"A"},"x/b":{"id":"x/b","name":"B"}}},`+
		`"anthropic":{"name":"Anthropic","models":{"claude-sonnet-4-5":{"id":"claude-sonnet-4-5","name":"Claude Sonnet 4.5"}}},`+
		`"google":{"name":"Google","models":{"gemini-2.5-pro":{"id":"gemini-2.5-pro","name":"Gemini 2.5 Pro"}}},`+
		`"openai":{"name":"OpenAI","models":{"gpt-5.5":{"id":"gpt-5.5","name":"GPT-5.5"}}}}`), 0o644)
	catalog.Reset()
	a := goose(home, filepath.Join(home, ".config"))
	os.MkdirAll(filepath.Dir(a.Path), 0o755)
	natives := func() map[string]bool {
		got := map[string]bool{}
		for _, o := range a.Field("model").Options(a.Values()) {
			if p, _, ok := strings.Cut(o.Value, "/"); ok && p != gooseProviderID {
				got[p] = true
			}
		}
		return got
	}
	if len(catalog.Provider("openrouter")) == 0 || len(catalog.Provider("anthropic")) == 0 {
		t.Fatal("the test catalog lists no OpenRouter or Anthropic models")
	}

	// the reporter's: magpie alone
	os.WriteFile(a.Path, []byte("active_provider: magpie\nGOOSE_PROVIDER: magpie\nGOOSE_MODEL: zhipu/glm-5.3\nproviders:\n  magpie:\n    enabled: true\n    model: zhipu/glm-5.3\n    configured: true\n"), 0o644)
	if got := natives(); len(got) != 0 {
		t.Fatalf("a goose on magpie alone is offered %v", got)
	}

	os.WriteFile(a.Path, []byte("active_provider: magpie\nproviders:\n  magpie:\n    configured: true\n  anthropic:\n    enabled: true\n    model: claude-sonnet-4-5\n    configured: true\n  openai:\n    enabled: true\n    configured: false\ngoogle_configured: true\n"), 0o644)
	got := natives()
	if !got["anthropic"] || !got["google"] || got["openai"] || got["openrouter"] {
		t.Fatalf("configured anthropic (map) and google (marker), not openai or openrouter; got %v", got)
	}

	t.Setenv("OPENROUTER_API_KEY", "k")
	if got := natives(); !got["openrouter"] {
		t.Fatalf("OpenRouter's key in the environment, not offered: %v", got)
	}
}
