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
	size   uintptr
	hash   uint32
	kind   uint8
	flags  uint8
	align  uint8
	_      uint8
	name   string
	equal  func(a, b unsafe.Pointer) bool
	elem   *typeDesc  // array, slice, pointer, ring, owned and counted references
	len    uintptr    // array length, ring capacity, number of struct fields
	fields *fieldDesc // struct fields; for a packed type with reserved bits, the bytes of its other bits
	ptrTo  *typeDesc  // *T, if *T has methods that T lacks
	drop   uintptr    // code address of the drop adapter of a type held by an owning interface, or 0
}

// A fieldDesc describes one field of a struct type.
type fieldDesc struct {
	typ    *typeDesc
	offset uintptr
	name   string // empty when the build strips field names
	flags  uint8
}

const (
	flagDirect       = 1
	flagIfaceMethods = 2  // an interface type with methods: its values hold an itab
	flagMethods      = 4  // a type with methods
	flagSwapped      = 32 // a big-endian integer packed type: in memory byte-swapped, in an interface word not

	fieldExported = 1
	fieldEmbedded = 2
	fieldBlank    = 4 // a field named _, which comparison skips
)

type itab struct {
	typ   *typeDesc
	iface *typeDesc
	// followed by one code pointer per interface method
}

//pu:extern pluto.itabtable
func itabTable() unsafe.Pointer

// findItab returns the itab for concrete type typ and interface iface, or
// nil if typ does not implement iface.
//
// The itab table is an open-addressing hash table that the compiler lays
// out (internal/lower/iface.go, itabSlots): a power-of-two number of slots,
// then the slots, each an itab pointer or nil. A lookup starts at the slot
// itabHash picks from the two descriptors' hashes and probes forward until
// it finds the itab or an empty slot; at least half the slots are empty.
func findItab(typ, iface *typeDesc) *itab {
	if typ == nil {
		return nil
	}
	table := itabTable()
	mask := *(*uint)(table) - 1
	entries := unsafe.Add(table, 8)
	h := typ.hash*0x9E3779B1 ^ iface.hash
	h ^= h >> 15
	h *= 0x85EBCA6B
	h ^= h >> 13
	for i := uint(h) & mask; ; i = (i + 1) & mask {
		t := *(**itab)(unsafe.Add(entries, i*8))
		if t == nil {
			return nil
		}
		if t.typ == typ && t.iface == iface {
			return t
		}
	}
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
		if t.equal != nil {
			// A packed type with reserved bits, which comparison ignores.
			if t.flags&flagSwapped != 0 {
				xd, yd = swapWord(xd, t.size), swapWord(yd, t.size)
			}
			return t.equal(unsafe.Pointer(&xd), unsafe.Pointer(&yd))
		}
		return xd == yd
	}
	if t.equal == nil {
		panicMessage("runtime error: comparing uncomparable type ", t.name, "", "", "")
	}
	return t.equal(xd, yd)
}

// swapWord reverses the low size bytes of the interface data word w (size
// 1, 2, 4 or 8) and returns it as a data word.
func swapWord(w unsafe.Pointer, size uintptr) unsafe.Pointer {
	x := uint64(uintptr(w))
	var r uint64
	for i := uintptr(0); i < size; i++ {
		r = r<<8 | x&0xFF
		x >>= 8
	}
	return unsafe.Pointer(uintptr(r))
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
	KindChan          = 18
	KindFunc          = 19
	KindInterface     = 20
	KindMap           = 21
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
			// The data word holds a direct value's bits, zero-extended. A
			// word that is a pointer is read as one, so that the escape
			// analysis follows what the union holds (language spec section
			// 18.3).
			if vd.typ.size == 8 {
				out[1] = *(*unsafe.Pointer)(p)
				return vd.name, payload, true
			}
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
// Panics. Each panic function writes its message into the panic's message
// buffer (startPanic) and calls fatal, which hands the panic to the
// program's panic handler (panic.p). Panics do not unwind.

func panicIndex(i, n int) {
	startPanic()
	printString("runtime error: index out of range [")
	printInt(int64(i))
	printString("] with length ")
	printInt(int64(n))
	fatal(nil)
}

// panicNil reports a dereference of a nil pointer that the compiler checks
// explicitly: one whose offset from the pointer can reach past the unmapped
// region at address zero, where the hardware would not fault (language spec
// section 17.5). The message is the one a fault in that region gets.
func panicNil() {
	startPanic()
	printString("runtime error: invalid memory address or nil pointer dereference")
	fatal(nil)
}

func panicSlice(lo, hi, max int) {
	startPanic()
	printString("runtime error: slice bounds out of range [")
	printInt(int64(lo))
	printString(":")
	printInt(int64(hi))
	printString(":")
	printInt(int64(max))
	printString("]")
	fatal(nil)
}

// panicStaleRange diagnoses the first reclaimed address in a failed range,
// including a partial arena tail whose starting pointer remains live.
func panicStaleRange(p unsafe.Pointer, n uintptr) {
	bits := *(*uintptr)(unsafe.Pointer(&p))
	a := uintptr(p)
	end := a + n
	if n != 0 && end >= a && end <= uintptr(1)<<48 {
		for b := a; b < end; b = min(end, (b&^15)+16) {
			q := unsafe.Pointer(b | bits>>48<<48)
			if !GenRegisteredLive(q) {
				panicStale(q)
			}
		}
		q := unsafe.Pointer((end - 1) | bits>>48<<48)
		if !GenRegisteredLive(q) {
			panicStale(q)
		}
	}
	panicStale(p)
}

// panicStale reports an access through a heap pointer whose allocation has
// been freed (the generational-reference prototype, built with -gencheck).
// A tag of 0x8000 or more is an arena epoch (epoch_on.p): the arena was
// reset, released or dropped, or the allocation was freed, since p was
// handed out.
func panicStale(p unsafe.Pointer) {
	startPanic()
	if tag := *(*uintptr)(unsafe.Pointer(&p)) >> 48; tag >= 0x8000 {
		site := arenaEndedSite(p)
		if site == 0 {
			site = epochSite(tag)
		}
		if site>>63 != 0 {
			printString("runtime error: use of freed memory: an arena allocation freed")
		} else {
			printString("runtime error: use of memory from an arena that was reset")
		}
		if pc := site << 1 >> 1; pc != 0 {
			printString(" at ")
			PrintPC(pc - 1)
		}
		fatal(nil)
	}
	printString("runtime error: use of freed memory: pointer ")
	printUint(uint64(uintptr(p)))
	printString(" has an old generation")
	fatal(nil)
}

func panicDivide() {
	startPanic()
	printString("runtime error: integer divide by zero")
	fatal(nil)
}

func panicShift() {
	startPanic()
	printString("runtime error: negative shift amount")
	fatal(nil)
}

func panicSliceConvert(n, want int) {
	startPanic()
	printString("runtime error: cannot convert slice with length ")
	printInt(int64(n))
	printString(" to array of length ")
	printInt(int64(want))
	fatal(nil)
}

func panicAssert(have, want *typeDesc) {
	startPanic()
	printString("interface conversion: interface is ")
	if have == nil {
		printString("nil")
	} else {
		printString(have.name)
	}
	printString(", not ")
	printString(want.name)
	fatal(nil)
}

// panicValue implements panic(v).
// panicUnion reports a failed assertion u.(U.V) on a union value: union
// holds the variant have (empty if not known), not want.
func panicUnion(union, have, want string) {
	startPanic()
	printString("union variant: ")
	printString(union)
	if have == "" {
		printString(" does not hold ")
	} else {
		printString(" holds ")
		printString(have)
		printString(", not ")
	}
	printString(want)
	fatal(nil)
}

// panicUnionTag reports a union value whose tag names none of its
// variants, which only unsafe code or memory corruption can make.
func panicUnionTag(union string, tag int64) {
	startPanic()
	printString("invalid tag ")
	printInt(tag)
	printString(" in a value of union ")
	printString(union)
	fatal(nil)
}

// panicEnum reports a switch with no default case on a value of an enum
// type that is not one of its members, which only unsafe code, raw bits
// converted to a packed type or generic code on the underlying integer can
// make (language spec section 12.9).
func panicEnum(typ string, v int64) {
	startPanic()
	printString("runtime error: value ")
	printInt(v)
	printString(" of enum ")
	printString(typ)
	printString(" is not a member")
	fatal(nil)
}

func panicValue(v any) {
	startPanic()
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
	fatal(v)
}

// panicRangeState reports an iterator that called the yield function of a
// range-over-func loop when it may not; state is the loop's state, numbered
// as Go's abi.RF_* constants.
func panicRangeState(state int) {
	startPanic()
	printString("runtime error: range function ")
	switch state {
	case 0:
		printString("continued iteration after function for loop body returned false")
	case 2:
		printString("continued iteration after loop body panic")
	case 3:
		printString("continued iteration after whole loop exit")
	default:
		printString("recovered a loop body panic and did not resume panicking")
	}
	fatal(nil)
}

func panicMessage(a, b, c, d, e string) {
	startPanic()
	printString(a)
	printString(b)
	printString(c)
	printString(d)
	printString(e)
	fatal(nil)
}

// ----------------------------------------------------------------------------
// print and println. They write to the console (consoleWrite), except
// while a panic's message is being written (panic.p).

// printBytes writes n bytes at p where print writes.
func printBytes(p unsafe.Pointer, n int) {
	if n <= 0 {
		return
	}
	if capturing && loadUint64(&panicOwner) == selfID()+1 {
		captureBytes(p, n)
		return
	}
	consoleWrite(p, n)
}

func printSpace()   { printString(" ") }
func printNewline() { printString("\n") }

func printBool(b bool) {
	if b {
		printString("true")
	} else {
		printString("false")
	}
}

func printInt(v int64)              { writeInt(Console{}, v) }
func printUint(v uint64)            { writeUint(Console{}, v) }
func printPointer(p unsafe.Pointer) { writePointer(Console{}, uintptr(p)) }

// A Writer receives the text of a panic report (Panic.Print and
// Panic.PrintTrace). It has the method of pluto/io.Writer, so any
// io.Writer is one.
type Writer interface {
	Write(p []byte) (n int, err error)
}

// Console is a Writer that writes where print and println do: standard
// error in hosted builds, with a write system call that never waits for
// the scheduler, and the function registered with SetConsole in
// freestanding builds.
//
// The functions below that format text for a Writer are generic in its
// type, so that writing to the console calls Console.Write directly. The
// interrupt-context check (language spec section 20.10) follows a call
// through the Writer interface into every Writer the program has, which
// may include files that wait for the poller; a direct call reaches only
// the console.
type Console struct{}

// Write writes p to the console.
func (Console) Write(p []byte) (int, error) {
	if len(p) > 0 {
		printBytes(unsafe.Pointer(&p[0]), len(p))
	}
	return len(p), nil
}

// writeBytes writes n bytes at p to w.
func writeBytes[W Writer](w W, p unsafe.Pointer, n int) {
	if n > 0 {
		w.Write(unsafe.Slice((*byte)(p), n))
	}
}

func writeString[W Writer](w W, s string) {
	if len(s) > 0 {
		writeBytes(w, unsafe.Pointer(unsafe.StringData(s)), len(s))
	}
}

func writeInt[W Writer](w W, v int64) {
	if v < 0 {
		writeString(w, "-")
		writeUint(w, uint64(-v))
		return
	}
	writeUint(w, uint64(v))
}

func writeUint[W Writer](w W, v uint64) {
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
	writeBytes(w, unsafe.Pointer(&buf[i]), len(buf)-i)
}

func writePointer[W Writer](w W, v uintptr) {
	const digits = "0123456789abcdef"
	var buf [18]byte
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
	writeBytes(w, unsafe.Pointer(&buf[i]), len(buf)-i)
}

func printString(s string) {
	if len(s) > 0 {
		printBytes(unsafe.Pointer(unsafe.StringData(s)), len(s))
	}
}

// ----------------------------------------------------------------------------
// print statements
//
// A print or println statement is atomic: tasks (and processors) that
// print at the same time never mix their output within a line, as in Go.
// The compiler gives a statement that would write more than once (anything
// but print of one value) a call of printBegin first and of printEnd last,
// and the statement's pieces are written by the stmt functions below
// instead of the print functions above. printBegin takes the print lock;
// the stmt functions append to a fixed buffer; printEnd writes the buffer
// to the console in one call and releases the lock. A statement longer
// than the buffer is written in several calls, still under the lock.
// Nothing here allocates. The lock is a runtime Lock, which in kernel mode
// also masks interrupts on the processor that holds it, so an interrupt
// handler may print: it cannot find the lock held by the code it
// interrupted. The console write happens with the lock held, so a console
// must not wait for another task or for a lock of its own.
//
// The functions the panic machinery uses (printString and the others
// above) take no lock and bypass the buffer, so that a panic can always
// report itself.

// stmtBufSize is the largest print statement written in a single call.
const stmtBufSize = 512

var (
	stmtLock Lock
	stmtBuf  [stmtBufSize]byte
	stmtLen  int
	// stmtOwner is 0, or one more than the selfID of the processor holding
	// stmtLock; stmtNest counts the print statements that processor began
	// while it was already in one (freestanding builds only, see printBegin).
	stmtOwner uint64
	stmtNest  int
)

// printBegin starts a print statement whose pieces the stmt functions
// append to.
func printBegin() {
	if printReentrant && loadUint64(&stmtOwner) == selfID()+1 {
		// A handler of a fault, an interrupt that does not mask others, or
		// a panic prints in the middle of a statement of the code it
		// interrupted, which holds the lock for good as far as the handler
		// is concerned. Write what that statement has so far and print
		// alongside it, without the lock.
		stmtFlush()
		stmtNest++
		return
	}
	stmtLock.Lock()
	if printReentrant {
		casUint64(&stmtOwner, 0, selfID()+1)
	}
}

// printEnd ends the print statement: it writes what is left in the buffer
// and releases the lock.
func printEnd() {
	stmtFlush()
	if stmtNest > 0 {
		stmtNest--
		return
	}
	if printReentrant {
		casUint64(&stmtOwner, selfID()+1, 0)
	}
	stmtLock.Unlock()
}

// stmtFlush writes the buffered output.
func stmtFlush() {
	if n := stmtLen; n > 0 {
		stmtLen = 0
		consoleWrite(unsafe.Pointer(&stmtBuf[0]), n)
	}
}

// stmtBytes appends n bytes at p to the statement's output, writing the
// buffer first when they do not fit, and writing them directly when they
// would not fit in an empty buffer.
func stmtBytes(p unsafe.Pointer, n int) {
	if n <= 0 {
		return
	}
	if n > stmtBufSize-stmtLen {
		stmtFlush()
		if n >= stmtBufSize {
			consoleWrite(p, n)
			return
		}
	}
	memmove(unsafe.Pointer(&stmtBuf[stmtLen]), p, n)
	stmtLen += n
}

// stmtOut is a Writer that appends to the print statement in progress.
type stmtOut struct{}

func (stmtOut) Write(p []byte) (int, error) {
	if len(p) > 0 {
		stmtBytes(unsafe.Pointer(&p[0]), len(p))
	}
	return len(p), nil
}

func stmtString(s string) {
	if len(s) > 0 {
		stmtBytes(unsafe.Pointer(unsafe.StringData(s)), len(s))
	}
}

func stmtSpace()   { stmtString(" ") }
func stmtNewline() { stmtString("\n") }

func stmtBool(b bool) {
	if b {
		stmtString("true")
	} else {
		stmtString("false")
	}
}

func stmtInt(v int64)              { writeInt(stmtOut{}, v) }
func stmtUint(v uint64)            { writeUint(stmtOut{}, v) }
func stmtPointer(p unsafe.Pointer) { writePointer(stmtOut{}, uintptr(p)) }

// ----------------------------------------------------------------------------
// Memory

func memmove(dst, src unsafe.Pointer, n int) {
	if n <= 0 || dst == src {
		return
	}
	// Eight bytes at a time (x86-64 allows unaligned words), then the
	// rest. Forward when dst is below src or the ranges do not overlap,
	// backward otherwise, so every word is read before it is overwritten.
	d := uintptr(dst)
	s := uintptr(src)
	if d < s || d >= s+uintptr(n) {
		i := 0
		for ; i+8 <= n; i += 8 {
			*(*uint64)(unsafe.Add(dst, i)) = *(*uint64)(unsafe.Add(src, i))
		}
		for ; i < n; i++ {
			*(*byte)(unsafe.Add(dst, i)) = *(*byte)(unsafe.Add(src, i))
		}
		return
	}
	i := n
	for ; i >= 8; i -= 8 {
		*(*uint64)(unsafe.Add(dst, i-8)) = *(*uint64)(unsafe.Add(src, i-8))
	}
	for i--; i >= 0; i-- {
		*(*byte)(unsafe.Add(dst, i)) = *(*byte)(unsafe.Add(src, i))
	}
}

func memclr(p unsafe.Pointer, n int) {
	i := 0
	for ; i+8 <= n; i += 8 {
		*(*uint64)(unsafe.Add(p, i)) = 0
	}
	for ; i < n; i++ {
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
	s := unsafe.Slice(p, n) // bytes: this is what decides whether they are UTF-8
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
// task's, if it installed one with UseAllocator, otherwise the
// program-wide default. It returns nil if there is neither.
func DefaultAllocator() Allocator {
	if g := current(); g != nil && g.alloc != nil {
		return g.alloc
	}
	return defaultAllocator
}

// SetDefaultAllocator replaces the program-wide default allocator.
func SetDefaultAllocator(a Allocator) { defaultAllocator = a }

// UseAllocator makes a the running task's default allocator, in place
// of the program-wide default, and returns the allocator it replaced
// (nil if the task had none of its own); UseAllocator(prev) restores
// it. Other tasks, including those this one starts, are not affected
// (language spec section 18.9). Outside a task (in a scheduler's own
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
