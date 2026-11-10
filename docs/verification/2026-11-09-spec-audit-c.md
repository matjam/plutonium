# Spec audit C: 8.1 to 8.5, 10.1 to 10.5, chapter 11, 12.12, 12.13, appendices E and G

Source: /home/matjam/src/plutonium/.claude/worktrees/coord (origin/main), docs/spec/language/.
Read-only. Each finding was checked with grep across the whole language spec. Findings A1 to A8 of
audit A are not repeated.

Counts: high 1, medium 4, low 9.

---

## C1. An owner can be made outside its package by converting from a look-alike owner

- Kind: safety hole (contradiction between rule 1 and rule 3 of 11.24).
- Severity: high.
- Changes a promise: yes. It narrows rule 3 as written ("converting between two owner types with the
  same underlying type moves"). It keeps the promise rule 1 makes. The user decides.

Sections and quotes:

- 11.24 rule 1: converting a value to an owner type "is allowed only in the package that declares the
  owner type, where the invariants of the type are kept. In any other package it is an error (E3212)."
- 11.24 rule 3: "Converting an owner to another owner type with the same underlying type moves it." No
  package limit. 7.18, 8.11 and 18.5 repeat rule 3 without one.
- 11.24, non-constant conversions: "`x`'s type and `T` are pointer types that are not named types, and
  their pointer base types ... have identical underlying types." No owner limit.
- 11.24 and 7.13: "a value of another union type with identical variants converts explicitly." No owner
  limit.
- 7.18 example: `type Frame owner uintptr // a physical page frame, returned to the page allocator on Drop`.

Example (all safe code):

```go
package q

type Fake owner uintptr        // q declares Fake, so rule 1 allows Fake(n) here

func forge() p.Frame {
	return p.Frame(Fake(0x1000)) // rule 3: owner to owner, same underlying type
}                                // dropping the result frees frame 0x1000 through p's Drop

func forge2() p.Frame {
	var f Fake = Fake(0x2000)
	return take((*p.Frame)(&f))  // pointer conversion, then take moves a p.Frame out
}
```

The pointer form also lets code in any package run one type's `Drop` on another type's resource:
`*(*p.Frame)(&f) = p.Frame{}` drops the old value as a `p.Frame`. The forgery works for every owner whose
underlying type can be written outside its package: integers, `uintptr`, pointers, `unsafe.Pointer`,
arrays of these, structs with only exported fields, and owner unions over such payloads. These are the
owner types a kernel uses most (frames, handles, descriptors).

A related case: a composite literal of an owner struct with exported fields (`p.Region{Base: b, Len: n}`)
makes a new owner outside `p`. 11.3 restricts only unkeyed literals with unexported fields. This one is
the declaring package's choice, since it exported the fields, but the spec should say so.

Proposed resolution: give rule 3, the pointer rule and the union rule the same package limit as rule 1.
A conversion whose result is an owner type `T` (or a pointer to one, or an owner union) is allowed only
when the operand's type is also `T`'s package's type, or the conversion is written in `T`'s package,
or the operand's type is defined from `T` (`type B owner T`). Otherwise E3212. Add one sentence to 7.18
saying that exported fields of an owner struct make its literal public. This follows the goal of
Rust-level safety without annotations: the rule needs no new syntax, and the declaring package keeps its
invariants.

---

## C2. A type switch binding on an owning interface moves the owner into an ordinary interface

- Kind: hole (contradiction between 12.9 and 7.9).
- Severity: medium.
- Changes a promise: yes, if the "every clause" sentence of 12.9 is narrowed. The user decides.

Sections and quotes:

- 12.9: "`switch v := x.(type)` moves the value out of `x` into `v` in every clause, the default clause
  included. The variable `v` is dropped at the end of its clause unless the clause moves it on".
- 7.9: an ordinary interface "never owns what it holds". Converting an owner to it "lends it: the
  interface is a view of the variable that was converted".
- 11.13 and 7.9 define assertions from an owning interface only "to an owner type". An assertion from an
  owning interface to an ordinary interface type (`r.(io.Writer)`) is not described. 11.13 also advises
  "Assert it to an interface type", but an owning interface is an owner type (7.9), so asserting an
  ordinary interface to one is E3308 too.

Example:

```go
type Resource owner interface{ Read(p []byte) (int, error) }

func f(r Resource) {
	switch v := r.(type) {
	case io.Writer:     // v has type io.Writer, an ordinary interface
		v.Write(nil)    // v lends from which variable? r was moved and cleared
	}                   // dropping v does nothing, so the owner is never dropped
}
```

Either the owner leaks, or `v` is a view of a cleared `r`. Two readings give two programs.

Proposed resolution: in a clause whose type is an ordinary interface, nothing moves. `v` lends from `x`,
as a conversion to an ordinary interface does, and `x` keeps the value. After the switch `x` counts as
possibly moved (E3302) if any other clause moved it. State the same for the assertion `x.(I)` with `I`
ordinary: it lends and does not move. In 11.13, change "assert it to an interface type" to "assert it
to an ordinary interface type". This keeps the Go shape of the code and adds no annotation.

---

## C3. "Moved only where `ok` is true" needs flow tracking of a boolean, with no stated limit

- Kind: hole.
- Severity: medium.
- Changes a promise: no.

Sections and quotes:

- 11.13: "In the comma-ok form `v, ok := x.(T)`, `x` is moved only where `ok` is true, and it is still
  usable on the path where `ok` is false." The same for owner unions: `f, ok := s.(Slot.Open)`.
- 7.9 and 18.5 repeat it. 18.5 defines E3301 and E3302 by paths. Nothing says how the compiler knows
  which paths have `ok` true.

Example:

```go
v, ok := x.(Conn)
done := !ok
if done { use(x) }      // is x usable here?
if ok && ready() { ... } else { use(x) }   // else covers ok true and ready() false
ok = false              // ok reassigned: what does x hold now?
```

Proposed resolution: say that the move happens at the assertion when `ok` is true, and that the compiler
follows only the direct tests `if ok`, `if !ok`, `if ok && ...` (then-branch) and `if !ok || ...`
(then-branch) on the same `ok`, with no assignment to `ok` in between. On every other path `x` counts
as possibly moved (E3302). The error should suggest `if !ok { ... }` right after the assertion. This
keeps the check local and predictable, which the goal "no implementation knowledge for ordinary code"
needs.

---

## C4. Whether an enum is an "integer type" decides which enum conversions compile

- Kind: contradiction.
- Severity: medium.
- Changes a promise: no.

Sections and quotes:

- 11.24 bullets: "`x`'s type and `T` are both integer or floating-point types." and "`x` is an integer
  or an enum value, and `T` is an enum type (below)."
- 11.24, Enums: "For a non-constant integer `n`, `E(n)` is valid only in the comma-ok form ... (E2314 for
  the form without `ok`)" and "A conversion from one enum type to another goes through the integer type
  ...: `v, ok := Shade(uint8(color))`."
- 7.12: "An enum is a defined type whose underlying type is an integer type" and "An enum value is an
  integer everywhere else". 11.11 says "an enum value is an integer for this purpose", which suggests it
  is not one in general.

Example:

```go
var n uint8 = 7
c := Color(n)        // E2314 by the Enums paragraph; allowed by "both integer types" if Color is one
s := Shade(color)    // allowed by "x is an enum value, and T is an enum type"; not by "goes through the integer type"
v, ok := Color(9)    // 9 is a constant but not a member: error, or ok == false?
```

Proposed resolution: say in 7.12 that an enum type is not an integer type for the rules of 11.24, and
change the bullet to "`x` is an integer value, and `T` is an enum type (comma-ok form only, below)".
Say that a non-member constant in the comma-ok form is an error, as in the single-value form. This
keeps every unchecked path to a non-member value out of safe code.

---

## C5. A type defined from a union or packed type has no stated meaning

- Kind: hole.
- Severity: medium.
- Changes a promise: no.

Sections and quotes:

- 8.2: for "a packed type or a union type, the corresponding underlying type is `T` itself". So in
  `type V U`, the underlying type of `V` is `U`, a defined type.
- 7.12 decides the enum case: "`type F E` declares an ordinary integer type with `E`'s underlying type
  and no members."
- 7.13, 7.14 and 10.8 say nothing for unions or packed types.

Example:

```go
type Shape union { Circle float64; Square float64 }
type Shape2 Shape
s := Shape2.Circle(1)      // does Shape2 have the variants?
switch s.(type) { ... }    // is this a union switch, and must it be exhaustive?
t := Shape(s)              // identical underlying types, so a conversion?
type R2 ControlReg         // does R2 keep the fields and layout of the packed ControlReg?
```

Proposed resolution: say in 7.13 and 7.14 that `type V U` makes a new union with the same variants
(and a new packed type with the same fields and layout), with none of `U`'s methods, and that `V` and
`U` convert explicitly. That matches how Go treats `type B A` for a struct, so a Go reader guesses it.

---

## C6. The underlying type of `owned[T]`, `volatile[T]` and the other constructors is not defined

- Kind: hole.
- Severity: low.
- Changes a promise: no.

Sections and quotes:

- 8.2: the underlying type is `T` itself for predeclared basic types, type literals, packed and union
  types. "Otherwise, `T`'s underlying type is the underlying type of the type to which `T` refers in its
  declaration." The constructors have no declaration.
- 10.3: the constructors "are not types on their own". E.7: they "are not separate productions".
- 7.18 allows `type B owned[T]`; 12.12 requires "the box type must not be a named type"; conversions
  and `~` terms depend on underlying types.

Proposed resolution: add to 8.2 that each instance of a type constructor (`owned[T]`, `shared[T]`,
`synced[T]`, `weak[P]`, `volatile[T]`, `aligned[T, N]`, `ring[T, N]`) is its own underlying type.

---

## C7. The lifetime of a composite literal temporary is stated two ways

- Kind: contradiction (wording).
- Severity: low.
- Changes a promise: no.

Sections and quotes:

- 11 intro: a temporary is "the result of a call, a conversion, a concatenation, a composite literal or a
  receive. A temporary lives until the end of the statement that makes it".
- 11.3: "The array of a slice literal is a temporary variable of the frame ... The slice may be used
  within the function". 18.8 (the release list) agrees with 11.3: "a slice literal's array ... when the
  function returns".
- 11.15: the variadic array "is a temporary in the frame of the caller". 18.2 lists it as frame storage,
  but the 18.8 release list (interface copy, `&T{...}` object, slice literal array, method value
  receiver, deferred arguments, closure region) omits it, so the release time of counted elements in it
  is not stated.

An implementer who follows the chapter 11 intro releases the strings of `ss := []string{a, b}` at the
end of the statement, while `ss` still views them.

Proposed resolution: in the chapter 11 intro, say that the storage of a slice literal, an `&T{...}`
object and a variadic array is frame storage with the lifetime of 18.8, and only the value itself is a
temporary. Add the variadic array to the 18.8 list.

---

## C8. Slicing a type parameter whose type set has strings: dead text

- Kind: mistake (text kept from Go).
- Severity: low.
- Changes a promise: no.

11.12: "If the type set contains string types it may also contain byte slices ... In this case, the
slice expression must be valid for an operand of string type." The same section makes every slice of a
string an error (E2019). So the case is always an error, but the text reads as if it were allowed.

Proposed resolution: replace it with "If the type set contains a string type, the slice expression is an
error (E2019)."

---

## C9. "Unions cannot appear in constraints" reads as a ban on type-set unions

- Kind: wording.
- Severity: low.
- Changes a promise: no.

11.16: "A union type is a valid type argument, and unions cannot appear in constraints." The next
examples use `~int | ~float64`. 7.13 says it clearly: "unions cannot appear in constraints, where
`A | B` keeps its meaning as a union of type terms".

Proposed resolution: write "union types (section 7.13) cannot appear as terms of a constraint".

---

## C10. Packed types: 11.24 lets a byte array convert in one direction only

- Kind: contradiction.
- Severity: low.
- Changes a promise: no.

- 11.24: "A packed type converts to and from an integer type of its size and from a byte array of its
  length".
- 7.14: "A packed type converts to and from an integer type of the same size, or a byte array of the
  same length ... Every bit is kept in both directions". 7.13 says the same for packed unions.

Proposed resolution: change 11.24 to "to and from an integer type of its size or a byte array of its
length".

---

## C11. A typed integer constant converted to a string

- Kind: contradiction.
- Severity: low.
- Changes a promise: no.

- 11.24, constant conversions: a constant converts "if `x` is an integer or rune constant and `T` is a
  string type". This includes typed constants.
- 11.24, conversions to a string: "A value of type `rune`, or an untyped integer or rune constant,
  converts ... A value of any other integer type is an error". Appendix A.2 agrees.

Example: `const n int = 65; s := string(n)`. The first rule allows it, the second rejects it.

Proposed resolution: change the constant rule to "an untyped integer or rune constant, or a constant of
type `rune`". Go accepts this line, so the error is a removal under A.2, which already covers it.

---

## C12. The conversion bullet for owners has no condition on the underlying type

- Kind: wording.
- Severity: low.
- Changes a promise: no.

11.24 bullets: "`x` is a value, and `T` is an owner type, or `x` is an owner and `T` is not (below)."
Rule 1 below adds only the package limit. Read literally, `Frame("abc")` and `Frame(1.5)` are allowed
in `Frame`'s package.

Proposed resolution: add "and the conversion would be valid between the underlying types".

---

## C13. Appendix E: two parses of `f[int]`

- Kind: grammar ambiguity.
- Severity: low.
- Changes a promise: no.

E.4 (from 11.1 and 11.6): `Operand = Literal | OperandName [ TypeArgs ] | ...` and `PrimaryExpr = ... |
PrimaryExpr TypeArgs | ...`. A generic name with type arguments matches both. Go has only the first
form. Every appendix E production matches its chapter text word for word, so this is a chapter issue.

Proposed resolution: drop `[ TypeArgs ]` from `Operand`, or say which form `pkg.F[int]` and
`x.Method[int]` use.

---

## C14. `task` with a variadic call: "Packed variadic arguments" is unclear

- Kind: wording.
- Severity: low.
- Changes a promise: no.

12.12: "Packed variadic arguments are copied into the new task, so each one crosses as a value." 11.15
calls the array a temporary of the caller's frame, and line 802 forbids passing a view of the starting
frame. The word "packed" also names `packed` types (7.14). A reader can take the sentence to be about
packed types.

Proposed resolution: write "The array that holds the variadic arguments of the call (section 11.15) is
copied into the new task, so the slice the function receives is not a view of the starting frame."

---

## Checked and dropped (decided elsewhere)

- `x.f = consume(x)`: 18.5 "An expression or statement that moves a variable and uses the same variable
  anywhere else is a compile error" covers the left side.
- Consuming methods "under section 10.14": 10.14 defines them (line 883).
- Enum as shift count or index: 7.12 and 11.18 allow it.
- Struct tags: 7.6 and the `Tag` production define them.
- `type F E` for an enum: 7.12 decides it.
- Order of evaluation in send, `task`, `defer` and `select`: 12.5, 12.12, 12.19 and 12.13 point to 11.26.
- Enum member or union variant with the same name as a method: 7.12, 7.13 and 11.7 give the member
  precedence.
- `task (f)(x)` and `task` before `(`: 12.12 decides it.
- `f(s...)` with a slice of owners: passes a view, not owners, so E3310 does not apply.
- 12.13 select with owners in send cases: consistent with 18.5 (E3302 after the statement).
- 11.18 to 11.23 against Go (overflow, shifts by the width, division, FMA, float to integer): same as Go.
  No false friend found.
- Appendix A: no missing false friend in my sections. The string conversions are in A.2, the order of
  evaluation is in A.3.
- Appendix E against all chapter productions (checked with a script that compares each production to
  its chapter): identical. Contextual keywords match 4.6.
- Appendix G: consistent with the "Adapted in part" footers. No problem found.
- 10.1 to 10.5 and 10.3 against appendix D: consistent.
- 8.1, 8.3 to 8.5: no new finding.

## Sections not read closely

- Chapter 8 after 8.6 (8.6 only at its start; 8.11 read).
- Chapter 10 after 10.5, except the passages grep found in 10.8 and 10.14.
- Chapter 12 apart from 12.12, 12.13 and the parts of 12.7 and 12.9 cited above.
- Chapter 7: only 7.9 (owning interfaces), 7.12, 7.13 (conversions), 7.14 (conversions) and 7.18.
- Chapter 18: 18.2, 18.3 start, 18.5, and the release list of 18.8. Not 18.7, 18.9 to 18.12.
- Chapter 19: only by grep (E2010, E3501).
- 11.5 (closures and regions) was read once, without checking each rule against 18.3.
- Chapter 5 (constant precision) was not checked against 11.25.
