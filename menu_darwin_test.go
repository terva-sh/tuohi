//go:build darwin

package tuohi

import (
	"fmt"
	"sort"
	"strings"
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

// pressKey sends a key-down event for chars with the given modifiers to win
// through NSApp, on the main thread.
func pressKey(win objc.ID, chars string, code uint16, mods uint) {
	performOnMain(func() {
		autorelease(func() {
			ev := class("NSEvent").Send(
				sel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
				uint(nsEventTypeKeyDown), cgPoint{0, 0}, mods, float64(0),
				objc.Send[int](win, sel("windowNumber")), objc.ID(0),
				nsstr(chars), nsstr(chars), false, code)
			class("NSApplication").Send(sel("sharedApplication")).Send(sel("sendEvent:"), ev)
		})
	})
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
	w := &View{Frame: framed, Width: 320, Height: 240}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()
	var hung atomic.Bool
	time.AfterFunc(15*time.Second, func() { hung.Store(true); w.Close() })
	go func() {
		loopUp := make(chan struct{})
		ui.run(func() { close(loopUp) })
		<-loopUp
		w.Focus(true)
		pressKey(native(w).window, "w", keyW, nsEventModifierFlagCommand)
	}()
	native(w).loadHTML(`<!DOCTYPE html><html><body>close me</body></html>`)
	w.w.Run()
	if hung.Load() {
		return "not closed"
	}
	return "closed"
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
// select all and cut, undo, and redo. Each step reports the field's value and
// the pasteboard's text. With noMenu it removes the main menu first and only
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
		press := func(chars string, code uint16, mods uint) {
			pressKey(nw.window, chars, code, nsEventModifierFlagCommand|mods)
			time.Sleep(300 * time.Millisecond) // the web process acts on it
		}
		w.Eval(`var f = document.getElementById('f'); f.focus(); f.select();`)
		time.Sleep(200 * time.Millisecond)
		press("c", keyC, 0)
		steps := []string{"copy=" + pasteboardText()}
		if noMenu {
			result <- strings.Join(steps, " ")
			return
		}
		w.Eval(`var f = document.getElementById('f'); f.value = ''; f.focus();`)
		time.Sleep(200 * time.Millisecond)
		press("v", keyV, 0)
		steps = append(steps, "paste="+value())
		performOnMain(func() { generalPasteboard().Send(sel("clearContents")) })
		press("a", keyA, 0)
		press("x", keyX, 0)
		steps = append(steps, "cut="+value()+"/"+pasteboardText())
		press("z", keyZ, 0)
		steps = append(steps, "undo="+value())
		press("Z", keyZ, nsEventModifierFlagShift)
		steps = append(steps, "redo="+value())
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
	want := "copy=tuohi-menu-text paste=tuohi-menu-text cut=/tuohi-menu-text undo=tuohi-menu-text redo="
	if got != want {
		t.Fatalf("Edit keys:\n got %s\nwant %s", got, want)
	}
}

// TestCopyWithoutMainMenu records whether ⌘C copies from a page's text field
// when the app has no main menu, which is how tuohi apps ran before the
// default menu. It is a record, not a requirement.
func TestCopyWithoutMainMenu(t *testing.T) {
	got, _ := resMenuNoMenu.Load().(string)
	requireGUI(t, got)
	t.Logf("⌘C with no main menu: %s (copied when it names the field's text)", got)
}
