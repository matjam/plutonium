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
//	                domain.MakeKey(i, P, {GE: kg}). Two more forms come of
//	                an offset anywhere in an object (OpField with
//	                eir.AnyStep): In(i, P, fge), P's pointers at any offset
//	                (the key's FieldsGE), and In(i, P, deep), what those
//	                point at, transitively: P, any offsets, a load, then
//	                anything (the key's Deep), never P's pointers
//	                themselves. Each stands for its key; at most one of kg,
//	                fge and deep is set.
//	Heap, CHeap     heap or static memory; CHeap a counted reference
//	                (eir.HeapCounted). Never a frame.
//
// Reg(s) names the region this function provides for the closures the call
// at site s returns (section 11.5). It is a frame object of this function
// beyond Objects (index len(Objects)+s), with lifetime LifeIter of the
// innermost loop of the call's block, or LifeFrame outside loops; its tokens
// are Obj(Reg(s), p, pg). What the callee put in it is not in the summary, so
// its place <Reg(s), Root, true> starts with what the callee may have stored
// there: <t, Root, true> for every t in reach(v) over the values v of the
// call's Args and of its Target (when it has one, whatever the binding),
// Obj(Reg(s), Root, true) itself, and <Heap, Root, true>. In a package's
// initialization (Func.Key.Init) the region of a call is static memory
// (section 15.2): Results[r] gets <Heap, Root, true> instead, and the values
// of the call's Args and Target are kept there: sink(reach(v), E3002) at
// the call.
//
// A box view (eir.ObjBoxView: the heap object of a box held in a local
// variable, seen through it; section 18.7) is a frame object as a source,
// with the box variable's lifetime, but as a store destination it is Heap
// (store, the case d = Heap), and a load or reach through it reads
// <Heap, Root, false>. A loop's header variable (eir.LifeHeader) is LifeIter
// of its loop as a source; as a destination it lives across its loop's
// iterations: LifeIter of the enclosing loop, or LifeFrame outside loops
// (outlives).
//
// An object o with Objects[o].Life == LifeHeap is Heap: every rule that
// would make a token Obj(o, ...) of it (OpAddr, OpConv, OpClosure,
// OpMethodVal) makes Heap instead, and every rule that would store into a
// place of it stores into Heap (store, the case d = Heap). So does Obj =
// eir.NoObj in OpClosure and OpMethodVal: a func value with no record in the
// frame (a declared function, a method expression, a union variant) is
// static. An object with Life == LifeCaller (a region the caller
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
// GE subsumption: a value never needs an In token that a GE In token of the
// same value covers (same input, a path that is a prefix of the token's,
// and a tag that holds its tag: the same tag, GE or exact for an exact one,
// or a GE prefix of it), nor one a Deep In token covers (likewise, and the
// token's path has more loads than the Deep token's, or the token is Deep
// too: domain.Covers). A solver may drop such a token when it arrives, and
// may drop the tokens a GE token covers when it arrives; everything the
// covered token leads to (loads, offsets, fields, summary keys) the covering
// one leads to, so the summary after Widen and the findings are the same.
//
// A place is <o, p, pg> for a frame object; pt(place) is the value it holds.
// Places are flow-insensitive: a place holds everything ever stored in it.
// Registers are SSA, so a register's value is flow-insensitive too and
// equals its one definition; flow sensitivity for unaddressed locals comes
// from SSA (section 18.3, "How precise the frame check is").
//
// # Paths past the limits
//
// Extending a path P by a step past the limits of section 18.3 (the cut
// domain.Paths.Extend reports) gives the prefix of P through its last load
// (P itself if it has none) with GE: it covers P and the step, and every
// extension. (The domain's own cut goes back to its prefix through the
// second load, which may name the pointer P was reached through rather than
// what it leads to; keeping all of P's steps costs ten times the work on
// the corpus's largest function.) Every "cut" below is this, except a load
// past the limits (load, In): its pointers are those the load and its
// extensions reach, never those of P or of P offset (P through its last
// load is the start of the object P points into, and every step after it
// is an offset), so it is Deep on that prefix, which covers them and leaves
// out the pointers P names: section 18.3's "everything reachable" beyond the
// limit, not the pointer the walk was reached through (a closure record's
// pointer to a captured variable, under the records of the closures that
// call it).
//
// # Operations on values
//
//	off(t, Q)    pointer t offset by field path Q:
//	             Obj(o,p,pg) -> Obj(o, p+Q, pg or cut) (unchanged when pg);
//	             In(i,P,kg) -> In(i, P+Q, kg or cut) (unchanged when kg,
//	             fge or deep);
//	             Heap -> Heap; CHeap -> CHeap. Tags are kept.
//	sel(v, Q)    field Q of the struct value v, for each <t,T,g> in v, the
//	             first case that applies, in this order (Q = Root: v):
//	             Q prefix of T          -> <t, T-Q, g>
//	             g and T prefix of Q    -> <t, Root, true>
//	             t = In(i,P,kg), T=Root, not g -> <off(t,Q), Root, false>
//	             t = Heap/CHeap, T=Root, not g -> <t, Root, false> (a whole
//	                                       value of heap pointers)
//	             otherwise              -> nothing (another field)
//	pre(v, Q)    v placed at field Q of a struct value: <t, Q+T, g or cut>.
//	             Heap and CHeap tokens carry tags like any other, so that a
//	             heap pointer in one field is not taken for every field.
//	step(v, s)   one step s of a key path applied to v (a callee's step,
//	             which was a field offset of a pointer or a field selection
//	             of a value; tags tell which): for a field or element step,
//	             sel(v, s), plus for each Obj token <t,T,g> in v: with T = Root
//	             and not g, <off(t, s), Root, false>; with g,
//	             <off(t, s), Root, true> (In tokens are covered by sel); for a
//	             load, load(v).
//	load(v)      for each token t in v (whatever its tag):
//	             Obj(o,p,pg): for each place q = <o,r,rg> of o:
//	                 r = p+R        -> pre(pt(q), R), g set if rg or pg
//	                 p = r+R        -> sel(pt(q), R), g set if pg
//	                 (any other r: not the same memory)
//	             In(i,P,false): <In(i, P+load), Root, false>; past the
//	                 limits <In(i, P', deep), Root, false>, P' the prefix of
//	                 P through its last load ("Paths past the limits");
//	             In(i,P,true): <In(i,P,true), Root, false> (it covers P+load)
//	             In(i,P,fge): <In(i,P,deep), Root, false>
//	             In(i,P,deep): <In(i,P,deep), Root, false> (it covers P+load)
//	             Heap, CHeap: <Heap, Root, false>
//	             With pg, every token of every place with r = p+R or p = r+R
//	             gets the tag <_, Root, true> instead.
//	             A load of a heap-only field (Env.HeapOnly, invariant I6) is
//	             <Heap, Root, false> instead, whatever t is.
//	reach(v)     the closure of v under load, every token of the result
//	             geified: tag made <_, Root, true>, an In token made kg (a deep one stays deep); Obj
//	             tokens keep their path and pg. (Field steps add nothing: a load through an offset
//	             pointer reads a subset of what the load through the pointer
//	             reads, and kg covers the extensions of In tokens.)
//	val(k, β)    the value key k = (i, P, kg) names at a call with input
//	             binding β: step(...step(β(i), P[0])..., P[n-1]), then reach
//	             if kg; for a FieldsGE k, then any offset (OpField's
//	             eir.AnyStep); for a Deep k, then any offset, load, and
//	             reach.
//
// # The store check (one check for E3002, E3003, E3006, E3009, E3101)
//
// store(D, v, why) stores value v through every pointer token d of D,
// whatever d's own tag:
//
//	d = Obj(o,p,pg)   pt(<o,p,pg>) gets v, and for each Obj(o2,..) token in
//	                  v: o2 must live at least as long as o (outlives
//	                  below), else a finding:
//	                    o2 = Reg(s), LifeIter   E3101
//	                    o2 LifeIter             E3006
//	                    o2 LifeStmt             E3009
//	                    otherwise               E3002
//	d = In(i,P,kg)    for each <u,T,g> in v, with D' = off(d, T) (the place
//	                  of the field T of what d points to), FieldsGE when g
//	                  (that place or any under it by field and element steps,
//	                  not past a load; domain.Key.FieldsGE), a kg key staying
//	                  GE:
//	                    u = Obj (frame)     finding E3002 (E3003 when why is a call)
//	                    u = In(j,Q,kq)      summary Store pair (u's key, D''s key)
//	                    u = Heap, CHeap     summary HeapStore D''s key
//	d = Heap, CHeap   for each token u in v:
//	                    u frame             finding E3002 (E3003 when why is a call)
//	                    u = In              summary Esc u's key
//
// outlives(o2, o): o2 is o; or Life(o2) > Life(o) in the order LifeStmt <
// LifeIter < LifeFrame < LifeCaller < LifeHeap, LifeBlock counting as
// LifeFrame (section 18.2: a local variable lives in the frame, whatever its
// block; what ends with a block is an owner's drop, which is section 18.3's
// E3313 and not this check), except that for
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
// # Deferred calls (decision D49)
//
// A CallFunc of this function that Env.Deferred names (a call of a function
// value derived from one input, with arguments derived from inputs, heap or
// static memory and constants, and results that hold no pointers) is not
// applied by its candidates: it is a dcall. So is each Deferred entry
// (Fn, Args, Origin, Site) of a summary a call applies, with function value
// val(Fn, β) and arguments ∪ val(k, β) over Args[i].Keys, and <Heap, Root,
// false> when Args[i].Heap. A dcall is one per (call of this function,
// origin call); what every resolution at that call gives joins in it. For
// each token t of its function value:
//
//	t = Obj(o, Root, false), o the record of an OpClosure of this function
//	    with Callee C: apply C's summary, input i bound to argument i and the
//	    record input (when C has one input more) to <t>
//	t = In(i, P, kg): the summary's Deferred gets (key of t, the
//	    arguments' keys, Origin, Site): the keys of their In tokens (GE
//	    for a token whose tag is not <Root, false>: its value anywhere,
//	    and what it reaches), and Heap when they hold heap memory. When an
//	    argument holds an Obj token (a frame pointer, which no caller's
//	    key names), the entry has no arguments, and the fallback applies
//	    as well. When the function is External, the fallback instead, and
//	    no Deferred entry
//	otherwise: the fallback
// The
// fallback, once per dcall: the summaries of Env.Candidates(Origin's
// function, the call at Site), each with input i bound to argument i and
// the record input (when the candidate has one input more than the
// arguments) to the rest of the function value (below). Unless the
// function is External, the rest's In tokens are bound apart, with the
// candidate's entries that name the record input alone (Esc, HeapStore and
// Res keys of that input, Store pairs with both keys of it, Deferred
// entries whose function value and argument keys are all of it) left out:
// those are the callers', through the Deferred entries of those tokens,
// and a caller resolves them against the closure it actually passes,
// whose record layout is that closure's own. When the solve may not read a
// candidate (Env.CanRead), the default of a bodyless callee with the
// arguments and the function value as its inputs instead. A deferred call
// is thus resolved precisely at a caller, or as it would be without
// deferral.
//
// Applications are shared at a call: the dcalls of one call of this
// function that resolve to the same closure record apply its summary once,
// with the union of their arguments; those that fall back to the same
// candidate apply it once (once with the In tokens bound apart, as above),
// with the union of their arguments and of the rests of their function
// values. A function value reached through many origin calls would apply
// its literal once per origin call otherwise.
//
// The origin calls of the Deferred keys of one input, less the function's
// own calls (Origin the function), are at most
// MaxDeferredOrigins: ranked by (Origin, Site), those past it make no
// Deferred entry, and their dcalls resolve here, the In tokens of the rest
// bound with nothing left out (the In-full group). As more origin calls
// appear an origin call only moves down the ranks, so the choice settles,
// whatever the order of the solve.
//
// # Calls of function values
//
// An own record of a function value is a token Obj(o, Root, false), of any
// tag, where o is the record of an OpClosure of this function with Callee
// C, and Env.CanRead(f, C). It is C's closure: no other function runs when
// it is called. The rest of a function value is the value less its own
// records. A CallFunc with a Target that is not a dcall applies, for each
// own record t of Target, C's summary with input i bound to Args[i] and the
// record input to <t>; and, once the rest of Target holds a token, the
// summary of each candidate, its record input (when it has one input more
// than Args) bound to the rest. A candidate's summary thus never reads the
// record of another closure (a literal's keys name its own record's
// layout).
//
// # One rule per instruction
//
//	OpAddr       Dst ⊇ <Obj(Obj, Path, cut), Root, false> (Heap if LifeHeap)
//	OpHeap       Dst ⊇ <Heap, Root, false>; <CHeap, ...> for HeapCounted and
//	             HeapCountedCurrent (HeapCurrent is Heap)
//	OpInput      Dst ⊇ <In(Index, Root, false), Root, false>
//	OpLoad       Dst ⊇ load(X), or, when the location read is a heap-only
//	             field, Dst ⊇ <Heap, Root, false> whatever X holds (the field class comes from X's definition:
//	             OpAddr by Objects[o].Type along Path, OpField by its Type,
//	             the type of what its X points to, along Path)
//	OpStore      store(X, Y, own)
//	OpField      Dst ⊇ <off(t, Path), Root, false> for each token t of X
//	             (whatever t's tag: the result is a pointer)
//	             A Path holding eir.AnyStep (an offset that is not known:
//	             unsafe.Add, a reinterpreted pointer) makes, for each token:
//	             Obj(o, Root, true); for In(i, P, kg), In(i, P', fge)
//	             (In(i, P', true) when kg) with P' the prefix of P through
//	             its last load (the start of the object P points into);
//	             In(i, P, deep) stays (it stands for every offset of what
//	             it stands for); Heap stays Heap.
//	OpElem       Dst ⊇ <off(t, [ElemStep]), Root, false> likewise
//	OpSelect     Dst ⊇ sel(X, Path)
//	OpCompose    Dst ⊇ pre(Args[i], Fields[i]) for each i
//	OpCopy       Dst ⊇ X
//	OpPhi        Dst ⊇ Args[k] for each k; and the back-edge check below
//	OpConv       Obj = NoObj: Dst ⊇ X. Otherwise store(Obj(Obj, Root), X,
//	             own) and Dst ⊇ <Obj(Obj, Root), Root, false>. A box whose
//	             Type is a type parameter is made only for a non-direct
//	             instance: when Env.Instantiated knows every instance and each
//	             is direct (pointer-shaped, a scalar of at most a word, zero-sized, or an
//	             interface, converted by taking its data word),
//	             Dst ⊇ X as for NoObj (solve.BoxNeeded).
//	OpClosure    store(Obj(Obj, [k]), Args[k], own) for each k;
//	             Dst ⊇ <Obj(Obj, Root), Root, false>
//	OpMethodVal  store(Obj(Obj, [0]), X, own); Dst ⊇ <Obj(Obj, Root), Root, false>
//	OpAssert     Dst ⊇ X (stage 2 keeps no dynamic types; a builder that
//	             unboxes a non-direct value emits the OpLoad after it). To a
//	             concrete pointer-shaped Type the result is the data word of
//	             a direct value, never a box: Obj tokens of eir.ObjBox
//	             objects are left out. To an interface Type (one that
//	             mentions no type parameter), Obj tokens of eir.ObjBox
//	             objects whose Type (the dynamic type of the boxed value)
//	             mentions no type parameter and does not implement it are
//	             left out: the assertion fails on them (solve.BoxAsserts).
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
//	                                                      key, T, FieldGE g
//	                                                      unless the key is
//	                                                      GE, which is held
//	                                                      anywhere under T
//	                                                      already)
//	                 u = Heap / CHeap                     Res[Index].Heap /
//	                                                      CountedHeap T
//	             SinkGlobal: sink(X, E3002); SinkSend: sink(X, E3005);
//	             SinkTask: sink(X, E3004); SinkExternal: sink(X, E3003);
//	             SinkBoxCopy: each Obj(m, ...) in X with m an object of
//	             Func.ArenaMem (a local arena's memory) is a finding E3313
//	             (the box's copy is released after the arena is gone)
//	OpDrop       for an object o of LifeStmt: every register live after the
//	             drop whose value holds Obj(o, ...) is a finding E3009 at the
//	             drop (a place that holds it is found by the store check)
//	OpBorrow     Dst ⊇ X; and Dst ⊇ Y once X holds a CHeap token (a value
//	             taken out of a counted error: a view of its holder, eir/
//	             borrow.go)
//	OpArenaView  Dst ⊇ X, each <Obj(o, ...), T, g> taken as
//	             <Obj(m, Root, false), T, g>, m = Func.ArenaMem[o], or Obj
//	             for an o with none (or a region): what memory allocated
//	             from the allocator X is a view of (eir/arena.go); In,
//	             Heap and CHeap tokens stay
//	OpCall       for each candidate c (CallStatic: Callee; CallIface,
//	             CallTypeParam, CallFunc: Env.Candidates, all of them, by
//	             union; CallContract: Env.Contract), with s its summary.
//	             A CallFunc with a Target is as "Calls of function
//	             values" says.
//	             A callee eir.NoFunc is bodyless with as many inputs as
//	             Args and as many results as Results (the default below);
//	             otherwise the default's inputs and results are the
//	             function's (Inputs, Results), and the binding gives the
//	             values of its inputs.
//	             A candidate ID at or past len(Program.Funcs) is a virtual
//	             function the environment defines by its summary alone (a
//	             rule for dynamic calls that fixes one summary per signature,
//	             D47, plugs in this way): its inputs are s.Inputs
//	             (Env.Summary; nil is the bottom summary, except for a
//	             bodyless function, whose default keeps every input:
//	             Esc (i, Root, GE) and Res (i, Root, GE) for every input i,
//	             and Heap in every result) and β its input binding:
//	               Esc k              sink(val(k, β), E3003); at a call
//	                                  that starts a task (Implicit ImplTask),
//	                                  or whose entry's reason has Sink
//	                                  SinkTask (a callee that starts one),
//	                                  a frame token of a sync.WaitGroup
//	                                  object is E3004 (section 12.12), and
//	                                  the entry it makes keeps SinkTask
//	               Store (a, b)       store(val(b, β), val(a, β), call);
//	                                  b FieldsGE: val(b, β) as for b exact,
//	                                  and the stored value's tags made
//	                                  (Root, GE)
//	               HeapStore k        store(val(k, β), <Heap>, call); k
//	                                  FieldsGE: as for k exact, with
//	                                  <Heap, Root, true>
//	               Res[r].From (k,F,G) Results[r] ⊇ pre(val(k, β), F), tags
//	                                  GE when G (FieldGE)
//	               Res[r].Heap F      Results[r] ⊇ <Heap, F, true>
//	               Res[r].CountedHeap Results[r] ⊇ <CHeap, F, true>
//	                                  (true: the summary keeps no depth
//	                                  under F)
//	             A result field F with a load step is no field path: it is
//	             taken as Root with GE.
//	               Region.OuterResults r  Results[r] ⊇ <Obj(Reg(site), Root, true), Root, true>
//	               Region.Lits            the literals whose closures Reg(site) may
//	                                  hold: a deferred call whose function value
//	                                  holds Obj(Reg(site), ...) applies each, with
//	                                  its record bound to that token; eir.NoFunc
//	                                  among them, or a region no summary names
//	                                  the literals of, falls back (regionlits.go).
//	                                  A function's own Lits are its records that
//	                                  live in its caller's memory and the Lits
//	                                  of the regions it returns. An fcall applies
//	                                  them too, in place of its candidates, when
//	                                  all are named and readable.
//	               Region.Self            Reg(site)'s place holds the region itself
//	                                  only then: the callee stored a pointer to its
//	                                  caller's memory there (a closure capturing a
//	                                  closure), or returns a region with Self.
//
// Input binding β at a call: CallStatic and CallContract bind input i to
// Args[i] (receiver first). A CallTypeParam with RecvAddr binds its
// receiver, per candidate, to RecvAddr when the candidate's method has a
// pointer receiver and every instance of the type parameter is a value
// type, to Args[0] when the method has a value receiver or every instance
// is a pointer, and to both when the instances are unknown or mixed
// (solve.RecvChoice). CallIface and CallTypeParam bind input 0 to
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
// the empty value. A finding is reported once per (code, object, position); two solvers
// agree when they report the same (code, position, function, object) set.
// Findings: a finding about a region Reg(s) has Obj eir.NoObj and Site s;
// any other has the object, and for a finding through a call the call's
// Site, Callee and the summary entry (Esc entries first, then Store; -1 for
// HeapStore and the bodyless default).
//
// # The summary (plan invariant I4)
//
// The summary collects, over the fixed point: Esc keys, Store pairs and
// HeapStore keys from the store check and sinks, Res from SinkReturn, and
// Region facts. Past summary.MaxRawStorePerInput distinct Store pairs of one
// input (by its source), that input's pairs are cut (summary.CutKey on both
// keys), those collected and those to come; so a store of n keys through m
// pointers does not hold n*m pairs. Then summary.Widen applies the caps and makes every set an
// antichain (the reference solver calls the same Widen, so the two agree on
// the reduction). Store pairs with both keys of one input are kept (I4(b)).
// Paths are those of Env.Paths, shared by both solvers. Esc and HeapStore
// keys of an input in eir.Func.ResultHolds are left out: a standard
// library function's //pu:resultholds p says it keeps p only in its result
// (section 20.3), which the caller checks as it checks any result; for
// alloc.Use, what is allocated in the window it opens is a view of p
// (eir/arena.go).
