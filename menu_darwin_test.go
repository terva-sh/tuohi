//go:build darwin

package tuohi

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
)

// The default main menu (TKT-01M3N2Q03HV78EKDSH9E4CG0EY). Every key below is
// sent through [NSApp sendEvent:], the way a keyboard's would be, so it
// reaches the main menu's key equivalents before the window.

var (
	resMenuKeys   atomic.Value // string
	resMenuClose  atomic.Value // string
	resMenuEdit   atomic.Value // string
	resMenuNoMenu atomic.Value // string
)

// Virtual key codes of the keys the scenarios press (kVK_ANSI_*).
const (
	keyA = 0
	keyZ = 6
	keyX = 7
	keyC = 8
	keyV = 9
	keyW = 13
)

// pressKey hands a key-down event for chars with the given modifiers to the
// main menu's performKeyEquivalent:, on the main thread, which is where
// AppKit routes a key equivalent before any window sees it. Going to the menu
// directly tests its routing without depending on how the runner delivers
// synthesized events: GitHub run 36825339187 sent them through [NSApp
// sendEvent:] and none reached the menu. With no main menu the event goes to
// NSApp, which is the path a page gets without one. It returns the state that
// explains a key that did nothing: whether the menu handled it, and which
// windows were key and main.
func pressKey(win objc.ID, chars string, code uint16, mods uint, makeKey ...bool) string {
	var state string
	performOnMain(func() {
		autorelease(func() {
			app := class("NSApplication").Send(sel("sharedApplication"))
			before := ""
			if len(makeKey) > 0 && makeKey[0] {
				// In the same main-thread turn as the key, so nothing between
				// them can give key status back to another window: GitHub run
				// 36831446783 lost it between two separate turns.
				// Activate only an inactive app: every run so far reported it
				// active, and activating an active one can hand key status
				// back to the window that had it.
				if !objc.Send[bool](app, sel("isActive")) {
					app.Send(sel("activateIgnoringOtherApps:"), true)
				}
				win.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
				before = fmt.Sprintf("keyBefore=%v ", app.Send(sel("keyWindow")) == win)
			}
			ev := class("NSEvent").Send(
				sel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
				uint(nsEventTypeKeyDown), cgPoint{0, 0}, mods, float64(0),
				objc.Send[int](win, sel("windowNumber")), objc.ID(0),
				nsstr(chars), nsstr(chars), false, code)
			handled := "none"
			if menu := app.Send(sel("mainMenu")); menu != 0 {
				handled = fmt.Sprint(objc.Send[bool](menu, sel("performKeyEquivalent:"), ev))
			} else {
				app.Send(sel("sendEvent:"), ev)
			}
			state = fmt.Sprintf("[%shandled=%s key=%v main=%v active=%v]", before, handled,
				app.Send(sel("keyWindow")) == win, app.Send(sel("mainWindow")) == win,
				objc.Send[bool](app, sel("isActive")))
		})
	})
	return state
}

// menuKeysScenario lists the main menu's key equivalents as "key=action",
// with ⌥ marked, sorted.
func menuKeysScenario() string {
	var out []string
	performOnMain(func() {
		bar := class("NSApplication").Send(sel("sharedApplication")).Send(sel("mainMenu"))
		if bar == 0 {
			return
		}
		for i := 0; i < objc.Send[int](bar, sel("numberOfItems")); i++ {
			sub := bar.Send(sel("itemAtIndex:"), i).Send(sel("submenu"))
			if sub == 0 {
				continue
			}
			for j := 0; j < objc.Send[int](sub, sel("numberOfItems")); j++ {
				item := sub.Send(sel("itemAtIndex:"), j)
				key := cstr(item.Send(sel("keyEquivalent")).Send(sel("UTF8String")))
				if key == "" {
					continue
				}
				if objc.Send[uint](item, sel("keyEquivalentModifierMask"))&nsEventModifierFlagOption != 0 {
					key = "opt-" + key
				}
				if objc.Send[int](item, sel("tag")) != menuItemTag {
					// AppKit adds its own items to a menu titled Edit, such
					// as Start Dictation and Emoji & Symbols.
					continue
				}
				out = append(out, key+"="+selName(objc.Send[objc.SEL](item, sel("action"))))
			}
		}
	})
	if len(out) == 0 {
		return "no main menu"
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

// selName names one of the actions the default menu uses, "?" for another.
func selName(s objc.SEL) string {
	for _, name := range []string{
		"orderFrontStandardAboutPanel:", "hide:", "hideOtherApplications:", "unhideAllApplications:",
		"tuohiQuit:", "undo:", "redo:", "cut:", "copy:", "paste:", "selectAll:",
		"tuohiMinimize:", "tuohiClose:",
	} {
		if sel(name) == s {
			return name
		}
	}
	return "?"
}

// TestDefaultMainMenu checks that the default main menu carries the standard
// key equivalents.
func TestDefaultMainMenu(t *testing.T) {
	got, _ := resMenuKeys.Load().(string)
	requireGUI(t, got)
	want := "Z=redo: a=selectAll: c=copy: h=hide: m=tuohiMinimize: opt-h=hideOtherApplications: q=tuohiQuit: v=paste: w=tuohiClose: x=cut: z=undo:"
	if got != want {
		t.Fatalf("main menu keys:\n got %s\nwant %s", got, want)
	}
}

// menuCloseScenario presses ⌘W on a framed and on a frameless window, each
// the key window, and reports whether each closed.
func menuCloseScenario() string {
	var out []string
	for _, framed := range []bool{true, false} {
		out = append(out, fmt.Sprintf("frame=%v:%s", framed, menuCloseOne(framed)))
	}
	return strings.Join(out, " ")
}

func menuCloseOne(framed bool) string {
	loaded := make(chan struct{}, 1)
	w := &View{Frame: framed, Width: 320, Height: 240, Bind: map[string]any{"loaded": func() {
		select {
		case loaded <- struct{}{}:
		default:
		}
	}}}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()
	nw := native(w)
	result := make(chan string, 1)
	time.AfterFunc(30*time.Second, func() { w.Close() })
	go func() {
		// Run ends only when the last window does, and earlier scenarios
		// leave windows counted, so the scenario ends Run itself once it
		// knows whether the window closed.
		defer w.Close()
		// Press only once the page is up and the window is key: GitHub run
		// 36827912989 pressed before either, and ⌘W closed whatever window
		// was key instead.
		select {
		case <-loaded:
		case <-time.After(10 * time.Second):
			result <- "not closed [page never loaded]"
			return
		}
		// pressKey makes the window key itself, in the same turn as the key:
		// after Focus, and after a separate makeKeyAndOrderFront:, another
		// window was key by the time ⌘W arrived (GitHub runs 36829402711
		// and 36831446783).
		state := pressKey(nw.window, "w", keyW, nsEventModifierFlagCommand, true)
		// The window's own close: onWindowWillClose closes nw.closed.
		select {
		case <-nw.closed:
			result <- "closed"
		case <-time.After(5 * time.Second):
			result <- "not closed " + state
		}
	}()
	nw.loadHTML(`<!DOCTYPE html><html><body>close me
<script>window.addEventListener('load', function(){ window.loaded(); });</script></body></html>`)
	w.w.Run()
	select {
	case r := <-result:
		return r
	default:
		return "no report"
	}
}

// TestMenuCloseKey checks that ⌘W closes the key window, framed or not.
func TestMenuCloseKey(t *testing.T) {
	got, _ := resMenuClose.Load().(string)
	requireGUI(t, got)
	if want := "frame=true:closed frame=false:closed"; got != want {
		t.Fatalf("⌘W: got %q, want %q", got, want)
	}
}

// menuEditScenario drives the Edit keys in a page's text field: copy, paste,
// undo and redo of the paste, then select all and cut. Each step reports the
// field's value and the pasteboard's text. With noMenu it removes the main menu first and only
// copies, to record what a page gets without one, then puts the menu back.
func menuEditScenario(noMenu bool) string {
	const text = "tuohi-menu-text"
	values := make(chan string, 4)
	loaded := make(chan struct{}, 1)
	w := &View{
		Width: 400, Height: 200,
		Bind: map[string]any{
			"value": func(s string) { values <- s },
			"loaded": func() {
				select {
				case loaded <- struct{}{}:
				default:
				}
			},
		},
	}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	result := make(chan string, 1)
	time.AfterFunc(60*time.Second, func() { w.Close() })
	go func() {
		defer w.Close()
		select {
		case <-loaded:
		case <-time.After(15 * time.Second):
			result <- "page never loaded"
			return
		}
		nw := native(w)
		var saved objc.ID
		if noMenu {
			performOnMain(func() {
				app := class("NSApplication").Send(sel("sharedApplication"))
				saved = app.Send(sel("mainMenu")).Send(sel("retain"))
				app.Send(sel("setMainMenu:"), objc.ID(0))
			})
			defer performOnMain(func() {
				class("NSApplication").Send(sel("sharedApplication")).Send(sel("setMainMenu:"), saved)
				saved.Send(sel("release"))
			})
		}
		w.Focus(true)
		performOnMain(func() {
			nw.window.Send(sel("makeFirstResponder:"), nw.webView)
			generalPasteboard().Send(sel("clearContents"))
		})
		value := func() string {
			w.Eval(`window.value(document.getElementById('f').value)`)
			select {
			case v := <-values:
				return v
			case <-time.After(5 * time.Second):
				return "?"
			}
		}
		// valueWait reads the field until it is want, for up to two
		// seconds: undo and redo reach the page asynchronously.
		valueWait := func(want string) string {
			v := ""
			for i := 0; i < 10; i++ {
				if v = value(); v == want {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
			return v
		}
		var states []string
		press := func(chars string, code uint16, mods uint) {
			states = append(states, chars+pressKey(nw.window, chars, code, nsEventModifierFlagCommand|mods))
			time.Sleep(300 * time.Millisecond) // the web process acts on it
		}
		defer func() { menuEditStates.Store(noMenu, strings.Join(states, " ")) }()
		w.Eval(`var f = document.getElementById('f'); f.focus(); f.select();`)
		time.Sleep(200 * time.Millisecond)
		press("c", keyC, 0)
		steps := []string{"copy=" + pasteboardWait(text)}
		if noMenu {
			result <- strings.Join(steps, " ")
			return
		}
		w.Eval(`var f = document.getElementById('f'); f.value = ''; f.focus();`)
		time.Sleep(200 * time.Millisecond)
		press("v", keyV, 0)
		steps = append(steps, "paste="+value())
		// Undo and redo the paste, the one undoable edit so far (the field was
		// emptied by script, which is not undoable). Synthesized presses are
		// not events, so AppKit may not close an undo group between two of
		// them, and an undo after paste and cut could revert both: GitHub run
		// 36833406011 showed an undo that ran (canRedo=true) and left the
		// field empty either way.
		time.Sleep(time.Second)
		press("z", keyZ, 0)
		steps = append(steps, "undo="+valueWait(""))
		press("Z", keyZ, nsEventModifierFlagShift)
		steps = append(steps, "redo="+valueWait(text))
		performOnMain(func() { generalPasteboard().Send(sel("clearContents")) })
		press("a", keyA, 0)
		press("x", keyX, 0)
		steps = append(steps, "cut="+value()+"/"+pasteboardWait(text))
		result <- strings.Join(steps, " ")
	}()

	native(w).loadHTML(`<!DOCTYPE html><html><body><input id="f" value="` + text + `">
<script>window.addEventListener('load', function(){ window.loaded(); });</script></body></html>`)
	w.w.Run()
	select {
	case r := <-result:
		return r
	default:
		return "no report"
	}
}

const nsEventModifierFlagShift = 1 << 17

func generalPasteboard() objc.ID {
	return class("NSPasteboard").Send(sel("generalPasteboard"))
}

// menuEditStates keeps, per noMenu, the state pressKey reported for each key
// of menuEditScenario, for the failure message.
var menuEditStates sync.Map

// pasteboardWait reads the pasteboard's text, waiting up to two seconds for
// it to be want: the web process answers copy: and cut: asynchronously. It
// returns the last text it read.
func pasteboardWait(want string) string {
	s := ""
	for i := 0; i < 20; i++ {
		if s = pasteboardText(); s == want {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return s
}

// pasteboardText reads the general pasteboard's text, "" when it has none.
func pasteboardText() string {
	var s string
	performOnMain(func() {
		if t := generalPasteboard().Send(sel("stringForType:"), nsstr("public.utf8-plain-text")); t != 0 {
			s = cstr(t.Send(sel("UTF8String")))
		}
	})
	return s
}

// TestMenuEditKeys checks that cut, copy, paste, select all, undo and redo
// work in a page's text field.
func TestMenuEditKeys(t *testing.T) {
	got, _ := resMenuEdit.Load().(string)
	requireGUI(t, got)
	want := "copy=tuohi-menu-text paste=tuohi-menu-text undo= redo=tuohi-menu-text cut=/tuohi-menu-text"
	if got != want {
		states, _ := menuEditStates.Load(false)
		t.Fatalf("Edit keys:\n got %s\nwant %s\nkeys %v", got, want, states)
	}
}

// TestCopyWithoutMainMenu records whether ⌘C copies from a page's text field
// when the app has no main menu, which is how tuohi apps ran before the
// default menu. It is a record, not a requirement.
func TestCopyWithoutMainMenu(t *testing.T) {
	got, _ := resMenuNoMenu.Load().(string)
	requireGUI(t, got)
	states, _ := menuEditStates.Load(true)
	t.Logf("⌘C with no main menu: %s (copied when it names the field's text); keys %v", got, states)
}
