package amd64

import (
	"fmt"

	"github.com/matjam/plutonium/internal/ssa"
)

// A Code is a compiled function.
type Code struct {
	Sym    *ssa.Sym
	Bytes  []byte
	Relocs []Reloc
	Asm    string // machine IR listing, when requested
}

// Compile turns an SSA function into machine code. The function must be
// scheduled and have no critical edges (see Prepare).
func Compile(f *ssa.Func, listing bool) (*Code, error) {
	mf, err := Select(f)
	if err != nil {
		return nil, err
	}
	expandPCopies(mf)
	if err := allocate(mf); err != nil {
		return nil, err
	}
	layoutFrame(mf)
	insertFrame(mf)
	order := layoutBlocks(mf)
	code, err := encodeFunc(mf, order)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	code.Sym = f.Sym
	if listing {
		mf.Blocks = order
		code.Asm = mf.String()
	}
	return code, nil
}

// Prepare applies the SSA transformations the backend relies on.
func Prepare(f *ssa.Func) {
	ssa.CopyElim(f)
	ssa.DeadCode(f)
	ssa.SplitCriticalEdges(f)
	ssa.Schedule(f)
}

// layoutBlocks orders the blocks for emission and removes jumps to the
// next block.
func layoutBlocks(f *MFunc) []*MBlock {
	// Every block still ends in an explicit transfer. Thread branches through
	// blocks that only jump on (left behind by critical-edge splitting when
	// no copies were needed), then drop those that nothing reaches.
	forward := func(t *MBlock) *MBlock {
		for range 8 {
			if len(t.Insts) != 1 || t.Insts[0].Op != MJMP || t.Insts[0].Target == t {
				break
			}
			t = t.Insts[0].Target
		}
		return t
	}
	used := map[*MBlock]bool{f.Order[0]: true}
	for _, b := range f.Order {
		for _, in := range b.Insts {
			if in.Op == MJMP || in.Op == MJCC {
				in.Target = forward(in.Target)
				used[in.Target] = true
			}
		}
	}
	var order []*MBlock
	for _, b := range f.Order {
		if used[b] {
			order = append(order, b)
		}
	}

	// Remove jumps to the following block; turn "jcc T; jmp F" with T next
	// into "jncc F".
	for i, b := range order {
		var next *MBlock
		if i+1 < len(order) {
			next = order[i+1]
		}
		n := len(b.Insts)
		if n == 0 {
			continue
		}
		last := b.Insts[n-1]
		if last.Op != MJMP {
			continue
		}
		if last.Target == next {
			b.Insts = b.Insts[:n-1]
			continue
		}
		if n >= 2 {
			prev := b.Insts[n-2]
			if prev.Op == MJCC && prev.Target == next && (n < 3 || b.Insts[n-3].Op != MJCC) {
				prev.Cond = prev.Cond.Invert()
				prev.Target = last.Target
				b.Insts = b.Insts[:n-1]
			}
		}
	}
	return order
}

// encodeFunc emits the blocks in order and resolves branches, using short
// jumps where the target is near.
func encodeFunc(f *MFunc, order []*MBlock) (*Code, error) {
	e := &encoder{}
	for _, b := range order {
		b.offset = len(e.code)
		for _, in := range b.Insts {
			e.encode(in)
		}
	}
	if e.err != nil {
		return nil, e.err
	}
	for _, fx := range e.fixups {
		rel := int64(fx.target.offset - fx.end)
		binary32(e.code[fx.at:], rel)
	}
	return &Code{Bytes: e.code, Relocs: e.relocs}, nil
}

func binary32(b []byte, v int64) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}
