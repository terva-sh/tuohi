// Command loopback shows an interface a program already serves over HTTP in a
// tuohi window, which is how terva uses tuohi: the program keeps its
// net/http server, points the window at it, and adds bindings for what the
// page needs from Go that HTTP does not give it.
//
// The server listens on 127.0.0.1 and runs in a goroutine, while main opens
// the window and runs the UI loop, as the package documentation's "The main
// thread" section asks. The window trusts the origin it is navigated to, so
// the page served there can call the bindings; a page on any other origin
// cannot. The server itself is reachable by every process on this machine,
// so serve nothing through it that another local user must not see.
//
// Run it from the examples directory:
//
//	go run ./loopback          the window, until it is closed
//	go run ./loopback -check   load the page, check that it reached both
//	                           the server and Go, and exit 0 or 1
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/terva-sh/tuohi"
)

//go:embed page
var page embed.FS

func main() {
	check := flag.Bool("check", false, "load the page, check it reached the server and Go, and exit 0 or 1")
	flag.Parse()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Handler: handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("loopback: serve: %v", err)
		}
	}()

	app := &tuohi.App{Name: "tuohi loopback", Exit: true}

	// The page reports what it found through a binding, which only -check
	// waits for.
	type report struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	reports := make(chan report, 1)

	view := &tuohi.View{
		URL:    "http://" + ln.Addr().String() + "/",
		Frame:  true,
		Width:  640,
		Height: 420,
		Bind: map[string]any{
			// A binding runs on a goroutine of its own, never the UI thread.
			"loopback.hostname": func() (string, error) { return os.Hostname() },
			"loopback.report": func(r report) {
				select {
				case reports <- r:
				default:
				}
				if *check {
					app.Quit()
				}
			},
		},
	}
	if *check {
		view.URL += "#check"
		go func() {
			time.Sleep(30 * time.Second)
			log.Println("loopback: the page did not report within 30s")
			app.Quit()
		}()
	}

	if err := app.Show(view); err != nil {
		log.Fatal(err)
	}
	waitErr := app.Wait()
	_ = srv.Close()
	if waitErr != nil {
		log.Fatal(waitErr)
	}
	if !*check {
		return
	}
	select {
	case r := <-reports:
		log.Printf("loopback: ok=%v %s", r.OK, r.Detail)
		if !r.OK {
			os.Exit(1)
		}
	default:
		log.Println("loopback: no report from the page")
		os.Exit(1)
	}
}

// handler serves the embedded page and one JSON endpoint, standing in for
// whatever the program already serves.
func handler() http.Handler {
	root, err := fs.Sub(page, "page")
	if err != nil {
		panic(err) // the embed directive names the directory
	}
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(root))
	mux.HandleFunc("GET /api/greeting", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"greeting": "Hello from net/http"})
	})
	return mux
}
