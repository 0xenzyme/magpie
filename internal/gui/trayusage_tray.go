//go:build !nogui

package gui

import (
	"context"
	"time"

	"github.com/yetone/magpie/internal/settings"
)

// watchTrayUsage keeps the tray's text up to date, and plain "magpie" in
// the tooltip while it is off.
func (h *host) watchTrayUsage() {
	wake := make(chan struct{}, 1)
	onTrayUsage = func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	go func() {
		<-h.ready // the tray is made once the app runs
		shown := ""
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			label, tip := "", "magpie"
			if q, ok := trayUsageCard(ctx); ok {
				if label, tip = trayUsageText(q, time.Now()); tip == "" {
					tip = "magpie"
				}
			}
			cancel()
			if label+"\x00"+tip != shown {
				shown = label + "\x00" + tip
				h.tray.SetLabel(label)
				h.tray.SetTooltip(tip)
			}
			// the first answer can take a while; look again soon after it
			next := trayUsageEvery
			if label == "" && settings.Load().TrayUsage != "" {
				next = 20 * time.Second
			}
			select {
			case <-wake:
			case <-time.After(next):
			}
		}
	}()
}
