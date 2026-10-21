package cache

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/types2"
)

// hasher writes the parts of a digest with their lengths, so that two
// different sequences of parts never give the same bytes. The parts are
// collected and hashed at the end (the same digest as hashing them as they
// come; no hash.Hash, which the self-hosted compiler's library lacks).
type hasher struct{ b []byte }

func newHasher() *hasher { return &hasher{} }

func (h *hasher) int(v int64) {
	h.b = binary.AppendVarint(h.b, v)
}

func (h *hasher) str(s string) {
	h.int(int64(len(s)))
	h.b = append(h.b, s...)
}

func (h *hasher) bytes(b []byte) {
	h.int(int64(len(b)))
	h.b = append(h.b, b...)
}

func (h *hasher) bool(b bool) {
	if b {
		h.int(1)
	} else {
		h.int(0)
	}
}

func (h *hasher) ints(xs []int) {
	h.int(int64(len(xs)))
	for _, x := range xs {
		h.int(int64(x))
	}
}

func (h *hasher) sum() digest {
	var d digest
	full := sha256.Sum256(h.b)
	copy(d[:], full[:])
	return d
}

// typeKey is a type as the digests write it: its string with package paths
// and, for a defined type, its underlying type's string too, so that a
// change of a type's definition changes the digest of every function that
// names it. (Types further in are not followed; a digest that misses a
// change costs at most a summary that must pass the check, never
// soundness.)
func (c *Cache) typeKey(T types2.Type) string {
	if T == nil {
		return "-"
	}
	if s, ok := c.types[T]; ok {
		return s
	}
	s := typeString(T)
	if n, ok := T.(*types2.Named); ok {
		s += " = " + typeString(n.Underlying())
	}
	c.types[T] = s
	return s
}

// shapeOf is what a solve reads of a callee besides its summary
// (solve/call.go): its inputs, results and whether it has a body.
func shapeOf(f *eir.Func) shape { return shape{inputs: len(f.Inputs), results: f.Results} }

func (c *Cache) writeFuncRef(h *hasher, id eir.FuncID) {
	if id == eir.NoFunc || int(id) >= len(c.prog.Funcs) {
		h.str("?")
		return
	}
	f := c.prog.Funcs[id]
	h.str(c.idOrName(id))
	h.int(int64(len(f.Inputs)))
	h.int(int64(f.Results))
	h.bool(f.Bodyless)
	h.bool(f.External)
}

// idOrName is a function's stable ID, or for one without, its name: what
// a digest that refers to it writes. A function without an ID is never
// loaded, so a digest that names it by its name is only less often reused.
func (c *Cache) idOrName(id eir.FuncID) string {
	if s := c.ids[id]; s != "" {
		return s
	}
	return "?" + c.prog.Funcs[id].Name
}

// eirDigest is the digest of a function's EIR without positions, with the
// cache's salt (format, compiler, flags). Callees are written by stable ID
// and shape, types by typeKey.
func (c *Cache) eirDigest(id eir.FuncID) digest {
	if c.eirDone[id] {
		return c.eirSum[id]
	}
	f := c.prog.Funcs[id]
	h := newHasher()
	h.bytes(c.salt[:])
	h.str(c.idOrName(id))
	h.bool(f.External)
	h.bool(f.Bodyless)
	h.int(int64(f.Results))
	h.int(int64(len(f.Inputs)))
	for _, in := range f.Inputs {
		h.str(c.typeKey(in.Type))
	}
	h.int(int64(len(f.Objects)))
	for _, o := range f.Objects {
		h.int(int64(o.Kind))
		h.str(c.typeKey(o.Type))
		h.int(int64(o.Life))
		h.int(int64(o.Loop))
	}
	h.int(int64(len(f.Loops)))
	for _, l := range f.Loops {
		h.int(int64(l.Header))
		h.int(int64(l.Parent))
		h.int(int64(len(l.Blocks)))
		for _, b := range l.Blocks {
			h.int(int64(b))
		}
	}
	h.int(int64(f.NumRegs))
	h.int(int64(f.NumSites))
	h.int(int64(len(f.Blocks)))
	for _, b := range f.Blocks {
		h.int(int64(b.ID))
		h.int(int64(b.Loop))
		h.int(int64(len(b.Succs)))
		for _, s := range b.Succs {
			h.int(int64(s))
		}
		h.int(int64(len(b.Preds)))
		for _, s := range b.Preds {
			h.int(int64(s))
		}
		h.int(int64(len(b.Instrs)))
		for i := range b.Instrs {
			c.writeInstr(h, &b.Instrs[i])
		}
	}
	d := h.sum()
	c.eirSum[id], c.eirDone[id] = d, true
	return d
}

func regsInts(rs []eir.Reg) []int {
	out := make([]int, len(rs))
	for i, r := range rs {
		out[i] = int(r)
	}
	return out
}

func (c *Cache) writeInstr(h *hasher, in *eir.Instr) {
	h.int(int64(in.Op))
	h.int(int64(in.Dst))
	h.int(int64(in.X))
	h.int(int64(in.Y))
	h.ints(regsInts(in.Args))
	h.int(int64(in.Obj))
	h.ints(in.Path)
	h.int(int64(len(in.Fields)))
	for _, f := range in.Fields {
		h.ints(f)
	}
	h.int(int64(in.Index))
	h.str(c.typeKey(in.Type))
	h.str(c.typeKey(in.To))
	h.int(int64(in.Heap))
	h.int(int64(in.Sink))
	h.str(funcString(in.Method))
	if in.Op == eir.OpClosure || in.Op == eir.OpMethodVal {
		c.writeFuncRef(h, in.Callee)
	}
	if cl := in.Call; cl != nil {
		h.int(int64(cl.Kind))
		if cl.Kind == eir.CallStatic {
			c.writeFuncRef(h, cl.Callee)
		}
		h.str(funcString(cl.Method))
		h.str(c.typeKey(cl.Iface))
		if cl.Sig != nil {
			h.str(c.typeKey(cl.Sig))
		} else {
			h.str("-")
		}
		h.int(int64(cl.Target))
		h.str(cl.Contract)
		h.ints(regsInts(cl.Args))
		h.ints(regsInts(cl.Results))
		h.int(int64(cl.Implicit))
		h.int(int64(cl.Site))
	}
}

// sumDigest is the digest of a summary's canonical encoding. Summaries are
// final when a digest is taken of them (callees of earlier SCCs, contract
// entries), and the map keeps every one it has seen alive, so a pointer
// names one summary for the whole run.
func (c *Cache) sumDigest(s *summary.Summary) digest {
	if d, ok := c.sums[s]; ok {
		return d
	}
	h := newHasher()
	h.bytes(appendSummary(nil, c.paths, s))
	d := h.sum()
	c.sums[s] = d
	return d
}

// recorder implements sched.Recorder: the digest of what a solve reads of
// the stage B facts and the contract table.
// Each distinct question is written once, sorted, so the digest does not
// depend on how often or in what order the solve asked.
type recorder struct {
	c        *Cache
	heap     map[heapQ]bool
	contract map[string]*summary.Summary // nil: no entry
	inst     map[*types2.TypeParam]instA
}

// instA is an answer of Instantiated: the type arguments, written, and
// whether the set is known.
type instA struct {
	types []string
	known bool
}

type heapQ struct {
	T     types2.Type
	field int
}

func (c *Cache) newRecorder() *recorder {
	return &recorder{c: c, heap: map[heapQ]bool{}, contract: map[string]*summary.Summary{}, inst: map[*types2.TypeParam]instA{}}
}

func (r *recorder) HeapOnly(T types2.Type, field int, heap bool) {
	r.heap[heapQ{T, field}] = heap
}

func (r *recorder) Instantiated(tp *types2.TypeParam, types []types2.Type, known bool) {
	if _, ok := r.inst[tp]; ok {
		return
	}
	a := instA{known: known}
	for _, T := range types {
		a.types = append(a.types, r.c.typeKey(T))
	}
	slices.Sort(a.types)
	r.inst[tp] = a
}

func (r *recorder) Contract(name string, s *summary.Summary, ok bool) {
	if !ok {
		s = nil
	}
	r.contract[name] = s
}

type heapA struct {
	t     string
	field int
	heap  bool
}

func (r *recorder) sum() digest {
	as := make([]heapA, 0, len(r.heap))
	for q, a := range r.heap {
		as = append(as, heapA{r.c.typeKey(q.T), q.field, a})
	}
	slices.SortFunc(as, func(a, b heapA) int {
		if c := strings.Compare(a.t, b.t); c != 0 {
			return c
		}
		if a.field != b.field {
			return a.field - b.field
		}
		return boolInt(a.heap) - boolInt(b.heap)
	})
	as = slices.Compact(as) // one type may have several types2.Type values
	h := newHasher()
	h.int(int64(len(as)))
	for _, a := range as {
		h.str(a.t)
		h.int(int64(a.field))
		h.bool(a.heap)
	}
	// A type parameter is written as its declaring object and name, which
	// stay the same from run to run.
	type instW struct {
		tp string
		a  instA
	}
	var insts []instW
	for tp, a := range r.inst {
		insts = append(insts, instW{tpKey(tp), a})
	}
	// Two type parameters may write alike (one name and index in two
	// generic functions); their answers are then ordered by content.
	slices.SortFunc(insts, func(a, b instW) int {
		if c := strings.Compare(a.tp, b.tp); c != 0 {
			return c
		}
		return strings.Compare(fmt.Sprint(a.a), fmt.Sprint(b.a))
	})
	h.int(int64(len(insts)))
	for _, w := range insts {
		h.str(w.tp)
		h.bool(w.a.known)
		h.int(int64(len(w.a.types)))
		for _, t := range w.a.types {
			h.str(t)
		}
	}
	names := make([]string, 0, len(r.contract))
	for n := range r.contract {
		names = append(names, n)
	}
	sort.Strings(names)
	h.int(int64(len(names)))
	for _, n := range names {
		h.str(n)
		s := r.contract[n]
		h.bool(s != nil)
		if s != nil {
			d := r.c.sumDigest(s)
			h.bytes(d[:])
		}
	}
	return h.sum()
}

// tpKey names a type parameter: the object that declares it and its index
// there.
func tpKey(tp *types2.TypeParam) string {
	owner := "-"
	if o := tp.Obj(); o != nil {
		if p := o.Pkg(); p != nil {
			owner = p.Path()
		}
		owner += "." + o.Name()
	}
	return fmt.Sprintf("%s#%d %s", owner, tp.Index(), typeString(tp.Constraint()))
}
