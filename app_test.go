package tuohi

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
	"unsafe"

	"github.com/terva-sh/tuohi/dialog"
	"github.com/terva-sh/tuohi/tray"
)

// TestSnapshotConfigCarriesTray verifies that App.Tray is part of the
// committed settings snapshot: it is a declarative, read-once config that Wait
// applies around its run loop (tray.Set before the loop, Remove after). Only
// the snapshot is asserted here - showing the icon needs a live tray backend
// (status bar / session D-Bus), which unit tests must not touch.
func TestSnapshotConfigCarriesTray(t *testing.T) {
	cfg := &tray.Config{Tooltip: "snapshot-test"}
	got := snapshotConfig(&App{Name: "snapshot-test", Tray: cfg})
	if got.Tray != cfg {
		t.Fatalf("snapshot Tray = %v, want the committed config pointer", got.Tray)
	}
	if snapshotConfig(&App{}).Tray != nil {
		t.Fatal("snapshot Tray should be nil when App.Tray is unset")
	}
}

// Shared fixtures for the tests below.
const (
	testIndex   = "index.html"
	testCSSBody = "body{}"
	testHTML    = "<h1>hi</h1>"
)

// TestSnapshotConfigCarriesExit verifies that App.Exit is committed into the
// settings snapshot: Exit ends the process when the last window closes (the
// default keeps it alive until App.Quit).
func TestSnapshotConfigCarriesExit(t *testing.T) {
	if !snapshotConfig(&App{Exit: true}).Exit {
		t.Fatal("snapshot Exit = false, want the committed true")
	}
	if snapshotConfig(&App{}).Exit {
		t.Fatal("snapshot Exit should default to false")
	}
}

// eventsFakeWV is a View that records the Init/Bind/Eval the events bridge
// performs and runs Dispatch synchronously, so the whole Go side is testable
// without a real window. The embedded stub supplies the rest of the interface.
type eventsFakeWV struct {
	*bindMethodsWebViewStub

	mu     sync.Mutex
	initJS []string
	evalJS []string
	bound  map[string]any
	ev     *events // installed by wireEvents
}

func newEventsFakeWV() *eventsFakeWV {
	return &eventsFakeWV{
		bindMethodsWebViewStub: &bindMethodsWebViewStub{},
		bound:                  map[string]any{},
	}
}

func (f *eventsFakeWV) Init(js string) {
	f.mu.Lock()
	f.initJS = append(f.initJS, js)
	f.mu.Unlock()
}

func (f *eventsFakeWV) Eval(js string) {
	f.mu.Lock()
	f.evalJS = append(f.evalJS, js)
	f.mu.Unlock()
}

func (f *eventsFakeWV) Dispatch(fn func()) { fn() }

func (f *eventsFakeWV) Bind(name string, vals ...any) error {
	f.mu.Lock()
	f.bound[name] = vals[0]
	f.mu.Unlock()
	return nil
}

func (f *eventsFakeWV) lastEval() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.evalJS) == 0 {
		return ""
	}
	return f.evalJS[len(f.evalJS)-1]
}

// On/Off/Emit delegate to the fake's events bridge (wireEvents installs it),
// mirroring how the real webview routes the View-level methods.
func (f *eventsFakeWV) On(name string, handler func(args ...json.RawMessage)) func() {
	if f.ev == nil {
		return func() {}
	}
	return f.ev.On(name, handler)
}

func (f *eventsFakeWV) Off(name string) {
	if f.ev != nil {
		f.ev.Off(name)
	}
}

func (f *eventsFakeWV) Emit(name string, data ...any) error {
	if f.ev == nil {
		return errors.New("events bridge not installed")
	}
	return f.ev.Emit(name, data...)
}

// wireEvents installs the events bridge on f (like App.Show does on every
// real view) and returns its state. An empty global exercises the default
// name ("events").
func wireEvents(t *testing.T, f *eventsFakeWV) *events {
	t.Helper()
	e, err := installEvents(f, "")
	if err != nil {
		t.Fatalf("install events: %v", err)
	}
	f.ev = e
	return e
}

func TestInstallEventsWiresBridge(t *testing.T) {
	f := newEventsFakeWV()
	wireEvents(t, f)
	if len(f.initJS) != 1 || !strings.Contains(f.initJS[0], "window.events") {
		t.Fatalf("events JS not injected via Init: %q", f.initJS)
	}
	_, ok := f.bound[eventsBindName]
	if !ok {
		t.Fatalf("bridge %q not bound; bound names: %v", eventsBindName, keysOf(f.bound))
	}
}

func TestEventsAPICustomGlobal(t *testing.T) {
	// App.Events names the page global the events API is installed at:
	// window.<name> directly (no nested .events member), and Emit reaches
	// JS listeners through that same global.
	f := newEventsFakeWV()
	e, err := installEvents(f, "acme")
	if err != nil {
		t.Fatalf("install events: %v", err)
	}
	f.ev = e
	if len(f.initJS) != 1 {
		t.Fatalf("init scripts = %d, want 1", len(f.initJS))
	}
	js := f.initJS[0]
	if !strings.Contains(js, "window.acme") {
		t.Fatalf("events JS does not install window.acme: %q", js)
	}
	if strings.Contains(js, ".events") {
		t.Fatalf("events JS still nests a .events member: %q", js)
	}
	if err := f.Emit("ping", "pong"); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	ev := f.lastEval()
	if !strings.Contains(ev, "window.acme;if(g&&g._dispatch){g._dispatch(\"ping\"") {
		t.Fatalf("Eval does not reach the acme global's _dispatch: %q", ev)
	}
}

func TestEmitFiresGoHandlerAndEvalsJS(t *testing.T) {
	f := newEventsFakeWV()
	wireEvents(t, f)

	var got []json.RawMessage
	f.On("greet", func(args ...json.RawMessage) { got = args })

	err := f.Emit("greet", map[string]any{"name": "crg"}, 42)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	// Go handler ran with both arguments as raw JSON.
	if len(got) != 2 {
		t.Fatalf("handler received %d args, want 2", len(got))
	}
	var payload struct {
		Name string `json:"name"`
	}
	err = json.Unmarshal(got[0], &payload)
	if err != nil || payload.Name != "crg" {
		t.Fatalf("arg 0 = %s (err %v), want {name:crg}", got[0], err)
	}
	if string(got[1]) != "42" {
		t.Fatalf("arg 1 = %s, want 42", got[1])
	}

	// JS listeners were notified via an Eval carrying the encoded payload.
	js := f.lastEval()
	if !strings.Contains(js, `_dispatch("greet"`) {
		t.Fatalf("Eval did not dispatch the event: %q", js)
	}
	if !strings.Contains(js, `"name":"crg"`) || !strings.Contains(js, "42") {
		t.Fatalf("Eval missing the payload: %q", js)
	}
}

func TestReceiveFromJSDispatchesToGo(t *testing.T) {
	f := newEventsFakeWV()
	wireEvents(t, f)

	var got []json.RawMessage
	f.On("ui:click", func(args ...json.RawMessage) { got = args })

	// Simulate the page calling the bound bridge function (a JS-side emit).
	bridge, ok := f.bound[eventsBindName].(func(string, []json.RawMessage))
	if !ok {
		t.Fatalf("bound bridge has unexpected type %T", f.bound[eventsBindName])
	}
	bridge("ui:click", []json.RawMessage{json.RawMessage(`"save"`)})

	if len(got) != 1 || string(got[0]) != `"save"` {
		t.Fatalf("Go handler received %v, want [\"save\"]", got)
	}
}

func TestOnCancelStopsHandler(t *testing.T) {
	f := newEventsFakeWV()
	wireEvents(t, f)

	n := 0
	cancel := f.On("tick", func(args ...json.RawMessage) { n++ })

	_ = f.Emit("tick")
	cancel()
	_ = f.Emit("tick")

	if n != 1 {
		t.Fatalf("handler fired %d times, want 1 (cancelled after first emit)", n)
	}
}

func TestOffRemovesAllHandlers(t *testing.T) {
	f := newEventsFakeWV()
	wireEvents(t, f)

	n := 0
	f.On("x", func(args ...json.RawMessage) { n++ })
	f.On("x", func(args ...json.RawMessage) { n++ })

	_ = f.Emit("x")
	if n != 2 {
		t.Fatalf("both handlers should fire: got %d, want 2", n)
	}

	f.Off("x")
	_ = f.Emit("x")
	if n != 2 {
		t.Fatalf("no handler should fire after Off: got %d, want 2", n)
	}
}

func TestEmitRejectsUnencodableData(t *testing.T) {
	f := newEventsFakeWV()
	wireEvents(t, f)

	fired := false
	f.On("bad", func(args ...json.RawMessage) { fired = true })

	err := f.Emit("bad", make(chan int))
	if err == nil {
		t.Fatal("Emit should fail to encode a channel")
	}
	if fired {
		t.Fatal("no handler should fire when encoding fails")
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

type bindMethodsWebViewStub struct {
	bound      map[string]any
	failOn     string
	bindCalls  int
	bindOrder  []string
	unbindCall []string
}

func (s *bindMethodsWebViewStub) Run() {}

func (s *bindMethodsWebViewStub) Terminate() {}

func (s *bindMethodsWebViewStub) Dispatch(_ func()) {}

func (s *bindMethodsWebViewStub) Destroy() {}

func (s *bindMethodsWebViewStub) Window() unsafe.Pointer { return nil }

func (s *bindMethodsWebViewStub) Navigate(_ string) {}

func (s *bindMethodsWebViewStub) Init(_ string) {}

func (s *bindMethodsWebViewStub) Eval(_ string) {}

func (s *bindMethodsWebViewStub) Focus() {}

func (s *bindMethodsWebViewStub) Raise() {}

func (s *bindMethodsWebViewStub) Show() {}

func (s *bindMethodsWebViewStub) Hide() {}

func (s *bindMethodsWebViewStub) Maximize() {}

func (s *bindMethodsWebViewStub) Minimize() {}

func (s *bindMethodsWebViewStub) Unminimize() {}

func (s *bindMethodsWebViewStub) Unmaximize() {}

func (s *bindMethodsWebViewStub) Bind(name string, vals ...any) error {
	s.bindCalls++
	s.bindOrder = append(s.bindOrder, name)
	if name == s.failOn {
		return errors.New("bind failure")
	}
	if s.bound == nil {
		s.bound = make(map[string]any)
	}
	s.bound[name] = vals[0]
	return nil
}

func (s *bindMethodsWebViewStub) Unbind(name string) error {
	s.unbindCall = append(s.unbindCall, name)
	delete(s.bound, name)
	return nil
}

func (s *bindMethodsWebViewStub) On(string, func(...json.RawMessage)) func() { return func() {} }

func (s *bindMethodsWebViewStub) Off(string) {}

func (s *bindMethodsWebViewStub) Emit(string, ...any) error { return nil }

func (s *bindMethodsWebViewStub) Dialog(_ dialog.Options) ([]string, error) { return nil, nil }

type bindMethodsService struct{}

func (bindMethodsService) GetUserByID(_ int) int { return 1 }

func (bindMethodsService) Ping() {}

func TestBindFuncAndConstantBindOneName(t *testing.T) {
	// A value binds under its (possibly dotted) key as ONE name: a function
	// value is bound as-is, and a non-function value - here a struct, the
	// former "object binding" case - is bound wholesale as a constant. The
	// struct's methods and fields are NEVER expanded into separate names.
	w := &bindMethodsWebViewStub{}
	fn := func() {}
	names, err := bindEntry(w, "app.someAPI.call", fn)
	if err != nil {
		t.Fatalf("bindEntry(func): %v", err)
	}
	if len(names) != 1 || names[0] != "app.someAPI.call" {
		t.Fatalf("bindEntry(func) names = %v, want [app.someAPI.call]", names)
	}
	if _, ok := w.bound["app.someAPI.call"]; !ok {
		t.Fatalf("missing binding for app.someAPI.call; bound: %v", keysOf(w.bound))
	}

	svc := bindMethodsService{}
	cw := &bindMethodsWebViewStub{}
	names, err = bindEntry(cw, "app.someAPI", svc)
	if err != nil {
		t.Fatalf("bindEntry(constant): %v", err)
	}
	if len(names) != 1 || names[0] != "app.someAPI" {
		t.Fatalf("bindEntry(constant) names = %v, want [app.someAPI] (no method expansion)", names)
	}
	if len(cw.bound) != 1 {
		t.Fatalf("bindEntry(constant) bound %d names, want exactly 1; got %v", len(cw.bound), keysOf(cw.bound))
	}
	if got := cw.bound["app.someAPI"]; got != svc {
		t.Fatal("bindEntry(constant) must record the value under its exact name")
	}
}

func TestBindNilWebView(t *testing.T) {
	_, err := bindEntry(nil, "api", func() {})
	if err == nil {
		t.Fatal("Bind() expected error for nil View")
	}
}

func TestBindNilValue(t *testing.T) {
	w := &bindMethodsWebViewStub{}
	_, err := bindEntry(w, "api", nil)
	if err == nil {
		t.Fatal("Bind() expected error for nil value")
	}
}

func TestBindBindError(t *testing.T) {
	w := &bindMethodsWebViewStub{failOn: "sum"}
	names, err := bindEntry(w, "sum", func(a, b int) int { return a + b })
	if err == nil {
		t.Fatal("Bind() expected bind error")
	}
	if len(names) != 0 {
		t.Fatalf("Bind() names len = %d, want 0 (nothing bound on error)", len(names))
	}
	if len(w.bound) != 0 {
		t.Fatalf("Bind() bound %d names on error, want none", len(w.bound))
	}
}

// ---- Open/Reveal scheme validation ----
// The scheme allow-list is the package's security boundary, so it is tested
// directly. The tests never call Open/Reveal with a launchable target, so
// nothing actually opens a browser or file manager on CI.

func TestValidateSchemeAllows(t *testing.T) {
	allowed := []string{
		"http://example.com",
		"https://example.com/a?b=c#d",
		"HTTPS://EXAMPLE.COM", // scheme is matched case-insensitively
		"mailto:someone@example.com",
	}
	for _, u := range allowed {
		err := validateScheme(u)
		if err != nil {
			t.Errorf("validateScheme(%q) = %v, want nil", u, err)
		}
	}
}

func TestValidateSchemeRejects(t *testing.T) {
	rejected := []string{
		"",                    // no scheme
		"example.com",         // bare host, no scheme
		"/etc/passwd",         // bare path
		"ftp://example.com",   // not in the allow-list
		"javascript:alert(1)", // would run script in some handlers
		"vbscript:msgbox(1)",
		"data:text/html,<h1>x",
		"smb://host/share",
		"file:///tmp/report.pdf",        // launches executables through the OS opener
		"FILE:///C:/Windows/calc.exe",   // scheme matched case-insensitively
		"file://server/share/setup.exe", // a UNC host on Windows
	}
	for _, u := range rejected {
		err := validateScheme(u)
		if !errors.Is(err, ErrScheme) {
			t.Errorf("validateScheme(%q) = %v, want ErrScheme", u, err)
		}
	}
}

// TestOpenRejectsBadScheme checks the public entry point refuses a disallowed
// scheme before it reaches the platform backend (so nothing is launched).
func TestOpenRejectsBadScheme(t *testing.T) {
	app := testApp()
	err := app.Open("javascript:alert(1)")
	if !errors.Is(err, ErrScheme) {
		t.Fatalf("testApp().Open(javascript:) = %v, want ErrScheme", err)
	}
	// The refusal precedes the app scope, so a disallowed URL never initializes
	// the platform backend - this must hold on a box with no GUI stack too.
	if app.scope != nil {
		t.Fatal("Open opened the app scope for a disallowed scheme")
	}
}

// testApp returns an empty App for tests that exercise App methods.
func testApp() *App { return &App{} }

// fsys builds an in-memory filesystem for tests from name->content pairs.
func fsys(files map[string]string) fs.FS {
	m := fstest.MapFS{}
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

// TestServeAppFSNil verifies the content-resolution wiring: without App.FS
// there is no resolver, so nothing gets registered or served; the per-view
// serve field is simply nil.
func TestServeAppFSNil(t *testing.T) {
	if got := serveAppFS(nil); got != nil {
		t.Fatalf("serveAppFS(nil) = %v, want nil", got)
	}
	if got := serveAppFS(fsys(map[string]string{testIndex: "x"})); got == nil {
		t.Fatal("serveAppFS with content = nil, want a resolver")
	}
}

// TestServeAppFSMapping exercises the filesystem resolver directly: a request
// path reads a file with the right MIME type, an empty path serves the root
// index.html, unknown paths and ".." traversal attempts answer nil (not
// found) - fs.ReadFile rejects any path that escapes the filesystem.
func TestServeAppFSMapping(t *testing.T) {
	serve := serveAppFS(fsys(map[string]string{
		testIndex: testHTML,
		"app.css": testCSSBody,
	}))
	if resp := serve(&request{URL: "app://app/app.css"}); resp == nil || string(resp.Body) != testCSSBody || resp.MIME != "text/css; charset=utf-8" {
		t.Fatalf("app.css served as %+v, want body{} css", resp)
	}
	if resp := serve(&request{URL: "http://localhost:4123/"}); resp == nil || string(resp.Body) != testHTML {
		t.Fatalf("root served as %+v, want the index.html fallback", resp)
	}
	if resp := serve(&request{URL: "app://app/missing.txt"}); resp != nil {
		t.Fatalf("missing file answered %+v, want nil", resp)
	}
	for _, evil := range []string{"app://app/../secret", "app://app/a/../../secret", "/etc/passwd"} {
		if resp := serve(&request{URL: evil}); resp != nil {
			t.Fatalf("traversal %q answered %+v, want nil", evil, resp)
		}
	}
}

// TestEmitWithoutBridge verifies the View-level events methods degrade safely
// on a view that never had the bridge installed: Emit reports it, On returns
// a no-op cancel and Off does nothing.
func TestEmitWithoutBridge(t *testing.T) {
	w := &webview{}
	if err := w.Emit("x"); err == nil {
		t.Fatal("Emit on a view without the events bridge should error")
	}
	if cancel := w.On("x", func(args ...json.RawMessage) {}); cancel == nil {
		t.Fatal("On without the bridge should still return a cancel func")
	}
	w.Off("x") // must not panic
}

func TestApplyBindsDeterministicOrderAndOverride(t *testing.T) {
	// Bind maps are applied deterministically: app entries first, then view
	// entries, each in alphabetical key order; a non-nil view entry overrides
	// the same app name, a nil view entry unbinds it, and a nil app entry
	// binds nothing. Map insertion order must never matter.
	appBinds := map[string]any{
		"zeta":  func() {}, // inserted last alphabetically on purpose
		"alpha": func() {},
		"ghost": nil, // nil app entry: binds nothing
		"mid":   func() {},
	}
	viewBinds := map[string]any{
		"beta":  func() {},
		"mid":   nil,       // unbind the app-wide "mid"
		"alpha": func() {}, // override the app-wide "alpha"
	}
	s := &bindMethodsWebViewStub{}
	if err := applyBinds(s, appBinds, viewBinds); err != nil {
		t.Fatalf("applyBinds: %v", err)
	}
	wantOrder := []string{"alpha", "mid", "zeta", "alpha", "beta"}
	if len(s.bindOrder) != len(wantOrder) {
		t.Fatalf("bind order = %v, want %v", s.bindOrder, wantOrder)
	}
	for i, name := range wantOrder {
		if s.bindOrder[i] != name {
			t.Fatalf("bind order = %v, want %v", s.bindOrder, wantOrder)
		}
	}
	if len(s.unbindCall) != 1 || s.unbindCall[0] != "mid" {
		t.Fatalf("unbind calls = %v, want [mid]", s.unbindCall)
	}
	wantBound := []string{"alpha", "beta", "zeta"}
	got := keysOf(s.bound)
	if len(got) != len(wantBound) {
		t.Fatalf("bound names = %v, want %v", got, wantBound)
	}
	for _, n := range wantBound {
		if _, ok := s.bound[n]; !ok {
			t.Errorf("name %q should be bound; bound: %v", n, got)
		}
	}
	if _, ok := s.bound["mid"]; ok {
		t.Error(`"mid" must be unbound by the nil view entry`)
	}
	if _, ok := s.bound["ghost"]; ok {
		t.Error(`"ghost" (nil app entry) must not be bound`)
	}
}

func TestApplyBindsNilViewEntryWithoutAppBindingIsNoop(t *testing.T) {
	// Unbinding a name the app never bound is a silent no-op, not an error.
	s := &bindMethodsWebViewStub{}
	if err := applyBinds(s, map[string]any{"a": func() {}}, map[string]any{"missing": nil}); err != nil {
		t.Fatalf("applyBinds: %v", err)
	}
	if len(s.unbindCall) != 0 {
		t.Fatalf("unbind calls = %v, want none", s.unbindCall)
	}
}

func TestSortedMapKeys(t *testing.T) {
	m := map[string]int{"b": 2, "a": 1, "c": 3}
	if got, want := sortedMapKeys(m), []string{"a", "b", "c"}; len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("sortedMapKeys = %v, want %v", got, want)
	}
}

// --- per-view loopback serving (App.FS / App.HTTP / darwin) ----------------

// loopbackRawGet performs one raw HTTP request against addr and returns the
// status line, the parsed headers and the body.
func loopbackRawGet(t *testing.T, addr, target string) (status string, headers map[string]string, body string) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: localhost\r\n\r\n", target); err != nil {
		t.Fatalf("write request: %v", err)
	}
	br := bufio.NewReader(conn)
	statusLine, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	headers = make(map[string]string)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read headers: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if name, value, ok := strings.Cut(line, ":"); ok {
			headers[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(value)
		}
	}
	// The response carries Content-Length; read exactly that many bytes.
	rest := make([]byte, 0, 256)
	buf := make([]byte, 1024)
	remaining, _ := strconv.Atoi(headers["content-length"])
	for remaining > 0 {
		n, err := br.Read(buf)
		if n > 0 {
			rest = append(rest, buf[:n]...)
			remaining -= n
		}
		if err != nil {
			break
		}
	}
	return strings.TrimRight(statusLine, "\r\n"), headers, string(rest)
}

func mustLoopbackServe(t *testing.T, h serveFunc) *loopbackServer {
	t.Helper()
	srv, _, err := listenLoopbackHTTP(h)
	if err != nil {
		t.Fatalf("listenLoopbackHTTP: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

func TestLoopbackServesBodyAndMIME(t *testing.T) {
	srv := mustLoopbackServe(t, func(r *request) *response {
		if r.Method != "GET" {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if !strings.HasPrefix(r.URL, "http://localhost/") {
			t.Errorf("URL = %q, want an http://localhost origin", r.URL)
		}
		return &response{Body: []byte(testHTML), MIME: "text/html; charset=utf-8"}
	})
	status, headers, body := loopbackRawGet(t, srv.ln.Addr().String(), "/index.html")
	if !strings.HasPrefix(status, "HTTP/1.1 200") {
		t.Fatalf("status = %q, want 200", status)
	}
	if body != testHTML {
		t.Errorf("body = %q", body)
	}
	if ct := headers["content-type"]; ct != "text/html; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}
	// The loopback responses carry the cross-origin-isolation headers: COOP:
	// same-origin + COEP: require-corp make the origin cross-origin isolated
	// (SharedArrayBuffer), and CORP: same-origin keeps COEP from blocking the
	// page's own subresources.
	for name, want := range map[string]string{
		"cross-origin-opener-policy":   "same-origin",
		"cross-origin-embedder-policy": "require-corp",
		"cross-origin-resource-policy": "same-origin",
	} {
		if got := headers[name]; got != want {
			t.Errorf("header %s = %q, want %q", name, got, want)
		}
	}
}

func TestLoopbackServesAppFSAndNotFound(t *testing.T) {
	serve := serveAppFS(fsys(map[string]string{testIndex: "<h1>root</h1>", "app.css": testCSSBody}))
	srv, base, err := listenLoopbackHTTP(serve)
	if err != nil {
		t.Fatalf("listenLoopbackHTTP: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if !strings.HasPrefix(base, "http://localhost:") {
		t.Fatalf("base = %q, want http://localhost:<port>", base)
	}
	addr := srv.ln.Addr().String()
	if status, _, body := loopbackRawGet(t, addr, "/app.css"); !strings.HasPrefix(status, "HTTP/1.1 200") || body != testCSSBody {
		t.Fatalf("GET /app.css = %q %q, want 200 body{}", status, body)
	}
	if status, _, _ := loopbackRawGet(t, addr, "/missing.html"); !strings.HasPrefix(status, "HTTP/1.1 404") {
		t.Fatalf("missing file = %q, want 404", status)
	}
}

func TestLoopbackCloseIdempotent(t *testing.T) {
	srv := mustLoopbackServe(t, func(r *request) *response { return &response{Body: []byte("x")} })
	addr := srv.ln.Addr().String()
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("second Close: %v, want nil", err)
	}
	if conn, err := net.Dial("tcp", addr); err == nil {
		_ = conn.Close()
		t.Error("connection succeeded after Close, want refused")
	}
}

func TestLoopbackIdleTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test; skipped under -short")
	}
	srv, _, err := listenLoopbackHTTP(func(r *request) *response { return nil })
	if err != nil {
		t.Fatalf("listenLoopbackHTTP: %v", err)
	}
	defer func() { _ = srv.Close() }()
	deadline := time.Now().Add(2 * loopbackIdleTimeout)
	for !srv.isClosed() {
		if time.Now().After(deadline) {
			t.Fatalf("server still open after %v idle; idle timeout not firing", 2*loopbackIdleTimeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestRewriteAppURL(t *testing.T) {
	base := "http://localhost:41239"
	for _, tc := range []struct{ in, want string }{
		{"app://app/index.html", base + "/index.html"},
		{"app://app/a/b.css?x=1#frag", base + "/a/b.css?x=1#frag"},
		{"https://example.com/x", "https://example.com/x"},
		{"about:blank", "about:blank"},
		{"", ""},
	} {
		if got := rewriteAppURL(base, tc.in); got != tc.want {
			t.Errorf("rewriteAppURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// Empty base: app:// stays untouched, so the native scheme serves it.
	if got := rewriteAppURL("", "app://app/index.html"); got != "app://app/index.html" {
		t.Errorf("rewriteAppURL with empty base = %q, want the raw app:// URL", got)
	}
}

// setScope installs s as the active app scope for one test and restores the
// previous scope on cleanup (tests run sequentially; the engines' scopePtr is
// a package global).
func setScope(t *testing.T, s *appScope) {
	t.Helper()
	prev := scopePtr.Load()
	scopePtr.Store(s)
	t.Cleanup(func() { scopePtr.Store(prev) })
}

func TestViewContentBaseSchemeMode(t *testing.T) {
	// Without App.HTTP a Windows/Linux view serves app:// through the native
	// scheme - no loopback base, no temporary server.
	setScope(t, &appScope{cfg: appConfig{FS: fsys(map[string]string{testIndex: "x"})}})
	base, transient, err := viewContentBase(&View{}, false)
	if err != nil || base != "" || transient != nil {
		t.Fatalf("viewContentBase(scheme view) = %q, %v, %v; want \"\", nil, nil", base, transient, err)
	}
}

func TestViewContentBaseHTTPAndDarwin(t *testing.T) {
	// App.HTTP opts a Windows/Linux view into the temporary loopback server,
	// and a darwin view gets one even with App.HTTP off (macOS always serves
	// its app content over the loopback origin).
	setScope(t, &appScope{cfg: appConfig{FS: fsys(map[string]string{"ping": "pong"})}})
	base, transient, err := viewContentBase(&View{}, false)
	if err != nil || base != "" || transient != nil {
		t.Fatalf("no-HTTP non-darwin view = %q, %v, %v; want scheme", base, transient, err)
	}
	setScope(t, &appScope{cfg: appConfig{FS: fsys(map[string]string{"ping": "pong"}), HTTP: true}})
	base, transient, err = viewContentBase(&View{}, false)
	if err != nil {
		t.Fatalf("viewContentBase (App.HTTP): %v", err)
	}
	if transient == nil || !strings.HasPrefix(base, "http://localhost:") {
		t.Fatal("App.HTTP view: want a temporary loopback server")
	}
	stopLoopback(transient)
	setScope(t, &appScope{cfg: appConfig{FS: fsys(map[string]string{"ping": "pong"})}})
	base, transient, err = viewContentBase(&View{}, true)
	if err != nil {
		t.Fatalf("viewContentBase (darwin): %v", err)
	}
	if transient == nil || !strings.HasPrefix(base, "http://localhost:") {
		t.Fatal("darwin view: want a temporary loopback server even without App.HTTP")
	}
	stopLoopback(transient)
	setScope(t, &appScope{})
	base, transient, err = viewContentBase(&View{}, true)
	if err != nil || base != "" || transient != nil {
		t.Fatalf("viewContentBase without App.FS = %q, %v, %v; want nothing", base, transient, err)
	}
}

// --- Bind-plan validation (R2 prefix collisions, E4 denylist, R1 reserved
// names) --------------------------------------------------------------

func TestValidateTopLevelRejectsReservedAndDenylisted(t *testing.T) {
	// E4/R1: top-level names that would clobber appkit's page surface or a
	// common window global fail at plan time; deeper segments are the
	// consumer's own namespace and pass.
	bad := []string{
		"close", "open", "name", "fetch", "document", "location", // windowGlobalDenylist
		"__webview__",           // the bridge instance
		"__appkit_event__",      // the events binding
		"__appkitWindowDrag",    // an internal message method
		"events",                // the default events global
		"close.thing", "name.x", // top segment is what counts
	}
	for _, name := range bad {
		if err := validateTopLevel(name, "events"); err == nil {
			t.Errorf("validateTopLevel(%q) = nil, want error", name)
		}
	}
	good := []string{
		"demo.close", "demo.theme", "appApi", "closeWindow", "eventsBus",
	}
	for _, name := range good {
		if err := validateTopLevel(name, "events"); err != nil {
			t.Errorf("validateTopLevel(%q) = %v, want nil", name, err)
		}
	}
	// A renamed events global moves the protected name with it.
	if err := validateTopLevel("bus", "bus"); err == nil {
		t.Error(`validateTopLevel("bus", "bus") must reject a binding on the events global`)
	}
	if err := validateTopLevel("events", "bus"); err != nil {
		t.Errorf(`validateTopLevel("events", "bus") = %v, want nil (no clash after rename)`, err)
	}
}

func TestCheckDottedPrefixes(t *testing.T) {
	// R2: a leaf and a namespace under it cannot both be bound; unrelated
	// names and plain overrides are fine.
	if err := checkDottedPrefixes(map[string]bool{"api": true, "api.id": true}); err == nil {
		t.Fatal("api + api.id must collide")
	}
	if err := checkDottedPrefixes(map[string]bool{"app.x": true, "app.x.y": true, "app.x.y.z": true}); err == nil {
		t.Fatal("nested namespace chain must collide")
	}
	if err := checkDottedPrefixes(map[string]bool{"api": true}); err != nil {
		t.Fatalf("single name: %v", err)
	}
	if err := checkDottedPrefixes(map[string]bool{"a.b": true, "a.c": true, "b": true}); err != nil {
		t.Fatalf("sibling names must not collide: %v", err)
	}
	// "b.a" IS nested under "b" - that pair collides even among "siblings".
	if err := checkDottedPrefixes(map[string]bool{"b": true, "b.a": true}); err == nil {
		t.Fatal("b + b.a must collide")
	}
	// "apix" is not a dotted prefix of "api" - segment boundaries matter.
	if err := checkDottedPrefixes(map[string]bool{"api": true, "apix": true}); err != nil {
		t.Fatalf("apix is a different leaf, not a nested name: %v", err)
	}
}

func TestApplyBindsRejectsInvalidPlans(t *testing.T) {
	// The whole plan (names from both maps, overrides collapsed) is validated
	// BEFORE anything binds: prefix collisions, reserved names and denylisted
	// top-level names fail App.Show with a loud error instead of silently
	// destroying bindings on the page.
	w := &bindMethodsWebViewStub{}
	cases := []struct {
		name      string
		app, view map[string]any
	}{
		{"prefix collision across maps", map[string]any{"api": func() {}}, map[string]any{"api.id": func() {}}},
		{"prefix collision within one map", nil, map[string]any{"demo": func() {}, "demo.theme": func() {}}},
		{"reserved name", nil, map[string]any{"__appkit_event__": func() {}}},
		{"denylisted top level", nil, map[string]any{"name": func() {}}},
		{"events global", nil, map[string]any{"events": func() {}}},
		{"bad segments", nil, map[string]any{"a..b": func() {}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := applyBinds(w, tc.app, tc.view); err == nil {
				t.Fatal("applyBinds must reject the plan")
			}
			if len(w.bindOrder) != 0 {
				t.Fatalf("nothing must be bound on a rejected plan, bound: %v", w.bindOrder)
			}
		})
	}
	// An override with the SAME name is not a collision (the view entry wins).
	if err := applyBinds(w, map[string]any{"api": func() {}}, map[string]any{"api": func() {}}); err != nil {
		t.Fatalf("same-name override must be allowed: %v", err)
	}
}

// FuzzParseRequestLine pins the hand-rolled request-line parser: it must never
// panic, and whenever it accepts a line the result is three non-empty,
// whitespace-free tokens.
func FuzzParseRequestLine(f *testing.F) {
	for _, seed := range []string{
		"GET / HTTP/1.1", "HEAD /a?b=c HTTP/1.0", "", "GET /",
		"GET  /  HTTP/1.1", "GET\t/x\tHTTP/1.1", "A B C D",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		method, target, version, ok := parseRequestLine(raw)
		if !ok {
			return
		}
		if method == "" || target == "" || version == "" {
			t.Fatalf("parseRequestLine(%q) accepted an empty token: %q %q %q", raw, method, target, version)
		}
		if strings.ContainsAny(target, " \t") {
			t.Fatalf("parseRequestLine(%q) accepted a target with whitespace: %q", raw, target)
		}
	})
}
