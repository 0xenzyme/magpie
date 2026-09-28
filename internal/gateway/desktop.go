package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

// Claude Desktop, pointed at a third-party gateway, keeps a model only when
// its id reads as Anthropic's: it says claude, sonnet, opus, haiku, fable,
// mythos or anthropic, and nowhere names another vendor's model. Its check
// (qo in its app.asar, 2.7032) is
//
//	Vxe.test(id) ? false : Ko.test(id) || ["claude",…,"anthropic"].some(w => id.includes(w))
//
// on the lowercased id, with Vxe the list below. It runs on /v1/models rows,
// on Models entries typed in by hand ("model routing must reference an
// Anthropic model") and on the model a session is started with, so
// anthropic/deepseek/deepseek-flash is turned away for its "deepseek".
var (
	desktopDenied = regexp.MustCompile(`ark-code|astron|command-r|deepseek|doubao|gemini|gemma|glm|gpt|grok|hermes|hy3|kimi|lfm|\bling\b|llama|longcat|mimo|minimax|mistral|mixtral|moonshot|nemotron|openai|phi-|qianfan|qwen|tc-code|\bunic\b|yi-|stepfun|step-3|seed-|bytedance|hunyuan|granite|amazon\.nova|nova-|devstral|ministral|ernie|codex|arcee|trinity|abab|phi\d|\bk2\.|\bm2\.|jamba|arctic|solar|mercury|zamba|kat-coder|\bds-|dpsk`)
	desktopTier   = regexp.MustCompile(`^(sonnet|opus|haiku|fable|mythos)(-[\d.]+)?$`)
	desktopWords  = []string{"claude", "sonnet", "opus", "haiku", "fable", "mythos", "anthropic"}
)

// desktopAccepts is Claude Desktop's check of a gateway model id.
func desktopAccepts(id string) bool {
	l := strings.ToLower(id)
	if desktopTier.MatchString(l) {
		return true
	}
	if desktopDenied.MatchString(l) {
		return false
	}
	for _, w := range desktopWords {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// desktopAlias is the prefix of the id a model is listed by to Claude
// Desktop when its own id names no Claude model: anthropic/magpie-<number>,
// the number a hash of magpie's id, so it stays the model's while the
// catalog changes around it. Desktop's list of other vendors' names grows
// from release to release, so no part of such a model's id is shown to it;
// its display_name and description carry the model's name and magpie id.
const desktopAlias = "anthropic/magpie-"

func aliasFor(id string) string {
	h := fnv.New64a()
	h.Write([]byte(id))
	return fmt.Sprintf("%s%010d", desktopAlias, h.Sum64()%1e10)
}

// claudeLooking is a model's id as Claude Desktop is shown it: as it is when
// it already reads as a Claude model's, else its alias (unprefixed serves it
// again).
func claudeLooking(id string) string {
	if desktopAccepts(id) && !strings.HasPrefix(id, desktopAlias) {
		return id
	}
	return aliasFor(id)
}

// desktopModels is /v1/models as Claude Desktop is shown it: every model by
// an id it keeps (claudeLooking), named so the picker tells them apart —
// it shows the name, not the id, and folds rows of one name into one entry.
func desktopModels(entries []provider.Entry) []map[string]any {
	names := map[string]int{}
	for _, e := range entries {
		names[desktopName(e)]++
	}
	data := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		m := modelObject(e)
		name := desktopName(e)
		if names[name] > 1 && name != e.ID {
			name += " (" + e.ID + ")"
		}
		m["display_name"] = name
		if m["id"] = claudeLooking(e.ID); m["id"] != e.ID {
			m["description"] = e.ID + " in magpie"
		}
		data = append(data, m)
	}
	return data
}

func desktopName(e provider.Entry) string {
	if e.Name != "" {
		return e.Name
	}
	return e.ID
}

// aliased is the catalog id an anthropic/magpie-<number> alias stands for.
func aliased(id string) (string, bool) {
	id = strings.TrimSuffix(id, "[1m]")
	if !strings.HasPrefix(id, desktopAlias) {
		return "", false
	}
	for _, e := range provider.Catalog() {
		if aliasFor(e.ID) == id {
			return e.ID, true
		}
	}
	return "", false
}

// isClaudeDesktop is a request from Claude Desktop's own gateway client: its
// Electron session's User-Agent ("Mozilla/5.0 … Claude/2.7032.0 Chrome/…"),
// which says nothing of it before the first slash.
func isClaudeDesktop(r *http.Request) bool {
	ua := r.Header.Get("User-Agent")
	return strings.HasPrefix(ua, "Mozilla/") && strings.Contains(ua, " Claude/")
}

// Claude Desktop sends some requests on a model of its own choosing rather
// than the one its session is on. A session's title (and branch name) is
// asked for by one tool-less request whose model is its "small_fast" pick
// from the gateway's list — the first id with haiku in it, else sonnet,
// else opus (_$n in its app.asar, 2.7032), the session's model only when
// none has one — so {"model":"claude-sonnet-5-thinking","max_tokens":200,
// "system":"You write short session titles. …"} went to a Claude model the
// user never picked. Claude Code in its Code tab asks for its own
// claude-haiku-… by name for small tasks too.
//
// A session's turns carry tools; the model the latest one is for is the one
// the user picked. A small tool-less request (a title's max_tokens is 200,
// a turn's tens of thousands) for a model whose id reads as Claude's, and
// any request for a model magpie doesn't serve, goes to that model instead,
// so a chat the user started on a Claude model of their own stays on it. It is kept on disk, so a title asked for before the first
// turn after a restart goes there too.
var desktopPicked struct {
	sync.Mutex
	model string
	from  string // the file it was read from
}

func desktopPickedPath() string { return filepath.Join(settings.Dir(), "claude-desktop.model") }

// desktopTurn is the model a Claude Desktop request for asked is served by.
func desktopTurn(asked string, body []byte) string {
	if asked == "" {
		return asked
	}
	tools := hasTools(body)
	desktopPicked.Lock()
	defer desktopPicked.Unlock()
	if path := desktopPickedPath(); desktopPicked.from != path {
		b, _ := os.ReadFile(path)
		desktopPicked.model, desktopPicked.from = strings.TrimSpace(string(b)), path
	}
	picked := desktopPicked.model
	if asked == picked {
		return asked
	}
	if unserved(asked) || !tools && small(body) && desktopAccepts(asked) {
		if picked != "" {
			return picked
		}
		return asked
	}
	if tools {
		desktopPicked.model = asked
		if os.MkdirAll(settings.Dir(), 0o755) == nil {
			os.WriteFile(desktopPickedPath(), []byte(asked+"\n"), 0o600)
		}
	}
	return asked
}

// small: the request asks for a short answer (max_tokens at most 4096), as
// Desktop's title does, not a session's turn.
func small(body []byte) bool {
	var v struct {
		MaxTokens int `json:"max_tokens"`
	}
	return json.Unmarshal(body, &v) == nil && v.MaxTokens > 0 && v.MaxTokens <= 4096
}

// hasTools: the request offers the model tools (Anthropic's, Chat's and
// Responses' "tools" alike).
func hasTools(body []byte) bool {
	if !bytes.Contains(body, []byte(`"tools"`)) {
		return false
	}
	var v struct {
		Tools []json.RawMessage `json:"tools"`
	}
	return json.Unmarshal(body, &v) == nil && len(v.Tools) > 0
}
