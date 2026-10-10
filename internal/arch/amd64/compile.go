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
	// Greedy chaining: follow fall-through preferences from each block,
	// starting with the entry, placing unlikely (panic) paths last.
	placed := map[*MBlock]bool{}
	var order []*MBlock
	var cold []*MBlock
	isCold := func(b *MBlock) bool {
		n := len(b.Insts)
		return n > 0 && b.Insts[n-1].Op == MUD2
	}
	visit := func(b *MBlock) {
		for b != nil && !placed[b] {
			placed[b] = true
			if isCold(b) && b != f.Blocks[0] {
				cold = append(cold, b)
			} else {
				order = append(order, b)
			}
			// Prefer to fall into the target of the final jump.
			var next *MBlock
			if n := len(b.Insts); n > 0 {
				last := b.Insts[n-1]
				if last.Op == MJMP && !placed[last.Target] {
					next = last.Target
				}
			}
			b = next
		}
	}
	visit(f.Blocks[0])
	for _, b := range f.Blocks {
		visit(b)
	}
	order = append(order, cold...)

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
