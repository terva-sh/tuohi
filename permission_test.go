package tuohi

import (
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPermits checks the one rule every engine's permission handler asks:
// granted only when the view lists every permission asked for and the page
// asking is on a trusted origin (TKT-01M3HWWRTVWVYSEDPRKSDPE783).
func TestPermits(t *testing.T) {
	c := &viewCore{
		origins:     map[string]bool{"http://127.0.0.1:8080": true},
		permissions: permissionSet([]Permission{PermissionCamera}),
	}
	cases := []struct {
		name      string
		requester string
		perms     []Permission
		want      bool
	}{
		{"listed, trusted", "http://127.0.0.1:8080/page", []Permission{PermissionCamera}, true},
		{"listed, trusted origin with no path", "http://127.0.0.1:8080", []Permission{PermissionCamera}, true},
		{"not listed", "http://127.0.0.1:8080/page", []Permission{PermissionMicrophone}, false},
		{"one of two not listed", "http://127.0.0.1:8080/page", []Permission{PermissionCamera, PermissionMicrophone}, false},
		{"untrusted origin", "http://127.0.0.1:9090/page", []Permission{PermissionCamera}, false},
		{"other scheme", "https://127.0.0.1:8080/page", []Permission{PermissionCamera}, false},
		{"no requester", "", []Permission{PermissionCamera}, false},
		{"about:blank", "about:blank", []Permission{PermissionCamera}, false},
		{"nothing asked", "http://127.0.0.1:8080/page", nil, false},
	}
	for _, tc := range cases {
		if got := c.permits(tc.requester, tc.perms...); got != tc.want {
			t.Errorf("%s: permits(%q, %v) = %v, want %v", tc.name, tc.requester, tc.perms, got, tc.want)
		}
	}
	if empty := (&viewCore{origins: c.origins}); empty.permits("http://127.0.0.1:8080/page", PermissionCamera) {
		t.Error("a view with no Permissions granted the camera")
	}
	if s := PermissionClipboard.String(); s != "clipboard" {
		t.Errorf("PermissionClipboard.String() = %q", s)
	}
	if s := Permission(9).String(); s != "permission(9)" {
		t.Errorf("Permission(9).String() = %q", s)
	}
}

var resPermissions atomic.Value // string

// permissionsScenario shows a trusted page three times: in a view that lists
// no permissions, in one that lists the camera, and in one that lists the
// clipboard. Each time the page asks for the camera and the microphone, pastes
// from script without a user gesture, and has a frame on another origin,
// delegated the camera with allow=, ask for the camera too. The capture
// devices are fakes (enableFakeCapture).
func permissionsScenario() string {
	var runs []string
	for _, perms := range [][]Permission{nil, {PermissionCamera}, {PermissionClipboard}} {
		name := "none"
		if len(perms) > 0 {
			name = perms[0].String()
		}
		runs = append(runs, name+": "+permissionsRun(perms))
	}
	return strings.Join(runs, "; ")
}

func permissionsRun(perms []Permission) string {
	const tryMedia = `async function tryMedia(c) {
  try {
    var s = await navigator.mediaDevices.getUserMedia(c);
    s.getTracks().forEach(function(t) { t.stop(); });
    return 'ok';
  } catch (e) {
    return e.name === 'NotAllowedError' ? 'denied' : e.name;
  }
}`
	other, closeOther, err := servePlain(func(target string) (string, string) {
		return "", `<!DOCTYPE html><html><body><script>` + tryMedia + `
tryMedia({video: true}).then(function(r) { parent.postMessage('frame=' + r, '*'); });
</script></body></html>`
	})
	if err != nil {
		return "listen error: " + err.Error()
	}
	defer closeOther()
	trusted, closeTrusted, err := servePlain(func(target string) (string, string) {
		return "", `<!DOCTYPE html><html><body><script>` + tryMedia + `
window.addEventListener('load', async function() {
  var out = [];
  out.push('video=' + await tryMedia({video: true}));
  out.push('audio=' + await tryMedia({audio: true}));
  var pasted = false;
  document.addEventListener('paste', function() { pasted = true; });
  var ran = false;
  try { ran = document.execCommand('paste'); } catch (e) {}
  out.push('paste=' + (ran && pasted));
  try { await navigator.clipboard.readText(); out.push('read=ok'); } catch (e) { out.push('read=' + (e.name === 'NotAllowedError' ? 'denied' : e.name)); }
  out.push('notify=' + (typeof Notification === 'undefined' ? 'none' : await Notification.requestPermission()));
  var frame = await new Promise(function(resolve) {
    window.addEventListener('message', function(e) { resolve(String(e.data)); });
    var f = document.createElement('iframe');
    f.setAttribute('allow', 'camera; microphone');
    f.src = '` + other + `frame';
    document.body.appendChild(f);
    setTimeout(function() { resolve('frame=timeout'); }, 10000);
  });
  out.push(frame);
  window.done(out.join(' '));
});
</script></body></html>`
	})
	if err != nil {
		return "listen error: " + err.Error()
	}
	defer closeTrusted()

	var askedMu sync.Mutex
	var asked []string
	permissionDecided = func(_ string, ps []Permission, granted bool) {
		names := make([]string, len(ps))
		for i, p := range ps {
			names[i] = p.String()
		}
		verdict := "no"
		if granted {
			verdict = "yes"
		}
		askedMu.Lock()
		asked = append(asked, strings.Join(names, "+")+":"+verdict)
		askedMu.Unlock()
	}
	defer func() { permissionDecided = nil }()
	w := &View{Permissions: perms}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()
	enableFakeCapture(w.w)
	res := make(chan string, 1)
	_ = w.w.Bind("done", func(s string) { res <- s })
	time.AfterFunc(45*time.Second, func() { w.Close() })
	result := make(chan string, 1)
	go func() {
		defer w.Close()
		select {
		case r := <-res:
			askedMu.Lock()
			sort.Strings(asked)
			r += " asked=" + strings.Join(asked, ",")
			askedMu.Unlock()
			result <- r
		case <-time.After(40 * time.Second):
			result <- "no report"
		}
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

// TestPermissions checks, on every engine, that a page gets the camera, the
// microphone, and script access to the clipboard only when the view lists
// them (TKT-01M3HWWRTVWVYSEDPRKSDPE783).
//
// Engines differ where their APIs do. WebKitGTK names no frame, so a frame the
// trusted page delegated the camera to shares its grant; WebView2 and WKWebView
// decide the frame's own origin. Script paste without a gesture exists only on
// WebKitGTK. macOS never grants in the test: WebKit would then open a capture
// device, and a test binary without camera usage strings is killed by TCC.
func TestPermissions(t *testing.T) {
	got, _ := resPermissions.Load().(string)
	requireGUI(t, got)
	var want string
	switch runtime.GOOS {
	case "windows":
		// No capture devices on the runner: Chromium fails the capture
		// before it asks, and tuohi cannot give WebView2 fake ones (it loads
		// the runtime without the loader that reads browser arguments). The
		// clipboard read and the notification reach the handler.
		want = "none: video=NotFoundError audio=NotFoundError paste=false read=denied notify=denied frame=NotFoundError asked=clipboard:no; " +
			"camera: video=NotFoundError audio=NotFoundError paste=false read=denied notify=denied frame=NotFoundError asked=clipboard:no; " +
			"clipboard: video=NotFoundError audio=NotFoundError paste=false read=ok notify=denied frame=NotFoundError asked=clipboard:yes"
	case "darwin":
		// With no camera on the runner, WebKit rejects video before it asks;
		// the microphone request reaches the delegate and is denied.
		want = "none: video=OverconstrainedError audio=denied paste=false read=denied notify=denied frame=OverconstrainedError asked=microphone:no"
		got, _, _ = strings.Cut(got, ";")
	default:
		want = "none: video=denied audio=denied paste=false read=denied notify=denied frame=denied asked=camera:no,camera:no,microphone:no; " +
			"camera: video=ok audio=denied paste=false read=denied notify=denied frame=ok asked=camera:yes,camera:yes,microphone:no; " +
			"clipboard: video=denied audio=denied paste=true read=denied notify=denied frame=denied asked=camera:no,camera:no,microphone:no"
	}
	if got != want {
		t.Fatalf("permissions:\n got %s\nwant %s", got, want)
	}
}
