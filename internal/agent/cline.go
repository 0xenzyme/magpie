package agent

// Cline's CLI (3.x) keeps its providers in $CLINE_DIR/data/settings/
// providers.json, ~/.cline by default:
//
//	{"version":1,"lastUsedProvider":"<id>","modes":{},"providers":{"<id>":{
//	  "settings":{"provider":"<id>","apiKey":…,"model":…,"baseUrl":…,
//	    "headers":{…},"reasoning":{"effort":…}},
//	  "updatedAt":…,"tokenSource":"manual"}}}
//
// A session runs the model of lastUsedProvider, asking for its
// reasoning.effort. A provider of one's own, added to models.json, is
// refused when a session starts ("Unknown or disabled provider",
// cline/cline#14180), so magpie takes Cline's built-in openai-compatible
// provider, which reads the models from the gateway's /v1/models, and makes
// it the one in use. What was there, and the provider in use, are stashed
// and put back when magpie steps out. Cline's requests name only the AI SDK,
// so the header says they are Cline's.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/gateway"
)

// clineSlot is the provider magpie takes in Cline.
const clineSlot = "openai-compatible"

func cline(home string) *Agent {
	dir := os.Getenv("CLINE_DIR")
	if dir == "" {
		dir = filepath.Join(home, ".cline")
	}
	path := filepath.Join(dir, "data", "settings", "providers.json")
	key := "cline:" + path + ":"
	slot := "providers." + clineSlot
	get := func(k string) string { v, _ := edit.GetJSON(path, k); return v }
	inUse := func() string { return get("lastUsedProvider") }
	onMagpie := func() bool {
		return inUse() == clineSlot && get(slot+".settings.apiKey") == gateway.Token
	}
	// restore puts back the openai-compatible provider and the provider in
	// use the user had before magpie
	restore := func() error {
		entry, last := unstash(key+"entry"), unstash(key+"lastUsedProvider")
		if entry != "" {
			if err := edit.SetJSON(path, edit.KV{Path: slot, Value: json.RawMessage(entry)}); err != nil {
				return err
			}
		} else if err := edit.DelJSON(path, slot); err != nil {
			return err
		}
		if last != "" {
			return edit.SetJSON(path, edit.KV{Path: "lastUsedProvider", Value: last})
		}
		return edit.DelJSON(path, "lastUsedProvider")
	}
	// own is the provider a model or effort of Cline's own is set on: the
	// one in use, else Cline's default
	own := func() string {
		if p := inUse(); p != "" {
			return p
		}
		return "cline"
	}
	return &Agent{
		ID: "cline", Name: "Cline", Icon: "cline", Aliases: []string{"cline-cli"},
		UA:  []string{"cline"},
		Bin: "cline", Dir: dir, Path: path,
		Sync: func() error {
			if !onMagpie() {
				return nil
			}
			return syncJSON(path, slot+".settings.baseUrl", func() any { return gatewayV1() })
		},
		Notice: func() string {
			if Running(`(^|/)cline( |$)`) {
				return "Cline reads its provider as a session starts — open sessions keep the model they have; new ones use this."
			}
			return ""
		},
		Check: func() string {
			if !onMagpie() {
				return ""
			}
			return wiringOff("Cline", path, func(k string) (string, bool) { return edit.GetJSON(path, slot+".settings."+k) },
				"baseUrl", gatewayV1())
		},
		Fields: []Field{{
			Key: "model", Label: "model",
			Get: func() string {
				p := inUse()
				if p == "" {
					return ""
				}
				v := get("providers." + p + ".settings.model")
				if v != "" && onMagpie() {
					return magpieID + "/" + v
				}
				return v
			},
			Set: func(v string) error {
				if v == "" {
					if onMagpie() {
						return restore()
					}
					if p := inUse(); p != "" {
						return edit.DelJSON(path, "providers."+p+".settings.model")
					}
					return nil
				}
				if ref, ok := cutMagpie(v); ok {
					effort := get("providers." + own() + ".settings.reasoning.effort")
					if !onMagpie() {
						stash(map[string]string{
							key + "entry":            get(slot),
							key + "lastUsedProvider": inUse(),
						})
					}
					return clineWrite(path,
						edit.KV{Path: slot, Value: clineProvider(ref, effort)},
						edit.KV{Path: "lastUsedProvider", Value: clineSlot})
				}
				if onMagpie() {
					if err := restore(); err != nil {
						return err
					}
				}
				p := own()
				return clineWrite(path,
					edit.KV{Path: "providers." + p + ".settings.provider", Value: p},
					edit.KV{Path: "providers." + p + ".settings.model", Value: v})
			},
			Options: func(cur map[string]string) []Option {
				return append(ownOptions("", cur["model"]), viaMagpie("cline", magpieID+"/")...)
			},
		}, {
			// the reasoning.effort of the provider in use, what Cline's
			// --thinking asks for when it is not given
			Key: "effort", Label: "effort",
			Get: func() string {
				p := inUse()
				if p == "" {
					return ""
				}
				return get("providers." + p + ".settings.reasoning.effort")
			},
			Set: func(v string) error {
				k := "providers." + own() + ".settings.reasoning"
				if v == "" {
					return edit.DelJSON(path, k)
				}
				return clineWrite(path, edit.KV{Path: k + ".effort", Value: v})
			},
			Options: func(map[string]string) []Option {
				return static("none", "low", "medium", "high", "xhigh")
			},
		}},
	}
}

// cutMagpie is the model a magpie/<ref> value names, when it is magpie's.
func cutMagpie(v string) (string, bool) {
	ref, ok := strings.CutPrefix(v, magpieID+"/")
	return ref, ok && isMagpie(ref)
}

// clineProvider is magpie's openai-compatible entry, asking model with
// effort (none unset).
func clineProvider(model, effort string) map[string]any {
	s := map[string]any{
		"provider": clineSlot, "apiKey": gateway.Token, "model": model, "baseUrl": gatewayV1(),
		"headers": map[string]string{"User-Agent": "cline"},
	}
	if effort != "" {
		s["reasoning"] = map[string]any{"effort": effort}
	}
	return map[string]any{"settings": s, "updatedAt": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), "tokenSource": "manual"}
}

// clineWrite sets kvs in providers.json, which Cline keeps private.
func clineWrite(path string, kvs ...edit.KV) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte("{\"version\":1,\"modes\":{},\"providers\":{}}\n"), 0o600); err != nil {
			return err
		}
	}
	return edit.SetJSON(path, kvs...)
}
