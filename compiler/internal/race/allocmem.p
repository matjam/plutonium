package race

import (
	"compiler/compat"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strings"
)

// Memory allocated from an allocator (language spec sections 18.9 and
// 19.4, Memory reached through views). An arena takes back in bulk what it
// handed out (Reset, Release, Drop) and may then hand it out again, so a
// view of an allocation from &a is a view of the memory of a, wherever a
// is held, and a call that takes memory back in bulk is a write of all of
// it. Alloc and Free are not: by section 18.9, Alloc never takes back
// memory it handed out and Free takes back only the memory it is given,
// so an allocation never meets a live view of another. The place
// allocMem(R) stands for the memory the allocator at place R has handed
// out. It lies past a load, so the allocator's own fields do not hold it
// (a task may read a.Used() while another allocates), and R# does.
//
// A call of a bulk method of an allocator type is recorded, in the
// method's summary, as an atomic write of its receiver's memory. In the
// check of scoped tasks, a use of memory reached through a value built on
// the allocator (a vector holding &a, or a pointer into a's buffer) is
// also a plain read of the allocator's memory, which conflicts with a bulk
// call of another party unless both hold a common lock. A task's
// allocation from an allocator of its own is the task's memory, as
// before: allocMem of a variable the task declares is the task's own. The
// default allocator, which does not take back what it handed out (section
// 18.9), needs nothing: the runtime's and pluto/alloc's allocators are not
// counted.

// memStep is the step that names an allocator's memory. It is a field step
// no program can write.
const memStep = ".<mem>"

// allocMem returns the place of the memory the allocator at place r has
// handed out.
func allocMem(r vpath) vpath { return r.join(compat.Concat("*", memStep)) }

// isAllocMem reports whether p is the memory of an allocator.
func isAllocMem(p vpath) bool { return strings.HasSuffix(p.path, memStep) }

// allocMemOwner returns the allocator whose memory p is.
func allocMemOwner(p vpath) vpath {
	p.path = strings.TrimSuffix(strings.TrimSuffix(p.path, memStep), "*")
	return p
}

// userAllocator reports whether T is an allocator type outside the runtime
// and pluto/alloc, whose allocators are the default allocator's.
func (a *analyzer) userAllocator(T types2.Type) bool {
	n, ok := types2.Unalias(T).(*types2.Named)
	if !ok || n.Obj() == nil || n.Obj().Pkg() == nil {
		return false
	}
	switch p := n.Obj().Pkg().Path(); {
	case p == "pluto/rt", strings.HasPrefix(p, "pluto/rt/"), p == "pluto/alloc":
		return false
	}
	return implementsAllocator(a, T)
}

// recycler reports whether f is a method of an allocator type that takes
// back in bulk memory the allocator handed out.
func (a *analyzer) recycler(f *function) bool {
	if f.obj == nil || f.sig == nil || f.sig.Recv() == nil || f.bodyless || len(f.params) == 0 {
		return false
	}
	switch f.obj.Name() {
	case "Reset", "Release", "Drop":
	default:
		return false
	}
	el := pointerElem(f.sig.Recv().Type())
	return el != nil && a.userAllocator(el)
}

// noteRecycle records, in the summary of a bulk method of an allocator, the write of all the memory its receiver handed out.
func (w *walker) noteRecycle() {
	if w.frame != nil || !w.a.recycler(w.fn) {
		return
	}
	var pos token.Pos
	if w.fn.decl != nil {
		pos = w.fn.decl.Name.Pos()
	}
	recv := vpath{root: root{kind: rParam, idx: 0}}.join("*")
	w.access(allocMem(recv), true, true, pos, nil, false, "")
}

// noteAllocMem records, in the check of scoped tasks, the read of an
// allocator's memory that use x makes when it reaches memory allocated
// from it: x is a use through variable v, past a load, and v is the
// allocator or holds its address in a field of allocator type.
func (fc *frameCheck) noteAllocMem(w *walker, x *frameAcc) {
	r := x.place.root
	if r.kind != rLocal && r.kind != rUnknown || r.v == nil {
		return
	}
	if isAllocMem(x.place) {
		fc.mapAllocMem(w, x)
		return
	}
	if !strings.ContainsAny(x.place.path, "*%$#") {
		return
	}
	for _, R := range fc.allocatorsOf(w, r) {
		fc.add(w.party, pu.New(frameAcc{place: allocMem(R), locks: x.locks, pos: x.pos, via: x.via, typ: x.typ, run: x.run, wrote: x.write}))
	}
}

// mapAllocMem carries a bulk call made through a field of variable v that
// holds an allocator's address (h.a.Reset() where h.a holds &a) over to
// that allocator's memory: the memory of v.f* is the memory of a when v.f
// holds &a.
func (fc *frameCheck) mapAllocMem(w *walker, x *frameAcc) {
	owner := allocMemOwner(x.place)
	if !strings.HasSuffix(owner.path, "*") {
		return
	}
	fp := strings.TrimSuffix(owner.path, "*")
	if fp == "" {
		return
	}
	for _, s := range steps(fp) {
		if !strings.HasPrefix(s, ".") {
			return
		}
	}
	for _, o := range w.u.vars[owner.root.v] {
		if o.tag != fp || !strings.HasSuffix(o.vp.path, "&") {
			continue
		}
		switch o.vp.root.kind {
		case rLocal, rGlobal, rParam:
		default:
			continue
		}
		if o.vp.root.kind == rGlobal && fc.a.runtimeVar(o.vp.root.v) {
			continue
		}
		y := *x
		y.place = allocMem(o.vp.join("*"))
		fc.add(w.party, pu.New(y))
	}
}

// allocatorsOf returns the allocators whose memory a value reached through
// root r may be in: r's variable itself, when it is an allocator, and the
// allocators whose addresses it holds in fields of allocator type.
func (fc *frameCheck) allocatorsOf(w *walker, r root) []vpath {
	var out []vpath
	v := r.v
	if r.kind == rLocal && fc.a.userAllocator(v.Type()) {
		out = compat.Append(out, vpath{root: r})
	}
	for _, o := range w.u.vars[v] {
		if o.tag == "" || !strings.HasSuffix(o.vp.path, "&") {
			continue
		}
		switch o.vp.root.kind {
		case rLocal, rGlobal, rParam:
		default:
			continue
		}
		if o.vp.root.kind == rGlobal && fc.a.runtimeVar(o.vp.root.v) {
			continue
		}
		ft := fieldTypeAt(v.Type(), o.tag)
		if ft == nil {
			continue
		}
		ok := false
		if el := pointerElem(ft); el != nil {
			ok = fc.a.userAllocator(el)
		} else if obj := fc.a.allocatorIface(); obj != nil && types2.Identical(types2.Unalias(ft), types2.Unalias(obj.Type())) {
			// An interface: the allocator is the variable whose address it
			// holds, when its type is a user allocator's.
			ok = o.vp.path == "&" && o.vp.root.v != nil && fc.a.userAllocator(o.vp.root.v.Type())
		}
		if !ok {
			continue
		}
		R := o.vp.join("*")
		dup := false
		for _, p := range out {
			if p == R {
				dup = true
			}
		}
		if !dup {
			out = compat.Append(out, R)
		}
	}
	return out
}

// fieldTypeAt returns the type of the field that tag, a path of field
// steps, selects in a value of type T, or nil.
func fieldTypeAt(T types2.Type, tag string) types2.Type {
	for _, s := range steps(tag) {
		if !strings.HasPrefix(s, ".") {
			return nil
		}
		st, ok := underlying(T).(*types2.Struct)
		if !ok {
			return nil
		}
		name := strings.SliceFrom(s, 1)
		var next types2.Type
		for i := 0; i < st.NumFields(); i++ {
			if f := st.Field(i); f.Name() == name {
				next = f.Type()
				break
			}
		}
		if next == nil {
			return nil
		}
		T = next
	}
	return T
}
