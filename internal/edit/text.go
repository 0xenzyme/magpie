package edit

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Top-level YAML keys are edited line by line. That keeps every comment and
// every other line byte-for-byte intact, which a round trip through a parser
// would not. TOML's are located by its parser (see toml.go), since a value
// there can run over several lines.

var (
	// tomlTable and tomlKV only read a TOML file its parser refuses; see
	// GetTOMLTop.
	tomlTable = regexp.MustCompile(`^\s*\[`)
	tomlKV    = regexp.MustCompile(`^\s*([A-Za-z0-9_.-]+|"[^"]*")\s*=\s*(.*?)\s*$`)
	yamlKV    = regexp.MustCompile(`^([A-Za-z0-9_.-]+)\s*:\s*(.*?)\s*$`)
	yamlPlain = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
)

// GetYAMLTop reads a top-level scalar key from a YAML file.
func GetYAMLTop(path, key string) (string, bool) {
	raw, err := Read(path)
	if err != nil || raw == nil {
		return "", false
	}
	for _, line := range splitLines(string(raw)) {
		if m := yamlKV.FindStringSubmatch(line); m != nil && m[1] == key {
			return yamlValue(m[2]), true
		}
	}
	return "", false
}

// SetYAMLTop sets top-level scalar keys in a YAML file.
func SetYAMLTop(path string, kvs ...KV) error {
	raw, err := Read(path)
	if err != nil {
		return err
	}
	lines := splitLines(string(raw))
	for _, kv := range kvs {
		v := toString(kv.Value)
		var decoded any
		_, isString := kv.Value.(string)
		if !yamlPlain.MatchString(v) || (isString && (yaml.Unmarshal([]byte(v), &decoded) != nil || decoded != v)) {
			v = strconv.Quote(v)
		}
		lines = setYAMLLine(lines, kv.Path, kv.Path+": "+v)
	}
	return WriteAtomic(path, []byte(joinLines(lines)))
}

// setYAMLLine replaces the top-level entry for key with newLine, or inserts
// newLine after the last top-level entry. An entry is its key line and the
// lines that belong to it (see yamlEntryEnd), so a new key never lands
// between a block such as extensions: and its children.
func setYAMLLine(lines []string, key, newLine string) []string {
	at := 0
	for i := 0; i < len(lines); i++ {
		m := yamlKV.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		end := yamlEntryEnd(lines, i)
		if m[1] == key {
			out := append(lines[:i:i], newLine)
			return append(out, lines[end:]...)
		}
		at = end
		i = end - 1
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:at]...)
	out = append(out, newLine)
	return append(out, lines[at:]...)
}

// yamlKeepScalar matches a block scalar header that keeps trailing blank lines
// (|+, >+, |2+ …); those blank lines are part of its value.
var yamlKeepScalar = regexp.MustCompile(`^[|>](\+[0-9]?|[0-9]\+)\s*(#.*)?$`)

// yamlEntryEnd returns the index just past the top-level entry whose key is on
// line i: its indented lines, a sequence written at column 0 under it, and
// any blank or column-0 comment lines between them. Blank lines and comments
// after its last such line are left to what follows, unless it is a block
// scalar that keeps them.
func yamlEntryEnd(lines []string, i int) int {
	keep := false
	if m := yamlKV.FindStringSubmatch(lines[i]); m != nil {
		keep = yamlKeepScalar.MatchString(m[2])
	}
	end := i + 1
	for j := i + 1; j < len(lines); j++ {
		l := lines[j]
		switch {
		case strings.TrimSpace(l) == "":
			if keep && j < len(lines)-1 {
				end = j + 1
			}
		case l[0] == ' ' || l[0] == '\t' || l == "-" || strings.HasPrefix(l, "- "):
			end = j + 1
		case l[0] == '#':
		default:
			return end
		}
	}
	return end
}

// DelYAMLTop removes top-level scalar keys from a YAML file.
func DelYAMLTop(path string, keys ...string) error {
	raw, err := Read(path)
	if err != nil || raw == nil {
		return err
	}
	drop := map[string]bool{}
	for _, k := range keys {
		drop[k] = true
	}
	var out []string
	lines := splitLines(string(raw))
	for i := 0; i < len(lines); i++ {
		if m := yamlKV.FindStringSubmatch(lines[i]); m != nil && drop[m[1]] {
			i = yamlEntryEnd(lines, i) - 1
			continue
		}
		out = append(out, lines[i])
	}
	return WriteAtomic(path, []byte(joinLines(out)))
}

// setLine replaces the line whose key matches, or inserts newLine after the
// last key line in the header section (before the first line matching stop).
func setLine(lines []string, key, newLine string, stop *regexp.Regexp, keyOf func(string) (string, bool)) []string {
	lastKey := -1
	for i, line := range lines {
		if stop != nil && stop.MatchString(line) {
			break
		}
		k, ok := keyOf(line)
		if !ok {
			continue
		}
		if k == key {
			lines[i] = newLine
			return lines
		}
		lastKey = i
	}
	at := lastKey + 1
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:at]...)
	out = append(out, newLine)
	out = append(out, lines[at:]...)
	return out
}

// unquote strips a leading quoted scalar (and anything after it, such as an
// inline comment); ok is false when v does not start with a quote.
func unquote(v string) (string, bool) {
	if v == "" || (v[0] != '"' && v[0] != '\'') {
		return "", false
	}
	q := v[0]
	for i := 1; i < len(v); i++ {
		if q == '"' && v[i] == '\\' {
			i++
			continue
		}
		if v[i] == q {
			if q == '"' {
				if s, err := strconv.Unquote(v[:i+1]); err == nil {
					return s, true
				}
			}
			return v[1:i], true
		}
	}
	return v[1:], true
}

func tomlValue(v string) string {
	if s, ok := unquote(v); ok {
		return s
	}
	if i := strings.Index(v, "#"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

func yamlValue(v string) string {
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(v), &n); err == nil && len(n.Content) > 0 && n.Content[0].Kind == yaml.ScalarNode {
		return n.Content[0].Value
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	}
	return fmt.Sprint(v)
}

// splitLines splits on '\n' but keeps a trailing newline as an empty final
// element so joinLines can restore the file exactly.
func splitLines(s string) []string {
	if s == "" {
		return []string{""}
	}
	return strings.Split(s, "\n")
}

func joinLines(lines []string) string {
	s := strings.Join(lines, "\n")
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}
