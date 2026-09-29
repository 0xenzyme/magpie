package provider

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
)

// devinListed is a CLI list the way Devin gives it: families whose
// variants are the family at an effort, in either spelling, and variants
// that are no effort at all.
var devinListed = []DevinFamily{
	{UID: "swe-2", Label: "SWE-2", Aliases: []string{"swe"}, Models: []catalog.Model{
		{ID: "swe-2-low", Name: "SWE-2 Low", Provider: "devin", Context: 262000, Output: 128000},
		{ID: "swe-2-medium", Name: "SWE-2 Medium", Provider: "devin", Context: 262000, Output: 128000},
		{ID: "swe-2-high", Name: "SWE-2 High", Provider: "devin", Context: 262000, Output: 128000},
		{ID: "swe-2-max", Name: "SWE-2 Max", Provider: "devin", Context: 262000, Output: 128000},
	}},
	{UID: "claude-sonnet-5-5", Label: "Claude Sonnet 5.5", Models: []catalog.Model{
		{ID: "MODEL_CLAUDE_SONNET_5_5_MEDIUM", Name: "Claude Sonnet 5.5 Medium", Provider: "devin", Context: 1000000, Output: 64000},
		{ID: "MODEL_CLAUDE_SONNET_5_5_HIGH", Name: "Claude Sonnet 5.5 High", Provider: "devin", Context: 1000000, Output: 64000},
		{ID: "claude-sonnet-5-5-high-fast", Name: "Claude Sonnet 5.5 High Fast", Provider: "devin"},
	}},
	{UID: "glm-5.2", Label: "GLM-5.2", Models: []catalog.Model{
		{ID: "glm-5-2", Name: "GLM-5.2", Provider: "devin", Context: 200000},
		{ID: "glm-5-2-1m", Name: "GLM-5.2 1M", Provider: "devin", Context: 1000000},
	}},
	{UID: "kimi-k3", Label: "Kimi K3", Models: []catalog.Model{{ID: "kimi-k3-0901", Name: "Kimi K3", Provider: "devin"}}},
}

func devinOffered(ms []catalog.Model) string {
	var out []string
	for _, m := range ms {
		out = append(out, m.ID+"|"+strings.Join(m.Efforts, ","))
	}
	return strings.Join(out, " ")
}

// Devin's list is offered as its families, each with the efforts its
// variants are at, so the effort is picked as any model's is; a variant
// that is no effort (a 1M window, a fast one) stays a model of its own.
func TestDevinModelsOfferFamilies(t *testing.T) {
	got := devinOffered(devinModels(devinListed))
	want := "swe-2|low,medium,high,max claude-sonnet-5-5|medium,high claude-sonnet-5-5-high-fast| glm-5.2| glm-5-2| glm-5-2-1m| kimi-k3|"
	if got != want {
		t.Fatalf("offered\n %s\nwant\n %s", got, want)
	}
	for _, m := range devinModels(devinListed) {
		if m.ID == "swe-2" && (m.Name != "SWE-2" || m.Context != 262000) {
			t.Fatalf("a family keeps its name and window: %+v", m)
		}
	}
}

// A list saved flat, before the families were one model, is collapsed
// the same way once the CLI list is read, but for the variants the user
// picked: those stay, at the one effort each id is at.
func TestDevinCollapseKeepsPickedVariants(t *testing.T) {
	flat := devinModelsFlatten(devinListed)
	got := devinOffered(devinCollapse(flat, devinListed, []string{"swe-2-medium", "MODEL_CLAUDE_SONNET_5_5_MEDIUM"}))
	want := "swe-2|low,medium,high,max swe-2-medium|medium claude-sonnet-5-5|medium,high MODEL_CLAUDE_SONNET_5_5_MEDIUM|medium claude-sonnet-5-5-high-fast| glm-5.2| glm-5-2| glm-5-2-1m| kimi-k3|"
	if got != want {
		t.Fatalf("collapsed\n %s\nwant\n %s", got, want)
	}
	// a list already collapsed, with no CLI list read: a picked variant is
	// added back at its effort, one nobody knows of isn't
	got = devinOffered(devinCollapse(devinModels(devinListed), nil, []string{"swe-2-high", "made-up"}))
	if !strings.HasSuffix(got, " kimi-k3| swe-2-high|high") {
		t.Fatalf("picked back: %s", got)
	}
}

// A family is asked for as its variant at the effort asked for, or the
// nearest it has; a variant's own id goes as it is, whatever the effort.
func TestDevinVariantIn(t *testing.T) {
	for _, c := range []struct{ model, effort, want string }{
		{"swe-2", "", "swe-2-low"},
		{"swe-2", "medium", "swe-2-medium"},
		{"swe", "high", "swe-2-high"},
		{"swe-2", "xhigh", "swe-2-max"}, // a tie goes up
		{"swe-2", "minimal", "swe-2-low"},
		{"claude-sonnet-5-5", "max", "MODEL_CLAUDE_SONNET_5_5_HIGH"},
		{"claude-sonnet-5-5", "low", "MODEL_CLAUDE_SONNET_5_5_MEDIUM"},
		{"glm-5.2", "high", "glm-5-2"},
		// the id the user picked is the effort it runs at (蓝猫 on Discord)
		{"swe-2-medium", "max", "swe-2-medium"},
		{"MODEL_CLAUDE_SONNET_5_5_MEDIUM", "max", "MODEL_CLAUDE_SONNET_5_5_MEDIUM"},
		{"claude-sonnet-5-5-high-fast", "low", "claude-sonnet-5-5-high-fast"},
		{"unknown", "high", "unknown"},
	} {
		if got := devinVariantIn(devinListed, c.model, c.effort); got != c.want {
			t.Errorf("%s at %q: %s, want %s", c.model, c.effort, got, c.want)
		}
	}
}

// Through the provider: a family takes its variants' efforts, a picked
// variant and one an agent was set to (not in the list) take the one
// their id is at, so an effort asked of them is fitted to it.
func TestDevinEfforts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	devinFamiliesCached(devinListed)
	t.Cleanup(func() { devinFamiliesCached(nil) })
	if err := catalog.SaveLive("devin", "", devinModels(devinListed)); err != nil {
		t.Fatal(err)
	}
	p := Provider{ID: "devin", Models: []string{"swe-2", "swe-2-medium"}}
	for model, want := range map[string]string{
		"swe-2": "low,medium,high,max", "claude-sonnet-5-5": "medium,high",
		"swe-2-medium": "medium", "MODEL_CLAUDE_SONNET_5_5_HIGH": "high", "glm-5-2-1m": "",
	} {
		if got := strings.Join(p.Efforts(model), ","); got != want {
			t.Errorf("%s: %q, want %q", model, got, want)
		}
	}
	ex := p.Exposed()
	if len(ex) != 2 || ex[1].ID != "swe-2-medium" || ex[1].Context != 262000 || strings.Join(ex[1].Efforts, ",") != "medium" {
		t.Fatalf("exposed %+v", ex)
	}
}
