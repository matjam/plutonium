## 13. Built-in functions

Built-in functions are predeclared (section 10.3, Predeclared identifiers). They are called like other
functions, but some of them accept a type instead of an expression as the first argument.

The built-in functions have no Plutonium types, so they can appear only in call expressions. They cannot
be used as function values, and a call of one cannot be the function of a `task` statement (E2591). A `defer`
statement accepts a call of a built-in function exactly when the call may stand as an expression
statement: `clear`, `close`, `copy`, `delete`, `panic`, `print` and `println` (section 12.19, Defer
statements). Deferring any other built-in function, or a function of package `unsafe`, is E2532, and the
fix is to defer a function literal that makes the call.

A call of `len`, `cap`, `make`, `min`, `max`, `new` or `take` yields a value, so it cannot stand alone as
an expression statement unless section 12.4 (Expression statements) says so. The calls `clear`, `close`,
`copy`, `delete`, `panic`, `print` and `println` can.

| Call | Meaning | Section |
|---|---|---|
| `clear(m)`, `clear(s)` | empty a map, or set the elements of a slice to the zero value | 13.3 |
| `close(ch)` | record that no more values will be sent on a channel | 13.4 |
| `copy(dst, src)` | copy slice elements | 13.2 |
| `delete(m, k)` | remove an entry from a map | 13.5 |
| `len(x)`, `cap(x)` | length and capacity | 13.6 |
| `make(T, ...)` | make a channel or a map | 13.7 |
| `min(x, ...)`, `max(x, ...)` | smallest and largest value | 13.8 |
| `new(T)`, `new(v)` | allocate a variable and return its owner | 13.9 |
| `take(p)` | move a value out of the place `p` points to | 13.10 |
| `panic(v)` | stop the program | 13.11 |
| `print(x, ...)`, `println(x, ...)` | write to the console | 13.12 |

The functions of package `unsafe` are described in section 20.1 (Package `unsafe`).

### 13.1 Built-ins Plutonium does not have

The names `append`, `recover`, `complex`, `real` and `imag` are not functions of the language and are not
predeclared (section 10.3, Predeclared identifiers). A use of one of them, when the program has not
declared the name itself, is a compile error with a code of its own that says why the function does not
exist and what to use instead. A program may declare its own `append`, `recover`, `complex`, `real` or
`imag`, which is then an ordinary identifier (chapter 10, Declarations and scope).

**`append` (E2013).** A slice is a view of an array that something else owns (section 18.3, Views). It
cannot grow, because growing needs a new array, and a new array would belong to no one (section 18.10,
No allocation without an owner or an arena). Every use of the undeclared name `append` is an error, whatever
its arguments.
A sequence that grows is a `vec.Vec[T]`, an owner (section 7.18, Owner types). The error names the form of
the idiom at hand:

| Instead of | Write | Allocates |
|---|---|---|
| `v = append(v, x)`, `v = append(v, xs...)` | `v.Push(x)`, `v.Append(xs...)` on a `vec.Vec[T]` | when it grows |
| `b = append(b, s...)` with a string `s` | `vec.AppendString(&b, s)` | when `b` is a `vec.Vec` that grows |
| `append([]T(nil), s...)` | `slices.Clone(s)`, a `vec.Vec[T]` | yes, an owner |
| `c := append(a, b...)` as a new sequence | `slices.Concat(a, b)`, a `vec.Vec[T]` | yes, an owner |
| `s = append(s[:i], s[j:]...)` | `s = slices.Delete(s, i, j)` | no |
| `append(buf[:0], ...)` into an array the code owns | `vec.NewFixed(buf[:])`, then `Push` and `Append` | no |
| `s = append(s, x)` within a capacity the code chose | `s = s[:len(s)+1]; s[len(s)-1] = x` | no |

Everything else that slices do they still do: indexing, slicing within the capacity, `copy`, `clear` and
`range`. The functions of the standard library named `Append...` (`strconv.AppendInt`, `fmt.Appendf`)
append to a `vec.Vec[byte]` or to a `vec.Fixed[byte]`, a bounded vector over a caller's array that never
allocates.

```go
package main

import "pluto/vec"

func main() {
	v := vec.New[int]()
	v.Push(1)
	v.Push(2)
	// v = append(v, 3)   // error E2013: a slice cannot grow
	println(v.Len())
}
```

**`recover` (E2004).** A panic never returns control to the function that panicked (section 17.1,
Panics), so there is nothing to resume and nothing to recover. A function that panics leaves its owners
in whatever state the panic found them in, and code that went on running after a recovery would have to
guess which of them hold a value. Deferred calls run during a hosted panic (section 17.2, Deferred calls
on a panic), but they cannot stop it. A failure that a program must survive is an `error` result
(chapter 16, Errors).

**`complex`, `real` and `imag` (E2006), and the types `complex64` and `complex128`.** The language has
no complex types (section 7.2, Numeric types); a use of the name of either type is E2006 as well. A value that needs a real part and an imaginary part is a struct of two floating-point fields.

### 13.2 Copying slices

The built-in function `copy` copies slice elements from a source `src` to a destination `dst` and returns
the number of elements copied. Both arguments must have identical element type `E` and must be assignable
to a slice of type `[]E`. The number of elements copied is the smaller of `len(src)` and `len(dst)`. The
result does not depend on whether the memory referenced by the arguments overlaps.

As a special case, `copy` also accepts a destination assignable to `[]byte` with a source of string type.
This form copies the bytes of the string into the byte slice.

```go
copy(dst, src []T) int
copy(dst []byte, src string) int
```

If the type of one or both arguments is a type parameter, all types in their respective type sets must
have the same underlying slice type `[]E`.

`copy` stores into elements that already exist. It never allocates and never changes the length or the
capacity of either slice. The destination is a view (section 18.3, Views), and the elements it looks at
belong to someone else. This makes `copy` the way to fill an array or a vector that the caller provides:
`copy(buf[:], s)` replaces the conversion `[]byte(s)`, which does not exist (section 11.24, Conversions).

If the elements are of a counted type (a string, a `shared[T]`, a `synced[T]`, a channel or a map), each
copy is a copy of a counted value: the source's reference is counted first, and then the reference the
destination element held is released, as in an assignment (section 12.7), so a copy between overlapping
slices, or onto an element that holds the last reference to the same object, frees nothing in use
(section 18.7, Shared ownership). If the elements are owners, `copy` is a compile error
(E3306): it would make a second owner of every element. Move the elements one by one, or use the container
type's `Clone` (section 18.5, Moves).

```go
var a = [...]int{0, 1, 2, 3, 4, 5, 6, 7}
var s [6]int
var b [5]byte

n1 := copy(s[:], a[0:])           // n1 == 6, s is [6]int{0, 1, 2, 3, 4, 5}
n2 := copy(s[:], s[2:])           // n2 == 4, s is [6]int{2, 3, 4, 5, 4, 5}
n3 := copy(b[:], "Hello, World!") // n3 == 5, b holds "Hello"
```

### 13.3 Clear

The built-in function `clear` takes an argument of map, slice or type parameter type, and deletes or
zeroes all its elements.

| Call | Argument type | Result |
|---|---|---|
| `clear(m)` | `map[K]V` | deletes every entry, leaving an empty map (`len(m) == 0`) |
| `clear(s)` | `[]T` | assigns the zero value of `T` to every element up to `len(s)` |
| `clear(t)` | type parameter | see below |

Elements of a slice beyond `len(s)` are not changed.

Clearing assigns, so it follows the rule for assignment (section 12.7, Assignment statements): assigning
to a place that holds an owner drops the old value first (section 18.6, Drops). `clear(s)` therefore
drops the elements of an owner type one at a time, from index 0 up, and each owner's `Drop` method runs
before its place becomes the zero value. A slice of files is closed by `clear`. For elements of a counted
type, each old reference is released. Nothing is leaked and nothing is dropped twice.

`clear(m)` deletes every entry and releases each counted key and each counted value. A map holds no
owners (section 7.10, Map types), so there is nothing else to drop. The order in which entries are
released is not specified.

If the type of the argument is a type parameter, all types in its type set must be maps or slices, and
`clear` performs the operation that matches the type argument. Clearing a nil map or a nil slice does
nothing. An argument of any other type is an error (E0148). To clear an array, slice it: `clear(a[:])`.

```go
var files [8]os.File // an owner type
clear(files[:])      // closes every file that is open and leaves the zero value in each place

counts := map[string]int{"a": 1}
clear(counts)        // len(counts) == 0
```

A container of the standard library (a `vec.Vec`, a `hash.Map`) is emptied by its own method, not by
`clear`.

### 13.4 Close

For a channel `ch`, the built-in function `close(ch)` records that no more values will be sent on the
channel. It is an error (E0085) if `ch` is not a channel or is a receive-only channel. Sending to a
closed channel causes a run-time panic, and so does closing a closed channel or the nil channel (section
17.1, Panics).

After `close`, and after any previously sent values have been received, receive operations return the
zero value of the channel's element type without blocking. The multi-valued receive operation
reports `false` as its second result once the channel is closed and empty (section 11.23, Receive
operator).

If the type of the argument is a type parameter, all types in its type set must be channels, and it is
an error if any of them is a receive-only channel.

`close` ends the sending; it does not free the channel. A channel is a counted reference (section 7.11,
Channel types): the channel and its buffer are freed when the last reference to it goes (section 18.7,
Shared ownership). Values still in the buffer at that point are dropped first, so owners in the buffer
are freed rather than leaked (section 18.6, Drops). `close` never waits, so interrupt handlers may call it
(section 20.10, Interrupts).

### 13.5 Deletion of map elements

The built-in function `delete` removes the entry with key `k` from a map `m`. The value `k` must be
assignable to the key type of `m`.

```go
delete(m, k) // remove m[k] from the map m
```

If `m` is not a map, the call is an error (E0088). If the type of `m` is a type parameter, all types in
its type set must be maps, and they must all have identical key types. If the map `m` is nil or the entry
`m[k]` does not exist, `delete` does nothing.

The entry's key and value, when they are of a counted type, are released (section 18.7, Shared
ownership). Deleting entries while a `for` statement ranges over the map is allowed, and the iteration
rules of section 12.10 (For statements) say which entries the loop still visits.

The containers of the standard library delete with a method (`m.Delete(k)` on a `hash.Map`). The
predeclared `delete` applies to the built-in map only.

### 13.6 Length and capacity

The built-in functions `len` and `cap` take arguments of various types and return a result of type
`int`. The implementation guarantees that the result always fits into an `int`.

| Call | Argument type | Result |
|---|---|---|
| `len(s)` | string type | string length in bytes |
| | `[n]T`, `*[n]T` | array length (`n`) |
| | `[]T` | slice length |
| | `map[K]V` | map length (number of defined keys) |
| | `chan T` | number of elements queued in the channel buffer |
| | `owned[A]`, `shared[A]`, `synced[A]` with `A` an array type | array length of `A` |
| | `ring[T, N]` | number of elements in the ring (the language handles it, not a `Len` call) |
| | a type that has the method `Len() int` (below) | the result of `Len` |
| | type parameter | see below |
| `len(E)` | an enum type `E` | the number of members of `E` (an untyped integer constant) |
| `cap(s)` | `[n]T`, `*[n]T` | array length (`n`) |
| | `[]T` | slice capacity |
| | `chan T` | channel buffer capacity |
| | type parameter | see below |

**Containers.** A type that is not a string, array, pointer to an array, slice, map or channel, and that
has a method `Len() int` with a receiver that the operand can supply, has the length that `Len` returns.
For an owner operand the overload must be nonconsuming, using a pointer receiver (section 11.11);
an owner value-receiver `Len` cannot supply it. So `len(v)` of a `vec.Vec[T]` calls `v.Len()`. The form of the method and the rest of the indexing
interface are defined in section 11.11 (Indexing library containers). The built-in types are always
handled by the language, even when a named type of one of them declares a `Len` method. `cap` has no such
form: a container reports its capacity with a method of its own (for a ring, `r.Cap()`).

**Type parameters.** If the argument type is a type parameter `P`, the call `len(e)` (or `cap(e)`) must
be valid for each type in `P`'s type set. The result is the length (or capacity) of the argument whose type
corresponds to the type argument with which `P` was instantiated. A type parameter that is constrained
only by methods calls its `Len` method (section 11.11).

**Relations.** The capacity of a slice is the number of elements for which there is space in the array
behind it. At any time `0 <= len(s) <= cap(s)`. The length of a nil slice, map or channel is 0, and the
capacity of a nil slice or channel is 0.

**Constants.** The expression `len(s)` is constant if `s` is a string constant. The expressions `len(s)`
and `cap(s)` are constants if the type of `s` is an array or pointer to an array and the expression `s`
contains no channel receives and no (non-constant) function calls; in this case `s` is not evaluated.
Otherwise a call of `len` or `cap` is not constant, and `s` is evaluated. `len(E)` of an enum type is
always constant.

```go
const (
	c1 = len([10]float64{2}) // [10]float64{2} contains no function calls
	c2 = len("abc")          // 3
)

type Level enum {
	Low
	Mid
	High
}

const levels = len(Level) // 3
```

### 13.7 Making channels and maps

The built-in function `make` takes a type `T`, which must be a channel type or a map type, or a type
parameter, optionally followed by a size and by an allocator. It returns a value of type `T` (not a
pointer to one). The memory it allocates is zeroed.

| Call | Type `T` | Result |
|---|---|---|
| `make(T)` | channel | an unbuffered channel |
| `make(T, n)` | channel | a buffered channel with room for `n` elements |
| `make(T)` | map | an empty map |
| `make(T, n)` | map | an empty map with initial space for approximately `n` entries |
| `make(T, a)`, `make(T, n, a)` | channel or map | as above, allocated from the allocator `a` |

**The allocator.** Without an allocator argument the memory comes from the current allocator (section
18.9, Allocators). The final argument is a size if its type is an integer type or it is an untyped
numeric constant, and an allocator otherwise. An allocator is any value assignable to `alloc.Allocator`
(`&arena`, for example). A nil allocator, and a call with no allocator when no default allocator has
been set, cause a run-time panic (section 17.1, Panics). If the allocator returns no memory, the call
panics.

**Sizes.** Each size argument must be of integer type, have a type set containing only integer types, or
be an untyped constant. A constant size must be non-negative and representable by a value of type `int`;
an untyped constant is given type `int`. If `n` is negative at run time, a run-time panic occurs. For a
map, `n` is only a hint: the precise behavior is not specified, and the map still grows as needed.

**Results.** A channel and a map are each one word and a counted reference (sections 7.10 and 7.11). The
call returns the first reference to a new channel or map; copying it counts another, and the object is
freed to its allocator when the last reference goes (section 18.7, Shared ownership). The allocator is
recorded in the object, so freeing goes to the allocator the object came from, whichever is the default
at the time. A map or channel made in an arena is released like any other, and the
arena counts it as a live allocation until then: `Reset`, `Release` and `Drop` of the arena panic
while it is referenced (section 18.9, Allocators, Live allocations). A map or channel stored in a plain
view of the arena (`a.New[T]()`, `a.Slice[T](n)`) is released by the arena's drop entries, which run
before the count is checked (section 18.9, Drop entries).

A map literal allocates a map from the current allocator. A map in a package-level initializer is subject
to the rule of section 15.2 (Package initialization).

If the first argument is a type parameter, all types in its type set must be map types with identical key
and element types, or all channel types with the same element type and channel directions that do not
conflict.

**Slices.** `make` of a slice type is a compile error (E2014). A slice is a view, and the array behind a
slice made by `make` would belong to no one (section 18.10, No allocation without an owner or an arena).
The error names the form that fits the idiom:

| Instead of | Write | Allocates |
|---|---|---|
| `make([]T, n)` | `vec.Make[T](n)`, a `vec.Vec[T]` of `n` zero elements | yes, an owner |
| `make([]T, 0, c)` | `vec.WithCapacity[T](c)`, an empty `vec.Vec[T]` with room for `c` elements | yes, an owner |
| `make([]T, n)` with `n` a constant | `var a [n]T`, then `a[:]` | no |
| a slice that only an arena should own | `a.Slice[T](n)` on an arena `a` | from the arena |

`v.Slice()` on a `vec.Vec` lends the elements as a slice. The allocator argument of `vec.Make` and
`vec.WithCapacity` follows the same rule as the allocator of `make`.

```go
package main

import "pluto/vec"

func main() {
	c := make(chan int, 10)   // buffered channel
	m := make(map[string]int) // empty map
	big := make(map[string]int, 1000)
	done := make(chan struct{})

	buf := vec.Make[byte](4096) // 4096 zero bytes, owned by buf

	_, _, _, _, _ = c, m, big, done, buf
	// s := make([]int, 10)     // error E2014: use vec.Make
}
```

### 13.8 Min and max

The built-in functions `min` and `max` compute the smallest, or largest, value of a fixed number of
arguments of ordered types (section 8.10, Comparable types). There must be at least one argument.
A call with an argument that is not ordered, or with a slice argument (`min(s...)`), is an error (E0150).

The same type rules as for binary operators apply to the types of the arguments (section 11.18, Operators):
after untyped constants are converted, all arguments must have one type, and the result has that type (and
similarly for `max`). Enum arguments are accepted, and `min(Debug, Warn)` has the enum type. An owner
argument is E0150, since the result would be a second owner. If all
arguments are constant, the result is constant.

```go
var x, y int
m := min(x)                // m == x
m = min(x, y)              // m is the smaller of x and y
m = max(x, y, 10)          // m is the larger of x and y but at least 10
c := max(1, 2.0, 10)       // c == 10.0 (floating-point kind)
f := max(0, float32(x))    // type of f is float32
t := max("", "foo", "bar") // t == "foo" (string kind)
```

For numeric arguments, assuming all NaNs are equal, `min` and `max` are commutative and associative:

```
min(x, y)    == min(y, x)
min(x, y, z) == min(min(x, y), z) == min(x, min(y, z))
```

For floating-point arguments, negative zero, NaN and infinity follow these rules:

| x | y | min(x, y) | max(x, y) |
|---|---|---|---|
| -0.0 | 0.0 | -0.0 | 0.0 |
| -Inf | y | -Inf | y |
| +Inf | y | y | +Inf |
| NaN | y | NaN | NaN |

Negative zero is smaller than non-negative zero. If any argument is a NaN, the result is a NaN.

For string arguments the result of `min` is the first argument with the smallest value, and for `max` the
first with the largest value, compared bytewise:

```
min(x, y)    == if x <= y then x else y
min(x, y, z) == min(min(x, y), z)
```

A result of string type is one of the arguments: it is a copy of a counted string and allocates nothing
(section 18.8, Strings).

### 13.9 Allocation

The built-in function `new` creates a new, initialized variable on the heap and returns the **owner** of
it, an `owned[T]` (section 7.19, `owned`, `shared`, `synced` and `weak`). It accepts an argument that is
either a type or an expression, and an optional allocator.

```go
new(T)       // a variable of type T, zeroed
new(v)       // a variable of the type of v, holding v
new(T, a)    // as new(T), from the allocator a
new(v, a)    // as new(v), from the allocator a
```

If the first argument is a type `T`, `new(T)` allocates a variable of type `T` initialized to its zero
value (section 15.1, The zero value). If it is an expression `v`, `new(v)` allocates a variable of the
type of `v` and stores `v` in it. If `v` is an untyped constant, it is first converted to its default
type, and an untyped boolean value to `bool`. The predeclared identifier `nil` cannot be an argument.
Whether the first operand is a type decides between `new(T, a)` and `new(v, a)`. The value `v` is
evaluated before the allocator. If `v` is an owner, `new(v)` moves it into the new variable (section 18.5,
Moves). `new(T{...})` is how a composite literal outlives its frame (section 11.3, Composite literals).

**The allocator** is any value assignable to `alloc.Allocator`, such as `&arena`. Without one, the memory
comes from the current allocator (section 18.9, Allocators). A call with no allocator when none has been
set, as in a freestanding program before `alloc.SetDefault`, panics, and so does a nil allocator or an
allocator that returns no memory (section 17.1, Panics).

**The result** is the single owner of the new variable. Dropping it drops the `T` and frees the memory to
the allocator recorded in the box's header (section 18.6, Drops). The result dereferences like a `*T`:
`p.field`, `*p` and the methods of `*T` work on it, so `n := new(Node); n.val = 1` works as written. It is
not a `*T`. Storing it in a variable of type `*T`, returning it as a `*T`, or passing it where a
`*T` is expected is an assignability error (section 8.4, Assignability), so code that keeps the pointer
of a `new` shows where an owner must go. `(*T)(n)` and `&*n` lend a `*T` view of the variable, bounded by
the owner it came from (section 18.3, Views).

```go
type Node owner struct {
	val  int
	next owned[Node]
}

func head() int {
	n := new(Node{val: 1}) // n is an owned[Node]; dropped, and freed, when head returns
	n.val++
	return n.val
}
```

A box made with an arena as its allocator (`new(T, &a)`) is an owner like any other, and the arena counts
it as a live allocation until the box is dropped: `Reset`, `Release` and `Drop` of the arena panic while
it lives (section 18.9, Allocators, Live allocations). Memory that only an arena should own is allocated
through the arena, which returns views: `a.New[T]()` gives a `*T`, valid until the arena is reset or
dropped, and not counted. When `T` has drop glue, the arena records a drop entry and drops the value in
place when it takes the memory back, before it checks its count (section 18.9, Drop entries).

### 13.10 Moving out of a place

An owner can be moved only out of a whole variable (section 18.5, Moves). Moving it out of a field, an
element, a value reached through a pointer or a package-level variable is a compile error (E3303): the
place would still believe it holds the value and would drop it again. The built-in function `take` is the
way to move out of such a place.

```go
take(p) T // p has type *T; T is any type
```

`take(p)` returns the value `*p` and leaves the zero value of `T` in `*p`. The place still holds a value
afterwards: the zero value, which an owner drops without doing anything (section 7.18, Owner types), so
that a later assignment to the place, or the end of its life, is correct. The move checker treats `take`
as a use of the place and a move of its old contents.

The argument must be a pointer. Any other argument is an error (E3208). `take` works for values of every
type: for a value that is not an owner it reads the place and sets it to zero. It does not drop anything.
Dereferencing a nil `p` faults like any nil dereference (section 17.5, Faults). The write through `p` is
a write like any other for the rules of section 19.3 (Data race freedom).

```go
type Stack owner struct {
	top owned[Frame]
}

type Frame owner struct {
	next owned[Frame]
	val  int
}

func (s *Stack) Pop() owned[Frame] {
	f := take(&s.top)    // s.top holds nothing from here on
	if f != nil {
		s.top = take(&f.next) // moves the next frame into s.top
	}
	return f
}
```

Two other ways to move an owner out of a place are available. An assignment that permutes places
(`a, b = b, a`, `s[i], s[j] = s[j], s[i]`) is allowed, since nothing is duplicated or lost. A `Clone` copies
instead of moving.

### 13.11 Handling panics

The built-in function `panic` stops the program.

```go
func panic(v any)
```

A call of `panic` stops the execution of the function that called it, and of the whole program. It does
not unwind the stack: control never returns to a function that panicked, and the owners held by the
frames of the stack are not dropped (section 18.6, Drops). In a hosted build, the panicking task first
runs its pending deferred calls, innermost frame first (section 17.2, Deferred calls on a panic). Then the
runtime calls the program's panic handler, which reports the panic and never returns (section 17.4,
Panics in freestanding programs). In freestanding and kernel builds no deferred calls run. A call of
`panic` is a terminating statement (section 12.1, Terminating statements).

The argument may be a value of any type, including `nil`. The panic's message is the value's text if it
is a string, an `error` (its `Error` result), a boolean or an integer, and a placeholder that names
no contents otherwise. The handler receives the value itself in `rt.Panic.Value`.

```go
panic(42)
panic("unreachable")
panic(errors.New("cannot parse"))
```

`panic` is the only built-in for failure. There is no `recover` (section 13.1, Built-ins Plutonium does not
have), so a panic cannot be caught: it ends the program (section 17.1). Code that must go on after a failure
returns an `error` (chapter 16, Errors). The run-time errors of chapter 17 (Run-time panics) are panics in exactly
this sense.

### 13.12 Bootstrapping

The built-in functions `print` and `println` write their arguments to the console. They do not return a
result and need no package to be imported, so they work in a program that has no standard library, such as
the first lines of a kernel.

| Function | Behavior |
|---|---|
| `print` | writes all its arguments |
| `println` | like `print`, but writes a space between the arguments and a newline at the end |

**What they print.** Each argument is written in a fixed form:

| Argument type | Form |
|---|---|
| a value whose underlying type is boolean | `true` or `false` |
| a value whose underlying type is an integer, including a defined integer type and an enum | decimal |
| a value whose underlying type is floating-point | the shortest decimal form that reads back as the same value, in the `g` format of `strconv` (`1.5`, `1e+21`, `-0`), at the precision of its type |
| a value whose underlying type is a string | its bytes |
| a pointer, `unsafe.Pointer`, function, or the address of an `owned`, `shared`, `synced` or `weak` value | the address, in hexadecimal with `0x` |

A floating-point argument in kernel mode counts as a use of floating point and is allowed only in a
`//pu:fpu` function (section 20.7, Floating point in kernel mode). It is printed as its bit pattern
(`float64=0x4020000000000000`), because the console code of a kernel cannot use the floating-point unit.
An owner type cannot be printed, even when its underlying type is an integer: write `int32(fd)`, so that printing never raises the question of a move. Other types cannot be printed either (E2521).

**Where.** In a hosted build `print` and `println` write to standard error, with a system call that
never waits. In a freestanding build they write with the function registered by `rt.SetConsole`.

**Atomic statements.** A `print` or `println` statement is atomic: statements that run at the same time on
other tasks or processors never mix their output within the statement. Its pieces are collected and
written together when the statement ends. A statement whose output is longer than the runtime's buffer is
written in several pieces, which still do not mix with another statement's. In kernel mode the statement
also masks interrupts on its processor while it runs, so an interrupt handler may print (section 20.10,
Interrupts), and the console function runs with them masked and must not wait for another task. Masking
does not hold off an exception or a nonmaskable interrupt, so a handler for one of them may interrupt a
`print` on its own processor. Its `print` then does not wait for the statement it interrupted, which could
never finish: the handler's output is dropped. On another processor it waits as usual. The
message of a panic bypasses this mechanism, so that a panic can always report itself (section 17.4).

Nothing in these functions allocates. They are documented for use during bootstrapping and in the panic
path; programs write text with the packages of the standard library.

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
