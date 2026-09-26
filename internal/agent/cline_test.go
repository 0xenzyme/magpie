package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/provider"
)

func TestCline(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLINE_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".cline", "data", "settings", "providers.json")
	type entry struct {
		Settings map[string]any `json:"settings"`
	}
	type file struct {
		Version   int              `json:"version"`
		LastUsed  string           `json:"lastUsedProvider"`
		Providers map[string]entry `json:"providers"`
	}
	read := func() (file, string) {
		var f file
		b, _ := os.ReadFile(path)
		json.Unmarshal(b, &f)
		return f, string(b)
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte(`{"version":1,"lastUsedProvider":"anthropic","modes":{},"providers":{
  "anthropic":{"settings":{"provider":"anthropic","apiKey":"sk-a","model":"claude-opus-5","reasoning":{"effort":"high"}},"updatedAt":"2026-09-01T00:00:00.000Z","tokenSource":"manual"},
  "openai-compatible":{"settings":{"provider":"openai-compatible","apiKey":"sk-o","model":"mine","baseUrl":"https://x/v1"},"updatedAt":"2026-09-01T00:00:00.000Z","tokenSource":"manual"}}}`), 0o600)

	a := cline(home)
	f, e := a.Field("model"), a.Field("effort")
	if f.Get() != "claude-opus-5" || e.Get() != "high" {
		t.Fatalf("get: %q %q", f.Get(), e.Get())
	}
	if err := f.Set("magpie/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	c, raw := read()
	s := c.Providers["openai-compatible"].Settings
	if c.LastUsed != "openai-compatible" || s["model"] != "deepseek/pro" || s["baseUrl"] != gatewayV1() || s["apiKey"] != gateway.Token ||
		s["headers"].(map[string]any)["User-Agent"] != "cline" || s["reasoning"].(map[string]any)["effort"] != "high" ||
		c.Providers["anthropic"].Settings["apiKey"] != "sk-a" {
		t.Fatalf("magpie:\n%s", raw)
	}
	if f.Get() != "magpie/deepseek/pro" || e.Get() != "high" || a.Check() != "" {
		t.Fatalf("get: %q %q %q", f.Get(), e.Get(), a.Check())
	}
	// another magpie model keeps what was stashed first; the effort is set
	// on magpie's provider
	if err := f.Set("magpie/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	if err := e.Set("low"); err != nil {
		t.Fatal(err)
	}
	c, raw = read()
	if s := c.Providers["openai-compatible"].Settings; s["model"] != "deepseek/flash" || s["reasoning"].(map[string]any)["effort"] != "low" {
		t.Fatalf("flash:\n%s", raw)
	}

	// back to Cline's own: the user's openai-compatible and provider return
	if err := f.Set("claude-sonnet-5"); err != nil {
		t.Fatal(err)
	}
	c, raw = read()
	if c.LastUsed != "anthropic" || c.Providers["anthropic"].Settings["model"] != "claude-sonnet-5" ||
		c.Providers["openai-compatible"].Settings["apiKey"] != "sk-o" || c.Providers["openai-compatible"].Settings["baseUrl"] != "https://x/v1" {
		t.Fatalf("own:\n%s", raw)
	}

	// reset from magpie, with no Cline settings before it: nothing of
	// magpie's is left
	os.Remove(path)
	if f.Get() != "" {
		t.Fatalf("empty get: %q", f.Get())
	}
	if err := f.Set("magpie/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	c, raw = read()
	if c.Version != 1 || c.LastUsed != "openai-compatible" {
		t.Fatalf("new file:\n%s", raw)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	c, raw = read()
	if c.LastUsed != "" || len(c.Providers) != 0 || c.Version != 1 {
		t.Fatalf("reset:\n%s", raw)
	}
}
