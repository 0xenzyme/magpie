package provider

import (
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
)

func TestPlanModels(t *testing.T) {
	p, err := FromPreset("clinepass")
	if err != nil {
		t.Fatal(err)
	}
	// the Cline API's list: its paid models, one of the plan's among them
	got := p.planModels([]catalog.Model{{ID: "anthropic/claude-opus-5-5"}, {ID: "cline-pass/glm-5.3"}})
	if len(got) != 1 || got[0].ID != "cline-pass/glm-5.3" {
		t.Fatalf("kept: %+v", got)
	}
	// none of the plan's listed: the plan's own
	if got := p.planModels([]catalog.Model{{ID: "x/y"}}); len(got) != len(Preset("clinepass").Models) || got[0].ID != "cline-pass/glm-5.3" {
		t.Fatalf("plan's: %+v", got)
	}
	// the Cline API's free models are kept beside the plan's, marked free,
	// while its paid ones are still left out (lml on Discord)
	got = p.planModels([]catalog.Model{{ID: "anthropic/claude-opus-5-5"}, {ID: "qwen/qwen3.8-27b:free"}, {ID: "google/gemma-4-31b-it:free"}})
	if n := len(Preset("clinepass").Models); len(got) != n+2 || got[0].ID != "cline-pass/glm-5.3" || got[n].ID != "qwen/qwen3.8-27b:free" || got[n+1].ID != "google/gemma-4-31b-it:free" {
		t.Fatalf("free: %+v", got)
	}
	for _, m := range got {
		if m.Free != strings.HasSuffix(m.ID, ":free") {
			t.Fatalf("free flag: %+v", m)
		}
	}
	// a free model costs nothing, not its tagless model's price
	if pr, ok := p.ListPrice("qwen/qwen3.8-27b:free"); !ok || pr != (catalog.Price{}) {
		t.Fatalf("free price: %+v %v", pr, ok)
	}
	// another provider's list is left as it is
	o, _ := FromPreset("opencode-go")
	if got := o.planModels([]catalog.Model{{ID: "a"}, {ID: "b"}}); len(got) != 2 {
		t.Fatalf("other: %+v", got)
	}
}
