package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/usage"
)

// Cursor Private Inference (#299) is found by its product.json among the
// apps, beside a regular Cursor of the same app name that isn't it, and its
// row gives the command that starts it on magpie, with its own key; its
// requests are its own in the usage, not Cursor's.
func TestCursorLocal(t *testing.T) {
	root := t.TempDir()
	app := func(dir, nameShort, exe string) {
		t.Helper()
		res := filepath.Join(root, dir, "Contents", "Resources", "app")
		if err := os.MkdirAll(res, 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(res, "product.json"), []byte(`{"nameShort":"`+nameShort+`","applicationName":"cursor","dataFolderName":".cursor"}`), 0o644)
		if exe != "" {
			os.WriteFile(filepath.Join(root, dir, "Contents", "Info.plist"), []byte("<plist><dict>\n\t<key>CFBundleExecutable</key>\n\t<string>"+exe+"</string>\n</dict></plist>"), 0o644)
		}
	}
	app("Cursor.app", "Cursor", "Cursor")
	if got := findCursorLocal([]string{root, filepath.Join(root, "none")}); got != "" {
		t.Fatalf("a regular Cursor found as it: %q", got)
	}
	app("Cursor Private Inference.app", "Cursor Private Inference", "Cursor PI")
	want := filepath.Join(root, "Cursor Private Inference.app", "Contents", "MacOS", "Cursor PI")
	if got := findCursorLocal([]string{root}); got != want {
		t.Fatalf("found %q, want %q", got, want)
	}
	os.Remove(filepath.Join(root, "Cursor Private Inference.app", "Contents", "Info.plist"))
	if got := findCursorLocal([]string{root}); filepath.Base(got) != "Cursor" || !strings.Contains(got, "Private Inference.app") {
		t.Fatalf("without an Info.plist: %q", got)
	}

	// the command: the gateway's /v1 and the agent's key, the app quoted
	cmd := CursorLocalLaunch("/Applications/It's.app/Contents/MacOS/Cursor", "http://127.0.0.1:3425")
	for _, s := range []string{"CURSOR_LOCAL_AGENT_BASE_URL", "http://127.0.0.1:3425/v1", "CURSOR_LOCAL_AGENT_API_KEY", gateway.TokenFor("cursor-local")} {
		if !strings.Contains(cmd, s) {
			t.Errorf("%q lacks %q", cmd, s)
		}
	}
	if runtime.GOOS != "windows" && !strings.HasSuffix(cmd, ` '/Applications/It'\''s.app/Contents/MacOS/Cursor'`) {
		t.Errorf("app not quoted: %q", cmd)
	}

	// the agent: there and launchable when the app is, else not there
	old := cursorLocalApp
	t.Cleanup(func() { cursorLocalApp = old })
	a, err := Find(CursorLocalID)
	if err != nil {
		t.Fatal(err)
	}
	cursorLocalApp = func() string { return want }
	if !a.detect() || !strings.Contains(a.Launch(), want) {
		t.Errorf("installed: detect %v, launch %q", a.detect(), a.Launch())
	}
	cursorLocalApp = func() string { return "" }
	if a.detect() || a.Launch() != "" {
		t.Error("not installed, yet there")
	}

	if got := usage.AgentOf("cursor-local"); got != CursorLocalID {
		t.Errorf("AgentOf(cursor-local) = %q", got)
	}
	if got := usage.AgentOf("Cursor/3.2"); got != "cursor" {
		t.Errorf("AgentOf(Cursor/3.2) = %q", got)
	}
}
