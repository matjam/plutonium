package solve

import (
	"encoding/binary"
	"fmt"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// Deferred calls (decision D49; rules.go, "Deferred calls"). A dcall is a
// call of a function value this solve resolves per token of the value: a
// CallFunc of the function's own code whose results hold no pointers, or a
// Deferred entry of a summary it applied. A closure record the function
// made applies that closure's summary; an In token of the function defers
// the call to its own callers (Summary.Deferred), as long as the arguments
// hold nothing but its input pointers and heap memory; anything else, or an
// argument holding a frame token, applies the per-signature join of the
// origin call's candidates (the fallback), once.

type dcall struct {
	in       *eir.Instr // the call of this function it belongs to
	args     []uint32   // the node of each argument, by the callee's input index (the record excluded)
	target   uint32     // the node of the function value
	origin   eir.FuncID // the call it stands for, for the fallback
	site     eir.SiteID
	fellBack bool         // the candidates' join applied
	argsBad  bool         // an argument holds what no caller's key names
	kept     bool         // the bodyless default applied
	keys     []domain.Key // the In tokens of the function value: the Deferred entries it makes
}

// ownDeferrable reports whether call in of the function's own code is a
// dcall: a call of a function value whose results hold no pointers.
func ownDeferrable(in *eir.Instr) bool {
	c := in.Call
	if c.Kind != eir.CallFunc || c.Target == 0 {
		return false
	}
	for _, r := range c.Results {
		if r != 0 {
			return false
		}
	}
	return true
}

// newDcall makes the dcall of call in for the origin call (origin, site),
// or joins target and args into the one it has: one dcall per (call,
// origin call), with a node of its own for its function value and each
// argument. That keeps them finite: resolving a deferred call applies
// summaries with deferred calls of their own, whose function values are
// found through ever longer paths, and those join into the dcall they
// stand for.
func (s *solver) newDcall(in *eir.Instr, target uint32, args []uint32, origin eir.FuncID, site eir.SiteID) {
	k := fmt.Sprintf("%p %d %d", in, origin, site)
	i, ok := s.dcallIdx[k]
	if !ok {
		i = len(s.dcalls)
		s.dcallIdx[k] = i
		dc := &dcall{in: in, target: s.newNode(nkPinned), origin: origin, site: site}
		s.dcalls = append(s.dcalls, dc)
		s.addCons(dc.target, cDCall, uint64(i), 0)
	}
	dc := s.dcalls[i]
	for len(dc.args) < len(args) {
		n := s.newNode(nkPinned)
		dc.args = append(dc.args, n)
		s.addCons(n, cDArg, uint64(i), 0)
	}
	if target != 0 {
		s.addEdge(target, dc.target, xw(xID, 0, false), 0)
	}
	for j, a := range args {
		if a != 0 {
			s.addEdge(a, dc.args[j], xw(xID, 0, false), 0)
		}
	}
}

// ownDcall makes the dcall of call in of the function's own code.
func (s *solver) ownDcall(in *eir.Instr) {
	c := in.Call
	args := make([]uint32, len(c.Args))
	for i, a := range c.Args {
		args[i] = s.canonBase(a)
	}
	s.newDcall(in, s.canonBase(c.Target), args, s.f.ID, c.Site)
}

// fireDcall handles token t of the function value of dcall i.
func (s *solver) fireDcall(i int, t tok) {
	dc := s.dcalls[i]
	switch t.kind {
	case tObj:
		if fn, ok := s.closureOf[t.base]; ok && !t.ge && t.path == domain.Root {
			s.applyClosure(i, fn, t)
			return
		}
	case tIn:
		if s.f.External {
			// No caller binds the function: the origin call's candidates.
			s.fallback(i)
			return
		}
		k := key(asPointer(t))
		for _, x := range dc.keys {
			if x == k {
				return
			}
		}
		dc.keys = append(dc.keys, k)
		if dc.argsBad {
			// An argument a caller's key does not name: what the call
			// does with its arguments is resolved here (fallback), and
			// what it does with the function value alone at the callers.
			s.fallback(i)
		}
		return
	}
	s.fallback(i)
}

// fireDarg handles token t of an argument of dcall i. A frame token is
// nothing a caller's key names: what the call does with its arguments is
// resolved here (fallback), and its Deferred entries have no arguments. A
// pointer held in a field of a value is passed as a GE key (argKey).
func (s *solver) fireDarg(i int, t tok) {
	dc := s.dcalls[i]
	if dc.argsBad || t.kind != tObj {
		return
	}
	dc.argsBad = true
	if len(dc.keys) > 0 {
		s.fallback(i)
	}
}

// keep applies, once, the default of a bodyless callee at dcall i, with its
// arguments and its function value as the inputs: what cannot be resolved
// or deferred is kept, and no summary is read (the scheduler orders none
// for it).
func (s *solver) keep(i int) {
	dc := s.dcalls[i]
	if dc.kept {
		return
	}
	dc.kept = true
	bind := make([]uint64, len(dc.args)+1)
	for j, a := range dc.args {
		bind[j] = uint64(a)
	}
	bind[len(dc.args)] = uint64(dc.target)
	s.applyDefault(dc.in, bind, eir.NoFunc)
}

// applyClosure applies the summary of closure fn, whose record t this
// function made, at dcall i.
func (s *solver) applyClosure(i int, fn eir.FuncID, t tok) {
	dc := s.dcalls[i]
	k := [2]uint64{uint64(i), s.intern(t)}
	if s.dcallDone[k] {
		return
	}
	s.dcallDone[k] = true
	if !s.env.CanRead(s.f, fn) {
		s.fallback(i) // the scheduler did not order this literal before f
		return
	}
	f := s.prog.Funcs[fn]
	bind := s.a.Alloc(len(f.Inputs))
	for j := range bind {
		if j < len(dc.args) {
			bind[j] = uint64(dc.args[j])
		}
	}
	if len(f.Inputs) == len(dc.args)+1 {
		bind[len(f.Inputs)-1] = uint64(s.tokNode(t))
	}
	s.applyBound(dc.in, fn, bind, -1)
}

// fallback applies the per-signature join of the origin call's candidates
// at dcall i, once.
func (s *solver) fallback(i int) {
	dc := s.dcalls[i]
	if dc.fellBack {
		return
	}
	dc.fellBack = true
	// The origin call by its site: dc.in is a call of this function's
	// that applied a summary with the Deferred entry, which is the origin
	// call only for a call of its own (a recursive callee's entry may name
	// this function as its origin through a static call).
	origin := s.f
	if dc.origin != s.f.ID {
		origin = s.prog.Funcs[dc.origin]
	}
	call := siteCall(origin, dc.site)
	if call == nil {
		panic("internal compiler error: escape2/solve: a deferred call's origin has no such call site")
	}
	cands := s.env.Candidates(origin, call)
	for _, cand := range cands {
		if cand != eir.NoFunc && !s.env.CanRead(s.f, cand) {
			s.keep(i)
			return
		}
	}
	for _, cand := range cands {
		n := len(dc.args)
		if int(cand) < len(s.prog.Funcs) {
			n = len(s.prog.Funcs[cand].Inputs)
		} else if sum := s.env.Summary(cand); sum != nil {
			n = sum.Inputs
		}
		bind := s.a.Alloc(n)
		for j := range bind {
			if j < len(dc.args) {
				bind[j] = uint64(dc.args[j])
			}
		}
		if n != len(dc.args)+1 {
			s.applyBound(dc.in, cand, bind, -1)
			continue
		}
		// The record input is bound to the rest of the value: the closures
		// it holds that this function made resolve themselves
		// (applyClosure). Its In tokens are Deferred entries
		// (deferredOut), whose callers apply the entries that name the
		// record alone: here, only the entries that name an argument.
		rest := s.restNode(dc.target)
		if s.f.External {
			bind[n-1] = uint64(rest)
			s.applyBound(dc.in, cand, bind, -1)
			continue
		}
		bind[n-1] = uint64(s.splitNode(rest, false))
		s.applyBound(dc.in, cand, bind, -1)
		bindIn := s.a.Alloc(n)
		copy(bindIn, bind)
		bindIn[n-1] = uint64(s.splitNode(rest, true))
		s.applyBound(dc.in, cand, bindIn, n-1)
	}
}

// splitNode returns the node of the In tokens of node n (in), or of its
// other tokens.
func (s *solver) splitNode(n uint32, in bool) uint32 {
	kind := uint64(dSplitOther)
	x := xNoIn
	if in {
		kind, x = dSplitIn, xOnlyIn
	}
	if r, ok := s.derived.get(kind|uint64(n)<<20, 0); ok {
		return uint32(r)
	}
	r := s.newNode(nkPinned)
	s.derived.put(s.a, kind|uint64(n)<<20, 0, uint64(r))
	s.addEdge(n, r, xw(x, 0, false), 0)
	return r
}

// siteCall is the call at site of function f.
func siteCall(f *eir.Func, site eir.SiteID) *eir.Call {
	for _, b := range f.Blocks {
		for i := range b.Instrs {
			if c := b.Instrs[i].Call; c != nil && c.Site == site {
				return c
			}
		}
	}
	return nil
}

// applyBound applies the summary of cand with binding bind at call in (or
// the default of a bodyless function), less the entries that name input
// skip alone (applySkip), and records it for State.Resume.
func (s *solver) applyBound(in *eir.Instr, cand eir.FuncID, bind []uint64, skip int) {
	bodyless := cand == eir.NoFunc
	var sum *summary.Summary
	if !bodyless {
		if int(cand) < len(s.prog.Funcs) {
			bodyless = s.prog.Funcs[cand].Bodyless
		}
		sum = s.env.Summary(cand)
	}
	if !bodyless {
		s.applied = append(s.applied, application{in: in, cand: cand, bind: bind, sum: sum, skip: skip})
	}
	if sum == nil {
		if bodyless {
			s.applyDefault(in, bind, cand)
		}
		return
	}
	s.applySkip(in, bind, sum, cand, skip)
}

// applyDeferred makes a dcall for each Deferred entry of sum, applied at
// call in with binding bind, less those that name input skip alone
// (applySkip).
func (s *solver) applyDeferred(in *eir.Instr, bind []uint64, sum *summary.Summary, skip int) {
	for _, d := range sum.Deferred {
		if skip >= 0 && deferredOnly(d, skip) {
			continue
		}
		fi := d.Fn.Input()
		if fi >= len(bind) || bind[fi] == 0 {
			continue
		}
		target := s.valNode(uint32(bind[fi]), d.Fn.Path(), d.Fn.GE())
		args := make([]uint32, len(d.Args))
		for j, a := range d.Args {
			args[j] = s.argNode(a, bind)
		}
		s.newDcall(in, target, args, d.Origin, d.Site)
	}
}

// argKey is the key a Deferred entry's argument gives In token t: its own,
// and for a token in a field of the value (a tag other than <Root,
// false>), GE: what the key leads to, anywhere in the value, covers the
// token wherever it sits.
func argKey(t tok) domain.Key {
	k := key(asPointer(t))
	if t.tag != domain.Root || t.tagGE {
		k = k.WithPath(k.Path(), true)
	}
	return k
}

// deferredOnly reports whether Deferred entry d names input i alone.
func deferredOnly(d summary.Deferred, i int) bool {
	if d.Fn.Input() != i {
		return false
	}
	for _, a := range d.Args {
		for _, k := range a.Keys {
			if k.Input() != i {
				return false
			}
		}
	}
	return true
}

// argNode is the node of what a deferred call's argument holds at a call
// with binding bind: the values of its keys, and heap memory.
func (s *solver) argNode(a summary.DeferredArg, bind []uint64) uint32 {
	// The key: the (node, key) pairs, and a last byte for Heap. The lookup
	// converts the reused buffer without allocating; only a new key is
	// copied.
	b := s.argBuf[:0]
	for _, k := range a.Keys {
		i := k.Input()
		if i < len(bind) && bind[i] != 0 {
			b = binary.LittleEndian.AppendUint64(b, bind[i])
			b = binary.LittleEndian.AppendUint64(b, uint64(k))
		}
	}
	if a.Heap {
		b = append(b, 'h')
	}
	s.argBuf = b
	if n, ok := s.argNodes[string(b)]; ok {
		return n
	}
	n := s.newNode(nkPinned)
	s.argNodes[string(b)] = n
	for _, k := range a.Keys {
		i := k.Input()
		if i < len(bind) && bind[i] != 0 {
			s.addEdge(s.valNode(uint32(bind[i]), k.Path(), k.GE()), n, xw(xID, 0, false), 0)
		}
	}
	if a.Heap {
		s.addTok(n, heapTok)
	}
	return n
}

// deferredOut is the Deferred entries of the summary: for each dcall that
// did not fall back, one per In token of its function value, with what its
// arguments hold.
func (s *solver) deferredOut() []summary.Deferred {
	var out []summary.Deferred
	for _, dc := range s.dcalls {
		if len(dc.keys) == 0 {
			continue
		}
		args := make([]summary.DeferredArg, len(dc.args))
		for j, a := range dc.args {
			if a == 0 || dc.argsBad {
				// With an argument no caller's key names, what the call
				// does with its arguments was resolved here: the callers
				// resolve it with no arguments (fallback).
				continue
			}
			for _, id := range s.pts(s.find(a)) {
				switch t := s.tokAt(id); t.kind {
				case tIn:
					args[j].Keys = append(args[j].Keys, argKey(t))
				case tHeap, tCHeap:
					args[j].Heap = true
				}
			}
		}
		for _, k := range dc.keys {
			out = append(out, summary.Deferred{Fn: k, Args: args, Origin: dc.origin, Site: dc.site})
		}
	}
	return out
}
