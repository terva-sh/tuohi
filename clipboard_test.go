package tuohi

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/terva-sh/tuohi/clipboard"
)

var resClipboard atomic.Value // string

// clipboardText is what the clipboard scenario round-trips: non-ASCII text
// that a Latin-1 or byte-truncating path would mangle.
const clipboardText = "tuohi ✓ 日本語 — ünïcödé"

// clipboardScenario round-trips text through tuohi/clipboard. On GTK and
// macOS the package reaches the UI thread through the hook newView
// publishes; Windows needs none. It copies and pastes once on the UI
// thread before the loop runs, and once from another goroutine while the
// loop runs, which GTK and macOS marshal to the UI thread. The two texts
// differ, so the second paste cannot pass on the first copy.
func clipboardScenario() string {
	w := &View{Width: 300, Height: 200}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	roundTrip := func(text string) string {
		if err := clipboard.Copy(text); err != nil {
			return "copy error: " + err.Error()
		}
		got, err := clipboard.Paste()
		if err != nil {
			return "paste error: " + err.Error()
		}
		return got
	}
	onUI := roundTrip(clipboardText)

	result := make(chan string, 1)
	time.AfterFunc(15*time.Second, func() { w.Close() })
	go func() {
		defer w.Close()
		result <- roundTrip("off " + clipboardText)
	}()
	w.w.Run()
	select {
	case offUI := <-result:
		return fmt.Sprintf("ui=%s|off=%s", onUI, offUI)
	default:
		return fmt.Sprintf("ui=%s|off=no report", onUI)
	}
}

// TestClipboardRoundTrip checks that non-ASCII text survives Copy then Paste
// through the native clipboard, on and off the UI thread.
func TestClipboardRoundTrip(t *testing.T) {
	got, _ := resClipboard.Load().(string)
	requireGUI(t, got)
	want := fmt.Sprintf("ui=%s|off=off %s", clipboardText, clipboardText)
	if got != want {
		t.Fatalf("clipboard round trip:\n got %q\nwant %q", got, want)
	}
}
