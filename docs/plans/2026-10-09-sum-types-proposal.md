# Sum types: a proposal

**Date:** 2026-10-09
**Status:** proposal, for the user to decide. Nothing here is part of the language yet, and none of it is
in `docs/spec/language.md`.

## 1. The problem

Kernel and driver code is full of values that are exactly one of a few shapes: a device that is off,
probing, ready or failed; a system call that returned a value or an errno; a page-table entry that is
either a mapping or a swap slot. Plutonium has two ways to write these today, and both are poor:

- **An interface and a type switch.** The set of variants is open, so the compiler cannot say a switch
  missed one. Variants larger than a word are boxed in the converting function's frame (language spec
  section 4.4), so a function cannot return one. Every variant needs its own named type and a method to
  satisfy the interface.
- **A struct with a kind field.** Every variant's fields are present at once (wasting space, or overlaid
  by hand with `unsafe`), nothing stops code from reading the `irq` of a device that is `Off`, and nothing
  checks that a switch on the kind handles every value.

A sum type (tagged union) stores one variant inline, with a tag, and lets the compiler check that every
switch handles every variant. It needs no allocation and no boxing, so it can be returned, stored in
package variables and put in arrays like any struct.

### Goals

1. Read like Go. A Go programmer should guess what a declaration, a literal and a switch mean.
2. Values are stored inline: size is the tag plus the largest variant. No allocation, no frame boxing.
3. Switches over a sum type are checked for exhaustiveness.
4. Bit-exact variants for hardware formats, through `packed`.
5. Work with generics (`Result[T]`) without special cases.

### Non-goals

- Pattern matching beyond one level of variant selection. Nested destructuring can come later, if ever.
- Replacing `(T, error)`. Go's error idiom stays the default for library APIs.

## 2. Three syntax options

The same three examples are written in each option: a device state machine, a system call result, and an
x86-64 page-table entry.

### Option A: `union` with named variants (recommended)

A `union` type lists its variants like struct fields: a name, and a payload type. A name on its own has
an empty payload. Each variant name declares a type, `U.Name`, whose underlying type is the payload; a
value of a variant type converts implicitly to the union. A type switch selects the variant.

```go
type DeviceState union {
	Off
	Probing struct{ attempt int }
	Ready   struct {
		irq  uint8
		base uintptr
	}
	Failed syscall.Errno
}

var st DeviceState                       // zero value: Off, the first variant
st = DeviceState.Probing{attempt: 1}
st = DeviceState.Ready{irq: 11, base: 0xFEB0_0000}
st = DeviceState.Failed(syscall.EIO)

switch s := st.(type) {
case DeviceState.Off:
	start()
case DeviceState.Probing:
	retry(s.attempt)                     // s has type DeviceState.Probing
case DeviceState.Ready:
	enableIRQ(s.irq)
case DeviceState.Failed:
	log("device failed:", s)
}                                        // a missing case is a compile error
```

System call result, generic:

```go
type Result[T any] union {
	Ok  T
	Err syscall.Errno
}

func Read(fd int, p []byte) Result[int] {
	r := rawSyscall(sysRead, uintptr(fd), uintptr(unsafe.Pointer(unsafe.SliceData(p))), uintptr(len(p)))
	if r > ^uintptr(4095) {
		return Result[int].Err(syscall.Errno(-r))
	}
	return Result[int].Ok(int(r))
}

for {
	switch r := Read(fd, buf).(type) {
	case Result[int].Ok:
		consume(buf[:r])
		return nil
	case Result[int].Err:
		if r != syscall.EINTR {
			return r
		}
	}
}
```

`Result[int].Ok(5)` is a conversion to the variant type `Result[int].Ok`, whose underlying type is `int`;
`Result[int].Ok{...}` would be a composite literal when the payload is a struct. Inside a switch over a
union, the variant may be named without the union's name when that is unambiguous (`case Ok:`), which
keeps generic code short; whether to allow this is an open question (section 7).

Page-table entry, using the packed form of section 4:

```go
type PTE packed union uint64 {
	present bits[bool, 1]                // the tag: shared by every variant

	Swapped case false {
		slot bits[uint64, 63]
	}
	Mapped case true {
		writable bits[bool, 1]
		user     bits[bool, 1]
		_        bits[uint, 9]
		frame    bits[uint64, 40]
		_        bits[uint, 11]
		nx       bits[bool, 1]
	}
}

pte := PTE(table[i])                     // converting from the raw word never fails
switch e := pte.(type) {
case PTE.Mapped:
	return uintptr(e.frame) << 12
case PTE.Swapped:
	return swapIn(e.slot)
}
```

### Option B: a union of existing types

The variants are existing named types, listed with `|` as in Go's type-set constraints. The union stores
whichever one it holds inline; a type switch selects it.

```go
type Off struct{}
type Probing struct{ attempt int }
type Ready struct {
	irq  uint8
	base uintptr
}

type DeviceState union{ Off | Probing | Ready | syscall.Errno }

var st DeviceState = Ready{irq: 11, base: 0xFEB0_0000}

switch s := st.(type) {
case Off:
	start()
case Probing:
	retry(s.attempt)
case Ready:
	enableIRQ(s.irq)
case syscall.Errno:
	log("device failed:", s)
}
```

```go
type Result[T any] union{ T | syscall.Errno }

func Read(fd int, p []byte) Result[int] { ... return int(r) ... return syscall.Errno(-r) }
```

Variant types are ordinary types: they can have methods, be reused in several unions, and be passed around
on their own. The cost is that two variants cannot share a payload type (no `Read int` and `Write int`
without declaring two named types), every variant needs a separate declaration, and generic unions can
collide: `Result[syscall.Errno]` has the same type twice and must be rejected at instantiation. The
packed page-table entry needs the variant types declared separately, with the tag value attached to each
(`union packed uint64 { present: false => Swapped | true => Mapped }`), which reads less well than
option A.

### Option C: `enum` with constructors and patterns

Closest to Rust and Swift: variants are constructors with positional fields, and `switch` binds them with
patterns.

```go
type DeviceState enum {
	Off
	Probing(attempt int)
	Ready(irq uint8, base uintptr)
	Failed(err syscall.Errno)
}

st := DeviceState.Ready(11, 0xFEB0_0000)

switch st {
case .Off:
	start()
case .Probing(n):
	retry(n)
case .Ready(irq, _):
	enableIRQ(irq)
case .Failed(e):
	log("device failed:", e)
}
```

It is the most compact, but it adds a new keyword, constructor-call syntax, positional fields, a leading-dot
shorthand and binding patterns: five things a Go programmer has not seen. It also diverges from type
switches, which Go code already uses for the same job on interfaces.

## 3. Semantics (option A; B and C differ only in syntax)

- **Zero value.** The first variant, with its payload's zero value. Put the idle or empty state first.
- **Assignability.** A value of variant type `U.V` is assignable to `U`. Nothing else is: a `U` is not
  assignable to a variant type, and an untyped constant or composite literal needs the variant named.
- **Selection.** A type switch on a value of union type has one case per variant (several variants may
  share a case, `case U.A, U.B:`, and then the bound variable has type `U`). A type assertion `u.(U.V)`
  yields the payload or panics; `v, ok := u.(U.V)` does not panic.
- **Exhaustiveness.** A type switch over a union must name every variant or have a `default` case;
  otherwise it is a compile error listing the missing variants. Naming a variant twice is an error, as
  naming a type twice is in Go. There is no `nil` case: a union is never nil.
- **In-place update.** `p, ok := (&u).(*U.V)` yields a pointer into the payload of an addressable union
  when it holds `V`, so a state machine can update a field without copying the variant out and back.
  Changing the variant is an assignment of a new variant value.
- **Comparison.** A union is comparable if every payload type is. Two values are equal when they hold
  the same variant with equal payloads.
- **Methods.** A union type may have methods like any defined type. Variant types are declared by the
  union and cannot have methods of their own (declare a named payload type if a variant needs methods).

## 4. Memory layout

**Ordinary unions.** A union is laid out as

```
struct {
	tag     uintN       // N = 8 for up to 256 variants, else 16
	payload [size]byte  // size and alignment of the largest variant
}
```

with the payload at the first offset that satisfies the largest variant alignment. `DeviceState` above is
24 bytes (tag at 0, payload of 16 bytes at 8). Variants with empty payloads take no space beyond the tag.
The tag numbers the variants in declaration order from 0, which makes the zero value the first variant.

The compiler may later use a **niche** instead of a tag (a nil pointer for an empty variant, as Rust does
for `Option<&T>`), but only for unions without `packed`; the layout of ordinary unions is not part of the
ABI, and `unsafe.Offsetof` does not apply to variants.

**Calling convention.** For the internal ABI (abi.md section 3), a union of at most 32 bytes is passed and
returned as its words, each an integer-class leaf (float payloads travel as bits in general-purpose
registers, which is also what makes unions usable in kernel mode). A larger union is passed by
reference to a caller copy, like any large aggregate. Unions are not allowed in `//pu:extern` signatures.

**Packed unions.** `packed union [le|be] Backing { tagfields; variants }` gives a bit-exact layout:

- The fields before the first variant form the tag. They occupy the same bits in every variant.
- Each variant names the tag value it is selected by (`case false`, `case 0b10`, or a list
  `case 1, 2`), and lists its own fields, laid out after the tag fields by the packed rules of language
  spec section 5.5.
- Converting an integer or byte array of the backing size to a packed union never fails. A switch over a
  packed union whose `case` values do not cover every value of the tag fields must have a `default`
  case, since hardware may hold any bit pattern.
- Converting a packed union back to its backing type yields the bits.
- A packed union is one memory location (memory model section 4.3), as any packed value is.

## 5. Interactions

**Interfaces.** A union converts to an interface like any other value: in the data word if it is at most
a word (only possible for packed unions of up to 8 bytes), otherwise boxed in the converting function's
frame. A type switch on an interface sees the union type, not its variants; switch again on the union to
select the variant. A variant's payload may itself be an interface (`Failed error`); the union then holds
the two interface words inline.

Unions do not replace interfaces: interfaces stay the tool for open sets of types with shared behaviour,
unions for closed sets of shapes with no shared behaviour.

**Generics.** A union may have type parameters (`Result[T]`). Every instantiation has the same variants,
so a switch in generic code over `Result[T]` is checked once, for all `T`. Option A has no collision
problem when two payload types are equal, because variants are told apart by name. A union type can be a
type argument like any other type. Unions cannot appear in constraints; Go's `A | B` constraint syntax
keeps its meaning.

**Packed types.** Covered by section 4. A packed union may appear as a field of a packed struct, and a
packed struct may be a variant payload of a packed union.

**owned, shared, synced.** A variant payload may be an `owned[T]`. Dropping a union drops the payload of
the variant it holds; assigning a new variant drops the old payload first. The move checker treats moving
a payload out through a type switch as a move of the whole union.

**Switch lowering.** A type switch over a union is a switch on the tag: a jump table when there are many
variants, compares otherwise. When the union value is known at compile time (for example after inlining
a constructor), the switch folds.

## 6. Recommendation

**Option A, with the packed form for hardware layouts.**

- It reads like Go: a declaration that looks like a struct, literals that look like conversions and
  composite literals, and selection with the type switch every Go programmer knows.
- Variants are named once, in one place, which is what state machines and result types want, and payload
  types may repeat.
- Generics need no special rule: `Result[syscall.Errno]` is fine.
- The packed form expresses page-table entries, descriptor variants and protocol headers that differ by a
  tag bit, which is where a kernel needs sum types most.

Option B is the right choice only if variants with methods of their own, reused across unions, matter
more than concise declarations; it can be added later as `union{ A | B }` without conflict. Option C is
not recommended: too many new constructs for a language whose first goal is that it reads like Go.

### Implementation outline

1. `types2`: a `Union` type (variants, tag size), variant type objects, assignability, type switch and
   assertion checking, and the exhaustiveness check.
2. Parser: `union { ... }` and `packed union`, variant `case` clauses for packed unions.
3. `lower`: layout (section 4), construction, tag switch lowering, payload pointers for in-place update,
   type descriptors (kind `union`) so `fmt` can print the variant name.
4. `ssa`/`amd64`: nothing new; unions are aggregates.
5. Tests: `testdata/run` cannot use Go's output here, so `testdata/lib`-style tests with hand-checked
   output, plus `types2` tests for exhaustiveness errors.

## 7. Questions for the user

1. Option A, B or C?
2. Unqualified variant names in `case` clauses of a switch over a known union (`case Ok:` for
   `case Result[int].Ok:`)? Recommendation: yes, only inside a type switch whose operand has union type,
   and only when the name is not shadowed.
3. Keyword: `union` (recommended; it says what the layout is) or `enum` (familiar from Rust and Swift, but
   in C and Go culture an enum is a set of named integers)?
4. Should in-place update (`(&u).(*U.V)`) be part of the first version, or wait for real use?
