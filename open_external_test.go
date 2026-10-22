package tuohi

import (
	"errors"
	"testing"
	"time"
)

// Opening a scope normally initializes a native toolkit. These tests commit
// only its configuration to exercise the shared URL policy without a display.
func committedOpenApp(a *App) *App {
	a.scopeOnce.Do(func() { a.scope = &appScope{cfg: snapshotConfig(a)} })
	return a
}

func TestOpenExternalScopeAndErrors(t *testing.T) {
	wantErr := errors.New("consumer opener failed")
	var first, second []string
	a := committedOpenApp(&App{OpenExternal: func(u string) error {
		first = append(first, u)
		return wantErr
	}})
	b := committedOpenApp(&App{OpenExternal: func(u string) error {
		second = append(second, u)
		return nil
	}})
	// Changing the public field after commitment must not change the opener.
	a.OpenExternal = func(string) error { t.Fatal("used uncommitted opener"); return nil }
	if err := a.Open("https://example.invalid/a"); !errors.Is(err, wantErr) {
		t.Fatalf("Open error = %v, want consumer error", err)
	}
	if err := b.Open("mailto:someone@example.invalid"); err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0] != "https://example.invalid/a" ||
		len(second) != 1 || second[0] != "mailto:someone@example.invalid" {
		t.Fatalf("cross-app handoffs: first=%v second=%v", first, second)
	}
	for _, raw := range []string{"file:///etc/passwd", "javascript:alert(1)", "example.invalid"} {
		if err := a.Open(raw); !errors.Is(err, ErrScheme) {
			t.Errorf("Open(%q) = %v, want ErrScheme", raw, err)
		}
	}
	if len(first) != 1 {
		t.Fatalf("disallowed URL reached consumer: %v", first)
	}
}

func TestOutsideHookPreservesNavigationPolicy(t *testing.T) {
	called := make(chan string, 4)
	blocked := make(chan struct{})
	defer close(blocked)
	w := &webview{}
	w.externalOpener = func(u string) error {
		called <- u
		<-blocked // the UI thread must not wait for the consumer
		return nil
	}
	w.trustURL("https://trusted.invalid/")
	w.openOutside("file:///etc/passwd")
	w.openOutside("https://trusted.invalid/page")
	w.openOutside("https://outside.invalid/link")
	select {
	case u := <-called:
		if u != "https://outside.invalid/link" {
			t.Fatalf("hook received %q", u)
		}
	case <-time.After(time.Second):
		t.Fatal("outside navigation did not reach the App opener")
	}
	select {
	case u := <-called:
		t.Fatalf("unexpected navigation handoff %q", u)
	default:
	}
}
