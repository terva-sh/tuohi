//go:build linux || freebsd || netbsd

package tuohi

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

var resMaximize atomic.Value

// Only the dedicated test recipe sets TUOHI_TEST_WM, on a private Xvfb display.
// Starting a WM on the person's own display would interfere with their desktop.
func maximizeWithWM() string {
	cmd := exec.Command("openbox", "--sm-disable", "--startup", "printf 'tuohi-wm-ready\\n'")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err.Error()
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return "start window manager: " + err.Error()
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	wmReady := make(chan bool, 1)
	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			if s.Text() == "tuohi-wm-ready" {
				wmReady <- true
				return
			}
		}
		wmReady <- false
	}()
	select {
	case ready := <-wmReady:
		if !ready {
			return "window manager exited before startup"
		}
	case <-time.After(5 * time.Second):
		return "window manager startup timed out"
	}
	return maximizeScenario()
}

func maximizeScenario() string {
	hard := time.AfterFunc(25*time.Second, func() {
		stack := make([]byte, 1<<20)
		n := runtime.Stack(stack, true)
		panic(fmt.Sprintf("maximize scenario stalled\n%s", stack[:n]))
	})
	defer hard.Stop()
	ready := make(chan struct{})
	app := &App{Exit: false}
	v := &View{Width: 400, Height: 300, URL: "data:text/html,<p>Maximize</p>", Ready: func() { close(ready) }}
	if err := app.Show(v); err != nil {
		return err.Error()
	}
	defer v.Close()
	w := v.live().(*webview)
	result := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer func() { app.Quit(); close(done) }()
		select {
		case <-ready:
		case <-time.After(10 * time.Second):
			result <- "page not ready"
			return
		}
		for _, want := range []bool{true, false, true, false} {
			// The real app-region bridge message reaches handleInternal; the JS
			// tracker double-click gesture is independently covered in view_test.
			if err := ui.call(func() {
				w.Eval(`window.__webview__.post(JSON.stringify({method:"__tuohiWindowToggleMaximize"}))`)
			}); err != nil {
				result <- err.Error()
				return
			}
			matched := false
			for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
				var maximized bool
				if err := ui.call(func() {
					// Inspect real GDK state using the API's documented values,
					// independently of toggleMaximize's selected mask.
					if gtk4 {
						maximized = gdkToplevelGetState(gtkNativeGetSurface(w.window))&2 != 0
					} else {
						maximized = gdkWindowGetState(gtkWidgetGetWindow(w.window))&4 != 0
					}
				}); err != nil {
					result <- err.Error()
					return
				}
				if maximized == want {
					matched = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !matched {
				result <- fmt.Sprintf("native maximized state did not become %t", want)
				return
			}
		}
		result <- "ok"
	}()
	waitErr := app.Wait()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		return "Quit did not complete"
	}
	if waitErr != nil {
		return waitErr.Error()
	}
	return <-result
}

func TestAppRegionToggleMaximize(t *testing.T) {
	if os.Getenv("TUOHI_TEST_WM") != "1" {
		t.Skip("run just test-maximize on isolated Xvfb with openbox")
	}
	got, _ := resMaximize.Load().(string)
	requireGUI(t, got)
	if got != "ok" {
		t.Fatal(got)
	}
}
