# Escape analysis redesign: a proposal

Status: proposed, 2026-10-20. Not normative: sections 18.3, 18.4, 11.5 and 12.11 of the specification
define what the analysis must accept and reject, and this plan changes none of them. Owner decisions that
bind it (recorded in the project decision log):

- **D45**: no garbage collector anywhere, not even in the bootstrap compiler; the escape analysis is
  redesigned, with bounded and reclaimable memory by design, before bootstrap stage 2, and stage 2 waits
  for it.
- **D44**: analysis summaries may be cached in a file and checked (never trusted) in one pass; no
  annotations, ever.
- **D47 (revised 2026-10-21, made under the owner's delegation)**: **no language rule for dynamic calls.**
  At a call through an interface method or a func value, the callee's summary is the join of the summaries
  of every implementation callable there (per method name and signature, per func-value type, from the
  candidate sets of stage B); a caller that passes a view of its frame into a call whose join keeps it is
  rejected at the caller, as today. No specification change is needed. A provisional rule (a view passed
  through a dynamic call may not be kept by the callee) was measured first and dropped:

  | Program set | dynamically reached functions that keep a non-receiver parameter's own pointer | that keep the receiver |
  |---|---|---|
  | `lib/pluto` (over the corpus) | 1 func value, 0 methods (2 more keep a pointer loaded through a parameter) | 4 methods |
  | corpus programs, outside the library | 9 func values, 1 method | 23 methods |
  | translated front end (`selfhost/demo/diagdemo`, about 1,300 dynamically reachable functions) | 59 func values, 9 methods (71 and 12 counting loaded pointers) | 64 methods |

  "Keep" is the current analysis's summary: stored in heap or package memory, or into memory another
  parameter points to; restricted to the functions the lowered code reaches through an interface or
  func-value call (`go run ./tools/escapeoracle -keepers`, `-translated`). The keeps in the translated
  compiler are of heap (arena) pointers, which are safe, and a `*T` parameter cannot tell a frame view from
  a heap pointer, so a per-parameter rule would reject ordinary Go-familiar code.

  Consequences: stage 3 is per-signature joins over the candidate sets; there is no deferred dispatch and
  there are no obligations (section (c)'s tags may still sharpen the join where a caller knows the dynamic
  type, but nothing depends on them). Scheduling keeps the dynamic edges (dispatch nodes D(M) to every
  candidate), so the largest SCC is the one measured with dynamic edges unioned (section (e) and the stage
  0 numbers below). Summaries stay cacheable per package (D44), but a summary that applied a join depends
  on the program-wide candidate set of that signature: the cache records the join's members and is
  invalidated when they change. `sched.Facts.Candidates` remains the single place candidates come from.
- **Open**: whether the precision paragraph of section 18.3 needs rewording (section (h), "Spec text").

Build status: stages 0 to 2 are being built in `internal/escape2`, off by default, by several builders in
parallel; the shared interfaces are the files named in `internal/escape2/doc.go`.

The original study's status line follows. Design study, no code. Written against `cd87595c` (this worktree), the four adversarial
reviews (REVIEW.md to REVIEW4.md in the worktrees named in the brief), the selfSafe notes of
`940e37f3`, decision D44 (`project:plutonium:decisions`) and the specification sections 11.5, 11.11,
12.11, 12.12, 18.3, 18.4, 18.6, 18.9, 18.12, 19.3 and 19.4. Line numbers are of this worktree unless
a report is named. Nothing was built or run for this study; every estimate marked "estimate" is one.

## 0. Summary

Recommendation: build a second analysis beside `internal/escape`, in stages, and cut over when it
matches or beats the current one on the review programs, the escapedigest corpus and the translated
compiler. Its five structural changes, each aimed at a cause found in the code, not at a single hole:

1. **One desugaring, checked against the code generator.** The analysis runs on a small explicit IR
   (EIR) built by one module that turns every construct the code generator turns into a call (implicit
   drops, index and `len` sugar, range bodies, intrinsics, wrappers, adapters) into a call
   instruction. A test-time oracle compares, for every function of the corpus and the translated
   compiler, the calls, itab methods and func-value symbols that `internal/lower` emits with what EIR
   has. Twelve of the review defect classes are missing calls or missing candidates; the oracle turns
   that class into a mechanical test failure.
2. **Dynamic types come only from conversions.** An interface or func value carries a tag, the static
   type (or literal identity) at the conversion that made it. No rule ever derives a dynamic type from
   the type of an abstract location. The type filters of today (`walkkey.go:123-158`,
   `call.go:597-696`) judged pointers by location types; seven defect classes in the reviews are that.
3. **Deferred dispatch.** A dynamic call whose target comes from a parameter is not unioned in the
   callee; the summary records it, and the caller that knows the tag resolves it exactly. This replaces
   filters as the source of precision for `fmt`, iterators and method values.
4. **Bottom-up over call-graph SCCs with all whole-program facts fixed first.** Candidate sets,
   instantiation sets and heap-only fields are computed before any summary, so nothing global changes
   during the fixed point (today every change of them re-walks every function, `deps.go:37-48`). A
   summary is final when its SCC converges; the SCC's working memory is then released.
5. **A compact, bounded domain.** Keys are packed integers over interned access paths, with no
   `types2.Type` and no strings; sets are hash-consed sorted arrays in a per-SCC arena; each summary has
   a fixed cap with a sound widening.

Estimated analysis-owned peak memory on the translated compiler: tens to a few hundred MB, the upper
end only if one giant SCC exists (section (e)). D44's checked cache is the post-fixpoint check of
section (f), per SCC, with per-package files.

## 1. Where the cost comes from today

The current design (package comment `escape.go:1-28`) walks the AST of every function in rounds until
no summary grows (`escape.go:363-386`, at most 100 rounds), and inside each round walks each function
again until its frame stops growing (`escape.go:413-431`, at most 50 walks).

- **Global invalidation.** A function is skipped only if nothing it read changed (`deps.go:37-48`), but
  any change to a global counter (`globalVers`, `deps.go:15-21`) re-walks every function. The counters
  move during the rounds: a new converted type (`escape.go:710-711`), a new func value
  (`escape.go:844-846`, `857-859`), a withdrawn heap-only field (`fields.go:21-23`), a new literal
  (`escape.go:475`). So candidate discovery and the fixed point are interleaved, and each discovery
  restarts the whole program. This is the most likely reason for about 22 rounds.
- **Fat tokens and keys.** A `tok` (`tokens.go:74-105`) carries two strings, an interface value
  (`filt types2.Type`), three pointers and several ints: about 120 bytes, hashed as a map key. A `pkey`
  (`tokens.go:652-665`) has a string path and a `types2.Type` filter; every distinct (path, filter,
  filter position, filter shape, flags) is a separate key, which is why summaries reach 8.8M keys (about
  1,100 per function for roughly 8,000 functions; section (e) counts them).
- **Set construction.** `tbuf` (`tokens.go:410-447`) builds a fresh hash index for every set past 12
  tokens; every `load`, `offset`, `asShape`, `tagField`, `union` builds a new set
  (`walkkey.go:21-56`, `tokens.go:148-154`, `254-262`, `398-404`). `loc.get` sorts the matching keys on
  every miss (`tokens.go:599-624`). This is the 1.29 TB in `tbuf.add`.
- **Per-site state that only grows.** `siteState` (`escapes.go:43-64`) keeps, for every call site,
  callee and receiver mode, a map from every caller-parameter token to a count; it lives until the end
  (`escape.go:364`). `reachesParam` allocates a map per call (`escapes.go:166-196`).
- **Nothing is reclaimable mid-run.** Summaries, sites, frame state and reasons of all functions stay
  live until the reporting pass, because any function may be walked again in any round.

The recent speedups (`033ce513`, `6002fd24`) attack the constant factors (apply each key once per
pointer, skip unchanged functions, hash keys once). They cannot remove the global invalidation or make
memory reclaimable, since both follow from whole-program rounds.

## 2. Why the holes keep coming

Grouping the defects of the four reviews by mechanism (details in the table of section (d)):

| Class | Mechanism | Defects |
|---|---|---|
| A. Missing calls | a construct the code generator turns into a call is not a call to the analysis | implicit drops (R3 §3), index sugar (R3 §5), `len` (R4 §7), intrinsics with placeholder bodies (R3 §1), range over func (R3 §2), func-typed type parameters (R4 §3) |
| B. Missing candidates | the list of functions a dynamic call may reach is built by a separate enumeration that misses a case | union variants as values (R2 §2), `*T` keyed by type text (R3 §4), interface promoted from an embedded field (R4 §4), promoted method expressions (R4 §8), `yield` behind a pointer (R4 §5), fmt's descriptor walk (R1 f03-f10) |
| C. Unsound narrowing | a filter drops a pointer because of the type of the abstract location it names | closure records given a dynamic type (R1 §1), method values on elements and converted pointers (R1 §2), direct structs and interior pointers in interfaces (R1 §4), shapes dropped from keys (R2 §1), unmarked conversions (R2 §3), value-method narrowing with an incomplete candidate set (R3 §4, n21) |
| D. Missing summary effects | a store the callee does is left out of its summary | `selfSafe` (R4 §1) |
| E. Unchecked global assumptions | an optimistic whole-program fact misses a write | heap-only fields (R3 §6, R4 §6), `interfaceMayHoldFunc` (R1 c08, c09) |
| F. Trusted unsafe library code | library code hides a pointer from the rules | `viewWord` in fmt (R1 §3), `WaitGroup.Task` with `rt.Pinned` (R4 §2) |
| G. Out of scope or partly | lifetime classes beyond the frame | local owners, counted references, arenas (R3 §7, roadmap 138); `goto` loops, E3401 (R3 §8, roadmap 139) |

A and B are enumerations duplicated between the analysis and `internal/lower`: the lowerer decides
what is called, the analysis guesses it separately (`sugar.go`, `drops.go`, `rangefunc.go`,
`intrinsics.go`, `ifaceCandidates` at `escape.go:884-931`, `funcCandidates` at `escape.go:994-1008`,
`regionSig`/`holdsFunc` in `regions.go:31-41` "has the same rule" as lower). C is one design choice
(filters that inspect `loc.valueType()`/`loc.dynamicType()`) applied in four places. Fixing each case
when a reviewer finds it does not end either class.

## 3. The proposal

### 3.1 EIR: one desugaring

EIR is a per-function list of instructions over virtual registers in SSA form (for locals whose
address is never taken; the rest are frame objects), with a control-flow graph:

| Instruction | Meaning |
|---|---|
| `r = &o.f` | address of frame object `o` (variable, parameter slot, temporary, interface box, closure or method-value record, slice-literal array, variadic array, statement temporary, region, arena window object), field path `f` |
| `r = heap` | allocation, static memory, constant, pointer from `uintptr` (18.3) |
| `r = in(i)` | input `i` of the function (receiver first; a literal's closure record is an input) |
| `r = load x.f`, `store x.f <- y` | memory through a pointer, with the static type of the location written |
| `r = field x.f`, `r = elem x` | offset into what `x` points to |
| `r = conv x : T -> I` | interface conversion; makes a box object when `T` is not direct; sets the tag `T` |
| `r = closure L {captures}` / `r = methodval T.M x` | record object plus tag (literal `L`, or `T.M`) |
| `r = assert x : T` / `: I` | type assertion; refines the tag (below) |
| `call` | static `f`, interface `I.M`, func value of signature `S`, or a contract-table entry; arguments, results, call-site id, source position |
| `sink kind x` | return, package variable, channel send, task statement argument, external |
| `phi` | SSA join |

The desugarer is one module, a type switch over every AST node kind with a default that is an internal
compiler error (AGENTS.md "No silent failures"). It produces calls for: explicit calls; calls through a
value whose core type is a signature, type parameters included (R4 §3); implicit drops at every scope
exit, reassignment, statement temporary and parameter, and `defer x.Drop()`, by the rules of 18.6; the
11.11 forms `Index`, `SetIndex`, `IndexOk`, `op=`, `++` and `Len`; `All` for a range over a container;
a range over a function as a call of the iterator with the body as a synthesized function (12.11); the
wrappers the lowerer emits (promoted methods, itab adapters, `ifaceMethodValueWrapper`, promoted method
expressions, union variants used as values); deferred calls at each exit; task statements and
`sync.Scope.Task` (12.12, 19.4); and every function the lowerer expands inline, through a contract table
shared with the lowerer instead of a second list (`intrinsics.go:33` today duplicates
`lower/strops.go`).

**The lowering oracle.** In a test mode (`PLUTO_ESCAPE_ORACLE=1`), after lowering, before inlining and
optimisation, every function's SSA is compared with its EIR:

- every `OpCall` (`ssa/op.go:150`) at position p with symbol s has an EIR call at p whose callee is s,
  or whose candidate set contains s, or s is in the runtime contract table;
- every `OpCallInd` (`ssa/op.go:151`) at p has an EIR dynamic call at p;
- every method in an itab the lowerer emits (`lower/iface.go:330`, `itab`, `itabAdapter` at 869) and
  every method reachable from an emitted type descriptor (fmt's walk) is a candidate of its name;
- every function symbol whose address the lowerer stores as a func value is in the func-value set;
- every instantiation the lowerer emits is in the instantiation set.

Values carry positions and inline sites (`ssa/func.go:19-33`), so the comparison is exact. The oracle
does not need the two modules to agree on anything but the calls; if both share a mistake (both skip a
drop), the generated code does not make that call either, so the analysis is still right about the
program that runs. It runs on the corpus, the review programs and the translated compiler in CI.

### (a) Abstract domain and representation

**Inputs.** A summary is relative to inputs: the parameters (receiver first; for a literal, its record),
then one pseudo-input per deferred dispatch result (section (c)). A caller binds each input to an
abstract value.

**Access paths.** As the specification promises (18.3, "How precise the frame check is"): up to three
loads and eight steps, two field steps between loads; a longer path is cut back to its first load plus
one load and marked `ge` (this path or any extension), as `tok.extend` does now (`tokens.go:313-347`).
Paths are interned in one program-wide trie: a path is a `uint32`, and extend, cut and prefix tests are
memoized table lookups. The alphabet is load plus field indices, so the trie is bounded by the program's
types and the limits.

**Keys.** `(input:8, path:32, ge:1, borrowed:1, counts:1, directCount:1)` packed in a `uint64`. The
counted-error flags of today stay (16.5 to 16.7; `call.go:286-306`). There is no filter, no filter
position and no shape in a key: those dimensions are what multiplied keys and what R2 §1 broke.

**Summary of f.** All sets are antichains under subsumption (a `ge` key covers every key of the same
input and flags whose path extends it), kept sorted:

- `Esc`: keys whose pointers f keeps where they outlive the call (heap, package variables, channels,
  tasks, external code).
- `Store`: pairs (src key, dst key): f stores what src leads to into memory dst leads to. Pairs with
  `src.input == dst.input` are included (the selfSafe fix).
- `HeapStore`: keys of places f fills with heap pointers (`summary.addHeapStore`, `tokens.go:757-773`).
- `Res[r]`: (key, result field tag) pairs and heap flags, as `resultSum` (`tokens.go:726-749`).
- `DD`: deferred dispatches, at most 16 (section (c)).
- region facts (`builds`, outer results; `tokens.go:899-905`).

**Caps and widening.** Per input at most 64 `Esc` keys and 128 `Store` pairs; past the cap the keys of
that input are replaced by their cut to one load with `ge`, which covers them (monotone, sound). Size of
one summary is then bounded by about `inputs x (64 + 128 x 2 + results x 32 + 16 DD)` words; with the
antichain the typical size is a few dozen keys (estimate).

**Intraprocedural tokens.** `Obj(o, fieldpath, tag)` for a frame object (dense per-function index,
field path up to two fields as now, `capFields` at `tokens.go:294-304`), `In(input, path, flags)` for
what an input leads to, `Heap(counted, tag)`. Packed in a `uint64` with a per-function tag table. Sets
of tokens are sorted `[]uint64`, hash-consed in the SCC arena, referred to by a `uint32`; sets of up to
two tokens live inline in the node. Union is a merge with a memo table. Frame object contents are a set
per (object, field path). Location lifetimes form a small order (statement temporary < loop iteration <
block < frame < caller memory < heap/static) that one store check uses for E3001 to E3006 and E3009
(today spread over `store`, `keptPast`, `stmtTempKept`, `expr.go:1457-1530`).

**Solver.** Inclusion constraints (Andersen style) over EIR registers and object fields, solved with a
worklist and difference propagation, copy cycles collapsed online. Loads, stores and call applications
are complex constraints that fire on the delta of their operand sets, so a callee key is applied once
per new pointer (what `siteState` approximates today, `escapes.go:66-138`) without a per-site map that
outlives the function. Flow sensitivity for unaddressed locals comes from SSA, replacing `flow.go`'s
environments; a loop back edge is a phi, where E3006 for SSA values is checked (18.4, "checked where the
next iteration starts"). Loops are natural loops of the CFG, so `goto` loops are loops (R3 l05).

**Field sensitivity bound.** Unchanged from 18.3: fields two levels deep in objects and between loads;
struct parameters field by field (`bindParamFields`, `walk.go:135-151`).

### (b) Scheduling

**Stage A, per package (cacheable).** Build EIR for every function, literal, loop body and synthesized
wrapper. Extract facts: conversions (converted type and its by-value parts, for fmt's walk, today
`noteConverted`, `escape.go:666-747`), func values taken, instantiations, static call edges, dynamic call
sites, field writes (for heap-only fields), and a summary-free origin analysis of each dynamic call
(below). EIR can then be dropped and rebuilt on demand, or kept if memory allows.

**Stage B, whole program, small.** Compute once, before any summary: the instantiation closure; the
converted-type set with a canonical type interner (identity, type parameters qualified by their
declaring object; no text keys, R3 §4); candidate sets per (method name, signature) from the method sets
of converted types and their promoted wrappers, and per signature from the func values; heap-only
fields (section (d), I6). None of these changes afterwards, so no fact read by a summary moves under it.

**The call graph for scheduling.** Edges f→g for static calls; f→L and f→T.M for every closure L that f
builds and every type T that f converts (f may resolve deferred dispatches with those tags); and f→D(M)
for a dynamic call that may need a union, where D(M) is a dispatch node with edges to every candidate.
Whether a dynamic call may need a union is decided by an origin analysis that needs no callee summary:
it treats a call's results as `reach(args) ∪ heap ∪ region` (sound for every callee, since each callee
is itself checked not to return its frame) and callee stores as writing anything reachable into
anything reachable. A call whose target is only input-derived or tagged in f gets no D(M) edge. Tarjan
gives SCCs; they are processed callees first.

**Within an SCC.** Worklist over its functions: a function is re-solved (incrementally, from the delta)
when a summary it reads grows. The domain is finite and the transfer monotone, so it terminates; caps
bound the height.

**When a summary is final.** When its SCC's worklist is empty. Every later SCC only reads it. Errors of
the SCC's functions are reported then, while their EIR is in memory, since they depend only on final
callee summaries; the deferred checks a summary carries are reported by the callers that resolve them
(section (c)). Functions with no caller in the graph (main, initialisers, exported, interrupt and task
entry points, and dead code) resolve their own deferred dispatches by union at the end.

**Dynamic constructs.** Interface calls, func values, type-parameter method calls (by name and
signature over all instantiations, `ifaceCandidates` byName today), range over func (body function plus
iterator call), implicit `Drop` (a static call, or an interface call for an owning interface), index
sugar (a static or interface call by the receiver's type) all become EIR calls before scheduling, so
they are edges like any other.

### (c) Precision at dynamic calls without unsoundness

**Tags.** A token in an interface's data word, or a closure record, carries a tag: the static type of
the expression converted (`conv`), the literal (`closure`), or the method (`methodval`). For a value of
an interface type converted to another interface, the tag carries over. A type assertion to a concrete
`T` gives tag `T` (the assertion checked it at run time); an assertion to an interface `I` adds the
constraint "implements I" to an unknown tag. A generic body's conversion of a type-parameter value has
the type parameter as its tag; a caller that instantiates the function substitutes its type arguments.
A token loaded from an input or the heap has the unknown tag.

At a dynamic call in f with target tokens X:

1. tagged tokens dispatch exactly: tag `T` and method `M` select the one method of `T` (or of `*T`,
   by the method set of the tag, not of a location) and its summary is applied to that token alone;
2. tokens from an input with unknown tag produce a deferred dispatch (below);
3. other unknown-tag tokens (loaded from heap or package memory) dispatch by union over the full
   candidate set, filtered only by the assertion constraint. Such a target is never a frame closure or a
   frame box (18.3: a closure of a frame can never be stored in heap or package memory), but the
   arguments may be frame views, so the union applies to them.

**Deferred dispatch (DD).** A DD entry is (target key, method name and signature or func signature or
"reflective walk", constraint, per-argument binding, result input). Each argument binding is a key
(input-derived), `heap`, or an **obligation**: a frame object of f with the call site, which the
resolving caller must check is not kept. The call's results become a pseudo-input of f's summary, so
whatever f does with them (returns them, stores them, keeps them) is expressed as keys rooted at that
input, and the caller binds it to the results it computes when it resolves the DD. The caller
substitutes its own values for f's inputs; for each tag it sees at the target it applies that
candidate's summary (including the candidate's own DD entries); for an unknown tag from its own input it
re-roots the entry into its own summary; otherwise it applies the union. An obligation whose candidate
keeps the argument is an error reported at the call site in f, naming the caller's tag: "seq passes &x
to yield (iter.p:12), and the loop body in f keeps it (main.p:20)". Past 16 entries, the oldest is
resolved by union in f. Every entry is resolved somewhere: by a caller that knows the tag, by union at a
heap origin, or at a root.

This gives, without filters:

- `fmt.Println(&s)`: the caller converts `&s` with tag `*S`; `Println`'s summary has a DD for `String`
  (and the reflective walk) on its argument's data words; the caller applies `(*S).String` only.
- `k01` (R2): `g(p *[2]S) { call(&p[1]) }` converts `&p[1]` in g with tag `*S`, dispatches exactly in
  g, and records that g keeps an element of what p points to. Nothing about shapes reaches the caller.
- range over func: the body closure has the body as its tag; the iterator's `yield(&x)` with x in the
  iterator's frame is an obligation, checked by the loop's own function against its body's summary. No
  reachability test decides whether `yield` may be the body (R4 §5, `rangefunc.go:150` today).
- method values: the record's tag is `T.M` at creation, so `arr[1].visit` (R1 m02) applies `visit` to
  the interior token as it is.

**The selfSafe store.** The store rule records every edge between two input-derived pointers,
including those of one input (`expr.go:1508-1510` skips them today; `940e37f3` records them). The
fixer's note is right that a path-based shortcut is unsound: an abstract path names every object the
place held during the call. Precision comes from how callers apply edges:

- the caller applies a store edge to its own abstract objects: the destination set and the value set are
  what the two keys lead to in the caller's memory, and only a frame object reaching a destination that
  outlives it is an error. A move within a vector's storage (`vec.p:415-437`) leads from heap elements to
  heap storage in a caller whose vector holds heap pointers, and is a no-op there;
- in `realloc`, the destination is `v.data`, assigned from `v.a.Alloc(...)`, an interface call whose
  target is input-derived: today a `recvReach` union over every `Allocator` (`call.go:550-571`,
  `escape.go:901-907`) widens the destination to everything reachable from `v.a`. With a DD the caller
  resolves `Alloc` by the allocator's tag, or by union only when the allocator came from the heap;
- the cut of a long path keeps its first load (`tokens.go:326-334`), so a coarsened key never stands for
  the parameter's own pointer.

Whether these recover all six programs `940e37f3` rejects (testdata/lib/errors, exec, fmtappend,
osnames, vec; kernel/smoke) is the first experiment of stage 3; I could not run them in this study. If
some remain, the next step is the one D44 makes cheap to try: raise the path limits (18.3 allows raising
them, never lowering) for store edges only.

### (d) Soundness argument

The invariants below are what reviewers should attack. Each names how it is enforced.

- **I1 (calls).** Every call the generated code makes at a source position (explicit or implicit) is an
  EIR call at that position whose callee, or candidate set, contains the function called. Enforced by
  the desugarer being one total type switch, and checked by the lowering oracle.
- **I2 (candidates).** For each dynamic call, the functions that may run there are within its candidate
  set: the itab methods, descriptor-reachable methods and func-value symbols the generated code
  contains, and all methods of the name over all instantiations for type parameters. Computed once
  (stage B) by a module shared with the lowerer where they can share code (itab method sets, wrappers,
  `regionSig`), checked by the oracle.
- **I3 (tags).** A tag on a token equals the dynamic type, or closure identity, of the value at run
  time. Tags are made only at conversions and closure or method-value creation from static types, and
  changed only by assertions; no rule reads a tag from a location. An interface data word that is a
  pointer into a location is tagged by the type it was converted with, which is its dynamic type by the
  language (7.9), whatever location it points into.
- **I4 (summaries).** For every execution of f from a caller state σ, interpreting a key over σ closed
  under f's own store edges: (a) every non-heap pointer f (or anything it calls) keeps where it outlives
  the call is covered by `Esc`; (b) every non-heap pointer stored into memory reachable from an input is
  covered by a `Store` pair, including pairs within one input; heap stores by `HeapStore`; (c) every
  non-heap pointer in a result is covered by `Res`; (d) every dynamic call f cannot resolve is a DD
  entry, and every frame object of f passed to it is an obligation. The caller's solver applies edges to
  a fixed point, which gives the closure.
- **I5 (local check).** For every frame object o of f, every way a pointer to o reaches a result, heap or
  package memory, caller memory, a channel, a task, a location of a longer lifetime class, or an
  obligation that a candidate violates, is reported. Follows from I1 to I4, I6 and the solver reaching a
  fixed point.
- **I6 (heap-only fields).** A field class is (underlying struct type, field index), so `Node`, `Raw`,
  `type Alt Node` and `struct{n int; next *int}` with the same underlying fields share a class (R3 §6,
  R4 §6). A write is any instruction that puts a value of a struct type into a location of a type that
  holds it by value: stores, parameter and result binding, variable initialisation, composite literals,
  conversions when they are materialised, and for a location of a type-parameter type every struct
  instantiation. Heap-only is the greatest fixed point of "every write is heap": sources are
  allocations, nil, loads of heap-only fields, parameters every caller (all candidates for dynamic
  calls) passes heap, results every return of which is heap. Frame objects passed to any call count as
  holding anything. This is computed in stage B from per-function source facts; it is sound alone, so no
  validation round is needed. Writes through memory reinterpreted with `unsafe.Pointer` stay excluded
  (18.3, 18.12).
- **I7 (trusted library code).** Every library function that converts a pointer to `uintptr` and back,
  uses `rt.Pinned`, or has no body, has an entry in the contract table that states its effects (fmt's
  descriptor walk as a reflective DD; `WaitGroup.Task` as a task start that keeps its receiver), or is on
  a reviewed list of functions whose hiding is harmless. A test lists the unsafe sites with the
  `pluto vet -unsafe` machinery (18.12) and fails on one with neither.
- **I8 (bounds).** Cutting paths and capping keys only widen.

Mapping the review defects:

| Defect | Invariant | Why the new design cannot have it |
|---|---|---|
| R1 §1 s01-s04, closure records with a dynamic type | I3 | `HF(lit)` tags the record `HF`; `(HF).String` is applied to it |
| R1 §2 m02-m10, method value on an element or converted pointer | I3 | the record's tag is `(*S).visit` from the method value; the interior token is not judged |
| R1 §3 f08, R1 f03-f10, fmt laundering | I7, I2 | fmt's walk is a contract entry (reflective DD); its candidates are the descriptor-reachable methods the oracle checks |
| R1 §4 h02, g01, g02, n02, n04, n06 | I3 | a pointer-shaped struct is tagged `D`, `&arr[1]` and `(*S)(&b)` are tagged `*S` at conversion |
| R1 c08, c09, region predicate | I2 | `regionSig`/`holdsFunc` shared with the lowerer instead of copied |
| R2 §1 k01-k26, shapes dropped from keys | I3 | keys have no filters or shapes; the conversion in the callee dispatches there |
| R2 §2 u13, u20-u22, v05, union variants as values | I1, I2 | a variant used as a value is a synthesized function; the oracle finds the lowerer's thunk |
| R2 §3 m20-m24, unmarked conversions | I3 | tags do not depend on shape marks |
| R3 §1 intrinsics | I1, I7 | one contract table, shared with the lowerer's list of inline expansions |
| R3 §2 range over func | I1, I4(d) | the body is a function, the loop a call; `yield(&x)` is an obligation |
| R3 §3 implicit drops | I1 | drops are calls; the oracle sees the lowerer's drop calls |
| R3 §4 `*T` text keys, n21 narrowing | I2, I3 | canonical type interner; no value-method narrowing, a tag selects exactly |
| R3 §5 index sugar, R4 §7 `len` | I1 | 11.11 forms are calls |
| R3 §6, R4 §6 heap-only writes | I6 | field classes by underlying type; writes by location type |
| R3 §7 owner, counted, arena views | none | not addressed; lifetime classes give them a place (stage 4), roadmap 138 |
| R3 §8 l05 `goto` loops | 3.1 loops | loops are CFG loops |
| R3 §8 b16, b23 E3401 | none | scoped-task rule, roadmap 139 |
| R4 §1 selfSafe | I4(b) | same-input pairs are recorded |
| R4 §2 `WaitGroup.Task` | I7 | `rt.Pinned` use needs a contract entry |
| R4 §3 func-typed type parameters | I1 | calls classified by core type; `OpCallInd` at that position |
| R4 §4 promoted from embedded interface | I2 | the promoted wrapper is a candidate; the oracle checks itab contents |
| R4 §5 `yield` behind pointer, slice, interface | I2, I4(d) | no reachability narrowing of candidates; obligations resolve by tag |
| R4 §8 promoted method expressions | I1, I2 | the wrapper is the func value |

What the argument does not cover: I1 and I2 rely on the oracle's corpus for completeness of the
desugarer beyond its structure (a construct that appears in no corpus program is checked only by the
type switch); I7 relies on the contract table entries being right; G-class rules are outside it.

### (e) Memory

**Why it is bounded and reclaimable.** Three regions:

- *Program arena* (lives to the end): interned paths, the type interner, candidate and instantiation
  sets, call graph, final summaries, compact reasons (site id, callee id, key index, instead of closures
  over the analyzer, `tokens.go:843-855`). Each summary is capped (section (a)).
- *Package arena* (stage A): EIR and facts of one package; facts move to the program arena, EIR is freed
  or kept per the policy.
- *SCC arena*: constraint graphs, hash-consed sets, memo tables and per-site state of the functions of
  one SCC; reset when the SCC is final and its errors are reported.

Peak = program arena + largest SCC working set (+ kept EIR). There is no state that grows with the
number of rounds, since there are no whole-program rounds.

**Estimate for the translated compiler** (diagdemo plus the library; selfhost has about 127,000 lines,
4,745 top-level `func` declarations and 905 `func(` literals or types; the library has 50,000 lines and
2,323 functions):

| Item | Assumption | Estimate |
|---|---|---|
| functions | 4,745 + 2,323 + literals and loop bodies | about 8,000 |
| final summaries | typical 30 keys x 8 B plus headers; cap about 5 KB | 2 to 40 MB |
| interned paths, type ids | under 200,000 entries | under 5 MB |
| call graph, candidates | about 150,000 edges | under 5 MB |
| EIR, if all kept | about 600,000 instructions x 32 B | about 20 MB |
| largest SCC working set | 50 KB per function in the SCC | 5 MB (SCCs of 100) to 200 MB (one SCC of 4,000) |

So between about 50 MB and 300 MB owned by the analysis, against 4.5 GB peak heap measured now
(`translated_test.go:17-18`) and 1.72 TB allocated. Allocation total is about the peak times the number
of SCC iterations, not times 22 whole-program rounds. The unknown is the size of the largest SCC once
dynamic edges are in; stage 0 measures it.

### (f) D44's checked cache and per-package caching

D44 asks for summaries loaded from a file and checked in one pass: each function, analysed against its
callees' loaded summaries, must produce a summary no wider than its own loaded one.

- **The check.** For an SCC with loaded summaries S, solve each member once with S assumed for every
  member and the final summaries of earlier SCCs, and accept if F(S) ⊑ S for every member. Any
  post-fixed point of a monotone F lies above the least fixed point (Knaster-Tarski), so an accepted S
  is a sound over-approximation even when it was edited by hand to be narrower. A file that fails is
  recomputed for that SCC from bottom; the SCCs above it are then checked against the new summaries.
- **A wider file costs no false rejections.** A hand-widened S passes the check but could make a caller
  report an escape. Before any error is reported from a function whose callees' summaries came from the
  file, the SCCs on the path to the error are recomputed from bottom; the error stands only if it
  survives. A wrong file then costs time, never safety and never a verdict.
- **What a summary depends on.** Its function's EIR (by a hash of the source and the compiler version),
  its callees' summaries (by hash), and the stage B facts it read: the candidate sets of the D(M) nodes
  it unions over, the heap-only field classes it read, the instantiations it substituted. Stage B facts
  are recomputed every run from the per-package facts (cheap, cached per package too); a summary whose
  facts changed is rechecked.
- **Per package.** A package's file holds its stage A facts and the summaries of its functions, sorted,
  with paths written as step lists and types as canonical names, no addresses (D44's determinism
  rule). Since DD entries and obligations keep the effects that depend on callers inside the summary,
  a package's summaries do not depend on which program imports it, except through stage B facts, which
  are recorded.
- **Bootstrap.** Stage 1 writes the files; stages 2 and 3 run stage A and B, then one check per SCC.

### (g) Migration

The new analysis lives in `internal/escape2` behind `PLUTO_ESCAPE=new|old|both`; `both` runs the two
and diffs verdicts per error site. Oracles: the review suites (`testdata/review2`, `review3`, review4's
want.txt), `tools/escapedigest` on the corpus, `soundness_test.go`, the translated compiler's error set
(`translated_test.go`), and the lowering oracle.

| Stage | Content | Size (estimate) | Exit condition |
|---|---|---|---|
| 0 | Lowering oracle and SCC-size tool, run against the current analysis | 600 lines | lists every class A/B gap of today; reports the SCC size distribution of the translated compiler |
| 1 | EIR and desugarer, shared contract table and itab/wrapper module with the lowerer | 2,500 | oracle green on corpus, reviews, translated compiler |
| 2 | Domain, interner, solver, summaries, SCC scheduling, union-only dynamic calls, heap-only stage B | 3,000 | every review program rejected; corpus diff classified |
| 3 | Per-signature joins at dynamic calls (D47 revised: no deferred dispatch, no obligations), optional tags to sharpen joins, selfSafe edges | 1,500 | corpus accepted as by the current analysis, or each difference justified; the six `940e37f3` programs accepted |
| 4 | Regions (11.5), loops, statement temporaries, arenas, scoped tasks, `Calls` facts for `internal/moves` | 2,000 | escapedigest identical on accepted programs; moves tests pass |
| 5 | Messages and reasons at today's quality (`report.go`) | 800 | `cmd/pluto` diag goldens unchanged or reviewed |
| 6 | D44 cache, per-package files | 800 | adversarial file review per D44 |
| 7 | Cutover, delete `internal/escape`, regenerate go2pu translation | | translated compiler built with the new analysis |

Each stage ends with an adversarial review of its rules, as the last four did of the code. A small
reference solver (the same rules applied naively to a fixed point, no deltas, no caps) runs on the
review programs and `soundness_test.go` cases to check the optimised solver.

### (h) Risks and what I am unsure of

- **A giant SCC.** If interface dispatch in the translated type checker ties thousands of functions into
  one SCC, the SCC arena holds them all and iteration inside it looks like today's rounds, smaller. Stage
  0 measures it. The origin analysis already keeps input-derived dispatch out of the graph.
- **DD growth.** Entries propagate up call chains until a tag is known. The cap bounds them per summary,
  but resolving by union at the cap loses precision where the cap is hit; how often is unknown.
- **Result pseudo-inputs.** They make summaries a small graph rather than a list; application is a
  substitution. Correctness of re-rooting through several levels is the most intricate part to review.
- **Generic tags.** Substituting type arguments at callers needs bounded type terms; a recursive
  instantiation chain falls back to the unknown tag.
- **Heap-only in stage B.** The greatest fixed point over source facts is sound, but may call fewer
  fields heap-only than today's interleaved computation (`fields.go`, `passArgs` at 246), which uses full
  points-to sets. 18.3 states heap-only by what is stored, so any loss is a false rejection the
  specification does not allow; if the corpus shows one, the fix is a validation round (assume more,
  check every write at the end, shrink and recompute dependants).
- **The six selfSafe regressions.** Section (c) argues the main cause; not demonstrated.
- **Results of dynamic calls.** Where neither a tag nor a DD applies, results are bounded by the union
  of candidate summaries, as today; no change.
- **Messages.** Errors found through obligations are reported in a callee on behalf of a caller; the
  text must name both. Stage 5 may need more than its estimate.
- **The oracle's reach.** It sees what the lowerer does at `-O0` before inlining; a call the optimiser
  adds later (none should) would be invisible.
- **Spec text.** 18.3's precision paragraph describes summaries in today's terms ("narrowed by type
  assertions on the way and by the dynamic type where the caller knows it"). The proposal keeps every
  stated limit and is at least as precise in the cases named, but whether the paragraph needs rewording
  is a user decision before stage 3.

## 4. Compared with improving the current design

What incremental work can do:

- Record same-input store edges (the WIP of `940e37f3`) and fix the precision it loses.
- Add the lowering oracle (stage 0 works on today's analysis too) and fix each gap it lists in `sugar.go`,
  `drops.go`, `rangefunc.go`, `intrinsics.go`, candidate code.
- Replace location-judged filters with conversion tags: a new field on `tok`, removal of
  `filt`/`filtAt`/`filtShape` from `tok` and `pkey`, and changes to `walkKey`, `deriveKey`,
  `filterRecv`, `markRecv`, `filterBoundRecv`, `filterDyn`, `fastKey` and every key comparison
  (`lessKeyNamed`, `keyHash`).
- Compute candidates and heap-only fields before the rounds to stop global invalidation.
- Intern paths and filters, pack keys, replace `tbuf` maps with sorted arrays.

Constant factors could plausibly fall by an order of magnitude. Three things remain:

- memory is not reclaimable while rounds are whole-program; bounded, per-SCC reclaimable memory needs
  bottom-up scheduling, and that change plus the domain change touches nearly every function of the
  package;
- precision would still come from filters inspected at walk time, unless the tag change above is made,
  which is the larger part of the rewrite done in place;
- the code is shaped for Go's GC (maps per walker, closures in reasons, `escLog` chunks that never
  shrink); the self-hosted compiler needs explicit arenas anyway, so the translation would be reworked
  regardless.

So the incremental path converges on the same design through a longer series of edits to code under
review churn. I recommend the rebuild beside the current analysis, starting with stage 0 against the
current code, since the oracle's findings are holes today and fixing them in the current analysis while
the new one is built keeps the shipped compiler safer meanwhile.

## 5. Related work and what we take from it

Citations from memory, as given by the coordinator and from my own reading; the years and venues should
be checked before this text is quoted elsewhere.

- **Go's escape analysis rewrite** (Dempsky, Go 1.13, `cmd/compile/internal/escape`): a graph of
  locations with weighted edges counting dereferences, bottom-up over SCCs of the static call graph,
  per-parameter leak tags (to heap, to result k, with a minimum dereference count). We take SCC
  scheduling, compact per-parameter leak encodings and the idea that dereference counts summarise paths.
  We reject its fallbacks: Go moves a variable to the heap when unsure and treats interface and func-value
  calls as leaking their arguments, where Plutonium must reject and needs `fmt` to be precise. Go also
  needs no store edges between parameters, since storing into a parameter's pointee leaks to heap.
- **Demand-driven, CFL-reachability points-to** (Reps; Sridharan and Bodik, PLDI 2006, refinement-based).
  Our question is demand-shaped ("can this frame address reach something longer-lived"), but nearly every
  call that passes `&x` is a query, and D44 wants per-function results to cache. We take refinement in a
  narrow form: deferred dispatch refines a dynamic call only when a caller can, and obligations are
  queries answered by the caller that has the context. We reject a fully demand-driven analysis.
- **OCaml locality modes** (Lorenzen, White, Dolan, Eisenberg, Lindley, "Oxidizing OCaml with Modal
  Memory Management", ICFP 2024): inferred, modular, type-like local/global modes, with annotations in
  interfaces at module boundaries as I recall. We take the view of a summary as an inferred interface
  that is checked at a boundary, which is what D44's per-package files are. We reject boundary
  annotations (owner constraint) and the coarse two-point mode, since 18.3 promises path precision.
- **Region inference** (Tofte and Talpin; the ML Kit; Cyclone's regions with intraprocedural
  inference). 11.5's caller-provided regions are a fixed instance. We take nested lifetime classes as a
  partial order for the one store check (statement, iteration, block, frame). We reject region
  polymorphism and inference, which would change where memory lives (18.2 fixes it by declaration).
- **Set propagation**: Hardekopf and Lin (lazy cycle detection, PLDI 2007; staged flow-sensitive
  analysis, CGO 2011); Pereira and Berlin (wave and deep propagation, CGO 2009). We take difference
  propagation and online cycle collapse within a function and an SCC, and staging: the summary-free
  origin analysis and stage B facts are a cheap first stage that makes the precise stage sparse.
- **BDD points-to** (Berndl et al., PLDI 2003; Whaley and Lam, bddbddb, PLDI 2004). We take shared
  representation through hash-consing. We reject BDDs: sets here are small and function-local, and BDD
  node tables do not fit per-SCC arenas or deterministic output easily.
- **Steensgaard, Andersen, Das's one-level flow** (PLDI 2000). We use inclusion (Andersen) for the
  precise solve, since unification would merge frame objects with heap memory and every merge is a
  false rejection the user sees. Das-style one-level flow suits the origin analysis and the stage B
  heap-only facts, where cheapness matters and the result only guides scheduling or is validated by
  construction.
- **Datalog** (Doop, Bravenboer and Smaragdakis, OOPSLA 2009; Rust's Polonius). We take the discipline:
  one rule per EIR instruction kind, written down as a table for reviewers, and the naive reference
  solver of section (g) that implements exactly those rules. We do not take a Datalog engine as a
  dependency.
- **Compositional escape analysis for Java** (Whaley and Rinard, OOPSLA 1999; Choi et al., OOPSLA 1999):
  summaries parametric in the caller's context with inside and outside edges, close to our input-relative
  keys and store pairs.
- **Abstraction-carrying code** (Albert, Puebla and Hermenegildo, around 2004 to 2005): a fixed point
  shipped as a certificate and checked in one pass. That is D44's check.
- **Surveys**: Smaragdakis and Balatsouras, "Pointer Analysis" (Foundations and Trends in PL, 2015);
  Hind, "Pointer analysis: haven't we solved this problem yet?" (PASTE 2001).
