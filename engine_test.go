package tuohi

import (
	"strings"
	"testing"
)

func TestOriginOf(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:8080/app?x=1#f":  "http://127.0.0.1:8080",
		"HTTP://LocalHost:41234/":          "http://localhost:41234",
		"http://example.com:80/a":          "http://example.com",
		"https://example.com:443/":         "https://example.com",
		"https://example.com:8443/":        "https://example.com:8443",
		"http://[::1]:9000/":               "http://[::1]:9000",
		"http://[::1]/":                    "http://[::1]",
		"app://localhost/index.html":       "app://localhost",
		"https://app.localhost/index.html": "https://app.localhost",
		"about:blank":                      "",
		"about:srcdoc":                     "",
		"data:text/html,<p>hi</p>#x":       "data:text/html,<p>hi</p>",
		"about:blank#top":                  "",
		"http://%65xample.com/":            "http://example.com",
		"http://user@ex%41mple.com:8080/a": "http://example.com:8080",
		"http://%2f.example/":              "",
		"":                                 "",
		"/relative/path":                   "",
		"example.com":                      "",
		"http://%zz":                       "",
		// Canonicalized the way a browser reports the URL.
		"http://127.0.0.1:080/":          "http://127.0.0.1",
		"http://127.0.0.1:08080/":        "http://127.0.0.1:8080",
		"http://127.1:8080/":             "http://127.0.0.1:8080",
		"http://0x7f.0.0.1:8080/":        "http://127.0.0.1:8080",
		"http://2130706433:8080/":        "http://127.0.0.1:8080",
		"http://0177.0.0.1:8080/":        "http://127.0.0.1:8080",
		"http://[0:0:0:0:0:0:0:1]:9000/": "http://[::1]:9000",
		"http://B\u00fccher.example/":    "http://xn--bcher-kva.example",
		"http://example.com:99999/":      "",
		"http://256.0.0.1/":              "",
		"http://1.2.3.4.5/":              "",
		"http://example.123/":            "",
		"http://[::ffff:127.0.0.1]/":     "http://[::ffff:7f00:1]",
		"http://[2001:DB8:0:0:1:0:0:1]/": "http://[2001:db8::1:0:0:1]",
		"http://[1:0:0:2:0:0:0:3]/":      "http://[1:0:0:2::3]",
		"http://[::]/":                   "http://[::]",
		"http://[1:2:3:4:5:6:7:8]/":      "http://[1:2:3:4:5:6:7:8]",
		"http://[1::]/":                  "http://[1::]",
		"http://１２７.１:8080/":             "http://127.0.0.1:8080",
		"http://1.example/":              "http://1.example",
		// Opaque-path URLs, in the canonical form: escapes decoded, tabs and
		// newlines dropped, spaces, controls, '%' and non-ASCII encoded.
		"data:text/html,<p>a b</p>\t<i>\"q\" 'r'</i>?x y<z>é&k=%41#frag": `data:text/html,<p>a%20b</p><i>"q"%20'r'</i>?x%20y<z>%C3%A9&k=A`,
		"  data:text/html,a\nb\r\n  ":                                    "data:text/html,ab",
		"DATA:text/html,x":                                               "data:text/html,x",
		"data:text/html,100%":                                            "data:text/html,100%25",
		"data:text/html,%zz":                                             "data:text/html,%25zz",
		"data:text/html,é":                                               "data:text/html,%C3%A9",
		"About:blank":                                                    "",
		"mailto:a@b.invalid":                                             "mailto:a@b.invalid",
	}
	for in, want := range cases {
		if got := originOf(in); got != want {
			t.Errorf("originOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestViewCoreTrusts(t *testing.T) {
	var c viewCore
	c.trustURL("http://127.0.0.1:8080/start")
	c.trustURL("not a url")

	allowed := []string{
		"http://127.0.0.1:8080/other/page",
		"HTTP://127.0.0.1:8080",
	}
	for _, u := range allowed {
		if !c.trusts(u, true) {
			t.Errorf("trusts(%q) = false, want true", u)
		}
	}
	refused := []string{
		"http://127.0.0.1:8081/",          // another port is another origin
		"http://[::ffff:127.0.0.1]:8080/", // IPv4-mapped IPv6 is not the IPv4 origin
		"https://127.0.0.1:8080/",
		"http://localhost:8080/", // same machine, different origin
		"about:blank",
		"",
		"not a url",
	}
	for _, u := range refused {
		if c.trusts(u, true) {
			t.Errorf("trusts(%q) = true, want false", u)
		}
	}
	// An engine that cannot name the sender is not gated.
	if !c.trusts("", false) {
		t.Error("trusts with an unknown sender = false, want true")
	}
}

func TestNavigationPolicy_Decisions(t *testing.T) {
	var c viewCore
	c.trustURL("http://127.0.0.1:8080/start")
	c.trustURL("data:text/html,<p>app</p>")
	cases := map[string]navAction{
		"http://127.0.0.1:8080/other":    navProceed,
		"HTTP://127.0.0.1:8080/x?y#z":    navProceed,
		"data:text/html,<p>app</p>#frag": navProceed, // Go's own data: page
		"about:blank":                    navProceed,
		"ABOUT:BLANK#top":                navProceed,
		"http://127.0.0.1:8081/":         navExternal,
		"https://example.com/login":      navExternal,
		"mailto:someone@example.invalid": navExternal,
		"about:srcdoc":                   navCancel,
		"data:text/html,<p>other</p>":    navCancel,
		"blob:http://127.0.0.1:8080/abc": navCancel,
		"file:///etc/passwd":             navCancel,
		"vscode://file/x":                navCancel,
		"javascript:alert(1)":            navCancel,
		"not a url":                      navCancel,
	}
	for u, want := range cases {
		if got := c.navigationPolicy(u); got != want {
			t.Errorf("navigationPolicy(%q) = %d, want %d", u, got, want)
		}
	}
}

// TestOpaqueKeyAgreesAcrossEngines checks that every engine's spelling of the
// URL Navigate loads gets one key. The spellings follow what each engine
// reported: WebKitGTK 2.52 in a probe, WebView2 in GitHub run 36380534759,
// and the fully percent-encoded form NSURL may produce.
func TestOpaqueKeyAgreesAcrossEngines(t *testing.T) {
	loaded := canonicalNavigateURL("data:text/html,<p>a b</p>\n<i>'r'</i>?x y&k='z'")
	if strings.ContainsAny(loaded, "\t\n\r") {
		t.Fatalf("canonicalNavigateURL left raw whitespace: %q", loaded)
	}
	want := originOf(loaded)
	for _, s := range []string{
		"data:text/html,<p>a b</p>%0A<i>'r'</i>?x%20y&k=%27z%27",                       // WebKitGTK and WebView2
		"data:text/html,%3Cp%3Ea%20b%3C/p%3E%0A%3Ci%3E%27r%27%3C/i%3E?x%20y&k=%27z%27", // fully encoded
	} {
		if got := originOf(s); got != want {
			t.Errorf("originOf(%q) = %q, want %q", s, got, want)
		}
	}
}

// TestOpaqueKeyEncodesQueryPunctuation checks that another scheme's query is
// keyed as a browser serializes it: the URL standard's query percent-encode
// set encodes '"', '<' and '>' there, but not in the path. terva-review
// finding on PR #17.
func TestOpaqueKeyEncodesQueryPunctuation(t *testing.T) {
	want := "mailto:<a>@b.invalid?subject=%3Cx%3E%20%22q%22&t='s'"
	for _, s := range []string{
		"mailto:<a>@b.invalid?subject=<x> \"q\"&t='s'",
		"mailto:<a>@b.invalid?subject=%3Cx%3E%20%22q%22&t='s'",
	} {
		if got := originOf(s); got != want {
			t.Errorf("originOf(%q) = %q, want %q", s, got, want)
		}
	}
}

// TestOpaqueKeyKeepsDataMetadata checks that a data: URL's metadata, before
// its first raw comma, is never decoded: %2C there is not the comma that ends
// it, so decoding it moves where the body starts. Nor does decoding a body
// ever start a fragment. terva-review finding on PR #17.
func TestOpaqueKeyKeepsDataMetadata(t *testing.T) {
	if originOf("data:text/plain%2Cfoo,bar") == originOf("data:text/plain,foo,bar") {
		t.Error("an escaped comma in the metadata shares a key with a raw one")
	}
	for in, want := range map[string]string{
		"data:text/plain%2Cfoo,bar":      "data:text/plain%2Cfoo,bar",
		"data:text/html,a%23b#frag":      "data:text/html,a%23b#frag",
		"data:text/html;charset=a b,x y": "data:text/html;charset=a%20b,x%20y",
		"mailto:a%2Cb@x.invalid":         "mailto:a%2Cb@x.invalid",
	} {
		if got := canonicalNavigateURL(in); got != want {
			t.Errorf("canonicalNavigateURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestOpaqueKeyKeepsEscapedNewlines checks that an escaped newline is content:
// two data: URLs that differ only by one are different documents, and must
// not share a key. terva-review finding on PR #17.
func TestOpaqueKeyKeepsEscapedNewlines(t *testing.T) {
	for _, pair := range [][2]string{
		{"data:text/html,ab%0Acd", "data:text/html,abcd"},
		{"data:text/html,a%09b", "data:text/html,ab"},
		{"data:text/html,a%0D%0Ab", "data:text/html,ab"},
	} {
		if originOf(pair[0]) == originOf(pair[1]) {
			t.Errorf("originOf(%q) == originOf(%q) = %q", pair[0], pair[1], originOf(pair[0]))
		}
	}
	// A raw newline is stripped before parsing, as a browser does, so it is
	// not content; Navigate encodes it instead of passing it on.
	if originOf("data:text/html,ab\ncd") != originOf("data:text/html,abcd") {
		t.Error("a raw newline changed the key")
	}
	if got, want := canonicalNavigateURL(" data:text/html,a\nb#f "), "data:text/html,a%0Ab#f"; got != want {
		t.Errorf("canonicalNavigateURL = %q, want %q", got, want)
	}
	if got := canonicalNavigateURL("http://x/a b"); got != "http://x/a b" {
		t.Errorf("canonicalNavigateURL changed a URL with a host: %q", got)
	}
}
