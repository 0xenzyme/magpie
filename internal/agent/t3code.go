package agent

// T3 Code (pingdotgg/t3code, a GUI that drives Claude Code, Codex, Cursor
// and others; KevinXC on Discord) keeps its settings in
// ~/.t3/userdata/settings.json ($T3CODE_HOME/userdata), a sparse JSON file
// its server watches and reloads on an outside edit. Each provider it lists
// is an instance of a driver, and besides the built-in ones (keyed by the
// driver, "claudeAgent", "codex", …) it takes instances of one's own:
//
//	{"providerInstances":{"<id>":{"driver":"claudeAgent","displayName":…,
//	  "enabled":true,"environment":[{"name":…,"value":…,"sensitive":false}],
//	  "config":{"customModels":[{"slug":…,"name":…}]}}}}
//
// A claudeAgent instance runs Claude Code (Anthropic's Agent SDK, the
// user's ~/.claude settings read as Claude Code reads them) with the
// instance's environment added, and lists Claude's models and its custom
// models, each sent as Claude Code's model. So magpie is an instance of its
// own, "magpie": Claude Code pointed at the gateway (ANTHROPIC_BASE_URL and
// the token Claude Code is routed with, so its requests are Claude Code's
// to the gateway, tiers and all), every magpie model one of its custom
// models, a 1M one marked [1m] as magpie marks it for Claude Code. It is
// magpie's alone: the user's own instances, their custom models and T3's
// other keys stay as they are, and off takes only it out. A binary path or
// Claude home the user gave T3's own Claude is carried over, so it runs the
// same Claude Code. T3's Codex lists what Codex's model/list says, which is
// magpie's catalog once Codex is routed through magpie, so it needs nothing.
//
// The settings environment (~/.claude/settings.json's env) is applied over
// the process', so a Claude Code routed elsewhere in its own settings
// takes its requests there; one routed through magpie or not routed at all
// asks the gateway.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/gateway"
)

// t3Instance is the key of magpie's provider instance in T3 Code's
// settings.json.
const t3Instance = "providerInstances." + magpieID

func t3code(home string) *Agent {
	base := os.Getenv("T3CODE_HOME")
	if base == "" {
		base = filepath.Join(home, ".t3")
	} else if rest, ok := strings.CutPrefix(base, "~"); ok {
		base = filepath.Join(home, rest)
	}
	path := filepath.Join(base, "userdata", "settings.json")
	wired := func() bool { _, ok := edit.GetJSON(path, t3Instance); return ok }
	return &Agent{
		ID: "t3code", Name: "T3 Code", Icon: "t3code", Aliases: []string{"t3", "t3-code"},
		Dir: base, Path: path,
		// ~/.t3 is made at its first start; a Mac app never opened yet is
		// found by its bundle. No command: `t3` is a common name.
		detect: func() bool { return isDir(base) || t3App(home) },
		Sync: func() error {
			return syncJSON(path, t3Instance, func() any { return t3InstanceJSON(path, gateway.URL()) })
		},
		Fields: []Field{{
			Key: "provider", Label: "provider",
			Get: func() string {
				if wired() {
					return magpieID
				}
				return ""
			},
			Set: func(v string) error {
				if v == "" {
					return edit.DelJSON(path, t3Instance)
				}
				return edit.SetJSON(path, edit.KV{Path: t3Instance, Value: t3InstanceJSON(path, gateway.URL())})
			},
			Options: func(map[string]string) []Option {
				return []Option{{Value: magpieID, Label: "magpie", Icon: "magpie", Note: "every magpie model as a provider in T3 Code, on Claude Code"}}
			},
		}},
	}
}

// t3App reports whether T3 Code's app is in a Mac's Applications, by the
// names its builds take.
func t3App(home string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	for _, d := range []string{"/Applications", filepath.Join(home, "Applications")} {
		for _, n := range []string{"T3 Code (Alpha).app", "T3 Code.app"} {
			if _, err := os.Stat(filepath.Join(d, n)); err == nil {
				return true
			}
		}
	}
	return false
}

// t3InstanceJSON is magpie's provider instance in T3 Code's settings.json
// at path: a claudeAgent instance named magpie on the gateway at gw, with
// every magpie model as a custom model, and the binary path and Claude
// home of T3's own Claude when the user gave them one.
func t3InstanceJSON(path, gw string) map[string]any {
	mark := claude1MFor("t3code")
	models := []map[string]any{}
	for _, m := range magpieModels("t3code") {
		models = append(models, map[string]any{"slug": mark(m.ID), "name": m.Name})
	}
	config := map[string]any{"customModels": models}
	for _, k := range []string{"binaryPath", "homePath"} {
		// the user's Claude instance, else the setting it was before T3
		// had instances
		v, _ := edit.GetJSON(path, "providerInstances.claudeAgent.config."+k)
		if v == "" {
			v, _ = edit.GetJSON(path, "providers.claudeAgent."+k)
		}
		if v = strings.TrimSpace(v); v != "" {
			config[k] = v
		}
	}
	return map[string]any{
		"driver": "claudeAgent", "displayName": "magpie", "enabled": true,
		"environment": []map[string]any{
			{"name": "ANTHROPIC_BASE_URL", "value": gw, "sensitive": false},
			{"name": "ANTHROPIC_AUTH_TOKEN", "value": gateway.Token, "sensitive": false},
		},
		"config": config,
	}
}
