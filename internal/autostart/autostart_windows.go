package autostart

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

const (
	runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
	name   = "magpie"
)

// record is where the system keeps it, as a file: none, it's the registry's
func record() string { return "" }

func enabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(name)
	return err == nil
}

// the user's own Run value, which Windows starts at sign-in (and which
// Task Manager's Startup apps can switch off)
func enable(exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, `"`+exe+`" `+Arg)
}

func disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
