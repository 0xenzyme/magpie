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
//
// Qoder CN (qoderclicn, @qodercn-ai/qoderclicn) is the same CLI for the
// China site, its own accounts and settings in $QODERCN_CONFIG_DIR,
// ~/.qoder-cn by default.

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/gateway"
)

func qoder(home string) *Agent {
	return qoderSite(home, qoderGlobal)
}

func qoderCN(home string) *Agent {
	return qoderSite(home, qoderChina)
}

// qoderBuild is what tells Qoder's two builds apart.
type qoderBuild struct {
	id, name, env, dir, bin string
	aliases, procs          []string
}

var (
	qoderGlobal = qoderBuild{id: "qoder", name: "Qoder", env: "QODER_CONFIG_DIR", dir: ".qoder", bin: "qodercli",
		aliases: []string{"qodercli", "qoder-cli"}, procs: []string{`(^|/)qodercli( |$)`, `(^|/)qoder( |$)`}}
	qoderChina = qoderBuild{id: "qoder-cn", name: "Qoder CN", env: "QODERCN_CONFIG_DIR", dir: ".qoder-cn", bin: "qoderclicn",
		aliases: []string{"qoderclicn", "qodercn", "qodercn-cli"}, procs: []string{`(^|/)qoderclicn( |$)`, `(^|/)qodercn( |$)`}}
)

func qoderSite(home string, b qoderBuild) *Agent {
	dir := os.Getenv(b.env)
	if dir == "" {
		dir = filepath.Join(home, b.dir)
	}
	path := filepath.Join(dir, "settings.json")
	key := b.id + ":" + path + ":"
	slot := "providers." + magpieID
	get := func(k string) string { v, _ := edit.GetJSON(path, k); return v }
	onMagpie := func() bool {
		_, ok := cutMagpie(get("model.name"))
		return ok && qoderKeyed(get(slot+".apiKey"), b.id)
	}
	return &Agent{
		ID: b.id, Name: b.name, Icon: "qoder", Aliases: b.aliases,
		Bin: b.bin, Dir: dir, Path: path,
		Sync: func() error {
			ref, ok := cutMagpie(get("model.name"))
			if !ok {
				return nil
			}
			return syncJSON(path, slot, func() any { return qoderProvider(b.id, ref) })
		},
		Notice: func() string {
			if Running(b.procs...) {
				return b.name + " reads its settings as a session starts — open sessions keep the model they have; new ones use this."
			}
			return ""
		},
		Check: func() string {
			if !onMagpie() {
				return ""
			}
			return wiringOff(b.name, path, func(k string) (string, bool) { return edit.GetJSON(path, slot+"."+k) },
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
						edit.KV{Path: slot, Value: qoderProvider(b.id, ref)},
						edit.KV{Path: "model.name", Value: v})
				}
				// out of magpie: its provider goes, and the model the user
				// had comes back when none is asked for
				if onMagpie() || qoderKeyed(get(slot+".apiKey"), b.id) {
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
				return append(ownOptions("", cur["model"]), viaMagpie(b.id, magpieID+"/")...)
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
func qoderProvider(agent, model string) map[string]any {
	var ms []map[string]any
	for _, m := range magpieModels(agent) {
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
		"apiKey": gateway.TokenFor(agent), "model": model, "models": ms}
}

// qoderKeyed: key is one magpie gives Qoder's provider — its own,
// gateway.TokenFor, since Qoder's requests carry Bun's User-Agent and
// nothing of Qoder's, or gateway.Token, which it was given before; a sync
// gives it the new one.
func qoderKeyed(key, agent string) bool {
	return key == gateway.TokenFor(agent) || key == gateway.Token
}
