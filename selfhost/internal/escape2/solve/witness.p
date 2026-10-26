// Translated by go2pu from internal/escape2/solve/witness.go at the go-cutover tag; edited by hand since.

package solve

import (
	"selfhost/compat"
	"selfhost/internal/escape2/eir"
	"selfhost/internal/syntax/token"
)

// Explain returns the witness of a finding of Solve(f, env, ws): the
// positions, in order, of the instructions through which a pointer to the
// finding's object reaches it (where the pointer is made, the stores, loads,
// copies and calls it goes through, and the finding's own position). Stage 5
// turns them into the lines of a message, with the callee's own reasons for
// a finding through a call (Finding.Callee and Entry).
//
// Nothing of a solve is kept for this: Explain solves f again in ws and
// searches the constraint graph forward from the object, along the edges
// whose targets hold a pointer to it, for the node where the finding was
// made. It is meant for functions with findings only.
func Explain(f *eir.Func, env Env, ws *Workspace, fd Finding) []token.Pos {
	s := newSolver(f, env, ws)
	s.build()
	s.run()
	o := uint32(fd.Obj)
	if fd.Obj == eir.NoObj {
		o = uint32(len(f.Objects)) + uint32(fd.Site)
	}
	var queue vec
	var pred table // node -> predecessor node +1 (0: a start), and the edge's why
	var why table
	visit := func(n, from uint32, w uint64) {
		n = s.find(n)
		if _, ok := pred.get(uint64(n), 0); ok || !s.holdsObj(n, o) {
			return
		}
		pred.put(s.a, uint64(n), 0, uint64(from))
		why.put(s.a, uint64(n), 0, w)
		queue.push(s.a, uint64(n))
	}
	for r := 1; r <= f.NumRegs; r++ {
		if d := s.defs.s[r]; d != 0 {
			in := &f.Blocks[d>>32].Instrs[uint32(d)-1]
			switch in.Op {
			case eir.OpAddr, eir.OpConv, eir.OpClosure, eir.OpMethodVal:
				if uint32(in.Obj) == o {
					visit(uint32(r), 0, 0)
				}
			case eir.OpCall:
				if fd.Obj == eir.NoObj && in.Call.Site == fd.Site {
					visit(uint32(r), 0, 0)
				}
			}
		}
	}
	// The nodes calls share for a pointer passed as an argument (canonBase)
	// start a path too.
	for n := uint32(f.NumRegs + 1); int(n) < len(s.nodeRep.s); n++ {
		if s.nodeKind(n) == nkTok && s.find(n) == n {
			visit(n, 0, 0)
		}
	}
	end := uint32(0)
	found := false
	for i := 0; i < len(queue.s) && !found; i++ {
		n := uint32(queue.s[i])
		if s.madeAt(n, fd.Pos) {
			end, found = n, true
			break
		}
		for e := s.nodeEdges.s[n]; e != 0; e = s.edges.at(e - 1)[3] {
			w := s.edges.at(e - 1)
			visit(uint32(w[0]), n+1, w[2])
		}
	}
	if !found {
		return compat.Clone([]token.Pos{fd.Pos})
	}
	// Walk back, then reverse.
	var path []token.Pos
	add := func(p token.Pos) {
		if p.IsValid() && (len(path) == 0 || path[len(path)-1] != p) {
			path = compat.AppendPlain(path, p)
		}
	}
	add(fd.Pos)
	for n := end; ; {
		if w, _ := why.get(uint64(n), 0); w != 0 {
			p, _, _, _, _, _ := s.whyAt(w)
			add(p)
		}
		if int(n) <= f.NumRegs {
			if d := s.defs.s[n]; d != 0 {
				add(f.Blocks[d>>32].Instrs[uint32(d)-1].Pos)
			}
		}
		p, _ := pred.get(uint64(n), 0)
		if p == 0 {
			break
		}
		n = uint32(p - 1)
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// holdsObj reports whether node n holds a pointer into object o.
func (s *solver) holdsObj(n, o uint32) bool {
	for _, id := range s.pts(n) {
		if t := s.tokAt(id); t.kind == tObj && t.base == o {
			return true
		}
	}
	return false
}

// madeAt reports whether a finding at pos may have been made at node n: a
// constraint of n, or a store edge out of n, comes from pos; or n is a
// register live at a drop at pos.
func (s *solver) madeAt(n uint32, pos token.Pos) bool {
	for c := s.nodeCons.s[n]; c != 0; c = s.cons.at(c - 1)[3] {
		w := s.cons.at(c - 1)
		switch int(w[0]) {
		case cStore, cStoreIn, cSink, cReturn, cRegionChk, cRegionWalk, cBack:
			if p, _, _, _, _, _ := s.whyAt(w[2]); p == pos {
				return true
			}
		}
	}
	for e := s.nodeEdges.s[n]; e != 0; e = s.edges.at(e - 1)[3] {
		w := s.edges.at(e - 1)
		if w[2] != 0 {
			if p, _, _, _, _, _ := s.whyAt(w[2]); p == pos {
				return true
			}
		}
	}
	for _, d := range s.drops.s {
		if in := &s.f.Blocks[d>>32].Instrs[uint32(d)]; in.Pos == pos && int(n) <= s.f.NumRegs {
			return true
		}
	}
	return false
}
