package desktopentry

import (
	"slices"
	"testing"
)

// TestExec pins the written form of one argument: what the Desktop Entry
// Specification requires, escaped once by the Exec rules and once as a
// string value.
func TestExec(t *testing.T) {
	cases := []struct{ arg, want string }{
		{"/usr/bin/app", `/usr/bin/app`},
		{"日本語", `日本語`},
		{"", `""`},
		{"50%", `50%%`},
		{"%u", `%%u`},
		{"a b", `"a b"`},
		{"a\tb", `"a\tb"`},
		{"a\nb", `"a\nb"`},
		{`a"b`, `"a\\"b"`},
		{"a'b", `"a'b"`},
		{`a\b`, `"a\\\\b"`},
		{"a>b", `"a>b"`},
		{"a<b", `"a<b"`},
		{"~/x", `"~/x"`},
		{"a|b", `"a|b"`},
		{"a&b", `"a&b"`},
		{"a;b", `"a;b"`},
		{"$HOME", `"\\$HOME"`},
		{"a*", `"a*"`},
		{"a?", `"a?"`},
		{"#a", `"#a"`},
		{"(a)", `"(a)"`},
		{"a`b`", "\"a\\\\`b\\\\`\""},
		{"50% off", `"50%% off"`},
	}
	for _, c := range cases {
		if got := Exec(c.arg); got != c.want {
			t.Errorf("Exec(%q) = %s, want %s", c.arg, got, c.want)
		}
	}
	// Every reserved character forces quotes on its own.
	for _, r := range reserved {
		if got := Exec("x" + string(r)); got[0] != '"' {
			t.Errorf("Exec(%q) = %s, not quoted", "x"+string(r), got)
		}
	}
}

// TestSplitExecRoundTrip checks that SplitExec reads back exactly the
// arguments Exec wrote, for each hard case alone and all of them together.
func TestSplitExecRoundTrip(t *testing.T) {
	args := []string{
		"/opt/My App/bin/app", "日本語", "", "50%", "%u", "a\tb", "a\nb",
		`a"b`, "a'b", `a\b`, `a\\b`, "$HOME", "a`b`", "~/x", "a|b&c;d",
		"<in>", "a*?#()", `"`, `\`, "%%", "plain",
	}
	for _, a := range args {
		got, err := SplitExec(Exec(a))
		if err != nil || !slices.Equal(got, []string{a}) {
			t.Errorf("SplitExec(Exec(%q)) = %q, %v", a, got, err)
		}
	}
	got, err := SplitExec(Exec(args...))
	if err != nil || !slices.Equal(got, args) {
		t.Errorf("SplitExec(Exec(all)) = %q, %v, want %q", got, err, args)
	}
}

// TestSplitExec covers values written by hand rather than by Exec, and the
// unterminated quote it refuses.
func TestSplitExec(t *testing.T) {
	cases := []struct {
		value string
		want  []string
	}{
		{`/usr/bin/app %u`, []string{"/usr/bin/app", "%u"}},
		{`  app   --flag  `, []string{"app", "--flag"}},
		{`"/opt/My App/app" --x`, []string{"/opt/My App/app", "--x"}},
		{`app\sarg`, []string{"app", "arg"}},
		{`app "a\\\\b"`, []string{"app", `a\b`}},
	}
	for _, c := range cases {
		got, err := SplitExec(c.value)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("SplitExec(%s) = %q, %v, want %q", c.value, got, err, c.want)
		}
	}
	if _, err := SplitExec(`"unterminated`); err == nil {
		t.Error("SplitExec accepted an unterminated quote")
	}
}

// TestString checks the string-value escapes a Name value needs.
func TestString(t *testing.T) {
	if got, want := String("My\\App\nNext\tTab"), `My\\App\nNext\tTab`; got != want {
		t.Errorf("String = %s, want %s", got, want)
	}
	if got := String("Plain Name"); got != "Plain Name" {
		t.Errorf("String(plain) = %s", got)
	}
}
