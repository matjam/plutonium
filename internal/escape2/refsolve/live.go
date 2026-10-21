package refsolve

import "github.com/matjam/plutonium/internal/escape2/eir"

// uses reports whether a non-phi instruction reads register r.
func uses(in *eir.Instr, r eir.Reg) bool {
	if in.X == r || in.Y == r {
		return true
	}
	for _, a := range in.Args {
		if a == r {
			return true
		}
	}
	return in.Call != nil && in.Call.Target == r
}

// defines reports whether in defines register r.
func defines(in *eir.Instr, r eir.Reg) bool {
	if in.Dst == r {
		return true
	}
	if in.Call != nil {
		for _, x := range in.Call.Results {
			if x == r {
				return true
			}
		}
	}
	return false
}

// liveAfter reports whether register r is live after instruction at in (of
// block b): some path from there reaches a use of r without passing a
// definition of r. A phi uses its argument at the end of the predecessor.
func (s *solver) liveAfter(b *eir.Block, at *eir.Instr, r eir.Reg) bool {
	i := s.indexOf[at]
	if rest, done := s.scan(b, i+1, r); done {
		return rest
	}
	seen := map[eir.BlockID]bool{}
	work := []eir.BlockID{}
	push := func(from *eir.Block) bool { // leaving from: phi uses, then entering
		for _, sid := range from.Succs {
			succ := s.f.Blocks[sid]
			for _, p := range succ.Instrs {
				if p.Op != eir.OpPhi {
					break
				}
				for k, a := range p.Args {
					if a == r && k < len(succ.Preds) && succ.Preds[k] == from.ID {
						return true
					}
				}
			}
			if !seen[sid] {
				seen[sid] = true
				work = append(work, sid)
			}
		}
		return false
	}
	if push(b) {
		return true
	}
	for len(work) > 0 {
		cur := s.f.Blocks[work[0]]
		work = work[1:]
		killed := false
		for _, p := range cur.Instrs {
			if p.Op == eir.OpPhi && p.Dst == r {
				killed = true
			}
		}
		if killed {
			continue
		}
		if live, done := s.scan(cur, 0, r); done {
			if live {
				return true
			}
			continue
		}
		if push(cur) {
			return true
		}
	}
	return false
}

// scan looks at the non-phi instructions of b from index from: done reports
// that a use (live true) or a definition (live false) ended the search.
func (s *solver) scan(b *eir.Block, from int, r eir.Reg) (live, done bool) {
	for j := from; j < len(b.Instrs); j++ {
		in := &b.Instrs[j]
		if in.Op == eir.OpPhi {
			continue
		}
		if uses(in, r) {
			return true, true
		}
		if defines(in, r) {
			return false, true
		}
	}
	return false, false
}
