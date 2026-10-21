package solve

import (
	"github.com/matjam/plutonium/internal/escape2/domain"
)

// The constraint graph. Nodes are the registers (node r for register r,
// node 0 the empty register), the places of frame objects, and nodes the
// solver derives for call application (the values of summary keys). Each
// node has the tagged tokens it holds as an append-only list, with a bitset
// over token numbers for membership, and the length of the list already
// propagated (done): difference propagation sends only the list's tail. A
// node's memory is linear in what it holds; hash-consed sets would keep
// every intermediate set of a node that grows one token at a time, which is
// quadratic (seed 5401 of the differential test passed 256 MB that way).
//
// Simple constraints are edges with a transform (xform); complex ones
// (loads, stores, sinks, summary keys) are attached to the node whose tokens
// they read and fire on each new token. Copy cycles among registers and
// derived nodes are collapsed (lazy cycle detection: an identity edge whose
// two ends have equal sets after propagation starts a search).

// Node words: lists, bits and done (per node, above), nodeRep (union-find parent),
// nodeEdges, nodeCons (list heads, index+1), nodeInfo (kind and the place's
// object and path), nodeNext (next place of the same object, +1).
const (
	nkReg    = iota // a register, or a derived plain node: collapsible
	nkPlace         // a place <o, p, pg>
	nkReach         // a reach node (not collapsible)
	nkPinned        // a derived node that must not be merged
)

// xform kinds, in an edge's transform word: kind (bits 0-7), flag (bit 8),
// path (bits 32-63).
const (
	xID      = iota // identity
	xSel            // sel(path)
	xPre            // pre(path), tag GE when flag
	xOff            // off(path)
	xStep           // a summary key's field step: sel ∪ off of root pointers
	xGe             // geify
	xRetag          // the tag made (Root, GE) only (loads through a cut pointer)
	xPreLoad        // a load link: pre(path), GE when flag
	xSelLoad        // a load link: sel(path), tags GE when flag
	xSelGe          // a reach link: sel(path), then geify
)

func xw(kind int, path domain.PathID, flag bool) uint64 {
	w := uint64(kind) | uint64(path)<<32
	if flag {
		w |= 1 << 8
	}
	return w
}

// Edge words: to, xform, why, next (index+1), flags (bit 0: cycle checked).
const edgeWords = 5

// Constraint words: kind, a, b, next (index+1).
const consWords = 4

const (
	cLoad       = iota + 1 // a: the destination node; b: 1 if heap-only
	cReach                 // the node's own tokens: subscribe their places
	cStore                 // a: the value node; b: why
	cStoreIn               // a: the destination token; b: why
	cSink                  // a: code index; b: why
	cReturn                // a: result index; b: why
	cRegionChk             // a: result index; b: why
	cBack                  // a: loop index; b: why
	cRegionWalk            // a: result index; b: why: the regions of calls in it
)

func (s *solver) newNode(kind uint64) uint32 {
	n := uint32(len(s.nodeRep.s))
	s.lists = append(s.lists, vec{})
	s.bits = append(s.bits, vec{})
	s.done.push(s.a, 0)
	s.nodeRep.push(s.a, uint64(n))
	s.nodeEdges.push(s.a, 0)
	s.nodeCons.push(s.a, 0)
	s.nodeInfo.push(s.a, kind)
	s.nodeInfo2.push(s.a, 0)
	s.nodeNext.push(s.a, 0)
	s.inWork.push(s.a, 0)
	return n
}

func (s *solver) find(n uint32) uint32 {
	for {
		p := uint32(s.nodeRep.s[n])
		if p == n {
			return n
		}
		gp := uint32(s.nodeRep.s[p])
		s.nodeRep.s[n] = uint64(gp)
		n = p
	}
}

// pts is the list of the tokens node n holds. The slice stays valid (and
// unchanged below its length) while the node grows.
func (s *solver) pts(n uint32) []uint64 { return s.lists[n].s }

// has reports whether node n holds token id.
func (s *solver) has(n uint32, id uint64) bool {
	b := s.bits[n].s
	w := int(id / 64)
	return w < len(b) && b[w]&(1<<(id%64)) != 0
}

// addID adds token id to node n and reports whether it was new.
func (s *solver) addID(n uint32, id uint64) bool {
	if s.has(n, id) {
		return false
	}
	w := int(id / 64)
	if w >= len(s.bits[n].s) {
		s.bits[n].resize(s.a, max(w+1, 2*len(s.bits[n].s)))
	}
	s.bits[n].s[w] |= 1 << (id % 64)
	s.lists[n].push(s.a, id)
	return true
}

func (s *solver) nodeKind(n uint32) int { return int(s.nodeInfo.s[n] & 0xff) }

// addTokens adds tokens to node n and queues it if it grew.
func (s *solver) addTokens(n uint32, ids []uint64) bool {
	n = s.find(n)
	grew := false
	for _, id := range ids {
		if s.addID(n, id) {
			grew = true
		}
	}
	if grew {
		s.enqueue(n)
	}
	return grew
}

func (s *solver) addTok(n uint32, t tok) bool {
	n = s.find(n)
	if !s.addID(n, s.intern(t)) {
		return false
	}
	s.enqueue(n)
	return true
}

func (s *solver) enqueue(n uint32) {
	if s.inWork.s[n] != 0 {
		return
	}
	s.inWork.s[n] = 1
	s.work.push(s.a, uint64(n))
}

// addEdge adds an edge from n to m and sends n's tokens along it at once.
// Duplicate edges are dropped.
func (s *solver) addEdge(n, m uint32, x uint64, why uint64) {
	n, m = s.find(n), s.find(m)
	if n == m && x == xw(xID, 0, false) {
		return
	}
	// De-duplicate exactly: an edge with a why (a store into a place) has
	// a transform with no path (identity or geify).
	k2 := x
	if why != 0 {
		if x>>32 != 0 || why >= 1<<54 {
			panic("internal compiler error: escape2/solve: store edge with a path")
		}
		k2 = 1<<63 | why<<8 | x&0xff
	}
	if !s.edgeSeen.add(s.a, uint64(n)<<32|uint64(m), k2) {
		return
	}
	e := uint64(len(s.edges.s) / edgeWords)
	s.edges.push(s.a, uint64(m))
	s.edges.push(s.a, x)
	s.edges.push(s.a, why)
	s.edges.push(s.a, s.nodeEdges.s[n])
	s.edges.push(s.a, 0)
	s.nodeEdges.s[n] = e + 1
	s.sendAlong(e, s.pts(n))
}

// addCons attaches a complex constraint to n and fires it on n's tokens.
func (s *solver) addCons(n uint32, kind int, a, b uint64) {
	n = s.find(n)
	if a >= 1<<32 || b >= 1<<32 {
		panic("internal compiler error: escape2/solve: constraint operand out of range")
	}
	if !s.consSeen.add(s.a, uint64(n)<<8|uint64(kind), a|b<<32) {
		return
	}
	c := uint64(len(s.cons.s) / consWords)
	s.cons.push(s.a, uint64(kind))
	s.cons.push(s.a, a)
	s.cons.push(s.a, b)
	s.cons.push(s.a, s.nodeCons.s[n])
	s.nodeCons.s[n] = c + 1
	for i := 0; i < len(s.lists[n].s); i++ {
		s.fire(n, c, s.lists[n].s[i])
	}
}

// sendAlong applies edge e to the token set d and adds the result to its
// target.
func (s *solver) sendAlong(e uint64, d []uint64) {
	if len(d) == 0 {
		return
	}
	w := s.edges.s[e*edgeWords : e*edgeWords+edgeWords]
	m := s.find(uint32(w[0]))
	x, why := w[1], w[2]
	kind := int(x & 0xff)
	if kind == xID && why == 0 {
		s.addTokens(m, d)
		return
	}
	s.buf.s = s.buf.s[:0]
	for _, id := range d {
		s.transform(x, s.tokAt(id))
	}
	if why != 0 && s.nodeKind(m) == nkPlace {
		for _, id := range s.buf.s {
			s.checkStore(m, s.tokAt(id), why)
		}
	}
	if len(s.buf.s) > 0 {
		s.addTokens(m, s.buf.s)
	}
}

// transform applies transform word x to t, pushing the results on s.buf.
func (s *solver) transform(x uint64, t tok) {
	path := domain.PathID(x >> 32)
	flag := x&(1<<8) != 0
	switch int(x & 0xff) {
	case xID:
		s.buf.push(s.a, s.intern(t))
	case xSel:
		if u, ok := s.sel(t, path); ok {
			s.buf.push(s.a, s.intern(u))
		}
	case xPre:
		s.buf.push(s.a, s.intern(s.pre(t, path, flag)))
	case xOff:
		u := asPointer(s.off(t, s.ps.Steps(path))) // the result is a pointer
		if flag && (u.kind == tObj || u.kind == tIn) {
			u.ge = true // the path was cut: anywhere under it
		}
		s.buf.push(s.a, s.intern(u))
	case xStep:
		if u, ok := s.sel(t, path); ok {
			s.buf.push(s.a, s.intern(u))
		}
		if t.kind == tObj && t.tag == domain.Root && !t.tagGE {
			s.buf.push(s.a, s.intern(s.off(asPointer(t), s.ps.Steps(path))))
		}
		if t.kind == tObj && t.tagGE {
			// Somewhere in the value: it may be the pointer the step
			// offsets.
			s.buf.push(s.a, s.intern(geify(s.off(t, s.ps.Steps(path)))))
		}
	case xGe:
		s.buf.push(s.a, s.intern(geify(t)))
	case xRetag:
		t.tag, t.tagGE = domain.Root, true
		s.buf.push(s.a, s.intern(t))
	case xPreLoad:
		u := s.pre(t, path, flag)
		s.buf.push(s.a, s.intern(u))
	case xSelLoad:
		if u, ok := s.sel(t, path); ok {
			if flag {
				u.tag, u.tagGE = domain.Root, true
			}
			s.buf.push(s.a, s.intern(u))
		}
	case xSelGe:
		if u, ok := s.sel(t, path); ok {
			s.buf.push(s.a, s.intern(geify(u)))
		}
	}
}

// run propagates until nothing changes.
func (s *solver) run() {
	for len(s.work.s) > 0 {
		// FIFO through a moving head.
		for s.head < len(s.work.s) {
			n := uint32(s.work.s[s.head])
			s.head++
			s.inWork.s[n] = 0
			if s.find(n) != n {
				continue
			}
			s.process(n)
		}
		s.work.s = s.work.s[:0]
		s.head = 0
	}
}

func (s *solver) process(n uint32) {
	pts := s.pts(n)
	d := pts[s.done.s[n]:]
	if len(d) == 0 {
		return
	}
	s.done.s[n] = uint64(len(pts))
	for c := s.nodeCons.s[n]; c != 0; c = s.cons.s[(c-1)*consWords+3] {
		for _, id := range d {
			s.fire(n, c-1, id)
		}
		if s.find(n) != n {
			return // merged while firing; the merged node is queued again
		}
	}
	for e := s.nodeEdges.s[n]; e != 0; e = s.edges.s[(e-1)*edgeWords+3] {
		s.sendAlong(e-1, d)
		if s.find(n) != n {
			return
		}
		s.lazyCycle(n, e-1)
	}
}

// lazyCycle looks for a copy cycle through identity edge e of n when its two
// ends hold the same set (Hardekopf and Lin's lazy cycle detection).
func (s *solver) lazyCycle(n uint32, e uint64) {
	w := s.edges.s[e*edgeWords : e*edgeWords+edgeWords]
	if w[1] != xw(xID, 0, false) || w[2] != 0 || w[4]&1 != 0 {
		return
	}
	m := s.find(uint32(w[0]))
	if s.noCollapse || m == n || s.nodeKind(n) != nkReg || s.nodeKind(m) != nkReg || !s.sameSet(n, m) {
		return
	}
	w[4] |= 1
	s.collapseFrom(m)
}

// sameSet reports whether nodes n and m hold the same tokens.
func (s *solver) sameSet(n, m uint32) bool {
	if len(s.lists[n].s) != len(s.lists[m].s) {
		return false
	}
	for _, id := range s.lists[n].s {
		if !s.has(m, id) {
			return false
		}
	}
	return true
}

// collapseFrom runs Tarjan's algorithm over the identity edges between
// collapsible nodes reachable from root, and merges each cycle it finds.
func (s *solver) collapseFrom(root uint32) {
	s.tjIndex.s = s.tjIndex.s[:0]
	s.tjLow.s = s.tjLow.s[:0]
	s.tjStack.s = s.tjStack.s[:0]
	s.tjSeen.n = 0
	clear(s.tjSeen.slots)
	s.tjCounter = 0
	s.tarjan(root)
}

func (s *solver) tjNum(n uint32) (int, bool) {
	v, ok := s.tjSeen.get(uint64(n), 0)
	return int(v), ok
}

func (s *solver) tarjan(n uint32) uint64 {
	idx := s.tjCounter
	s.tjCounter++
	s.tjSeen.put(s.a, uint64(n), 0, uint64(len(s.tjIndex.s)))
	slot := len(s.tjIndex.s)
	s.tjIndex.push(s.a, idx)
	s.tjLow.push(s.a, idx|tjOnStack)
	s.tjStack.push(s.a, uint64(n))
	for e := s.nodeEdges.s[n]; e != 0; e = s.edges.s[(e-1)*edgeWords+3] {
		w := s.edges.s[(e-1)*edgeWords : (e-1)*edgeWords+edgeWords]
		if w[1] != xw(xID, 0, false) || w[2] != 0 {
			continue
		}
		m := s.find(uint32(w[0]))
		if s.nodeKind(m) != nkReg || m == n {
			continue
		}
		if ms, ok := s.tjNum(m); !ok {
			s.lower(slot, s.tarjan(m))
		} else if s.tjLow.s[ms]&tjOnStack != 0 {
			s.lower(slot, s.tjIndex.s[ms])
		}
	}
	if s.tjLow.s[slot]&^tjOnStack == s.tjIndex.s[slot] {
		// Pop the component and merge it into n.
		k := len(s.tjStack.s) - 1
		for ; ; k-- {
			m := uint32(s.tjStack.s[k])
			ms, _ := s.tjNum(m)
			s.tjLow.s[ms] &^= tjOnStack
			if m == n {
				break
			}
		}
		for _, m := range s.tjStack.s[k+1:] {
			s.merge(n, uint32(m))
		}
		s.tjStack.s = s.tjStack.s[:k]
	}
	return s.tjLow.s[slot] &^ tjOnStack
}

// tjOnStack marks, in tjLow, a node on Tarjan's stack.
const tjOnStack = 1 << 63

// lower lowers the low link of slot to low, keeping its on-stack mark.
func (s *solver) lower(slot int, low uint64) {
	if low < s.tjLow.s[slot]&^tjOnStack {
		s.tjLow.s[slot] = low | s.tjLow.s[slot]&tjOnStack
	}
}

// merge folds node b into node a: sets, edges and constraints. The merged
// node is propagated again from scratch (constraints are idempotent).
func (s *solver) merge(a, b uint32) {
	a, b = s.find(a), s.find(b)
	if a == b {
		return
	}
	s.nodeRep.s[b] = uint64(a)
	for _, id := range s.lists[b].s {
		s.addID(a, id)
	}
	s.done.s[a] = 0
	s.nodeEdges.s[a] = s.appendList(s.nodeEdges.s[a], s.nodeEdges.s[b], s.edges.s, edgeWords)
	s.nodeCons.s[a] = s.appendList(s.nodeCons.s[a], s.nodeCons.s[b], s.cons.s, consWords)
	s.merged++
	s.enqueue(a)
}

// appendList appends list b to list a (heads index+1; next at word 3).
func (s *solver) appendList(a, b uint64, words []uint64, size int) uint64 {
	if a == 0 {
		return b
	}
	if b == 0 {
		return a
	}
	e := a
	for {
		next := words[(e-1)*uint64(size)+3]
		if next == 0 {
			words[(e-1)*uint64(size)+3] = b
			return a
		}
		e = next
	}
}
