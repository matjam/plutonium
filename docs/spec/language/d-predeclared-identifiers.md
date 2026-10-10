## D. Predeclared identifiers

The following identifiers are implicitly declared in the universe block (section 10.3, Predeclared
identifiers). A program may redeclare any of them in the package block or an inner block.

**Types.**

| Identifier | Meaning | Section |
|---|---|---|
| `any` | alias for `interface{}` | 7.9 Interface types |
| `bool` | the boolean type | 7.1 Boolean types |
| `byte` | alias for `uint8` | 7.2 Numeric types |
| `comparable` | the constraint of strictly comparable types; a constraint only | 10.9 Type parameter declarations |
| `error` | the interface `interface{ Error() string }` | 16.1 The error interface |
| `float32`, `float64` | IEEE 754 floating-point numbers | 7.2 |
| `int`, `int8`, `int16`, `int32`, `int64` | signed integers; `int` is 64 bits | 7.2 |
| `portable` | the constraint of types that may cross between address spaces; a constraint only | 8.9 Portable types |
| `rune` | alias for `int32` | 7.2 |
| `string` | immutable counted UTF-8 text | 7.3 String types |
| `uint`, `uint8`, `uint16`, `uint32`, `uint64` | unsigned integers; `uint` is 64 bits | 7.2 |
| `uintptr` | an unsigned integer large enough for an address; 64 bits | 7.2 |

**Type constructors.** These are generic and are written with arguments in square brackets.

| Identifier | Meaning | Section |
|---|---|---|
| `volatile[T]` | a `T` accessed with exactly one load or store per read or write | 7.15 `volatile[T]` |
| `aligned[T, N]` | a `T` placed on an `N`-byte boundary | 7.16 `aligned[T, N]` |
| `ring[T, N]` | a fixed-capacity ring buffer stored inline | 7.17 `ring[T, N]` |
| `owned[T]` | the single owner of a heap `T` | 7.19 `owned`, `shared`, `synced` and `weak` |
| `shared[T]` | a counted reference used by one task | 7.19 |
| `synced[T]` | a counted reference whose count is atomic | 7.19 |
| `weak[P]` | a reference to the object of `P` that does not keep it alive | 7.19 |

**Constants.**

| Identifier | Meaning | Section |
|---|---|---|
| `true`, `false` | the untyped boolean constants | 7.1; chapter 5 Constants |
| `iota` | the index of the current constant specification in a `const` declaration; not defined in an enum | 10.7 Iota |

**Zero value.**

| Identifier | Meaning | Section |
|---|---|---|
| `nil` | the zero value of pointers, functions, interfaces, slices, maps and channels | 15.1 The zero value |

**Functions.**

| Identifier | Meaning | Section |
|---|---|---|
| `cap` | capacity of an array, slice or channel | 13.6 Length and capacity |
| `clear` | sets elements to zero values, dropping owners first; empties a map | 13.3 Clear |
| `close` | closes a channel | 13.4 Close |
| `copy` | copies elements between slices | 13.2 Copying slices |
| `delete` | removes an element from a map | 13.5 Deletion of map elements |
| `len` | length of a string, array, slice, map or channel, or the number of members of an enum | 13.6 |
| `make` | makes a channel or a map | 13.7 Making channels and maps |
| `max`, `min` | the largest and smallest of their arguments | 13.8 Min and max |
| `new` | allocates a variable and returns an `owned[T]` | 13.9 Allocation |
| `panic` | stops the program | 13.11 Handling panics |
| `print`, `println` | write their arguments; for bootstrapping and debugging | 13.12 Bootstrapping |
| `take` | moves a value out of a place, leaving the zero value | 13.10 Moving out of a place |

**Names that are not declared.** `append`, `recover`, `complex`, `real`, `imag`, `complex64` and
`complex128` are Go's predeclared identifiers that Plutonium does not have. A use of one without a
declaration of the program's own is a compile error that names the replacement (section 13.1, Built-ins
Plutonium does not have).

**Not in the universe block.** The functions of package `unsafe` (`Pointer`, `Sizeof`, `Alignof`,
`Offsetof`, `Add`, `Slice`, `SliceData`, `String`, `StringData` and `Drop`) are declared by importing
`unsafe` (section 20.1, Package `unsafe`). The contextual keywords `owner`, `union`, `task`, `bits`, `le`
and `be` are not declared at all (section 4.6, Contextual keywords).

Adapted in part from the Go specification, copyright The Go Authors, CC BY 4.0; see
[Attribution](../language.md#attribution).
