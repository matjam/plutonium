// Package ssawrite writes an SSA program in the textual form that
// internal/ssatext reads (the form is described there). The writer is the
// front end's half of the join between a front end and a back end that
// cannot share a process; it is not part of the back end, and the
// self-hosted compiler does not translate it.
//
// Writing is exact or it fails: a program that holds something the form
// cannot carry (a type that is not one of the predeclared types or a tuple
// or vector, an aux of another kind, a value used but in no block) is an
// error, never a program that reads back differently.
package ssawrite

import (
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/ssatext"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// Write writes p, whose positions are in fset.
func Write(out io.Writer, p *ssa.Program, fset *token.FileSet) error {
	w := &writer{out: out, typeID: map[*ssa.Type]int{}, symID: map[*ssa.Sym]int{}, fset: fset}
	w.program(p)
	if w.err != nil {
		return w.err
	}
	return nil
}

type writer struct {
	out    io.Writer
	buf    []byte
	err    error
	fset   *token.FileSet
	typeID map[*ssa.Type]int
	types  []*ssa.Type
	symID  map[*ssa.Sym]int
	files  map[int]*token.File // by base
}

func (w *writer) fail(format string, args ...any) {
	if w.err == nil {
		w.err = fmt.Errorf("ssawrite: "+format, args...)
	}
}

// word appends a space and s.
func (w *writer) word(s string) { w.buf = append(append(w.buf, ' '), s...) }

// tag starts a record.
func (w *writer) tag(s string) { w.buf = append(w.buf, s...) }

func (w *writer) int(n int64) { w.buf = strconv.AppendInt(append(w.buf, ' '), n, 10) }

func (w *writer) bool(b bool) {
	if b {
		w.int(1)
	} else {
		w.int(0)
	}
}

func (w *writer) str(s string) { w.buf = append(append(w.buf, ' '), strconv.Quote(s)...) }

// end finishes a record.
func (w *writer) end() {
	w.buf = append(w.buf, '\n')
	if len(w.buf) > 1<<16 {
		w.flush()
	}
}

func (w *writer) flush() {
	if w.err == nil {
		if _, err := w.out.Write(w.buf); err != nil {
			w.err = err
		}
	}
	w.buf = w.buf[:0]
}

var predeclared = []struct {
	t    *ssa.Type
	name string
}{
	{ssa.TVoid, "void"}, {ssa.TMem, "mem"}, {ssa.TBool, "bool"}, {ssa.TI8, "i8"}, {ssa.TI16, "i16"},
	{ssa.TI32, "i32"}, {ssa.TI64, "i64"}, {ssa.TF32, "f32"}, {ssa.TF64, "f64"}, {ssa.TPtr, "ptr"},
	{ssa.TFlags, "flags"}, {ssa.TF64x2, "f64x2"},
}

// note registers a type, and the types it holds, in the type list.
func (w *writer) note(t *ssa.Type) {
	if t == nil {
		w.fail("a nil type")
		return
	}
	for _, d := range predeclared {
		if d.t == t {
			return
		}
	}
	if _, ok := w.typeID[t]; ok {
		return
	}
	if t.Kind != ssa.KTuple && t.Kind != ssa.KVec {
		w.fail("type %s is neither predeclared, a tuple nor a vector", t)
		return
	}
	for _, e := range t.Elems {
		w.note(e)
	}
	w.typeID[t] = len(w.types)
	w.types = append(w.types, t)
}

// typeRef writes a type.
func (w *writer) typeRef(t *ssa.Type) {
	for _, d := range predeclared {
		if d.t == t {
			w.word(d.name)
			return
		}
	}
	id, ok := w.typeID[t]
	if !ok {
		w.fail("type %s was not noted", t)
		return
	}
	w.buf = strconv.AppendInt(append(w.buf, " #"...), int64(id), 10)
}

func (w *writer) typeList(ts []*ssa.Type) {
	if ts == nil {
		w.int(-1)
		return
	}
	w.int(int64(len(ts)))
	for _, t := range ts {
		w.typeRef(t)
	}
}

func (w *writer) symRef(s *ssa.Sym) {
	if s == nil {
		w.int(-1)
		return
	}
	id, ok := w.symID[s]
	if !ok {
		w.fail("symbol %q is not in the program's symbol table", s.Name)
		return
	}
	w.int(int64(id))
}

// notePos registers the file that holds a position.
func (w *writer) notePos(p token.Pos) {
	if !p.IsValid() {
		return
	}
	f := w.fset.File(p)
	if f == nil {
		w.fail("position %d is in no file", int(p))
		return
	}
	w.files[f.Base()] = f
}

func (w *writer) noteSlot(s *ssa.Slot) {
	for _, l := range s.Leaves {
		w.note(l.Type)
	}
	w.noteParts(s.Parts)
}

func (w *writer) noteParts(parts []ssa.Part) {
	for i := range parts {
		for _, l := range parts[i].Leaves {
			w.note(l.Type)
		}
		w.noteParts(parts[i].Parts)
	}
}

func (w *writer) program(p *ssa.Program) {
	w.files = map[int]*token.File{}
	syms := p.Syms()
	for i, s := range syms {
		w.symID[s] = i
	}
	for _, f := range p.Funcs {
		w.scan(f)
	}

	w.tag("ssatext")
	w.int(ssatext.Version)
	w.end()
	w.tag("ops")
	w.int(int64(ssa.OpGenericEnd))
	w.end()
	for op := ssa.OpInvalid; op < ssa.OpGenericEnd; op++ {
		w.tag("op")
		w.int(int64(op))
		w.word(op.String())
		w.end()
	}
	w.tag("types")
	w.int(int64(len(w.types)))
	w.end()
	for _, t := range w.types {
		w.tag("type")
		if t.Kind == ssa.KTuple {
			w.word("tuple")
		} else {
			w.word("vec")
			w.int(t.Size)
		}
		w.typeList(t.Elems)
		w.end()
	}

	bases := make([]int, 0, len(w.files))
	for b := range w.files {
		bases = append(bases, b)
	}
	sort.Ints(bases)
	w.tag("files")
	w.int(int64(len(bases)))
	w.end()
	for _, b := range bases {
		f := w.files[b]
		w.tag("file")
		w.int(int64(f.Base()))
		w.int(int64(f.Size()))
		w.str(f.Name())
		lines := f.Lines()
		w.int(int64(len(lines)))
		for _, off := range lines {
			w.int(int64(off))
		}
		infos := f.LineInfos()
		w.int(int64(len(infos)))
		for _, in := range infos {
			w.int(int64(in.Offset))
			w.str(in.Filename)
			w.int(int64(in.Line))
			w.int(int64(in.Column))
		}
		w.end()
	}

	w.tag("syms")
	w.int(int64(len(syms)))
	w.end()
	for _, s := range syms {
		w.tag("sym")
		w.int(int64(s.Kind))
		w.bool(s.PerCPU)
		w.str(s.Name)
		w.end()
	}

	w.tag("datas")
	w.int(int64(len(p.Data)))
	w.end()
	for _, d := range p.Data {
		w.tag("data")
		w.symRef(d.Sym)
		w.int(d.Size)
		w.int(d.Align)
		w.bool(d.ReadOnly)
		w.bool(d.Local)
		w.str(d.Section)
		w.hex(d.Bytes)
		w.int(int64(len(d.Relocs)))
		for _, r := range d.Relocs {
			w.int(r.Off)
			w.symRef(r.Sym)
			w.int(r.Addend)
		}
		w.end()
	}

	w.tag("funcs")
	w.int(int64(len(p.Funcs)))
	w.end()
	for _, f := range p.Funcs {
		w.function(f)
	}
	w.flush()
}

func (w *writer) hex(b []byte) {
	switch {
	case b == nil:
		w.word("-")
	case len(b) == 0:
		w.word("=")
	default:
		const digits = "0123456789abcdef"
		w.buf = append(w.buf, ' ')
		for _, c := range b {
			w.buf = append(w.buf, digits[c>>4], digits[c&15])
		}
	}
}

// scan notes the types and positions of a function.
func (w *writer) scan(f *ssa.Func) {
	for _, t := range f.Params {
		w.note(t)
	}
	for _, t := range f.Results {
		w.note(t)
	}
	w.notePos(f.Pos)
	w.notePos(f.End)
	cur := f.SetPos(0)
	f.SetPos(cur)
	w.notePos(cur)
	for _, s := range f.Inlines {
		w.notePos(s.Pos)
		w.notePos(s.Start)
		w.notePos(s.End)
	}
	for _, s := range f.Slots {
		w.noteSlot(s)
	}
	for _, b := range f.Blocks {
		w.notePos(b.Pos)
		for _, v := range b.Values {
			w.scanValue(v)
		}
	}
	for _, v := range detached(f) {
		w.scanValue(v)
	}
}

func (w *writer) scanValue(v *ssa.Value) {
	w.note(v.Type)
	w.notePos(v.Pos)
	if t, ok := v.Aux.(*ssa.Type); ok {
		w.note(t)
	}
	if v.Call != nil {
		for _, t := range v.Call.Results {
			w.note(t)
		}
	}
}

// detached returns the values that no block holds but a string hold of f
// refers to (lowering removes dead calls from its blocks, not from the
// holds), and the values those use. The optimiser reads their ops.
func detached(f *ssa.Func) []*ssa.Value {
	in := map[*ssa.Value]bool{}
	for _, b := range f.Blocks {
		for _, v := range b.Values {
			in[v] = true
		}
	}
	var out []*ssa.Value
	var visit func(v *ssa.Value)
	visit = func(v *ssa.Value) {
		if in[v] {
			return
		}
		in[v] = true
		out = append(out, v)
		for _, a := range v.Args {
			visit(a)
		}
	}
	for _, h := range f.StrHolds {
		for _, list := range [][]*ssa.Value{h.Retains, h.Releases, h.Covered} {
			for _, v := range list {
				visit(v)
			}
		}
	}
	return out
}

func (w *writer) function(f *ssa.Func) {
	if f.GenNoFree != nil || f.GenNoReset != nil {
		w.fail("%s: GenNoFree or GenNoReset is set; they belong to the optimiser", f.Name)
		return
	}
	cur := f.SetPos(0)
	f.SetPos(cur)
	w.tag("func")
	w.str(f.Name)
	w.symRef(f.Sym)
	w.int(int64(f.ABI))
	w.int(int64(f.Pragmas))
	w.str(f.Section)
	w.int(int64(f.Pos))
	w.int(int64(f.End))
	w.str(f.Pkg)
	w.bool(f.NoSSE)
	w.int(int64(f.NumValues()))
	w.int(int64(f.NumBlocks()))
	w.int(int64(f.NumSlots()))
	w.int(int64(cur))
	w.symRef(f.GenPanic)
	w.symRef(f.GenRangePanic)
	w.symRef(f.GenLive)
	w.symRef(f.GenRange)
	w.symRef(f.DeferRunner)
	if f.DeferRecord != nil {
		w.int(int64(f.DeferRecord.ID))
	} else {
		w.int(-1)
	}
	if f.Entry == nil {
		w.fail("%s has no entry block", f.Name)
		return
	}
	w.int(int64(f.Entry.ID))
	w.end()

	w.tag("exports")
	if f.Exports == nil {
		w.int(-1)
	} else {
		w.int(int64(len(f.Exports)))
		for _, e := range f.Exports {
			w.str(e)
		}
	}
	w.end()
	w.tag("params")
	w.typeList(f.Params)
	w.end()
	w.tag("results")
	w.typeList(f.Results)
	w.end()
	w.tag("paramonstack")
	if f.ParamOnStack == nil {
		w.int(-1)
	} else {
		w.int(int64(len(f.ParamOnStack)))
		for _, b := range f.ParamOnStack {
			w.bool(b)
		}
	}
	w.end()
	w.tag("genowner")
	if f.GenOwnerArgs == nil {
		w.int(-1)
	} else {
		w.int(int64(len(f.GenOwnerArgs)))
		for _, n := range f.GenOwnerArgs {
			w.int(n)
		}
	}
	w.end()

	w.tag("inlines")
	w.int(int64(len(f.Inlines)))
	w.end()
	for _, s := range f.Inlines {
		w.tag("inline")
		w.int(int64(s.Parent))
		w.str(s.Func)
		w.int(int64(s.Pos))
		w.int(int64(s.Start))
		w.int(int64(s.End))
		w.end()
	}

	w.tag("slots")
	w.int(int64(len(f.Slots)))
	w.end()
	for _, s := range f.Slots {
		w.tag("slot")
		w.int(int64(s.ID))
		w.str(s.Name)
		w.int(s.Size)
		w.int(s.Align)
		w.int(s.Offset)
		w.bool(s.Closure)
		w.leaves(s.Leaves)
		w.pads(s.Pads)
		w.parts(s.Parts)
		w.end()
	}

	// The values that exist: a reference to any other is an error.
	exists := make(map[*ssa.Value]bool, f.NumValues())
	blocks := make(map[*ssa.Block]bool, len(f.Blocks))
	for _, b := range f.Blocks {
		blocks[b] = true
		for _, v := range b.Values {
			exists[v] = true
		}
	}
	dead := detached(f)
	for _, v := range dead {
		exists[v] = true
	}
	if !blocks[f.Entry] {
		w.fail("%s: the entry block is not among its blocks", f.Name)
		return
	}
	where := ""
	id := func(v *ssa.Value) int64 {
		if !exists[v] {
			w.fail("%s: %s uses a value (v%d, %s) that is in no block", f.Name, where, v.ID, v.Op)
			return -1
		}
		return int64(v.ID)
	}
	w.tag("strholds")
	w.int(int64(len(f.StrHolds)))
	w.end()
	for _, h := range f.StrHolds {
		where = "a string hold"
		w.tag("strhold")
		for _, list := range [][]*ssa.Value{h.Retains, h.Releases, h.Covered} {
			w.int(int64(len(list)))
			for _, v := range list {
				w.int(id(v))
			}
		}
		w.end()
	}

	w.tag("blocks")
	w.int(int64(len(f.Blocks)))
	w.end()
	for _, b := range f.Blocks {
		where = fmt.Sprintf("the controls of b%d", b.ID)
		w.tag("block")
		w.int(int64(b.ID))
		w.int(int64(b.Kind))
		w.int(int64(b.Likely))
		w.int(int64(b.Pos))
		w.int(int64(b.Inl))
		w.int(int64(len(b.Controls)))
		for _, c := range b.Controls {
			w.int(id(c))
		}
		w.int(int64(len(b.Succs)))
		for _, e := range b.Succs {
			if !blocks[e.B] {
				w.fail("%s: an edge to a block that is not in the function", f.Name)
				return
			}
			w.int(int64(e.B.ID))
			w.int(int64(e.I))
		}
		w.int(int64(len(b.Preds)))
		for _, e := range b.Preds {
			if !blocks[e.B] {
				w.fail("%s: an edge from a block that is not in the function", f.Name)
				return
			}
			w.int(int64(e.B.ID))
			w.int(int64(e.I))
		}
		w.int(int64(len(b.Values)))
		w.end()
		for _, v := range b.Values {
			where = fmt.Sprintf("v%d (%s)", v.ID, v.Op)
			w.value(f, v, id)
		}
	}
	w.tag("detached")
	w.int(int64(len(dead)))
	w.end()
	for _, v := range dead {
		where = fmt.Sprintf("the detached v%d (%s)", v.ID, v.Op)
		w.value(f, v, id)
	}
	w.tag("endfunc")
	w.end()
}

func (w *writer) value(f *ssa.Func, v *ssa.Value, id func(*ssa.Value) int64) {
	w.tag("v")
	w.int(int64(v.ID))
	w.int(int64(v.Op))
	w.typeRef(v.Type)
	w.int(v.AuxInt)
	switch a := v.Aux.(type) {
	case nil:
		w.word("-")
	case *ssa.Sym:
		w.buf = append(w.buf, " s"...)
		sid, ok := w.symID[a]
		if !ok {
			w.fail("%s: symbol %q is not in the program's symbol table", f.Name, a.Name)
		}
		w.buf = strconv.AppendInt(w.buf, int64(sid), 10)
	case *ssa.Slot:
		w.buf = strconv.AppendInt(append(w.buf, " l"...), int64(a.ID), 10)
	case *ssa.Type:
		w.buf = append(w.buf, " t"...)
		w.typeRefNoSpace(a)
	case *ssa.Intrinsic:
		w.buf = append(append(w.buf, " i"...), strconv.Quote(string(*a))...)
	default:
		w.fail("%s: v%d has an aux of type %T", f.Name, v.ID, v.Aux)
	}
	w.int(int64(v.Pos))
	w.int(int64(v.Inl))
	w.int(int64(v.Uses))
	w.int(int64(len(v.Args)))
	for _, a := range v.Args {
		w.int(id(a))
	}
	if ci := v.Call; ci == nil {
		w.int(0)
	} else {
		w.int(1)
		w.int(int64(ci.ABI))
		w.bool(ci.Variadic)
		w.bool(ci.NoReturn)
		w.bool(ci.Implicit)
		w.int(int64(ci.Alloc))
		w.typeList(ci.Results)
		if ci.OnStack == nil {
			w.int(-1)
		} else {
			w.int(int64(len(ci.OnStack)))
			for _, b := range ci.OnStack {
				w.bool(b)
			}
		}
	}
	w.end()
}

// typeRefNoSpace writes a type without the space before it.
func (w *writer) typeRefNoSpace(t *ssa.Type) {
	n := len(w.buf)
	w.typeRef(t)
	if len(w.buf) > n && w.buf[n] == ' ' {
		w.buf = append(w.buf[:n], w.buf[n+1:]...)
	}
}

func (w *writer) leaves(ls []ssa.Leaf) {
	if ls == nil {
		w.int(-1)
		return
	}
	w.int(int64(len(ls)))
	for _, l := range ls {
		w.int(l.Off)
		w.typeRef(l.Type)
	}
}

func (w *writer) pads(ps []ssa.Pad) {
	if ps == nil {
		w.int(-1)
		return
	}
	w.int(int64(len(ps)))
	for _, p := range ps {
		w.int(p.Off)
		w.int(p.Size)
	}
}

func (w *writer) parts(ps []ssa.Part) {
	if ps == nil {
		w.int(-1)
		return
	}
	w.int(int64(len(ps)))
	for i := range ps {
		p := &ps[i]
		w.int(p.Off)
		w.int(p.Size)
		w.int(p.Align)
		w.bool(p.Closure)
		w.bool(p.Region)
		w.leaves(p.Leaves)
		w.pads(p.Pads)
		w.parts(p.Parts)
	}
}
