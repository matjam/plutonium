package solve

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/escape2/summary/summarytest"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
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
	return &testEnv{
		prog:      &eir.Program{},
		paths:     summarytest.NewPaths(),
		sums:      map[eir.FuncID]*summary.Summary{},
		cands:     map[*eir.Call][]eir.FuncID{},
		contracts: map[string]*summary.Summary{},
		deferred:  map[*eir.Call]bool{},
		unread:    map[eir.FuncID]bool{},
		heapOnly:  map[string]bool{},
	}
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
	return e.heapOnly[fmt.Sprintf("%s.%d", T, field)]
}

// solve solves fn and records its summary in the environment.
func (e *testEnv) solve(t *testing.T, fn *eir.Func) Result {
	t.Helper()
	ws := &Workspace{Arena: &summarytest.Arena{}, Sets: summarytest.NewSets()}
	r := Solve(fn, e, ws)
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
	f := &eir.Func{ID: eir.FuncID(len(e.prog.Funcs)), Name: name, Results: results}
	for range inputs {
		f.Inputs = append(f.Inputs, eir.Input{})
	}
	e.prog.Funcs = append(e.prog.Funcs, f)
	b := &fb{env: e, f: f}
	b.cur = b.block()
	return b
}

func (b *fb) block() *eir.Block {
	blk := &eir.Block{ID: eir.BlockID(len(b.f.Blocks)), Loop: -1}
	b.f.Blocks = append(b.f.Blocks, blk)
	return blk
}

// at sets the position of the next instructions.
func (b *fb) at(p int) *fb { b.pos = token.Pos(p); return b }

func (b *fb) reg() eir.Reg {
	b.f.NumRegs++
	return eir.Reg(b.f.NumRegs)
}

func (b *fb) emit(in eir.Instr) {
	in.Pos = b.pos
	b.cur.Instrs = append(b.cur.Instrs, in)
}

func (b *fb) obj(name string, life eir.Lifetime, loop int) eir.ObjID {
	b.f.Objects = append(b.f.Objects, eir.Object{Kind: eir.ObjVar, Name: name, Life: life, Loop: loop})
	return eir.ObjID(len(b.f.Objects) - 1)
}

func (b *fb) typedObj(name string, T types2.Type) eir.ObjID {
	b.f.Objects = append(b.f.Objects, eir.Object{Kind: eir.ObjVar, Name: name, Life: eir.LifeFrame, Loop: -1, Type: T})
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
	return b.call(&eir.Call{Kind: eir.CallStatic, Callee: callee.ID, Args: args, Results: make([]eir.Reg, results)})
}

// key makes a key of input i with the steps given ("*" a load, a number a
// field, "[]" an element) and GE when ge.
func (e *testEnv) key(i int, ge bool, steps ...string) domain.Key {
	var ss []domain.Step
	for _, st := range steps {
		switch st {
		case "*":
			ss = append(ss, domain.StepLoad)
		case "[]":
			ss = append(ss, domain.StepElem)
		default:
			n, err := strconv.Atoi(st)
			if err != nil {
				panic(err)
			}
			ss = append(ss, domain.Step(n))
		}
	}
	return domain.MakeKey(i, e.paths.Of(ss...), domain.KeyFlags{GE: ge})
}

func (e *testEnv) keyString(k domain.Key) string {
	s := fmt.Sprintf("%d%s", k.Input(), e.paths.String(k.Path()))
	if k.GE() {
		s += "+"
	}
	return s
}

// describe prints a summary for test messages and comparisons.
func (e *testEnv) describe(s *summary.Summary) string {
	var parts []string
	for _, k := range s.Esc {
		parts = append(parts, "esc "+e.keyString(k))
	}
	for _, p := range s.Store {
		parts = append(parts, "store "+e.keyString(p.Src)+"->"+e.keyString(p.Dst))
	}
	for _, k := range s.HeapStore {
		parts = append(parts, "heapstore "+e.keyString(k))
	}
	for r, res := range s.Res {
		for _, rk := range res.From {
			ge := ""
			if rk.FieldGE {
				ge = "~"
			}
			parts = append(parts, fmt.Sprintf("res%d%s%s %s", r, e.paths.String(rk.Field), ge, e.keyString(rk.Key)))
		}
		if len(res.Heap) > 0 {
			parts = append(parts, fmt.Sprintf("res%d heap", r))
		}
		if len(res.CountedHeap) > 0 {
			parts = append(parts, fmt.Sprintf("res%d cheap", r))
		}
	}
	for _, d := range s.Deferred {
		var args []string
		for _, a := range d.Args {
			var ks []string
			for _, k := range a.Keys {
				ks = append(ks, e.keyString(k))
			}
			if a.Heap {
				ks = append(ks, "heap")
			}
			args = append(args, strings.Join(ks, " "))
		}
		parts = append(parts, fmt.Sprintf("defer f%d@%d %s(%s)", d.Origin, d.Site, e.keyString(d.Fn), strings.Join(args, ", ")))
	}
	if s.Region.Builds {
		parts = append(parts, "builds")
	}
	for _, r := range s.Region.OuterResults {
		parts = append(parts, fmt.Sprintf("outer%d", r))
	}
	slices.Sort(parts)
	return strings.Join(parts, "; ")
}

// findings prints findings as "code@pos obj", sorted.
func findings(r Result) string {
	var parts []string
	for _, f := range r.Findings {
		parts = append(parts, fmt.Sprintf("%s@%d", f.Code, f.Pos))
	}
	slices.Sort(parts)
	return strings.Join(parts, " ")
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

var _ = diag.ReturnFrameView
