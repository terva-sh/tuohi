package tuohi

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBindEntryFuncBindsDirectly(t *testing.T) {
	w := &bindMethodsWebViewStub{bound: map[string]any{}}
	sum := func(a, b int) int { return a + b }
	names, err := bindEntry(w, "sum", sum)
	if err != nil {
		t.Fatalf("bindEntry: %v", err)
	}
	if len(names) != 1 || names[0] != "sum" {
		t.Fatalf("bindEntry names = %v, want [sum]", names)
	}
	if _, ok := w.bound["sum"]; !ok {
		t.Fatal("bindEntry did not bind the function under its name")
	}
	if len(w.bound) != 1 {
		t.Fatalf("bindEntry bound %d names, want exactly 1 (no method expansion)", len(w.bound))
	}
}

func TestMakeBindingFuncAndConstant(t *testing.T) {
	// A function value becomes a callable binding; any other value - scalar,
	// struct, map or slice - becomes a constant binding carrying the value's
	// JSON. Nothing is derived from the value's type: one value, one name.
	sum := func(a, b int) int { return a + b }
	b, err := makeBinding(sum)
	if err != nil {
		t.Fatalf("makeBinding(func): %v", err)
	}
	if b.kind != bindingFunc || b.fn == nil {
		t.Fatalf("func value: kind = %v, wrapper present = %v; want bindingFunc with a wrapper", b.kind, b.fn != nil)
	}

	cases := []struct {
		name string
		v    any
		want string
	}{
		{"int", 42, "42"},
		{"string", "hi", `"hi"`},
		{"bool", true, "true"},
		{"slice", []int{1, 2}, "[1,2]"},
		{"map", map[string]int{"a": 1}, `{"a":1}`},
		{"struct", struct{ X int }{X: 3}, `{"X":3}`},
	}
	for _, c := range cases {
		b, err := makeBinding(c.v)
		if err != nil {
			t.Fatalf("makeBinding(%s): %v", c.name, err)
		}
		if b.kind != bindingConst || b.fn != nil {
			t.Fatalf("makeBinding(%s): kind = %v, want bindingConst", c.name, b.kind)
		}
		if b.value != c.want {
			t.Fatalf("makeBinding(%s): value = %s, want %s", c.name, b.value, c.want)
		}
	}
}

func TestMakeBindingCallableArity(t *testing.T) {
	// A function's arity decides its variable semantics: zero arguments make
	// a callable getter (function AND readable variable), one argument a
	// callable setter (function AND writable variable), anything else a plain
	// callable.
	cases := []struct {
		name     string
		fn       any
		settable bool
	}{
		{"getter", func() (string, error) { return "", nil }, false},
		{"setter", func(s string) error { return nil }, true},
		{"plain2", func(a, b int) int { return a + b }, false},
		{"variadic", func(xs ...int) int { return 0 }, false},
	}
	for _, c := range cases {
		b, err := makeBinding(c.fn)
		if err != nil {
			t.Fatalf("makeBinding(%s): %v", c.name, err)
		}
		if b.kind != bindingFunc {
			t.Fatalf("makeBinding(%s): kind = %v, want bindingFunc", c.name, b.kind)
		}
		if b.settable != c.settable {
			t.Fatalf("makeBinding(%s): settable = %v, want %v", c.name, b.settable, c.settable)
		}
	}
	// The zero-argument getter is the awaitable "read as variable" form.
	getter := func() (string, error) { return "now", nil }
	b, err := makeBinding(getter)
	if err != nil || b.settable {
		t.Fatalf("getter classification: %+v, %v", b, err)
	}
}

func TestMakeBindingRejectsNilAndUnmarshalable(t *testing.T) {
	if _, err := makeBinding(nil); err == nil {
		t.Fatal("makeBinding(nil) should fail")
	}
	if _, err := makeBinding((func())(nil)); err == nil {
		t.Fatal("makeBinding(nil func) should fail")
	}
	if _, err := makeBinding(make(chan int)); err == nil {
		t.Fatal("makeBinding(chan) should fail: not JSON-encodable")
	}
	if _, err := makeBinding(func() {}); err != nil {
		t.Fatalf("makeBinding(func) should succeed: %v", err)
	}
}

func TestMakeBindingAccessorPair(t *testing.T) {
	// A length-2 array/slice of functions is an accessor pair: the getter
	// must take no arguments, the setter exactly one.
	getSize := func() string { return "7px" }
	setSize := func(v string) {}
	for _, pair := range []any{
		[2]any{getSize, setSize},
		[]any{getSize, setSize},
		[]any{func() string { return "x" }, func(string) {}},
	} {
		b, err := makeBinding(pair)
		if err != nil {
			t.Fatalf("makeBinding(pair %T): %v", pair, err)
		}
		if b.kind != bindingAccessor || b.fn == nil || b.set == nil {
			t.Fatalf("pair %T: kind = %v, want bindingAccessor with get+set wrappers", pair, b.kind)
		}
	}
	// Not a pair: a single element or a non-func element keeps the constant
	// path and fails as unmarshalable (functions cannot be JSON-encoded).
	if _, err := makeBinding([]any{getSize}); err == nil {
		t.Fatal("one-element func slice should fail (not JSON-encodable)")
	}
	if _, err := makeBinding([]any{getSize, "not a func"}); err == nil {
		t.Fatal("pair with a non-func element should fail")
	}
	// Signature validation.
	if _, err := makeAccessorBinding(func(v string) {}, func(string) {}); err == nil {
		t.Fatal("getter with arguments must be rejected")
	}
	if _, err := makeAccessorBinding(func() string { return "" }, func(a, b string) {}); err == nil {
		t.Fatal("setter with two arguments must be rejected")
	}
	// One-sided accessors are valid (read-only / write-only variables).
	if _, err := makeAccessorBinding(func() string { return "x" }, nil); err != nil {
		t.Fatalf("getter-only accessor: %v", err)
	}
	if _, err := makeAccessorBinding(nil, func(v string) {}); err != nil {
		t.Fatalf("setter-only accessor: %v", err)
	}
	if _, err := makeAccessorBinding(nil, nil); err == nil {
		t.Fatal("accessor with neither side must be rejected")
	}
}

func TestBindEntriesExpansion(t *testing.T) {
	// A plain value yields one registry entry; an accessor pair yields three:
	// the marker entry plus the two synthetic dispatch keys. The marker keeps
	// the page name, so the doc-start script installs the accessor property
	// and the JS dispatch finds the getter/setter under the synthetic keys.
	entries, err := bindEntries("sum", func(a, b int) int { return a + b })
	if err != nil || len(entries) != 1 || entries[0].name != "sum" || entries[0].kind != bindingFunc {
		t.Fatalf("bindEntries(func) = %+v, %v; want one func entry", entries, err)
	}
	entries, err = bindEntries("app.size", func() string { return "x" }, func(v string) {})
	if err != nil {
		t.Fatalf("bindEntries(pair): %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("bindEntries(pair) len = %d, want 3 entries", len(entries))
	}
	if entries[0].name != "app.size" || entries[0].kind != bindingAccessor {
		t.Fatalf("marker entry = %+v, want accessor at app.size", entries[0])
	}
	want := []string{accessorGetKey("app.size"), accessorSetKey("app.size")}
	for i, n := range []string{entries[1].name, entries[2].name} {
		if n != want[i] || entries[i+1].kind != bindingFunc {
			t.Fatalf("dispatch entry %d = %+v, want func at %s", i, entries[i+1], want[i])
		}
	}
	// A pair passed as a single map-style value expands identically.
	asValue, err := bindEntries("app.size", []any{
		func() string { return "x" },
		func(v string) {},
	})
	if err != nil || len(asValue) != 3 || asValue[0].name != "app.size" {
		t.Fatalf("bindEntries(pair as value) = %+v, %v", asValue, err)
	}
	// Arity and nil-name validation.
	if _, err := bindEntries("", func() {}); err == nil {
		t.Fatal("empty binding name must be rejected")
	}
	if _, err := bindEntries("x", func() {}, func() {}, func() {}); err == nil {
		t.Fatal("three values must be rejected")
	}
}

// --- Bind-name validation (RE2), bindingsReplace (R1/RE4) and script
// builders (P2/E2), install error reporting (R5) ---------------------------

func TestValidateBindNameSegmentRules(t *testing.T) {
	// RE2: names with empty dot segments or whitespace are rejected loudly;
	// names that install cleanly pass.
	bad := []string{"", ".x", "x.", "a..b", "a b", "a.b c", "a\tb"}
	for _, name := range bad {
		if err := validateBindName(name); err == nil {
			t.Errorf("validateBindName(%q) = nil, want error", name)
		}
	}
	good := []string{"a", "a.b.c", "demo.theme", "appkit", "x1._y"}
	for _, name := range good {
		if err := validateBindName(name); err != nil {
			t.Errorf("validateBindName(%q) = %v, want nil", name, err)
		}
	}
	// NUL stays rejected - it is the accessor synthetic-key separator.
	if err := validateBindName("a\x00b"); err == nil {
		t.Error("validateBindName with NUL must be rejected")
	}
}

func TestBindingsReplaceRemovesOldSyntheticKeys(t *testing.T) {
	// R1(a)/RE4: storing a new binding under a page name removes every key the
	// old binding could own, including the accessor's synthetic dispatch keys
	// - replacing a function with an accessor (or vice versa) can never leak
	// stale entries.
	m := map[string]binding{}
	replace := func(entries []binding) { bindingsReplace(m, entries) }

	fn := binding{name: "demo.x", kind: bindingFunc, settable: true}
	replace([]binding{fn})
	if len(m) != 1 {
		t.Fatalf("after function bind: %d entries, want 1: %v", len(m), m)
	}
	acc, err := bindEntries("demo.x", func() string { return "v" }, func(string) error { return nil })
	if err != nil {
		t.Fatalf("bindEntries: %v", err)
	}
	replace(acc)
	if len(m) != 3 { // marker + get + set
		t.Fatalf("after accessor bind: %d entries, want 3: %v", len(m), sortedMapKeys(m))
	}
	// Replacing the accessor with a plain function must drop the synthetic keys.
	replace([]binding{binding{name: "demo.x", kind: bindingFunc, gettable: true}})
	if len(m) != 1 {
		t.Fatalf("after replace accessor with function: %d entries, want 1 (no stale synthetic keys): %v", len(m), sortedMapKeys(m))
	}
	if _, ok := m["demo.x\x00get"]; ok {
		t.Fatal("stale accessor get key survived a replace")
	}
}

// installCallFor builds one entry through bindEntries and returns its install
// call expression, so the arity/kind -> installer selection is asserted
// directly (E2 and the onBindFn split).
func installCallFor(t *testing.T, val any) string {
	t.Helper()
	entries, err := bindEntries("demo.fn", val)
	if err != nil {
		t.Fatalf("bindEntries(%T): %v", val, err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	return bindInstallCall(entries[0])
}

func TestInstallCallAritySelection(t *testing.T) {
	// Zero-argument functions install through onBind (awaitable getters -
	// E2's .then lives there); one-argument functions through onBindSetter;
	// any other arity through onBindFn (plain callable, no .then).
	if got := installCallFor(t, func() string { return "x" }); got != `onBind("demo.fn")` {
		t.Errorf("zero-arg -> %s", got)
	}
	if got := installCallFor(t, func(s string) string { return s }); got != `onBindSetter("demo.fn")` {
		t.Errorf("one-arg -> %s", got)
	}
	if got := installCallFor(t, func(a, b int) int { return a + b }); got != `onBindFn("demo.fn")` {
		t.Errorf("two-arg -> %s", got)
	}
	if got := installCallFor(t, func(args ...string) {}); got != `onBind("demo.fn")` {
		// variadic with no required arguments is callable with zero args
		t.Errorf("empty variadic -> %s", got)
	}
	if got := installCallFor(t, func(prefix string, args ...string) {}); got != `onBindFn("demo.fn")` {
		t.Errorf("variadic with required arg -> %s", got)
	}
}

func TestCreateBindScriptSortedDeterministic(t *testing.T) {
	// P2: the document-start bind script emits entries in alphabetical name
	// order no matter how the registry map iterated, so the output is
	// deterministic and golden-testable.
	entries := []binding{
		{name: "zeta", kind: bindingFunc, settable: true},
		{name: "demo.a", kind: bindingConst, value: `{"k":1}`},
		{name: "alpha", kind: bindingFunc, gettable: true},
		{name: "demo.b", kind: bindingFunc}, // plain multi-arg function
	}
	script := createBindScript(entries)
	zPos := strings.Index(script, `w.onBindSetter("zeta")`)
	aPos := strings.Index(script, `w.onBindValue("demo.a",`)
	alphaPos := strings.Index(script, `w.onBind("alpha")`)
	bPos := strings.Index(script, `w.onBindFn("demo.b")`)
	order := []int{alphaPos, aPos, bPos, zPos}
	for i, p := range order {
		if p < 0 {
			t.Fatalf("installer call %d missing from script:\n%s", i, script)
		}
	}
	for i := 1; i < len(order); i++ {
		if order[i-1] > order[i] {
			t.Fatalf("script not sorted by name:\n%s", script)
		}
	}
	if !strings.HasSuffix(script, "w.freezeBinds();\n})()") {
		t.Fatalf("script must end with freezeBinds:\n%s", script)
	}
	// Deterministic: same input, same output.
	if again := createBindScript(entries); again != script {
		t.Fatal("createBindScript is not deterministic for the same entries")
	}
}

func TestLiveBindScriptGuardsAndReports(t *testing.T) {
	// R5/R6: a live bind batch is a no-op before the bridge exists and every
	// install failure is reported to Go through internalBindError instead of
	// being swallowed by the fire-and-forget Eval.
	script := liveBindScript([]binding{{name: "demo.x", kind: bindingFunc, gettable: true}})
	if !strings.Contains(script, "var w=window.__webview__;if(!w){return;}") {
		t.Fatalf("live bind script lost its bridge guard: %s", script)
	}
	if !strings.Contains(script, `method:"`+internalBindError+`"`) {
		t.Fatalf("live bind script does not report install failures: %s", script)
	}
	if !strings.Contains(script, "w.freezeBinds()") {
		t.Fatalf("live bind script must freeze after installs: %s", script)
	}
	unbind := liveUnbindJS("demo.x")
	if !strings.Contains(unbind, `w.onUnbind("demo.x")`) {
		t.Fatalf("live unbind script: %s", unbind)
	}
	if !strings.Contains(unbind, `method:"`+internalBindError+`"`) {
		t.Fatalf("live unbind script does not report failures: %s", unbind)
	}
	if empty := liveBindScript(nil); empty != "" {
		t.Fatalf("liveBindScript(nil) = %q, want empty", empty)
	}
}

// FuzzValidateBindName pins the dotted-name validator (RE2): it must never
// panic, and a name it accepts must split into non-empty, whitespace-free
// segments.
func FuzzValidateBindName(f *testing.F) {
	for _, seed := range []string{"demo.theme", "a..b", ".x", "x.", "a b", "", "\x00"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		err := validateBindName(name)
		if err != nil {
			return
		}
		if name == "" {
			t.Fatal("accepted an empty name")
		}
		for _, seg := range strings.Split(name, ".") {
			if seg == "" {
				t.Fatalf("accepted name with an empty segment: %q", name)
			}
			if strings.ContainsAny(seg, " \t\r\n") {
				t.Fatalf("accepted name with whitespace: %q", name)
			}
		}
	})
}

// FuzzMakeFuncWrapperArgDecode feeds arbitrary JSON argument payloads into a
// bound-function wrapper (variadic and typed); decoding must never panic and
// must either succeed or return an error.
func FuzzMakeFuncWrapperArgDecode(f *testing.F) {
	fn := func(prefix string, n int, rest ...float64) string { return prefix }
	wrapper, err := makeFuncWrapper(fn)
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []string{`["p"]`, `["p",1]`, `["p",1,2.5]`, `["p","x"]`, `[]`, `{"a":1}`, `"p"`, `[1,2]`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		_, _ = wrapper("", raw)
	})
}

// TestGeneratedScriptsParse runs node --check over every script appkit
// generates and injects (RD2): the scripts live inside Go raw strings, where
// a syntax error has no editor support and would only surface as a silent
// page failure. The behavioral harnesses below exercise them too, but this
// parse gate fails FAST with a clear message naming the broken script. It
// skips when node is missing (the Makefile js-check target makes it
// mandatory in CI).
func TestGeneratedScriptsParse(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	// One generated script per family: the app-region tracker, the bridge
	// (createInitScript), the events bridge and the bind script with every
	// installer kind present (constant, accessor, setter, getter, plain fn).
	scripts := map[string]string{
		"appRegions":  createAppRegionScript(true, true, "linux"),
		"bridge":      createInitScript("function(m){}", strings.Repeat("0", bridgeTokenLen), []string{"http://127.0.0.1:8080", "data:text/html,x"}, false),
		"bridgeLinks": createInitScript("function(m){}", strings.Repeat("0", bridgeTokenLen), []string{"http://127.0.0.1:8080"}, true),
		"events":      eventsInitScript("events"),
		"bind": createBindScript([]binding{
			{name: "demo.a", kind: bindingConst, value: `{"k":1}`},
			{name: "demo.b", kind: bindingAccessor},
			{name: "demo.c", kind: bindingFunc, settable: true},
			{name: "demo.d", kind: bindingFunc, gettable: true},
			{name: "demo.e", kind: bindingFunc},
		}),
		"liveBind":   liveBindScript([]binding{{name: "demo.x", kind: bindingFunc, gettable: true}}),
		"liveUnbind": liveUnbindJS("demo.x"),
	}
	for name, src := range scripts {
		file := filepath.Join(t.TempDir(), name+".js")
		if err := os.WriteFile(file, []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		out, err := exec.Command(node, "--check", file).CombinedOutput()
		if err != nil {
			t.Fatalf("node --check failed for the %s script: %v\n%s", name, err, out)
		}
	}
}

// TestSerialQueueRunsInOrder pins the ordering the engines rely on for
// read-after-write: tasks submitted to a view's serialQueue must run in
// submission order, so a read issued after a write cannot observe the old
// value. The queue is the only access to got, so no lock is needed.
func TestSerialQueueRunsInOrder(t *testing.T) {
	var q serialQueue
	const n = 200
	got := make([]int, 0, n)
	done := make(chan struct{})
	for i := 0; i < n; i++ {
		i, last := i, i == n-1
		q.do(func() {
			got = append(got, i)
			if last {
				close(done)
			}
		})
	}
	<-done
	if len(got) != n {
		t.Fatalf("ran %d tasks, want %d", len(got), n)
	}
	for pos, task := range got {
		if task != pos {
			t.Fatalf("task %d ran at position %d - serialQueue is out of order", task, pos)
		}
	}
}

// TestBridgeGate runs the document-start bridge under node against the
// locations a browser gives a document, parsed by node's WHATWG URL parser.
// The bridge must appear only in a top-level document whose origin key, as
// the script computes it, is one Go trusted, and that key must be the one
// originOf gives for the same URL, or a trusted page would lose its bridge.
func TestBridgeGate(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	token := strings.Repeat("cd", bridgeTokenLen/2)
	// Each URL is trusted in Go by its own spelling, then loaded as a browser
	// would canonicalize it.
	trusted := []string{
		"http://LocalHost:08080/page",
		"https://app.localhost/index.html",
		"app://app/index.html",
		"http://[::ffff:127.0.0.1]:80/",
		"http://１２７.１:8080/",
		"http://bücher.example/",
		"data:text/html,<p>hi</p>#frag",
		"data:text/html,<p>a b</p>\n<i>\"q\" é</i>?x y<z>&k=%41#frag",
		"data:text/html,<p>a b</p>\n<i>'r'</i>?x y&k='z'", // loaded below as WebView2 and NSURL spell it
		"data:text/html,%3Cp%3E%F0%9F%99%82%20ok%3C/p%3E",
		"data:text/plain,<p>a,b",
		"data:text/html,a%23b%2Cc#frag",
		"data:text/html,100%",
		"mailto:<a>@b.invalid?subject=<x> \"q\"&t='s'",
		"data:text/plain;charset=\"utf-8\",<p>hello",
	}
	// Navigate loads and trusts each URL in its canonical form.
	for i, u := range trusted {
		trusted[i] = canonicalNavigateURL(u)
	}
	var c viewCore
	c.trustURLs(trusted)
	c.trustURL("about:blank") // never trusted, so never in the script
	c.mu.Lock()
	c.token = token
	bridge := c.bridgeScriptLocked("function(m) { posted.push(m); }")
	c.mu.Unlock()

	type load struct {
		URL   string `json:"url"`
		Top   bool   `json:"top"`
		Want  bool   `json:"want"`
		GoKey string `json:"goKey"`
	}
	var loads []load
	for _, u := range trusted {
		loads = append(loads, load{URL: u, Top: true, Want: true, GoKey: originOf(u)})
	}
	loads = append(loads,
		// Go trusted the spelling above; the engine may report another.
		load{URL: "data:text/html,<p>a b</p>%0A<i>'r'</i>?x%20y&k=%27z%27", Top: true, Want: true},
		load{URL: "data:text/html,%3Cp%3Ea%20b%3C/p%3E%0A%3Ci%3E%27r%27%3C/i%3E?x%20y&k=%27z%27", Top: true, Want: true},
		load{URL: "mailto:<a>@b.invalid?subject=%3Cx%3E%20%22q%22&t='s'", Top: true, Want: true},
		// An escaped comma in the metadata is not the comma that ends it.
		load{URL: "data:text/plain%2C<p>a,b", Top: true},
		load{URL: "http://localhost:8081/page", Top: true},
		load{URL: "http://localhost:8080/page", Top: false},
		load{URL: "about:blank", Top: true},
		load{URL: "data:text/html,<p>other</p>", Top: true},
	)
	loadsJSON, err := json.Marshal(loads)
	if err != nil {
		t.Fatal(err)
	}

	harness := `
const bridge = ` + marshalJSON(bridge) + `;
const loads = ` + string(loadsJSON) + `;
let failed = false;
for (const l of loads) {
  const u = new URL(l.url);
  const posted = [];
  const win = { crypto: { getRandomValues: function(a) { return a; } },
    location: { protocol: u.protocol, host: u.host, href: u.href } };
  win.top = l.top ? win : {};
  new Function('window', 'posted', bridge)(win, posted);
  const got = typeof win.__webview__ === 'object';
  if (got !== l.want) {
    console.error(l.url + ' (top=' + l.top + '): bridge=' + got + ', want ' + l.want +
      '; browser key ' + (u.host ? u.protocol + '//' + u.host : u.href.split('#')[0]) +
      ', Go key ' + l.goKey);
    failed = true;
    continue;
  }
  if (got) {
    win.__webview__.post('{"method":"m"}');
    if (posted[0] !== ` + marshalJSON(token) + ` + '{"method":"m"}') {
      console.error(l.url + ': post sent ' + posted[0]);
      failed = true;
    }
  }
}
if (bridge.indexOf('"about:') >= 0) { console.error('about: is in the trusted set'); failed = true; }
process.exit(failed ? 1 : 0);
`
	file := filepath.Join(t.TempDir(), "gate.js")
	if err := os.WriteFile(file, []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, file).CombinedOutput(); err != nil {
		t.Fatalf("bridge gate: %v\n%s", err, out)
	}
}

func TestCheckToken(t *testing.T) {
	var c viewCore
	if _, ok := c.checkToken(`{"method":"m"}`); ok {
		t.Fatal("a view with no token accepted a message")
	}
	c.mu.Lock()
	_ = c.bridgeScriptLocked("function(m){}")
	token := c.token
	c.mu.Unlock()
	if len(token) != bridgeTokenLen {
		t.Fatalf("token length %d, want %d", len(token), bridgeTokenLen)
	}
	if body, ok := c.checkToken(token + `{"method":"m"}`); !ok || body != `{"method":"m"}` {
		t.Fatalf("checkToken(token+body) = %q, %v", body, ok)
	}
	wrong := strings.Repeat("0", bridgeTokenLen)
	for _, body := range []string{"", token[:10], `{"method":"m"}`, wrong + `{"method":"m"}`} {
		if _, ok := c.checkToken(body); ok {
			t.Errorf("checkToken(%q) accepted", body)
		}
	}
}

// TestOutsideLinksScript runs the bridge's outside-link intercept (see
// initOutsideLinks) in node against a stub window, and checks which clicks,
// form submits, and navigate events it hands to Go and which it leaves to
// the page and the engine.
func TestOutsideLinksScript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	token := strings.Repeat("ef", bridgeTokenLen/2)
	bridge := createInitScript("function(m) { posted.push(m); }", token, []string{"http://127.0.0.1:8080"}, true)
	harness := `
const bridge = ` + marshalJSON(bridge) + `;
const token = ` + marshalJSON(token) + `;
const listeners = {}, navListeners = [];
const posted = [];
const win = { crypto: { getRandomValues: function(a) { return a; } },
  location: { protocol: 'http:', host: '127.0.0.1:8080', href: 'http://127.0.0.1:8080/page' },
  addEventListener: function(type, fn) { (listeners[type] = listeners[type] || []).push(fn); },
  navigation: { addEventListener: function(type, fn) { if (type === 'navigate') navListeners.push(fn); } } };
win.top = win;
function StubFormData(form) { return form.fields; }
new Function('window', 'posted', 'FormData', bridge)(win, posted, StubFormData);
function el(attrs, props) {
  return Object.assign({ getAttribute: function(n) { return n in attrs ? attrs[n] : null; },
    hasAttribute: function(n) { return n in attrs; } }, props);
}
function ev(props) {
  return Object.assign({ defaultPrevented: false, button: 0, cancelled: false,
    preventDefault: function() { this.defaultPrevented = true; this.cancelled = true; } }, props);
}
function click(a, extra) {
  const e = ev(Object.assign({ target: { closest: function() { return a; } } }, extra));
  listeners.click.forEach(function(fn) { fn(e); });
  return e;
}
// A click on a link inside a shadow root: window sees the event retargeted to
// the host, which is not a link, and only the composed path holds the link.
function shadowClick(a) {
  const span = el({}, { localName: 'span' }), host = el({}, { localName: 'my-widget' });
  const e = ev({ target: { closest: function() { return null; } },
    composedPath: function() { return [span, a, { nodeType: 11 }, host, { localName: 'body' }, win]; } });
  listeners.click.forEach(function(fn) { fn(e); });
  return e;
}
function submit(form, extra) {
  const e = ev(Object.assign({ target: form }, extra));
  listeners.submit.forEach(function(fn) { fn(e); });
  return e;
}
function navigate(url, extra) {
  const e = ev(Object.assign({ cancelable: true, hashChange: false, formData: null, downloadRequest: null,
    navigationType: 'push', destination: { url: url } }, extra));
  navListeners.forEach(function(fn) { fn(e); });
  return e;
}
const out = 'https://example.com/a?b=1#c';
const cases = [
  ['outside link', function() { return click(el({ href: out }, { href: out })); }, out],
  ['outside link, target _top', function() { return click(el({ href: out, target: '_top' }, { href: out })); }, out],
  ['trusted link', function() { return click(el({ href: '/x' }, { href: 'http://127.0.0.1:8080/x' })); }, null],
  ['ctrl-click', function() { return click(el({ href: out }, { href: out }), { ctrlKey: true }); }, null],
  ['middle button', function() { return click(el({ href: out }, { href: out }), { button: 1 }); }, null],
  ['target _blank', function() { return click(el({ href: out, target: '_blank' }, { href: out })); }, null],
  ['download', function() { return click(el({ href: out, download: '' }, { href: out })); }, null],
  ['page handled it', function() { return click(el({ href: out }, { href: out }), { defaultPrevented: true }); }, null],
  ['mailto', function() { return click(el({ href: 'mailto:a@b.invalid' }, { href: 'mailto:a@b.invalid' })); }, null],
  ['no link', function() { return click(null); }, null],
  ['outside link in a shadow root', function() { return shadowClick(el({ href: out }, { localName: 'a', href: out })); }, out],
  ['trusted link in a shadow root', function() { return shadowClick(el({ href: '/x' }, { localName: 'a', href: 'http://127.0.0.1:8080/x' })); }, null],
  ['anchor without href in a shadow root', function() { return shadowClick(el({}, { localName: 'a' })); }, null],
  ['GET form', function() { return submit(el({ method: 'get' }, { action: 'https://example.com/s', fields: [['q', 'a b']] })); }, 'https://example.com/s?q=a+b'],
  ['POST form', function() { return submit(el({ method: 'post' }, { action: 'https://example.com/s', fields: [] })); }, null],
  ['trusted GET form', function() { return submit(el({}, { action: 'http://127.0.0.1:8080/s', fields: [] })); }, null],
  ['navigate push', function() { return navigate(out); }, out],
  ['navigate replace', function() { return navigate(out, { navigationType: 'replace' }); }, out],
  ['navigate traverse', function() { return navigate(out, { navigationType: 'traverse' }); }, null],
  ['navigate hash', function() { return navigate(out, { hashChange: true }); }, null],
  ['navigate not cancelable', function() { return navigate(out, { cancelable: false }); }, null],
  ['navigate trusted', function() { return navigate('http://127.0.0.1:8080/y'); }, null],
];
let failed = false;
for (const [name, run, want] of cases) {
  posted.length = 0;
  const e = run();
  let got = null;
  if (posted.length === 1 && posted[0].slice(0, token.length) === token) {
    const m = JSON.parse(posted[0].slice(token.length));
    got = m.method === ` + marshalJSON(internalOpenExternal) + ` ? m.params[0] : 'method ' + m.method;
  } else if (posted.length > 1) {
    got = posted.length + ' posts';
  }
  if (got !== want || (want !== null) !== e.cancelled) {
    console.error(name + ': posted ' + got + ' cancelled ' + e.cancelled + ', want ' + want);
    failed = true;
  }
}
process.exit(failed ? 1 : 0);
`
	file := filepath.Join(t.TempDir(), "outside.js")
	if err := os.WriteFile(file, []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, file).CombinedOutput(); err != nil {
		t.Fatalf("outside links: %v\n%s", err, out)
	}
}
