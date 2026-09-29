package tuohi

// The binding registry model and value-to-binding conversion: what a Bind
// entry becomes on the page (function / constant / accessor), the dotted-name
// rules, engine-side registry helpers (bindingsReplace, prepareBindBatch) and
// the live-install error reporting. Its generated-JS counterpart is
// bind_gen.go; the events bridge it relies on lives in bind_evt.go.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"unicode"
)

var errorType = reflect.TypeFor[error]()

// bindingKind tells what a registered binding installs on the page.
type bindingKind uint8

const (
	bindingFunc     bindingKind = iota // a Go function the page can call - zero-arg: also an awaitable getter; one-arg: also an assignable setter
	bindingConst                       // a JSON constant installed in the page
	bindingAccessor                    // a getter and/or setter installed as an accessor property
)

// binding is one registered name on a webview. The binding process is
// strictly per-name: a name maps to a Go function the page calls
// (bindingFunc), a constant value (bindingConst) or a readable/writable
// accessor backed by a getter and a setter function (bindingAccessor) -
// nothing is derived from the Go type, structs and maps are never walked,
// and there is no auto-generated naming (the entry's dotted key IS the full
// page path). A bound function doubles as a getter: awaiting it
// (`await window.name`) calls it with no arguments and resolves to the
// result, so "call it" and "read its value" are both available on one name.
type binding struct {
	name     string // the registry key (== the page path)
	kind     bindingKind
	fn       func(id, req string) (any, error) // bindingFunc / bindingAccessor: the marshalling wrapper (getter)
	set      func(id, req string) (any, error) // bindingAccessor: the setter wrapper
	settable bool                              // bindingFunc: bound from a 1-argument function - doubles as a callable setter (assignable)
	gettable bool                              // bindingFunc: callable with ZERO arguments - also an awaitable getter (await window.name)
	value    string                            // bindingConst: the raw JSON constant
}

// makeBinding turns one Bind value into a registry entry: a function value
// becomes a callable binding (validated once through makeFuncWrapper) - a
// zero-argument function also works as a callable getter (awaitable), a
// one-argument function as a callable setter (assignable) - a length-2
// array of exactly two functions ([2]any{getter, setter}) becomes an
// accessor pair (see funcPair and makeAccessorBinding), and any other
// non-nil value is JSON-encoded into an immutable constant binding. A value
// that is none of these - or a nil value - returns an error.
func makeBinding(v any) (binding, error) {
	if v == nil {
		return binding{}, errors.New("cannot bind a nil value")
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Func {
		if rv.IsNil() {
			return binding{}, errors.New("cannot bind a nil function")
		}
		wrapper, err := makeFuncWrapper(v)
		if err != nil {
			return binding{}, err
		}
		// A function taking exactly one (non-variadic) argument doubles as a
		// callable setter: the page can call it AND assign to it. A function
		// callable with zero arguments doubles as a callable getter: the page
		// can call it AND await it (`await window.name` reads its value).
		// "Callable with zero arguments" subtracts the variadic slice
		// parameter (reflect reports it as one input), so func(...string) -
		// zero required arguments - is a getter too. Functions of any other
		// arity stay plain callables - no assignment, no awaitable read.
		ft := rv.Type()
		required := ft.NumIn()
		if ft.IsVariadic() {
			required-- // the variadic slice parameter takes no required argument
		}
		return binding{
			kind:     bindingFunc,
			fn:       wrapper,
			settable: ft.NumIn() == 1 && !ft.IsVariadic(),
			gettable: required == 0,
		}, nil
	}
	if g, s, ok := funcPair(v); ok {
		return makeAccessorBinding(g, s)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return binding{}, err
	}
	return binding{kind: bindingConst, value: string(data)}, nil
}

// funcPair recognizes the accessor-pair value shape: an array or slice of
// exactly two non-nil functions (getter, setter) - the documented
// [2]any{getter, setter} form. ok is false for any other value, which then
// takes the constant path (and errors there - a container of functions is
// not JSON-encodable).
func funcPair(v any) (getter, setter any, ok bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Array && rv.Kind() != reflect.Slice {
		return nil, nil, false
	}
	if rv.Len() != 2 {
		return nil, nil, false
	}
	g, s := rv.Index(0), rv.Index(1)
	// Elements whose static type is interface{} (e.g. [2]any{get, set})
	// report Kind reflect.Interface - unwrap them to see the dynamic kind.
	if g.Kind() == reflect.Interface {
		g = g.Elem()
	}
	if s.Kind() == reflect.Interface {
		s = s.Elem()
	}
	if !g.IsValid() || !s.IsValid() ||
		g.Kind() != reflect.Func || s.Kind() != reflect.Func {
		return nil, nil, false
	}
	if g.IsNil() || s.IsNil() {
		return nil, nil, false
	}
	return g.Interface(), s.Interface(), true
}

// makeAccessorBinding validates a getter and/or setter (a nil side is
// allowed - a read-only variable has no setter, a write-only one no getter)
// and turns them into an accessor binding. The getter, when present, must
// take no arguments (it answers reads, `await window.name`); the setter,
// when present, must take exactly one (it receives the assigned value).
// Returns an error for a function that fails validation.
func makeAccessorBinding(getter, setter any) (binding, error) {
	b := binding{kind: bindingAccessor}
	if getter != nil {
		g, err := makeFuncWrapper(getter)
		if err != nil {
			return binding{}, fmt.Errorf("accessor getter: %w", err)
		}
		gv := reflect.ValueOf(getter).Type()
		if gv.NumIn() != 0 && !gv.IsVariadic() {
			return binding{}, errors.New("accessor getter must take no arguments")
		}
		b.fn = g
	}
	if setter != nil {
		s, err := makeFuncWrapper(setter)
		if err != nil {
			return binding{}, fmt.Errorf("accessor setter: %w", err)
		}
		sv := reflect.ValueOf(setter).Type()
		if sv.NumIn() != 1 || sv.IsVariadic() {
			return binding{}, errors.New("accessor setter must take exactly one argument")
		}
		b.set = s
	}
	if b.fn == nil && b.set == nil {
		return binding{}, errors.New("accessor binding needs a getter, a setter, or both")
	}
	return b, nil
}

// accessorGetKey / accessorSetKey are the internal dispatch names the JS
// accessor property calls the getter/setter through. "\x00" can never appear
// in a user-supplied dotted name (binding names are validated against it), so
// the synthetic keys cannot collide with a real binding.
func accessorGetKey(name string) string { return name + "\x00get" }
func accessorSetKey(name string) string { return name + "\x00set" }

// isAccessorKey reports whether a registry key is a synthetic accessor
// dispatch key (see accessorGetKey / accessorSetKey). Such entries exist
// only so the engines can dispatch the getter/setter calls; they never
// install anything on the page themselves.
func isAccessorKey(name string) bool {
	return strings.HasSuffix(name, "\x00get") || strings.HasSuffix(name, "\x00set")
}

// validateBindName rejects names that would clash with the internal accessor
// key scheme or confuse the dotted-path installer: an empty name, a name
// with a NUL byte (the accessor synthetic-key separator, see accessorGetKey),
// a name with empty dot segments ("a..b", ".x", "x.") or a name whose
// segments contain whitespace cannot be installed as window properties
// through the dot walker and are almost certainly caller mistakes - they
// fail loudly at App.Show instead of creating odd, unreachable page
// properties.
func validateBindName(name string) error {
	if name == "" {
		return errors.New("binding name must not be empty")
	}
	if strings.ContainsRune(name, 0) {
		return errors.New("binding name must not contain NUL")
	}
	for _, seg := range strings.Split(name, ".") {
		if seg == "" {
			return fmt.Errorf("binding name %q must not have empty dot segments", name)
		}
		if strings.IndexFunc(seg, unicode.IsSpace) >= 0 {
			return fmt.Errorf("binding name %q must not contain whitespace", name)
		}
	}
	return nil
}

// bindEntries prepares the registry entries for one engine Bind call: a
// single value (function, constant, or a [2]any{getter, setter} accessor
// pair) or an explicit (getter, setter) pair passed as two arguments.
// An accessor expands into the marker entry at the page name (installs the
// accessor property) plus the synthetic dispatch entries the JS accessor
// calls through - a getter-only accessor only gets the getter entry, a
// setter-only one only the setter. The caller stores them all under
// their own keys and drives the live install from entries[0].
func bindEntries(name string, vals ...any) ([]binding, error) {
	if err := validateBindName(name); err != nil {
		return nil, err
	}
	var entry binding
	var err error
	switch len(vals) {
	case 1:
		entry, err = makeBinding(vals[0])
	case 2:
		entry, err = makeAccessorBinding(vals[0], vals[1])
	default:
		return nil, fmt.Errorf("Bind expects one value or a (getter, setter) pair, got %d values", len(vals))
	}
	if err != nil {
		return nil, err
	}
	entry.name = name
	if entry.kind != bindingAccessor {
		return []binding{entry}, nil
	}
	out := []binding{entry}
	if entry.fn != nil {
		out = append(out, binding{name: accessorGetKey(name), kind: bindingFunc, fn: entry.fn})
	}
	if entry.set != nil {
		out = append(out, binding{name: accessorSetKey(name), kind: bindingFunc, fn: entry.set})
	}
	return out, nil
}

// makeFuncWrapper inspects a user-supplied function "f" via reflection once,
// validating its signature and caching the relevant details.
// It returns a closure that, given (id, req string),
// decodes JSON args, calls the underlying function, and returns (value, error).
//
//nolint:cyclop,funlen // reflection over the caller's signature is inherently branchy; splitting it would scatter the single validation path.
func makeFuncWrapper(f any) (func(id, req string) (any, error), error) {
	v := reflect.ValueOf(f)
	if v.Kind() != reflect.Func {
		return nil, errors.New("only functions can be bound")
	}

	funcType := v.Type()
	outCount := funcType.NumOut()
	if outCount > 2 {
		return nil, errors.New("function may only return a value or value+error")
	}

	numIn := funcType.NumIn()
	isVariadic := funcType.IsVariadic()
	inTypes := make([]reflect.Type, numIn)
	for i := range numIn {
		inTypes[i] = funcType.In(i)
	}

	var returnsError bool
	switch outCount {
	case 1:
		if funcType.Out(0).Implements(errorType) {
			returnsError = true
		}
	case 2:
		if !funcType.Out(1).Implements(errorType) {
			return nil, errors.New("second return value must implement error")
		}
	}

	fn := func(_, req string) (any, error) {
		var rawArgs []json.RawMessage
		err := json.Unmarshal([]byte(req), &rawArgs)
		if err != nil {
			return nil, err
		}
		if (!isVariadic && len(rawArgs) != numIn) || (isVariadic && len(rawArgs) < numIn-1) {
			return nil, errors.New("function arguments mismatch")
		}

		args := make([]reflect.Value, len(rawArgs))
		for i := range rawArgs {
			var argVal reflect.Value
			if isVariadic && i >= numIn-1 {
				argVal = reflect.New(inTypes[numIn-1].Elem())
			} else {
				argVal = reflect.New(inTypes[i])
			}
			err = json.Unmarshal(rawArgs[i], argVal.Interface())
			if err != nil {
				return nil, err
			}
			args[i] = argVal.Elem()
		}

		res := v.Call(args)

		switch outCount {
		case 0:
			return nil, nil //nolint:nilnil // a nil value with a nil error is the "no result" contract the bridge marshals.
		case 1:
			if returnsError {
				v := res[0].Interface()
				if v != nil {
					return nil, v.(error)
				}
				return nil, nil //nolint:nilnil // an error-only function returning nil means "no error".
			}
			return res[0].Interface(), nil
		case 2:
			var err error
			v := res[1].Interface()
			if v != nil {
				err = v.(error)
			}
			return res[0].Interface(), err
		default:
			panic("unreachable")
		}
	}

	return fn, nil
}

// preparedBind is one validated bind request of a batch: the page name plus
// the registry entries bindEntries produced for it (the marker entry plus,
// for an accessor, its synthetic dispatch keys).
type preparedBind struct {
	name    string
	entries []binding
}

// prepareBindBatch validates every request of a bind batch through
// bindEntries - one bad name or value fails the WHOLE batch before anything
// is stored, so a failed App.Show never leaves a half-bound window -
// and returns the per-request entries (for bindingsReplace) plus the
// flattened, page-installing entries in request order (for the live install
// script; the accessors' synthetic dispatch keys never install anything on
// the page themselves). The engine BindBatch implementations call it before
// taking their registry lock.
func prepareBindBatch(batch []bindRequest) ([]preparedBind, []binding, error) {
	prepared := make([]preparedBind, 0, len(batch))
	var live []binding
	for _, req := range batch {
		entries, err := bindEntries(req.name, req.vals...)
		if err != nil {
			return nil, nil, err
		}
		prepared = append(prepared, preparedBind{name: req.name, entries: entries})
		for _, e := range entries {
			if e.kind == bindingFunc && isAccessorKey(e.name) {
				continue
			}
			live = append(live, e)
		}
	}
	return prepared, live, nil
}

// bindingsReplace stores entries into a binding registry map, REPLACING any
// binding already registered under the same page name - the override
// semantics App.Show documents ("a view entry overrides the same app-wide
// name"): the engine no longer answers "name already bound", because the
// declarative order (app entries first, then view entries) makes the LAST
// binding of a name the one that wins, deterministically. Every key the old
// binding could own - the page name plus its two synthetic accessor dispatch
// keys - is deleted first, so a replaced binding cannot leak stale dispatch
// keys or unreachable entries. entries[0].name is the page name;
// the remaining entries are the accessor's synthetic dispatch keys. Callers
// hold the registry lock.
func bindingsReplace(m map[string]binding, entries []binding) {
	if len(entries) == 0 {
		return
	}
	page := entries[0].name
	for _, old := range []string{page, accessorGetKey(page), accessorSetKey(page)} {
		delete(m, old)
	}
	for _, e := range entries {
		m[e.name] = e
	}
}

// bindErrorReport is the payload of an internalBindError message: the page
// name whose live install/unbind failed and the thrown error text.
type bindErrorReport struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// handleInternalBindError logs a live-install failure the page reported (see
// installErrorJS). The engines route internalBindError messages here from
// their onMessage switch, before any registry lookup.
func handleInternalBindError(params json.RawMessage) {
	var reports []bindErrorReport
	if err := json.Unmarshal(params, &reports); err != nil || len(reports) == 0 {
		return
	}
	r := reports[0]
	log.Printf("tuohi: live bind/unbind of %q failed on the page: %q", r.Name, r.Error)
}

// serialQueue runs submitted functions one at a time, in submission order,
// each on its own goroutine. The engines dispatch every binding call through
// a view's single queue, so the page observes the effects of its calls in the
// order it made them: a read issued right after a write
// (`window.count = 1; await window.count`) must not overtake the write, even
// though the two calls arrive as separate messages (each would otherwise run
// on its own goroutine, in scheduler order). fn still runs OFF the UI thread,
// so a blocking binding (a native dialog, say) never stalls the engine - it
// only delays that view's later calls, which the page is awaiting anyway.
type serialQueue struct {
	mu   sync.Mutex
	prev chan struct{}
}

// do submits fn to run after every previously submitted function.
func (q *serialQueue) do(fn func()) {
	q.mu.Lock()
	prev := q.prev
	done := make(chan struct{})
	q.prev = done
	q.mu.Unlock()
	go func() {
		if prev != nil {
			<-prev
		}
		defer close(done)
		fn()
	}()
}

// --- webview binding surface (shared by every engine) ---------------------

// Bind registers ONE name/value on the webview (the events bridge and the
// per-name fallback path use it); the batch form BindBatch does the work.
func (w *webview) Bind(name string, vals ...any) error {
	return w.BindBatch([]bindRequest{{name: name, vals: vals}})
}

// bindingEntriesLocked returns every registered binding that installs
// something on the page, for the document-start bind script (see
// createBindScript). The synthetic dispatch entries of accessor pairs are
// skipped - the marker entry carries the install.
func (w *webview) bindingEntriesLocked() []binding {
	entries := make([]binding, 0, len(w.bindings))
	for _, e := range w.bindings {
		if e.kind == bindingFunc && isAccessorKey(e.name) {
			continue
		}
		entries = append(entries, e)
	}
	return entries
}

// eventsGlobalName reports the page global this view's events API lives at
// (window.<name>); the bind-name validation uses it so a top-level binding
// cannot clobber the events bridge (see eventsGlobalProvider).
func (w *webview) eventsGlobalName() string { return w.eventsGlobal }
