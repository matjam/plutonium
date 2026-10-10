# Enums

**Date:** 2026-10-09
**Status:** implemented; specified in language spec section 5.9.

## 1. The problem

Go writes enumerations as a defined integer type and a block of `iota` constants:

```go
type Color uint8

const (
	Red Color = iota
	Green
	Blue
)
```

That works, and Plutonium keeps it, but it gives the compiler nothing to check:

- `Color(42)` and `var c Color = 7` are valid, so a `Color` can hold a value that names nothing.
- A `switch` on a `Color` that forgets `Blue` compiles.
- `Red + Blue` compiles.
- The constants sit in the package namespace, so two enumerations in one package cannot both have a
  member called `None`, and names grow prefixes (`ColorRed`) to avoid clashes.
- Printing a member's name needs a hand-written or generated `String` method, and there is no way to
  iterate the members or count them.

An enum is a closed set of named integer constants of its own type. It is the payload-free sibling of the
`union` type accepted in the [sum types proposal](2026-10-09-sum-types-proposal.md) (option A): members
are named once in the declaration, selected through the type's name (`Color.Red`, like `DeviceState.Ready`),
switched over with unqualified case names, and checked for exhaustiveness. A later `union` reuses the same
selector, case-name and exhaustiveness code.

## 2. Syntax

```
EnumType   = "enum" [ Type ] "{" { EnumMember ";" } "}" .
EnumMember = identifier [ "=" Expression ] .
```

```go
type Color enum {
	Red
	Green
	Blue
}

type Errno enum uint16 { // explicit underlying integer type
	OK     = 0
	EPERM               // 1: one more than the previous member
	ENOENT              // 2
	EIO    = 5
}

type Order enum { // a negative member makes the default type signed: int8
	Less = -1
	Equal
	Greater
}
```

`enum` becomes a keyword. Nothing in `lib/`, `examples/`, `testdata/` or `bench/` used it as an
identifier.

An enum type may only appear as the type of a type definition (`type E enum {...}`): its members are
reached through the type's name, so an unnamed enum would have unreachable members. It may not be the
right-hand side of an alias, and an enum type cannot have type parameters.

## 3. Semantics

### 3.1 The type

An enum is a defined type whose underlying type is an integer type:

- the type written after `enum`, which must be an integer type (its underlying type is used, so
  `enum MyInt` works); or
- by default, the smallest of `uint8`, `uint16`, `uint32`, `uint64` that holds every member, or the
  smallest of `int8` to `int64` if any member is negative.

Because the underlying type is an integer, an enum value is a plain integer everywhere below the type
checker: it has the integer's size and alignment, lives in one register, is stored directly in an
interface's data word, and needs no new code in the SSA, optimiser or backend.

### 3.2 Members

- The first member is 0 unless it has a value; every other member without a value is one more than the
  previous member, like `iota`.
- A value is a constant expression of integer value (untyped, or of any integer or enum type),
  evaluated in the scope that encloses the type declaration. It cannot refer to the enum being declared.
- Every value must be representable in the underlying type. Continuing past the largest value is an
  error.
- Two members may not have the same value, and member names must be distinct and not `_`.
- **One member must have the value 0.** It is the enum's zero value. See section 6 for why.

### 3.3 Names are scoped to the type

A member is written `Color.Red`, a typed constant of type `Color`. The type may be qualified by a package
(`gfx.Color.Red`) or reached through an alias. Members follow Go's export rule: `Color.red` is visible only
in its own package. Members do not enter any scope, so two enums in one package can each have a `None`.

On a type name, a member takes precedence over a method of the same name: `Color.Red` is the member even
if `Color` has a method `Red`. On a value, `c.Red` is the method, as usual. The two only meet in method
expressions, which are rare for enums; the alternative (forbidding methods named like members) would stop
an enum with a member `String` from having a `String` method at all.

### 3.4 Bare member names in switches

In an expression switch whose tag has enum type `E`, a case expression that is a single identifier naming
a member of `E` denotes that member:

```go
switch c {
case Red, Green:
	warm()
case Blue:
	cool()
}
```

The member wins over any variable or constant of the same name in scope. The switch context says which
type is meant, and a declaration added elsewhere later cannot change what an existing case means or make
an exhaustive switch silently non-exhaustive. Only a lone identifier is treated this way, so a variable
that happens to share a member's name is still reachable as `case (x):`.

Bare names are **not** accepted anywhere else (assignment to an `E` variable, an `E` argument, a return,
`c == Red`). Recognising them there would make an identifier's meaning depend on the type the context
expects, which Go's checker does not compute, and every rule for it (member first, or scope first) has a
case where adding an unrelated declaration changes the meaning of existing code. `c = Red` therefore
reports `undefined: Red (did you mean Color.Red?)`.

The union proposal recommended that bare variant names apply "only when the name is not shadowed". This
design picks "member wins" instead, for the reason above, and recommends that `union` follow it so that
both closed-set types read the same way.

### 3.5 Exhaustive switches

An expression switch whose tag has enum type and which has no `default` case must list every member
among its constant case values, or it is a compile error that names the missing members:

```
missing case in switch on c (Color): Blue (add it or a default case)
```

A switch in another package cannot name unexported members, so it needs a `default`. A tag-less
`switch { case c == Red: }` is not checked.

### 3.6 Operations

- `==`, `!=`, `<`, `<=`, `>`, `>=` compare values (ordering is by value).
- Arithmetic, bitwise operators, `++`, `--` and shifting an enum value are errors: none of them is
  guaranteed to produce a member. Convert to the integer type first.
- An enum value may be a shift count and may index an array, slice or string, like any integer.
- Untyped constants do not convert implicitly to an enum: `var c Color = 1` and `c == 1` are errors.

### 3.7 Conversions

- **Integer constant to enum** (`Color(2)`): the value must be a member's value; the result is that
  member, a constant. Anything else is a compile error.
- **Non-constant integer to enum**: only the comma-ok form, in an assignment, short variable declaration
  or variable declaration with two names:

  ```go
  c, ok := Color(n)   // ok reports whether n is a member's value; if not, c is the zero member
  ```

  The single-value form `Color(n)` is a compile error that suggests the comma-ok form. The argument must
  have an integer (or enum) type; floats and packed values are rejected.
- **Enum to integer** (`uint8(c)`, `int(c)`): an ordinary conversion.
- **Enum to the same enum**: allowed, a no-op.

### 3.8 What the compiler generates

| Form | Meaning |
|---|---|
| `c.String()` | the member's name; `"Color(?)"` for a value that is not a member (section 6) |
| `len(Color)` | the number of members, an untyped integer constant |
| `for c := range Color` | the members in declaration order |

`String` is a method with a value receiver, so every enum satisfies `fmt.Stringer`. If the type declares
its own `String` method, that one is used and nothing is generated. The method is generated only if the
program reaches it (lowering instantiates functions on demand), so an enum that is never printed costs
nothing.

For a dense enum (values 0 to `len(E)-1`) an array of length `len(E)` is a table indexed directly by the
enum:

```go
var names = [len(Color)]string{Color.Red: "red", Color.Green: "green", Color.Blue: "blue"}
fmt.Println(names[c])
```

## 4. Interactions

- **Interfaces.** An enum value is one word in the interface data word, with a type descriptor of its
  integer kind and the enum's name. Type switches and assertions see the enum type.
- **fmt.** `%v` and `%s` use `String`; `%d` and `%x` print the integer.
- **Generics.** An enum satisfies constraints by its underlying type (`interface{ ~uint8 }`,
  `comparable`) and works as a type argument. Inside generic code the value is a value of the type
  parameter, so the constraint's operations apply: arithmetic or `T(n)` on a `T ~uint8` instantiated with
  an enum is not checked and can produce a non-member. An enum type cannot itself have type parameters.
- **Packed types.** A packed field of enum type takes the bits its values need, not the size of the
  underlying type: `mode Mode` with members 0 to 5 is 3 bits wide, unsigned; a signed enum takes the two's
  complement width of its range. `mode bits[Mode, 4]` gives an explicit width, which must be at least the
  width needed. Reads give the enum type; writes store its low bits.
- **volatile, aligned.** `volatile[Color]` and `aligned[Color, N]` work as for any integer type.
- **Directives.** None are needed. `//pu:` directives apply to functions and variables; the generated
  `String` is emitted only when used.
- **C.** An enum passes through `//pu:extern` and `//pu:export` as its underlying integer, which matches
  C when the underlying type matches the C enum's (usually `int32` or `uint32`).

## 5. Flag sets

Enums do not support bit flags (`Read | Write`). A flag set is a set of members, not a member, so it would
need a second type anyway, and packed `bits` groups already give named one-bit flags with exact layout:

```go
type Perm packed uint8 {
	flags bits { Read Write Exec }
}
```

`1 << c` with an enum shift count builds a mask from an enum when one is needed.

## 6. Decisions and alternatives

**Zero member required.** The user's draft allowed `Errno enum { EPERM = 1; ... }` with no zero member.
Every Plutonium type has a zero value, and `var e Errno` would then hold 0, which names nothing: the zero
value of every struct field, array element and fresh allocation of that type would be outside the type,
and exhaustive switches would have a hole that every program reaches without `unsafe`. Requiring a zero
member keeps the invariant "every value of an enum type is a member" true except through `unsafe`, packed
values converted from raw bits, and generic code (section 4). It mirrors the union rule that the zero
value is the first variant. Hardware encodings that leave 0 reserved name it (`Reserved = 0`).

**Non-constant conversion: comma-ok, not panic.** A conversion that can panic would be the only Go
conversion with a hidden runtime check, and kernel code converting device registers wants to handle bad
values, not stop. The comma-ok form follows `v, ok := x.(T)` and needs no new names. The draft's
`Color.Parse(n)` was rejected because it shares the member namespace (an enum could not have a member
named `Parse`) and because `Parse` means string parsing in Go.

**`len(E)` and `range E`, not `E.Count` and `E.Values()`.** Both alternatives take names from the member
namespace. `len` and `range` are the forms Go already uses for "how many" and "each of them", and Go 1.22's
`for i := range 10` is the precedent for ranging over something that is not a container.

**Invalid values.** Values outside the enum can still arise through `unsafe`, raw packed bits and generic
code. A switch with no matching case does nothing, as in Go; `String` returns `"Color(?)"` (it cannot
format the number without allocating). Making them panic was rejected: it would add a check to every
exhaustive switch to guard against code that already opted out of checking.

**`type C2 Color`** declares a plain integer type with `Color`'s underlying type and no members, the same
way a defined type does not inherit methods. Members belong to the enum type, not to its underlying
type.

**Representation in the compiler.** The checker records the members on the defined type
(`types2.Named.Enum()`) and gives the type the integer as its underlying type, rather than adding an
`Enum` type kind. Constants, conversions to integers, layout, interfaces, comparison and code generation
then work unchanged; the checker adds the restrictions of section 3.6 and 3.7 on top. `String`, the
comma-ok conversion and `range E` are built in lowering from the member list.

## 7. Candidates in the standard library

- `pluto/hash`: how a `Map` hashes its keys is now `type hashMethod enum { none; word; float; str;
  hasher; custom }`, and the switch in `Map.hash` is exhaustive, so a new method cannot be added without
  handling it.
- `pluto/sort`: the pdqsort pivot hint (`unknownHint`, `increasingHint`, `decreasingHint`) fits, but its
  uses are in `zsort*.p`, which mirror Go's generated pdqsort; left unchanged to keep them diffable.
- `syscall.Errno` is the obvious candidate (with `OK = 0`), but the errors work owns it; left unchanged.
- `rt.Kind*` could become an enum once `fmt` takes the kind as one; it is a public API shared with the
  compiler's descriptor numbering, so it is left for a separate change.
