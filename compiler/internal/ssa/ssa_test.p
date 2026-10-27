// Ported from internal/ssa/schedule_test.go and verify_test.go at the
// go-cutover tag.

package ssa

import (
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/pu"
)

// indexIn returns the place of v in vs, or -1.
func indexIn(vs []*Value, v *Value) int {
	for i, w := range vs {
		if w == v {
			return i
		}
	}
	return -1
}

// TestScheduleLoadPastDisjointStore checks that a load reading the memory
// from before a store it cannot alias keeps its place after that store,
// and that a load it may alias still goes first.
func TestScheduleLoadPastDisjointStore(t *testing.T) {
	f := NewFunc("f")
	f.Params = compat.Append[*Type](nil, TPtr)
	f.Results = compat.Append[*Type](nil, TI64)
	b := f.Entry
	b.Kind = BlockRet
	mem := b.NewValue(OpInitMem, TMem)
	p := b.NewValueI(OpArg, TPtr, 0)
	p8 := b.NewValueI(OpOffPtr, TPtr, 8, p)
	one := f.ConstInt(TI64, 1)
	st0 := b.NewValueA(OpStore, TMem, TI64, p, one, mem)  // writes p[0]
	ld8 := b.NewValue(OpLoad, TI64, p8, mem)              // p[8], from before st0
	ld0 := b.NewValue(OpLoad, TI64, p, mem)               // p[0], from before st0
	st8 := b.NewValueA(OpStore, TMem, TI64, p8, one, st0) // writes p[8]
	sum := b.NewValue(OpAdd, TI64, ld8, ld0)
	b.SetControl(st8, sum)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	Schedule(f)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	pld0, pst0 := indexIn(b.Values, ld0), indexIn(b.Values, st0)
	pld8, pst8 := indexIn(b.Values, ld8), indexIn(b.Values, st8)
	if pld0 > pst0 {
		t.Errorf("load of p[0] moved below the store to p[0]:\n%s", f.String())
	}
	if pld8 < pst0 || pld8 > pst8 {
		t.Errorf("load of p[8] should stay between the stores:\n%s", f.String())
	}
}

type disjointCase struct {
	p    *Value
	n    int64
	q    *Value
	m    int64
	want bool
}

func TestDisjoint(t *testing.T) {
	f := NewFunc("f")
	b := f.Entry
	p := b.NewValueI(OpArg, TPtr, 0)
	q := b.NewValueI(OpArg, TPtr, 1)
	s1 := b.NewValueA(OpSymAddr, TPtr, pu.New(Sym{Name: "a"}))
	s2 := b.NewValueA(OpSymAddr, TPtr, pu.New(Sym{Name: "b"}))
	off := func(x *Value, c int64) *Value { return b.NewValueI(OpOffPtr, TPtr, c, x) }
	tests := []disjointCase{
		{p, 8, off(p, 8), 8, true},
		{p, 8, off(p, 4), 8, false},
		{p, 8, q, 8, false},             // unknown pointers
		{s1, 8, s2, 8, true},            // different globals
		{off(s1, 16), 8, s2, -1, true},  // unknown size, different globals
		{s1, -1, off(s1, 64), 8, false}, // unknown size, same global
		{off(s1, 8), 8, off(s1, 16), 8, true},
	}
	for i, tc := range tests {
		if got := Disjoint(tc.p, tc.n, tc.q, tc.m); got != tc.want {
			t.Errorf("%d: Disjoint = %v, want %v", i, got, tc.want)
		}
	}
	if !SameBytes(off(off(p, 8), 8), off(p, 16)) {
		t.Error("p+8+8 and p+16 should be the same bytes")
	}
}

// posCase is one way of breaking the positions in TestVerifyPos.
type posCase struct {
	name string
	set  int
	want string
}

// TestVerifyPos checks that VerifyPos accepts positions in the code their
// inline site names and reports a position from an inlined body that has
// lost its inline site, the way a pass copying Pos without Inl loses it.
func TestVerifyPos(t *testing.T) {
	g := NewFunc("g")
	g.Pos, g.End = 300, 400

	f := NewFunc("f")
	f.Pos, f.End = 100, 200
	b := f.Entry
	b.Kind = BlockRet
	b.Pos = 190
	mem := b.NewValue(OpInitMem, TMem)
	p := b.NewValue(OpConst, TPtr)
	p.Pos = 150
	call := b.NewValue(OpCall, TMem, mem)
	call.Pos = 160
	site := f.AddInline(call, g).Map(0)
	if s := f.Inlines[site-1]; s.Start != g.Pos || s.End != g.End || s.Pos != call.Pos {
		t.Fatalf("inline site %d/%d/%d", int64(s.Start), int64(s.End), int64(s.Pos))
	}
	ld := b.NewValue(OpLoad, TI64, p, mem)
	ld.Pos, ld.Inl = 350, site
	b.SetControl(mem)
	if err := f.VerifyPos(); err != nil {
		t.Fatal(err)
	}

	// A copy keeps the inline site.
	cp := b.NewValue(OpLoad, TI64, p, mem)
	cp.CopyPos(ld)
	if cp.Pos != ld.Pos || cp.Inl != ld.Inl {
		t.Errorf("CopyPos: got %d/%d, want %d/%d", int64(cp.Pos), cp.Inl, int64(ld.Pos), ld.Inl)
	}
	nb := f.NewBlock(BlockPlain)
	nb.Pos, nb.Inl = 320, site
	c := f.NewBlock(BlockPlain)
	c.CopyPos(nb)
	if c.Pos != nb.Pos || c.Inl != nb.Inl {
		t.Errorf("Block.CopyPos: got %d/%d, want %d/%d", int64(c.Pos), c.Inl, int64(nb.Pos), nb.Inl)
	}
	if err := f.VerifyPos(); err != nil {
		t.Fatal(err)
	}

	cases := []posCase{
		{"value without its site", 0, compat.Concat(cp.String(), " (Load): position 350 (inline site 0) is outside f [100, 200]")},
		{"value with the wrong site", 1, "position 150 (inline site 1) is outside g [300, 400]"},
		{"block without its site", 2, compat.Concat(c.String(), ": position 320 (inline site 0) is outside f")},
		{"site out of range", 3, "inline site 7 out of range (1 sites)"},
	}
	for _, tc := range cases {
		saved := [...]int32{cp.Inl, p.Inl, c.Inl, ld.Inl}
		switch tc.set {
		case 0:
			cp.Inl = 0
		case 1:
			p.Inl = site
		case 2:
			c.Inl = 0
		case 3:
			ld.Inl = 7
		}
		err := f.VerifyPos()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", tc.name, err, tc.want)
		}
		cp.Inl, p.Inl, c.Inl, ld.Inl = saved[0], saved[1], saved[2], saved[3]
	}

	// Without a known extent (synthesized functions) positions are not
	// checked, but sites still are.
	f.End = 0
	cp.Inl = 0
	if err := f.VerifyPos(); err != nil {
		t.Errorf("function without an extent: %v", err)
	}
}

// verifyFixture is the function TestVerify breaks in each case.
type verifyFixture struct {
	f      *Func
	c      *Block
	mem, x *Value
}

func buildVerifyFixture() verifyFixture {
	f := NewFunc("f")
	b := f.Entry
	b.Kind = BlockPlain
	mem := b.NewValue(OpInitMem, TMem)
	x := b.NewValue(OpConst, TI64)
	c := f.NewBlock(BlockRet)
	b.AddEdgeTo(c)
	c.NewValue(OpAdd, TI64, x, x)
	c.SetControl(mem)
	return verifyFixture{f, c, mem, x}
}

// TestVerify checks that Verify reports values and blocks listed twice,
// arguments from another function, wrong use counts and arguments that do
// not dominate their uses.
func TestVerify(t *testing.T) {
	if fx := buildVerifyFixture(); fx.f.Verify() != nil {
		t.Fatal(fx.f.Verify())
	}
	cases := []posCase{
		{"value listed twice", 0, "appears in"},
		{"block listed twice", 1, "listed twice"},
		{"argument from another function", 2, "not in function"},
		{"use count", 3, "use count 3, counted 2"},
		{"use before definition", 4, "does not dominate its use"},
	}
	for _, tc := range cases {
		fx := buildVerifyFixture()
		f, c, x := fx.f, fx.c, fx.x
		switch tc.set {
		case 0:
			c.Values = compat.Append(c.Values, c.Values[0])
		case 1:
			f.Blocks = compat.Append(f.Blocks, c)
		case 2:
			g := NewFunc("g")
			y := g.Entry.NewValue(OpConst, TI64)
			c.Values[0].SetArg(1, y)
		case 3:
			x.Uses++
		case 4:
			c.NewValue(OpNeg, TI64, x)
			c.Values[0], c.Values[1] = c.Values[1], c.Values[0]
			c.Values[0].SetArg(0, c.Values[1])
		}
		err := f.Verify()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", tc.name, err, tc.want)
		}
	}
}
