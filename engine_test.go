package tuohi

import "testing"

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

// TestOpaqueKeyAgreesAcrossEngines checks that every engine's spelling of one
// data: URL gets one key. The spellings follow what each engine reported:
// WebKitGTK 2.52 in a probe, WebView2 in GitHub run 36380534759, and the fully
// percent-encoded form NSURL may produce.
func TestOpaqueKeyAgreesAcrossEngines(t *testing.T) {
	spellings := []string{
		"data:text/html,<p>a b</p>\n<i>'r'</i>?x y&k='z'",                              // as Go wrote it
		"data:text/html,<p>a b</p><i>'r'</i>?x%20y&k=%27z%27",                          // WebKitGTK
		"data:text/html,<p>a b</p>%0A<i>'r'</i>?x%20y&k=%27z%27",                       // WebView2
		"data:text/html,%3Cp%3Ea%20b%3C/p%3E%0A%3Ci%3E%27r%27%3C/i%3E?x%20y&k=%27z%27", // fully encoded
	}
	want := originOf(spellings[0])
	for _, s := range spellings[1:] {
		if got := originOf(s); got != want {
			t.Errorf("originOf(%q) = %q, want %q", s, got, want)
		}
	}
}
