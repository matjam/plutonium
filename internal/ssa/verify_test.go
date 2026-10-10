package ssa

import (
	"strings"
	"testing"
)

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
		t.Fatalf("inline site %+v", s)
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
		t.Errorf("CopyPos: got %d/%d, want %d/%d", cp.Pos, cp.Inl, ld.Pos, ld.Inl)
	}
	nb := f.NewBlock(BlockPlain)
	nb.Pos, nb.Inl = 320, site
	c := f.NewBlock(BlockPlain)
	c.CopyPos(nb)
	if c.Pos != nb.Pos || c.Inl != nb.Inl {
		t.Errorf("Block.CopyPos: got %d/%d, want %d/%d", c.Pos, c.Inl, nb.Pos, nb.Inl)
	}
	if err := f.VerifyPos(); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		set  func()
		want string
	}{
		{"value without its site", func() { cp.Inl = 0 }, cp.String() + " (Load): position 350 (inline site 0) is outside f [100, 200]"},
		{"value with the wrong site", func() { p.Inl = site }, "position 150 (inline site 1) is outside g [300, 400]"},
		{"block without its site", func() { c.Inl = 0 }, c.String() + ": position 320 (inline site 0) is outside f"},
		{"site out of range", func() { ld.Inl = 7 }, "inline site 7 out of range (1 sites)"},
	} {
		saved := [...]int32{cp.Inl, p.Inl, c.Inl, ld.Inl}
		tc.set()
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

// TestVerify checks that Verify reports values and blocks listed twice,
// arguments from another function, wrong use counts and arguments that do
// not dominate their uses.
func TestVerify(t *testing.T) {
	build := func() (*Func, *Block, *Value, *Value) {
		f := NewFunc("f")
		b := f.Entry
		b.Kind = BlockPlain
		mem := b.NewValue(OpInitMem, TMem)
		x := b.NewValue(OpConst, TI64)
		c := f.NewBlock(BlockRet)
		b.AddEdgeTo(c)
		c.NewValue(OpAdd, TI64, x, x)
		c.SetControl(mem)
		return f, c, mem, x
	}
	if f, _, _, _ := build(); f.Verify() != nil {
		t.Fatal(f.Verify())
	}
	for _, tc := range []struct {
		name string
		set  func(f *Func, c *Block, mem, x *Value)
		want string
	}{
		{"value listed twice", func(f *Func, c *Block, mem, x *Value) {
			c.Values = append(c.Values, c.Values[0])
		}, "appears in"},
		{"block listed twice", func(f *Func, c *Block, mem, x *Value) {
			f.Blocks = append(f.Blocks, c)
		}, "listed twice"},
		{"argument from another function", func(f *Func, c *Block, mem, x *Value) {
			g := NewFunc("g")
			y := g.Entry.NewValue(OpConst, TI64)
			c.Values[0].SetArg(1, y)
		}, "not in function"},
		{"use count", func(f *Func, c *Block, mem, x *Value) {
			x.Uses++
		}, "use count 3, counted 2"},
		{"use before definition", func(f *Func, c *Block, mem, x *Value) {
			c.NewValue(OpNeg, TI64, x)
			c.Values[0], c.Values[1] = c.Values[1], c.Values[0]
			c.Values[0].SetArg(0, c.Values[1])
		}, "does not dominate its use"},
	} {
		f, c, mem, x := build()
		tc.set(f, c, mem, x)
		err := f.Verify()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", tc.name, err, tc.want)
		}
	}
}
