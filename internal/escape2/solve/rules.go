package solve

// The rules of the solver, one per EIR instruction kind (plan
// docs/plans/2026-10-20-escape-redesign.md, section 5, "one rule per EIR
// instruction kind, written down as a table for reviewers"). The optimised
// solver (solve.go) and the naive reference solver (internal/escape2/refsolve)
// both implement exactly these rules; a difference between their results is a
// bug in one of them. Section numbers are of the language specification.
//
// # Abstract values
//
// A token is one thing a pointer may point at:
//
//	Obj(o, p, pg)   a pointer into frame object o at field path p (fields and
//	                element steps only, at most domain.MaxFieldNest of them);
//	                pg: or anywhere under p (set when a path is cut).
//	In(i, P, kg)    the pointers found by following access path P from input
//	                i (fields, elements and loads, cut by domain.Paths.Extend);
//	                kg: or any extension of P. This is exactly the key
//	                domain.MakeKey(i, P, {GE: kg}).
//	Heap, CHeap     heap or static memory; CHeap a counted reference
//	                (eir.HeapCounted). Never a frame.
//
// Reg(s) names the region this function provides for the closures the call
// at site s returns (section 11.5). It is a frame object of this function
// beyond Objects (index len(Objects)+s), with lifetime LifeIter of the
// innermost loop of the call's block, or LifeFrame outside loops; its tokens
// are Obj(Reg(s), p, pg). What the callee put in it is not in the summary, so
// its place <Reg(s), Root, true> starts with what the callee may have stored
// there: <t, Root, true> for every t in reach(β(i)) over the call's inputs,
// Obj(Reg(s), Root, true) itself, and Heap.
//
// An object o with Objects[o].Life == LifeHeap is Heap: OpAddr of it makes
// the token Heap. An object with Life == LifeCaller (a region the caller
// provides, eir.ObjRegion) is the caller's memory: it is never reported as a
// frame of this function, and returning a pointer to it sets Region facts.
//
// A value (what a register or a place holds) is a set of tagged tokens
// <t, T, g>: token t is held at field path T of the value (domain.Root for
// the value as a whole), or anywhere under T when g is set. A pointer value
// is <t, Root, false>. A struct value has its pointers at their fields.
//
// For an In token, the tag Root (not g) means the value is the whole value
// found at P, whatever its type: a field step on it extends P.
//
// A place is <o, p, pg> for a frame object; pt(place) is the value it holds.
// Places are flow-insensitive: a place holds everything ever stored in it.
// Registers are SSA, so a register's value is flow-insensitive too and
// equals its one definition; flow sensitivity for unaddressed locals comes
// from SSA (section 18.3, "How precise the frame check is").
//
// # Operations on values
//
//	off(t, Q)    pointer t offset by field path Q:
//	             Obj(o,p,pg) -> Obj(o, p+Q, pg or cut); In(i,P,kg) ->
//	             In(i, P+Q, kg or cut) (unchanged when kg); Heap -> Heap.
//	sel(v, Q)    field Q of the struct value v, for each <t,T,g> in v:
//	             Q prefix of T          -> <t, T-Q, g>
//	             g and T prefix of Q    -> <t, Root, true>
//	             t = In(i,P,kg), T=Root, not g -> <off(t,Q), Root, false>
//	             t = Heap or CHeap      -> <t, Root, false>
//	             otherwise              -> nothing (another field)
//	pre(v, Q)    v placed at field Q of a struct value: <t, Q+T, g or cut>.
//	step(v, s)   one step s of a key path applied to v (a callee's step,
//	             which was a field offset of a pointer or a field selection
//	             of a value; tags tell which): for a field or element step,
//	             sel(v, s) plus off(t, s) for every <t,Root,_> in v that is
//	             not an In token (In tokens are covered by sel); for a load,
//	             load(v).
//	load(v)      for each token t in v (whatever its tag):
//	             Obj(o,p,pg): for each place q = <o,r,rg> of o:
//	                 r = p+R        -> pre(pt(q), R), g set if rg or pg
//	                 p = r+R        -> sel(pt(q), R), g set if pg
//	                 (any other r: not the same memory)
//	             In(i,P,kg): <In(i, P+load, kg or cut), Root, false>
//	             Heap, CHeap: <Heap, Root, false>
//	             A load of a heap-only field (Env.HeapOnly, invariant I6) is
//	             <Heap, Root, false> instead, whatever t is.
//	reach(v)     the closure of v under load and every field step, every
//	             tag made <_, Root, true> and every In token made kg.
//	val(k, β)    the value key k = (i, P, kg) names at a call with input
//	             binding β: step(...step(β(i), P[0])..., P[n-1]), then reach
//	             if kg.
//
// # The store check (one check for E3002, E3003, E3006, E3009, E3101)
//
// store(D, v, why) stores value v through every pointer token d of D:
//
//	d = Obj(o,p,pg)   pt(<o,p,pg>) gets v, and for each Obj(o2,..) token in
//	                  v: o2 must live at least as long as o (outlives
//	                  below), else a finding:
//	                    o2 = Reg(s), LifeIter   E3101
//	                    o2 LifeIter             E3006
//	                    o2 LifeStmt             E3009
//	                    otherwise               E3002
//	d = In(i,P,kg)    for each <u,T,g> in v, with D' = In(i, P+T, kg or g or
//	                  cut) (the place of the field T of what d points to):
//	                    u = Obj (frame)     finding E3002 (E3003 when why is a call)
//	                    u = In(j,Q,kq)      summary Store pair (u's key, D''s key)
//	                    u = Heap, CHeap     summary HeapStore D''s key
//	d = Heap, CHeap   for each token u in v:
//	                    u frame             finding E3002 (E3003 when why is a call)
//	                    u = In              summary Esc u's key
//
// outlives(o2, o): o2 is o; or Life(o2) > Life(o) in the order LifeStmt <
// LifeIter < LifeBlock < LifeFrame < LifeCaller < LifeHeap, except that for
// Life(o2) = Life(o) = LifeIter, o's loop must be o2's loop or nested in it;
// or Life(o2) = Life(o) and neither is LifeIter. Reg(s) has the lifetime of
// its call (above), with one exception: Reg(s) of a call outside any loop
// may be stored in a LifeCaller object (section 11.5: a function that
// returns the closures of a call passes part of its own region down for
// them). A token is a frame token when it is Obj(o, ...) of an
// object o whose Life is not LifeHeap (LifeCaller objects and regions
// included).
//
// sink(v, code) keeps v where it outlives the call: for each token u in v, a
// frame token is a finding with code; u = In is summary Esc of u's key; Heap
// is nothing.
//
// # One rule per instruction
//
//	OpAddr       Dst ⊇ <Obj(Obj, Path, cut), Root, false> (Heap if LifeHeap)
//	OpHeap       Dst ⊇ <Heap, Root, false>; <CHeap, ...> for HeapCounted
//	OpInput      Dst ⊇ <In(Index, Root, false), Root, false>
//	OpLoad       Dst ⊇ load(X), or <Heap> when the location read is a
//	             heap-only field (the field class comes from X's definition:
//	             OpAddr by Objects[o].Type along Path, OpField by its Type,
//	             the type of what its X points to, along Path)
//	OpStore      store(X, Y, own)
//	OpField      Dst ⊇ off(t, Path) for each token t of X
//	OpElem       Dst ⊇ off(t, [ElemStep]) for each token t of X
//	OpSelect     Dst ⊇ sel(X, Path)
//	OpCompose    Dst ⊇ pre(Args[i], Fields[i]) for each i
//	OpCopy       Dst ⊇ X
//	OpPhi        Dst ⊇ Args[k] for each k; and the back-edge check below
//	OpConv       Obj = NoObj: Dst ⊇ X. Otherwise store(Obj(Obj, Root), X,
//	             own) and Dst ⊇ <Obj(Obj, Root), Root, false>.
//	OpClosure    store(Obj(Obj, [k]), Args[k], own) for each k;
//	             Dst ⊇ <Obj(Obj, Root), Root, false>
//	OpMethodVal  store(Obj(Obj, [0]), X, own); Dst ⊇ <Obj(Obj, Root), Root, false>
//	OpAssert     Dst ⊇ X (stage 2 keeps no dynamic types; a builder that
//	             unboxes a non-direct value emits the OpLoad after it)
//	OpSink       SinkReturn: for each <u,T,g> in X:
//	                 u = Obj frame, LifeFrame or shorter  finding E3001
//	                 u = Obj LifeCaller (a region)        Region.Builds,
//	                                                      OuterResults += Index,
//	                                                      and for every
//	                                                      Obj(Reg(s),...) in
//	                                                      its reach, the check
//	                                                      below
//	                 u = Obj(Reg(s),...)                  Region.OuterResults
//	                                                      += Index, and every
//	                                                      frame token in
//	                                                      reach(pt(Reg(s)))
//	                                                      other than a
//	                                                      LifeCaller object or
//	                                                      region is a finding
//	                                                      E3001 (the region
//	                                                      would outlive it)
//	                 u = In                               Res[Index].From (u's
//	                                                      key, T); with g the
//	                                                      key is GE and the
//	                                                      field is T cut
//	                 u = Heap / CHeap                     Res[Index].Heap /
//	                                                      CountedHeap T
//	             SinkGlobal: sink(X, E3002); SinkSend: sink(X, E3005);
//	             SinkTask: sink(X, E3004); SinkExternal: sink(X, E3003)
//	OpDrop       for an object o of LifeStmt: every register live after the
//	             drop whose value holds Obj(o, ...) is a finding E3009 at the
//	             drop (a place that holds it is found by the store check)
//	OpCall       for each candidate c (CallStatic: Callee; CallIface,
//	             CallTypeParam, CallFunc: Env.Candidates, all of them, by
//	             union; CallContract: Env.Contract), with s its summary
//	             (Env.Summary; nil is the bottom summary, except for a
//	             bodyless function, whose default keeps every input:
//	             Esc (i, Root, GE) and Res (i, Root, GE) for every input i,
//	             and Heap in every result) and β its input binding:
//	               Esc k              sink(val(k, β), E3003)
//	               Store (a, b)       store(val(b, β), val(a, β), call)
//	               HeapStore k        store(val(k, β), <Heap>, call)
//	               Res[r].From (k,F)  Results[r] ⊇ pre(val(k, β), F)
//	               Res[r].Heap F      Results[r] ⊇ <Heap, F, false>
//	               Res[r].CountedHeap Results[r] ⊇ <CHeap, F, false>
//	               Region.OuterResults r  Results[r] ⊇ <Obj(Reg(site), Root, true), Root, true>
//
// Input binding β at a call: CallStatic and CallContract bind input i to
// Args[i] (receiver first). CallIface and CallTypeParam bind input 0 to
// Target (the interface value's data word: a box pointer, or the direct
// value) and input i+1 to Args[i]; when Target is 0 the receiver is Args[0]
// and inputs bind as for a static call. CallFunc binds input i to Args[i]
// and, when the candidate has one input more than Args (a literal's closure
// record, a method value's record), the last input to Target. An input with
// no argument binds to the empty value.
//
// Back-edge check (section 18.4, "checked where the next iteration
// starts"): for an OpPhi in the header of loop L, for each Args[k] whose
// predecessor Preds[k] is a block of L, every Obj(o2, ...) in it with o2 of
// LifeIter whose loop is L or nested in L (a region Reg(s) of a call in L
// included), or of LifeStmt, is a finding E3006 (E3101 for a region, E3009
// for LifeStmt) at the phi.
//
// Every instruction the rules do not name, and every operand register 0, is
// the empty value. A finding is reported once per (code, object, position).
//
// # The summary (plan invariant I4)
//
// The summary collects, over the fixed point: Esc keys, Store pairs and
// HeapStore keys from the store check and sinks, Res from SinkReturn, and
// Region facts; then summary.Widen applies the caps. Store pairs with both
// keys of one input are kept (I4(b)).
