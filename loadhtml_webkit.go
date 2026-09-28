//go:build !windows

package tuohi

// loadHTMLBase is the base URL the WebKit engines' test-only loadHTML gives
// its page. about:blank cannot be trusted (see originOf), so the page gets an
// origin of its own on a host that never resolves, which no other page can
// navigate to. WebView2's NavigateToString takes no base URL, so Windows
// needs another way when its sender check lands.
const loadHTMLBase = "http://loadhtml.tuohi.invalid/"
