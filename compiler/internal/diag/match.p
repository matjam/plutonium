// Translated by go2pu from internal/diag/match.go at the go-cutover tag; edited by hand since.

package diag

import (
	"compiler/compat"
	"pluto/strings"
)

// This file holds the matchers the type checker's message rewriting and the
// linker's output reading use, written by hand because the compiler does not
// use regexp (it is not part of the library the compiler is ported to). Each
// matcher returns what the regular expression in its comment would return
// from FindStringSubmatch (the whole match first, then the groups; nil when
// there is no match), and match_test.go checks that it does, on generated
// text. The messages the type checker matches are one line long: a matcher of
// a type checker message returns nil for text with a newline in it, as its
// expression would for all but the unanchored ones.

// firstLine returns s up to its first newline.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// isWord reports whether c is in \w: [0-9A-Za-z_].
func isWord(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

// isNonSpace reports whether c is in \S: not one of \t \n \f \r and space.
func isNonSpace(c byte) bool {
	return c != '\t' && c != '\n' && c != '\f' && c != '\r' && c != ' '
}

// run returns the length of the longest prefix of s whose bytes satisfy f.
func run(s string, f func(byte) bool) int {
	n := 0
	for n < len(s) && f(s[n]) {
		n++
	}
	return n
}

// lastIndexBefore returns the largest i <= max with sep at s[i:], or -1.
func lastIndexBefore(s, sep string, max int) int {
	if max > len(s)-len(sep) {
		max = len(s) - len(sep)
	}
	for i := max; i >= 0; i-- {
		if strings.HasPrefix(strings.SliceFrom(s, i), sep) {
			return i
		}
	}
	return -1
}

// groups returns the substrings s[bounds[0]:bounds[1]], s[bounds[2]:bounds[3]]
// and so on; a pair of -1 is a group that did not take part ("").
func groups(s string, bounds ...int) []string {
	out := compat.MakeCap[string](0, len(bounds)/2)
	for i := 0; i+1 < len(bounds); i += 2 {
		if bounds[i] < 0 {
			out = compat.Append(out, "")
		} else {
			out = compat.Append(out, strings.Slice(s, bounds[i], bounds[i+1]))
		}
	}
	return out
}

// matchCannotUse is
//
//	^cannot use (.+) as (.+?) value in (.+?)(?:: (.+))?$
//
// It returns the whole text and the four groups, the last "" when absent.
func matchCannotUse(s string) []string {
	const (
		pre  = "cannot use "
		as   = " as "
		val  = " value in "
		cont = ": "
	)
	if strings.Contains(s, "\n") || !strings.HasPrefix(s, pre) {
		return nil
	}
	// The first (.+) is greedy: its end e1 goes from the right, and takes
	// at least one byte.
	for e1 := len(s); ; e1-- {
		e1 = lastIndexBefore(s, as, e1)
		if e1 <= len(pre) {
			return nil
		}
		rest2 := e1 + len(as)
		// The second is lazy: its end e2 goes from the left.
		for e2 := rest2 + 1; e2+len(val) <= len(s); e2++ {
			if !strings.HasPrefix(strings.SliceFrom(s, e2), val) {
				continue
			}
			rest3 := e2 + len(val)
			// The third is lazy too, and ends the text or is followed by
			// ": " and at least one more byte.
			for e3 := rest3 + 1; e3 <= len(s); e3++ {
				if e3 == len(s) {
					return compat.Append(compat.Clone([]string{s}), groups(s, len(pre), e1, rest2, e2, rest3, e3, -1, -1)...)
				}
				if strings.HasPrefix(strings.SliceFrom(s, e3), cont) && e3+len(cont) < len(s) {
					return compat.Append(compat.Clone([]string{s}), groups(s, len(pre), e1, rest2, e2, rest3, e3, e3+len(cont), len(s))...)
				}
			}
		}
	}
}

// matchConsumesRecv is
//
//	^(.+) does not implement (.+?) \(method (\w+) has a value receiver of owner type
func matchConsumesRecv(s string) []string {
	const (
		dni  = " does not implement "
		meth = " (method "
		tail = " has a value receiver of owner type"
	)
	if strings.Contains(s, "\n") {
		return nil
	}
	for e1 := len(s); ; {
		e1 = lastIndexBefore(s, dni, e1)
		if e1 < 1 {
			return nil
		}
		start2 := e1 + len(dni)
		for e2 := start2 + 1; e2+len(meth) <= len(s); e2++ {
			if !strings.HasPrefix(strings.SliceFrom(s, e2), meth) {
				continue
			}
			start3 := e2 + len(meth)
			n := run(strings.SliceFrom(s, start3), isWord)
			if n > 0 && strings.HasPrefix(strings.SliceFrom(s, start3+n), tail) {
				return compat.Append(compat.Clone([]string{strings.Slice(s, 0, start3+n+len(tail))}), groups(s, 0, e1, start2, e2, start3, start3+n)...)
			}
		}
		e1--
	}
}

// matchDoesNotImpl is
//
//	^(.+) does not implement (.+?) \((missing method|method) (\w+)(\)| has pointer receiver\))
func matchDoesNotImpl(s string) []string {
	const (
		dni = " does not implement "
		lp  = " ("
	)
	if strings.Contains(s, "\n") {
		return nil
	}
	for e1 := len(s); ; {
		e1 = lastIndexBefore(s, dni, e1)
		if e1 < 1 {
			return nil
		}
		start2 := e1 + len(dni)
		for e2 := start2 + 1; e2+len(lp) <= len(s); e2++ {
			if !strings.HasPrefix(strings.SliceFrom(s, e2), lp) {
				continue
			}
			p := e2 + len(lp)
			for _, kind := range []string{"missing method", "method"} {
				if !strings.HasPrefix(strings.SliceFrom(s, p), compat.Concat(kind, " ")) {
					continue
				}
				w := p + len(kind) + 1
				n := run(strings.SliceFrom(s, w), isWord)
				if n == 0 {
					continue
				}
				end := w + n
				for _, closer := range []string{")", " has pointer receiver)"} {
					if strings.HasPrefix(strings.SliceFrom(s, end), closer) {
						return compat.Append(compat.Clone([]string{strings.Slice(s, 0, end+len(closer))}), groups(s, 0, e1, start2, e2, p, p+len(kind), w, end, end, end+len(closer))...)
					}
				}
			}
		}
		e1--
	}
}

// trimOverflowSuffix is overflowSuffix.ReplaceAllString(s, ""), with
//
//	\((overflows|truncated)\)$
func trimOverflowSuffix(s string) string {
	for _, suffix := range []string{" (overflows)", " (truncated)"} {
		if strings.HasSuffix(s, suffix) {
			return strings.Slice(s, 0, len(s)-len(suffix))
		}
	}
	return s
}

// matchUndefinedName is
//
//	^undefined: (\w+)
func matchUndefinedName(s string) []string {
	const pre = "undefined: "
	if !strings.HasPrefix(s, pre) {
		return nil
	}
	n := run(strings.SliceFrom(s, len(pre)), isWord)
	if n == 0 {
		return nil
	}
	return compat.Clone([]string{strings.Slice(s, 0, len(pre)+n), strings.Slice(s, len(pre), len(pre)+n)})
}

// matchUndefinedQualified is
//
//	^undefined: (\w+)\.(\w+)
func matchUndefinedQualified(s string) []string {
	const pre = "undefined: "
	if !strings.HasPrefix(s, pre) {
		return nil
	}
	a := len(pre)
	n := run(strings.SliceFrom(s, a), isWord)
	if n == 0 || a+n >= len(s) || s[a+n] != '.' {
		return nil
	}
	b := a + n + 1
	m := run(strings.SliceFrom(s, b), isWord)
	if m == 0 {
		return nil
	}
	return compat.Clone([]string{strings.Slice(s, 0, b+m), strings.Slice(s, a, a+n), strings.Slice(s, b, b+m)})
}

// matchInfer is
//
//	^in call to (.+), cannot infer (\w+)
func matchInfer(s string) []string {
	const (
		pre = "in call to "
		mid = ", cannot infer "
	)
	if strings.Contains(s, "\n") || !strings.HasPrefix(s, pre) {
		return nil
	}
	for e1 := len(s); ; {
		e1 = lastIndexBefore(s, mid, e1)
		if e1 <= len(pre) {
			return nil
		}
		w := e1 + len(mid)
		if n := run(strings.SliceFrom(s, w), isWord); n > 0 {
			return compat.Clone([]string{strings.Slice(s, 0, w+n), strings.Slice(s, len(pre), e1), strings.Slice(s, w, w+n)})
		}
		e1--
	}
}

// matchUnusedVar is
//
//	declared and not used: (\w+)|^(\w+) declared and not used
//
// It returns the whole match and the two groups, of which the one that did
// not take part is "".
func matchUnusedVar(s string) []string {
	const (
		a = "declared and not used: "
		b = " declared and not used"
	)
	tryA := func(p int) []string {
		if !strings.HasPrefix(strings.SliceFrom(s, p), a) {
			return nil
		}
		w := p + len(a)
		n := run(strings.SliceFrom(s, w), isWord)
		if n == 0 {
			return nil
		}
		return compat.Clone([]string{strings.Slice(s, p, w+n), strings.Slice(s, w, w+n), ""})
	}
	if m := tryA(0); m != nil {
		return m
	}
	if n := run(s, isWord); n > 0 && strings.HasPrefix(strings.SliceFrom(s, n), b) {
		return compat.Clone([]string{strings.Slice(s, 0, n+len(b)), "", strings.Slice(s, 0, n)})
	}
	for p := 1; p < len(s); p++ {
		if m := tryA(p); m != nil {
			return m
		}
	}
	return nil
}

// matchUnusedImport is
//
//	^"([^"]+)" imported (?:as (\w+) )?and not used
func matchUnusedImport(s string) []string {
	const (
		imp  = `" imported `
		tail = "and not used"
	)
	if !strings.HasPrefix(s, `"`) {
		return nil
	}
	q := strings.IndexByte(strings.SliceFrom(s, 1), '"')
	if q < 1 {
		return nil
	}
	path := strings.Slice(s, 1, 1+q)
	rest := 1 + q
	if !strings.HasPrefix(strings.SliceFrom(s, rest), imp) {
		return nil
	}
	rest += len(imp)
	if strings.HasPrefix(strings.SliceFrom(s, rest), "as ") {
		w := rest + len("as ")
		n := run(strings.SliceFrom(s, w), isWord)
		if n > 0 && strings.HasPrefix(strings.SliceFrom(s, w+n), " "+tail) {
			end := w + n + 1 + len(tail)
			return compat.Clone([]string{strings.Slice(s, 0, end), path, strings.Slice(s, w, w+n)})
		}
	}
	if strings.HasPrefix(strings.SliceFrom(s, rest), tail) {
		return compat.Clone([]string{strings.Slice(s, 0, rest+len(tail)), path, ""})
	}
	return nil
}

// isMismatched reports whether s matches
//
//	\(mismatched types (.+) and (.+)\)$
func isMismatched(s string) bool {
	const (
		pre = "(mismatched types "
		and = " and "
	)
	if strings.Contains(s, "\n") || !strings.HasSuffix(s, ")") {
		return false
	}
	p := strings.Index(s, pre)
	if p < 0 {
		return false
	}
	// The first (.+) needs a byte, then comes " and ", a byte and the ")":
	// the last " and " that leaves room for the byte and the ")".
	q := lastIndexBefore(s, and, len(s)-len(and)-2)
	return q >= p+len(pre)+1
}

// matchUndefinedOp is
//
//	operator (\S+) not defined on (.+)$
func matchUndefinedOp(s string) []string {
	const (
		pre = "operator "
		mid = " not defined on "
	)
	if strings.Contains(s, "\n") {
		return nil
	}
	for p := 0; ; p++ {
		i := strings.Index(strings.SliceFrom(s, p), pre)
		if i < 0 {
			return nil
		}
		p += i
		a := p + len(pre)
		n := run(strings.SliceFrom(s, a), isNonSpace)
		if n > 0 && strings.HasPrefix(strings.SliceFrom(s, a+n), mid) && a+n+len(mid) < len(s) {
			b := a + n + len(mid)
			return compat.Clone([]string{strings.SliceFrom(s, p), strings.Slice(s, a, a+n), strings.SliceFrom(s, b)})
		}
	}
}

// matchQuoted is "<before>([^']*)'": the text between the first before and
// the quote after it. It returns that text and true when there is one.
func matchQuoted(s, before string) (string, bool) {
	i := strings.Index(s, before)
	if i < 0 {
		return "", false
	}
	rest := strings.SliceFrom(s, i+len(before))
	j := strings.IndexByte(rest, '\'')
	if j < 0 {
		return "", false
	}
	return strings.Slice(rest, 0, j), true
}

// matchRelocTrunc is
//
//	relocation truncated to fit: (R_X86_64_[A-Z0-9]+) against (?:symbol )?`?([^' ]*)'?
//
// It returns the relocation and the symbol.
func matchRelocTrunc(s string) (reloc, sym string, ok bool) {
	const (
		pre     = "relocation truncated to fit: "
		rtype   = "R_X86_64_"
		against = " against "
	)
	upper := func(c byte) bool { return c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' }
	for p := 0; ; p++ {
		i := strings.Index(strings.SliceFrom(s, p), pre+rtype)
		if i < 0 {
			return "", "", false
		}
		p += i
		a := p + len(pre)
		n := run(strings.SliceFrom(s, a+len(rtype)), upper)
		if n == 0 || !strings.HasPrefix(strings.SliceFrom(s, a+len(rtype)+n), against) {
			continue
		}
		reloc = strings.Slice(s, a, a+len(rtype)+n)
		rest := strings.SliceFrom(s, a+len(rtype)+n+len(against))
		rest = strings.TrimPrefix(rest, "symbol ")
		rest = strings.TrimPrefix(rest, "`")
		end := strings.IndexAny(rest, "' ")
		if end < 0 {
			end = len(rest)
		}
		return reloc, strings.Slice(rest, 0, end), true
	}
}
