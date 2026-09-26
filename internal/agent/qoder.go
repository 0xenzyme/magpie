package agent

// Qoder's CLI (1.1) keeps its settings in $QODER_CONFIG_DIR/settings.json,
// ~/.qoder by default. A provider of one's own is an entry of "providers",
// its models picked as "<provider>/<model>":
//
//	{"providers":{"magpie":{"displayName":"magpie","protocol":"openai",
//	   "baseUrl":"http://127.0.0.1:3425/v1","apiKey":"magpie","model":…,
//	   "models":[{"model":…,"displayName":…,"contextWindow":…,"maxOutputTokens":…,
//	     "capabilities":{"tools":true,"vision":…,"thinking":{"modes":["enabled"],
//	       "supportsEffort":true,"supportedEffortLevels":[…]}}}]}},
//	 "model":{"name":"magpie/<model>","reasoningEffort":…}}
//
// An openai provider is asked at baseUrl + /chat/completions, with
// reasoning_effort when the model says it takes one. Qoder offers custom
// providers only to a signed-in account whose plan has BYOK. Its requests
// say only undici, so they are not told apart from other clients'.

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/gateway"
)

func qoder(home string) *Agent {
	dir := os.Getenv("QODER_CONFIG_DIR")
	if dir == "" {
		dir = filepath.Join(home, ".qoder")
	}
	path := filepath.Join(dir, "settings.json")
	key := "qoder:" + path + ":"
	slot := "providers." + magpieID
	get := func(k string) string { v, _ := edit.GetJSON(path, k); return v }
	onMagpie := func() bool {
		_, ok := cutMagpie(get("model.name"))
		return ok && get(slot+".apiKey") == gateway.Token
	}
	return &Agent{
		ID: "qoder", Name: "Qoder", Icon: "qoder", Aliases: []string{"qodercli", "qoder-cli"},
		Bin: "qodercli", Dir: dir, Path: path,
		Sync: func() error {
			ref, ok := cutMagpie(get("model.name"))
			if !ok {
				return nil
			}
			return syncJSON(path, slot, func() any { return qoderProvider(ref) })
		},
		Notice: func() string {
			if Running(`(^|/)qodercli( |$)`, `(^|/)qoder( |$)`) {
				return "Qoder reads its settings as a session starts — open sessions keep the model they have; new ones use this."
			}
			return ""
		},
		Check: func() string {
			if !onMagpie() {
				return ""
			}
			return wiringOff("Qoder", path, func(k string) (string, bool) { return edit.GetJSON(path, slot+"."+k) },
				"baseUrl", gatewayV1())
		},
		Fields: []Field{{
			Key: "model", Label: "model",
			Get: func() string { return get("model.name") },
			Set: func(v string) error {
				if ref, ok := cutMagpie(v); ok {
					if !onMagpie() {
						stash(map[string]string{key + "model": get("model.name")})
					}
					return edit.SetJSON(path,
						edit.KV{Path: slot, Value: qoderProvider(ref)},
						edit.KV{Path: "model.name", Value: v})
				}
				// out of magpie: its provider goes, and the model the user
				// had comes back when none is asked for
				if onMagpie() || get(slot+".apiKey") == gateway.Token {
					if err := edit.DelJSON(path, slot); err != nil {
						return err
					}
					if v == "" {
						v = unstash(key + "model")
					}
				}
				if v == "" {
					return edit.DelJSON(path, "model.name")
				}
				return edit.SetJSON(path, edit.KV{Path: "model.name", Value: v})
			},
			Options: func(cur map[string]string) []Option {
				return append(ownOptions("", cur["model"]), viaMagpie("qoder", magpieID+"/")...)
			},
		}, {
			// what Qoder asks a model for when it has no effort of its own
			// in model.preferences
			Key: "effort", Label: "effort",
			Get: func() string { return get("model.reasoningEffort") },
			Set: func(v string) error {
				if v == "" {
					return edit.DelJSON(path, "model.reasoningEffort")
				}
				return edit.SetJSON(path, edit.KV{Path: "model.reasoningEffort", Value: v})
			},
			Options: func(map[string]string) []Option {
				return static("low", "medium", "high", "xhigh", "max")
			},
		}},
	}
}

// qoderLevels are the efforts Qoder can ask for.
var qoderLevels = []string{"low", "medium", "high", "xhigh", "max"}

// qoderProvider is magpie's entry in Qoder's providers, every magpie model
// in it, model the one it starts on.
func qoderProvider(model string) map[string]any {
	var ms []map[string]any
	for _, m := range magpieModels("qoder") {
		caps := map[string]any{"tools": true, "vision": m.Images}
		var levels []string
		for _, e := range m.Efforts {
			if slices.Contains(qoderLevels, e) && !slices.Contains(levels, e) {
				levels = append(levels, e)
			}
		}
		if len(levels) > 0 {
			caps["thinking"] = map[string]any{"modes": []string{"enabled"}, "supportsEffort": true,
				"supportedEffortLevels": levels, "requiresBudgetForEnabled": false}
		}
		e := map[string]any{"model": m.ID, "displayName": m.Name, "capabilities": caps}
		if m.Context > 0 {
			e["contextWindow"] = m.Context
		}
		if m.Output > 0 {
			e["maxOutputTokens"] = m.Output
		}
		ms = append(ms, e)
	}
	return map[string]any{"displayName": "magpie", "protocol": "openai", "baseUrl": gatewayV1(),
		"apiKey": gateway.Token, "model": model, "models": ms}
}
