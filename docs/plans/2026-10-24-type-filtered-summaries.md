# Type-filtered summary entries

Status: built, 2026-10-24. Decision D51 (owner, via the coordinator): go, extended to direct pointers as
well as boxes (lib/fmtappend: the join of the `Append` candidates applies `(*Vec).Append` to a frame
`vec.Fixed`). Section 18.3 defines what the analysis accepts; the sentence below is now part of it. The
rules the solvers implement are in internal/escape2/solve/rules.go, "Type filters". This document records
the design and what changed from the first plan while building it.

## The problem

The new analysis (internal/escape2) narrowed a type assertion only for what the function itself makes. An
`OpAssert` to an interface drops the `Obj` tokens of boxes whose dynamic type does not implement it
(`solve.BoxAsserts`), and an assertion to a pointer-shaped concrete type drops every box. An input's `In`
token carried no type, so the narrowing was lost at the summary. When a callee asserts its parameter and
keeps only what passes the assertion, its caller saw the parameter as kept whatever it passes:

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

The larger case is pluto/fmt's walk (eir/fmtwalk.go): it calls every String and Error method of the
program on what it is given, through one join of all of them (D47), and each candidate's effects landed on
the boxes and frame objects of every type, not only on those of the candidate's receiver type. One method
that keeps something of its receiver (types2's `typeWriter.typ` stores the type it prints in a heap map)
made every value printed anywhere escape.

## The rule

A summary entry may carry a **filter**: a union of type predicates and the point on the key's path where
it applies. The entry then stands only for the pointers found through a pointer, at that point, that the
filter admits. Three predicates (domain.Atom):

- **implements I**, for an assertion to an interface `I` that mentions no type parameter;
- **is T**, for an assertion to a concrete type `T` other than a pointer, for an interface adapter of a
  value of `T`, for a method of pointer receiver `*T`, and, at a call through an interface, for a method
  of value receiver `T` (a candidate that stands for its adapter: the receiver is the data word);
- **pointer to an instance of G**, for a method of a generic type's pointer receiver.

Where filters are made: at `OpAssert`; at the receiver of each candidate of a `CallIface`; and at the
receiver keys of each member of a join of a dynamic call's candidates. A call through an interface has
virtual functions of its own (a set of candidates of a `CallIface` is not shared with a call on a type
parameter), because only there is a value method's receiver the data word: a call on a type parameter
passes the struct value itself, whose pointers say nothing of its type.

An assertion to a pointer type makes no filter. The compiler's own type switches over pointer types gave
each case's value a filtered form of every input it reached; on the translated compiler that cost half
again the solve steps and removed no finding.

**What a filter removes.** Only `Obj` tokens, and of those only a pointer, tagged `Root` (the filter's
place holds one pointer, the data word), that is exact and either at the root of a typed variable,
parameter slot, temporary, slice literal or variadic array, or anywhere in a box, of a type that mentions
no type parameter. The pointer points at a value of type `U` (the type at its path):

- "is `*V`" admits it only when `V` has `U`'s underlying type: a `*U` converts to a `*V` then
  (review2/k06);
- "is `T`" for a `T` that is not direct admits it only when `U` is or holds a `T`. The data word of a `T`
  points at a `T`, which may be a field of the value: pluto/fmt's walk calls a field's methods with the
  field's address, and its contract calls them on what holds it. A box's value has its own type; any
  other object may be viewed as a `T` of its underlying type, so for one of those a value of `T`'s
  underlying type is enough;
- "is `T`" for a direct `T` admits every pointer (a direct value of a type other than a pointer may point
  at any object), and "implements I" admits every pointer but the root of a box of a type that does not
  implement I;
- a pointer into a box at a place that is no value of its own (inside a string, a slice header) is
  admitted only by "implements". A key's field step applies to a value's pointers as well as to the value
  itself, so this is what a caller finds where a callee's key names a field of an interface value's data
  word.

A pointer inside an object other than a box is never removed: code that reinterprets an object's memory
with `unsafe.Pointer` may make a `*V` of a pointer inside it (review2/m20, m22, m24). Unsafe code that
reinterprets the root of a variable as another type, and passes it where a filter applies, is outside
the rule, as section 18.12 leaves unsafe code outside the frame rules.

Heap tokens, counted heap tokens, `In` tokens other than exact ones (which take the filter instead),
objects of other kinds (closure records, regions), and anything whose type mentions a type parameter are
never removed. That is what keeps the rule sound: an entry is dropped only for a value whose dynamic type
the program's own assertion or dispatch rejects at run time.

## Format

**Keys** (domain.Key). Bits 46 to 59 hold a filter ID (0 for none; at most 16,383 filters, past which an
entry gets none, which only widens it), and bits 60 to 63 the filter's position: the number of path steps
before the filtered pointer. `Covers` gains one condition: `a` covers `b` when it covers `b`'s path and
flags as before and `a` has no filter, or `b`'s filter is at the same position and its atoms are among
`a`'s. Store pairs filter their source key only; a destination keeps none.

**The table** (domain.Filters) belongs to the analysis's paths (`Paths.Filters`): one per run. Filters
are interned as solves meet them, and parallel solves and joins meet them in an order their timing decides,
which changes no set a summary stands for, only the IDs. When the analysis is over, `sched` renumbers the
filters by their atoms' texts and renames every summary's keys and the entries findings name
(`canonicalFilters`), so the results do not depend on the number of workers or their timing.

**Tokens.** The solver's `In` token carries the ID and position in word 0 (bits 40 to 57); the reference
solver's `tok` gains two fields.

**Cache** (internal/escape2/cache). Format version 7. A filtered key's encoding is followed by its position
and its atoms' texts (kind and type, packages by path). Loading resolves each text among the atoms the
program's assertions and candidates make; a text that does not resolve, or that two types share, loads the
key without its filter, which only widens it. A cached summary is checked, never trusted (D44).

## How the solvers apply it

**Moving filters.** Extending a filtered token's path keeps the filter and its position. Cutting a path to
a prefix shorter than the position drops it (GE cuts, the Deep load cut, `anyOffset`, `summary.CutKey`).
A GE or Deep `In` token with a filter covers no other token in a value (GE subsumption).

**Applying a filtered entry.** The key's path is followed from the argument up to the position, the value
found goes through the filter, and the rest of the path and the flags follow.

**Widening.** `summary.Widen` gives the entries that differ only in their filter one entry with the union
of their filters (a join of 95 candidates' entries on one receiver is then one entry). Past a cap, the
cuts keep filters where the cut path still reaches their position, and merge again; only a third pass
drops them. Dropping filters first, as first planned, left the translated compiler's joins unfiltered
(89 findings instead of 19).

**Joins.** Each member's entries keyed on the receiver input take the member's filter at position 0: Esc,
Store sources, Res, and Deferred entries' function values and arguments.

## Tests

internal/escape2/filter_test.go: assertions to an interface (a string box passed where only Namers are
kept: accepted; a frame value that is a Namer, a pointer to a frame Namer: rejected), through a middle
function and through a struct field; calls through an interface (a value whose String keeps nothing:
accepted; a pointer whose String keeps it, a value whose String keeps what it points to: rejected). The
review suites (review2's k and m programs) are the oracle for conversions and unsafe reinterpretation. The
differential generator (refsolve/gen.go) gains objects and boxes of named types, assertions to them,
adapters and methods of them as candidates, and filtered callee keys, from a random source of its own so
every seed's case is otherwise as it was; 30,000 cases agree, and the canonical form compares filters by
their atoms' texts. Cache: TestRoundTrip round-trips the filtered keys.

## The text of section 18.3

Added to "How precise the frame check is", after the bullet on calls through an interface:

> - A value converted to an interface and then asserted, by a type assertion or a type switch case, to an
>   interface or to a type other than a pointer is followed after the assertion only for the dynamic types
>   the assertion admits; so is the receiver of a method called through an interface, for the type whose
>   method is called. A function that keeps what passes an assertion does not keep a caller's value of a
>   type the assertion rejects.

## Measurements

The translated compiler (selfhost/cmd/pluto, deferral on): 89 findings before, 19 after; solve steps
0.77 G before, 1.23 G after. The build with the translated escape analysis (selfhost/demo/escape2demo): 85
findings before, 49 after. The three corpus programs it was built for (examples/24-errors,
lib/fmtstringerkeeps, lib/fmtappend) now agree with the old analysis.
