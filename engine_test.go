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
		"about:blank":                      "about:blank",
		"about:blank#top":                  "about:blank",
		"":                                 "",
		"/relative/path":                   "",
		"example.com":                      "",
		"http://%zz":                       "",
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
		"http://127.0.0.1:8081/", // another port is another origin
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
