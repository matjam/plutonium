package rt

import "unsafe"

// ----------------------------------------------------------------------------
// Strings. A string is three words (abi.md section 1): its data, its length
// and a control word. The control word of a counted string is the address of
// the header in front of its buffer, or'ed with flag bits; a control word
// below 8 holds only flags and marks a string nobody counts (a literal, the
// zero string, or a view from unsafe.String). Copies of a counted string
// share its buffer, and the compiler calls strRetain and strRelease as they
// are made and dropped; the last release frees the buffer. Every string
// holds valid UTF-8 (language spec section 6.10).
//
// The compiler lowers its own string operations to the functions below,
// which return a new string as its three words, so that the value is not
// itself copied (and counted) on its way out. Library code that builds
// strings uses MakeString and the exported functions at the end.

// strWords is the layout of a string.
type strWords struct {
	p   *byte
	n   int
	ctl uintptr
}

// Flags in the control word, mirrored by internal/lower/strrep.go.
const (
	// strNonASCII is set when the string may hold a byte >= 0x80.
	strNonASCII = 1
	// strFlagMask covers the flag bits.
	strFlagMask = 7
)

// strHeader precedes the bytes of a counted string buffer.
type strHeader struct {
	count int64     // references
	a     Allocator // the allocator the buffer came from
	size  uintptr   // bytes allocated, the header included
}

const strHeaderSize = 32

// atomicCounts makes string counts atomic. Hosted programs start
// single-threaded and count with plain arithmetic; freestanding programs,
// whose processors the kernel starts, count atomically from the start.
var atomicCounts = atomicCountsAtStart

// SetThreaded makes string reference counts atomic from now on. A scheduler
// calls it before the program's second thread runs Plutonium code; a kernel
// in freestanding mode need not, since its counts are atomic already. It
// cannot be undone.
func SetThreaded() { atomicCounts = true }

// xadd64 atomically adds delta to *p and returns the new value
// (str_amd64.s).
func xadd64(p *int64, delta int64) int64

// strRetain takes a reference to the buffer of the string whose control
// word is ctl. The compiler calls it when a string is copied into a place
// that keeps it.
//
//pu:inline
func strRetain(ctl uintptr) {
	if ctl > strFlagMask {
		h := (*strHeader)(unsafe.Pointer(ctl &^ strFlagMask))
		if atomicCounts {
			xadd64(&h.count, 1)
		} else {
			h.count++
		}
	}
}

// strRelease drops a reference; the last one frees the buffer.
//
//pu:inline
func strRelease(ctl uintptr) {
	if ctl > strFlagMask {
		strFree(ctl)
	}
}

// strFree drops a reference to a counted buffer and frees it when it was
// the last.
//
//pu:noinline
func strFree(ctl uintptr) {
	h := (*strHeader)(unsafe.Pointer(ctl &^ strFlagMask))
	var n int64
	if atomicCounts {
		n = xadd64(&h.count, -1)
	} else {
		h.count--
		n = h.count
	}
	if n > 0 {
		return
	}
	if n < 0 {
		panicMessage("runtime error: string reference count below zero (a string was released twice)", "", "", "", "")
	}
	a := h.a
	a.Free(unsafe.Pointer(h), h.size, 8)
}

// strAlloc allocates a counted buffer for n > 0 bytes from the current
// allocator, publishing pc as the allocation's site. The buffer holds one
// reference. It returns the bytes and the control word, without flags.
func strAlloc(n int, pc uintptr) (*byte, uintptr) {
	a := defaultAllocator
	if a == nil {
		panicMessage("no allocator: call alloc.SetDefault before building strings", "", "", "", "")
	}
	return strAllocFrom(a, n, pc)
}

func strAllocFrom(a Allocator, n int, pc uintptr) (*byte, uintptr) {
	if n < 0 || n > 1<<48 {
		panicMessage("runtime error: string length out of range", "", "", "", "")
	}
	size := uintptr(strHeaderSize + n)
	p := allocAt(a, size, 8, pc)
	h := (*strHeader)(p)
	h.count = 1
	h.a = a
	h.size = size
	return (*byte)(unsafe.Add(p, strHeaderSize)), uintptr(p)
}

// concatStrings implements a + b + ...: the operands' bytes in one new
// buffer. If at most one operand is not empty, the result is that operand
// itself, shared rather than copied. The result holds one reference.
func concatStrings(a []string) (*byte, int, uintptr) {
	n := 0
	nonEmpty := -1
	count := 0
	flags := uintptr(0)
	for i := 0; i < len(a); i++ {
		w := (*strWords)(unsafe.Pointer(&a[i]))
		if w.n > 0 {
			if n+w.n < n {
				panicMessage("runtime error: string concatenation too long", "", "", "", "")
			}
			n += w.n
			count++
			nonEmpty = i
			flags |= w.ctl & strFlagMask
		}
	}
	if count == 0 {
		return nil, 0, 0
	}
	if count == 1 {
		w := (*strWords)(unsafe.Pointer(&a[nonEmpty]))
		strRetain(w.ctl)
		return w.p, w.n, w.ctl
	}
	p, ctl := strAlloc(n, CallerPC())
	off := 0
	for i := 0; i < len(a); i++ {
		w := (*strWords)(unsafe.Pointer(&a[i]))
		if w.n > 0 {
			memmove(unsafe.Add(unsafe.Pointer(p), off), unsafe.Pointer(w.p), w.n)
			off += w.n
		}
	}
	return p, n, ctl | flags
}

// ----------------------------------------------------------------------------
// UTF-8

const runeError = 0xFFFD

// scanUTF8 reports whether the n bytes at p are valid UTF-8 and whether
// they are all ASCII.
func scanUTF8(p *byte, n int) (valid bool, ascii bool) {
	i := 0
	// Eight ASCII bytes at a time.
	for i+8 <= n {
		w := *(*uint64)(unsafe.Add(unsafe.Pointer(p), i))
		if w&0x8080808080808080 != 0 {
			break
		}
		i += 8
	}
	ascii = true
	for i < n {
		if *(*byte)(unsafe.Add(unsafe.Pointer(p), i)) < 0x80 {
			i++
			continue
		}
		ascii = false
		r, size := decodeRune(p, n, i)
		if r == runeError && size == 1 {
			return false, false
		}
		i += size
	}
	return true, ascii
}

// encodeRune writes the UTF-8 encoding of r (U+FFFD if r is not a valid
// code point) at p and returns its length; p has room for 4 bytes.
func encodeRune(p *byte, r rune) int {
	if r < 0 || r > 0x10FFFF || r >= 0xD800 && r <= 0xDFFF {
		r = runeError
	}
	b := (*[4]byte)(unsafe.Pointer(p))
	switch {
	case r < 0x80:
		b[0] = byte(r)
		return 1
	case r < 0x800:
		b[0] = byte(0xC0 | r>>6)
		b[1] = byte(0x80 | r&0x3F)
		return 2
	case r < 0x10000:
		b[0] = byte(0xE0 | r>>12)
		b[1] = byte(0x80 | (r>>6)&0x3F)
		b[2] = byte(0x80 | r&0x3F)
		return 3
	}
	b[0] = byte(0xF0 | r>>18)
	b[1] = byte(0x80 | (r>>12)&0x3F)
	b[2] = byte(0x80 | (r>>6)&0x3F)
	b[3] = byte(0x80 | r&0x3F)
	return 4
}

// runeLen returns the length of r's encoding, counting an invalid r as
// U+FFFD.
func runeLen(r rune) int {
	switch {
	case r < 0 || r > 0x10FFFF || r >= 0xD800 && r <= 0xDFFF:
		return 3
	case r < 0x80:
		return 1
	case r < 0x800:
		return 2
	case r < 0x10000:
		return 3
	}
	return 4
}

// stringFromBytes implements string(b): a copy of the bytes in a new
// buffer, with each byte that does not start a valid UTF-8 sequence
// replaced by U+FFFD, as ranging over the bytes would decode it.
func stringFromBytes(p *byte, n int) (*byte, int, uintptr) {
	return stringFromBytesAt(p, n, CallerPC())
}

func stringFromBytesAt(p *byte, n int, pc uintptr) (*byte, int, uintptr) {
	if n == 0 {
		return nil, 0, 0
	}
	valid, ascii := scanUTF8(p, n)
	if valid {
		q, ctl := strAlloc(n, pc)
		memmove(unsafe.Pointer(q), unsafe.Pointer(p), n)
		if !ascii {
			ctl |= strNonASCII
		}
		return q, n, ctl
	}
	// Measure, then copy with replacements.
	m := 0
	for i := 0; i < n; {
		r, size := decodeRune(p, n, i)
		if r == runeError && size == 1 {
			m += 3
		} else {
			m += size
		}
		i += size
	}
	q, ctl := strAlloc(m, pc)
	off := 0
	for i := 0; i < n; {
		r, size := decodeRune(p, n, i)
		dst := (*byte)(unsafe.Add(unsafe.Pointer(q), off))
		if r == runeError && size == 1 {
			off += encodeRune(dst, runeError)
		} else {
			memmove(unsafe.Pointer(dst), unsafe.Add(unsafe.Pointer(p), i), size)
			off += size
		}
		i += size
	}
	return q, m, ctl | strNonASCII
}

// stringFromRunes implements string(runes); invalid code points become
// U+FFFD, as in Go.
func stringFromRunes(p *rune, n int) (*byte, int, uintptr) {
	m := 0
	ascii := true
	for i := 0; i < n; i++ {
		r := *(*rune)(unsafe.Add(unsafe.Pointer(p), 4*i))
		m += runeLen(r)
		if r < 0 || r >= 0x80 {
			ascii = false
		}
	}
	if m == 0 {
		return nil, 0, 0
	}
	q, ctl := strAlloc(m, CallerPC())
	off := 0
	for i := 0; i < n; i++ {
		r := *(*rune)(unsafe.Add(unsafe.Pointer(p), 4*i))
		off += encodeRune((*byte)(unsafe.Add(unsafe.Pointer(q), off)), r)
	}
	if !ascii {
		ctl |= strNonASCII
	}
	return q, m, ctl
}

// asciiTable holds the 128 one-byte strings that string(r) returns for an
// ASCII r, without allocating.
var asciiTable = [128]byte{
	0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31,
	32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47,
	48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63,
	64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79,
	80, 81, 82, 83, 84, 85, 86, 87, 88, 89, 90, 91, 92, 93, 94, 95,
	96, 97, 98, 99, 100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111,
	112, 113, 114, 115, 116, 117, 118, 119, 120, 121, 122, 123, 124, 125, 126, 127,
}

// stringFromRune implements string(r) for a rune that is not a constant.
// An ASCII rune gives a static string; others allocate.
func stringFromRune(r rune) (*byte, int, uintptr) {
	if r >= 0 && r < 0x80 {
		return &asciiTable[r], 1, 0
	}
	n := runeLen(r)
	q, ctl := strAlloc(n, CallerPC())
	encodeRune(q, r)
	return q, n, ctl | strNonASCII
}

// zerobase is where an empty slice made by a conversion points.
var zerobase byte

// bytesFromString implements []byte(s): a copy of the bytes in memory from
// the current allocator, as make([]byte, len(s)) would allocate it.
func bytesFromString(p *byte, n int) *byte {
	if n == 0 {
		return &zerobase
	}
	a := defaultAllocator
	if a == nil {
		panicMessage("no allocator: call alloc.SetDefault before allocating", "", "", "", "")
	}
	q := allocAt(a, uintptr(n), 1, CallerPC())
	memmove(q, unsafe.Pointer(p), n)
	return (*byte)(q)
}

// runesFromString implements []rune(s): the decoded runes in memory from
// the current allocator.
func runesFromString(p *byte, n int) (*rune, int) {
	count := 0
	for i := 0; i < n; {
		_, size := decodeRune(p, n, i)
		i += size
		count++
	}
	if count == 0 {
		return (*rune)(unsafe.Pointer(&zerobase)), 0
	}
	a := defaultAllocator
	if a == nil {
		panicMessage("no allocator: call alloc.SetDefault before allocating", "", "", "", "")
	}
	q := allocAt(a, uintptr(4*count), 4, CallerPC())
	k := 0
	for i := 0; i < n; {
		r, size := decodeRune(p, n, i)
		*(*rune)(unsafe.Add(q, 4*k)) = r
		k++
		i += size
	}
	return (*rune)(q), count
}

// checkRuneBounds panics unless lo and hi, bounds of a slice of the n
// bytes at p already checked against n, are each at the start of a rune or
// at n. The compiler calls it for strings whose flags do not say ASCII. It
// is not inlined, so that a panic's trace starts at the slice expression.
//
//pu:noinline
func checkRuneBounds(p *byte, n, lo, hi int) {
	if lo < n && *(*byte)(unsafe.Add(unsafe.Pointer(p), lo))&0xC0 == 0x80 {
		panicSliceRune(lo)
	}
	if hi < n && *(*byte)(unsafe.Add(unsafe.Pointer(p), hi))&0xC0 == 0x80 {
		panicSliceRune(hi)
	}
}

// panicSliceRune reports a string slice expression whose bound i falls
// inside a rune's encoding.
func panicSliceRune(i int) {
	startPanic()
	printString("runtime error: slice bound ")
	printInt(int64(i))
	printString(" is not at the start of a rune: a string holds valid UTF-8, so it can only be sliced between runes")
	fatal(nil)
}

// ----------------------------------------------------------------------------
// The library's interface to string buffers. These are the unchecked layer
// that pluto/strings and pluto/fmt build strings with; ordinary code builds
// strings with +, conversions and strings.Builder.

// NewStringBuffer allocates a counted buffer for n bytes from a and returns
// its bytes and its control word. The buffer holds one reference, which
// belongs to the caller: release it with ReleaseString when done. The bytes
// may be written until a string made from them with MakeString is used, and
// must then be left unchanged.
func NewStringBuffer(n int, a Allocator) (*byte, uintptr) {
	if a == nil {
		panicMessage("NewStringBuffer: nil allocator", "", "", "", "")
	}
	if n == 0 {
		n = 1
	}
	return strAllocFrom(a, n, CallerPC())
}

// MakeString returns the string of the n bytes at p, in the buffer whose
// control word is ctl (0 for memory nobody counts, which must then outlive
// every copy of the string). The string takes a reference of its own. The
// bytes must be valid UTF-8; ascii says they are all ASCII.
func MakeString(p *byte, n int, ctl uintptr, ascii bool) string {
	if n == 0 {
		return ""
	}
	w := strWords{p: p, n: n, ctl: ctl &^ strFlagMask}
	if !ascii {
		w.ctl |= strNonASCII
	}
	// Returning a copy of the string read through a pointer takes the
	// reference.
	return *(*string)(unsafe.Pointer(&w))
}

// StringControl returns the control word of s: the address of its buffer's
// header with the flag bits, or only the flags for a string nobody counts.
// The compiler expands it inline.
func StringControl(s string) uintptr {
	return (*strWords)(unsafe.Pointer(&s)).ctl
}

// KnownASCII reports whether s's flags say that every byte of s is ASCII.
// It is set for strings whose bytes were checked when they were made (every
// string but views from unsafe.String and substrings of non-ASCII strings),
// so a false result means only "maybe not". The library uses it for fast
// paths: an ASCII string's runes are its bytes. The compiler expands it
// inline.
func KnownASCII(s string) bool {
	return (*strWords)(unsafe.Pointer(&s)).ctl&strNonASCII == 0
}

// RetainString takes a reference to the buffer of a counted control word.
func RetainString(ctl uintptr) { strRetain(ctl) }

// ReleaseString drops a reference taken with NewStringBuffer or
// RetainString; the last one frees the buffer.
func ReleaseString(ctl uintptr) { strRelease(ctl) }

// The drop glue of values that hold strings, for containers written with
// unsafe memory (pluto/vec, pluto/hash). The compiler expands each call
// inline; for a T that holds no strings, ReleaseValues and RetainValues
// compile to nothing. They cannot be used as function values (the compiler
// reports it), so the bodies below never run.

// ReleaseValues drops the string references held by the n values of type T
// at p, as dropping those values would. The memory is left as it is.
func ReleaseValues[T any](p *T, n int) { notCalled() }

// RetainValues takes a reference for every string in the n values of type
// T at p, as copying them into a place that keeps them would.
func RetainValues[T any](p *T, n int) { notCalled() }

// MoveValues copies the n values of type T at src to dst (which may
// overlap) without counting: the references move with the bytes, so the
// source must not be released afterwards.
func MoveValues[T any](dst, src *T, n int) { notCalled() }

// HoldsStrings reports whether values of type T hold string references,
// so that containers can skip loops of ReleaseValues for other types.
func HoldsStrings[T any]() bool {
	notCalled()
	return false
}

func notCalled() {
	panicMessage("internal error: a compiler intrinsic of pluto/rt was called through a function value", "", "", "", "")
}

// ValidUTF8 reports whether the n bytes at p are valid UTF-8, and whether
// they are all ASCII.
func ValidUTF8(p *byte, n int) (valid, ascii bool) { return scanUTF8(p, n) }

// StringFromBytes returns a string holding a copy of the n bytes at p, in a
// buffer from a, with each byte that does not start a valid UTF-8 sequence
// replaced by U+FFFD.
func StringFromBytes(p *byte, n int, a Allocator) string {
	if a == nil {
		panicMessage("StringFromBytes: nil allocator", "", "", "", "")
	}
	saved := defaultAllocator
	defaultAllocator = a
	q, m, ctl := stringFromBytesAt(p, n, CallerPC())
	defaultAllocator = saved
	s := MakeString(q, m, ctl, ctl&strNonASCII == 0)
	strRelease(ctl)
	return s
}
