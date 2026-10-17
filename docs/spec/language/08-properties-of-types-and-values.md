## 8. Properties of types and values

This chapter defines the relations between types (underlying types, identity, assignability, method sets,
comparability), the relation between a constant and a type (representability), and the properties of types
that the rest of the specification relies on: how a value is represented, which kind of type it has (value,
view, owner or counted value), which values may be sent to another task, and which may be sent to another
address space.

### 8.1 Representation of values

Values of boolean and numeric types, enums, packed types, arrays, structs and unions are self-contained: each
such value contains a complete copy of all its data, and variables of such types store the entire value. For
instance, an array variable provides the storage (the variables) for all elements of the array. The respective
zero values are specific to the value's types; they are never `nil`.

Non-nil pointer, function, slice, string, map, channel, box (`owned`, `shared`, `synced`) and `weak` values
contain references to underlying data which may be shared by multiple values:

- A pointer value is a reference to the variable holding the pointer base type value.
- A function value contains a reference to the (possibly anonymous) function and enclosed variables: the
  closure record of section 7.8.
- A slice value contains the slice length, capacity, and a reference to its underlying array.
- A string value contains the string length and a reference to its bytes, which are immutable.
- A map or channel value is a reference to the data structure of the map or channel. Copies of the value share
  that data structure.
- An `owned[T]` value is the reference to a heap `T` that has no other holder. A `shared[T]` or `synced[T]`
  value is a reference to a heap `T` that other values also refer to. A `weak[P]` value refers to the same
  object without keeping it alive.

An interface value may be self-contained or contain references to underlying data depending on the interface's
dynamic type (section 7.9). The predeclared identifier `nil` is the zero value for types whose values can
contain references: pointer, function, slice, map, channel and interface types, `unsafe.Pointer`, and the
types `owned[T]`, `shared[T]`, `synced[T]` and `weak[P]`. A string has no `nil` value; its zero value is the
empty string.

When multiple values share underlying data, changing one value may change another. For instance, changing an
element of a slice will change that element in the underlying array for all slices that share the array. A map
or channel value that is copied refers to the same map or channel. Sharing a string's bytes is not observable,
because they never change. Sharing is also what makes a view dangerous: the kinds of section 8.7 say which
values share, which count their sharers, and which have exactly one holder.

**Words.** Every value occupies a whole number of bytes. The values that refer to other data are made of these
words:

| Type | Words | Contents |
|---|---|---|
| pointer, `unsafe.Pointer`, `owned[T]`, `shared[T]`, `synced[T]`, `weak[P]` | 1 | the address of the object, or zero |
| function | 1 | the address of the closure record (section 7.8) |
| channel | 1 | the address of the channel |
| map | 1 | the address of the counted table (section 7.10) |
| string | 3 | the address of the first byte, the length in bytes, the control word (section 7.3) |
| slice | 3 | the address of the first element, the length, the capacity |
| interface | 2 | the type descriptor or method table, the data word (section 7.9) |
| `ring[T, N]` | 2 + `N` elements | two `uint64` indexes, then the elements of type `T` |
| union | tag, then payload | the tag numbers the variants from 0 (section 7.13) |

An array is its elements in order. A struct is its fields in declaration order, each aligned (section 20.2,
Size and alignment guarantees). `volatile[T]` and `aligned[T, N]` are laid out as `T`, the second at an
`N`-byte boundary. An enum is its underlying integer. A packed type occupies its backing, or its field width
rounded up to whole bytes (section 7.14). Sizes and alignments are guaranteed by section 20.2; the binary
layout, calling convention and data formats are specified in abi.md.

The zero value of every type is defined in section 15.1 (The zero value).

### 8.2 Underlying types

Each type `T` has an underlying type: If `T` is one of the predeclared boolean, numeric, or string types, a
type literal, a packed type or a union type, the corresponding underlying type is `T` itself. If `T` is an
enum type, its underlying type is the integer type of the enum (section 7.12). Otherwise, `T`'s underlying
type is the underlying type of the type to which `T` refers in its declaration. For a type parameter that is
the underlying type of its type constraint, which is always an interface.

The `owner` qualifier (section 7.18) belongs to the definition of a type, not to its underlying type: the
underlying type of `type Frame owner uintptr` is `uintptr`, and the underlying type of
`type File owner struct{ fd int32 }` is `struct{ fd int32 }`.

```go
type (
	A1 = string
	A2 = A1
)

type (
	B1 string
	B2 B1
	B3 []B1
	B4 B3
)

type Color enum { Red; Green }    // underlying type uint8
type Frame owner uintptr          // underlying type uintptr

func f[P any](x P) {}
```

The underlying type of `string`, `A1`, `A2`, `B1`, and `B2` is `string`. The underlying type of `[]B1`, `B3`,
and `B4` is `[]B1`. The underlying type of `Color` is `uint8`, and that of `Frame` is `uintptr`. The
underlying type of `P` is `interface{}`.

### 8.3 Type identity

Two types are either identical ("the same") or different.

A named type is always different from any other type. Otherwise, two types are identical if their underlying
type literals are structurally equivalent; that is, they have the same literal structure and corresponding
components have identical types. In detail:

- Two array types are identical if they have identical element types and the same array length.
- Two slice types are identical if they have identical element types.
- Two struct types are identical if they have the same sequence of fields, and if corresponding pairs of
  fields have the same names, identical types, and identical tags, and are either both embedded or both not
  embedded. Non-exported field names from different packages are always different.
- Two pointer types are identical if they have identical base types.
- Two function types are identical if they have the same number of parameters and result values, corresponding
  parameter and result types are identical, and either both functions are variadic or neither is. Parameter
  and result names are not required to match. Default parameter values are not part of a function type and
  take no part in identity.
- Two interface types are identical if they define the same type set.
- Two map types are identical if they have identical key and element types.
- Two channel types are identical if they have identical element types and the same direction.
- Two types `owned[S]` and `owned[T]` are identical if `S` and `T` are identical, and the same holds for
  `shared`, `synced`, `weak` and `volatile`. Two types `aligned[S, M]` and `aligned[T, N]`, or `ring[S, M]`
  and `ring[T, N]`, are identical if `S` and `T` are identical and `M` and `N` are equal. A type of one of
  these forms is never identical to a type of another form.
- Two instantiated types are identical if their defined types and all type arguments are identical.

The types that can be written only in a type definition (enum types, union types, packed types and types
marked `owner`) are always defined types. Each definition creates a new type, so two such types are different
even when their declarations are written the same way. The `owner` qualifier is part of the definition: a type
declared `owner` is never identical to a type declared without it, and a type definition `type B owner A`
creates a type different from `A`. A type that is an owner because of its construction (`[4]File`,
`ring[File, 8]`, `owned[T]`) is identical to another type of the same construction with identical components.

Given the declarations

```go
type (
	A0 = []string
	A1 = A0
	A2 = struct{ a, b int }
	A3 = int
	A4 = func(A3, float64) *A0
	A5 = func(x int, _ float64) *[]string

	B0 A0
	B1 []string
	B2 struct{ a, b int }
	B3 struct{ a, c int }
	B4 func(int, float64) *B0
	B5 func(x int, y float64) *A1

	C0 = B0
	D0[P1, P2 any] struct{ x P1; y P2 }
	E0 = D0[int, string]
)
```

these types are identical:

```
A0, A1, and []string
A2 and struct{ a, b int }
A3 and int
A4, func(int, float64) *[]string, and A5

B0 and C0
D0[int, string] and E0
[]int and []int
struct{ a, b *B5 } and struct{ a, b *B5 }
func(x int, y float64) *[]string, func(int, float64) (result *[]string), and A5
```

`B0` and `B1` are different because they are new types created by distinct type definitions;
`func(int, float64) *B0` and `func(x int, y float64) *[]string` are different because `B0` is different from
`[]string`; and `P1` and `P2` are different because they are different type parameters. `D0[int, string]` and
`struct{ x int; y string }` are different because the former is an instantiated defined type while the latter
is a type literal (but they are still assignable).

Two struct types with the same fields are different if their tags differ:

```go
type (
	S1 = struct{ x int `a:"1"` }
	S2 = struct{ x int `a:"2"` }   // different from S1
	S3 = struct{ x int `a:"1"` }   // identical to S1
)
```

### 8.4 Assignability

A value `x` of type `V` is assignable to a variable of type `T` ("`x` is assignable to `T`") if one of the
following conditions applies:

- `V` and `T` are identical.
- `V` and `T` have identical underlying types but are not type parameters, at least one of `V` or `T` is
  not a named type, and neither `V` nor `T` is an owner type.
- `V` and `T` are channel types with identical element types, `V` is a bidirectional channel, and at least one
  of `V` or `T` is not a named type.
- `T` is an interface type, but not a type parameter, and `x` implements `T` (section 7.9; method sets are in
  section 8.6).
- `x` is a (possibly partially instantiated) generic function, `T` is a function type, and any type arguments
  not provided explicitly for `x` can be inferred such that (after full instantiation) `x` and `T` have
  identical underlying types.
- `x` is the predeclared identifier `nil` and `T` is a pointer, function, slice, map, channel or interface
  type, one of the types `owned[U]`, `shared[U]`, `synced[U]` or `weak[P]`, or `unsafe.Pointer`, but not a
  type parameter.
- `x` is an untyped constant representable by a value of type `T`, and `T` is not an enum type.

Additionally, if `x`'s type `V` or `T` are type parameters, `x` is assignable to a variable of type `T` if one
of the following conditions applies:

- `x` is the predeclared identifier `nil`, `T` is a type parameter, and `x` is assignable to each type in
  `T`'s type set.
- `V` is not a named type, `T` is a type parameter, and `x` is assignable to each type in `T`'s type set.
- `V` is a type parameter and `T` is not a named type, and values of each type in `V`'s type set are
  assignable to `T`.

Assignability depends on types only. Whether an assignment copies, moves or counts follows from the kind of
the type (section 8.7) and is defined in section 12.7 (Assignment statements) and section 18.5 (Moves).

Four rules deserve particular attention:

- **An untyped constant is not assignable to an enum type.** `var c Color = 1` and `c == 1` are errors
  (E2315), because a constant that is not a member's value would make a value that is not a member, and a
  constant that is one hides the member's name. The conversion `Color(1)` is valid if `1` is a member's value
  (section 11.24, Conversions).
- **There is no implicit conversion into a union.** A value is assignable to a union type only if its type is
  identical to it. `var u U = x` and `u = nil` are errors, where `x` is a value of a variant's payload type.
- **A box is not a pointer.** An `owned[T]`, `shared[T]` or `synced[T]` value is assignable only to a variable
  of an identical type, never to `*T`, and a `*T` is never assignable to a box type. The conversions out of a
  box are in section 11.24.
- **Interfaces and owners.** An owner type is assignable only to an identical type or to an interface; every other change of type involving an owner is a conversion (section 11.24, Owners). A value of an owner type is assignable to an ordinary interface (the interface
  lends it) and to an owning interface that it implements (it moves in). It implements an interface only
  through the methods that section 8.6 lets count: the value-receiver methods of an owner type never do.

```go
type Color enum { Red; Green }

func demo() {
	var a Color = Color.Green    // a member is a typed constant
	var b uint8 = 1
	// var c Color = 1           // error: an untyped constant is not assignable to an enum
	// var d Color = Color(b)    // error: b is not a constant (use the comma-ok conversion)
	_, _ = a, b
}
```

### 8.5 Representability

A constant `x` is representable by a value of type `T`, where `T` is not a type parameter, if one of the
following conditions applies:

- `x` is in the set of values determined by `T`.
- `T` is a floating-point type and `x` can be rounded to `T`'s precision without overflow. Rounding uses IEEE
  754 round-to-even rules but with an IEEE negative zero further simplified to an unsigned zero. Note that
  constant values never result in an IEEE negative zero, NaN, or infinity.
- `T` is an enum type and `x` is an integer constant equal to the value of a member of `T`.

If `T` is a type parameter, `x` is representable by a value of type `T` if `x` is representable by a value of
each type in `T`'s type set.

```
x                   T           x is representable by a value of T because

'a'                 byte        97 is in the set of byte values
97                  rune        rune is an alias for int32, and 97 is in the set of 32-bit integers
"foo"               string      "foo" is in the set of string values
1024                int16       1024 is in the set of 16-bit integers
42.0                byte        42 is in the set of unsigned 8-bit integers
1e10                uint64      10000000000 is in the set of unsigned 64-bit integers
2.718281828459045   float32     2.718281828459045 rounds to 2.7182817 which is in the set of float32 values
-1e-1000            float64     -1e-1000 rounds to IEEE -0.0 which is further simplified to 0.0
1                   Color       1 is the value of a member of Color, if some member has the value 1
```

```
x                   T           x is not representable by a value of T because

0                   bool        0 is not in the set of boolean values
'a'                 string      'a' is a rune, it is not in the set of string values
1024                byte        1024 is not in the set of unsigned 8-bit integers
-1                  uint16      -1 is not in the set of unsigned 16-bit integers
1.1                 int         1.1 is not an integer value
1e1000              float64     1e1000 overflows to IEEE +Inf after rounding
7                   Color       7 is not the value of a member of Color
```

Representability decides whether a constant may be converted (section 11.24) and, for the types other than
enums, whether it may be assigned (section 8.4). The precision of constants is in chapter 5 (Constants).

### 8.6 Method sets

The method set of a type determines the methods that can be called on an operand of that type. Every type has
a (possibly empty) method set associated with it:

- The method set of a defined type `T` consists of all methods declared with receiver type `T`. For an enum
  type, it includes the `String` method that the compiler generates, unless the type declares its own.
- The method set of a pointer to a defined type `T` (where `T` is neither a pointer nor an interface) is the
  set of all methods declared with receiver `*T` or `T`.
- The method set of an interface type is the intersection of the method sets of each type in the interface's
  type set (the resulting method set is usually just the set of declared methods in the interface).
- The method set of `owned[T]`, `shared[T]` and `synced[T]` is the method set of `*T`, as these types
  dereference like `*T` (section 7.19). The method set of `owned[T]` also has its own methods, `Drop` and,
  when `T` is not an owner, `Clone`. The box's own methods are never promoted from an embedded box and never
  satisfy an interface method. The method set of `weak[P]` is its one method, `Upgrade`.
- The method set of a type parameter is the method set of its constraint.

Further rules apply to structs (and pointer to structs) containing embedded fields, as described in the
section on struct types (section 7.6). The method set of `*ring[T, N]` is the methods of section 7.17 (all have pointer receivers). Any other type, including `volatile[T]` and `aligned[T, N]` (read the value as a `T` first), has an empty method set. In a method set, each method
must have a unique non-blank method name.

**Methods of owners.** A method with a value receiver of an owner type consumes its receiver: calling it moves
the owner into the method, which drops it when it returns (section 18.5, Moves). Such a method cannot be
called through an ordinary or an owning interface, because the interface cannot give up what it holds, and it
cannot be called through a pointer or a box, because that would move the owner out of a place that still
believes it holds it. Therefore:

- When a type is checked against an interface (section 7.9), the methods with a value receiver of an owner
  type are left out of its method set. This holds for the owner type itself, for a pointer to it, for an
  `owned`, `shared` or `synced` box of it, and for a method promoted from an embedded owner field.
- A conversion that needs such a method is a compile error that shows the fix: a pointer receiver, or `&b`.
- The method stays in the method set for direct calls on a whole owner variable and for a method expression: `b.Finish()` moves `b`, and `Buf.Finish(b)` does too.
- Calling it through a pointer, a box, an element, or as a method promoted from an embedded owner field (`p.Finish()`; `w.Finish()` where `w` embeds `Buf`) would move the owner out of a place that still holds it, and is error E3303 (section 18.5, Places).

The rule is the same for ordinary and owning interfaces, and for types and boxes. Code that needs to consume
an owner calls the method directly, or after a type assertion that moves the owner out of an owning interface.

```go
type Buf owner struct{ data []byte }

func (b *Buf) Len() int   { return len(b.data) }   // pointer receiver: in the method set of *Buf
func (b Buf) Finish() int { return len(b.data) }   // value receiver: consumes b

type Lener interface{ Len() int }

func demo(b Buf) int {
	var l Lener = &b       // *Buf implements Lener through Len
	// var m Lener = b     // error: Len has a pointer receiver; use &b
	n := l.Len()
	return n + b.Finish()  // b is consumed here
}
```

**`Drop`.** A method named `Drop` is never promoted from an embedded field. When the outer type is dropped,
its own `Drop` (if any) runs and then the embedded field is dropped like any other field (section 7.18).

**Methods with type parameters.** A method with type parameters of its own is in the method set but does not
count towards implementing an interface (section 10.15, Methods with type parameters).

### 8.7 Values, views, owners and counted values

Every type has a **kind**. The kind says what happens to a value of the type when it is assigned, passed,
returned and dropped, and so which rules of chapter 18 (Ownership and memory) apply to it. There are four
kinds.

- A **value** is only bits. It is copied on assignment, on passing and on return, and nothing happens when it
  goes out of scope.
- A **counted value** is a value that refers to shared memory and counts the values that refer to it. It is
  copied like a value, and each copy adds one to the count. When a copy is dropped it subtracts one, and the
  last strong release destroys the object; storage reclamation also respects weak references (section
  18.7). Strong cycles are not collected. Strings, channels, maps and the `shared`, `synced` and `weak` types are counted
  values. Counting is not chosen by the program: it follows from the type.
- A **view** looks at memory that something else owns and is copied like a value. A pure view does not
  release the memory it borrows. A composite view still retains and releases its counted components;
  its kind does not suppress their cleanup. It may not outlive what it looks at (section 18.3, Views).
- An **owner** holds memory or another resource and releases it when it is dropped. Assigning it **moves** it:
  the bytes go to the new place and the old place holds nothing. A moved-from variable may not be used until
  it is given a new value. Copies are made only by calling `Clone`. At the end of its life an owner is
  **dropped**: its `Drop` method runs, if it has one, and then its fields are dropped in declaration order and its array
  elements in index order.

| Kind | Assignment, passing, returning | When it ends | Zero value |
|---|---|---|---|
| value | copies | nothing | all bits zero |
| counted value | copies and adds a count | releases a count; last strong release destroys, remaining weak references delay reclamation | holds no reference |
| view | copies; retains counted components, if any | releases counted components; does not free borrowed memory | `nil` or empty |
| owner | moves | dropped | holds nothing; dropping it does nothing |

**The kind of every type.**

| Type | Kind |
|---|---|
| `bool`, integer and floating-point types, `uintptr` | value |
| enum types | value |
| packed types | value |
| `string` | counted value (a string made with `unsafe.String`, or in a local arena, is a view; sections 18.8 and 18.9) |
| `chan T` | counted value |
| `map[K]V` | counted value |
| `shared[T]`, `synced[T]`, `weak[P]` | counted value |
| `owned[T]` | owner |
| `*T`, `unsafe.Pointer` | view |
| `[]T` | view |
| function types | view |
| an interface type not declared `owner` | view (an `error` counts a counted box it holds, section 16.7) |
| an interface type declared `owner` | owner |
| `[N]T`, `volatile[T]`, `aligned[T, N]`, `ring[T, N]` | the kind of `T` |
| a struct type | the strongest kind among its fields; an owner only if declared `owner` |
| a union type | the strongest kind among its payloads; an owner only if declared `owner` |
| a type declared `owner` | owner |
| a defined type `type T U` | the kind of `U` |
| an alias | the kind of the type it denotes |
| a type parameter | an owner if the parameter is declared `owner` (section 10.9); otherwise the kind of the type argument, which is never an owner |

The kinds are ordered from the strongest to the weakest: owner, view, counted value, value. The kind of a
composite type is the strongest kind of its components. A struct with a counted field and a view field is a
view, and copying it still adds a count for the counted field. A type that contains a counted value anywhere,
or an owner, has **drop glue**: the code that the compiler generates to release the counts and run the `Drop`
methods when a value of the type is dropped. When a value of such a type is dropped is defined in section
18.6 (Drops) and section 18.8 (Strings).

Three rules keep the table honest:

- **An owner is declared, never accidental.** A struct or union whose components include an owner is an owner
  and must be declared `owner` (E3202, E2406); the compiler does not make a type an owner silently, because a
  type that looks like a plain struct and moves would be a false friend. The types that are owners by
  construction are `owned[T]` and an array, a `ring` or an `aligned` of an owner; an owning interface and an owner union are owners because they are declared `owner`.
  Section 7.18 (Owner types) gives the full rules.
- **Function and interface types are views.** A value of such a type may be a closure or a frame copy that
  refers to a frame, so the type has the kind that the strictest value could have. The escape analysis
  (section 18.3) checks each value by what it refers to: a function value that captures nothing may be
  returned or stored anywhere, as the type's kind alone would not allow.
- **A string is not an owner.** It is a counted value: it never moves, a moved-from string does not exist, and
  copying is always allowed, except for a string whose buffer is in a local arena, which is a view of the arena (section 18.9, Allocators).

```go
type Point struct{ x, y int }                  // value
type Named struct{ name string; p Point }      // counted value (a string field)
type Span struct{ buf []byte; n int }          // view
type Conn owner struct{ fd int32; buf []byte } // owner, declared
type Cache struct{ m map[string]int }          // counted value (a map field)
```

### 8.8 Sendable types

A value **crosses** between tasks in the cases that section 19.2 (The sendable rule) lists: an argument or receiver of a task statement (section 12.12, Task statements), an element sent on or received from a channel (section 12.5, Send statements), and the other cases of that list. A value that crosses must be safe
to use from several processors at once. A type is **sendable** if values of the type may cross, subject to the
conditions on views below.

A type is sendable unless it contains, directly or in any field, element, payload, referent of a box, or
target of a pointer or slice:

- a `shared[T]` or a `weak[shared[T]]`, whose counts are not changed atomically (E2010).

Following the structure of the types:

- Booleans, numbers, strings, enums, packed types, channels and function types are sendable. Strings are
  sendable because their bytes never change and their counts are atomic once a program has started a task
  (section 18.7, Shared ownership).
- An array, a `ring`, a `volatile` or an `aligned` type is sendable if its element type is. A struct is
  sendable if every field is, and a union if every payload is.
- A pointer `*T` or a slice `[]T` is sendable if `T` is. The rule looks through pointers: a `*S` whose `S`
  holds a `shared[T]` is not sendable, since two tasks that copy the field out of the one `S` would change
  its count without atomic operations. Whether the memory a view points to may be used by both tasks is
  checked where the value crosses, as below. `unsafe.Pointer` is sendable; what it points to is the
  responsibility of unsafe code (section 18.12, Safe and unsafe code).
- A map `map[K]V` is sendable if `K` and `V` are. A map is a counted value whose count follows
  the rule of section 18.7 (Shared ownership). Its contents fall under the race rules of section 19.3 (Data race freedom).
- `owned[T]` is sendable if `T` is. Sending it moves it, so only one task has it.
- `synced[T]` and `weak[synced[T]]` are sendable if `T` is. The count is atomic. The data is not: the fields
  of `T` that change are guarded by a lock of the same object or used atomically (section 19.3).
- An owner type is sendable if what it holds is sendable: its fields, and through a view field, the type
  the view points to. Sendability depends only on the types and the memory an owner holds, never on which
  packages its package imports. A view field that holds only memory the owner's own code allocated (inferred
   as a heap-only field is, section 18.3, Views) points into memory the owner owns, and that memory moves
   with the owner, so only what it holds is checked; this is how `vec.Vec`, `hash.Map` and
   `strings.Builder` cross. Construction must not retain an independent conflicting alias. A later
   transfer also accounts for outstanding borrows, including borrows through fields, helpers and
   aliases: no surviving alias may permit a conflicting access while the receiving task uses the
   allocation. A borrow whose last use precedes transfer remains allowed. If the compiler cannot prove
   the absence of conflicting access, it rejects the transfer under section 19.3 and identifies the
   borrow, transfer and conflicting use. Generation checks establish lifetime, not race freedom.
   A view field that may hold other memory is checked where the owner crosses, as any view is.
- An interface type is sendable if every type that the program converts to it, directly or through another
  interface, is. An owning interface moves when it crosses.
- A type parameter is checked for each type it is instantiated with.

**Views that cross.** A value of a sendable type crosses only if it contains no view of the frame of the
function that sends it (section 18.3, Views; E3004, E3005), and every view it contains points to memory that
cannot change while both tasks may use it, or where every change is synchronized. Section 19.2 (The sendable
rule) states the rule at the places where values cross, and section 19.3 states which memory is safe and which
error (E3501) reports the rest.

```go
type Job struct {
	id   int
	name string
	out  chan int
}

func worker(jobs chan Job) {}

func demo() {
	jobs := make(chan Job, 4)   // Job is sendable: a number, a string and a channel
	task worker(jobs)
	jobs <- Job{id: 1, name: "a", out: make(chan int)}
}
```

### 8.9 Portable types

A **portable** type is a type whose values may be sent between address spaces, as between two processes of an
operating system that do not share memory. A portable value means the same thing to the receiver as to the
sender, because it refers to nothing that lives in the sender's address space.

A type is portable if it is built only from booleans, numbers and enums, packed types, and arrays, structs,
unions, `ring`, `volatile` and `aligned` types of portable types. A type is not portable if it contains,
directly or at any depth:

- a pointer, `unsafe.Pointer`, a view (slice or pointer), or a function value;
- a counted value, including a string, a channel, a map, and the types `shared`, `synced` and `weak`;
- an owner: a type declared `owner`, an `owned[T]`, an owning interface, or a type built from them;
- an interface value.

A string is not portable because its bytes live in the sender's memory. A `uintptr` is a number and is
portable, but its value means nothing to a receiver in another address space. An owner is not portable even
when it is built from numbers, such as `type Frame owner uintptr`: sending it to another address space would
also transfer the duty to drop it, which the structural rule does not express.

`portable` is a predeclared constraint satisfied by exactly the portable types. It is used like `comparable`:
it can be a type constraint and nothing else (section 7.9, Interface types), so that a function can accept
only values that can be sent between address spaces.

```go
type Header struct {
	kind uint8
	len  uint32
}

func Send[T portable](port int, v T) {}   // Send(1, Header{}) compiles; Send(1, "text") does not
```

The rule is structural. The library that sends values between address spaces is specified with the operating
system's interface for typed communication between processes. That design will define how a type declares the
transfer of its values, so that handles and owners of pages can cross; until it does, no owner is portable,
and the structural rule is the part that needs no declaration.

### 8.10 Comparable types

The equality operators `==` and `!=` apply to operands of **comparable** types. The ordering operators `<`,
`<=`, `>` and `>=` apply to operands of **ordered** types. These terms and the result of the comparisons are
defined as follows; the operators themselves are in section 11.20 (Comparison operators).

- Boolean types are comparable. Two boolean values are equal if they are either both `true` or both `false`.
- Integer types are comparable and ordered. Two integer values are compared in the usual way.
- Floating-point types are comparable and ordered. Two floating-point values are compared as defined by the
  IEEE 754 standard.
- String types are comparable and ordered. Two string values are compared lexically byte-wise.
- Enum types are comparable and ordered, as their underlying integer types are. Two enum values are compared
  as integers.
- Packed types are comparable. Two packed values are equal if their storage is equal; reserved bits are always
  zero. They are not ordered.
- Pointer types are comparable. Two pointer values are equal if they point to the same variable or if both
  have value `nil`. Pointers to distinct zero-size variables may or may not be equal.
- Channel types are comparable. Two channel values are equal if they were created by the same call to `make`
  or if both have value `nil`.
- Boxes (`owned[T]`, `shared[T]`, `synced[T]`) and `weak[P]` are comparable. Two values of the same type are
  equal if they hold the same address, which includes both being `nil`. Each can be compared to `nil`. A
  comparison reads both operands and moves neither.
- Interface types that are not type parameters are comparable. Two interface values are equal if they have
  identical dynamic types and equal dynamic values or if both have value `nil`.
- A value `x` of non-interface type `X` and a value `t` of interface type `T` can be compared if type `X` is
  comparable and `X` implements `T`. They are equal if `t`'s dynamic type is identical to `X` and `t`'s
  dynamic value is equal to `x`.
- Struct types are comparable if all their field types are comparable. Two struct values are equal if their
  corresponding non-blank field values are equal. The fields are compared in source order, and comparison
  stops as soon as two field values differ (or all fields have been compared).
- Array types are comparable if their array element types are comparable. Two array values are equal if their
  corresponding element values are equal. The elements are compared in ascending index order, and comparison
  stops as soon as two element values differ (or all elements have been compared).
- Union types are comparable if every payload type is. Two union values are equal if they hold the same
  variant with equal payloads.
- `volatile[T]` and `aligned[T, N]` are comparable if `T` is, and compare as `T`. A `ring[T, N]` is not
  comparable.
- Type parameters are comparable if they are strictly comparable (see below).

**Owner types.** An owner type is comparable when its underlying type is. Comparing two owners reads both and
moves neither, so it never duplicates ownership: `type Fd owner int32` values compare as integers, and
`owned[Node]` compares by address.

```go
type Fd owner int32

type Node struct{ k int }

func demo(f, g Fd, root owned[Node]) bool {
	if f == g {          // reads f and g; neither moves
		return true
	}
	return root == nil   // an empty box
}
```

A comparison of two interface values with identical dynamic types causes a run-time panic if that type is not
comparable. This behavior applies not only to direct interface value comparisons but also when comparing
arrays of interface values or structs with interface-valued fields.

Slice, map and function types are not comparable. However, as a special case, a slice, map or function value
may be compared to the predeclared identifier `nil`. Comparison of pointer, channel, box, `weak` and interface
values to `nil` is also allowed and follows from the general rules above.

A type is **strictly comparable** if it is comparable and not an interface type nor composed of interface
types. Specifically:

- Boolean, numeric, string, enum, packed, pointer, channel, box and `weak` types are strictly comparable.
- Struct types are strictly comparable if all their field types are strictly comparable.
- Array types are strictly comparable if their array element types are strictly comparable.
- Union types are strictly comparable if all their payload types are strictly comparable.
- Type parameters are strictly comparable if all types in their type set are strictly comparable.

The predeclared constraint `comparable` is satisfied by the comparable types. The key type of a map must be
comparable (section 7.10), and may not be an owner.

### 8.11 Convertible types

Whether a value of one type can be converted to another is defined in section 11.24 (Conversions). That
section builds on this chapter: identical underlying types (section 8.2), identity (section 8.3),
assignability (section 8.4), representability (section 8.5) and the kinds of section 8.7. The rules that
depend on the kind of a type are these:

- Converting a value of non-owner type to an owner type is allowed only in the package that declares the owner
  type, where its invariants are kept (E3212). Converting an owner to a non-owner type reads its value and
  moves nothing; the result is a plain value with no `Drop`. Converting between two owner types with the same
  underlying type moves.
- A box never converts to or from a pointer, except through `(*T)(p)`, `&*p` and `unsafe`. A `weak[P]` is made
  by converting a reference (section 7.19).
- A rune, or a slice of runes, converts to a string; a string converts to no rune and to no slice (section 11.24, Conversions to and from a string type).

---

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
