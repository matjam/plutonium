// Package filepath manipulates slash-separated file paths, as Go's
// path/filepath does on Unix.
//
// Functions whose result is part of their argument (Base, Ext, Split)
// return substrings of it. Join, Clean, Dir, Abs and Rel return strings as
// Go's do, allocated from the current allocator only when the result is
// not already a substring of an argument; their Append forms (AppendJoin,
// AppendClean, AppendDir, AppendAbs, AppendRel) write into a caller's
// buffer and never allocate, for kernel code and hot paths.
package filepath

import (
	"pluto/errors"
	"pluto/os"
	"pluto/unicode/utf8"
	"unsafe"
)

// Separator and ListSeparator are the path and path-list separators.
const (
	Separator     = '/'
	ListSeparator = ':'
)

// ErrBadPattern reports a malformed Match pattern.
var ErrBadPattern = errors.New("syntax error in pattern")

// errRel is reported by AppendRel for paths it cannot relate.
var errRel = errors.New("Rel: can't make target relative to base")

// IsAbs reports whether path is absolute.
func IsAbs(path string) bool { return len(path) > 0 && path[0] == '/' }

// VolumeName returns "": Unix paths have no volume.
func VolumeName(path string) string { return "" }

// ToSlash returns path: the separator is already a slash.
func ToSlash(path string) string { return path }

// FromSlash returns path: the separator is already a slash.
func FromSlash(path string) string { return path }

// Base returns the last element of path, without trailing slashes: "." for
// an empty path and "/" for a path of slashes only.
func Base(path string) string {
	if path == "" {
		return "."
	}
	for len(path) > 0 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	i := len(path) - 1
	for i >= 0 && path[i] != '/' {
		i--
	}
	if i >= 0 {
		path = path[i+1:]
	}
	if path == "" {
		return "/"
	}
	return path
}

// Ext returns the extension of the last element of path: the suffix from
// its final dot, or "".
func Ext(path string) string {
	for i := len(path) - 1; i >= 0 && path[i] != '/'; i-- {
		if path[i] == '.' {
			return path[i:]
		}
	}
	return ""
}

// Split splits path after its final slash into a directory part and a
// file name; either may be empty. dir+file == path.
func Split(path string) (dir, file string) {
	i := len(path) - 1
	for i >= 0 && path[i] != '/' {
		i--
	}
	return path[:i+1], path[i+1:]
}

// Dir returns all but the last element of path, cleaned, as Go's Dir does:
// "." if there is none. For a path whose directory part is already clean
// it is a substring of path and allocates nothing.
//
//pu:alloc
func Dir(path string) string {
	dir, _ := Split(path)
	return Clean(dir)
}

// AppendDir appends Go's Dir of path, the cleaned directory part, to dst.
func AppendDir(dst []byte, path string) []byte {
	dir, _ := Split(path)
	return AppendClean(dst, dir)
}

// AppendClean appends the shortest path equivalent to path by lexical
// processing (Go's Clean) to dst: it removes repeated slashes, "."
// elements, and ".." elements with the element before them, and ".."
// right after the root; an empty result is ".".
func AppendClean(dst []byte, path string) []byte {
	return appendClean(dst, strBytes(path))
}

// strBytes views a string's bytes. The caller must not modify them.
func strBytes(s string) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// appendClean is AppendClean for a path that is bytes. Cleaning looks only
// at '/' and '.', so it works on any bytes, valid UTF-8 or not, and can be
// done in place: it never writes ahead of what it reads.
func appendClean(dst []byte, path []byte) []byte {
	start := len(dst)
	if len(path) == 0 {
		return append(dst, '.')
	}
	rooted := path[0] == '/'
	n := len(path)
	r, dotdot := 0, 0 // read index; where ".." may no longer back up
	if rooted {
		dst = append(dst, '/')
		r, dotdot = 1, 1
	}
	for r < n {
		switch {
		case path[r] == '/':
			r++ // empty element
		case path[r] == '.' && (r+1 == n || path[r+1] == '/'):
			r++ // "."
		case path[r] == '.' && path[r+1] == '.' && (r+2 == n || path[r+2] == '/'):
			r += 2 // ".."
			w := len(dst) - start
			switch {
			case w > dotdot:
				// Back up to the previous separator.
				w--
				for w > dotdot && dst[start+w] != '/' {
					w--
				}
				dst = dst[:start+w]
			case !rooted:
				// Cannot back up: keep the "..".
				if w > 0 {
					dst = append(dst, '/')
				}
				dst = append(dst, '.', '.')
				dotdot = len(dst) - start
			}
		default:
			// A real element: add a separator if needed, then copy it.
			if rooted && len(dst)-start != 1 || !rooted && len(dst)-start != 0 {
				dst = append(dst, '/')
			}
			for ; r < n && path[r] != '/'; r++ {
				dst = append(dst, path[r])
			}
		}
	}
	if len(dst) == start {
		dst = append(dst, '.')
	}
	return dst
}

// AppendJoin joins the non-empty elements with slashes, cleans the result
// (as Go's Join does) and appends it to dst. With no non-empty element it
// appends nothing.
func AppendJoin(dst []byte, elem ...string) []byte {
	var buf [4096]byte
	joined := buf[:0]
	for _, e := range elem {
		if e == "" {
			continue
		}
		if len(joined) > 0 {
			if len(joined) == cap(joined) {
				return appendJoinSlow(dst, elem)
			}
			joined = append(joined, '/')
		}
		if len(joined)+len(e) > cap(joined) {
			return appendJoinSlow(dst, elem)
		}
		joined = append(joined, e...)
	}
	if len(joined) == 0 {
		return dst
	}
	// The elements are strings, valid UTF-8, joined by slashes: valid.
	return AppendClean(dst, unsafe.String(&joined[0], len(joined)))
}

// appendJoinSlow joins elements too long for AppendJoin's buffer by
// cleaning each element's contribution in turn, at the end of dst.
func appendJoinSlow(dst []byte, elem []string) []byte {
	start := len(dst)
	for _, e := range elem {
		if e == "" {
			continue
		}
		if len(dst) > start {
			dst = append(dst, '/')
		}
		dst = append(dst, e...)
	}
	// Clean in place: AppendClean never writes ahead of what it reads, so
	// cleaning the joined text onto its own start is safe.
	return appendClean(dst[:start], dst[start:])
}

// AppendAbs appends an absolute, clean form of path to dst: path joined to
// the working directory if it is relative. The working directory is taken
// as its exact bytes (os.AppendWd), so the result is exact even if the
// directory's name is not valid UTF-8.
func AppendAbs(dst []byte, path string) ([]byte, error) {
	if IsAbs(path) {
		return AppendClean(dst, path), nil
	}
	var wd [4096]byte
	cwd, err := os.AppendWd(wd[:0])
	if err != nil {
		return dst, err
	}
	// Build "cwd/path" at the end of dst, then clean it in place.
	start := len(dst)
	dst = append(dst, cwd...)
	dst = append(dst, '/')
	dst = append(dst, path...)
	return appendClean(dst[:start], dst[start:]), nil
}

// AppendRel appends a relative path that is lexically equivalent to targ
// when joined to base (Go's Rel) to dst. It fails if targ cannot be made
// relative to base, such as when one is absolute and the other is not.
func AppendRel(dst []byte, basepath, targpath string) ([]byte, error) {
	var bb, tb [4096]byte
	baseb := AppendClean(bb[:0], basepath)
	targb := AppendClean(tb[:0], targpath)
	// Cleaned copies of strings, valid UTF-8 (cleaning only drops whole
	// elements and slashes).
	base := unsafe.String(&baseb[0], len(baseb))
	targ := unsafe.String(&targb[0], len(targb))
	if targ == base {
		return append(dst, '.'), nil
	}
	if base == "." {
		base = ""
	}
	baseSlashed := len(base) > 0 && base[0] == '/'
	targSlashed := len(targ) > 0 && targ[0] == '/'
	if baseSlashed != targSlashed {
		return dst, errRel
	}
	bl := len(base)
	tl := len(targ)
	var b0, bi, t0, ti int
	for {
		for bi < bl && base[bi] != '/' {
			bi++
		}
		for ti < tl && targ[ti] != '/' {
			ti++
		}
		if targ[t0:ti] != base[b0:bi] {
			break
		}
		if bi < bl {
			bi++
		}
		if ti < tl {
			ti++
		}
		b0 = bi
		t0 = ti
	}
	if base[b0:bi] == ".." {
		return dst, errRel
	}
	if b0 != bl {
		seps := 0
		for i := b0; i < bl; i++ {
			if base[i] == '/' {
				seps++
			}
		}
		dst = append(dst, '.', '.')
		for i := 0; i < seps; i++ {
			dst = append(dst, '/', '.', '.')
		}
		if t0 != tl {
			dst = append(dst, '/')
			dst = append(dst, targ[t0:]...)
		}
		return dst, nil
	}
	return append(dst, targ[t0:]...), nil
}

// Match reports whether name matches the shell pattern (Go's Match):
// '*' matches any sequence of non-slash characters, '?' one non-slash
// character, '[...]' a character class (with ranges and '^' negation), and
// '\\' escapes. The only error is ErrBadPattern.
func Match(pattern, name string) (matched bool, err error) {
Pattern:
	for len(pattern) > 0 {
		var star bool
		var chunk string
		star, chunk, pattern = scanChunk(pattern)
		if star && chunk == "" {
			// Trailing * matches the rest unless it has a slash.
			for i := 0; i < len(name); i++ {
				if name[i] == '/' {
					return false, nil
				}
			}
			return true, nil
		}
		t, ok, err := matchChunk(chunk, name)
		if ok && (len(t) == 0 || len(pattern) > 0) {
			name = t
			continue
		}
		if err != nil {
			return false, err
		}
		if star {
			// Look for a match skipping i+1 bytes; no slashes.
			for i := 0; i < len(name) && name[i] != '/'; i++ {
				t, ok, err := matchChunk(chunk, name[i+1:])
				if ok {
					if len(pattern) == 0 && len(t) > 0 {
						continue
					}
					name = t
					continue Pattern
				}
				if err != nil {
					return false, err
				}
			}
		}
		// Check the rest of the pattern for syntax errors before
		// failing.
		for len(pattern) > 0 {
			_, chunk, pattern = scanChunk(pattern)
			if _, _, err := matchChunk(chunk, ""); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	return len(name) == 0, nil
}

// scanChunk gets the next segment of pattern: a non-star part, possibly
// preceded by a star.
func scanChunk(pattern string) (star bool, chunk, rest string) {
	for len(pattern) > 0 && pattern[0] == '*' {
		pattern = pattern[1:]
		star = true
	}
	inrange := false
	var i int
Scan:
	for i = 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			if i+1 < len(pattern) {
				i++
			}
		case '[':
			inrange = true
		case ']':
			inrange = false
		case '*':
			if !inrange {
				break Scan
			}
		}
	}
	return star, pattern[0:i], pattern[i:]
}

// matchChunk checks whether chunk matches the beginning of s and returns
// the remainder of s.
func matchChunk(chunk, s string) (rest string, ok bool, err error) {
	failed := false
	for len(chunk) > 0 {
		if !failed && len(s) == 0 {
			failed = true
		}
		switch chunk[0] {
		case '[':
			var r rune
			if !failed {
				var n int
				r, n = decodeRune(s)
				s = s[n:]
			}
			chunk = chunk[1:]
			negated := false
			if len(chunk) > 0 && chunk[0] == '^' {
				negated = true
				chunk = chunk[1:]
			}
			match := false
			nrange := 0
			for {
				if len(chunk) > 0 && chunk[0] == ']' && nrange > 0 {
					chunk = chunk[1:]
					break
				}
				var lo, hi rune
				if lo, chunk, err = getEsc(chunk); err != nil {
					return "", false, err
				}
				hi = lo
				if chunk[0] == '-' {
					if hi, chunk, err = getEsc(chunk[1:]); err != nil {
						return "", false, err
					}
				}
				if lo <= r && r <= hi {
					match = true
				}
				nrange++
			}
			if match == negated {
				failed = true
			}
		case '?':
			if !failed {
				if s[0] == '/' {
					failed = true
				}
				_, n := decodeRune(s)
				s = s[n:]
			}
			chunk = chunk[1:]
		case '\\':
			chunk = chunk[1:]
			if len(chunk) == 0 {
				return "", false, ErrBadPattern
			}
			if !failed {
				if chunk[0] != s[0] {
					failed = true
				}
				s = s[1:]
			}
			chunk = chunk[1:]
		default:
			if !failed {
				if chunk[0] != s[0] {
					failed = true
				}
				s = s[1:]
			}
			chunk = chunk[1:]
		}
	}
	if failed {
		return "", false, nil
	}
	return s, true, nil
}

// getEsc gets a possibly escaped character from chunk, for a character
// class.
func getEsc(chunk string) (r rune, nchunk string, err error) {
	if len(chunk) == 0 || chunk[0] == '-' || chunk[0] == ']' {
		err = ErrBadPattern
		return
	}
	if chunk[0] == '\\' {
		chunk = chunk[1:]
		if len(chunk) == 0 {
			err = ErrBadPattern
			return
		}
	}
	r, n := decodeRune(chunk)
	if r == 0xFFFD && n == 1 {
		err = ErrBadPattern
	}
	nchunk = chunk[n:]
	if len(nchunk) == 0 {
		err = ErrBadPattern
	}
	return
}

func decodeRune(s string) (rune, int) { return utf8.DecodeRuneInString(s) }
