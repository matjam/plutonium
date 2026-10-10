// Package rt is the Plutonium runtime: the few routines compiled code calls
// on its own (panics, memory copies, interface conversions, allocation) and
// the program entry point in hosted builds.
//
// Every program links rt. It must not import any other package except
// unsafe, so that it can be initialised first.
package rt

import "unsafe"

// ----------------------------------------------------------------------------
// Type descriptors and itabs. The layouts must match the compiler's
// (internal/lower/iface.go).

type typeDesc struct {
	size  uintptr
	hash  uint32
	kind  uint8
	flags uint8
	_     uint16
	name  string
	equal func(a, b unsafe.Pointer) bool
}

const flagDirect = 1

type itab struct {
	typ   *typeDesc
	iface *typeDesc
	// followed by one code pointer per interface method
}

//pu:extern pluto.itabtable
func itabTable() unsafe.Pointer

// findItab returns the itab for concrete type typ and interface iface, or
// nil if typ does not implement iface.
func findItab(typ, iface *typeDesc) *itab {
	if typ == nil {
		return nil
	}
	table := itabTable()
	n := *(*int)(table)
	entries := unsafe.Add(table, 8)
	for i := 0; i < n; i++ {
		t := *(**itab)(unsafe.Add(entries, i*8))
		if t.typ == typ && t.iface == iface {
			return t
		}
	}
	return nil
}

// assertItab implements x.(I) for a non-empty interface I.
func assertItab(typ, iface *typeDesc) *itab { return findItab(typ, iface) }

// convI2I converts between interfaces where the static types guarantee
// success.
func convI2I(typ, iface *typeDesc) *itab {
	if typ == nil {
		return nil
	}
	t := findItab(typ, iface)
	if t == nil {
		panicMessage("interface conversion: ", typ.name, " is not ", iface.name, ": missing method")
	}
	return t
}

// ifaceEqual compares two interface values given their dynamic types and
// data words.
func ifaceEqual(xt unsafe.Pointer, xd unsafe.Pointer, yt unsafe.Pointer, yd unsafe.Pointer) bool {
	if xt != yt {
		return false
	}
	if xt == nil {
		return true
	}
	t := (*typeDesc)(xt)
	if t.flags&flagDirect != 0 {
		return xd == yd
	}
	if t.equal == nil {
		panicMessage("runtime error: comparing uncomparable type ", t.name, "", "", "")
	}
	return t.equal(xd, yd)
}

// Comparable reports whether v's dynamic type is comparable, so that == on
// interface values holding it cannot panic. It is true for nil.
func Comparable(v any) bool {
	words := (*[2]unsafe.Pointer)(unsafe.Pointer(&v))
	t := (*typeDesc)(words[0])
	return t == nil || t.flags&flagDirect != 0 || t.equal != nil
}

// ----------------------------------------------------------------------------
// Inspecting interface values. Plutonium has no reflection, but formatting
// needs a value's kind, size and type name, which the descriptor holds.

// Kind numbers, as in the compiler's type descriptors.
const (
	KindBool          = 1
	KindInt           = 2
	KindInt8          = 3
	KindInt16         = 4
	KindInt32         = 5
	KindInt64         = 6
	KindUint          = 7
	KindUint8         = 8
	KindUint16        = 9
	KindUint32        = 10
	KindUint64        = 11
	KindUintptr       = 12
	KindFloat32       = 13
	KindFloat64       = 14
	KindArray         = 17
	KindFunc          = 19
	KindInterface     = 20
	KindPointer       = 22
	KindSlice         = 23
	KindString        = 24
	KindStruct        = 25
	KindUnsafePointer = 26
	KindPacked        = 27
	KindRing          = 28
	KindOwned         = 29
	KindUnion         = 30
)

// unionDesc is the descriptor of a union type (KindUnion), which continues
// a typeDesc with the payload's offset and a table of the variants.
type unionDesc struct {
	typeDesc
	payload  uintptr      // offset of the payload
	variants *variantDesc // the first of count entries, in tag order
	count    int
}

type variantDesc struct {
	name  string
	typ   *typeDesc // the payload's descriptor; nil without a payload
	iface uintptr   // 1 if the payload is an empty interface, 2 another interface
}

// UnionInfo describes the union value an empty interface value holds: the
// name of the variant it holds, and whether the variant has a payload and
// that payload as an interface value. It returns "" if v does not hold a
// union.
func UnionInfo(v any) (variant string, payload any, hasPayload bool) {
	words := (*[2]unsafe.Pointer)(unsafe.Pointer(&v))
	t := (*typeDesc)(words[0])
	if t == nil || t.kind != KindUnion {
		return "", nil, false
	}
	u := (*unionDesc)(unsafe.Pointer(t))
	data := words[1]
	tag := int(*(*uint8)(data))
	if u.count > 256 {
		tag = int(*(*uint16)(data))
	}
	if tag >= u.count {
		return "?", nil, false
	}
	vd := (*variantDesc)(unsafe.Add(unsafe.Pointer(u.variants), tag*int(unsafe.Sizeof(variantDesc{}))))
	if vd.typ == nil {
		return vd.name, nil, false
	}
	p := unsafe.Add(data, u.payload)
	out := (*[2]unsafe.Pointer)(unsafe.Pointer(&payload))
	switch vd.iface {
	case 1:
		payload = *(*any)(p)
	case 2:
		inner := (*[2]unsafe.Pointer)(p)
		if inner[0] != nil {
			out[0] = unsafe.Pointer((*itab)(inner[0]).typ)
			out[1] = inner[1]
		}
	default:
		out[0] = unsafe.Pointer(vd.typ)
		out[1] = p
		if vd.typ.flags&flagDirect != 0 {
			// The data word holds a direct value's bits, zero-extended.
			var bits uint64
			b := (*[8]byte)(unsafe.Pointer(&bits))
			for i := uintptr(0); i < vd.typ.size; i++ {
				b[i] = *(*byte)(unsafe.Add(p, i))
			}
			out[1] = unsafe.Pointer(uintptr(bits))
		}
	}
	return vd.name, payload, true
}

// TypeInfo describes the dynamic type of an empty interface value: its kind,
// size and name (all zero for nil), and its data word. For kinds whose
// values fit in a word the data word is the value's bits; otherwise it
// points at the value.
func TypeInfo(v any) (kind uint8, size uintptr, name string, data unsafe.Pointer) {
	words := (*[2]unsafe.Pointer)(unsafe.Pointer(&v))
	t := (*typeDesc)(words[0])
	if t == nil {
		return 0, 0, "", nil
	}
	return t.kind, t.size, t.name, words[1]
}

// ----------------------------------------------------------------------------
// Panics. Panicking prints a message and a stack trace (trace.p) and stops;
// it does not unwind.

func panicIndex(i, n int) {
	printString("panic: runtime error: index out of range [")
	printInt(int64(i))
	printString("] with length ")
	printInt(int64(n))
	printNewline()
	fatal()
}

func panicSlice(lo, hi, max int) {
	printString("panic: runtime error: slice bounds out of range [")
	printInt(int64(lo))
	printString(":")
	printInt(int64(hi))
	printString(":")
	printInt(int64(max))
	printString("]\n")
	fatal()
}

func panicDivide() {
	printString("panic: runtime error: integer divide by zero\n")
	fatal()
}

func panicShift() {
	printString("panic: runtime error: negative shift amount\n")
	fatal()
}

func panicAppend(n, c int) {
	printString("panic: append beyond capacity: need ")
	printInt(int64(n))
	printString(", have ")
	printInt(int64(c))
	printNewline()
	fatal()
}

func panicMakeLen(n, c int) {
	printString("panic: runtime error: makeslice: len out of range\n")
	fatal()
}

func panicSliceConvert(n, want int) {
	printString("panic: runtime error: cannot convert slice with length ")
	printInt(int64(n))
	printString(" to array of length ")
	printInt(int64(want))
	printNewline()
	fatal()
}

func panicAssert(have, want *typeDesc) {
	printString("panic: interface conversion: interface is ")
	if have == nil {
		printString("nil")
	} else {
		printString(have.name)
	}
	printString(", not ")
	printString(want.name)
	printNewline()
	fatal()
}

// panicValue implements panic(v).
// panicUnion reports a failed assertion u.(U.V) on a union value: union
// holds the variant have (empty if not known), not want.
func panicUnion(union, have, want string) {
	printString("panic: union variant: ")
	printString(union)
	if have == "" {
		printString(" does not hold ")
	} else {
		printString(" holds ")
		printString(have)
		printString(", not ")
	}
	printString(want)
	printNewline()
	fatal()
}

// panicUnionTag reports a union value whose tag names none of its
// variants, which only unsafe code or memory corruption can make.
func panicUnionTag(union string, tag int64) {
	printString("panic: invalid tag ")
	printInt(tag)
	printString(" in a value of union ")
	printString(union)
	printNewline()
	fatal()
}

func panicValue(v any) {
	printString("panic: ")
	switch x := v.(type) {
	case string:
		printString(x)
	case error:
		printString(x.Error())
	case int:
		printInt(int64(x))
	case int64:
		printInt(x)
	case uint64:
		printUint(x)
	case bool:
		printBool(x)
	default:
		printString("(")
		if x == nil {
			printString("nil")
		} else {
			printString("value")
		}
		printString(")")
	}
	printNewline()
	fatal()
}

func panicMessage(a, b, c, d, e string) {
	printString("panic: ")
	printString(a)
	printString(b)
	printString(c)
	printString(d)
	printString(e)
	printNewline()
	fatal()
}

// ----------------------------------------------------------------------------
// print and println

func printSpace()   { printString(" ") }
func printNewline() { printString("\n") }

func printBool(b bool) {
	if b {
		printString("true")
	} else {
		printString("false")
	}
}

func printInt(v int64) {
	if v < 0 {
		printString("-")
		printUint(uint64(-v))
		return
	}
	printUint(uint64(v))
}

func printUint(v uint64) {
	var buf [20]byte
	i := len(buf)
	for {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
		if v == 0 {
			break
		}
	}
	printBytes(unsafe.Pointer(&buf[i]), len(buf)-i)
}

func printPointer(p unsafe.Pointer) {
	const digits = "0123456789abcdef"
	var buf [18]byte
	v := uintptr(p)
	i := len(buf)
	for {
		i--
		buf[i] = digits[v&15]
		v >>= 4
		if v == 0 {
			break
		}
	}
	i--
	buf[i] = 'x'
	i--
	buf[i] = '0'
	printBytes(unsafe.Pointer(&buf[i]), len(buf)-i)
}

func printFloat(f float64) {
	// Enough for diagnostics: sign, integer part, six decimals.
	if f != f {
		printString("NaN")
		return
	}
	if f < 0 {
		printString("-")
		f = -f
	}
	if f > 1e18 {
		printString("+Inf")
		return
	}
	ip := uint64(f)
	printUint(ip)
	printString(".")
	frac := f - float64(ip)
	for i := 0; i < 6; i++ {
		frac *= 10
		d := uint64(frac)
		printUint(d)
		frac -= float64(d)
	}
}

func printString(s string) {
	if len(s) > 0 {
		printBytes(unsafe.Pointer(unsafe.StringData(s)), len(s))
	}
}

// ----------------------------------------------------------------------------
// Memory

func memmove(dst, src unsafe.Pointer, n int) {
	if n <= 0 || dst == src {
		return
	}
	d := uintptr(dst)
	s := uintptr(src)
	if d < s || d >= s+uintptr(n) {
		for i := 0; i < n; i++ {
			*(*byte)(unsafe.Add(dst, i)) = *(*byte)(unsafe.Add(src, i))
		}
		return
	}
	for i := n - 1; i >= 0; i-- {
		*(*byte)(unsafe.Add(dst, i)) = *(*byte)(unsafe.Add(src, i))
	}
}

func memclr(p unsafe.Pointer, n int) {
	for i := 0; i < n; i++ {
		*(*byte)(unsafe.Add(p, i)) = 0
	}
}

func memequal(a, b unsafe.Pointer, n int) bool {
	for i := 0; i < n; i++ {
		if *(*byte)(unsafe.Add(a, i)) != *(*byte)(unsafe.Add(b, i)) {
			return false
		}
	}
	return true
}

func strequal(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return memequal(unsafe.Pointer(unsafe.StringData(a)), unsafe.Pointer(unsafe.StringData(b)), len(a))
}

func strcmp(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

// decodeRune decodes the UTF-8 sequence at p[i:n] and returns the rune and
// its length. Invalid encodings decode as U+FFFD with length 1.
func decodeRune(p *byte, n, i int) (rune, int) {
	s := unsafe.String(p, n)
	b0 := s[i]
	if b0 < 0x80 {
		return rune(b0), 1
	}
	const bad = 0xFFFD
	need := 0
	var r rune
	switch {
	case b0&0xE0 == 0xC0:
		need, r = 1, rune(b0&0x1F)
	case b0&0xF0 == 0xE0:
		need, r = 2, rune(b0&0x0F)
	case b0&0xF8 == 0xF0:
		need, r = 3, rune(b0&0x07)
	default:
		return bad, 1
	}
	if i+need >= n {
		return bad, 1 // truncated sequence
	}
	for k := 1; k <= need; k++ {
		c := s[i+k]
		if c&0xC0 != 0x80 {
			return bad, 1
		}
		r = r<<6 | rune(c&0x3F)
	}
	switch {
	case need == 1 && r < 0x80, need == 2 && r < 0x800, need == 3 && r < 0x10000:
		return bad, 1 // overlong
	case r > 0x10FFFF, r >= 0xD800 && r <= 0xDFFF:
		return bad, 1
	}
	return r, need + 1
}

// ----------------------------------------------------------------------------
// Allocation

// An Allocator provides memory for new, make and the pluto/alloc functions.
// pluto/alloc re-exports it.
type Allocator interface {
	// Alloc returns size bytes of zeroed memory aligned to align, or nil.
	Alloc(size, align uintptr) unsafe.Pointer
	// Free releases memory returned by Alloc with the same size and align.
	Free(p unsafe.Pointer, size, align uintptr)
}

var defaultAllocator Allocator

// DefaultAllocator returns the current default allocator: the running
// puroutine's, if it installed one with UseAllocator, otherwise the
// program-wide default. It returns nil if there is neither.
func DefaultAllocator() Allocator {
	if g := current(); g != nil && g.alloc != nil {
		return g.alloc
	}
	return defaultAllocator
}

// SetDefaultAllocator replaces the program-wide default allocator.
func SetDefaultAllocator(a Allocator) { defaultAllocator = a }

// UseAllocator makes a the running puroutine's default allocator, in place
// of the program-wide default, and returns the allocator it replaced
// (nil if the puroutine had none of its own); UseAllocator(prev) restores
// it. Other puroutines, including those this one starts, are not affected
// (language spec section 7.1). Outside a puroutine (in a scheduler's own
// context) it changes the program-wide default instead.
func UseAllocator(a Allocator) Allocator {
	g := current()
	if g == nil {
		prev := defaultAllocator
		defaultAllocator = a
		return prev
	}
	prev := g.alloc
	g.alloc = a
	return prev
}

// alloc implements new and make without an explicit allocator.
func alloc(size, align uintptr) unsafe.Pointer {
	a := DefaultAllocator()
	if a == nil {
		panicMessage("no allocator: call alloc.SetDefault before allocating", "", "", "", "")
	}
	return allocAt(a, size, align, CallerPC())
}

// allocWith implements new and make with an explicit allocator.
func allocWith(a Allocator, size, align uintptr) unsafe.Pointer {
	if a == nil {
		panicMessage("nil allocator passed to new or make", "", "", "", "")
	}
	return allocAt(a, size, align, CallerPC())
}

// allocAt allocates for the new or make whose call returns to pc,
// publishing pc through AllocSite while the allocator runs if TrackSites
// is set.
func allocAt(a Allocator, size, align, pc uintptr) unsafe.Pointer {
	if size == 0 {
		size = 1
	}
	if TrackSites {
		SetAllocSite(pc)
	}
	p := a.Alloc(size, align)
	if TrackSites {
		SetAllocSite(0)
	}
	if p == nil {
		panicMessage("out of memory", "", "", "", "")
	}
	return p
}
