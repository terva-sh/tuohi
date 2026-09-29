package tuohi

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// titleEngine records the titles applyTitle sets. Only core and setTitle are
// implemented; applyTitle calls nothing else.
type titleEngine struct {
	engine
	c   viewCore
	set []string
}

func (e *titleEngine) core() *viewCore       { return &e.c }
func (e *titleEngine) setTitle(title string) { e.set = append(e.set, title) }

// TestApplyTitle checks the order applyTitle takes the window title's sources
// in: the Go title, then the trusted page's title, then App.Name, and that a
// window the host created takes only a Go title and is never blanked
// (TKT-01M3MV9PMXS3CMR71R186MW9BJ).
func TestApplyTitle(t *testing.T) {
	type step struct {
		name       string
		goTitle    *string
		page       *string
		wantWindow []string // titles set by this step
	}
	s := func(v string) *string { return &v }
	cases := []struct {
		name  string
		def   string
		host  bool
		steps []step
	}{
		{"owned, App.Name, then page, then Go", "Canvas", false, []step{
			{"show", nil, nil, []string{"Canvas"}},
			{"document start", nil, s(""), nil},
			{"page titled", nil, s("store-a"), []string{"store-a"}},
			{"same page title", nil, s("store-a"), nil},
			{"Go title", s("Mine"), nil, []string{"Mine"}},
			{"page changes under Go", nil, s("store-b"), nil},
			{"Go title cleared", s(""), nil, []string{"store-b"}},
			{"page untitled", nil, s(""), []string{"Canvas"}},
		}},
		{"owned, no App.Name", "", false, []step{
			{"show", nil, nil, nil},
			{"page titled", nil, s("store-a"), []string{"store-a"}},
			{"page untitled", nil, s(""), []string{""}},
		}},
		{"owned, Title at Show", "Canvas", false, []step{
			{"show", s("Fixed"), nil, []string{"Fixed"}},
			{"page titled", nil, s("store-a"), nil},
		}},
		{"host window", "Canvas", true, []step{
			{"show", nil, nil, nil},
			{"page titled", nil, s("store-a"), nil},
			{"Go title", s("Mine"), nil, []string{"Mine"}},
			{"Go title cleared", s(""), nil, nil},
		}},
	}
	for _, tc := range cases {
		e := &titleEngine{}
		e.c.titleDefault, e.c.hostWindow = tc.def, tc.host
		for _, st := range tc.steps {
			e.set = nil
			if st.goTitle != nil {
				e.c.titleGo = *st.goTitle
			}
			if st.page != nil {
				e.c.titlePage = *st.page
			}
			applyTitle(e)
			if strings.Join(e.set, "|") != strings.Join(st.wantWindow, "|") || len(e.set) != len(st.wantWindow) {
				t.Errorf("%s, %s: set %q, want %q", tc.name, st.name, e.set, st.wantWindow)
			}
		}
	}
}

var resTitle atomic.Value // string

// titleScenario shows a view on a trusted page and reads the native window
// title back after each change: App.Name before the page has one, the page's
// document.title, a Go title winning over the page, SetTitle("") handing the
// window back, and an untrusted about:blank page that cannot rename it.
func titleScenario() string {
	trusted, closeTrusted, err := servePlain(func(target string) (string, string) {
		return "", `<!DOCTYPE html><html><head><title>First</title></head><body>
<script>window.addEventListener('load', function(){ window.loaded(location.href); });</script>
</body></html>`
	})
	if err != nil {
		return "listen error: " + err.Error()
	}
	defer closeTrusted()

	app := testApp()
	app.Name = "Tuohi Title Test"
	w := &View{}
	if err := app.Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()
	loaded := make(chan string, 4)
	_ = w.w.Bind("loaded", func(href string) { loaded <- href })

	// Before any page, the window has App.Name.
	shown := "ok"
	if got := windowTitle(w.w); got != app.Name {
		shown = got
	}

	// read returns the native title once it equals want, or what it was
	// after two seconds: the page's report and SetTitle are both queued.
	read := func(want string) string {
		var got string
		for deadline := time.Now().Add(2 * time.Second); ; {
			if err := ui.call(func() { got = windowTitle(w.w) }); err != nil {
				return "error " + err.Error()
			}
			if got == want || time.Now().After(deadline) {
				return got
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	steps := []struct {
		name string
		do   func()
		want string
	}{
		{"page", nil, "First"},
		{"pagechange", func() { w.Eval(`document.title = 'Second'`) }, "Second"},
		{"go", func() { w.SetTitle("From Go") }, "From Go"},
		{"pageundergo", func() { w.Eval(`document.title = 'Third'`) }, "From Go"},
		{"cleared", func() { w.SetTitle("") }, "Third"},
		{"untitled", func() { w.Eval(`document.querySelector('title').remove()`) }, "Tuohi Title Test"},
		{"retitled", func() { w.Eval(`document.title = 'Fourth'`) }, "Fourth"},
		// about:blank is shown but never trusted: it gets no bridge, so it
		// cannot report a title, and the window keeps the last one.
		{"blank", func() { w.Eval(`location.href = 'about:blank'`) }, "Fourth"},
	}

	result := make(chan string, 1)
	time.AfterFunc(60*time.Second, func() { w.Close() })
	go func() {
		defer w.Close()
		report := []string{"show=" + shown}
		select {
		case <-loaded:
		case <-time.After(10 * time.Second):
			result <- "trusted page never loaded"
			return
		}
		for _, st := range steps {
			if st.do != nil {
				st.do()
			}
			if st.name == "blank" {
				if at := leftForBlank(w, loaded); at != "" {
					report = append(report, st.name+"="+at)
					continue
				}
			}
			if st.name == "pageundergo" || st.name == "blank" {
				// The title must not change, so give a wrong one time to
				// arrive.
				time.Sleep(500 * time.Millisecond)
			}
			if got := read(st.want); got == st.want {
				report = append(report, st.name+"=ok")
			} else {
				report = append(report, st.name+"="+got)
			}
		}
		result <- strings.Join(report, " ")
	}()

	w.w.Navigate(trusted + "page")
	w.w.Run()
	select {
	case r := <-result:
		return r
	default:
		return "no report"
	}
}

// leftForBlank waits until the view shows about:blank and the trusted page's
// bridge no longer answers, so the blank step checks an untrusted page rather
// than the trusted one it was leaving. It returns "" once both hold, and what
// it found otherwise.
func leftForBlank(w *View, loaded chan string) string {
	var at string
	for deadline := time.Now().Add(10 * time.Second); ; {
		if err := ui.call(func() { at = pageURL(w.w) }); err != nil {
			return "error " + err.Error()
		}
		if at == "about:blank" {
			break
		}
		if time.Now().After(deadline) {
			return "at " + at
		}
		time.Sleep(50 * time.Millisecond)
	}
	for len(loaded) > 0 {
		<-loaded
	}
	w.Eval(`window.loaded && window.loaded('still trusted')`)
	select {
	case <-loaded:
		return "bridge still answers"
	case <-time.After(time.Second):
		return ""
	}
}

// TestWindowTitle checks, on every engine, that the window takes App.Name, the
// trusted page's title, and a Go title in that order of precedence, and that
// an untrusted page cannot rename it (TKT-01M3MV9PMXS3CMR71R186MW9BJ).
func TestWindowTitle(t *testing.T) {
	got, _ := resTitle.Load().(string)
	requireGUI(t, got)
	want := "show=ok page=ok pagechange=ok go=ok pageundergo=ok cleared=ok untitled=ok retitled=ok blank=ok"
	if got != want {
		t.Fatalf("window title:\n got %s\nwant %s", got, want)
	}
}
