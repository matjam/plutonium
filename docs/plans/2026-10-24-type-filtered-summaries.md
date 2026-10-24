# Type-filtered summary entries

Status: being built, 2026-10-24. Decision D51 (owner, via the coordinator): go, extended to direct
pointers as well as boxes (lib/fmtappend: the join of the `Append` candidates applies `(*Vec).Append` to
a frame `vec.Fixed`). Section 18.3 defines what the analysis accepts; this plan adds one sentence to it
(below), which lands with the code. Not normative until then.

## The problem

The new analysis (internal/escape2) narrows a type assertion only for what the function itself makes. An
`OpAssert` to an interface drops the `Obj` tokens of boxes whose dynamic type does not implement it
(`solve.BoxAsserts`, rules.go "OpAssert"), and an assertion to a pointer-shaped concrete type drops every
box. An input's `In` token carries no type, so the narrowing is lost at the summary. When a callee asserts
its parameter and keeps only what passes the assertion, its caller sees the parameter as kept whatever it
passes:

```go
func sprintf(args ...any) {
	for _, a := range args {
		switch a := a.(type) {
		case Type:
			w.seen[a] = true // a heap store: only of a Type
		}
	}
}

check.errorf(x, code, "cannot range over %s", name) // the box of the string name is reported (E3003)
```

On the translated compiler (pluto, deferral on, at 821a84df), 78 of 357 findings are interface boxes,
55 of them boxes of strings. `types2.(*typeWriter).typ` accounts for 10 directly. A share of the 62
reached through fmt's walk are the same kind of problem: the walk calls every String and Error method on
an `In` token, and each candidate's effects land on the boxes of every type, not only those of the
candidate's receiver type. How many of these the deferral fix removes is to be measured first (D51).

## The rule

A summary entry may carry a **filter**: a type predicate and the point on the key's path where it
applies. The entry then stands only for pointers found through a pointer, at that point, whose dynamic
type the predicate admits. A caller applying the entry drops, at that point, the boxes the predicate
rejects. Everything else is applied as before.

Two predicates:

- **implements I**, for an assertion to an interface `I` that mentions no type parameter (what
  `BoxAsserts` checks today);
- **is T**, for an assertion to a concrete type `T` that mentions no type parameter, and for the receiver
  of a candidate method at a call through an interface (fmt's walk, every `CallIface`): a method of
  pointer receiver `*T` is reached only by a dynamic type `*T`; an adapter of a value receiver `T` (the
  data word points at a box of `T`) only by `T`.

A filter removes two kinds of `Obj` token, and nothing else:

- a box (`eir.ObjBox`) whose `Type`, the dynamic type of the value it holds, mentions no type parameter
  and fails the predicate;
- a **direct pointer**: an exact token at the root of an object that is a variable, a parameter's slot,
  a temporary or a composite literal's object, whose type `U` mentions no type parameter, when the
  predicate is "is `*V`" with `V` not identical to `U`. The data word of an interface value whose
  dynamic type is `*V` is a `*V`, and a `*V` points at a `V`: a token of an object of another type, at
  its root, is no such pointer. (Code that reinterprets memory with `unsafe.Pointer` is outside this, as
  it is outside the other rules of section 18.3; section 18.12.) "implements I" removes no direct
  pointer: a direct value of a type other than a pointer (a struct of one pointer, a map, a channel, a
  func) may hold a pointer to any object and implement `I`.

It never removes heap tokens, counted heap tokens, tokens with a path or GE, objects of other kinds
(closure records, regions), or anything whose type mentions a type parameter. Where the analysis cannot
tell, the token stays. That is what keeps the rule sound: an entry is dropped only for a value whose
dynamic type the program's own assertion or dispatch rejects at run time.

## Format

**Keys** (domain.Key, bits 46 to 63 free today). Bits 46 to 59 hold a filter id (0 for none; at most
16,383 filters, past which an entry simply gets none, which only widens it), and bits 60 to 63 hold the
filter's position: the number of path steps before the filtered pointer (0 to MaxSteps). Ids index one
program-wide table of predicates (a type and a kind), interned like paths, owned by the program arena.
`domain/api.go` is a shared interface; its comment rule ("change only by agreement") is met by D51.

**Tokens.** The solver's `In` token (solve/tokens.go) takes the same two fields: `w0` has bits 6 and 7
free and `base` needs 32 of its 56 bits, so the id and position fit in `w0`'s top 18 bits. The reference
solver's `tok` struct gains two fields.

**Summaries.** Entries keep their sets; a filtered key is a different key from the same key unfiltered,
and `Covers` gains one condition: `a` covers `b` when it covers `b`'s path and flags as today, and `a`
has no filter, or `a` and `b` have the same filter at the same position. An unfiltered entry therefore
absorbs filtered ones in the antichain (`summary.Widen`), as it must. Store pairs filter their source key
only; a filter on a destination key is dropped when the pair is made (the destination is where the store
goes, and narrowing it would need a second filter).

**Cache** (internal/escape2/cache). Format version 6. A file carries its package's filter table, each
entry as the kind and the type's canonical string (as for the contract table), and keys refer to it by
index. Loading re-interns the predicates. A predicate whose type no longer resolves makes the entry fail
its check, so the summary is solved again. A cached summary is checked, never trusted (D44), so a wrong
table costs a solve, not soundness.

## How the solvers apply it

**Making filters (both solvers).** At `OpAssert` to an interface or concrete type that mentions no type
parameter, an `In` token with no filter gets the filter (implements I or is T) at its current path length;
one that has a filter keeps its own. Keeping either filter is sound, and one slot keeps the format small.
At a `CallIface` the summary of candidate `m` is applied with the receiver given the filter of `m`'s
dynamic type ("is `*T`" for a method of pointer receiver, "is `T`" for a value receiver's adapter): `Obj`
tokens are narrowed at once, `In` tokens take the filter at their path length. A call whose candidates
are joined into one virtual function (D47) gets the same through the join: each member's entries keyed
on the receiver input take the member's filter at position 0 (the data word itself), so the join holds
one filtered entry per member where the members' keys differ only in their filter.

**Moving filters.** Extending a filtered token's path keeps the filter and its position. Cutting a path
to a prefix shorter than the position drops the filter, because the cut stands for more than the
filtered pointer. That covers GE cuts, the Deep load cut (0fe2a7d0), `summary.CutKey` and the caps.
`anyOffset` drops it. GE, FieldsGE and Deep keys keep a filter whose position is within their path.
Caps drop filters before they cut paths: dropping a filter is the cheapest widening.

**Applying a filtered entry at a caller.** The key's path is followed from the argument node as today
(graph.go). At the filter's position the solver takes the tokens found there through the predicate:
`Obj` box tokens it rejects are left out, and the rest go on. An `In` token of the caller found there
takes the filter, at its own path length plus the remaining offset, unless it has one already. The
reference solver does the same in `step` and `load`.

**Merged keys and joins.** The virtual join of a dynamic call's candidates (D47) keeps each member's
filters: two keys that differ only in their filter do not cover each other and both stay. Past a cap,
widening drops filters first (the unfiltered key covers its filtered forms), before it cuts paths. The
per-input store cut (`MaxRawStorePerInput`) drops filters first.

## Soundness tests

Each is a program in a new escape2 test file, accepted or rejected as stated, with both solvers.

1. A string boxed into `...any` and passed to a function that keeps only its `case Type:` operands in heap
   memory: accepted.
2. The same function given a box of a frame type that implements `Type` (a `*T` of a frame `T`, or a
   struct value): **rejected**. This is the test that the filter never drops a box that can reach the
   store.
3. As 2, with the operand reached through a field of a struct passed by pointer, and through a slice
   element: rejected. The filter's position is respected below the root.
4. As 1 and 2, with the asserting function two calls below the caller and the filter carried through a
   middle function's summary: accepted and rejected respectively.
5. A path past the limits, where the filter's position is cut away: rejected for a box that could not
   pass the assertion. The filter is dropped, not misapplied.
6. Comma-ok assertion (`v, ok := x.(I)`) and a type switch with several cases, one of which keeps: a box
   of a type of a keeping case is rejected, the others accepted.
7. An assertion to an interface that mentions a type parameter, and a box whose type mentions one: no
   filter, as `BoxAsserts`.
8. A direct value (a pointer to a frame object converted to `any`) asserted to an interface: still
   rejected (no direct pointer is filtered by "implements"). A pointer to a frame `vec.Fixed` passed
   where the only keeping candidate is `(*Vec).Append`: accepted; a pointer to a frame `Vec`: rejected.
9. A call through an interface whose only keeping candidate is `(*A).String`: a box of `B` (another
   Stringer that keeps nothing) accepted, a box of `A` rejected.
10. Cache: a summary with filtered keys round-trips through version 6, and one whose filter table names a
    type that no longer exists is solved again.

The differential generator (refsolve/gen.go) gains boxes of several types, assertions to interfaces and
concrete types, and type switches. 30,000 cases must agree, as for every change to the rules.

## The text of section 18.3

Add to "How precise the frame check is", after the bullet on calls through an interface:

> - A value converted to an interface and then asserted, by a type assertion or a type switch case, is
>   followed after the assertion only for the dynamic types the assertion admits; so is the receiver of
>   a method called through an interface, for the type whose method is called. A function that keeps
>   what passes an assertion does not keep a caller's value of a type the assertion rejects.

The sentence states the precision the rule gives and claims nothing beyond it: a value whose dynamic type
the analysis cannot tell (a direct value other than a pointer, a type parameter) is followed as before.

## Measurement plan

After D's deferral fix lands: rerun the class table (interface-box findings by object type and by the
function whose own code makes the escape). Build this if about 40 or more remain. Report pluto's findings
and time, corpus agree/differ, and E3010, before and after.
