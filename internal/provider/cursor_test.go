package provider

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseCursorModels(t *testing.T) {
	out := "\x1b[2K\x1b[GAvailable models\n\nauto - Auto (default)\ngpt-5.3-codex-high - Codex 5.3 High\nclaude-opus-5-thinking-high - Claude Opus 5 Thinking High (current)\n\nTip: use --model <id>\n"
	ms := parseCursorModels(out)
	want := [][2]string{{"auto", "Auto"}, {"gpt-5.3-codex-high", "Codex 5.3 High"}, {"claude-opus-5-thinking-high", "Claude Opus 5 Thinking High"}}
	if len(ms) != len(want) {
		t.Fatalf("got %d models: %+v", len(ms), ms)
	}
	for i, w := range want {
		if ms[i].ID != w[0] || ms[i].Name != w[1] {
			t.Errorf("model %d = %q %q, want %q %q", i, ms[i].ID, ms[i].Name, w[0], w[1])
		}
	}
}

// Cursor's names say which of its models hold 1M; the rest hold its 200K
func TestCursorContext(t *testing.T) {
	for _, c := range []struct {
		id, name string
		want     int
	}{
		{"claude-opus-5-5-high-fast", "Claude Opus 5.5 1M High Fast", 1_000_000},
		{"gpt-5.6-sol-xhigh", "GPT-5.6 Sol 1M Extra High", 1_000_000},
		{"gpt-5.5-medium-fast", "GPT-5.5 Fast", 200_000},
		{"composer-2.5", "Composer 2.5", 200_000},
		{"auto", "Auto", 200_000},
	} {
		if got := cursorContext(c.id, c.name); got != c.want {
			t.Errorf("%s (%s): %d, want %d", c.id, c.name, got, c.want)
		}
	}
	if ms := withCursorContexts(parseCursorModels("x-1 - X 1M\n")); ms[0].Context != 1_000_000 {
		t.Fatalf("%+v", ms)
	}
}

// The API is told the installed CLI's version: where its executable lives,
// else the newest one installed, else one known to work.
func TestCursorClientVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	exe := CursorExecutable
	defer func() { CursorExecutable = exe }()
	CursorExecutable = func() string { return "" }
	if v := CursorClientVersion(); v != "cli-"+cursorVersionFallback {
		t.Fatal(v)
	}
	versions := filepath.Join(home, ".local", "share", "cursor-agent", "versions")
	for _, v := range []string{"2026.08.01-abc123", "2026.09.01-def456", "notes"} {
		os.MkdirAll(filepath.Join(versions, v), 0o755)
	}
	if v := CursorClientVersion(); v != "cli-2026.09.01-def456" {
		t.Fatal(v)
	}
	bin := filepath.Join(versions, "2026.08.01-abc123", "cursor-agent")
	os.WriteFile(bin, nil, 0o755)
	link := filepath.Join(home, "cursor-agent")
	if os.Symlink(bin, link) == nil {
		CursorExecutable = func() string { return link }
		if v := CursorClientVersion(); v != "cli-2026.08.01-abc123" {
			t.Fatal(v)
		}
	}
}

func TestTokenExpiry(t *testing.T) {
	jwt := func(claims string) string {
		return "e30." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".sig"
	}
	if got := tokenExpiry(jwt(`{"exp":1790000000}`)); !got.Equal(time.Unix(1790000000, 0)) {
		t.Fatal(got)
	}
	for _, tok := range []string{"", "opaque", jwt(`{"sub":"x"}`), "a.!!.c"} {
		if !tokenExpiry(tok).IsZero() {
			t.Fatal(tok)
		}
	}
}
