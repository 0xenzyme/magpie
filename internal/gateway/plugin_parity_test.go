package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/provider"
)

// movedFake is the fake plugin signed in as the built-in id it was moved
// onto, its requests going to up.
func movedFake(t *testing.T, id string, up http.Handler) {
	t.Helper()
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("no bun on PATH")
	}
	fresh(t)
	t.Setenv("MAGPIE_BUN", bun)
	t.Setenv("FAKE_ID", id)
	t.Cleanup(plugin.Settle)
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	t.Setenv("FAKE_BASE", srv.URL+"/v1")
	dir := filepath.Dir(provider.Path())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{id: map[string]any{"state": provider.MovePlugin}})
	if err := os.WriteFile(filepath.Join(dir, "migrations.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if !provider.Moved(id) {
		t.Fatalf("%s isn't marked moved", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	abs, _ := filepath.Abs("../plugin/testdata/fake/index.js")
	if _, err := plugin.Add(ctx, abs); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.Providers(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.APIKey(ctx, id, 0, nil, "k1", plugin.NewAccount); err != nil {
		t.Fatal(err)
	}
	if p, err := provider.Find(id); err != nil || !p.IsPlugin() {
		t.Fatalf("Find(%s) = %+v, %v", id, p, err)
	}
}

// Factory moved onto its plugin counts a prompt's tokens as the built-in
// did, by estimate: the vendor isn't asked.
func TestMovedCountTokensEstimated(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	movedFake(t, "factory", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"input_tokens":999}`))
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages/count_tokens", strings.NewReader(`{"model":"factory/fake-claude","messages":[{"role":"user","content":"hello there"}]}`))
	New().Handler().ServeHTTP(rec, req)
	var got struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); rec.Code != 200 || err != nil || got.InputTokens == 0 {
		t.Fatalf("count_tokens: %d %s", rec.Code, rec.Body)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) > 0 || got.InputTokens == 999 {
		t.Fatalf("the vendor was asked to count: %v, %d", asked, got.InputTokens)
	}
}
