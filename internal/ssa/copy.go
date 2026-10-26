package ssa

import "strings"

// A Copier moves a function's code into fresh memory: its blocks, values,
// slots, call descriptions, tuple types and the slices and strings they
// hold. The self-hosted compiler optimises each function in scratch memory
// and copies it out this way, so that what the passes allocated and
// dropped goes when the step ends. NewCopier makes the copier's tables,
// which Copy only fills: they may come from the scratch memory, and
// nothing Copy allocates outlives the copy except the copy itself.
type Copier struct {
	vals   []valPair
	blocks []blockPair
	slots  []slotPair
	vwork  []*Value // originals whose copies are still to be filled
	bwork  []*Block
	prog   *ProgramCopier // when copying a whole program: its symbols' copies
}

type valPair struct{ old, new *Value }
type blockPair struct{ old, new *Block }
type slotPair struct{ old, new *Slot }

// NewCopier returns a copier for f as it is now.
func NewCopier(f *Func) *Copier {
	return &Copier{
		vals:   make([]valPair, f.nextValue),
		blocks: make([]blockPair, f.nextBlock),
		slots:  make([]slotPair, f.nextSlot),
		vwork:  make([]*Value, 0, f.nextValue),
		bwork:  make([]*Block, 0, f.nextBlock),
	}
}

// Copy replaces f's code with a copy of it in the current allocator's
// memory. f keeps its identity; the analyses it cached are dropped.
func (c *Copier) Copy(f *Func) {
	blocks := make([]*Block, len(f.Blocks))
	for i, b := range f.Blocks {
		blocks[i] = c.block(b, f)
	}
	var entry *Block
	if f.Entry != nil {
		entry = c.block(f.Entry, f)
	}
	slots := make([]*Slot, len(f.Slots))
	for i, s := range f.Slots {
		slots[i] = c.slot(s)
	}
	var rec *Slot
	if f.DeferRecord != nil {
		rec = c.slot(f.DeferRecord)
	}
	var holds []StrHold
	if f.StrHolds != nil {
		holds = make([]StrHold, len(f.StrHolds))
		for i, h := range f.StrHolds {
			holds[i] = StrHold{Retains: c.values(h.Retains), Releases: c.values(h.Releases), Covered: c.values(h.Covered)}
		}
	}
	c.drain(f)

	inlines := CloneInlines(f.Inlines)
	f.Blocks, f.Entry, f.Slots = blocks, entry, slots
	f.DeferRecord, f.StrHolds, f.Inlines = rec, holds, inlines
	f.Params, f.Results = c.types(f.Params), c.types(f.Results)
	f.ParamOnStack = copyBools(f.ParamOnStack)
	f.GenOwnerArgs = copyInts(f.GenOwnerArgs)
	f.cachedDom, f.cachedLoops, f.cachedOrder = nil, nil, nil
}

// drain fills in the copies made but not yet filled.
func (c *Copier) drain(f *Func) {
	for len(c.vwork) > 0 || len(c.bwork) > 0 {
		for len(c.vwork) > 0 {
			v := c.vwork[len(c.vwork)-1]
			c.vwork = c.vwork[:len(c.vwork)-1]
			c.fillValue(v, f)
		}
		for len(c.bwork) > 0 {
			b := c.bwork[len(c.bwork)-1]
			c.bwork = c.bwork[:len(c.bwork)-1]
			c.fillBlock(b, f)
		}
	}
}

// value returns the copy of v, making it if need be.
func (c *Copier) value(v *Value) *Value {
	if v == nil {
		return nil
	}
	if v.ID < 0 || v.ID >= len(c.vals) {
		panic("ssa: copying a value whose ID is out of range")
	}
	p := &c.vals[v.ID]
	if p.old == v {
		return p.new
	}
	if p.old != nil {
		panic("ssa: copying values that share an ID")
	}
	p.old, p.new = v, &Value{}
	c.vwork = append(c.vwork, v)
	return p.new
}

func (c *Copier) values(vs []*Value) []*Value {
	if vs == nil {
		return nil
	}
	out := make([]*Value, len(vs))
	for i, v := range vs {
		out[i] = c.value(v)
	}
	return out
}

func (c *Copier) fillValue(v *Value, f *Func) {
	n := c.vals[v.ID].new
	n.ID, n.Op, n.Type = v.ID, v.Op, c.typ(v.Type)
	n.AuxInt, n.Pos, n.Uses, n.Inl = v.AuxInt, v.Pos, v.Uses, v.Inl
	if len(v.Args) <= len(n.argStorage) {
		n.Args = n.argStorage[:len(v.Args)]
	} else {
		n.Args = make([]*Value, len(v.Args))
	}
	for i, a := range v.Args {
		n.Args[i] = c.value(a)
	}
	switch aux := v.Aux.(type) {
	case *Slot:
		n.Aux = c.slot(aux)
	case *Type:
		n.Aux = c.typ(aux)
	case *Sym:
		n.Aux = c.prog.Sym(aux)
	case *Intrinsic:
		if c.prog != nil {
			n.Aux = c.prog.Intrinsic(aux)
		} else {
			n.Aux = aux
		}
	default:
		if v.Aux != nil && c.prog != nil {
			panic("ssa: copying a value with an unknown kind of aux")
		}
		n.Aux = v.Aux
	}
	if v.Call != nil {
		ci := *v.Call
		ci.Results = c.types(ci.Results)
		ci.OnStack = copyBools(ci.OnStack)
		n.Call = &ci
	}
	if v.Block != nil {
		n.Block = c.block(v.Block, f)
	}
}

// block returns the copy of b, making it if need be.
func (c *Copier) block(b *Block, f *Func) *Block {
	if b.ID < 0 || b.ID >= len(c.blocks) {
		panic("ssa: copying a block whose ID is out of range")
	}
	p := &c.blocks[b.ID]
	if p.old == b {
		return p.new
	}
	if p.old != nil {
		panic("ssa: copying blocks that share an ID")
	}
	p.old, p.new = b, &Block{}
	c.bwork = append(c.bwork, b)
	return p.new
}

func (c *Copier) fillBlock(b *Block, f *Func) {
	n := c.blocks[b.ID].new
	n.ID, n.Kind, n.Func, n.Likely, n.Pos, n.Inl = b.ID, b.Kind, f, b.Likely, b.Pos, b.Inl
	n.Values = c.values(b.Values)
	n.Controls = c.values(b.Controls)
	n.Succs = c.edges(b.Succs, f)
	n.Preds = c.edges(b.Preds, f)
}

func (c *Copier) edges(es []Edge, f *Func) []Edge {
	if es == nil {
		return nil
	}
	out := make([]Edge, len(es))
	for i, e := range es {
		out[i] = Edge{B: c.block(e.B, f), I: e.I}
	}
	return out
}

// slot returns the copy of s, making it if need be.
func (c *Copier) slot(s *Slot) *Slot {
	if s.ID < 0 || s.ID >= len(c.slots) {
		panic("ssa: copying a slot whose ID is out of range")
	}
	p := &c.slots[s.ID]
	if p.old == s {
		return p.new
	}
	if p.old != nil {
		panic("ssa: copying slots that share an ID")
	}
	n := &Slot{ID: s.ID, Name: strings.Clone(s.Name), Size: s.Size, Align: s.Align, Closure: s.Closure, Offset: s.Offset}
	n.Leaves = c.leaves(s.Leaves)
	n.Pads = copyPads(s.Pads)
	n.Parts = c.parts(s.Parts)
	p.old, p.new = s, n
	return n
}

func (c *Copier) leaves(ls []Leaf) []Leaf {
	if ls == nil {
		return nil
	}
	out := make([]Leaf, len(ls))
	for i, l := range ls {
		out[i] = Leaf{Off: l.Off, Type: c.typ(l.Type)}
	}
	return out
}

func (c *Copier) parts(ps []Part) []Part {
	if ps == nil {
		return nil
	}
	out := make([]Part, len(ps))
	for i, p := range ps {
		out[i] = Part{Off: p.Off, Size: p.Size, Align: p.Align, Leaves: c.leaves(p.Leaves), Pads: copyPads(p.Pads),
			Closure: p.Closure, Region: p.Region, Parts: c.parts(p.Parts)}
	}
	return out
}

// typ returns t, or a copy of it if it is a tuple: tuples are made as
// needed and compared by their elements, every other type is predeclared
// and compared by pointer.
func (c *Copier) typ(t *Type) *Type {
	if t == nil || t.Kind != KTuple {
		return t
	}
	return &Type{Kind: KTuple, Size: t.Size, Elems: c.types(t.Elems)}
}

func (c *Copier) types(ts []*Type) []*Type {
	if ts == nil {
		return nil
	}
	out := make([]*Type, len(ts))
	for i, t := range ts {
		out[i] = c.typ(t)
	}
	return out
}

func copyPads(ps []Pad) []Pad {
	if ps == nil {
		return nil
	}
	out := make([]Pad, len(ps))
	copy(out, ps)
	return out
}

func copyBools(bs []bool) []bool {
	if bs == nil {
		return nil
	}
	out := make([]bool, len(bs))
	copy(out, bs)
	return out
}

// CloneInlines returns a copy of sites, and of the names they hold, in the
// current allocator's memory: what outlives the function's code, such as
// its debug information, keeps the copy.
func CloneInlines(sites []InlineSite) []InlineSite {
	if sites == nil {
		return nil
	}
	out := make([]InlineSite, len(sites))
	for i, s := range sites {
		s.Func = strings.Clone(s.Func)
		out[i] = s
	}
	return out
}

func copyInts(xs []int64) []int64 {
	if xs == nil {
		return nil
	}
	out := make([]int64, len(xs))
	copy(out, xs)
	return out
}
