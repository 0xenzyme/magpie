package gateway

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\n fake")

// easel is a vendor with an images API model and a chat model that answers
// with an image; what each path was sent is kept.
type easel struct {
	mu   sync.Mutex
	sent map[string][]string
	ct   map[string]string
}

func (e *easel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	e.mu.Lock()
	if e.sent == nil {
		e.sent, e.ct = map[string][]string{}, map[string]string{}
	}
	e.sent[r.URL.Path] = append(e.sent[r.URL.Path], string(body))
	e.ct[r.URL.Path] = r.Header.Get("Content-Type")
	e.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer key" {
		w.WriteHeader(401)
		return
	}
	b64 := base64.StdEncoding.EncodeToString(pngBytes)
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/v1/images/generations", "/v1/images/edits":
		io.WriteString(w, `{"created":1,"data":[{"b64_json":"`+b64+`"}],"usage":{"input_tokens":7,"output_tokens":100}}`)
	case "/v1/chat/completions":
		if !strings.Contains(string(body), `"modalities":["image","text"]`) {
			w.WriteHeader(400)
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Here it is.","images":[{"type":"image_url","image_url":{"url":"data:image/png;base64,`+b64+`"}}]}}],"usage":{"prompt_tokens":3,"completion_tokens":50}}`)
	default:
		w.WriteHeader(404)
	}
}

func (e *easel) got(path string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.sent[path]...)
}

func easeled(t *testing.T) (*Server, *easel) {
	t.Helper()
	fresh(t)
	e := &easel{}
	up := httptest.NewServer(e)
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "art", Name: "Art", Chat: up.URL + "/v1", Key: "key", Models: []string{"text", "gpt-image-1", "painter-image-preview"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("art", up.URL+"/v1", []catalog.Model{{ID: "text"}}); err != nil {
		t.Fatal(err)
	}
	return New(), e
}

type imagesAnswer struct {
	Model string `json:"model"`
	Data  []struct {
		B64 string `json:"b64_json"`
	} `json:"data"`
	Text  string `json:"text"`
	Usage struct {
		Input  int `json:"input_tokens"`
		Output int `json:"output_tokens"`
	} `json:"usage"`
}

func postImages(t *testing.T, s *Server, path, ct, body string) (int, imagesAnswer, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", ct)
	s.Handler().ServeHTTP(rec, req)
	var a imagesAnswer
	json.Unmarshal(rec.Body.Bytes(), &a)
	return rec.Code, a, rec.Body.String()
}

func TestImagesAPIModelDraws(t *testing.T) {
	s, e := easeled(t)
	code, a, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"art/gpt-image-1","prompt":"a magpie","size":"1024x1024","n":2}`)
	if code != 200 || len(a.Data) != 1 || a.Usage.Output != 100 {
		t.Fatalf("%d %s", code, raw)
	}
	if b, _ := base64.StdEncoding.DecodeString(a.Data[0].B64); string(b) != string(pngBytes) {
		t.Fatalf("image %q", b)
	}
	sent := e.got("/v1/images/generations")
	if len(sent) != 1 || !strings.Contains(sent[0], `"model":"gpt-image-1"`) || !strings.Contains(sent[0], `"n":2`) || strings.Contains(sent[0], "response_format") {
		t.Fatalf("vendor was sent %v", sent)
	}
}

func TestChatModelDrawsWithModalities(t *testing.T) {
	s, e := easeled(t)
	code, a, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"art/painter-image-preview","prompt":"a magpie","size":"1536x1024","n":2}`)
	if code != 200 || len(a.Data) != 2 || a.Text != "Here it is." || a.Usage.Input != 6 {
		t.Fatalf("%d %s", code, raw)
	}
	sent := e.got("/v1/chat/completions")
	if len(sent) != 2 || !strings.Contains(sent[0], "Aspect ratio: 3:2") {
		t.Fatalf("vendor was sent %v", sent)
	}
}

func TestEditSendsTheImages(t *testing.T) {
	s, e := easeled(t)
	img := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
	code, _, raw := postImages(t, s, "/v1/images/edits", "application/json", `{"model":"art/gpt-image-1","prompt":"make it blue","images":[{"image_url":"`+img+`"},{"image_url":"`+img+`"}]}`)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	e.mu.Lock()
	ct := e.ct["/v1/images/edits"]
	e.mu.Unlock()
	sent := e.got("/v1/images/edits")
	if !strings.HasPrefix(ct, "multipart/form-data") || len(sent) != 1 || strings.Count(sent[0], `name="image[]"`) != 2 || !strings.Contains(sent[0], "make it blue") {
		t.Fatalf("vendor was sent %s %v", ct, sent)
	}
	// the chat model is given them in its message
	code, _, raw = postImages(t, s, "/v1/images/edits", "application/json", `{"model":"art/painter-image-preview","prompt":"make it blue","image":"`+img+`"}`)
	if code != 200 || !strings.Contains(e.got("/v1/chat/completions")[0], img) {
		t.Fatalf("%d %s", code, raw)
	}
	// an edit with no image is turned away
	if code, _, _ := postImages(t, s, "/v1/images/edits", "application/json", `{"model":"art/gpt-image-1","prompt":"x"}`); code != 400 {
		t.Fatalf("edit without image: %d", code)
	}
}

func TestDrawerIsTheSettingOrAutomatic(t *testing.T) {
	s, e := easeled(t)
	// no model named: magpie picks one the provider can draw with
	if m, ok := drawer(); !ok || !strings.HasPrefix(m, "art/") {
		t.Fatalf("drawer = %q %v", m, ok)
	}
	st := settings.Load()
	st.ImageGen = "art/painter-image-preview"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	code, a, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"prompt":"a magpie"}`)
	if code != 200 || a.Model != "art/painter-image-preview" || len(e.got("/v1/chat/completions")) != 1 {
		t.Fatalf("%d %s", code, raw)
	}
	st.ImageGen = "off"
	settings.Save(st)
	if code, _, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"prompt":"a magpie"}`); code != 400 || !strings.Contains(raw, "Image generation") {
		t.Fatalf("off: %d %s", code, raw)
	}
}

func TestVendorFailureIsSaid(t *testing.T) {
	s, _ := easeled(t)
	p, _ := provider.Find("art")
	p.Key = "wrong"
	provider.Save(*p)
	code, _, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"art/gpt-image-1","prompt":"a magpie"}`)
	if code != 401 || !strings.Contains(raw, "401") {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestViaFor(t *testing.T) {
	google := provider.Provider{Chat: "https://generativelanguage.googleapis.com/v1beta/openai"}
	router := provider.Provider{Chat: "https://openrouter.ai/api/v1"}
	openai := provider.Provider{Chat: "https://api.openai.com/v1"}
	for _, tc := range []struct {
		p     provider.Provider
		model string
		want  drawVia
	}{
		{google, "gemini-2.5-flash-image", viaGemini},
		{google, "imagen-4.0-generate-001", viaImages},
		{router, "openai/gpt-5-image", viaChat},
		{router, "google/gemini-2.5-flash-image", viaChat},
		{openai, "gpt-image-1", viaImages},
		{openai, "dall-e-3", viaImages},
		{openai, "gpt-5-image", viaChat},
	} {
		if got := viaFor(tc.p, tc.model); got != tc.want {
			t.Errorf("%s at %s: %s, want %s", tc.model, tc.p.Chat, got, tc.want)
		}
	}
}

func TestAspectOf(t *testing.T) {
	for size, want := range map[string]string{"1024x1024": "1:1", "1536x1024": "3:2", "1024x1536": "2:3", "1792x1024": "16:9", "auto": "", "": "", "4:3": "4:3"} {
		if got := aspectOf(size); got != want {
			t.Errorf("aspectOf(%q) = %q, want %q", size, got, want)
		}
	}
}

func TestCatalogDrawers(t *testing.T) {
	fresh(t)
	for _, id := range catalog.Providers() {
		for _, m := range catalog.Drawers(id) {
			if l := strings.ToLower(m.ID); strings.Contains(l, "deep-research") || strings.HasSuffix(l, "/auto") {
				t.Errorf("%s/%s is a drawer", id, m.ID)
			}
		}
	}
}
