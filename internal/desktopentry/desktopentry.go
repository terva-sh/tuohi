// Package desktopentry writes and reads values of a freedesktop.org Desktop
// Entry file, for the two places tuohi writes one: the GTK3 Wayland identity
// in the root package and the XDG backend of tuohi/autostart.
//
// An Exec value is escaped twice, as the Desktop Entry Specification says.
// Each argument is first quoted by the Exec rules: a literal percent sign is
// doubled, and an argument holding a reserved character is put in double
// quotes, inside which a double quote, backtick, dollar sign or backslash is
// escaped with a backslash. The whole value is then escaped as a string
// value, which doubles every backslash again, so one literal backslash in an
// argument is written as four.
package desktopentry

import (
	"errors"
	"strings"
)

// reserved holds the characters that make the Desktop Entry Specification
// require an Exec argument to be quoted.
const reserved = " \t\n\"'\\><~|&;$*?#()`"

// Exec renders args as the value of an Exec key. An empty argument is written
// as "" so it survives.
func Exec(args ...string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = quoteArg(a)
	}
	return String(strings.Join(quoted, " "))
}

// quoteArg applies the Exec quoting rules to one argument.
func quoteArg(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	if s != "" && !strings.ContainsAny(s, reserved) {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '`', '$', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// stringEscaper applies the escapes of a string value: a backslash, and the
// control characters a value may not hold raw.
var stringEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`, "\r", `\r`)

// String escapes s as a string value, such as the value of a Name key.
func String(s string) string {
	return stringEscaper.Replace(s)
}

// errUnterminated is returned by SplitExec for a quote that is never closed.
var errUnterminated = errors.New("desktopentry: unterminated quote in Exec value")

// SplitExec parses the value of an Exec key into its arguments, undoing Exec.
// Field codes such as %u are kept as written; only %% becomes %.
func SplitExec(value string) ([]string, error) {
	s := unescapeString(value)
	var (
		args    []string
		cur     strings.Builder
		inArg   bool
		inQuote bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote && c == '\\' && i+1 < len(s) && strings.IndexByte("\"`$\\", s[i+1]) >= 0:
			i++
			cur.WriteByte(s[i])
		case inQuote && c == '"':
			inQuote = false
		case inQuote:
			cur.WriteByte(c)
		case c == '"':
			inQuote, inArg = true, true
		case c == ' ' || c == '\t' || c == '\n':
			if inArg {
				args = append(args, strings.ReplaceAll(cur.String(), "%%", "%"))
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteByte(c)
			inArg = true
		}
	}
	if inQuote {
		return nil, errUnterminated
	}
	if inArg {
		args = append(args, strings.ReplaceAll(cur.String(), "%%", "%"))
	}
	return args, nil
}

// unescapeString undoes the escapes of a string value. A backslash before any
// other character is kept, with the character.
func unescapeString(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case '\\':
			b.WriteByte('\\')
		case 's':
			b.WriteByte(' ')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
