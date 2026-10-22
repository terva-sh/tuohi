// Command tray is a basic tray example: one declarative tray icon with light/dark icon
// variants, tray-level click handlers, and a menu with a submenu, a checkbox
// and a notification item.
package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sync"
	"sync/atomic"
	"time"

	"github.com/terva-sh/tuohi/notify"
	"github.com/terva-sh/tuohi/tray"
)

func main() {
	// Light mode icon: green (visible on light taskbar/menu-bar backgrounds).
	iconLight := generateIcon(22, color.RGBA{R: 0, G: 180, B: 80, A: 255})
	// Dark mode icon: bright cyan (Windows dark theme / taskbar).
	iconDark := generateIcon(22, color.RGBA{R: 0, G: 230, B: 230, A: 255})

	var unread atomic.Bool
	var renderMu sync.Mutex
	var renderMenu func() []tray.Item
	updateUnread := func(value bool) {
		unread.Store(value)
		// Keep native callbacks short and serialize snapshots from Linux's
		// potentially concurrent D-Bus callbacks. Update marshals to native UI.
		go func() {
			renderMu.Lock()
			defer renderMu.Unlock()
			active := unread.Load()
			light, dark := iconLight, iconDark
			tooltip, title := "tuohi tray demo", ""
			if active {
				light = generateIcon(22, color.RGBA{R: 240, G: 120, A: 255})
				dark = light
				tooltip = "New activity"
				title = "1"
			}
			items := renderMenu()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := tray.Update(ctx, tray.Changes{Icon: light, DarkModeIcon: dark, Tooltip: &tooltip, Title: &title, Items: &items}); err != nil {
				fmt.Println("Update:", err)
			}
		}()
	}
	renderMenu = func() []tray.Item {
		active := unread.Load()
		return []tray.Item{
			{Label: "Unread indicator", Checkbox: true, Checked: active, OnClick: func() { updateUnread(!unread.Load()) }},
			{Label: "Mark unread", Disabled: active, OnClick: func() { updateUnread(true) }},
			{Label: "Mark read", Disabled: !active, OnClick: func() { updateUnread(false) }},
			{Label: "Show notification", OnClick: func() {
				if err := notify.Show("tuohi tray demo", "tuohi", "Hello from tray!"); err != nil {
					fmt.Println("Notify:", err)
				}
			}},
			{Separator: true},
			{Label: "Quit", OnClick: tray.Stop},
		}
	}
	cfg := tray.Config{
		Icon: iconLight, DarkModeIcon: iconDark, Tooltip: "tuohi tray demo", Items: renderMenu(),
		OnClick: func() { fmt.Println("Left click") }, OnDoubleClick: func() { fmt.Println("Double click") }, OnRightClick: func() { fmt.Println("Right click") },
		OnAvailability: func(state tray.Availability) {
			current := tray.CurrentAvailability()
			if state.Generation != current.Generation {
				return
			}
			fmt.Printf("Tray registered=%v host=%v error=%v\n", current.Registered, current.HostPresent, current.Err)
			// A window-owning adapter should keep its only window reachable unless
			// Running, Registered, HostPresent are true and Err is nil. Even then,
			// registration cannot prove the user can see the icon.
		},
	}

	fmt.Println("Tray running. Right-click the tray icon for the menu.")
	fmt.Println("Windows: toggle dark/light mode in Settings to see the icon change.")
	fmt.Println("Click Quit to exit.")
	if err := tray.Run(cfg); err != nil {
		fmt.Println("Run error:", err)
	}
}

// generateIcon returns a PNG of a bordered square in the given color.
func generateIcon(size int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if x == 0 || x == size-1 || y == 0 || y == size-1 {
				img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			} else {
				img.SetRGBA(x, y, c)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
