package lower

import (
	"encoding/binary"
	"hash/fnv"
	"sort"

	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Interface values are two words: a table pointer and a data word.
//
// For the empty interface the table is the dynamic type's descriptor; for
// other interfaces it is an itab, whose first word is the descriptor.
//
// A dynamic type is "direct" when its value fits in the data word (integers,
// floats, booleans, pointers, and structs and arrays that are a single
// pointer, types2.PointerShaped); the data word then holds the value's bits.
// Otherwise the data word points at a copy of the value in the stack frame of
// the function that made the interface value.
//
// Type descriptor layout (80 bytes), mirrored by pluto/rt.typeDesc (abi.md
// section 5). Its strings are static three-word strings (strrep.go).
//
//	0  size   uintptr
//	8  hash   uint32
//	12 kind   uint8
//	13 flags  uint8   (1 = direct, 2 = interface with methods, 4 = has methods)
//	14 align  uint8
//	16 name   string  (as Go's reflect writes it)
//	40 equal  func(a, b unsafe.Pointer) bool   (nil if not comparable or direct)
//	48 elem   *typeDesc  (array, slice, pointer, ring, owned and counted references)
//	56 len    uintptr    (array length, ring capacity, number of struct fields)
//	64 fields *fieldDesc (struct fields, or nil)
//	72 ptrTo  *typeDesc  (*T, if *T has methods T lacks, or nil)
//
// Field descriptor layout (48 bytes), an array of them per struct type:
//
//	0  typ    *typeDesc
//	8  offset uintptr
//	16 name   string  (empty when the build strips field names)
//	40 flags  uint8   (1 = exported, 2 = embedded)
//
// The descriptors of a type's elements, fields and pointer are emitted with
// it, so that pluto/fmt can walk a value of the type; only types converted
// to an interface, and the types they are made of, have descriptors.
//
// The descriptor of a union (kind 30) continues, mirrored by
// pluto/rt.unionDesc, with
//
//	80 payload  uintptr        offset of the payload
//	88 variants *[n]variant    one entry per variant, in tag order
//	96 count    int            n
//
// and each variant entry (40 bytes) is
//
//	0  name  string
//	24 typ   *typeDesc        the payload's descriptor; nil without a payload
//	32 iface uintptr          1 if the payload is an empty interface, 2 another interface
//
// Itab layout: type descriptor, interface descriptor, then one code pointer
// per interface method in method order. Each code pointer is an adapter
// taking the data word as its first argument.

const (
	descName      = 16
	descEqual     = descName + strSize
	descElem      = descEqual + 8
	descNum       = descElem + 8
	descFields    = descNum + 8
	descPtrTo     = descFields + 8
	descSize      = descPtrTo + 8
	unionDescSize = descSize + 24
	variantSize   = strSize + 16
	variantTyp    = strSize
	variantIface  = strSize + 8
	fieldDescSize = 16 + strSize + 8
	fieldName     = 16
	fieldFlags    = fieldName + strSize
	itabHeader    = 16

	flagDirect       = 1
	flagIfaceMethods = 2
	flagMethods      = 4

	fieldExported = 1
	fieldEmbedded = 2
)

// Kind numbers in type descriptors (Go's reflect numbering, plus Plutonium's).
const (
	kindBool = 1 + iota
	kindInt
	kindInt8
	kindInt16
	kindInt32
	kindInt64
	kindUint
	kindUint8
	kindUint16
	kindUint32
	kindUint64
	kindUintptr
	kindFloat32
	kindFloat64
	_
	_
	kindArray
	kindChan
	kindFunc
	kindInterface
	_
	kindPointer
	kindSlice
	kindString
	kindStruct
	kindUnsafePointer
	kindPacked
	kindRing
	kindOwned
	kindUnion
)

type descInfo struct {
	T      types2.Type
	sym    *ssa.Sym
	equal  *ssa.Sym // closure record of the equality function, or nil
	eqDone bool     // equal has been decided

	elem   *ssa.Sym // element or pointee descriptor, or nil
	n      int64    // array length, ring capacity or number of fields
	fields []fieldInfo
	ptrTo  *ssa.Sym // descriptor of *T, or nil
}

// A fieldInfo is one entry of a struct type's field descriptors.
type fieldInfo struct {
	typ      *ssa.Sym
	offset   int64
	name     string
	exported bool
	embedded bool
}

type itabInfo struct {
	T, I    types2.Type
	sym     *ssa.Sym
	methods []*ssa.Sym // adapters, in interface method order
}

// typeTable collects the type descriptors and itabs the program uses.
type typeTable struct {
	l        *Lowerer
	descs    map[string]*descInfo
	itabs    map[string]*itabInfo
	asserted map[string]types2.Type // interfaces that values are asserted to
	done     map[string]bool        // (type, interface) pairs already considered
}

func newTypeTable(l *Lowerer) *typeTable {
	return &typeTable{
		l:        l,
		descs:    map[string]*descInfo{},
		itabs:    map[string]*itabInfo{},
		asserted: map[string]types2.Type{},
		done:     map[string]bool{},
	}
}

// isDirect reports whether a T fits in an interface's data word: a scalar of
// at most a word, or a struct or array that is a single pointer
// (types2.PointerShaped), stored as that pointer.
func (l *Lowerer) isDirect(T types2.Type) bool {
	T = unwrap(T)
	if t := l.scalarType(T); t != nil {
		return t.Size <= 8
	}
	return types2.PointerShaped(T)
}

// desc returns the descriptor symbol of T, a type converted to an
// interface.
func (tt *typeTable) desc(T types2.Type) *ssa.Sym {
	return tt.descOf(T, true)
}

// descOf returns the descriptor symbol of T. A descriptor made only because
// T is an element, field or pointee of a converted type has no equality
// function (eq is false) until T itself is converted.
func (tt *typeTable) descOf(T types2.Type, eq bool) *ssa.Sym {
	key := typeString(T)
	d := tt.descs[key]
	if d == nil {
		d = &descInfo{T: T, sym: tt.l.out.Sym("type:"+key, ssa.SymData)}
		tt.descs[key] = d
		tt.describe(d)
	}
	if eq && !d.eqDone {
		d.eqDone = true
		if !tt.l.isDirect(T) && types2.Comparable(T) && !isInterface(T) {
			d.equal = tt.l.closureRecord(tt.l.equalFunc(T))
		}
	}
	if u, ok := T.Underlying().(*types2.TaggedUnion); ok {
		// fmt prints a union's variant and payload from its descriptor.
		for i := 0; i < u.NumVariants(); i++ {
			if v := u.Variant(i); v.HasPayload() {
				tt.desc(v.Payload())
			}
		}
	}
	return d.sym
}

// describe fills in the parts of d that refer to other types: its element,
// fields and pointer type, whose descriptors are made too.
func (tt *typeTable) describe(d *descInfo) {
	T := d.T
	switch u := T.Underlying().(type) {
	case *types2.Array:
		d.elem, d.n = tt.descOf(unwrap(u.Elem()), false), u.Len()
	case *types2.Slice:
		d.elem = tt.descOf(unwrap(u.Elem()), false)
	case *types2.Pointer:
		d.elem = tt.descOf(unwrap(u.Elem()), false)
	case *types2.Wrapper:
		if u.IsPointer() || u.Kind() == types2.WrapRing {
			d.elem = tt.descOf(unwrap(u.Elem()), false)
		}
		if u.Kind() == types2.WrapRing {
			d.n = u.N()
		}
	case *types2.Struct:
		vars := make([]*types2.Var, u.NumFields())
		for i := range vars {
			vars[i] = u.Field(i)
		}
		offs := tt.l.sizes.Offsetsof(vars)
		d.n = int64(len(vars))
		for i, f := range vars {
			fi := fieldInfo{
				typ:      tt.descOf(unwrap(f.Type()), false),
				offset:   offs[i],
				exported: f.Exported(),
				embedded: f.Embedded(),
			}
			if !tt.l.cfg.NoFieldNames {
				fi.name = f.Name()
			}
			d.fields = append(d.fields, fi)
		}
	}
	// pluto/fmt reaches the methods of *T, such as a container's formatting
	// methods, through ptrTo when it has the address of a T.
	if n, ok := types2.Unalias(T).(*types2.Named); ok && !isInterface(T) && !isPointerLike(T) {
		P := types2.NewPointer(T)
		if types2.NewMethodSet(P).Len() > types2.NewMethodSet(n).Len() {
			d.ptrTo = tt.descOf(P, false)
		}
	}
}

// unionDesc fills in the union part of the descriptor data of a union type
// u (named key): the payload offset and the table of variants.
func (tt *typeTable) unionDesc(data *ssa.Data, key string, u *types2.TaggedUnion) {
	l := tt.l
	_, off := types2.UnionLayout(l.sizes, u)
	n := u.NumVariants()
	binary.LittleEndian.PutUint64(data.Bytes[descSize:], uint64(off))
	binary.LittleEndian.PutUint64(data.Bytes[descSize+16:], uint64(n))
	table := &ssa.Data{Sym: l.out.Sym("variants:"+key, ssa.SymData), Size: int64(variantSize * n), Align: 8, ReadOnly: true}
	table.Bytes = make([]byte, table.Size)
	for i := range n {
		v := u.Variant(i)
		base := int64(variantSize * i)
		l.putStaticString(table, base, v.Name())
		if !v.HasPayload() {
			continue
		}
		P := v.Payload()
		table.Relocs = append(table.Relocs, ssa.DataReloc{Off: base + variantTyp, Sym: tt.descs[typeString(P)].sym})
		if iface, ok := P.Underlying().(*types2.Interface); ok {
			kind := uint64(2)
			if iface.Empty() {
				kind = 1
			}
			binary.LittleEndian.PutUint64(table.Bytes[base+variantIface:], kind)
		}
	}
	l.out.Data = append(l.out.Data, table)
	data.Relocs = append(data.Relocs, ssa.DataReloc{Off: descSize + 8, Sym: table.Sym})
}

// itab returns the itab symbol for concrete type T and interface I.
func (tt *typeTable) itab(T, I types2.Type) *ssa.Sym {
	key := typeString(T) + "," + typeString(I)
	if it := tt.itabs[key]; it != nil {
		return it.sym
	}
	it := &itabInfo{T: T, I: I, sym: tt.l.out.Sym("itab:"+key, ssa.SymData)}
	tt.itabs[key] = it
	tt.desc(T)
	tt.desc(I)
	iface := I.Underlying().(*types2.Interface)
	for i := 0; i < iface.NumMethods(); i++ {
		it.methods = append(it.methods, tt.l.itabAdapter(T, iface.Method(i)))
	}
	return it.sym
}

// finish creates itabs for every pair of a concrete type that was converted
// to an interface and an interface that some value is asserted to, so the
// runtime can find them. It reports whether it queued new work.
func (tt *typeTable) finish() bool {
	before := len(tt.l.queue)
	var concrete []types2.Type
	for _, d := range tt.descs {
		if !isInterface(d.T) {
			concrete = append(concrete, d.T)
		}
	}
	sort.Slice(concrete, func(i, j int) bool { return typeString(concrete[i]) < typeString(concrete[j]) })
	var ifaces []string
	for k := range tt.asserted {
		ifaces = append(ifaces, k)
	}
	sort.Strings(ifaces)
	for _, ik := range ifaces {
		I := tt.asserted[ik]
		iface := I.Underlying().(*types2.Interface)
		for _, T := range concrete {
			key := typeString(T) + "," + ik
			if tt.done[key] {
				continue
			}
			tt.done[key] = true
			if types2.Implements(T, iface) {
				tt.itab(T, I)
			}
		}
	}
	return len(tt.l.queue) > before
}

// emit writes the descriptors, itabs and the itab table.
func (tt *typeTable) emit() {
	l := tt.l
	var keys []string
	for k := range tt.descs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		d := tt.descs[k]
		descLen := int64(descSize)
		u, isUnion := d.T.Underlying().(*types2.TaggedUnion)
		if isUnion {
			descLen = unionDescSize
		}
		data := &ssa.Data{Sym: d.sym, Size: descLen, Align: 8, ReadOnly: true}
		buf := make([]byte, descLen)
		binary.LittleEndian.PutUint64(buf[0:], uint64(l.sizeof(d.T)))
		binary.LittleEndian.PutUint32(buf[8:], descHash(k))
		buf[12] = kindOf(d.T)
		if l.isDirect(d.T) {
			buf[13] |= flagDirect
		}
		if isInterface(d.T) {
			if !d.T.Underlying().(*types2.Interface).Empty() {
				buf[13] |= flagIfaceMethods
			}
		} else if types2.NewMethodSet(d.T).Len() > 0 {
			buf[13] |= flagMethods
		}
		if a := l.alignof(d.T); a < 256 {
			buf[14] = byte(a)
		}
		name := reflectName(d.T)
		binary.LittleEndian.PutUint64(buf[descNum:], uint64(d.n))
		data.Bytes = buf
		l.putStaticString(data, descName, name)
		for _, r := range []struct {
			off int64
			sym *ssa.Sym
		}{{descEqual, d.equal}, {descElem, d.elem}, {descFields, tt.fieldsData(k, d)}, {descPtrTo, d.ptrTo}} {
			if r.sym != nil {
				data.Relocs = append(data.Relocs, ssa.DataReloc{Off: r.off, Sym: r.sym})
			}
		}
		if isUnion {
			tt.unionDesc(data, k, u)
		}
		l.out.Data = append(l.out.Data, data)
	}

	keys = keys[:0]
	for k := range tt.itabs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// The itab table is an open-addressing hash table keyed by the two
	// descriptors' hashes (see itabSlots), probed linearly; an empty slot
	// ends a probe.
	hashes := make([][2]uint32, len(keys))
	for i, k := range keys {
		it := tt.itabs[k]
		hashes[i] = [2]uint32{descHash(typeString(it.T)), descHash(typeString(it.I))}
	}
	slots := itabSlots(hashes)
	table := &ssa.Data{Sym: l.out.Sym("pluto.itabs", ssa.SymData), Align: 8, ReadOnly: true}
	table.Size = int64(8 * (1 + len(slots)))
	table.Bytes = make([]byte, 8)
	binary.LittleEndian.PutUint64(table.Bytes, uint64(len(slots)))
	for _, k := range keys {
		it := tt.itabs[k]
		size := int64(itabHeader + 8*len(it.methods))
		data := &ssa.Data{Sym: it.sym, Size: size, Align: 8, ReadOnly: true}
		data.Relocs = append(data.Relocs,
			ssa.DataReloc{Off: 0, Sym: tt.descs[typeString(it.T)].sym},
			ssa.DataReloc{Off: 8, Sym: tt.descs[typeString(it.I)].sym})
		for j, m := range it.methods {
			data.Relocs = append(data.Relocs, ssa.DataReloc{Off: int64(itabHeader + 8*j), Sym: m})
		}
		l.out.Data = append(l.out.Data, data)
	}
	for j, i := range slots {
		if i >= 0 {
			table.Relocs = append(table.Relocs, ssa.DataReloc{Off: int64(8 * (j + 1)), Sym: tt.itabs[keys[i]].sym})
		}
	}
	l.out.Data = append(l.out.Data, table)
}

// descHash is the hash stored in a type descriptor: FNV-1a of the type's
// key string.
func descHash(key string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(key))
	return h.Sum32()
}

// itabHash combines the descriptor hashes of a concrete type and an
// interface into a starting slot. pluto/rt's findItab computes the same.
func itabHash(typ, iface uint32) uint32 {
	h := typ*0x9E3779B1 ^ iface
	h ^= h >> 15
	h *= 0x85EBCA6B
	h ^= h >> 13
	return h
}

// itabSlots lays out the itab table: a power-of-two number of slots, at
// least twice the number of itabs (so probes stay short and at least one
// slot is empty), each holding the index of an itab in hashes or -1. Itab i
// goes in the first free slot from itabHash(hashes[i]), wrapping around.
func itabSlots(hashes [][2]uint32) []int {
	size := 1
	for size < 2*len(hashes) {
		size <<= 1
	}
	slots := make([]int, size)
	for j := range slots {
		slots[j] = -1
	}
	for i, h := range hashes {
		j := int(itabHash(h[0], h[1])) & (size - 1)
		for slots[j] >= 0 {
			j = (j + 1) & (size - 1)
		}
		slots[j] = i
	}
	return slots
}

// fieldsData emits the field descriptors of the struct type d describes,
// whose descriptor key is key, and returns their symbol; nil for a type
// without fields.
func (tt *typeTable) fieldsData(key string, d *descInfo) *ssa.Sym {
	if len(d.fields) == 0 {
		return nil
	}
	l := tt.l
	sym := l.out.Sym("typefields:"+key, ssa.SymData)
	data := &ssa.Data{Sym: sym, Size: int64(fieldDescSize * len(d.fields)), Align: 8, ReadOnly: true}
	data.Bytes = make([]byte, data.Size)
	for i, f := range d.fields {
		off := int64(fieldDescSize * i)
		data.Relocs = append(data.Relocs, ssa.DataReloc{Off: off, Sym: f.typ})
		binary.LittleEndian.PutUint64(data.Bytes[off+8:], uint64(f.offset))
		l.putStaticString(data, off+fieldName, f.name)
		var flags byte
		if f.exported {
			flags |= fieldExported
		}
		if f.embedded {
			flags |= fieldEmbedded
		}
		data.Bytes[off+fieldFlags] = flags
	}
	l.out.Data = append(l.out.Data, data)
	return sym
}

func kindOf(T types2.Type) byte {
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		switch u.Kind() {
		case types2.Bool:
			return kindBool
		case types2.Int:
			return kindInt
		case types2.Int8:
			return kindInt8
		case types2.Int16:
			return kindInt16
		case types2.Int32:
			return kindInt32
		case types2.Int64:
			return kindInt64
		case types2.Uint:
			return kindUint
		case types2.Uint8:
			return kindUint8
		case types2.Uint16:
			return kindUint16
		case types2.Uint32:
			return kindUint32
		case types2.Uint64:
			return kindUint64
		case types2.Uintptr:
			return kindUintptr
		case types2.Float32:
			return kindFloat32
		case types2.Float64:
			return kindFloat64
		case types2.String:
			return kindString
		case types2.UnsafePointer:
			return kindUnsafePointer
		}
	case *types2.Array:
		return kindArray
	case *types2.Signature:
		return kindFunc
	case *types2.Chan:
		return kindChan
	case *types2.Interface:
		return kindInterface
	case *types2.Pointer:
		return kindPointer
	case *types2.Slice:
		return kindSlice
	case *types2.Struct:
		return kindStruct
	case *types2.Packed:
		return kindPacked
	case *types2.TaggedUnion:
		return kindUnion
	case *types2.Wrapper:
		if u.Kind() == types2.WrapRing {
			return kindRing
		}
		if u.IsPointer() {
			return kindOwned
		}
		return kindOf(u.Elem())
	}
	return 0
}

// ----------------------------------------------------------------------------
// Making interface values

// toInterface converts a concrete value of type from to interface type I.
func (s *fnState) toInterface(v value, from, I types2.Type) value {
	if b, ok := from.Underlying().(*types2.Basic); ok && b.Kind() == types2.UntypedNil {
		return s.zeroValue(I)
	}
	from = unwrap(from)
	if b, ok := from.Underlying().(*types2.Basic); ok && b.Info()&types2.IsUntyped != 0 {
		// An untyped value (a comparison result, a shift of a constant)
		// takes its default type.
		switch {
		case b.Info()&types2.IsBoolean != 0:
			from = types2.Typ[types2.Bool]
		case b.Info()&types2.IsFloat != 0:
			from = types2.Typ[types2.Float64]
		default:
			from = types2.Typ[types2.Int]
		}
	}
	if t := s.l.scalarType(from); t != nil && t.IsFloat() && s.f.NoSSE {
		s.floatError(t, "conversion to an interface")
	}
	var tab *ssa.Value
	if I.Underlying().(*types2.Interface).Empty() {
		tab = s.symAddr(s.l.types.desc(from))
	} else {
		tab = s.symAddr(s.l.types.itab(from, I))
	}
	data := s.dataWord(v, from)
	tmp := s.temp(I, "iface")
	s.store(ssa.TPtr, tmp, tab)
	s.store(ssa.TPtr, s.offPtr(tmp, 8), data)
	return aggValue(tmp, I)
}

// dataWord returns an interface data word for a value of concrete type T.
func (s *fnState) dataWord(v value, T types2.Type) *ssa.Value {
	if s.l.isDirect(T) {
		x := v.v
		if v.addr {
			// A struct or array holding one pointer: the pointer is the
			// data word.
			return s.load(ssa.TPtr, x)
		}
		switch {
		case x.Type.IsPtr():
			return x
		case x.Type.IsFloat():
			x = s.newValue(ssa.OpBitcast, ssa.IntType(x.Type.Size), x)
		}
		if x.Type.Size < 8 || x.Type.IsBool() {
			x = s.newValue(ssa.OpZeroExt, ssa.TI64, x)
		}
		return s.newValue(ssa.OpBitcast, ssa.TPtr, x)
	}
	if s.staticLits {
		// During package initialisation the interface value outlives the
		// init function, so the box gets static storage, like &T{...}.
		box := s.symAddr(s.l.staticObject(T))
		s.assign(lvalue{ptr: box, T: T}, v, true)
		return box
	}
	if s.l.counted(T) {
		// The box may be used until the function returns, so it holds
		// references of its own, released then (or when the conversion
		// runs again and refills it).
		box := s.frameSlot(T, "box")
		s.storeCounted(lvalue{ptr: box, T: T}, v, false)
		return box
	}
	// Box the value in this frame.
	box := s.lastingTemp(T, "box")
	s.storeTo(box, v, T)
	return box
}

// fromDataWord recovers a T from an interface data word.
func (s *fnState) fromDataWord(data *ssa.Value, T types2.Type) value {
	T = unwrap(T)
	if s.l.isDirect(T) {
		t := s.l.scalarType(T)
		if t == nil {
			// A struct or array holding one pointer: rebuild it around the
			// data word.
			tmp := s.temp(T, "unbox")
			s.store(ssa.TPtr, tmp, data)
			return aggValue(tmp, T)
		}
		if t.IsPtr() {
			return scalarValue(data, T)
		}
		x := s.newValue(ssa.OpBitcast, ssa.TI64, data)
		if t.Size < 8 || t.IsBool() {
			it := ssa.IntType(t.Size)
			if t.IsBool() {
				it = ssa.TI8
			}
			x = s.newValue(ssa.OpTrunc, it, x)
		}
		switch {
		case t.IsFloat():
			x = s.newValue(ssa.OpBitcast, t, x)
		case t.IsBool():
			x = s.newValue(ssa.OpNe, ssa.TBool, x, s.constInt(ssa.TI8, 0))
		}
		return scalarValue(x, T)
	}
	return s.loadFrom(data, T)
}

// ifaceParts returns an interface value's table and data words.
func (s *fnState) ifaceParts(v value) (tab, data *ssa.Value) {
	return s.load(ssa.TPtr, v.v), s.load(ssa.TPtr, s.offPtr(v.v, 8))
}

// typeDescOf returns the type descriptor of an interface value's dynamic
// type (nil for a nil interface).
func (s *fnState) typeDescOf(tab *ssa.Value, I types2.Type) *ssa.Value {
	if I.Underlying().(*types2.Interface).Empty() {
		return tab
	}
	// Non-empty: load itab.type, guarding nil.
	res := s.temp(types2.Typ[types2.UnsafePointer], "desc")
	s.store(ssa.TPtr, res, s.constInt(ssa.TPtr, 0))
	notNil := s.newValue(ssa.OpNe, ssa.TBool, tab, s.constInt(ssa.TPtr, 0))
	s.ifThen(notNil, func() { s.store(ssa.TPtr, res, s.load(ssa.TPtr, tab)) })
	return s.load(ssa.TPtr, res)
}

// ifaceToIface converts between interface types.
func (s *fnState) ifaceToIface(v value, to types2.Type, pos ast.Node) value {
	from := v.T
	tab, data := s.ifaceParts(v)
	toIface := to.Underlying().(*types2.Interface)
	tmp := s.temp(to, "iface")
	if toIface.Empty() {
		s.store(ssa.TPtr, tmp, s.typeDescOf(tab, from))
		s.store(ssa.TPtr, s.offPtr(tmp, 8), data)
		return aggValue(tmp, to)
	}
	// The static types guarantee the dynamic type implements `to`; find
	// its itab at run time.
	s.l.types.asserted[typeString(to)] = to
	desc := s.typeDescOf(tab, from)
	itab := s.callRuntime("convI2I", []*ssa.Type{ssa.TPtr}, []*ssa.Value{desc, s.symAddr(s.l.types.desc(to))})[0]
	s.store(ssa.TPtr, tmp, itab)
	s.store(ssa.TPtr, s.offPtr(tmp, 8), data)
	return aggValue(tmp, to)
}

// ifaceCall calls method fn on interface value iv. desc is the call as
// written (w.Write), for the interrupt-context check.
func (s *fnState) ifaceCall(iv value, I types2.Type, fn *types2.Func, sig *types2.Signature, args []value, pos token.Pos, desc string) []value {
	iface := I.Underlying().(*types2.Interface)
	idx := -1
	for i := 0; i < iface.NumMethods(); i++ {
		if iface.Method(i).Name() == fn.Name() && iface.Method(i).Pkg() == fn.Pkg() || iface.Method(i) == fn {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.l.internalf(pos, "method %s not found in %s", fn.Name(), I)
		return nil
	}
	tab, data := s.ifaceParts(iv)
	code := s.load(ssa.TPtr, s.offPtr(tab, int64(itabHeader+8*idx)))
	adapterSig := adapterSignature(sig)
	recv := scalarValue(data, types2.Typ[types2.UnsafePointer])
	site := &callSite{desc: desc, iface: I, method: idx}
	return s.emitCall(callee{code: code, ctx: s.constInt(ssa.TPtr, 0), site: site}, adapterSig, &recv, args, pos)
}

// adapterSignature is sig with an unsafe.Pointer receiver: the data word.
func adapterSignature(sig *types2.Signature) *types2.Signature {
	recv := types2.NewVar(token.NoPos, nil, "data", types2.Typ[types2.UnsafePointer])
	return types2.NewSignatureType(recv, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
}

// itabAdapter returns the function an itab slot points at for method m of
// interface I, implemented by concrete type T: it takes the data word,
// recovers the receiver and calls T's method.
func (l *Lowerer) itabAdapter(T types2.Type, m *types2.Func) *ssa.Sym {
	name := typeString(T) + "." + m.Name() + "·i"
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	obj, index, _ := types2.LookupFieldOrMethod(T, false, m.Pkg(), m.Name())
	fn, ok := obj.(*types2.Func)
	if !ok {
		l.internalf(token.NoPos, "%s has no method %s", T, m.Name())
		return sym
	}
	if isIfaceMethod(fn) {
		// Promoted from an embedded interface field: call through it.
		sig := fn.Type().(*types2.Signature)
		plain := types2.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
		adapter := adapterSignature(plain)
		l.queue = append(l.queue, &funcJob{sym: sym, sig: adapter, build: func(s *fnState) {
			in := s.incomingArgs(adapter)
			iface := recvBase(T, index)
			iv := s.receiverFromData(in[0].v, T, index, iface)
			s.returnValues(s.ifaceCall(iv, iface, fn, plain, in[1:], token.NoPos, fn.Name()))
		}})
		return sym
	}
	// The method's signature as seen through T (instantiated receiver).
	var targs []types2.Type
	if n, ok := types2.Unalias(derefType(recvBase(T, index))).(*types2.Named); ok && n.TypeArgs() != nil {
		for i := 0; i < n.TypeArgs().Len(); i++ {
			targs = append(targs, n.TypeArgs().At(i))
		}
	}
	target, full := l.methodFunc(fn, targs, ringMethodRing(fn, func(t types2.Type) types2.Type { return t }), token.NoPos, "called through an interface")
	plain := types2.NewSignatureType(nil, nil, nil, full.Params(), full.Results(), full.Variadic())
	adapter := adapterSignature(plain)
	l.queue = append(l.queue, &funcJob{sym: sym, sig: adapter, build: func(s *fnState) {
		in := s.incomingArgs(adapter)
		data := in[0].v
		recv := s.receiverFromData(data, T, index, full.Recv().Type())
		res := s.emitCall(callee{sym: target}, full, &recv, in[1:], token.NoPos)
		s.returnValues(res)
	}})
	return sym
}

// recvBase returns the type that declares the method found by index in T.
func recvBase(T types2.Type, index []int) types2.Type {
	cur := T
	for _, i := range index[:len(index)-1] {
		st := derefType(cur).Underlying().(*types2.Struct)
		cur = st.Field(i).Type()
	}
	return cur
}

func derefType(T types2.Type) types2.Type {
	if p, ok := T.Underlying().(*types2.Pointer); ok {
		return p.Elem()
	}
	return T
}

// receiverFromData builds the receiver for a method of T reached by index
// from an interface data word.
func (s *fnState) receiverFromData(data *ssa.Value, T types2.Type, index []int, recvT types2.Type) value {
	_, wantPtr := recvT.(*types2.Pointer)
	if len(index) == 1 {
		switch {
		case wantPtr:
			return scalarValue(data, recvT) // T is the pointer type
		case isPointerLike(T):
			// A value method called through a pointer: dereference.
			return s.hold(s.materialize(s.loadFrom(data, pointerElem(T))))
		}
		// The receiver is borrowed from the box, which the interface's
		// holder keeps alive for the call.
		v := s.materialize(s.fromDataWord(data, T))
		if v.addr {
			v.cnt = cntStable
		}
		return v
	}
	// A promoted method: walk the embedded fields.
	var cur lvalue
	if isPointerLike(T) {
		cur = lvalue{ptr: data, T: pointerElem(T)}
	} else if s.l.isDirect(T) {
		tmp := s.temp(T, "recv")
		s.storeTo(tmp, s.fromDataWord(data, T), T)
		cur = lvalue{ptr: tmp, T: T}
	} else {
		cur = lvalue{ptr: data, T: T}
	}
	cur = s.fieldPath(cur, index[:len(index)-1])
	if isPointerLike(unwrap(cur.T)) {
		cur = lvalue{ptr: s.loadLV(cur).v, T: pointerElem(unwrap(cur.T))}
	}
	if wantPtr {
		return scalarValue(cur.ptr, recvT)
	}
	return s.hold(s.materialize(s.loadLV(cur)))
}

// ifaceMethodValueWrapper synthesizes the code of a method value taken from
// an interface: the record holds the interface value after the code pointer.
func (l *Lowerer) ifaceMethodValueWrapper(I types2.Type, fn *types2.Func) *ssa.Sym {
	name := typeString(I) + "." + fn.Name() + "-fm"
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	// I's own method has the signature as instantiated in I, which fn,
	// selected in generic code, may not.
	iface := I.Underlying().(*types2.Interface)
	for i := 0; i < iface.NumMethods(); i++ {
		if m := iface.Method(i); m.Name() == fn.Name() && m.Pkg() == fn.Pkg() {
			fn = m
		}
	}
	sig := fn.Type().(*types2.Signature)
	plain := types2.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
	l.funcValues[sym] = plain
	l.queue = append(l.queue, &funcJob{sym: sym, sig: plain, build: func(s *fnState) {
		ctx := s.newValue(ssa.OpClosurePtr, ssa.TPtr)
		args := s.incomingArgs(plain)
		iv := aggValue(s.offPtr(ctx, 8), I)
		s.returnValues(s.ifaceCall(iv, I, fn, plain, args, token.NoPos, fn.Name()))
	}})
	return sym
}

// ----------------------------------------------------------------------------
// Assertions and equality

// typeAssert lowers x.(T). With commaOk it returns the value and a bool;
// otherwise a failed assertion panics.
func (s *fnState) typeAssert(e *ast.TypeAssertExpr, commaOk bool) (value, *ssa.Value) {
	if types2.UnionVariants(s.typeOf(e.X)) != nil {
		return s.unionAssert(e, commaOk)
	}
	iv := s.expr(e.X)
	I := s.typeOf(e.X)
	T := s.subst(s.info.Types[e.Type].Type)
	return s.assertTo(iv, I, T, commaOk, e.Pos())
}

func (s *fnState) assertTo(iv value, I, T types2.Type, commaOk bool, pos token.Pos) (value, *ssa.Value) {
	tab, data := s.ifaceParts(iv)
	if isInterface(T) {
		var ok *ssa.Value
		var res value
		if T.Underlying().(*types2.Interface).Empty() {
			desc := s.typeDescOf(tab, I)
			ok = s.newValue(ssa.OpNe, ssa.TBool, desc, s.constInt(ssa.TPtr, 0))
			tmp := s.temp(T, "iface")
			s.store(ssa.TPtr, tmp, desc)
			s.store(ssa.TPtr, s.offPtr(tmp, 8), data)
			res = aggValue(tmp, T)
		} else {
			s.l.types.asserted[typeString(T)] = T
			desc := s.typeDescOf(tab, I)
			itab := s.callRuntime("assertItab", []*ssa.Type{ssa.TPtr}, []*ssa.Value{desc, s.symAddr(s.l.types.desc(T))})[0]
			ok = s.newValue(ssa.OpNe, ssa.TBool, itab, s.constInt(ssa.TPtr, 0))
			tmp := s.temp(T, "iface")
			s.store(ssa.TPtr, tmp, itab)
			s.store(ssa.TPtr, s.offPtr(tmp, 8), data)
			res = aggValue(tmp, T)
		}
		if !commaOk {
			s.panicUnless(ok, pos, func() {
				s.callRuntimeNoReturn("panicAssert", s.typeDescOf(tab, I), s.symAddr(s.l.types.desc(T)))
			})
			return res, nil
		}
		zero := s.zeroValue(T)
		out := s.temp(T, "assert")
		s.storeTo(out, zero, T)
		s.ifThen(ok, func() { s.storeTo(out, res, T) })
		return s.loadFrom(out, T), ok
	}

	ok := s.typeMatches(tab, I, T)
	if !commaOk {
		s.panicUnless(ok, pos, func() {
			s.callRuntimeNoReturn("panicAssert", s.typeDescOf(tab, I), s.symAddr(s.l.types.desc(T)))
		})
		return s.fromDataWord(data, T), nil
	}
	out := s.temp(T, "assert")
	s.storeTo(out, s.zeroValue(T), T)
	s.ifThen(ok, func() { s.storeTo(out, s.fromDataWord(data, T), T) })
	return s.loadFrom(out, T), ok
}

// typeMatches tests whether an interface table denotes concrete type T.
func (s *fnState) typeMatches(tab *ssa.Value, I, T types2.Type) *ssa.Value {
	var want *ssa.Value
	iface := I.Underlying().(*types2.Interface)
	if iface.Empty() {
		want = s.symAddr(s.l.types.desc(T))
	} else if !types2.Implements(T, iface) {
		// Possible in generic code: an I never holds a T.
		return s.constBool(false)
	} else {
		want = s.symAddr(s.l.types.itab(T, I))
	}
	return s.newValue(ssa.OpEq, ssa.TBool, tab, want)
}

// ifaceEqual compares two interface values of the same interface type.
func (s *fnState) ifaceEqual(x, y value, I types2.Type) *ssa.Value {
	xt, xd := s.ifaceParts(x)
	yt, yd := s.ifaceParts(y)
	xdesc := s.typeDescOf(xt, I)
	ydesc := s.typeDescOf(yt, I)
	return s.callRuntime("ifaceEqual", []*ssa.Type{ssa.TBool}, []*ssa.Value{xdesc, xd, ydesc, yd})[0]
}

// equalFunc synthesizes func(a, b unsafe.Pointer) bool comparing two T
// values in memory, for interface equality on non-direct types.
func (l *Lowerer) equalFunc(T types2.Type) *ssa.Sym {
	name := "eq:" + typeString(T)
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	up := types2.Typ[types2.UnsafePointer]
	params := types2.NewTuple(types2.NewParam(token.NoPos, nil, "a", up), types2.NewParam(token.NoPos, nil, "b", up))
	results := types2.NewTuple(types2.NewParam(token.NoPos, nil, "", types2.Typ[types2.Bool]))
	sig := types2.NewSignatureType(nil, nil, nil, params, results, false)
	l.funcValues[sym] = sig
	l.queue = append(l.queue, &funcJob{sym: sym, sig: sig, build: func(s *fnState) {
		in := s.incomingArgs(sig)
		a := s.loadFrom(in[0].v, T)
		b := s.loadFrom(in[1].v, T)
		eq := s.equal(a, b, T, token.NoPos)
		s.returnValues([]value{scalarValue(eq, types2.Typ[types2.Bool])})
	}})
	return sym
}
