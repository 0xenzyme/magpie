package autostart

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
)

const label = "com.yetone.magpie"

func record() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

func enabled() bool {
	_, err := os.Stat(record())
	return err == nil
}

// a launch agent the system loads at the next login: the app itself, run
// once, not kept alive — quitting magpie quits it until then
func enable(exe string) error {
	p := record()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array><string>%s</string><string>%s</string></array>
	<key>RunAtLoad</key><true/>
	<key>LimitLoadToSessionType</key><string>Aqua</string>
	<key>ProcessType</key><string>Interactive</string>
</dict>
</plist>
`, label, html.EscapeString(exe), Arg)
	return os.WriteFile(p, []byte(body), 0o644)
}

func disable() error {
	if err := os.Remove(record()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
