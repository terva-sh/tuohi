// Command managednotify demonstrates notification activation at the desktop
// adapter edge. Routing stays in Go; the OS receives no account/room key.
// macOS needs this executable packaged inside a real .app (see notify/README).
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"log"
	"sync"

	"github.com/terva-sh/tuohi"
	"github.com/terva-sh/tuohi/notify"
)

//go:embed index.html
var page embed.FS

func main() {
	request := flag.Bool("request-permission", false, "explicitly ask the OS for notification permission")
	flag.Parse()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := &tuohi.App{Name: "tuohi managed notifications", FS: page, Exit: true}
	view := &tuohi.View{Frame: true, Width: 640, Height: 360, URL: "app://app/index.html"}
	// A real adapter maps random opaque IDs to account/room data in Go.
	// Never encode that data or a credential in a notification payload.
	const route = "demo-opaque-route"
	client, err := notify.NewClient(ctx, notify.Config{Name: app.Name, OnActivate: func(id string) {
		if id == route {
			if err := app.Show(view); err != nil {
				log.Print(err)
			}
		}
	}})
	if err != nil {
		if errors.Is(err, notify.ErrUnavailable) || errors.Is(err, notify.ErrUnsupported) {
			log.Print(err)
			return
		}
		log.Fatal(err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Print(err)
		}
	}()
	var mu sync.Mutex
	var handle *notify.Handle
	view.Bind = map[string]any{"roomRead": func() error {
		mu.Lock()
		h := handle
		mu.Unlock()
		if h != nil {
			return h.Dismiss(ctx)
		}
		return nil
	}}
	if err := app.Show(view); err != nil {
		log.Print(err)
		return
	}
	go func() {
		var status notify.PermissionStatus
		var err error
		if *request {
			status, err = client.RequestPermission(ctx)
		} else {
			status, err = client.Permission(ctx)
		}
		if err != nil {
			log.Print(err)
			return
		}
		if status != notify.PermissionAuthorized && status != notify.PermissionProvisional && status != notify.PermissionNotRequired {
			log.Print("Notification permission is not granted; choose it explicitly in settings or with -request-permission.")
			return
		}
		h, err := client.Post(ctx, notify.Notification{ID: route, Title: "New activity"})
		if err != nil {
			log.Print(err)
			return
		}
		mu.Lock()
		handle = h
		mu.Unlock()
		// An unread counter can update an existing notification without
		// changing the adapter's correlation handle.
		if err := h.Replace(ctx, notify.Notification{Title: "New activity available"}); err != nil {
			log.Print(err)
		}
	}()
	if err := app.Wait(); err != nil {
		log.Print(err)
	}
}
