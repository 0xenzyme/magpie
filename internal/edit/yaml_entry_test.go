package edit

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

// A key set or removed at the top of a YAML file goes in or out with the
// lines that belong to the entry before it, never between a block key and its
// children (magpie goose effort off on a config.yaml ending in extensions:).
func TestYAMLTopAfterBlockEntry(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"empty", "", "GOOSE_THINKING_EFFORT: off\n"},
		{"block mapping",
			"# my goose\nGOOSE_PROVIDER: openrouter\nGOOSE_MODEL: 'owner''s/model' # pinned\nextensions:\n  dev:\n    enabled: true\n",
			"# my goose\nGOOSE_PROVIDER: openrouter\nGOOSE_MODEL: 'owner''s/model' # pinned\nextensions:\n  dev:\n    enabled: true\nGOOSE_THINKING_EFFORT: off\n"},
		{"no trailing newline",
			"GOOSE_PROVIDER: openrouter\nextensions:\n  dev:\n    enabled: true",
			"GOOSE_PROVIDER: openrouter\nextensions:\n  dev:\n    enabled: true\nGOOSE_THINKING_EFFORT: off\n"},
		{"blank lines and comments",
			"extensions:\n  dev:\n    enabled: true\n\n# between\n  other:\n    enabled: false\n\n# the end\n",
			"extensions:\n  dev:\n    enabled: true\n\n# between\n  other:\n    enabled: false\nGOOSE_THINKING_EFFORT: off\n\n# the end\n"},
		{"sequence",
			"GOOSE_MODEL: m\nlist:\n- a\n- b\nnested:\n  - c\n",
			"GOOSE_MODEL: m\nlist:\n- a\n- b\nnested:\n  - c\nGOOSE_THINKING_EFFORT: off\n"},
		{"block scalar",
			"GOOSE_MODEL: m\nprompt: |\n  first\n\n  # not a comment\n",
			"GOOSE_MODEL: m\nprompt: |\n  first\n\n  # not a comment\nGOOSE_THINKING_EFFORT: off\n"},
		{"kept block scalar",
			"prompt: |+\n  first\n\n\n",
			"prompt: |+\n  first\n\n\nGOOSE_THINKING_EFFORT: off\n"},
		{"replace a block value",
			"GOOSE_THINKING_EFFORT: >\n  long\n  text\nGOOSE_MODEL: m\n",
			"GOOSE_THINKING_EFFORT: off\nGOOSE_MODEL: m\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(p, []byte(c.in), 0o600); err != nil {
				t.Fatal(err)
			}
			before := map[string]any{}
			if err := yaml.Unmarshal([]byte(c.in), &before); err != nil {
				t.Fatal(err)
			}
			delete(before, "GOOSE_THINKING_EFFORT")
			if err := SetYAMLTop(p, KV{"GOOSE_THINKING_EFFORT", "off"}); err != nil {
				t.Fatal(err)
			}
			got := read(t, p)
			if got != c.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, c.want)
			}
			after := map[string]any{}
			if err := yaml.Unmarshal([]byte(got), &after); err != nil {
				t.Fatalf("result does not parse: %v\n%s", err, got)
			}
			if after["GOOSE_THINKING_EFFORT"] != "off" {
				t.Fatalf("GOOSE_THINKING_EFFORT = %#v", after["GOOSE_THINKING_EFFORT"])
			}
			delete(after, "GOOSE_THINKING_EFFORT")
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("other keys changed:\n%#v\n%#v", before, after)
			}
			if err := DelYAMLTop(p, "GOOSE_THINKING_EFFORT"); err != nil {
				t.Fatal(err)
			}
			again := map[string]any{}
			if err := yaml.Unmarshal([]byte(read(t, p)), &again); err != nil {
				t.Fatalf("after delete does not parse: %v\n%s", err, read(t, p))
			}
			if !reflect.DeepEqual(before, again) {
				t.Fatalf("after delete:\n%#v\n%#v", before, again)
			}
		})
	}
}
