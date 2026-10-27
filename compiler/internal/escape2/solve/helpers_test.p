// Ported from internal/escape2/solve/helpers_test.go at the go-cutover tag.

package solve

import (
	"pluto/slices"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/summary"
	"compiler/internal/escape2/summary/summarytest"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
)

// testEnv is an Env over a hand-built program.
type testEnv struct {
	prog      *eir.Program
	paths     *summarytest.Paths
	sums      map[eir.FuncID]*summary.Summary
	cands     map[*eir.Call][]eir.FuncID
	contracts map[string]*summary.Summary
	heapOnly  map[string]bool
	deferred  map[*eir.Call]bool  // the calls deferred (D49)
	unread    map[eir.FuncID]bool // the summaries a resolution may not read
}

func newEnv() *testEnv {
	return pu.New(testEnv{
		prog:      pu.New(eir.Program{}),
		paths:     summarytest.NewPaths(),
		sums:      map[eir.FuncID]*summary.Summary{},
		cands:     map[*eir.Call][]eir.FuncID{},
		contracts: map[string]*summary.Summary{},
		deferred:  map[*eir.Call]bool{},
		unread:    map[eir.FuncID]bool{},
		heapOnly:  map[string]bool{},
	})
}

func (e *testEnv) Program() *eir.Program                            { return e.prog }
func (e *testEnv) Paths() domain.Paths                              { return e.paths }
func (e *testEnv) Summary(id eir.FuncID) *summary.Summary           { return e.sums[id] }
func (e *testEnv) Candidates(_ *eir.Func, c *eir.Call) []eir.FuncID { return e.cands[c] }
func (e *testEnv) Contract(name string) (*summary.Summary, bool) {
	s, ok := e.contracts[name]
	return s, ok
}
func (e *testEnv) Instantiated(*types2.TypeParam) ([]types2.Type, bool) { return nil, false }
func (e *testEnv) Deferred(f *eir.Func, c *eir.Call) bool               { return e.deferred[c] }
func (e *testEnv) CanRead(f *eir.Func, id eir.FuncID) bool              { return !e.unread[id] }
func (e *testEnv) Retyped(u, v types2.Type) bool                        { return true }
func (e *testEnv) HeapOnly(T types2.Type, field int) bool {
	return e.heapOnly[heapOnlyKey(T, field)]
}

// heapOnlyKey is the key of field of T in testEnv.heapOnly: "T.field".
func heapOnlyKey(T types2.Type, field int) string {
	return compat.Sprintfb("%s.%d", compat.ArgS(types2.TypeString(T, nil)), compat.ArgI(int64(field)))
}

// newWorkspace is a workspace on the test domain.
func newWorkspace() Workspace {
	return Workspace{Arena: pu.New(summarytest.Arena{}), Sets: summarytest.NewSets()}
}

// solve solves fn and records its summary in the environment.
func (e *testEnv) solve(t *testing.T, fn *eir.Func) Result {
	t.Helper()
	ws := newWorkspace()
	r := Solve(fn, e, &ws)
	e.sums[fn.ID] = r.Summary
	return r
}

// fb builds one function. Positions are small integers, so that a test
// names the instruction a finding is at.
type fb struct {
	env *testEnv
	f   *eir.Func
	cur *eir.Block
	pos token.Pos
}

func (e *testEnv) fn(name string, inputs, results int) *fb {
	f := pu.New(eir.Func{ID: eir.FuncID(len(e.prog.Funcs)), Name: name, Results: results})
	for range inputs {
		f.Inputs = compat.Append(f.Inputs, eir.Input{})
	}
	e.prog.Funcs = compat.Append(e.prog.Funcs, f)
	b := pu.New(fb{env: e, f: f})
	b.cur = b.block()
	return b
}

func (b *fb) block() *eir.Block {
	blk := pu.New(eir.Block{ID: eir.BlockID(len(b.f.Blocks)), Loop: -1})
	b.f.Blocks = compat.Append(b.f.Blocks, blk)
	return blk
}

// at sets the position of the next instructions.
func (b *fb) at(p int) *fb { b.pos = token.Pos(p); return b }

func (b *fb) reg() eir.Reg {
	b.f.NumRegs++
	return eir.Reg(b.f.NumRegs)
}

// clonePlain is a heap copy of s, nil for nil. Its elements hold no
// pointers, so the escape analysis sees it keeps nothing of s (which it
// cannot see of compat.Clone's generic copy).
func clonePlain[T compat.Plain](s []T) []T {
	if s == nil {
		return nil
	}
	return compat.AppendPlain(compat.MakeCap[T](0, len(s)), s...)
}

// regs is a heap copy of xs: the Args of a call, which the program keeps.
func regs(xs ...eir.Reg) []eir.Reg { return clonePlain(xs) }

// blocks is a heap copy of ids: a block's Succs or Preds, or a loop's
// Blocks.
func blocks(ids ...eir.BlockID) []eir.BlockID { return clonePlain(ids) }

// funcIDs is a heap copy of ids: the candidates of a call.
func funcIDs(ids ...eir.FuncID) []eir.FuncID { return clonePlain(ids) }

// keys is a heap copy of ks: the keys of a summary.
func keys(ks ...domain.Key) []domain.Key { return clonePlain(ks) }

// loops is a heap copy of ls: a function's Loops.
func loops(ls ...eir.Loop) []eir.Loop {
	var out []eir.Loop
	for _, l := range ls {
		out = compat.Append(out, eir.Loop{Header: l.Header, Blocks: clonePlain(l.Blocks), Parent: l.Parent})
	}
	return out
}

// emit appends in to the current block. The program is in the heap, so
// the slices of in are copied there.
func (b *fb) emit(in eir.Instr) {
	var fields [][]int
	for _, p := range in.Fields {
		fields = compat.Append(fields, clonePlain(p))
	}
	b.cur.Instrs = compat.Append(b.cur.Instrs, eir.Instr{
		Op: in.Op, Dst: in.Dst, X: in.X, Y: in.Y, Args: clonePlain(in.Args), Obj: in.Obj,
		Path: clonePlain(in.Path), Fields: fields, Index: in.Index, Type: in.Type, To: in.To,
		Heap: in.Heap, Sink: in.Sink, Method: in.Method, Callee: in.Callee, Call: in.Call, Pos: b.pos,
	})
}

func (b *fb) obj(name string, life eir.Lifetime, loop int) eir.ObjID {
	b.f.Objects = compat.Append(b.f.Objects, eir.Object{Kind: eir.ObjVar, Name: name, Life: life, Loop: loop})
	return eir.ObjID(len(b.f.Objects) - 1)
}

func (b *fb) typedObj(name string, T types2.Type) eir.ObjID {
	b.f.Objects = compat.Append(b.f.Objects, eir.Object{Kind: eir.ObjVar, Name: name, Life: eir.LifeFrame, Loop: -1, Type: T})
	return eir.ObjID(len(b.f.Objects) - 1)
}

func (b *fb) addr(o eir.ObjID, path ...int) eir.Reg {
	r := b.reg()
	b.emit(eir.Instr{Op: eir.OpAddr, Dst: r, Obj: o, Path: path})
	return r
}

func (b *fb) input(i int) eir.Reg {
	r := b.reg()
	b.emit(eir.Instr{Op: eir.OpInput, Dst: r, Index: i})
	return r
}

func (b *fb) heap() eir.Reg {
	r := b.reg()
	b.emit(eir.Instr{Op: eir.OpHeap, Dst: r, Heap: eir.HeapAlloc})
	return r
}

func (b *fb) load(x eir.Reg) eir.Reg {
	r := b.reg()
	b.emit(eir.Instr{Op: eir.OpLoad, Dst: r, X: x})
	return r
}

func (b *fb) store(x, y eir.Reg) { b.emit(eir.Instr{Op: eir.OpStore, X: x, Y: y}) }

func (b *fb) field(x eir.Reg, path ...int) eir.Reg {
	r := b.reg()
	b.emit(eir.Instr{Op: eir.OpField, Dst: r, X: x, Path: path})
	return r
}

func (b *fb) sel(x eir.Reg, path ...int) eir.Reg {
	r := b.reg()
	b.emit(eir.Instr{Op: eir.OpSelect, Dst: r, X: x, Path: path})
	return r
}

func (b *fb) compose(fields [][]int, args ...eir.Reg) eir.Reg {
	r := b.reg()
	b.emit(eir.Instr{Op: eir.OpCompose, Dst: r, Args: args, Fields: fields})
	return r
}

func (b *fb) copyOf(x eir.Reg) eir.Reg {
	r := b.reg()
	b.emit(eir.Instr{Op: eir.OpCopy, Dst: r, X: x})
	return r
}

func (b *fb) ret(i int, x eir.Reg) {
	b.emit(eir.Instr{Op: eir.OpSink, Sink: eir.SinkReturn, Index: i, X: x})
}

func (b *fb) sink(k eir.SinkKind, x eir.Reg) {
	b.emit(eir.Instr{Op: eir.OpSink, Sink: k, X: x})
}

func (b *fb) call(c *eir.Call) []eir.Reg {
	c.Site = eir.SiteID(b.f.NumSites)
	b.f.NumSites++
	for i := range c.Results {
		c.Results[i] = b.reg()
	}
	b.emit(eir.Instr{Op: eir.OpCall, Call: c})
	return c.Results
}

func (b *fb) static(callee *eir.Func, results int, args ...eir.Reg) []eir.Reg {
	return b.call(pu.New(eir.Call{Kind: eir.CallStatic, Callee: callee.ID, Args: clonePlain(args), Results: compat.Make[eir.Reg](results)}))
}

// key makes a key of input i with the steps given ("*" a load, a number a
// field, "[]" an element) and GE when ge.
func (e *testEnv) key(i int, ge bool, steps ...string) domain.Key {
	var ss []domain.Step
	for _, st := range steps {
		switch st {
		case "*":
			ss = compat.AppendPlain(ss, domain.StepLoad)
		case "[]":
			ss = compat.AppendPlain(ss, domain.StepElem)
		default:
			n := 0
			for j := 0; j < len(st); j++ {
				c := st[j]
				if c < '0' || c > '9' {
					panic(compat.Concat("key: bad step ", st))
				}
				n = n*10 + int(c-'0')
			}
			ss = compat.AppendPlain(ss, domain.Step(n))
		}
	}
	return domain.MakeKey(i, e.paths.Of(ss...), domain.KeyFlags{GE: ge})
}

func (e *testEnv) keyString(k domain.Key) string {
	s := compat.Sprintfb("%d%s", compat.ArgI(int64(k.Input())), compat.ArgS(e.paths.String(k.Path())))
	if k.GE() {
		s = compat.Concat(s, "+")
	}
	return s
}

// describe prints a summary for test messages and comparisons.
func (e *testEnv) describe(s *summary.Summary) string {
	var parts []string
	for _, k := range s.Esc {
		parts = compat.Append(parts, compat.Concat("esc ", e.keyString(k)))
	}
	for _, p := range s.Store {
		parts = compat.Append(parts, compat.Concat("store ", e.keyString(p.Src), "->", e.keyString(p.Dst)))
	}
	for _, k := range s.HeapStore {
		parts = compat.Append(parts, compat.Concat("heapstore ", e.keyString(k)))
	}
	for r, res := range s.Res {
		for _, rk := range res.From {
			ge := ""
			if rk.FieldGE {
				ge = "~"
			}
			parts = compat.Append(parts, compat.Sprintfb("res%d%s%s %s", compat.ArgI(int64(r)), compat.ArgS(e.paths.String(rk.Field)), compat.ArgS(ge), compat.ArgS(e.keyString(rk.Key))))
		}
		if len(res.Heap) > 0 {
			parts = compat.Append(parts, compat.Sprintfb("res%d heap", compat.ArgI(int64(r))))
		}
		if len(res.CountedHeap) > 0 {
			parts = compat.Append(parts, compat.Sprintfb("res%d cheap", compat.ArgI(int64(r))))
		}
	}
	for _, d := range s.Deferred {
		var args []string
		for _, a := range d.Args {
			var ks []string
			for _, k := range a.Keys {
				ks = compat.Append(ks, e.keyString(k))
			}
			if a.Heap {
				ks = compat.Append(ks, "heap")
			}
			args = compat.Append(args, compat.Join(ks, " "))
		}
		parts = compat.Append(parts, compat.Sprintfb("defer f%d@%d %s(%s)", compat.ArgI(int64(d.Origin)), compat.ArgI(int64(d.Site)), compat.ArgS(e.keyString(d.Fn)), compat.ArgS(compat.Join(args, ", "))))
	}
	if s.Region.Builds {
		parts = compat.Append(parts, "builds")
	}
	for _, r := range s.Region.OuterResults {
		parts = compat.Append(parts, compat.Sprintfb("outer%d", compat.ArgI(int64(r))))
	}
	slices.Sort(parts)
	return compat.Join(parts, "; ")
}

// findings prints findings as "code@pos obj", sorted.
func findings(r Result) string {
	var parts []string
	for _, f := range r.Findings {
		parts = compat.Append(parts, compat.Sprintfb("%s@%d", compat.ArgS(string(f.Code)), compat.ArgI(int64(f.Pos))))
	}
	slices.Sort(parts)
	return compat.Join(parts, " ")
}

func wantFindings(t *testing.T, r Result, want string) {
	t.Helper()
	if got := findings(r); got != want {
		t.Errorf("findings:\n got: %s\nwant: %s", got, want)
	}
}

func (e *testEnv) wantSummary(t *testing.T, r Result, want string) {
	t.Helper()
	if got := e.describe(r.Summary); got != want {
		t.Errorf("summary:\n got: %s\nwant: %s", got, want)
	}
}

// posList formats ps as fmt's %v formats a slice of integers: "[1 2 3]".
func posList(ps []token.Pos) string {
	var parts []string
	for _, p := range ps {
		parts = compat.Append(parts, compat.Sprintfb("%d", compat.ArgI(int64(p))))
	}
	return compat.Concat("[", compat.Join(parts, " "), "]")
}

var _ = diag.ReturnFrameView
