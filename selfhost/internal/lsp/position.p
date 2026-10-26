// Translated by go2pu from internal/lsp/position.go; edited by hand since.
// net/url and unicode/utf16 have no Plutonium versions, so the parts of
// them the server uses are written out here.

package lsp

import (
	"pluto/path/filepath"
	"pluto/strings"
	"pluto/unicode/utf8"
	"selfhost/compat"
	"selfhost/pu"
)

// LSP positions count lines from 0 and characters in UTF-16 code units;
// the compiler counts byte offsets. A text converts between the two.
type text struct {
	src   []byte
	lines []int // byte offset of the start of each line
}

func newText(src []byte) *text {
	t := pu.New(text{src: src, lines: compat.Clone([]int{0})})
	for i, b := range src {
		if b == '\n' {
			t.lines = compat.AppendPlain(t.lines, i+1)
		}
	}
	return t
}

// offset returns the byte offset of p, clamped to the text.
func (t *text) offset(p Position) int {
	if p.Line < 0 {
		return 0
	}
	if p.Line >= len(t.lines) {
		return len(t.src)
	}
	off := t.lines[p.Line]
	end := len(t.src)
	if p.Line+1 < len(t.lines) {
		end = t.lines[p.Line+1] - 1 // the newline
	}
	for units := 0; off < end && units < p.Character; {
		r, size := utf8.DecodeRune(t.src[off:end])
		units += runeUnits(r, size)
		off += size
	}
	return off
}

// runeUnits returns the number of UTF-16 code units of a decoded rune; an
// invalid byte counts as one.
func runeUnits(r rune, size int) int {
	if r == utf8.RuneError && size == 1 {
		return 1
	}
	if r >= 0x10000 {
		return 2 // a surrogate pair
	}
	return 1
}

// position returns the LSP position of a byte offset.
func (t *text) position(off int) Position {
	off = min(max(off, 0), len(t.src))
	line := 0
	lo, hi := 0, len(t.lines)
	for lo < hi { // last line start <= off
		m := (lo + hi) / 2
		if t.lines[m] <= off {
			line, lo = m, m+1
		} else {
			hi = m
		}
	}
	units := 0
	for s := t.src[t.lines[line]:off]; len(s) > 0; {
		r, size := utf8.DecodeRune(s)
		units += runeUnits(r, size)
		s = s[size:]
	}
	return Position{Line: line, Character: units}
}

func (t *text) rangeOf(start, end int) Range {
	return Range{Start: t.position(start), End: t.position(end)}
}

// pathFromURI returns the file path of a file: URI, or "" for another
// scheme or a URI that does not parse. It follows net/url's Parse: the
// path is unescaped, and a URI whose rest does not start with a slash is
// opaque and taken as it is.
func pathFromURI(uri string) string {
	for i := 0; i < len(uri); i++ {
		if c := uri[i]; c < 0x20 || c == 0x7f {
			return "" // net/url: invalid control character in URL
		}
	}
	rest, frag, _ := strings.Cut(uri, "#")
	if _, bad := unescapeURI(frag); bad {
		return ""
	}
	scheme := ""
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' {
			continue
		}
		if i > 0 && ('0' <= c && c <= '9' || c == '+' || c == '-' || c == '.') {
			continue
		}
		if c == ':' {
			if i == 0 {
				return "" // missing protocol scheme
			}
			scheme = strings.ToLower(strings.Slice(rest, 0, i))
			rest = strings.SliceFrom(rest, i+1)
		}
		break
	}
	if scheme != "file" {
		return ""
	}
	if strings.HasSuffix(rest, "?") && strings.Count(rest, "?") == 1 {
		rest = strings.Slice(rest, 0, len(rest)-1)
	} else {
		rest, _, _ = strings.Cut(rest, "?")
	}
	if !strings.HasPrefix(rest, "/") {
		return filepath.Clean(rest) // opaque
	}
	if strings.HasPrefix(rest, "//") {
		authority := strings.SliceFrom(rest, 2)
		rest = ""
		if i := strings.Index(authority, "/"); i >= 0 {
			authority, rest = strings.Slice(authority, 0, i), strings.SliceFrom(authority, i)
		}
		if !validAuthority(authority) {
			return ""
		}
	}
	p, bad := unescapeURI(rest)
	if bad {
		return ""
	}
	return filepath.Clean(p)
}

// validAuthority reports whether net/url's Parse accepts the authority of
// a URI: escapes that decode, and a port of digits after the host.
func validAuthority(authority string) bool {
	host := authority
	if i := strings.LastIndex(authority, "@"); i >= 0 {
		if _, bad := unescapeURI(strings.Slice(authority, 0, i)); bad {
			return false
		}
		host = strings.SliceFrom(authority, i+1)
	}
	if strings.HasPrefix(host, "[") {
		end := strings.LastIndex(host, "]")
		if end < 0 {
			return false // missing ']' in host
		}
		return validPort(strings.SliceFrom(host, end+1))
	}
	if i := strings.LastIndex(host, ":"); i >= 0 {
		if !validPort(strings.SliceFrom(host, i)) {
			return false
		}
		host = strings.Slice(host, 0, i)
	}
	_, bad := unescapeURI(host)
	return !bad
}

// validPort reports whether port is empty or a colon and digits.
func validPort(port string) bool {
	if port == "" {
		return true
	}
	for i := 1; i < len(port); i++ {
		if port[i] < '0' || port[i] > '9' {
			return false
		}
	}
	return port[0] == ':'
}

// unescapeURI decodes the %XX escapes of s; bad reports an escape that is
// not two hex digits.
func unescapeURI(s string) (string, bool) {
	if strings.IndexByte(s, '%') < 0 {
		return s, false
	}
	b := compat.MakeCap[byte](0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '%' {
			b = compat.AppendPlain(b, c)
			continue
		}
		if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
			return "", true
		}
		b = compat.AppendPlain(b, unhex(s[i+1])<<4|unhex(s[i+2]))
		i += 2
	}
	return compat.String(b), false
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// uriFromPath returns the file: URI of an absolute path, as net/url's
// URL{Scheme: "file", Path: path}.String() writes it.
func uriFromPath(path string) string {
	if path == "" {
		return "file:"
	}
	const upperHex = "0123456789ABCDEF"
	b := compat.MakeCap[byte](0, len(path)+7)
	b = compat.AppendString(b, "file://")
	for i := 0; i < len(path); i++ {
		c := path[i]
		switch {
		case 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9':
		case c == '-' || c == '_' || c == '.' || c == '~':
		case c == '$' || c == '&' || c == '+' || c == ',' || c == '/' || c == ':' || c == ';' || c == '=' || c == '@':
		default:
			b = compat.AppendPlain(b, '%', upperHex[c>>4], upperHex[c&15])
			continue
		}
		b = compat.AppendPlain(b, c)
	}
	return compat.String(b)
}

// isIdentByte reports whether b may appear in an identifier (ASCII only;
// other letters are handled by the callers through the AST).
func isIdentByte(b byte) bool {
	return b == '_' || '0' <= b && b <= '9' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' || b >= utf8.RuneSelf
}

// wordEnd returns the end of the identifier or token starting at off, at
// least one byte past off unless off is at the end of src or a line.
func wordEnd(src []byte, off int) int {
	if off >= len(src) || src[off] == '\n' {
		return off
	}
	end := off
	for end < len(src) && isIdentByte(src[end]) {
		end++
	}
	if end == off {
		_, size := utf8.DecodeRune(src[off:])
		end = off + size
	}
	return end
}

// identBefore returns the start of the identifier that ends at off.
func identBefore(src []byte, off int) int {
	start := off
	for start > 0 && isIdentByte(src[start-1]) {
		start--
	}
	return start
}

// hasPathPrefix reports whether path is dir or lies inside it.
func hasPathPrefix(path, dir string) bool {
	if dir == "" {
		return false
	}
	return path == dir || strings.HasPrefix(path, compat.Concat(dir, "/"))
}
