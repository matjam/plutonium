package lower

import (
	"compiler/compat"
	"compiler/internal/moves"
	"compiler/internal/ssa"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strconv"
)

// One exit path per frame (language spec sections 17.3 and 18.6).
//
// A call that ends with rt.ExitTask makes its caller leave through its
// return path: the deferred calls run, then the scopes open at the call
// release their slots, innermost first, then the frame slots, the
// parameters and the regions, and the function returns with the flag set
// again (leaveForGoexit). Emitted at every call that may exit, that path
// would grow with the number of owners open there, and a function with N
// owners and N such calls would have code quadratic in N.
//
// Instead each call that may exit records the releases its exit needs, in
// order, and jumps to a landing built once the whole body is lowered,
// when the frame slots are known too. A release is one slot destroyed with
// what the move checker knows of its variable there (a variable known to
// hold nothing has no release, one known to hold some parts releases those
// parts). The releases of every exit end alike: the frame's releases and
// the return. So the landings form a tree towards one return: a landing
// is one release followed by the landing of the rest, and two exits whose
// remaining releases are the same share the landing of the rest. An owner
// declared after k earlier ones adds one landing, which goes on to the
// landing the exit before it used: the code is linear in the number of
// owners and exits, and each exit still releases exactly what it did.
//
// The deferred calls run before any release. In a function with defer
// statements, every exit sets the number of the landing it goes to and
// jumps to one block that runs the deferred calls, whose armed bits say
// which ran, and then goes to that landing.

// An exitSite is a call that may end with rt.ExitTask, waiting for its
// landing.
type exitSite struct {
	b     *ssa.Block  // the exit, which ends in a jump to its landing
	acts  []exitAct   // the releases of the scopes open there, innermost first
	known moves.Known // what is known of the variables there
	entry *exitNode   // its first landing
}

// An exitAct is one release on the way out: the slot, destroyed whole or,
// if held is set, only the parts the move checker says may hold values.
type exitAct struct {
	slot    countedSlot
	known   bool
	held    moves.Held
	regions bool // the function's regions instead of a slot
	key     string
}

// An exitNode is a landing: act, then next. The return has no act.
type exitNode struct {
	id     int
	act    exitAct
	next   *exitNode
	b      *ssa.Block
	number int // its number among the exits' first landings, or -1
}

// exitThrough leaves the function for rt.ExitTask from the current block,
// with known what the move checker knows of the variables here.
func (s *fnState) exitThrough(known moves.Known) {
	// Releases on the way out collect exits in the function's slot, which
	// the code lowered after this point checks once it exists.
	s.implicitGoexitSlot()
	saved := s.leaving
	s.leaving = known
	var acts []exitAct
	for i := len(s.cleanups) - 1; i >= 0; i-- {
		acts = s.exitActs(acts, s.cleanups[i].slots)
	}
	s.leaving = saved
	end := s.newBlock(ssa.BlockInvalid)
	s.endPlain(end)
	s.seal(end)
	s.exits = compat.Append(s.exits, exitSite{b: end, acts: acts, known: known})
}

// exitActs appends the releases of slots, in the order releaseSlots
// releases them, with what s.leaving knows of their variables.
func (s *fnState) exitActs(acts []exitAct, slots []countedSlot) []exitAct {
	for _, scopes := range [2]bool{true, false} {
		for i := len(slots) - 1; i >= 0; i-- {
			c := slots[i]
			if c.first != scopes || c.tmp != nil && c.tmp.consumed {
				continue
			}
			held, known := s.leaving[c.v]
			if c.v != nil && known && !held.Box && len(held.Parts) == 0 {
				continue // it holds nothing (scope.go)
			}
			a := exitAct{slot: c, known: c.v != nil && known}
			if a.known {
				a.held = held
			}
			a.key = exitActKey(a)
			acts = compat.Append(acts, a)
		}
	}
	return acts
}

// exitActKey names what a release does: which slot, and which parts of it.
func exitActKey(a exitAct) string {
	key := compat.Concat("s", strconv.Itoa(a.slot.base.ID), "+", strconv.FormatInt(a.slot.off, 10))
	if !a.known {
		return compat.Concat(key, "w")
	}
	key = compat.Concat(key, "k")
	if a.held.Box {
		key = compat.Concat(key, "b")
	}
	for _, p := range a.held.Parts {
		key = compat.Concat(key, ",")
		for _, step := range p {
			key = compat.Concat(key, step, "/")
		}
	}
	return key
}

// finishExits builds the landings of the function's exits for rt.ExitTask
// and ends each exit with a jump to its own.
func (s *fnState) finishExits() {
	if len(s.exits) == 0 {
		return
	}
	pos := token.NoPos
	if s.f.End.IsValid() {
		pos = s.f.End - 1 // the closing brace: the landings belong to no one exit
	}
	oldPos := s.f.SetPos(pos)
	defer s.f.SetPos(oldPos)

	var nodes []*exitNode
	byKey := map[string]int{} // the ids of the landings made so far
	newNode := func(act exitAct, next *exitNode) *exitNode {
		n := pu.New(exitNode{id: len(nodes), act: act, next: next, b: s.newBlock(ssa.BlockInvalid), number: -1})
		nodes = compat.Append(nodes, n)
		return n
	}
	ret := newNode(exitAct{}, nil)
	var regions exitAct
	regions.regions = true
	regions.key = "regions"
	for i := range s.exits {
		x := &s.exits[i]
		acts := x.acts
		saved := s.leaving
		s.leaving = x.known
		acts = s.exitActs(acts, s.frameSlots)
		acts = s.exitActs(acts, s.ownedParams)
		s.leaving = saved
		if len(s.regionLocals) > 0 {
			acts = compat.Append(acts, regions)
		}
		// Each landing is made after the one it goes on to, so a landing's
		// predecessors have greater ids than it.
		n := ret
		for j := len(acts) - 1; j >= 0; j-- {
			key := compat.Concat(acts[j].key, ">", strconv.Itoa(n.id))
			if id, ok := byKey[key]; ok {
				n = nodes[id]
				continue
			}
			n = newNode(acts[j], n)
			byKey[key] = n.id
		}
		x.entry = n
	}

	gx := s.temp(types2.Typ[types2.Bool], "goexit")
	if s.hasDefers() {
		// The landings, numbered in the order the exits first use them.
		var entries []*exitNode
		for i := range s.exits {
			if e := s.exits[i].entry; e.number < 0 {
				e.number = len(entries)
				entries = compat.Append(entries, e)
			}
		}
		var which *ssa.Value
		if len(entries) > 1 {
			which = s.temp(types2.Typ[types2.Int], "exit.landing")
		}
		defers := s.newBlock(ssa.BlockInvalid)
		for i := range s.exits {
			x := s.exits[i]
			s.startBlock(x.b)
			if which != nil {
				s.store(ssa.TI64, which, s.constInt(ssa.TI64, int64(x.entry.number)))
			}
			s.endPlain(defers)
		}
		s.seal(defers)
		s.startBlock(defers)
		// The deferred calls' arguments may need temporaries, which the
		// exiting statement's entry held before; these are released once
		// the calls have run, before the landings.
		restore := s.collectCleanupGoexit()
		e := s.pushCleanup(nil, false)
		s.runDeferList(s.defers, s.deferBitsAddr(), s.deferRec, gx)
		s.popCleanup(e)
		restore()
		for k := 0; s.b != nil && k < len(entries)-1; k++ {
			hit := s.newValue(ssa.OpEq, ssa.TBool, s.load(ssa.TI64, which), s.constInt(ssa.TI64, int64(k)))
			other := s.newBlock(ssa.BlockInvalid)
			s.endIf(hit, entries[k].b, other)
			s.seal(other)
			s.startBlock(other)
		}
		s.endPlain(entries[len(entries)-1].b)
	} else {
		for i := range s.exits {
			x := s.exits[i]
			s.startBlock(x.b)
			s.endPlain(x.entry.b)
		}
	}

	for i := len(nodes) - 1; i >= 0; i-- {
		n := nodes[i]
		s.seal(n.b)
		if len(n.b.Preds) == 0 {
			continue // reached only from code that never returns
		}
		s.startBlock(n.b)
		if n == ret {
			// The deferred calls only ever set the flag, so it is set here,
			// once for every exit.
			s.store(ssa.TBool, gx, s.constBool(true))
			s.finishReturn(pendingReturn{b: n.b, pos: pos, results: s.results, gx: gx})
			continue
		}
		s.emitExitAct(n.act)
		s.endPlain(n.next.b)
	}
	s.exits = nil
}

// emitExitAct emits the release a, as releaseSome would.
func (s *fnState) emitExitAct(a exitAct) {
	defer s.collectCleanupGoexit()()
	e := s.pushCleanup(nil, false)
	defer s.popCleanup(e)
	if a.regions {
		s.dropLocalRegions()
		return
	}
	c := a.slot
	p := s.offPtr(c.base, c.off)
	from := len(s.callLog)
	restore := func() {}
	if c.pos.IsValid() {
		restore = s.at(c.pos) // errors name where the value was made
	}
	if a.known {
		s.destroyHeld(p, c.T, a.held)
	} else {
		s.destroyAt(p, c.T)
	}
	restore()
	if c.tmp != nil {
		s.noteRelease(c.tmp, from)
	}
}
