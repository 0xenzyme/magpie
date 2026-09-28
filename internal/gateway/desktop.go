package gateway

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"regexp"
	"strings"

	"github.com/yetone/magpie/internal/provider"
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
