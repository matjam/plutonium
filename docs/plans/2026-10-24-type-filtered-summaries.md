# Type-filtered summary entries

Status: proposed, 2026-10-24, not built. Decision D51 (owner, via the coordinator): yes in principle,
sequenced after D's fix of the deferral and fallback rule; build it if about 40 or more findings of the
interface-box class remain on the translated compiler after that fix. Section 18.3 defines what the
analysis accepts; this plan adds one sentence to it (below), which lands with the code. Not normative
until then.

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
  of a candidate method of `T` at a call through an interface (fmt's walk, every `CallIface`): that
  candidate's entries apply only to boxes of `T`, or of `*T` for a method with a pointer receiver.

A filter only removes `Obj` tokens of `eir.ObjBox` objects whose `Type` mentions no type parameter. It
never removes heap tokens, counted heap tokens, objects that are not boxes (a direct value's data word is
the pointer itself, whose pointee need not have the asserted type), or boxes whose type mentions a type
parameter. Where the analysis cannot tell, the token stays. That is what keeps the rule sound: an entry
is dropped only for a box whose dynamic type the program's own assertion or dispatch rejects at run time.

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

**Cache** (internal/escape2/cache). Format version 5. A file carries its package's filter table, each
entry as the kind and the type's canonical string (as for the contract table), and keys refer to it by
index. Loading re-interns the predicates. A predicate whose type no longer resolves makes the entry fail
its check, so the summary is solved again. A cached summary is checked, never trusted (D44), so a wrong
table costs a solve, not soundness.

## How the solvers apply it

**Making filters (both solvers).** At `OpAssert` to an interface or concrete type that mentions no type
parameter, an `In` token with no filter gets the filter (implements I or is T) at its current path length;
one that has a filter keeps its own. Keeping either filter is sound, and one slot keeps the format small.
At a `CallIface` whose receiver holds `In` tokens, the summary of candidate `m` of type `T` is applied
with the receiver's `In` tokens given the filter "is T" (or `*T`). `Obj` tokens are narrowed at once, as
`BoxAsserts` narrows them today.

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
filters. Combining two entries that differ only in their filter gives the unfiltered entry. The per-input
store cut (`MaxRawStorePerInput`) drops filters first.

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
8. A direct value (a pointer to a frame object converted to `any`) asserted to an interface its pointee
   type does not implement: still rejected. Direct values are never filtered.
9. A call through an interface whose only keeping candidate is `(*A).String`: a box of `B` (another
   Stringer that keeps nothing) accepted, a box of `A` rejected.
10. Cache: a summary with filtered keys round-trips through version 5, and one whose filter table names a
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
the analysis cannot tell (a direct value, a type parameter) is followed as before.

## Measurement plan

After D's deferral fix lands: rerun the class table (interface-box findings by object type and by the
function whose own code makes the escape). Build this if about 40 or more remain. Report pluto's findings
and time, corpus agree/differ, and E3010, before and after.
