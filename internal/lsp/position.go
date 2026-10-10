package lsp

import (
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// LSP positions count lines from 0 and characters in UTF-16 code units;
// the compiler counts byte offsets. A text converts between the two.
type text struct {
	src   []byte
	lines []int // byte offset of the start of each line
}

func newText(src []byte) *text {
	t := &text{src: src, lines: []int{0}}
	for i, b := range src {
		if b == '\n' {
			t.lines = append(t.lines, i+1)
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
	return utf16.RuneLen(r)
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
// scheme.
func pathFromURI(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	p := u.Path
	if p == "" {
		p = u.Opaque
	}
	return filepath.Clean(filepath.FromSlash(p))
}

// uriFromPath returns the file: URI of an absolute path.
func uriFromPath(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	return u.String()
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
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}
