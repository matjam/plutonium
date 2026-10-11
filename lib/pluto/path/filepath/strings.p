package filepath

import (
	"pluto/alloc"
	"pluto/os"
	"pluto/rt"
	"pluto/strings"
	"pluto/vec"
	"unsafe"
)

// The forms of Clean, Join, Abs and Rel that return strings (language spec
// section 6.10). Each builds the path with its Append form, in a buffer on
// the stack when it is short and in a new string's own buffer otherwise.

// stackPath is the size of the stack buffer paths are built in.
const stackPath = 256

// build returns the string that fill appends to an empty buffer of
// capacity n, which must be enough. If the result equals reuse, it returns
// reuse itself, so a path that is already clean allocates nothing.
func build(n int, reuse string, fill func(dst *vec.Fixed[byte])) string {
	if n <= stackPath {
		var stack [stackPath]byte
		f := vec.NewFixed(stack[:])
		fill(&f)
		out := f.Slice()
		if strings.FromUTF8Lossy(out) == reuse {
			return reuse
		}
		return strings.FromUTF8Lossy(out)
	}
	p, ctl := rt.NewStringBuffer(n, alloc.Default())
	f := vec.NewFixed(unsafe.Slice(p, n))
	fill(&f)
	out := f.Slice()
	s := reuse
	// Compared and dropped: a path built from valid strings (cut only at
	// slashes), so valid UTF-8 anyway.
	if unsafe.String(p, len(out)) != reuse {
		// The bytes are a path built from valid strings, cut only at
		// slashes: valid UTF-8.
		s = rt.MakeString(p, len(out), ctl, false)
	}
	rt.ReleaseString(ctl)
	return s
}

// Clean returns the shortest path equivalent to path by lexical processing,
// as Go's Clean does (see AppendClean). A path that is already clean is
// returned as it is.
func Clean(path string) string {
	return build(len(path)+1, path, func(dst *vec.Fixed[byte]) { AppendClean(dst, path) })
}

// Join joins the non-empty elements with slashes and cleans the result, as
// Go's Join does; it returns "" if every element is empty.
func Join(elem ...string) string {
	n, only := 0, ""
	nonEmpty := 0
	for _, e := range elem {
		n += len(e) + 1
		if e != "" {
			nonEmpty++
			only = e
		}
	}
	if nonEmpty == 0 {
		return ""
	}
	if nonEmpty > 1 {
		only = "" // the result is never one of the elements
	}
	return build(n+1, only, func(dst *vec.Fixed[byte]) { AppendJoin(dst, elem...) })
}

// Abs returns an absolute, clean form of path, joined to the working
// directory if it is relative.
func Abs(path string) (string, error) {
	if IsAbs(path) {
		return Clean(path), nil
	}
	wd := os.Getwd() or return
	return Join(wd, path), nil
}

// Rel returns a relative path that is lexically equivalent to targpath when
// joined to basepath, as Go's Rel does. It fails if targpath cannot be made
// relative to basepath, such as when one is absolute and the other is not.
func Rel(basepath, targpath string) (string, error) {
	// Each element of the base becomes at most "../", and the target's
	// rest is copied.
	n := 3*len(basepath) + len(targpath) + 4
	var err error
	s := build(n, "", func(dst *vec.Fixed[byte]) { err = AppendRel(dst, basepath, targpath) })
	if err != nil {
		return "", err
	}
	return s, nil
}
