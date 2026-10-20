package fmt

import (
	"pluto/rt"
	"unsafe"
)

// Composite values. Plutonium has no reflection; a value converted to an
// interface carries its type descriptor, which records the element type and
// length of arrays and slices, the fields of structs and the type a pointer
// points at (abi.md section 5). printValue walks a value with it, following
// Go's printValue so that the output is Go's byte for byte.

// A Lister is a container that formats as a list of its elements, like a
// slice: [a b c], or T{a, b, c} with %#v. FmtList returns a pointer to the
// first of n elements stored one after another, or nil and 0 if there are
// none. vec.Vec is a Lister.
type Lister interface {
	FmtList() (first any, n int)
}

// A Mapper is a container that formats like a Go map: map[k:v k:v] with its
// keys sorted, or T{k:v, k:v} with %#v. FmtSlots returns the number of
// slots, and FmtSlot pointers to the key and value in slot i, or false if
// the slot is empty. hash.Map is a Mapper.
type Mapper interface {
	FmtSlots() int
	FmtSlot(i int) (k, v any, ok bool)
}

// printValue formats the value of type t stored at ptr. depth is 0 for an
// operand of Printf itself. ro reports that the value was reached through
// an unexported struct field, whose methods are not called (Go's
// CanInterface).
func (p *printer) printValue(t *rt.Type, ptr unsafe.Pointer, verb rune, depth int, ro bool) {
	// An operand's own methods were tried by printArg. An interface is
	// tried for the value it holds, one level down.
	if depth > 0 && !ro && t.HasMethods() && t.Kind() != rt.KindInterface {
		if p.handleMethods(rt.Make(t, t.Word(ptr)), verb) {
			return
		}
	}
	curT, curP, curW := p.curT, p.curP, p.curWord
	p.curT, p.curP, p.curWord = t, uintptr(ptr), false
	p.printKind(t, ptr, verb, depth, ro)
	p.curT, p.curP, p.curWord = curT, curP, curW
}

// printPayload prints a union variant's payload as printArg prints an
// operand, except that %w wraps nothing: only an operand is wrapped.
func (p *printer) printPayload(arg any, verb rune) {
	if verb == 'w' {
		verb = 'v'
	}
	t, word := rt.TypeOf(arg)
	if t == nil {
		p.writeString("<nil>")
		return
	}
	if p.handleMethods(arg, verb) {
		return
	}
	p.printWord(t, word, verb, 0, false)
}

// printWord formats the value of type t held in the data word word of an
// interface (rt.TypeOf): a pointer to the value, or the value's bits if t is
// direct. A direct value that is or holds a pointer is walked from the word
// itself, not from a copy of it in this frame, so that the escape analysis
// follows the pointer to the methods of what it reaches: a copy's address
// would be taken for the receiver of every pointer method of that name, and a
// pointer made from the bits would hide the methods that keep it (language
// spec section 18.3).
func (p *printer) printWord(t *rt.Type, word unsafe.Pointer, verb rune, depth int, ro bool) {
	if !t.Direct() {
		p.printValue(t, word, verb, depth, ro)
		return
	}
	if depth > 0 && !ro && t.HasMethods() && t.Kind() != rt.KindInterface {
		if p.handleMethods(rt.Make(t, word), verb) {
			return
		}
	}
	curT, curP, curW := p.curT, p.curP, p.curWord
	p.curT, p.curP, p.curWord = t, wordBits(word), true
	switch t.Kind() {
	case rt.KindPointer, rt.KindOwned:
		// As printKind prints a pointer.
		if depth == 0 && word != nil {
			if e := t.Elem(); e != nil {
				switch e.Kind() {
				case rt.KindArray, rt.KindSlice, rt.KindStruct:
					p.writeByte('&')
					p.printValue(e, word, verb, depth+1, ro)
					p.curT, p.curP, p.curWord = curT, curP, curW
					return
				}
			}
		}
		p.fmtPointerBits(t, uint64(wordBits(word)), verb)
	case rt.KindUnsafePointer, rt.KindFunc:
		p.fmtPointerBits(t, uint64(wordBits(word)), verb)
	case rt.KindMap:
		if word == nil && p.sharpV {
			p.writeString(t.Name())
			p.writeString("(nil)")
		} else {
			p.printMap(t, builtinMap{t: t, m: word}, verb, depth)
		}
	case rt.KindStruct:
		if t.Size() == 0 {
			p.printWordBits(t, word, verb, depth, ro)
			break
		}
		// A struct of one pointer-shaped field.
		if p.sharpV {
			p.writeString(t.Name())
		}
		p.writeByte('{')
		for i := 0; i < t.Len(); i++ {
			f := t.Field(i)
			p.listSep(i)
			if (p.plusV || p.sharpV) && f.Name != "" {
				p.writeString(f.Name)
				p.writeByte(':')
			}
			p.printWord(f.Type, word, verb, depth+1, ro || !f.Exported)
		}
		p.writeByte('}')
	case rt.KindArray:
		if t.Size() == 0 {
			p.printWordBits(t, word, verb, depth, ro)
			break
		}
		// An array of one pointer-shaped element.
		if p.sharpV {
			p.writeString(t.Name())
			p.writeByte('{')
		} else {
			p.writeByte('[')
		}
		p.printWord(t.Elem(), word, verb, depth+1, ro)
		if p.sharpV {
			p.writeByte('}')
		} else {
			p.writeByte(']')
		}
	default:
		p.printWordBits(t, word, verb, depth, ro)
	}
	p.curT, p.curP, p.curWord = curT, curP, curW
}

// printWordBits formats a direct value that holds no pointer from a copy of its
// bits.
func (p *printer) printWordBits(t *rt.Type, word unsafe.Pointer, verb rune, depth int, ro bool) {
	bits := wordBits(word)
	p.printKind(t, unsafe.Pointer(&bits), verb, depth, ro)
}

// wordBits returns the bits of a data word, which a conversion of the pointer
// to uintptr need not keep when the bits are a float's or a large integer's.
func wordBits(word unsafe.Pointer) uintptr {
	return *(*uintptr)(unsafe.Pointer(&word))
}

// A sliceHeader is the layout of a slice value (abi.md section 1).
type sliceHeader struct {
	data unsafe.Pointer
	n    int
	c    int
}

func (p *printer) printKind(t *rt.Type, ptr unsafe.Pointer, verb rune, depth int, ro bool) {
	switch t.Kind() {
	case rt.KindBool:
		p.printBool(*(*bool)(ptr), verb)
	case rt.KindInt, rt.KindInt8, rt.KindInt16, rt.KindInt32, rt.KindInt64:
		p.printInt(loadInt(ptr, t.Size()), verb)
	case rt.KindUint, rt.KindUint8, rt.KindUint16, rt.KindUint32, rt.KindUint64, rt.KindUintptr:
		p.printNumber(loadUint(ptr, t.Size()), false, verb)
	case rt.KindFloat32:
		p.printFloatBits(uint64(*(*uint32)(ptr)), 32, verb)
	case rt.KindFloat64:
		p.printFloatBits(*(*uint64)(ptr), 64, verb)
	case rt.KindString:
		p.printString(*(*string)(ptr), verb)
	case rt.KindStruct:
		p.printStruct(t, ptr, verb, depth, ro)
	case rt.KindArray:
		p.printList(t, ptr, t.Len(), false, verb, depth, ro)
	case rt.KindSlice:
		// The data pointer is read as a pointer, so that the escape analysis
		// follows the elements to the methods of their types (language spec
		// section 18.3).
		s := (*sliceHeader)(ptr)
		p.printList(t, s.data, s.n, s.data == nil, verb, depth, ro)
	case rt.KindInterface:
		dt, word := t.IfaceAt(ptr)
		if dt == nil {
			if p.sharpV {
				p.writeString(t.Name())
				p.writeString("(nil)")
			} else {
				p.writeString("<nil>")
			}
			return
		}
		p.printWord(dt, word, verb, depth+1, ro)
	case rt.KindPointer, rt.KindOwned:
		// A pointer to an array, slice or struct operand prints as &value;
		// deeper pointers print as addresses, which also stops cycles.
		target := *(*unsafe.Pointer)(ptr)
		if depth == 0 && target != nil {
			if e := t.Elem(); e != nil {
				switch e.Kind() {
				case rt.KindArray, rt.KindSlice, rt.KindStruct:
					p.writeByte('&')
					p.printValue(e, target, verb, depth+1, ro)
					return
				}
			}
		}
		p.fmtPointer(t, ptr, verb)
	case rt.KindUnsafePointer, rt.KindFunc:
		p.fmtPointer(t, ptr, verb)
	case rt.KindUnion:
		// The variant's name, then its payload in parentheses.
		variant, payload, hasPayload := rt.UnionInfo(rt.Make(t, t.Word(ptr)))
		p.writeString(variant)
		if hasPayload {
			p.writeByte('(')
			p.printPayload(payload, verb)
			p.writeByte(')')
		}
	case rt.KindPacked:
		if t.Direct() {
			p.printNumber(loadUint(ptr, t.Size()), false, verb)
			return
		}
		p.printOpaque(t)
	case rt.KindMap:
		m := *(*unsafe.Pointer)(ptr)
		if m == nil && p.sharpV {
			p.writeString(t.Name())
			p.writeString("(nil)")
			return
		}
		p.printMap(t, builtinMap{t: t, m: m}, verb, depth)
	default:
		p.printOpaque(t)
	}
}

// printOpaque prints a value fmt cannot look into, such as a ring, as its
// type's name in braces.
func (p *printer) printOpaque(t *rt.Type) {
	p.writeByte('{')
	p.writeString(t.Name())
	p.writeByte('}')
}

// loadInt reads a signed integer of size bytes at ptr.
func loadInt(ptr unsafe.Pointer, size uintptr) int64 {
	switch size {
	case 1:
		return int64(*(*int8)(ptr))
	case 2:
		return int64(*(*int16)(ptr))
	case 4:
		return int64(*(*int32)(ptr))
	}
	return *(*int64)(ptr)
}

// loadUint reads an unsigned integer of size bytes at ptr.
func loadUint(ptr unsafe.Pointer, size uintptr) uint64 {
	switch size {
	case 1:
		return uint64(*(*uint8)(ptr))
	case 2:
		return uint64(*(*uint16)(ptr))
	case 4:
		return uint64(*(*uint32)(ptr))
	}
	return *(*uint64)(ptr)
}

// fmtPointer formats the pointer of type t stored at ptr, or the address of
// a slice's first element.
func (p *printer) fmtPointer(t *rt.Type, ptr unsafe.Pointer, verb rune) {
	switch t.Kind() {
	case rt.KindPointer, rt.KindUnsafePointer, rt.KindFunc, rt.KindOwned, rt.KindSlice:
	default:
		p.badVerb(verb)
		return
	}
	p.fmtPointerBits(t, uint64(*(*uintptr)(ptr)), verb)
}

// fmtPointerBits is fmtPointer for the pointer's bits u.
func (p *printer) fmtPointerBits(t *rt.Type, u uint64, verb rune) {
	switch t.Kind() {
	case rt.KindPointer, rt.KindUnsafePointer, rt.KindFunc, rt.KindOwned, rt.KindSlice:
	default:
		p.badVerb(verb)
		return
	}
	switch verb {
	case 'v':
		switch {
		case p.sharpV:
			p.writeByte('(')
			p.writeString(t.Name())
			p.writeString(")(")
			if u == 0 {
				p.writeString("nil")
			} else {
				p.fmt0x64(u, true)
			}
			p.writeByte(')')
		case u == 0:
			p.fieldString("<nil>")
		default:
			p.fmt0x64(u, !p.sharp)
		}
	case 'p':
		p.fmt0x64(u, !p.sharp)
	case 'b', 'o', 'd', 'x', 'X':
		p.printNumber(u, false, verb)
	default:
		p.badVerb(verb)
	}
}

// fmtBytes formats a []byte operand, or the bytes of an array or slice of
// bytes for %s, %q, %x and %X, as Go's fmtBytes does. typeName is the type
// for %#v; t and ptr are the value, for other verbs.
func (p *printer) fmtBytes(v []byte, verb rune, typeName string, t *rt.Type, ptr unsafe.Pointer) {
	switch verb {
	case 'v', 'd':
		if p.sharpV {
			p.writeString(typeName)
			if v == nil {
				p.writeString("(nil)")
				return
			}
			p.writeByte('{')
			for i, c := range v {
				if i > 0 {
					p.writeString(", ")
				}
				p.fmt0x64(uint64(c), true)
			}
			p.writeByte('}')
			return
		}
		p.writeByte('[')
		for i, c := range v {
			if i > 0 {
				p.writeByte(' ')
			}
			p.fmtInteger(uint64(c), false, 10, verb, false)
		}
		p.writeByte(']')
	case 's', 'q', 'x', 'X':
		// A view of the bytes for the printer to quote, hex-encode or write;
		// it is not stored or returned, and the printer reads invalid UTF-8
		// as U+FFFD (the result is a string only through rt.StringFromBytes,
		// which replaces invalid bytes).
		p.printString(unsafe.String(unsafe.SliceData(v), len(v)), verb)
	default:
		p.printKind(t, ptr, verb, 0, false)
	}
}

// printList formats an array or slice of type t whose n elements start at
// data. isNil reports a nil slice.
func (p *printer) printList(t *rt.Type, data unsafe.Pointer, n int, isNil bool, verb rune, depth int, ro bool) {
	et := t.Elem()
	switch verb {
	case 's', 'q', 'x', 'X':
		if et.Kind() == rt.KindUint8 {
			// A view of the bytes, as for []byte above.
			p.printString(unsafe.String((*byte)(data), n), verb)
			return
		}
	}
	if p.sharpV {
		p.writeString(t.Name())
		if isNil && t.Kind() == rt.KindSlice {
			p.writeString("(nil)")
			return
		}
		p.writeByte('{')
	} else {
		p.writeByte('[')
	}
	for i := 0; i < n; i++ {
		p.listSep(i)
		p.printValue(et, unsafe.Add(data, uintptr(i)*et.Size()), verb, depth+1, ro)
	}
	if p.sharpV {
		p.writeByte('}')
	} else {
		p.writeByte(']')
	}
}

// listSep writes the separator before element i of a list.
func (p *printer) listSep(i int) {
	if i == 0 {
		return
	}
	if p.sharpV {
		p.writeString(", ")
	} else {
		p.writeByte(' ')
	}
}

// printStruct formats a struct, or a container that is a Lister or Mapper
// through its pointer.
func (p *printer) printStruct(t *rt.Type, ptr unsafe.Pointer, verb rune, depth int, ro bool) {
	if pt := t.PtrTo(); pt != nil {
		switch c := rt.Make(pt, ptr).(type) {
		case Mapper:
			p.printMap(t, mapperSlots{c}, verb, depth)
			return
		case Lister:
			p.printLister(t, c, verb, depth)
			return
		}
	}
	if p.sharpV {
		p.writeString(t.Name())
	}
	p.writeByte('{')
	for i := 0; i < t.Len(); i++ {
		f := t.Field(i)
		p.listSep(i)
		if (p.plusV || p.sharpV) && f.Name != "" {
			p.writeString(f.Name)
			p.writeByte(':')
		}
		p.printValue(f.Type, unsafe.Add(ptr, f.Offset), verb, depth+1, ro || !f.Exported)
	}
	p.writeByte('}')
}

// printLister formats a container of type t that lists its elements.
func (p *printer) printLister(t *rt.Type, c Lister, verb rune, depth int) {
	first, n := c.FmtList()
	if p.sharpV {
		p.writeString(t.Name())
		p.writeByte('{')
	} else {
		p.writeByte('[')
	}
	if n > 0 {
		pt, data := rt.TypeOf(first)
		et := pt.Elem()
		for i := 0; i < n; i++ {
			p.listSep(i)
			p.printValue(et, unsafe.Add(data, uintptr(i)*et.Size()), verb, depth+1, false)
		}
	}
	if p.sharpV {
		p.writeByte('}')
	} else {
		p.writeByte(']')
	}
}

// mapSlots is a table of key-value pairs that printMap walks: a Mapper, or
// a built-in map.
type mapSlots interface {
	slots() int
	// slot returns the types and addresses of the key and value in slot
	// i, if it is full.
	slot(i int) (kt *rt.Type, k unsafe.Pointer, vt *rt.Type, v unsafe.Pointer, ok bool)
}

// mapperSlots walks a Mapper.
type mapperSlots struct{ m Mapper }

func (s mapperSlots) slots() int { return s.m.FmtSlots() }

func (s mapperSlots) slot(i int) (*rt.Type, unsafe.Pointer, *rt.Type, unsafe.Pointer, bool) {
	k, v, ok := s.m.FmtSlot(i)
	if !ok {
		return nil, nil, nil, nil, false
	}
	kt, kp := rt.TypeOf(k)
	vt, vp := rt.TypeOf(v)
	return kt.Elem(), kp, vt.Elem(), vp, true
}

// builtinMap walks the table of a map[K]V of type t, at m (nil for a nil
// map).
type builtinMap struct {
	t *rt.Type
	m unsafe.Pointer
}

func (b builtinMap) slots() int { return rt.MapSlots(b.m) }

func (b builtinMap) slot(i int) (*rt.Type, unsafe.Pointer, *rt.Type, unsafe.Pointer, bool) {
	k, v, ok := rt.MapSlot(b.t, b.m, i)
	if !ok {
		return nil, nil, nil, nil, false
	}
	return b.t.Field(0).Type, k, b.t.Field(1).Type, v, true
}

// printMap formats a container of type t that holds key-value pairs, in
// increasing order of key as Go's fmt sorts map keys. It allocates nothing:
// each step scans the slots for the least key after the previous one.
func (p *printer) printMap(t *rt.Type, m mapSlots, verb rune, depth int) {
	if p.sharpV {
		p.writeString(t.Name())
		p.writeByte('{')
	} else {
		p.writeString("map[")
	}
	slots := m.slots()
	last := -1
	var lastK unsafe.Pointer
	for count := 0; ; count++ {
		best := -1
		var kt, vt *rt.Type
		var bestK, bestV unsafe.Pointer
		for i := 0; i < slots; i++ {
			skt, k, svt, v, ok := m.slot(i)
			if !ok {
				continue
			}
			// Order by key, then by slot, so that equal keys (NaNs) each
			// print once.
			if last >= 0 {
				if c := compareValues(skt, k, lastK); c < 0 || c == 0 && i <= last {
					continue
				}
			}
			if best >= 0 && compareValues(skt, k, bestK) >= 0 {
				continue
			}
			best, kt, bestK, vt, bestV = i, skt, k, svt, v
		}
		if best < 0 {
			break
		}
		p.listSep(count)
		p.printValue(kt, bestK, verb, depth+1, false)
		p.writeByte(':')
		p.printValue(vt, bestV, verb, depth+1, false)
		last, lastK = best, bestK
	}
	if p.sharpV {
		p.writeByte('}')
	} else {
		p.writeByte(']')
	}
}

// compareValues orders two values of type t as Go's fmt orders map keys:
// numbers and strings by value, false before true, pointers by address,
// arrays and structs element by element, and interfaces with nil first,
// then by the dynamic type's name, then by value.
func compareValues(t *rt.Type, a, b unsafe.Pointer) int {
	switch t.Kind() {
	case rt.KindInt, rt.KindInt8, rt.KindInt16, rt.KindInt32, rt.KindInt64:
		return compareOrdered(loadInt(a, t.Size()), loadInt(b, t.Size()))
	case rt.KindUint, rt.KindUint8, rt.KindUint16, rt.KindUint32, rt.KindUint64, rt.KindUintptr,
		rt.KindPointer, rt.KindUnsafePointer, rt.KindOwned, rt.KindPacked:
		return compareOrdered(loadUint(a, t.Size()), loadUint(b, t.Size()))
	case rt.KindBool:
		return compareOrdered(loadUint(a, 1), loadUint(b, 1))
	case rt.KindFloat32:
		return compareFloatBits(uint64(*(*uint32)(a)), uint64(*(*uint32)(b)), 32)
	case rt.KindFloat64:
		return compareFloatBits(*(*uint64)(a), *(*uint64)(b), 64)
	case rt.KindString:
		return compareOrdered(*(*string)(a), *(*string)(b))
	case rt.KindArray:
		et := t.Elem()
		for i := 0; i < t.Len(); i++ {
			off := uintptr(i) * et.Size()
			if c := compareValues(et, unsafe.Add(a, off), unsafe.Add(b, off)); c != 0 {
				return c
			}
		}
	case rt.KindStruct:
		for i := 0; i < t.Len(); i++ {
			f := t.Field(i)
			if c := compareValues(f.Type, unsafe.Add(a, f.Offset), unsafe.Add(b, f.Offset)); c != 0 {
				return c
			}
		}
	case rt.KindInterface:
		at, aw := t.IfaceAt(a)
		bt, bw := t.IfaceAt(b)
		switch {
		case at == nil && bt == nil:
			return 0
		case at == nil:
			return -1
		case bt == nil:
			return 1
		case at != bt:
			return compareOrdered(at.Name(), bt.Name())
		}
		pa, pb := aw, bw
		if at.Direct() {
			pa, pb = unsafe.Pointer(&aw), unsafe.Pointer(&bw)
		}
		return compareValues(at, pa, pb)
	}
	return 0
}

func compareOrdered[T int64 | uint64 | string](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// compareFloatBits orders two floats given by their bits as cmp.Compare
// does (NaNs first and equal to each other, -0 equal to 0), using only
// integer instructions so that it works in kernel mode.
func compareFloatBits(a, b uint64, bitSize int) int {
	sign := uint64(1) << 63
	expMask := uint64(0x7FF) << 52
	if bitSize == 32 {
		sign = 1 << 31
		expMask = 0xFF << 23
	}
	isNaN := func(x uint64) bool { return x&expMask == expMask && x&^(sign|expMask) != 0 }
	switch an, bn := isNaN(a), isNaN(b); {
	case an && bn:
		return 0
	case an:
		return -1
	case bn:
		return 1
	}
	if a&^sign == 0 && b&^sign == 0 {
		return 0 // -0 and +0
	}
	// Map the bits to unsigned integers in the floats' order.
	key := func(x uint64) uint64 {
		if x&sign != 0 {
			return ^x
		}
		return x | sign
	}
	if bitSize == 32 {
		key = func(x uint64) uint64 {
			if x&sign != 0 {
				return ^x & 0xFFFFFFFF
			}
			return x | sign
		}
	}
	return compareOrdered(key(a), key(b))
}
