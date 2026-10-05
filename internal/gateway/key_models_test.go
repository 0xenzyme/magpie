package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/provider"
)

// A gateway key held to some models (#882) sees only those in the model
// lists, is refused any other before a provider is asked — by a bare
// name, through a group with one it may not use or as a fallback — while
// another key and magpie's own calls for it go on.
func TestGatewayKeyModelsHoldAKey(t *testing.T) {
	fresh(t)
	plan := &fake{t: t, ctype: "application/json", reply: `{"id":"from-plan","choices":[]}`}
	spare := &fake{t: t, ctype: "application/json", reply: `{"id":"from-spare","choices":[]}`}
	twoProviders(t, plan, spare)
	keys, secrets := newCaller(t, "Held", "Free")
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID, Models: []string{" plan/* ", "PLAN/*"}}); err != nil {
		t.Fatal(err)
	}
	if ks, _ := access.List(); !slices.Equal(ks[len(ks)-2].Models, []string{"plan/*"}) {
		t.Fatalf("stored %+v", ks[len(ks)-2])
	}
	for _, g := range []provider.Group{
		{Name: "Both", Members: []string{"plan/m1", "spare/m2"}, Routing: provider.Ordered},
		{Name: "Solo", Members: []string{"plan/m1"}, Routing: provider.Ordered},
	} {
		if err := provider.SaveGroup(g); err != nil {
			t.Fatal(err)
		}
	}
	h := New().Handler()
	do := func(secret, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	listed := func(secret string) []string {
		var l struct{ Data []struct{ ID string } }
		w := do(secret, "GET", "/v1/models", "")
		if err := json.Unmarshal(w.Body.Bytes(), &l); err != nil {
			t.Fatal(w.Body.String())
		}
		var ids []string
		for _, m := range l.Data {
			ids = append(ids, m.ID)
		}
		return ids
	}
	if ids := listed(secrets[0]); !slices.Contains(ids, "plan/m1") || !slices.Contains(ids, "group/solo") || slices.Contains(ids, "spare/m2") || slices.Contains(ids, "group/both") {
		t.Fatalf("held key lists %v", ids)
	}
	if ids := listed(secrets[1]); !slices.Contains(ids, "spare/m2") || !slices.Contains(ids, "group/both") {
		t.Fatalf("free key lists %v", ids)
	}
	if w := do(secrets[0], "GET", "/v1beta/models", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "m1") || strings.Contains(w.Body.String(), "m2") {
		t.Fatalf("gemini list %d %s", w.Code, w.Body.String())
	}
	if w := do(secrets[0], "GET", "/v1/models/spare/m2", ""); w.Code == 200 {
		t.Fatal("held key found a model it may not use", w.Body.String())
	}

	chat := func(secret, model string) *httptest.ResponseRecorder {
		return do(secret, "POST", "/v1/chat/completions", `{"model":"`+model+`","messages":[{"role":"user","content":"hi"}]}`)
	}
	for _, model := range []string{"spare/m2", "m2", "group/both"} {
		w := chat(secrets[0], model)
		var e struct {
			Error struct{ Message, Type string }
		}
		json.Unmarshal(w.Body.Bytes(), &e)
		if w.Code != 403 || e.Error.Type != "permission_error" || !strings.Contains(e.Error.Message, `"Held"`) || !strings.Contains(e.Error.Message, "plan/*") {
			t.Fatalf("%s: %d %s", model, w.Code, w.Body.String())
		}
		if rec := lastUsage(t); !rec.Rejected || rec.Status != 403 || rec.CallerKeyID != keys[0].ID {
			t.Fatalf("%s recorded as %+v", model, rec)
		}
	}
	if spare.calls != 0 || plan.calls != 0 {
		t.Fatal("a refused request reached a provider", plan.calls, spare.calls)
	}
	// said in Anthropic's shape on its API
	w := do(secrets[0], "POST", "/v1/messages", `{"model":"spare/m2","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)
	var a struct {
		Type  string
		Error struct{ Type string }
	}
	json.Unmarshal(w.Body.Bytes(), &a)
	if w.Code != 403 || a.Type != "error" || a.Error.Type != "permission_error" {
		t.Fatal("anthropic refusal", w.Code, w.Body.String())
	}
	for _, model := range []string{"plan/m1", "m1", "group/solo"} {
		if w := chat(secrets[0], model); w.Code != 200 || !strings.Contains(w.Body.String(), "from-plan") {
			t.Fatalf("%s: %d %s", model, w.Code, w.Body.String())
		}
	}
	if w := chat(secrets[1], "spare/m2"); w.Code != 200 || !strings.Contains(w.Body.String(), "from-spare") {
		t.Fatal("a free key was held", w.Code, w.Body.String())
	}
	// magpie's own call for the key (a search, a picture described) goes on
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"spare/m2","messages":[{"role":"user","content":"hi"}]}`))
	r = r.WithContext(magpieChose(r.Context()))
	r.Header.Set("Authorization", "Bearer "+secrets[0])
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatal("magpie's own call was held", rec.Code, rec.Body.String())
	}
	// plan out of quota: its fallback is a model the key may not use
	plan.code, plan.reply = 429, `{"error":{"message":"slow down"}}`
	spareCalls := spare.calls
	if w := chat(secrets[0], "plan/m1"); w.Code == 200 || spare.calls != spareCalls {
		t.Fatal("a held key fell back to a model it may not use", w.Code, w.Body.String())
	}
	if w := chat(secrets[1], "plan/m1"); w.Code != 200 || !strings.Contains(w.Body.String(), "from-spare") {
		t.Fatal("a free key's fallback", w.Code, w.Body.String())
	}
	// "all" takes it off
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID}); err != nil {
		t.Fatal(err)
	}
	plan.code, plan.reply = 0, `{"id":"from-plan","choices":[]}`
	if w := chat(secrets[0], "spare/m2"); w.Code != 200 {
		t.Fatal("a key let off is still held", w.Code, w.Body.String())
	}
}

// A provider's id inside the asked model can't pass for another
// provider's: OpenRouter's "anthropic/x" isn't Anthropic's.
func TestGatewayKeyModelsMatchTheServingProvider(t *testing.T) {
	who := access.Identity{KeyName: "k", Models: []string{"anthropic/*"}}
	if modelAllowed(who, provider.Provider{ID: "openrouter"}, "anthropic/claude") {
		t.Fatal("a model id passed for its provider")
	}
	if !modelAllowed(who, provider.Provider{ID: "anthropic"}, "claude") {
		t.Fatal("anthropic's own")
	}
	if !modelAllowed(access.Identity{}, provider.Provider{ID: "x"}, "y") {
		t.Fatal("a free key")
	}
	if membersAllowed(who, nil) || !membersAllowed(access.Identity{}, nil) {
		t.Fatal("an empty group")
	}
}
