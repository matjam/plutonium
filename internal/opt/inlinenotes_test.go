package opt

import (
	"strconv"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// TestInlineNotes checks the reasons recorded for calls to //pu:inline
// functions left out of line: a callee that never returns, one also marked
// //pu:noinline, a recursive call, a func value; and that calls are
// reported, and copies of one call merged, when the inliner did not run.
func TestInlineNotes(t *testing.T) {
	p := ssa.NewProgram()
	newFunc := func(name string, pragmas ssa.Pragma) *ssa.Func {
		f := ssa.NewFunc(name)
		f.Sym = p.Sym(name, ssa.SymFunc)
		f.Pragmas = pragmas
		p.Funcs = append(p.Funcs, f)
		return f
	}
	// call appends to b a call of g at pos, returning the memory after it.
	call := func(b *ssa.Block, g *ssa.Func, mem *ssa.Value, pos token.Pos) *ssa.Value {
		c := b.NewValueA(ssa.OpCall, ssa.TupleOf(ssa.TMem), g.Sym, mem)
		c.Call = &ssa.CallInfo{ABI: ssa.ABIInternal}
		c.Pos = pos
		return b.NewValueI(ssa.OpSelectN, ssa.TMem, 0, c)
	}

	exit := newFunc("exit", ssa.PragmaInline)
	exit.Entry.Kind = ssa.BlockExit
	exit.Entry.SetControl(exit.Entry.NewValue(ssa.OpInitMem, ssa.TMem))

	noin := newFunc("noin", ssa.PragmaInline|ssa.PragmaNoInline)
	noin.Entry.Kind = ssa.BlockRet
	noin.Entry.SetControl(noin.Entry.NewValue(ssa.OpInitMem, ssa.TMem))

	self := newFunc("self", ssa.PragmaInline)
	b := self.Entry
	b.Kind = ssa.BlockRet
	b.SetControl(call(b, self, b.NewValue(ssa.OpInitMem, ssa.TMem), 10))

	small := newFunc("small", ssa.PragmaInline)
	small.Entry.Kind = ssa.BlockRet
	small.Entry.SetControl(small.Entry.NewValue(ssa.OpInitMem, ssa.TMem))
	rec := p.Sym("small·f", ssa.SymData)
	p.Data = append(p.Data, &ssa.Data{Sym: rec, Size: 8, Align: 8, ReadOnly: true, Relocs: []ssa.DataReloc{{Sym: small.Sym}}})

	main := newFunc("main", 0)
	b = main.Entry
	b.Kind = ssa.BlockRet
	m := b.NewValue(ssa.OpInitMem, ssa.TMem)
	m = call(b, small, m, 20)
	m = call(b, noin, m, 21)
	m = call(b, self, m, 22)
	fv := b.NewValueA(ssa.OpSymAddr, ssa.TPtr, rec)
	fv.Pos = 23
	use := b.NewValueA(ssa.OpStore, ssa.TMem, ssa.TPtr, b.NewValueA(ssa.OpSymAddr, ssa.TPtr, p.Sym("sink", ssa.SymData)), fv, m)
	m = call(b, exit, use, 24)
	b.SetControl(m)
	for _, f := range p.Funcs {
		if err := f.Verify(); err != nil {
			t.Fatal(err)
		}
	}

	// Before inlining, every call is reported.
	got := notesString(collectInlineNotes(p, nil, false))
	want := "10 self: inlining runs only at -O2\n" +
		"20 small: inlining runs only at -O2\n" +
		"21 noin: inlining runs only at -O2\n" +
		"22 self: inlining runs only at -O2\n" +
		"23 small (func value): calls through a func value are not inlined\n" +
		"24 exit: inlining runs only at -O2\n"
	if got != want {
		t.Errorf("before inlining:\n%swant:\n%s", got, want)
	}

	why := map[*ssa.Value]string{}
	inline(p, why)
	got = notesString(collectInlineNotes(p, why, true))
	// self is inlined into main, and the call in each copy again, until
	// the rounds run out; the copies are merged with self's own call.
	want = "10 self: the call is recursive; the inliner stopped after its last round\n" +
		"21 noin: it is also marked //pu:noinline\n" +
		"23 small (func value): calls through a func value are not inlined\n" +
		"24 exit: it never returns\n"
	if got != want {
		t.Errorf("after inlining:\n%swant:\n%s", got, want)
	}
	if !strings.Contains(notInlinable(small), "too large") {
		t.Errorf("notInlinable(small) = %q", notInlinable(small))
	}
}

func notesString(notes []InlineNote) string {
	var b strings.Builder
	for _, n := range notes {
		b.WriteString(strconv.Itoa(int(n.Pos)) + " " + n.Callee)
		if n.FuncValue {
			b.WriteString(" (func value)")
		}
		b.WriteString(": " + n.Reason + "\n")
	}
	return b.String()
}
