// Translated by go2pu from internal/escape/walkkey.go at the go-cutover tag; edited by hand since.

package escape

import (
	"pluto/strings"
	"selfhost/compat"
	"selfhost/internal/syntax/token"
	"selfhost/internal/types2"
	"selfhost/pu"
	"unsafe"
)

// Following a path from a set of pointers.
//
// Every step of a path (a load, a field offset, a type filter) maps each
// pointer to the pointers it leads to, whatever the other pointers of the set
// are. walkKey therefore follows the path from one pointer at a time, which
// needs no set between steps; a set built of the results has the pointers in
// the order that building each step's set in turn gives, since a pointer's
// repeats lead where its first occurrence did.

// load returns what the memory the pointers in ts point to may hold.
func (w *walker) load(ts tset) tset {
	var out tbuf
	for _, t := range ts {
		loadTok(t, &out)
	}
	return out.done()
}

// loadTok adds to out what the memory t points to may hold.
func loadTok(t tok, out *tbuf) {
	switch t.kind {
	case tHeap:
		out.add(heapTok)
	case tFrame:
		if t.loc.kind == lkObject || t.loc.kind == lkArena || t.loc.object {
			out.add(heapTok) // what a box's object or an arena holds is heap memory
			return
		}
		for _, u := range t.loc.getShared(t.path) {
			out.add(t.loaded(u))
		}
	case tParam:
		out.add(t.untag().extend("*"))
	}
}

// offset returns pointers to field i of what the pointers in ts point to.
// Fields are told apart two levels deep: a field of a field of a field
// keeps the path of its enclosing one, which stands for everything inside.
func offset(ts tset, i int) tset {
	var out tbuf
	for _, t := range ts {
		out.add(offsetTok(t, i))
	}
	return out.done()
}

// offsetTok returns the pointer to field i of what t points to.
func offsetTok(t tok, i int) tok {
	t = t.untag()
	if t.shape >= shapeOffset {
		return t // a pointer into the object, at an offset it does not tell
	}
	if t.kind != tHeap && fieldTail(t.path) >= maxFieldNest {
		return t
	}
	switch t.kind {
	case tFrame:
		t.path = compat.Concat(t.path, fieldStep(i))
	case tParam:
		t = t.extend(fieldStep(i))
	}
	return t
}

// A dynFilter applies a type assertion to interface I (see filterDyn).
type dynFilter struct {
	I     types2.Type
	iface *types2.Interface
	exact bool // I is a concrete type, the receiver of a method
	ptr   bool // the dynamic type must also be a pointer (see ptrToFilter)
	// ptrTo, for a filter ptrToFilter made, is the type the dynamic type
	// must be a pointer to.
	ptrTo types2.Type
	noop  bool // the filter keeps every pointer
}

// ptrFilterPkg is the package of the types ptrToFilter makes.
var ptrFilterPkg = types2.NewPackage("escape.ptrfilter", "ptrfilter")

// ptrToFilter returns the filter for a data word that is the receiver of a
// pointer method of T, not promoted: the dynamic type is *T. It is a named
// type with *T as its underlying type, one for each T.
func (a *analyzer) ptrToFilter(T types2.Type) types2.Type {
	if p, ok := a.ptrFilters[T]; ok {
		return p
	}
	if a.ptrFilters == nil {
		a.ptrFilters = map[types2.Type]types2.Type{}
	}
	name := compat.Concat("ptr(", types2.TypeString(T, nil), ")")
	p := types2.NewNamed(types2.NewTypeName(token.NoPos, ptrFilterPkg, name, nil), types2.NewPointer(T), nil)
	a.ptrFilters[T] = p
	return p
}

func newDynFilter(I types2.Type) dynFilter {
	iface, _ := I.Underlying().(*types2.Interface)
	if n, ok := I.(*types2.Named); ok && n.Obj().Pkg() == ptrFilterPkg {
		if p, ok := n.Underlying().(*types2.Pointer); ok {
			return dynFilter{I: I, ptr: true, ptrTo: p.Elem()}
		}
		return dynFilter{I: I, iface: iface, ptr: true, noop: false}
	}
	exact := iface == nil
	return dynFilter{I: I, iface: iface, exact: exact,
		noop: exact && containsTypeParam(I) || !exact && iface.Empty()}
}

// keep applies the filter to t and reports whether t stays.
func (d *dynFilter) keep(t tok) (tok, bool) {
	switch t.kind {
	case tFrame:
		if t.path == "" && t.interior() {
			// A pointer into the location: of the type of something in it.
			if vt := t.loc.valueType(); vt != nil && !containsTypeParam(vt) {
				if d.ptrTo != nil {
					// A *T to a T in the location, a box included: pluto/fmt
					// walks a box by its descriptor and makes a *T to each
					// struct it reaches (rt.Make with PtrTo), to call a
					// container's pointer methods.
					if !holdsType(vt, d.ptrTo, 0) {
						return t, false
					}
					return t, true
				}
				if d.exact && !holdsType(vt, d.I, 0) {
					return t, false
				}
				// A pointer into a location is a pointer of the type of
				// something in it. In a box it may also be the address of a
				// boxed value of that type in an interface; and it may be a
				// pointer all the same, which pluto/fmt's walk makes (above).
				if !d.exact {
					asPointer := holdsImplementer(vt, d.iface, false, 0)
					asBoxedValue := t.loc.box && !d.ptr && holdsImplementer(vt, d.iface, true, 0)
					if !asPointer && !asBoxedValue {
						return t, false
					}
				}
			}
		} else if dyn := t.loc.dynamicType(); t.path == "" && dyn != nil && t.shape == shapeWhole && !containsTypeParam(dyn) {
			if d.ptrTo != nil {
				// A *T, or a box holding a T (fmt's *T to the box, above).
				isPtr := types2.Identical(dyn, types2.NewPointer(d.ptrTo))
				inBox := t.loc.box && holdsType(dyn, d.ptrTo, 0)
				if !isPtr && !inBox {
					return t, false
				}
				return t, true
			}
			if d.ptr {
				if _, isPtr := dyn.Underlying().(*types2.Pointer); !isPtr {
					return t, false
				}
			}
			if d.exact && !types2.Identical(dyn, d.I) && !types2.Identical(dyn, types2.NewPointer(d.I)) {
				return t, false
			}
			if !d.exact && !types2.Implements(dyn, d.iface) {
				return t, false
			}
		}
	case tParam:
		if t.filt == nil && !t.ge && t.shape != shapeOther {
			t.filt, t.filtAt, t.filtShape = d.I, len(t.path), t.shape
		}
	}
	return t, true
}

// filterDyn applies a type assertion to interface I to the data words in
// ts: a frame box or a pointer to a frame variable whose type does not
// implement I cannot be there. Parameter tokens carry the filter on to
// the caller.
//
// I may also be a concrete type, the receiver of a method a dynamic call
// may reach: then only data words whose dynamic type has that method as its
// own (I itself, or a pointer to I) are kept.
func (a *analyzer) filterDyn(ts tset, I types2.Type, _ int) tset {
	d := newDynFilter(I)
	if d.noop {
		return ts
	}
	var out tbuf
	for _, t := range ts {
		if t, ok := d.keep(t); ok {
			out.add(t)
		}
	}
	return out.done()
}

// walkKey follows k's path from the pointers in ts, applying its type
// filter where the path reaches the interface value it constrains.
func (w *walker) walkKey(ts tset, k pkey) tset {
	if len(ts) == 0 || k.path == "" && k.filt == nil && !k.ge && k.shape == shapeWhole {
		return ts
	}
	var d dynFilter
	if k.filt != nil {
		d = newDynFilter(k.filt)
	}
	var out tbuf
	for _, t := range ts {
		w.walkTok(t, k, &d, 0, &out)
	}
	if !k.ge {
		return out.done()
	}
	return w.reach(out.done())
}

// deriveKey follows a key from a pointer to a parameter of the caller and
// returns the key of the summary that the pointer found is recorded as,
// without making the pointer:
// it follows the key's path in buf (the path of t, then each step), which is
// what extend and offsetTok do to a token, without a string for every step.
// With preLoad, the pointer is loaded first. The path of the key returned
// lies in buf, and is valid until buf is used again; use strings.Clone to
// keep it.
func (w *walker) deriveKey(t tok, k pkey, preLoad bool, buf *[64]byte) pkey {
	kw := &keyWalk{b: compat.AppendString(buf[:0], t.path), first: -1,
		ge: t.ge, filt: t.filt, filtAt: t.filtAt, filtShape: t.filtShape, sh: t.shape}
	for i := 0; i < len(kw.b); i++ {
		switch kw.b[i] {
		case '*':
			kw.depth++
			kw.steps++
			kw.tail = 0
			if kw.first < 0 {
				kw.first = i
			}
		case '.':
			kw.steps++
			kw.tail++
		}
	}
	var d *dynFilter
	if k.filt != nil {
		d = w.a.dynFilterFor(k.filt)
	}
	over := preLoad && kw.star()
	p := k.path
	for pos := 0; !over; {
		if d != nil && pos == k.filtAt && !d.noop {
			// The pointer the filter judges is of the shape the key says it
			// had there, into what the pointer followed so far points to
			// (walkTok). A pointer that stands for every extension of its
			// path has no filter, and its shape says nothing.
			if !kw.ge {
				kw.sh = max(kw.sh, k.filtShape)
			}
			if kw.filt == nil && !kw.ge && kw.sh != shapeOther {
				kw.filt, kw.filtAt, kw.filtShape = d.I, len(kw.b), kw.sh
			}
		}
		if pos >= len(p) {
			break
		}
		if p[pos] == '*' {
			pos++
			over = kw.star()
			continue
		}
		start := pos
		for p[pos] != '.' {
			pos++
		}
		pos++
		if kw.tail >= maxFieldNest || kw.ge || kw.sh >= shapeOffset {
			continue
		}
		if kw.steps+1 > maxSteps {
			kw.overflowed()
			break
		}
		kw.b = compat.AppendString(kw.b, strings.Slice(p, start, pos))
		kw.steps++
		kw.tail++
	}
	if k.ge {
		kw.ge = true
	}
	shape := max(kw.sh, k.shape)
	key := pkey{idx: t.idx, counts: t.counted, directCount: t.directCount, borrowed: t.borrowed || k.borrowed,
		ge: kw.ge, shape: shape, filt: kw.filt, filtAt: kw.filtAt, filtShape: kw.filtShape}
	if len(kw.b) > 0 {
		key.path = unsafe.String(&kw.b[0], len(kw.b))
	}
	return key
}

// keyWalk is deriveKey's state as it follows a key's path: the path built
// in b, its loads (depth), steps, field steps since the last load (tail),
// the index of its first load, and the GE flag, filter and shape of the
// pointer followed. Its methods are steps of deriveKey (methods, not
// closures, so that the escape analysis of the self-hosted compiler resolves
// their calls).
type keyWalk struct {
	b                         []byte
	depth, steps, tail, first int
	ge                        bool
	filt                      types2.Type
	filtAt                    int
	filtShape, sh             ptrShape
}

// overflowed is extend's cutting of a path that has become too long, back
// to its first load and one more.
func (kw *keyWalk) overflowed() {
	kw.ge = true
	if kw.first >= 0 {
		kw.b = compat.AppendPlain(kw.b[:kw.first+1], '*')
	} else {
		kw.b = kw.b[:0]
	}
	if kw.filtAt > len(kw.b) {
		kw.filt, kw.filtAt, kw.filtShape = nil, 0, shapeWhole // the cut lost where the filter applied (see tok.extend)
	}
}

// star is the step of a load; it reports whether the path overflowed.
func (kw *keyWalk) star() bool {
	if kw.ge {
		return false
	}
	wide := kw.sh >= shapeOffset // what a load through a pointer at an offset finds (tok.extend)
	kw.sh = shapeWhole
	if kw.depth+1 > maxDepth || kw.steps+1 > maxSteps {
		kw.overflowed()
		return true
	}
	kw.b = compat.AppendPlain(kw.b, '*')
	if kw.first < 0 {
		kw.first = len(kw.b) - 1
	}
	kw.depth++
	kw.steps++
	kw.tail = 0
	if wide {
		kw.ge = true
	}
	return false
}

// structPrelude is walkParam's choice of a field of a struct parameter for
// one pointer: the pointer and key to follow, if the pointer is held in that
// field.
func (w *walker) structPrelude(t tok, k pkey, T types2.Type) (tok, pkey, bool) {
	for isStructValue(T) {
		i, rest, ok := cutFieldStep(k.path)
		if !ok {
			break
		}
		n := len(k.path) - len(rest)
		switch {
		case t.tag == "":
		case strings.HasPrefix(t.tag, strings.Slice(k.path, 0, n)):
			t.tag = strings.SliceFrom(t.tag, n)
		default:
			return t, k, false
		}
		T = fieldType(T, i)
		k.path = rest
		if k.filt != nil {
			if k.filtAt >= n {
				k.filtAt -= n
			} else {
				k.filt, k.filtAt, k.filtShape = nil, 0, shapeWhole
			}
		}
	}
	return t, k, true
}

// dynFilterFor returns the filter for type assertions to I.
func (a *analyzer) dynFilterFor(I types2.Type) *dynFilter {
	d := a.dynFilters[I]
	if d == nil {
		if a.dynFilters == nil {
			a.dynFilters = map[types2.Type]*dynFilter{}
		}
		f := pu.New(newDynFilter(I))
		d = f
		a.dynFilters[I] = d
	}
	return d
}

// walkTok follows k's path from t, from byte pos of the path, and adds the
// pointers it ends at to out.
func (w *walker) walkTok(t tok, k pkey, d *dynFilter, pos int, out *tbuf) {
	p := k.path
	for {
		if k.filt != nil && pos == k.filtAt && !d.noop {
			// The pointer the callee judged there may be one into what this
			// one points to (an element it took, &p[1]): it is of the shape
			// the key says.
			if !t.ge {
				t = t.as(k.filtShape)
			}
			var ok bool
			if t, ok = d.keep(t); !ok {
				return
			}
		}
		if pos >= len(p) {
			out.add(t.as(k.shape)) // the pointer found is of the shape the key says, after the filter
			return
		}
		if p[pos] == '*' {
			pos++
			switch t.kind {
			case tHeap:
				t = heapTok
			case tParam:
				t = t.untag().extend("*")
			case tFrame:
				if t.loc.kind == lkObject || t.loc.kind == lkArena || t.loc.object {
					t = heapTok
					continue
				}
				for _, u := range t.loc.getShared(t.path) {
					w.walkTok(t.loaded(u), k, d, pos, out)
				}
				return
			}
			continue
		}
		i := 0
		for p[pos] != '.' {
			i = i*10 + int(p[pos]-'0')
			pos++
		}
		pos++
		t = offsetTok(t, i)
	}
}
