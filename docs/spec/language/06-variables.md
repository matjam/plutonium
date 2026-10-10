## 6. Variables

A variable is a storage location for holding a value. The set of permissible values is determined by the
variable's type.

A variable declaration (section 10.10, Variable declarations) or, for function parameters and results, the
signature of a function declaration (section 10.12, Function declarations) or function literal (section
11.4, Function literals) reserves storage for a named variable.

Calling the built-in function `new` allocates storage for a variable at run time (section 13.9,
Allocation). Such an anonymous variable is reached through an `owned[T]` that owns it (section 7.19,
`owned`, `shared`, `synced` and `weak`). Taking the address of a composite literal, `&T{...}`, does not
allocate a variable on the heap: the literal is a variable of the enclosing function's frame, or of static
storage in a package-level initializer, and the address is a view of it (section 11.3, Composite
literals).

Structured variables of array, slice and struct types have elements and fields that may be addressed
individually. Each such element acts like a variable.

**Where a variable lives.** Where a variable lives is fixed by its declaration, never by how it is used
later:

- a local variable, parameter or result lives in the frame of its function, and ends when the call returns;
- a package-level variable lives in static storage for the whole run of the program;
- a field lives in its struct, and an element in its array;
- an element of a slice lives in the array the slice refers to;
- a variable made by `new` or by an allocator lives in memory that the allocator supplied, and is freed
  when its owner is dropped (chapter 18, Ownership and memory).

Plutonium never moves a variable to the heap because its address is taken. `&x` is a view of `x` where `x`
lives, and a view may not outlive its variable (section 18.2, Where variables live; section 18.3, Views).

```go
func bad() *int {
	x := 1
	return &x // error: &x is a view of x, which lives in bad's frame
}

func good() owned[int] {
	return new(1) // a variable on the heap, owned by the result
}
```

As in Go, a variable's address may be taken and passed down to calls. Unlike Go, returning it past the
variable's life is a compile error, not an implicit allocation.

**Loops reuse storage.** Every iteration of a loop uses the same storage for the variables declared in its
header and in its body (section 18.4, Loop iterations reuse their storage). A view of such a variable is
valid for one iteration, and keeping it where a later iteration can see it is a compile error. Unlike Go
1.22, the loop does not create a fresh variable for each iteration, and code that depends on that is
rejected, not silently different.

**Types of variables.** The *static type* (or just *type*) of a variable is the type given in its
declaration, the type provided in the `new` call or composite literal, or the type of an element of a
structured variable. Variables of interface type also have a distinct *dynamic type*, which is the
(non-interface) type of the value assigned to the variable at run time, unless the value is the predeclared
identifier `nil`, which has no type. The dynamic type may vary during execution, but values stored in
interface variables are always assignable to the static type of the variable (section 8.4,
Assignability).

```go
type T struct{}

var x interface{} // x is nil and has static type interface{}
var v *T          // v has value nil, static type *T
x = 42            // x has value 42 and dynamic type int
x = v             // x has value (*T)(nil) and dynamic type *T
```

**Values of variables.** A variable's value is retrieved by referring to the variable in an expression; it
is the most recent value assigned to the variable. If a variable has not yet been assigned a value, its
value is the zero value for its type (section 15.1, The zero value).

The kind of the variable's type decides what assigning to and leaving the variable does (section 8.7,
Values, views, owners and counted values):

- A value is copied.
- An owner is moved, and the variable it came from holds nothing until a new value is assigned to it
  (section 18.5, Moves). A variable that still holds an owner when its scope ends drops it (section 18.6,
  Drops).
- A counted value is copied by adding a reference, and the variable releases its reference when it is
  assigned over or goes out of scope (section 18.7, Shared ownership; section 18.8, Strings).

A variable that is read by more than one task is subject to the rules of data race freedom (section 19.3,
Data race freedom).

**Per-processor variables.** In kernel builds, a package-level variable marked `//pu:percpu` has one copy
per processor (section 20.9, Per-CPU variables).

Adapted in part from the Go specification, copyright The Go Authors, CC BY 4.0; see
[Attribution](../language.md#attribution).
