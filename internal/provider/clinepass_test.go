package provider

import (
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
	// another provider's list is left as it is
	o, _ := FromPreset("opencode-go")
	if got := o.planModels([]catalog.Model{{ID: "a"}, {ID: "b"}}); len(got) != 2 {
		t.Fatalf("other: %+v", got)
	}
}
