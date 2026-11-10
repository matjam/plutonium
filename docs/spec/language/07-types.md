## 7. Types

A type determines a set of values together with operations and methods specific to those values. It also
determines how its values behave when they are copied, moved and dropped: its **kind** (section 8.7, Values,
views, owners and counted values). A type may be denoted by a type name, if it has one, which must be followed
by type arguments if the type is generic. A type may also be specified using a type literal, which composes a
type from existing types.

```
Type        = TypeName [ TypeArgs ] | TypeLit | "(" Type ")" .
TypeName    = identifier | QualifiedIdent .
TypeArgs    = "[" TypeArgList [ "," ] "]" .
TypeArgList = TypeArg { "," TypeArg } .
TypeArg     = Type | Expression .
TypeLit     = ArrayType | StructType | PointerType | FunctionType | InterfaceType |
              SliceType | MapType | ChannelType .
```

A type argument that is an expression is allowed only for the size argument of `aligned` and `ring` (sections
7.16 and 7.17), and must be a constant.

The language predeclares certain type names. Others are introduced with type declarations (section 10.8, Type
declarations) or type parameter lists (section 10.9, Type parameter declarations). Composite types (array,
struct, pointer, function, interface, slice, map and channel types, and the types `owned[T]`, `shared[T]`,
`synced[T]`, `weak[P]`, `volatile[T]`, `aligned[T, N]` and `ring[T, N]`) may be constructed using type
literals.

Four declaration forms can be written only as the right-hand side of a type definition, not as
anonymous types elsewhere: enum declarations (section 7.12), union declarations (section 7.13), packed
declarations (section 7.14) and the `owner` qualifier (section 7.18). Existing named types can be
referenced or aliased under section 10.8. A type definition therefore has this right-hand side:

```
DefinedType = [ "owner" ] ( Type | EnumType | UnionType | PackedType | PackedUnionType ) .
```

Section 10.8 (Type declarations) gives the rest of the declaration. Which combinations are valid is stated
with each type: `owner` cannot qualify an enum, for example.

Predeclared types (excluding `any` and the generic type constructors named below), defined types and type parameters are called **named types**. An alias
denotes a named type if the type given in the alias declaration is a named type. All named types are distinct.
The predeclared types are:

```
bool byte error float32 float64 int int8 int16 int32 int64 rune string
uint uint8 uint16 uint32 uint64 uintptr any comparable portable
owned shared synced weak volatile aligned ring
```

`owned`, `shared`, `synced`, `weak`, `volatile`, `aligned` and `ring` are generic type constructors: each is
written with type arguments and has no meaning without them. They are not named types: an instantiation such as `owned[Node]` is written `TypeName TypeArgs` (section E.7) and counts as a type literal for sections 8.2 to 8.4, and two uses of it denote the same type (section 8.3, Type identity). The productions printed in sections 7.15 to 7.19 are notation for that form, not separate productions of the grammar. The predeclared identifiers are
listed in appendix D.

Plutonium has no complex number types. The identifiers `complex64`, `complex128`, `complex`, `real` and `imag`
are not predeclared (section 10.3, Predeclared identifiers).

### 7.1 Boolean types

A boolean type represents the set of Boolean truth values denoted by the predeclared constants `true` and
`false`. The predeclared boolean type is `bool`; it is a named type.

**Kind.** A boolean type is a value.

### 7.2 Numeric types

An integer or floating-point type represents the set of integer or floating-point values, respectively. They
are collectively called numeric types. The predeclared architecture-independent numeric types are:

```
uint8       the set of all unsigned  8-bit integers (0 to 255)
uint16      the set of all unsigned 16-bit integers (0 to 65535)
uint32      the set of all unsigned 32-bit integers (0 to 4294967295)
uint64      the set of all unsigned 64-bit integers (0 to 18446744073709551615)

int8        the set of all signed  8-bit integers (-128 to 127)
int16       the set of all signed 16-bit integers (-32768 to 32767)
int32       the set of all signed 32-bit integers (-2147483648 to 2147483647)
int64       the set of all signed 64-bit integers (-9223372036854775808 to 9223372036854775807)

float32     the set of all IEEE 754 32-bit floating-point numbers
float64     the set of all IEEE 754 64-bit floating-point numbers

byte        alias for uint8
rune        alias for int32
```

The value of an n-bit integer is n bits wide and represented using two's complement arithmetic.

There is also a set of predeclared integer types whose size the language fixes:

```
uint     the set of all unsigned 64-bit integers
int      the set of all signed 64-bit integers
uintptr  an unsigned 64-bit integer, large enough to store the uninterpreted bits of a pointer value
```

These three types are 64 bits wide on every target. A program never has to ask how wide an `int` is. A target
with a different pointer width would be a deliberate change to the language.

Plutonium has no complex types. All numeric types are named types and thus distinct, except `byte`, which is an alias for `uint8`, and `rune`,
which is an alias for `int32`. Explicit conversions are required when different numeric types are mixed in an
expression or assignment. For instance, `int32` and `int` are different types.

Integer arithmetic wraps on overflow, and integer division by zero panics (section 11.19, Arithmetic
operators). A floating-point operation never panics. Converting a floating-point value to an integer type when
the value is out of range, or is a NaN, gives an implementation-dependent value and never faults (section
11.24, Conversions).

A `uintptr` holds the bits of an address and nothing else. It is a number: converting a pointer to a `uintptr`
gives a value that the compiler no longer follows, and converting a `uintptr` to a pointer is available only
through package `unsafe` (section 20.1, Package unsafe).

**Kind.** A numeric type is a value.

### 7.3 String types

A string type represents the set of string values. A string value is a (possibly empty) sequence of bytes that
holds valid UTF-8. The number of bytes is called the length of the string and is never negative. Strings are
immutable: once created, it is impossible to change the contents of a string. The predeclared string type is
`string`; it is a named type.

```go
func demo() (int, byte) {
	s := "héllo"         // 6 bytes: h, two bytes for é, l, l, o
	return len(s), s[1]  // 6, and the byte 0xC3
}
```

**Valid UTF-8.** Every string value holds valid UTF-8. A string literal or constant that is not valid UTF-8 is
a compile error (E2012), and no conversion produces such a string. Binary data belongs in `[]byte`, `[N]byte`
or a growable owner of bytes from the library. The one way to make a string over bytes of unknown content is
`unsafe.String` (section 20.1, Package unsafe): the result is a view of those bytes. It is unsafe code, and
the program must ensure that the bytes are valid UTF-8; section 20.1 says what follows if they are not.

**Names from the operating system.** A name or a value that the operating system gives the program (a file
name, a directory entry, a working directory, an argument, an environment variable) is bytes, and the
system does not promise that they are valid UTF-8. No function of the library turns such bytes into a string
by changing them. The function that has the Go name returns the string and an error: `name, err := e.Name()`
for a directory entry, `v, err := os.Getenv(k)` for an environment variable. The error is a `*os.NameError`
when the bytes are not valid UTF-8, and it holds the bytes. Using the call as a single value
(`name := e.Name()`) is a compile error, and its explanation names `RawName`. Two other forms are always
available and are named so that they show what they do: `e.NameLossy()` returns a string with each invalid
byte replaced by U+FFFD, for display only, and `e.RawName()`, `os.GetenvBytes` and the `Bytes` forms of the
file functions (`os.OpenBytes`) take and return the exact bytes. The split is the one of `strings.FromUTF8`
and `strings.FromUTF8Lossy` (section F.6). Section F.9 lists the names. PlutoniumOS refuses a name that is not
valid UTF-8 when the file is created, so no name there needs this.

**Length and indexing.** The length of a string `s` can be discovered using the built-in function `len`
(section 13.6, Length and capacity). It counts bytes. The length is a compile-time constant if the string is a
constant. A string's bytes can be accessed by integer indices 0 through `len(s)-1`, and the result is a
`byte`. It is illegal to take the address of such an element: if `s[i]` is the i'th byte of a string, `&s[i]`
is invalid. A `range` clause over a string yields runes (section 12.10, For statements).

**Taking a substring.** A slice expression on a string is a compile error (E2019, section 11.12, Slice
expressions). `strings.Slice(s, i, j)` and `strings.SliceFrom(s, i)` take a substring. Each bound must be a
**rune boundary**: `0`, `len(s)`, or the offset of a byte that is not a UTF-8 continuation byte (a byte of the
form `10xxxxxx`). A bound that is not one panics (section 17.1, Panics). This keeps every substring valid
UTF-8, which is the invariant above. `strings.Truncate(s, n)` and `utf8.RuneStartBefore(s, i)` find the
boundary at or before a byte count. An offset returned by `strings.Index` and the other search functions
is always a rune boundary, and so is the offset just after a match.

**Counted values.** A string made at run time refers to a **buffer** with a count of the string values that
use it. Copying a string adds one to the count, and dropping a copy subtracts one; the last one frees the
buffer. A substring made by `strings.Slice` shares the buffer of `s`. Strings made from
literals and constants are static: they have no buffer and are never counted. The counting is automatic and a
string copies as freely as an integer; its kind is counted (section 8.7). Section 18.8 (Strings) gives the
rules for when counts change and what the compiler may leave out.

A string made by concatenation or by a library function allocates its buffer from the current allocator
(section 18.9, Allocators). `unsafe.String(p, n)` makes a string that is not counted: it is a view of the
memory at `p`, which must outlive every copy of the string (section 18.3, Views).

**Representation.** A string occupies three words: the address of its first byte, its length in bytes, and a
control word. The control word identifies the buffer that counts the string, or holds only flag bits for a
string that is not counted. One flag says whether any byte is at or above `0x80`; when it is clear, the
library can count, index and compare the runes of the string without decoding. The binary layout of the words
is in abi.md section 1.1.

**Zero value, conversions and comparison.** The zero value of a string type is the empty string, which is
static. Strings are compared bytewise (section 8.10, Comparable types). The conversions between strings and
runes or bytes are in section 11.24 (Conversions): `string(runes)` exists, and so does `string(r)` for a value
of type `rune` or an untyped constant; a conversion to `string` from any other integer type is a compile error
that shows `string(rune(i))` and `strconv.Itoa(i)`. Converting a string to `[]byte` or `[]rune`, and a byte
slice to a string, are compile errors that name the library function to use instead (`strings.Bytes`,
`strings.Runes`, `strings.FromUTF8`).

**Kind.** A string is a counted value (section 8.7). It is not an owner: it never moves, and copying it is
always allowed. Strings are sendable (section 8.8, Sendable types).

### 7.4 Array types

An array is a numbered sequence of elements of a single type, called the element type. The number of elements
is called the length of the array and is never negative.

```
ArrayType   = "[" ArrayLength "]" ElementType .
ArrayLength = Expression .
ElementType = Type .
```

The length is part of the array's type; it must evaluate to a non-negative constant representable by a value
of type `int`. The length of array `a` can be discovered using the built-in function `len`. The elements can
be addressed by integer indices 0 through `len(a)-1`. Array types are always one-dimensional but may be
composed to form multi-dimensional types.

```
[32]byte
[2*N] struct { x, y int32 }
[1000]*float64
[3][5]int
[2][2][2]float64  // same as [2]([2]([2]float64))
```

An array type `T` may not have an element of type `T`, or of a type containing `T` as a component, directly or
indirectly, if those containing types hold their components inline (section 7.6 lists them).

An array variable provides the storage for all its elements. Assigning an array copies every element. Two
arrays are equal if their elements are equal (section 8.10).

```go
func demo() (int, [3]int) {
	a := [3]int{1, 2, 3}
	b := a                // b is a copy
	b[0] = 9              // a[0] is still 1
	return a[0], a        // the array is copied; returning a[:] would escape this frame (E3001)
}
```

Indexing an array with a constant index outside its bounds is a compile error; with any other index out of
range it panics (section 17.1, Panics).

**Arrays of owners.** An array whose element type is an owner is an owner, without any declaration: its
elements move with it and are dropped in index order when it is dropped (section 18.6, Drops). `[4]File` is an
owner if `File` is. A named array type that is an owner must be declared `owner` (section 7.18).

**Kind.** An array has the kind of its element type (section 8.7).

### 7.5 Slice types

A slice is a view of a contiguous segment of an array that it does not own. A slice type denotes the set of
all slices of arrays of its element type. The number of elements is called the length of the slice and is
never negative. The value of an uninitialized slice is `nil`.

```
SliceType = "[" "]" ElementType .
```

A slice never grows. It has no storage of its own: the array behind it belongs to something else (an array
variable, a composite literal, an owner such as a growable vector from the library, or an arena) and a slice
can only look at it. There is no built-in `append`: the name is not predeclared, and a call of `append` in a
program that declares no `append` of its own is a compile error (E2013) that shows the library function to
use (section 13.1, Built-ins Plutonium does not have). A sequence that must grow is held by an owner that grows (the library's
`vec.Vec[T]`), and `v.Slice()` lends its elements as a slice.

The length of a slice `s` can be discovered by the built-in function `len`. It can change by slicing within
the capacity: `s[:len(s)+1]` has one more element if the array extends that far. The elements can be addressed
by integer indices 0 through `len(s)-1`. The slice index of a given element may be less than the index of the
same element in the underlying array.

A slice, once initialized, is always associated with an underlying array that holds its elements. A slice
therefore shares storage with its array and with other slices of the same array; by contrast, distinct arrays
always represent distinct storage. Changing an element of a slice changes that element for every slice that
shares the array.

The array underlying a slice may extend past the end of the slice. The capacity is a measure of that extent:
it is the sum of the length of the slice and the length of the array beyond the slice. A slice of length up to
that capacity can be created by slicing a new one from the original slice. The capacity of a slice `a` can be
discovered using the built-in function `cap(a)`.

```go
func demo() int {
	var a [8]int
	s := a[2:5]         // len 3, cap 6: a view of a[2], a[3], a[4]
	t := s[:6]          // allowed: within cap(s)
	s[0] = 1            // a[2] is 1
	return len(t)
}
```

A slice can be made in these ways:

- by slicing an addressable array, a pointer to an array, or a slice (section 11.12, Slice expressions);
- by a composite literal `[]T{...}`, which creates an array in the frame of the function (or in static storage
  in a package-level initializer) and a slice of it (section 11.3, Composite literals);
- from a library owner or an arena (`v.Slice()`, `a.Slice[T](n)`).

`make` of a slice type is a compile error (E2014): the array it would allocate would belong to no one. The
error shows the library call that returns an owner of the array (`vec.Make[T](n)` and
`vec.WithCapacity[T](c)`).

Like arrays, slices are always one-dimensional but may be composed to construct higher-dimensional objects.
With arrays of arrays, the inner arrays are, by construction, always the same length; however with slices of
slices (or arrays of slices), the inner lengths may vary dynamically. Moreover, the inner slices must be
initialized individually.

A slice may be compared to `nil` and to nothing else (section 8.10).

Because a slice is a view, it may not outlive the array it looks at. The rules that check this are in section
18.3 (Views). A slice whose elements are owners is still a view: it owns neither the array nor the elements.
An element of such a slice cannot be copied out; it is moved only by an assignment that permutes places or by
`take` (section 18.5, Moves).

**Representation.** A slice occupies three words: the address of its first element, its length and its
capacity.

**Kind.** A slice is a view (section 8.7).

### 7.6 Struct types

A struct is a sequence of named elements, called fields, each of which has a name and a type. Field names may
be specified explicitly (IdentifierList) or implicitly (EmbeddedField). Within a struct, non-blank field names
must be unique.

```
StructType    = "struct" "{" { FieldDecl ";" } "}" .
FieldDecl     = (IdentifierList Type | EmbeddedField) [ Tag ] .
EmbeddedField = [ "*" ] TypeName [ TypeArgs ] .
Tag           = string_lit .
```

```
// An empty struct.
struct {}

// A struct with 6 fields.
struct {
	x, y int
	u float32
	_ float32  // padding
	A *[]int
	F func()
}
```

Fields are stored in the order they are declared, each at an offset that satisfies its alignment (section
20.2, Size and alignment guarantees). A blank field `_` takes space and cannot be named.

**Embedded fields.** A field declared with a type but no explicit field name is called an embedded field. An
embedded field must be specified as a type name `T` or as a pointer to a non-interface type name `*T`, and `T`
itself may not be a pointer type or type parameter. It may also be a box type `owned[T]`, `shared[T]` or
`synced[T]` (section 7.19), where `T` is a type name. The unqualified type name acts as the field name; for a
box, the field name is the unqualified name of `T`.

```
// A struct with embedded fields of types T1, *T2, P.T3, *P.T4, and owned[T5].
struct {
	T1             // field name is T1
	*T2            // field name is T2
	P.T3           // field name is T3
	*P.T4          // field name is T4
	owned[T5]      // field name is T5
	x, y int       // field names are x and y
}
```

The following declaration is illegal because field names must be unique in a struct type:

```
struct {
	T     // conflicts with embedded field *T and *P.T
	*T    // conflicts with embedded field T and *P.T
	*P.T  // conflicts with embedded field T and *T
}
```

A field or method `f` of an embedded field in a struct `x` is called **promoted** if `x.f` is a legal selector
that denotes that field or method `f`. Promoted fields act like ordinary fields of a struct. A promoted
field may be a composite-literal key under section 11.3's selector, ambiguity, embedded-pointer and
overlapping-initialization restrictions.

Given a struct type `S` and a type name `T`, promoted methods are included in the method set of the struct as
follows (section 8.6, Method sets):

- If `S` contains an embedded field `T`, the method sets of `S` and `*S` both include promoted methods with
  receiver `T`. The method set of `*S` also includes promoted methods with receiver `*T`.
- If `S` contains an embedded field `*T`, `owned[T]`, `shared[T]` or `synced[T]`, the method sets of `S` and
  `*S` both include promoted methods with receiver `T` or `*T`.

An embedded box promotes the methods of `T` as an embedded `*T` does, and nothing else: the methods that a box
has of its own (`Drop` and `Clone`, section 7.19) are never promoted. The `Drop` method of an embedded owner
is never promoted either (section 7.18).

```go
type Style struct{ bold bool }

func (s *Style) Bold() bool { return s.bold }

type Widget owner struct {
	owned[Style]      // promotes Bold
	name string
}

func demo() bool {
	var w Widget
	w.Style = new(Style{bold: true})
	return w.Bold()   // calls (*Style).Bold on the box's object
}
```

**Tags.** A field declaration may be followed by an optional string literal tag, which becomes an attribute
for all the fields in the corresponding field declaration. An empty tag string is equivalent to an absent tag.
Tags take part in type identity for structs (section 8.3) but are otherwise ignored: they never change the
behavior of the program. The type descriptor of a struct type records them, so that a library can read them.

```
struct {
	x, y float64 ""  // an empty tag string is like an absent tag
	name string  "any string is permitted as a tag"
	_    [4]byte "ceci n'est pas un champ de structure"
}

// A struct corresponding to a TimeStamp protocol buffer.
// The tag strings define the protocol buffer field numbers.
struct {
	microsec  uint64 `protobuf:"1"`
	serverIP6 uint64 `protobuf:"2"`
}
```

**Recursive types.** A struct type `T` may not contain a field of type `T`, or of a type containing `T` as a
component, directly or indirectly, if those containing types hold their components inline. A type holds its
components inline if it is an array, a struct, a union, a `ring`, a `volatile` or an `aligned` type. A
pointer, slice, map, channel, function or interface type, a box (`owned`, `shared`, `synced`) and a `weak`
reference hold their component at an address, so they end the cycle.

```go
// invalid struct types
type (
	T1 struct{ T1 }            // T1 contains a field of T1
	T2 struct{ f [10]T2 }      // T2 contains T2 as component of an array
	T3 struct{ T4 }            // T3 contains T3 as component of an array in struct T4
	T4 struct{ f [10]T3 }      // T4 contains T4 as component of struct T3 in an array
)

// valid struct types
type (
	T5 struct{ f *T5 }         // T5 contains T5 as component of a pointer
	T6 struct{ f func() T6 }   // T6 contains T6 as component of a function type
	T7 struct{ f [10][]T7 }    // T7 contains T7 as component of a slice in an array
	T8 owner struct{ f owned[T8] } // T8 contains T8 behind a box
)
```

The same rule applies to array types (section 7.4) and union types (section 7.13).

**Owner fields.** A struct that has a field of an owner type, including an embedded field, an array of owners
or an `owned[T]`, owns what the field owns. Such a struct must be declared `owner` (E3202); an unnamed struct
type may not have owner fields (E3203). A struct whose fields are values, views and counted values needs no
declaration (section 7.18).

**Kind.** A struct has the strongest kind among its fields, in the order owner, view, counted, value (section
8.7). It is an owner only if it is declared `owner`.

### 7.7 Pointer types

A pointer type denotes the set of all pointers to variables of a given type, called the base type of the
pointer. The value of an uninitialized pointer is `nil`.

```
PointerType = "*" BaseType .
BaseType    = Type .
```

```
*Point
*[4]int
```

A pointer is a view of the variable it points to. It never owns or frees anything, and dropping one does
nothing. It may point to a variable in a frame, in static storage, in memory that an owner or an arena owns,
or to a device register. `&x` is a view of `x` where `x` lives: where a variable lives is fixed by its
declaration and does not change when its address is taken (section 18.2, Where variables live). A pointer may
not outlive the variable it points to; section 18.3 (Views) gives the rules and the errors.

Pointer arithmetic does not exist in the language. Package `unsafe` converts between pointers and integers
(section 20.1, Package unsafe). `unsafe.Pointer` is a pointer type whose base type is unspecified; it carries
the same facts about what it views as the pointer it was made from.

Dereferencing a `nil` pointer faults, and the fault is reported as a panic (section 17.5, Faults). A pointer
to a `volatile[T]` keeps the qualifier: the type of `&u.data` is `*volatile[uint8]` (section 7.15).

A pointer to an owner type is not an owner. It is how a method with a pointer receiver borrows its receiver
(section 18.5, Moves).

**Kind.** A pointer is a view (section 8.7). `unsafe.Pointer` is a view.

### 7.8 Function types

A function type denotes the set of all functions with the same parameter and result types. The value of an
uninitialized variable of function type is `nil`.

```
FunctionType  = "func" Signature .
Signature     = Parameters [ Result ] .
Result        = Parameters | Type .
Parameters    = "(" [ ParameterList [ "," ] ] ")" .
ParameterList = ParameterDecl { "," ParameterDecl } .
ParameterDecl = [ IdentifierList ] [ "..." ] Type [ DefaultValue ] .
```

A `DefaultValue` is allowed only in the parameters of a function or method declaration (section 10.13,
Default parameter values); a function type has none.

Within a list of parameters or results, the names (IdentifierList) must either all be present or all be
absent. If present, each name stands for one item (parameter or result) of the specified type and all
non-blank names in the signature must be unique. If absent, each type stands for one item of that type.
Parameter and result lists are always parenthesized except that if there is exactly one unnamed result it may
be written as an unparenthesized type.

The final incoming parameter in a function signature may have a type prefixed with `...`. A function with such
a parameter is called variadic and may be invoked with zero or more arguments for that parameter.

```
func()
func(x int) int
func(a, _ int, z float32) bool
func(a, b int, z float32) (bool)
func(prefix string, values ...int)
func(a, b int, z float64, opt ...interface{}) (success bool)
func(int, int, float64) (float64, *[]int)
func(n int) func(p *T)
```

**Default values are not part of the type.** The trailing parameters of a declared function or method may have
default values (section 10.13, Default parameter values). The default is a property of the declaration, not of
the type: a function type never has one, so a function value, a method value and a method expression take
every argument, and `func(a int, b int = 2)` is not a function type.

**Function values and closures.** A function value is one word, the address of a **closure record**. The first
word of the record is the address of the code to run. A function value comes from one of these:

- A declared function used as a value, or a function literal that captures no variable. Its record is static.
  It is an ordinary function value and may be used anywhere, like a number.
- A function literal that captures variables of its enclosing function, or a method value `x.M`. Its record is
  built in the frame of the function that evaluates the literal, and holds the address of each captured
  variable (captures are by reference) or a copy of the receiver. Such a closure is a view of that frame: it
  is valid while the frame is.

A closure may be called, passed as an argument, assigned to a local variable and deferred. Storing it in a
package-level variable or through a pointer into memory that may outlive the frame is a compile error (E3002).
A function that returns a closure builds it in a **region** of memory that its caller reserves in its own
frame. The size of a region is fixed when the program is compiled, no allocation takes place, and the closure
is valid as long as the caller's frame is. Section 11.4 (Function literals) and section 11.5 (Closures and
their regions) give the rules and the errors; section 18.3 (Views) gives the escape rules.

**Calling a function value.** A call through a function value passes every argument of its type. A function
value that is `nil` panics when called (section 17.5, Faults). A function value may be compared to `nil` and
to nothing else (section 8.10).

**Parameters and results of owner type.** An argument of owner type is moved into the callee, and a result of
owner type is moved to the caller (section 11.14, Calls).

**Kind.** A function type is a view (section 8.7): a value of the type may be a closure that refers to a
frame, and the compiler checks the actual values with the rules of section 18.3 rather than the type alone. A
function value that captures nothing carries no such restriction.

### 7.9 Interface types

An interface type defines a type set. A variable of interface type can store a value of any type that is in
the type set of the interface. Such a type is said to implement the interface. The value of an uninitialized
variable of interface type is `nil`.

```
InterfaceType  = "interface" "{" { InterfaceElem ";" } "}" .
InterfaceElem  = MethodElem | TypeElem .
MethodElem     = MethodName Signature .
MethodName     = identifier .
TypeElem       = TypeTerm { "|" TypeTerm } .
TypeTerm       = Type | UnderlyingType .
UnderlyingType = "~" Type .
```

An interface type is specified by a list of interface elements. An interface element is either a method or a
type element, where a type element is a union of one or more type terms. A type term is either a single type
or a single underlying type.

**Basic interfaces.** In its most basic form an interface specifies a (possibly empty) list of methods. The
type set defined by such an interface is the set of types which implement all of those methods, and the
corresponding method set consists exactly of the methods specified by the interface. Interfaces whose type
sets can be defined entirely by a list of methods are called basic interfaces. Interface methods cannot
declare type parameters (E1011), but they may use type parameters from the interface declaration. Interface methods
cannot declare default parameter values.

```
// A simple File interface.
interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
}
```

The name of each explicitly specified method must be unique and not blank.

```
interface {
	String() string
	String() string  // illegal: String not unique
	_(x int)         // illegal: method must have non-blank name
}
```

More than one type may implement an interface. For instance, if two types `S1` and `S2` have the method set

```
func (p T) Read(p []byte) (n int, err error)
func (p T) Write(p []byte) (n int, err error)
func (p T) Close() error
```

(where `T` stands for either `S1` or `S2`) then the `File` interface is implemented by both `S1` and `S2`,
regardless of what other methods `S1` and `S2` may have or share.

Every type that is a member of the type set of an interface implements that interface. Any given type may
implement several distinct interfaces. For instance, all types implement the empty interface, which stands for
the set of all (non-interface) types:

```
interface{}
```

For convenience, the predeclared type `any` is an alias for the empty interface; it is not a named type.

**Embedded interfaces.** In a slightly more general form an interface `T` may use a (possibly qualified)
interface type name `E` as an interface element. This is called embedding interface `E` in `T`. The type set
of `T` is the intersection of the type sets defined by `T`'s explicitly declared methods and the type sets of
`T`'s embedded interfaces. In other words, the type set of `T` is the set of all types that implement all the
explicitly declared methods of `T` and also all the methods of `E`.

```go
type Reader interface {
	Read(p []byte) (n int, err error)
	Close() error
}

type Writer interface {
	Write(p []byte) (n int, err error)
	Close() error
}

// ReadWriter's methods are Read, Write, and Close.
type ReadWriter interface {
	Reader  // includes methods of Reader in ReadWriter's method set
	Writer  // includes methods of Writer in ReadWriter's method set
}
```

When embedding interfaces, methods with the same names must have identical signatures.

```go
type ReadCloser interface {
	Reader   // includes methods of Reader in ReadCloser's method set
	Close()  // illegal: signatures of Reader.Close and Close are different
}
```

**General interfaces.** In their most general form, an interface element may also be an arbitrary type term
`T`, or a term of the form `~T` specifying the underlying type `T`, or a union of terms `t1|t2|…|tn`. Together
with method specifications, these elements enable the precise definition of an interface's type set as
follows:

- The type set of the empty interface is the set of all non-interface types.
- The type set of a non-empty interface is the intersection of the type sets of its interface elements.
- The type set of a method specification is the set of all non-interface types whose method sets include that
  method.
- The type set of a non-interface type term is the set consisting of just that type.
- The type set of a term of the form `~T` is the set of all types whose underlying type is `T`.
- The type set of a union of terms `t1|t2|…|tn` is the union of the type sets of the terms.

The quantification "the set of all non-interface types" refers not just to all (non-interface) types declared
in the program at hand, but all possible types in all possible programs, and hence is infinite. Similarly,
given the set of all non-interface types that implement a particular method, the intersection of the method
sets of those types will contain exactly that method, even if all types in the program at hand always pair
that method with another method.

By construction, an interface's type set never contains an interface type.

```
// An interface representing only the type int.
interface {
	int
}

// An interface representing all types with underlying type int.
interface {
	~int
}

// An interface representing all types with underlying type int that implement the String method.
interface {
	~int
	String() string
}

// An interface representing an empty type set: there is no type that is both an int and a string.
interface {
	int
	string
}
```

In a term of the form `~T`, the underlying type of `T` must be itself, and `T` cannot be an interface.

```go
type MyInt int

interface {
	~[]byte  // the underlying type of []byte is itself
	~MyInt   // illegal: the underlying type of MyInt is not MyInt
	~error   // illegal: error is an interface
}
```

Union elements denote unions of type sets:

```go
// The Float interface represents all floating-point types
// (including any named types whose underlying types are
// either float32 or float64).
type Float interface {
	~float32 | ~float64
}
```

The type `T` in a term of the form `T` or `~T` cannot be a type parameter, and the type sets of all
non-interface terms must be pairwise disjoint (the pairwise intersection of the type sets must be empty).
Given a type parameter `P`:

```
interface {
	P                // illegal: P is a type parameter
	int | ~P         // illegal: P is a type parameter
	~int | MyInt     // illegal: the type sets for ~int and MyInt are not disjoint (~int includes MyInt)
	float32 | Float  // overlapping type sets but Float is an interface
}
```

A union (with more than one term) cannot contain the predeclared identifier `comparable` or `portable`, or
interfaces that specify methods, or embed `comparable`, `portable` or interfaces that specify methods.

Interfaces that are not basic may only be used as type constraints, or as elements of other interfaces used as
constraints. They cannot be the types of values or variables, or components of other, non-interface types. The
predeclared `comparable` and `portable` (section 8.9) are not basic interfaces either.

```go
var x Float                     // illegal: Float is not a basic interface

var x interface{} = Float(nil)  // illegal

type Floatish struct {
	f Float                 // illegal
}
```

An interface type `T` may not embed a type element that is, contains, or embeds `T`, directly or indirectly.

```go
// illegal: Bad may not embed itself
type Bad interface {
	Bad
}

// illegal: Bad1 may not embed itself using Bad2
type Bad1 interface {
	Bad2
}
type Bad2 interface {
	Bad1
}

// illegal: Bad3 may not embed a union containing Bad3
type Bad3 interface {
	~int | ~string | Bad3
}

// illegal: Bad4 may not embed an array containing Bad4 as element type
type Bad4 interface {
	[10]Bad4
}
```

**Implementing an interface.** A type `T` implements an interface `I` if

- `T` is not an interface and is an element of the type set of `I`; or
- `T` is an interface and the type set of `T` is a subset of the type set of `I`.

A value of type `T` implements an interface if `T` implements the interface. Whether a type is an element of
the type set defined by methods depends on its method set (section 8.6, Method sets). A method with type
parameters of its own does not count towards implementing an interface (section 10.15, Methods with type
parameters). A method with default parameter values implements an interface method with the same parameter and
result types, since defaults are not part of the signature.

**Ordinary and owning interfaces.** An interface value has no owner of its own, so whether it owns what it
holds comes from its type.

- An **ordinary interface** is any interface type not declared `owner`: `any`, `error`, `io.Reader`. It never
  owns what it holds. Converting an owner, or a counted reference held in one word (a `shared[T]` or
  `synced[T]` reference, a `weak[P]`, a channel or a map), to an ordinary interface **lends** it: the interface is a view of the variable that was converted, valid while that variable holds
  the value, and the variable is not moved. That is what makes `fmt.Println(buf)` work on an owner without
  giving it away. Such an interface follows the view rules of section 18.3 (Views): it cannot be returned or
  stored beyond the life of the variable. The interface `error`, and every interface that embeds it, is the
  one exception for counted references: it counts a `synced[T]` reference it holds, as
  section 16.7 (Allocated errors and ownership) defines; a `shared[T]` cannot be converted to it (section 8.8,
  Sendable types).
- An **owning interface** is declared with the `owner` qualifier (section 7.18):
  `type Resource owner interface { Read(p []byte) (int, error) }`. It is an owner type. It moves, and dropping
  it drops the value inside it. Converting an owner to an owning interface moves the owner in. Converting a
  `shared[T]` or `synced[T]` reference, a `weak[P]`, a channel or a map copies it (count plus one, plain or atomic as
  section 18.7 says). Converting a value or a view copies it, as
  for any interface. Dropping it releases any counted components of that copy; a pure value or pure view
  needs no cleanup. It does not release the memory merely borrowed by a view component (section 8.7).

Every owner type satisfies the implicit `Drop` of an owning interface, whether or not it declares a `Drop`
method. An interface that embeds an owning interface must itself be declared `owner` (E3202).

A type assertion `x.(T)` from an owning interface to an owner type moves the value out of `x`. In the comma-ok
form `v, ok := x.(T)`, `x` is moved only where `ok` is true, which the compiler follows through direct tests of
`ok` (section 11.13). An assertion from an owning interface to an ordinary interface type moves nothing: the
result lends from `x`. A type switch with a binding moves `x` into the binding in every clause except one that
names a single ordinary interface type, where the binding lends from `x` (section 12.9, Switch statements).
Asserting an ordinary interface to an owner type, including an owning interface, is a compile error (E3308),
since the interface only lends the value and the result would be a second owner; assert it to an ordinary
interface type instead and use the value's methods (section 11.13, Type assertions).

A value-receiver method of an owner type consumes its receiver, so it never satisfies an interface method,
ordinary or owning (section 8.6). The conversion is a compile error that shows the fix. A pointer-receiver
method of an owner is reached through `&b` for an ordinary interface, or through a box (`new(b)`) for an
owning one.

```go
type Buf owner struct{ data []byte }

func (b *Buf) String() string { return "buf" }          // pointer receiver: borrows
func (b Buf) Finish() int     { return len(b.data) }     // value receiver: consumes b

func show(b Buf) {
	var s fmt.Stringer = &b   // a *Buf: a view of b, with String
	_ = s
	// var t fmt.Stringer = b // error: Buf has no String in its method set; use &b
	fmt.Println(b)            // lends b to an any; prints its fields
	fmt.Println(&b)           // calls String
}
```

**Representation.** An interface value occupies two words: `tab` and `data`. For the empty interface, `tab` is
the type descriptor of the dynamic type. For other interfaces it is a pointer to a static method table for the
dynamic type and the interface. A nil interface has a zero `tab`. An owning interface's method table has one
more entry after the methods: the drop code of the dynamic type. An owning interface with no methods holds the
type descriptor instead, and the drop code is found there. The layout is in abi.md section 5.

The data word holds the value itself when the dynamic type is one word that the language stores directly:

- an integer, floating-point or boolean value, an enum value, or a packed value of up to eight bytes;
- a pointer, an `unsafe.Pointer`, a function value, a channel, a map, a box (`owned`, `shared`, `synced`) or a
  `weak` reference;
- a struct with exactly one field, or an array of length 1, whose field or element is a single pointer in the
  sense of the second item, or recursively another such struct or array. An error type
  `type wrapErr struct{ d *detail }` can therefore be returned as an `error`, and two interface values holding
  one are equal when they hold the same pointer. A struct with one integer field is not covered: it is copied
  into the frame like any other struct.

A channel or a map in the data word of an ordinary interface is lent, as a `shared[T]` or `synced[T]`
reference is: the interface is a view of the variable that was converted and holds no count of its own. An
`any` made from a channel in a package-level variable may be kept anywhere, while
`func f() any { return make(chan int) }` is a compile error, since the interface would outlive the
temporary channel (section 18.3, Views).

A larger value (a string, a slice, a larger array, struct or union) is copied into a temporary in the frame of
the function that performs the conversion, and the data word points at that copy. The interface value is valid
until that function returns, exactly like a pointer to a local variable. A copy that holds counted values
holds references of its own. For an ordinary interface, the frame copy releases these references when
the function returns or before the same conversion refills that storage. For an owning interface, the
payload's cleanup obligation belongs to the interface: its drop releases the copied counted components
at that point, even before frame return. The frame is only storage, not a second owner of the same
references. Later frame cleanup or refill must not release components already consumed or cleared.
Neither case extends the lifetime of borrowed fields or introduces a hidden heap allocation.

An interface that holds such a frame copy is a view of the frame (section 18.3). Returning it, storing it in a
package-level variable or on the heap, or passing it to a function that keeps it, is a compile error. To keep
a large value in an interface beyond the current function, store a pointer to memory that lives long enough
instead, or move an owner into an owning interface. A value larger than a word that is moved into an owning
interface is copied into the frame like any other, so to return an owning interface, box the owner first:
`new(f)` is an `owned[File]`, one word, and its methods are `*File`'s.

The initializer of a package-level variable is the exception: it runs once and its results must outlive
package initialization, so a conversion there copies the value into static storage instead of a frame.
`var ErrBad error = parseError{line: 1}` is valid (section 15.2, Package initialization).

**Type descriptors.** For every type converted to an interface, and for the types it is made of, the compiler
emits a type descriptor. It records the size, alignment and kind of the type and what the standard library
needs to print, hash and compare it. There is no reflection package.

**Kind.** An ordinary interface is a view. An owning interface is an owner (section 8.7).

### 7.10 Map types

A map is an unordered group of elements of one type, called the element type, indexed by a set of unique keys
of another type, called the key type. The value of an uninitialized map is `nil`.

```
MapType = "map" "[" KeyType "]" ElementType .
KeyType = Type .
```

The comparison operators `==` and `!=` must be fully defined for operands of the key type (section 8.10,
Comparable types); thus the key type must not be a function, map or slice. If the key type is an interface
type, these comparison operators must be defined for the dynamic key values; failure will cause a run-time
panic.

```
map[string]int
map[*T]struct{ x, y float64 }
map[string]interface{}
```

**A counted reference.** A map value is one word, the address of a counted hash table. Copies of a map value
share one table: assigning a map or passing it to a function gives the other side the same table, so that a
function that receives a map and stores into it changes the caller's map. Each copy adds one to the count of
references to the table, and each copy that is dropped subtracts one. The table is freed
when the last reference goes, and the keys and elements in it are dropped first, so counted keys and elements
release their references. The count is plain or atomic by the rule of section 18.7 (Shared ownership, When
counts are atomic); the compiler leaves out counts that cannot be observed, as for strings (section 18.8,
Strings). A map
that holds a counted reference to itself (through an interface, for example) is never freed, like any cycle of
counted references; `weak` breaks cycles (section 7.19).

```go
func fill(m map[string]int) { m["a"] = 1 }

func demo() int {
	m := map[string]int{}
	fill(m)
	m2 := m          // m2 and m refer to one table
	m2["b"] = 2
	return len(m)    // 2
}
```

**Operations.** The number of map elements is called its length. For a map `m`, it can be discovered using the
built-in function `len` and may change during execution. Elements may be added during execution using
assignments and retrieved with index expressions (section 11.10, Index expressions); they may be removed with
the `delete` and `clear` built-in functions (sections 13.5 and 13.3). An index expression on a missing key
yields the zero value of the element type, and the comma-ok form reports whether the key was present. A map
element is not a variable: `&m[k]` is invalid and so is assigning to a field of `m[k]`.

A nil map is equivalent to an empty map for reading, `len` and `range`. Storing into a nil map panics (section
17.1, Panics).

**Making maps.** A new, empty map value is made using the built-in function `make`, which takes the map type,
an optional capacity hint and an optional allocator (section 13.7, Making channels and maps):

```
make(map[string]int)
make(map[string]int, 100)
make(map[string]int, 100, arena)
```

The initial capacity does not bound the size of a map: maps grow to accommodate the number of items stored in
them, with the exception of nil maps. The final argument is a capacity if it is an integer or an untyped
numeric constant, and an allocator otherwise, as for channels. A composite literal `map[K]V{...}` allocates
the table from the current allocator (section 18.9, Allocators), as `make(map[K]V)` does without an allocator
argument.

**Iteration.** The order in which a `range` clause visits the entries of a map is unspecified and may differ
from one iteration to the next. Section 12.10 (For statements) states which entries a loop visits when the
body adds or deletes entries. The implementation visits entries in the order of their slots in the table,
which depends on the keys' hashes. Each map has its own hash seed: in hosted programs the seeds come from
random bytes the operating system gives the program, so keys chosen by an attacker cannot be made to collide
and the order differs from run to run; freestanding and kernel programs start from a fixed seed, so the
order is the same in every run for the same operations, unless the kernel gives the runtime entropy with
`rt.SetMapSeed`.

**Owners are not keys or elements.** Neither the key type nor the element type of a map may be an owner.
Reading an element copies it, and copying an owner would make a second owner. The error shows how to keep
owners in a map: make the element a counted box, `map[K]synced[T]` (or `map[K]shared[T]` for a map that
stays in one task), whose copies count instead of duplicating the owner. Counted values (strings, `shared`,
`synced`, channels and maps) are allowed and are counted as they are copied.

**Concurrency.** A map is sendable if its key and element types are (section 8.8). Its contents fall under the
race rules of section 19.3 (Data race freedom), as the contents of a `synced[T]` do: writes from two tasks
must be guarded by a lock that the compiler infers, or confined to one task.

The library's `hash.Map[K, V]` is an owner, for code that must hold owners in place or avoid counting.

**Kind.** A map is a counted value (section 8.7).

### 7.11 Channel types

A channel provides a mechanism for concurrently executing functions to communicate by sending and receiving
values of a specified element type. The value of an uninitialized channel is `nil`.

```
ChannelType = ( "chan" | "chan" "<-" | "<-" "chan" ) ElementType .
```

The optional `<-` operator specifies the channel direction, send or receive. If a direction is given, the
channel is directional, otherwise it is bidirectional. A channel may be constrained only to send or only to
receive by assignment or explicit conversion.

```
chan T          // can be used to send and receive values of type T
chan<- float64  // can only be used to send float64s
<-chan int      // can only be used to receive ints
```

The `<-` operator associates with the leftmost `chan` possible:

```
chan<- chan int    // same as chan<- (chan int)
chan<- <-chan int  // same as chan<- (<-chan int)
<-chan <-chan int  // same as <-chan (<-chan int)
chan (<-chan int)
```

The element type of a channel must be sendable (section 8.8, Sendable types). A channel whose element type
contains a `shared[T]` is a compile error (E2010), since values sent on it would be shared between tasks with
a count that is not atomic.

A new, initialized channel value can be made using the built-in function `make`, which takes the channel type,
an optional capacity and an optional allocator as arguments (section 13.7, Making channels and maps):

```
make(chan int, 100)
make(chan Msg, 16, arena)
```

The capacity, in number of elements, sets the size of the buffer in the channel. If the capacity is zero or
absent, the channel is unbuffered and communication succeeds only when both a sender and receiver are ready.
Otherwise, the channel is buffered and communication succeeds without blocking if the buffer is not full
(sends) or not empty (receives). A nil channel is never ready for communication. The channel and its buffer
are allocated from the current allocator, or from the allocator given as the final argument.

A channel may be closed with the built-in function `close` (section 13.4, Close). The multi-valued assignment
form of the receive operator reports whether a received value was sent before the channel was closed. Sending
on a closed channel panics, and so does closing a closed or nil channel. Receiving from a closed channel
yields the zero value and `false`.

A single channel may be used in send statements, receive operations, and calls to the built-in functions `cap`
and `len` by any number of tasks without further synchronization. Channels act as first-in-first-out queues.
For example, if one task sends values on a channel and a second task receives them, the values are received in
the order sent.

**A counted reference.** A channel value is one word, a counted reference to the channel, because a channel
always has several users. Copying a channel value adds a reference and dropping one subtracts a reference; the count is
plain or atomic by the rule of section 18.7 (Shared ownership, When counts are atomic). When the
last reference goes, the values still in the buffer are dropped, so owners in the buffer are freed rather than
leaked and their `Drop` runs on the task that released the last reference, and then the channel and its buffer
are freed. A channel made for a timer owns the timer and stops it when the channel is freed (section 19.6,
Timers).

**Sending owners.** Sending a value of owner type moves it: after `ch <- b`, `b` holds nothing, and the
receiver of `v := <-ch` owns the value (sections 12.5 and 11.23). In a `select`, a send case moves an owner
only if that case proceeds (section 12.13, Select statements).

**Kind.** A channel is a counted value (section 8.7). Channels are sendable (section 8.8).

### 7.12 Enums

An enum type is a closed set of named integer constants of its own type. `enum` is a keyword.

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

type Errno enum uint16 {
	OK     = 0
	EPERM               // 1
	ENOENT              // 2
	EIO    = 5
}
```

The `enum { ... }` declaration form may only appear in a type definition: not directly on the right of
an alias declaration, not generic, and not written elsewhere (E2301, E2302). An alias of an existing
named enum type is permitted and preserves its identity and members (section 10.8).

**Type and values.** An enum is a defined type whose underlying type is an integer type: the type after `enum`
(which must be an integer type, E2303), or else the smallest of `uint8` to `uint64` that holds every member,
or of `int8` to `int64` if a member is negative. The first member is 0 unless given a value; a member without
a value is one more than the previous member. A value is a constant integer expression, evaluated in the scope
enclosing the declaration; it must be representable in the underlying type and may not refer to the enum being
declared. The predeclared identifier `iota` is not defined in a member's value, and using it there is an
error (E0036): a member without a value is already one more than the previous. Two members may not share a name or a
value, `_` is not a member name, and **one member must have the value 0**: it is the zero value of the type.
An enum has at least one member.

**Names.** A member is selected through its type, `Color.Red` (also `pkg.Color.Red`, or through an alias); it
is a typed constant of type `Color`. Members enter no scope. Unexported members are not visible in other
packages. On a type name a member takes precedence over a method of the same name.

**Switches.** In an expression switch whose tag has enum type `E`, a case expression that is a single
identifier naming a member of `E` denotes that member, whatever else the name denotes in scope:
`case Red, Green:`. Bare member names are not accepted anywhere else. A switch on an enum value that has no
`default` case must list every member among its constant case values; otherwise it is a compile error naming
the missing members (E2312). Such an exhaustive switch is a terminating statement when every clause is
(section 12.1, Terminating statements), and it panics for a value that is not a member (section 12.9,
Switch statements).

**Operations.** Enum values compare with `==`, `!=`, `<`, `<=`, `>`, `>=`. Arithmetic, bitwise operators,
`++`, `--` and shifting an enum value are errors (E2316); an enum value may be a shift count or an index. An
untyped constant is not assignable to an enum: `var c Color = 1` and `c == 1` are errors (E2315; section 8.4,
Assignability).

**Conversions.** Section 11.24 (Conversions) gives the rules. In summary:

- `E(c)` for an integer constant `c` is valid if `c` is a member's value, and gives that member.
- For a non-constant integer `n`, `E(n)` is valid only in the comma-ok form `v, ok := E(n)`: `ok` reports
  whether `n` equals a member's value, and `v` is that member, or the zero value if `ok` is false. Floats and
  other non-integer types do not convert to an enum.
- An enum type is not an integer type for these rules, so a value of another enum type does not convert
  directly: `Shade(color)` is an error, and `v, ok := Shade(uint8(color))` goes through the integer.
- An enum converts to any integer or floating-point type as its underlying integer does.

**Generated operations.**

| Form | Meaning |
|---|---|
| `x.String()` | the member's name, or `"E(?)"` for a value that is not a member; generated unless `E` declares `String` |
| `len(E)` | the number of members, an untyped integer constant |
| `for v := range E` | each member in declaration order; one iteration variable |

**Other types.** An enum type is not an integer type. It has the operations and conversions above, and
is an integer only in its representation: an enum value is stored like its underlying type, held
directly in an interface's data word, satisfies constraints by its underlying type (`~uint8`), and may be the
element of a `volatile` or `aligned` type. A packed field of enum type (section 7.14) is a bit field as wide
as the members need (unsigned, or two's complement if a member is negative); `f bits[E, N]` gives a wider
field. A value that is not a member can arise only through `unsafe`, packed values converted from raw bits,
and generic code operating on the underlying type. Such a value matches no member's case: a switch with a
`default` case runs the default, and an exhaustive switch without one panics (section 12.9). `type F E`
declares an ordinary integer type with `E`'s underlying type and no members.

Enums do not provide bit-flag sets; packed `bits` groups do that (section 7.14).

```go
type Level enum {
	Debug
	Info
	Warn
}

func name(l Level) string {
	switch l {
	case Debug:
		return "debug"
	case Info:
		return "info"
	case Warn:
		return "warn"
	}
}
```

**Kind.** An enum is a value. It cannot be declared `owner`; a handle that owns a resource is a struct
declared `owner` with an enum field (section 8.7).

### 7.13 Unions

A union type (a tagged union, or sum type) holds exactly one of a closed set of named variants, together with
that variant's payload, stored inline: no allocation and no boxing, so a union can be returned, stored in
package variables and put in arrays like a struct. It is the sibling of the enum, whose members have no
payloads.

```
UnionType       = "union" "{" { VariantDecl ";" } "}" .
VariantDecl     = identifier { "," identifier } Type | identifier .
PackedUnionType = "packed" "union" [ "le" | "be" ] Type "{" TagField ";" { PackedVariant ";" } "}" .
TagField        = identifier PackedFieldType .
PackedVariant   = identifier "case" ExpressionList "{" { PackedFieldDecl ";" } "}" .
```

`PackedFieldType` and `PackedFieldDecl` are defined in section 7.14.

```go
type DeviceState union {
	Off                                  // no payload
	Probing struct{ attempt int }
	Ready   struct {
		irq  uint8
		base uintptr
	}
	Failed Errno
}

var st DeviceState                       // zero value: Off, the first variant
st = DeviceState.Probing{attempt: 1}
st = DeviceState.Failed(Errno.EIO)

switch s := st.(type) {
case Off:
	start()
case Probing:
	retry(s.attempt)                     // s is the payload, a struct{ attempt int }
case Ready:
	enableIRQ(s.irq)
case Failed:
	log("device failed:", s)             // s is an Errno
}                                        // a missing case is a compile error
```

**Declaration.** The `union { ... }` declaration syntax appears only in a type definition (E2401).
An alias of an existing named union is permitted and preserves identity and variants (section 10.8). It may have type parameters; a packed union may not (E2402). `union`
is not a keyword: it begins a union type only as the type in a type declaration followed by `{`, or after
`packed`, so code that uses `union` as an identifier still compiles. Variants are written like struct fields:
a name and a payload type, or a name alone for a variant without a payload; `A, B T` declares two variants
with the same payload type. A union has at least one variant (E2403); variant names are distinct, not `_`, and
carry no tags (E2404). A payload may be any type, including an enum, an interface, a type parameter or another
union; a union cannot contain itself except through a type that holds its component at an address (section
7.6, Recursive types). Unexported variants are not visible in other packages.

**Construction.** For a variant `V` of union type `U`:

- without a payload, `U.V` is a value of type `U`;
- with a payload of type `P`, `U.V(x)` makes a `U` holding `x`, which must be assignable to `P` (as a function
  argument); `U.V{...}` is a composite literal of `P` making a `U`; and `U.V` alone is a function value of
  type `func(P) U`.

`U` may be qualified by a package or instantiated: `pkg.U.V`, `Result[int].Ok(5)`. On a type name a variant
takes precedence over a method of the same name. A variant is not a type: `var x U.V` is an error (E2409), and
a payload, once selected, has its own payload type. `U{...}` is an error (E2412), as is `U.V{}` for a variant
without a payload. There are no implicit conversions into a union: `var u U = x` and `u = nil` are errors, and
a value of another union type with identical variants converts only explicitly (section 8.4, Assignability),
and, when the result is an owner union, only within the package limits of section 11.24 (Conversions).

**Defined from a union.** `type V U`, where `U` is a union type, declares a new union type with the same
variants, in the same order, with the same payloads and layout, and none of `U`'s methods, as `type F E`
does for an enum (section 7.12). The variants are selected through `V`: `V.Circle(1)` is a `V`, and a type
switch on a `V` is a union switch with the same exhaustiveness rule. `V` and `U` have identical variants,
so they convert to each other explicitly and never implicitly (above). `V` is an owner union if `U` is,
or if the declaration says `owner` (section 7.18). The same holds for a packed union, which keeps its tag
field, backing and bit layout.

**Zero value.** The first variant, with its payload's zero value. Put the idle or empty state first.

**Type switches.** `switch v := u.(type)` on a union value selects the variant `u` holds. Each case names
variants, as `U.V` or by the bare name `V`; in a clause naming one variant, `v` has that variant's payload
type. A switch without a `default` case must name every variant, or it is a compile error that lists the
missing ones (E2414). Such an exhaustive switch is a terminating statement when every clause is, so a function
may end with it (section 12.1). Section 12.9 (Switch statements) gives the full rules, including the panic for
a tag that names no variant, which only `unsafe` code can make.

**Assertions.** `u.(U.V)` yields the payload of `V`, and panics (section 17.1, Panics) if `u` holds another
variant; `p, ok := u.(U.V)` reports instead, with `p` the zero payload when `ok` is false. A bare variant name
is not accepted here (section 11.13, Type assertions).

**Operations.** A union is comparable if every payload type is; two values are equal when they hold the same
variant with equal payloads (section 8.10). A union type may have methods and implement interfaces. Converting
a union to an interface copies it into the frame like a struct (section 7.9); a type switch on the interface
sees the union type, and a second switch on the union selects the variant. `fmt` prints a union that has no
`String` method as its variant and payload: `Some(5)`, `Err(no such file or directory)`, `None`.

**Generics.** Every instantiation of a generic union has the same variants, so a switch on a `Result[T]` in
generic code is checked once for every `T`, and payload types may coincide (`Result[Errno]`). Variants are
selected through the instance, `Result[int].Ok(5)`, or `Result[T].Ok(v)` in generic code. A union may be a
type argument; unions cannot appear in constraints, where `A | B` keeps its meaning as a union of type terms
(section 7.9).

**Layout.** A union is its tag, a `uint8` (a `uint16` for more than 256 variants) numbering the variants from
0 in declaration order, followed by the payload of the variant it holds at the first offset that satisfies the
largest payload alignment; its size is rounded up to its alignment. `DeviceState` above is 24 bytes. A variant
without a payload takes no space beyond the tag. The layout is not part of the C ABI: unions are not allowed
in `//pu:extern` or `//pu:export` signatures (section 20.4, Assembly and C).

**Packed unions.** A packed union gives a bit-exact layout for hardware words and records whose shape depends
on a tag bit:

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

func frameOf(pte PTE) uintptr {
	switch e := pte.(type) {
	case PTE.Mapped:
		return uintptr(e.frame) << 12
	case PTE.Swapped:
		return 0
	}
}
```

- The one field before the first variant is the tag: a `bool`, integer or enum field (section 7.14) that
  occupies the same bits in every variant. A packed union has a backing type, which fixes its size.
- Each variant lists the constant tag values that select it (`case false`, `case 1, 2`), each of the tag
  field's type and fitting its width (E2407, E2408); no value selects two variants. Its fields follow the tag,
  laid out by the packed rules of section 7.14.
- A variant's payload is a packed type with the same storage holding the tag and the variant's fields: in the
  switch above, `e.present` and `e.frame` are both fields of `e`. `PTE.Mapped(e)` converts a payload back,
  keeping its tag if it is one of the variant's values and otherwise setting the first; `PTE.Mapped{frame: f}`
  sets the tag to the variant's first value and may not set it itself.
- A packed union converts to and from its backing type or a byte array of its size, like any packed type, and
  is comparable as its bits. Its tag is readable as a field (`pte.present`); a variant's fields are not.
- A type switch without a `default` case must cover every value the tag field can hold, since hardware may
  hold any bit pattern; `PTE` above needs none, since its one-bit tag has two values (E2415).

**Owner unions.** A union whose payloads are all values is a value. A union with a variant whose payload is an
owner (a type declared `owner`, or `owned[T]`) must be declared `owner union`, like a struct with an owner
field (E2406):

```go
type Slot owner union {
	Empty
	Open File            // File is an owner
	Boxed owned[Node]
	Code int
}
```

An owner union moves instead of being copied. Dropping it drops the payload of the variant it holds, and
giving it a new variant drops the old payload first. A type switch with a binding on an owner union variable
(`switch v := s.(type)`) moves the payload into the binding of the clause that runs, which drops it at the end
of the clause unless it moves on; the variable is empty afterwards. The comma-ok assertion
`f, ok := s.(Slot.Open)` moves the payload out only where `ok` is true; where it is false `s` keeps it
(section 11.13 says how the compiler follows `ok`). A
union whose payloads hold counted values (`shared`, `synced`, `weak`, strings, channels, maps) and no owner
copies, counting each copy, as a struct of them does.

Updating a variant's payload in place through a pointer is not part of the language; assign a new variant
instead.

**Kind.** A union has the strongest kind among its payloads (section 8.7). It is an owner only if it is
declared `owner`.

### 7.14 `packed` types

A packed type lays out fields at exact bit positions, for hardware registers, descriptor tables, on-disk
structures and protocol headers. `packed` is a keyword.

```
PackedType      = "packed" [ "le" | "be" ] [ Backing ] "{" { PackedFieldDecl ";" } "}" .
Backing         = Type .
PackedFieldDecl = ( identifier | "_" ) PackedFieldType .
PackedFieldType = BitField | BitGroup | Type .
BitField        = "bits" "[" Type "," Expression "]" .
BitGroup        = "bits" "{" identifier { identifier } "}" .
```

A packed type may only be the type in a type definition (section 10.8, Type declarations).

```go
type ControlReg packed uint32 {
	mode     bits[uint, 4]
	enable   bits[bool, 1]
	priority bits[uint, 3]
	_        bits[uint, 24]
}

type IPv4Header packed be [20]byte {
	ihl      bits[uint8, 4]
	version  bits[uint8, 4]
	tos      uint8
	length   uint16
	id       uint16
	frag     Frag
	_        [12]byte
}

type Frag packed be uint16 {
	offset bits[uint16, 13]
	flags  bits[uint8, 3]
}

func demo(raw uint32) uint32 {
	reg := ControlReg(raw)   // a raw register value becomes a typed view
	reg.enable = true
	return uint32(reg)       // the reserved bits of raw are returned unchanged
}
```

- **Backing** is an integer type, a byte array `[N]byte`, or `[]byte`; it may be omitted. With an integer or
  array backing the type is exactly that size and the fields must fit (E2101, E2102). With `[]byte` or no
  backing the size is the total field width rounded up to whole bytes.
- **Layout** is least-significant-bit first in declaration order: the first field starts at bit 0. There is no
  padding between fields except where a field's type is `aligned[T, N]`, which moves it to an `N`-byte
  boundary.
- **Fields** are one of:
  - `name bits[T, N]`: an `N`-bit field read as `T`. `T` is an integer type at least `N` bits wide, an enum type whose members fit in `N` bits, or `bool`
    with `N` equal to 1 (E2103). Unsigned fields read zero-extended, signed fields sign-extended. A write
    keeps the low `N` bits of the value.
  - `name bits { A B C }`: a group of one-bit `bool` flags, accessed as `x.name.A`.
  - `name T`, where `T` is an integer, boolean, floating-point or packed type or an array of those: a
    byte-level field of `T`'s full size, at the next bit position (it need not be byte-aligned, except in a
    big-endian array-backed type; see Byte order).
  - `name E`, where `E` is an enum type: a bit field as wide as the members of `E` need (unsigned, or two's complement if a member is negative), as section 7.12 (Enums) says.
  - `name aligned[T, N]`, where `T` is one of the types of the byte-level bullet: a byte-level field of `T` that starts on an `N`-byte boundary (see Layout).
  - `_` names reserve bits. The **reserved bits** of a packed type are the bits of its `_` fields and the bits
    of the backing that no field covers.
- **Byte order.** `le` and `be` say how the backing is stored in memory. With neither, the target's native
  order applies, and accesses are plain loads, masks and shifts. The rule for field positions depends on the
  backing.
  - *Integer backing.* Field bit positions count from the least significant bit of the value after
    byte-swapping.
  - *Array backing* (`[N]byte`, `[]byte`, or no backing). Bit position `p` is bit `p mod 8` of byte `p / 8`,
    where bit 0 is the least significant. A bit field occupies consecutive positions, its least significant
    bit at the lowest. A byte-level field of `k` bytes is stored in its bytes in the stated order: with `be`,
    most significant byte first; with `le`, least significant byte first; with neither, in the target's order.
    In a big-endian packed type with an array backing (`be`, or no order on a big-endian target), a bit field
    may not cross a byte boundary and a byte-level field must start on one (E2021). The fix is a nested
    packed field with an integer backing, as `frag Frag` does in the example: the field is then defined by the
    integer rule.
- **Conversions.** A packed type converts to and from an integer type of the same size, or a byte array of the
  same length (section 11.24, Conversions). Every bit is kept in both directions, the reserved bits included,
  so converting back returns the same value. That is how a raw register value becomes a typed view. Writing a
  field changes only that field's bits.
- **Defined types.** `type R2 ControlReg` declares a new packed type with the same fields, backing, byte
  order and bit layout, and none of `ControlReg`'s methods. `R2` and `ControlReg` convert to each other
  explicitly, every bit kept, and each converts to and from the integer or byte array as above.
- **Literals** name their fields: `ControlReg{mode: 5, enable: true}`. The reserved bits are zero in a literal
  and in the zero value.
- **Comparison.** Packed values are comparable. Two packed values are equal when their bits outside the
  reserved bits are equal, compared bit by bit, so a floating-point field compares by its bits (section 8.10).

A packed value is a single memory location: writing one field rewrites its neighbors, so two tasks that write
different fields of one packed value race (section 19.7, Memory model).

```go
type StatusReg packed uint32 {
	flags bits {
		ENABLE
		READY
		ERROR
	}
	mode bits[uint, 4]
}

func ready(r StatusReg) bool { return r.flags.READY }
```

**Kind.** A packed type is a value (section 8.7).

### 7.15 `volatile[T]`

```
VolatileType = "volatile" "[" Type "]" .
```

A variable of type `volatile[T]` is accessed with exactly one load or store per read or write, in program
order. The compiler never removes, merges or reorders volatile accesses, and never moves other memory accesses
across them. `T` must be an integer type, boolean type, pointer type (including `unsafe.Pointer`) or
integer-backed packed type, or a defined type with one of these as its underlying type, enums included, but not an owner type
(E2108).

```go
type UART struct {
	data   volatile[uint8]
	status volatile[uint8]
}

func put(u *UART, b uint8) {
	for u.status&0x20 == 0 {
	}
	u.data = b
}
```

Reading a `volatile[T]` yields a `T`, and any value assignable to `T` may be assigned to it. Taking the
address keeps the qualifier: `&u.data` has type `*volatile[uint8]`. A pointer to memory-mapped input and
output is made with a conversion:

```go
func eoi() {
	lapic := (*volatile[uint32])(unsafe.Pointer(uintptr(0xFEE000B0)))
	*lapic = 0
}
```

Volatile is not atomic and implies no barrier between processors. Use `pluto/sync/atomic` for that. Volatile
accesses exclude only the same processor, so they let an interrupt handler and the code it interrupts share
per-CPU data (section 20.9, Per-CPU variables). Data that a handler may share with code on any processor must
be atomic or guarded by an `rt.Lock` (section 19.3, Data race freedom).

`volatile[T]` and `T` have the same size and alignment. A `volatile[T]` is comparable if `T` is (section
8.10).

**Kind.** `volatile[T]` has the kind of `T`: a value, or a view if `T` is a pointer (section 8.7).

### 7.16 `aligned[T, N]`

```
AlignedType = "aligned" "[" Type "," Expression "]" .
```

`aligned[T, N]` stores a `T` on an `N`-byte boundary. `N` must be a positive constant power of two (E2107,
E2109). The type is `N`-aligned wherever it appears: in a package variable, a local variable, a struct field,
an array element or an allocation. Its size is `T`'s size rounded up to a multiple of `N`. As with `volatile`,
it reads and writes as `T`.

```go
var pml4 aligned[[512]uint64, 4096]
var dma  aligned[[2048]byte, 64]
```

An `aligned[T, N]` is comparable if `T` is (section 8.10). Within a packed type, a field of type
`aligned[T, N]` moves the field to an `N`-byte boundary (section 7.14).

**Kind.** `aligned[T, N]` has the kind of `T`. It is an owner if `T` is an owner, without a declaration
(section 8.7).

### 7.17 `ring[T, N]`

```
RingType = "ring" "[" Type "," Expression "]" .
```

`ring[T, N]` is a fixed-capacity ring buffer stored inline: two `uint64` indexes followed by `[N]T`. `N` must
be a positive constant power of two (E2107, E2109). The zero value is an empty ring.

| Method | Result |
|---|---|
| `Push(v T) bool` | appends `v`; `false` if full |
| `Pop() (T, bool)` | removes the oldest element; `false` if empty |
| `Peek() (T, bool)` | the oldest element without removing it; `(*T, bool)`, a view, when `T` is an owner |
| `Len() int` | number of elements |
| `Cap() int` | `N` |
| `Full() bool`, `Empty() bool` | |
| `Reset()` | empties the ring |
| `All() iter.Seq2[int, T]` | an iterator over the index and value of each element, oldest first; `iter.Seq2[int, *T]` when `T` is an owner |
| `Values() iter.Seq[T]` | an iterator over the values, oldest first; `iter.Seq[*T]` when `T` is an owner |

All methods have pointer receivers. A ring is not safe for concurrent use; a single-producer, single-consumer
queue between an interrupt handler and a task can be built from `volatile` indexes. A ring literal must be
empty (E2110). A ring is not comparable.

`All` and `Values` are iterators for `for` loops over functions (section 12.11, Range over functions). A call
of one builds its closure, which holds the address of the ring, in the caller's frame; used as a method value
or a method expression, or called through an interface, it builds it in the region its caller provides, as any
function that returns a closure does (section 11.5, Closures and their regions). Their results have the type
`func(yield func(int, T) bool)` and `func(yield func(T) bool)`, which are assignable to `iter.Seq2[int, T]`
and `iter.Seq[T]`.

```go
func demo() {
	var r ring[int, 4]
	r.Push(10)
	r.Push(20)
	for i, x := range r.All() { // 0 10, then 1 20
		fmt.Println(i, x)
	}
	for i, x := range r { // the same: a ring ranges over its All method (section 12.10)
		fmt.Println(i, x)
	}
}
```

Since `All` has a pointer receiver, `range r` needs an addressable ring or a pointer to one.

The loop reads the ring as it goes, so the body may call `Push`, `Pop` and `Reset`. The loop keeps a position
among the elements ever pushed (the ring counts them, from the last `Reset`), starting at the oldest element
when the loop starts. On each step:

- If the body popped the element at the position, the loop moves on to the oldest element left.
- If the position has reached the end, nothing more has been pushed and the loop ends. Elements the body
  pushed are visited, as long as it finds room for them.
- Otherwise the loop yields the element, and the position moves on.

The index `All` yields is the element's place in the ring when it is yielded, so 0 is the oldest element now:
a body that pops each element as it is visited sees index 0 every time. A body that calls `Reset` should leave
the loop: the position then counts the pushes since the reset, so the loop ends, or goes on from where it was
among the elements pushed after the reset.

A ring of owners is an owner: popping moves an element out, and dropping the ring drops the elements it holds.
`Pop` moves the oldest element out of the ring (section 18.5, Moves); `Peek`, `All` and `Values` yield views (`*T`) of an owner element, as section 12.10 (Owners in iterators) says.

**Kind.** `ring[T, N]` has the kind of `T` (section 8.7).

### 7.18 Owner types

An **owner type** is a type whose values hold memory or another resource and release it when they are dropped.
A value of owner type has exactly one holder. Assigning it **moves** it to the new place, and the place it
left holds nothing. Copies are made only by calling `Clone`. The rules for moves and drops are in section 18.5
(Moves) and section 18.6 (Drops). This section says which types are owners and where the word `owner` may
appear.

`owner` is a contextual keyword (section 4.6, Contextual keywords). It precedes the type in a type definition,
as the production `DefinedType` at the start of this chapter shows, and the constraint in a type parameter
declaration (section 10.9, Type parameter declarations).

```go
type File owner struct {
	fd   int32
	open bool
}

func (f *File) Drop() {
	if f.open {
		syscall.Close(f.fd)
	}
}

type Frame owner uintptr            // a physical page frame, returned to the page allocator on Drop
type Pages owner [4]Frame
type Vec[T owner any] owner struct { data []T }
type Resource owner interface { Read(p []byte) (int, error) }
type Log owner File                 // dropping a Log closes the file
```

A declared owner type is a defined type. Constructed types can also be owners without a new definition.
A type is an owner only if it is declared so, or if its construction makes it one:

- **Declared.** `type T owner ...` makes `T` an owner. The qualifier may mark a struct type, an array type, an
  integer type (including `uintptr`), a pointer type or `unsafe.Pointer`, an interface type (an owning
  interface, section 7.9), a union type (an owner union, section 7.13), a `ring[T, N]` or `aligned[T, N]` type, or a named type of one of these. It
  may not mark a string, slice, map, channel or function type, a boolean, float, enum, packed or `volatile`
  type, or `owned`, `shared`, `synced` or `weak`, which have kinds of their own (E3204). A resource behind one
  of those types is wrapped in a struct declared `owner`.
- **Only in a type definition.** `owner` appears only in `type T owner ...`, including a generic type. An
  alias `type A = T` has the kind of `T` and takes no `owner` of its own (E3205). A type literal elsewhere
  cannot be marked.
- **Never inferred for a declared type.** The keyword makes a type whose values move visible where it is
  declared, so a change to a field's type elsewhere is reported at this declaration (E3202) and not as move
  errors at the uses of the type. A struct with a field of owner type, including an embedded field or
  an array of owners, must itself be declared `owner` (E3202), and so must an interface that embeds an owning
  interface. A type definition `type B A` where `A` is an owner type other than `owned[T]` must be written `type B owner A`. Where `A` is `owned[T]`, which has a kind of its own, `type B A` defines an owner and `owner` may not be written (E3204). A union with
  an owner payload must be declared `owner` (E2406). An unnamed struct type may not have owner fields (E3203).
- **Implied for a constructed type.** An unnamed array type `[N]T` whose element is an owner, a `ring[T, N]`
  whose element is an owner, and an `aligned[T, N]` of an owner are owners without a declaration, because
  their element type already says so. So is `owned[T]` for every `T` (section 7.19).
- **Defined from an owner.** In `type B owner A`, where `A` is an owner type, a value of `B` holds an `A`.
  Dropping it runs `B`'s `Drop` if `B` declares one, and then drops the value as an `A`: `A`'s `Drop`, then
  `A`'s fields. `B` has none of `A`'s methods (section 10.8, Type declarations); the drop of `A` is part of
  `B`'s drop glue.
- **`Drop`.** A method named `Drop` must have no parameters and no results (E3206), and may be declared only
  on a type declared `owner`, with a pointer receiver `*T` (E3201). An owner without a `Drop` method is
  valid: its values still move, and dropping one drops its fields. `Drop` is called by the compiler when an
  owner is dropped. `x.Drop()` on a whole owner variable drops it now (section 18.6), for every owner type,
  whether or not the type declares a `Drop` method; it is not a method value and does not put `Drop` in the
  method set.
- **Generics.** A type parameter admits owner types only if its declaration carries `owner`: `[T owner any]`,
  `[T owner io.Reader]` (E3207). An owning interface as the constraint does not by itself make the parameter an owner parameter. Inside such code, values of `T` move: assignment, passing and returning move
  them, using one after a move is an error, and a variable of type `T` still holding a value at the end of its
  scope is dropped (which does nothing for an instantiation with a value type). A type parameter without
  `owner` cannot be instantiated with an owner type, so ordinary generic code such as `Max[T cmp.Ordered]`
  keeps copying freely. A generic type that stores values of an `owner` type parameter must be declared
  `owner`; all of its instantiations then move, including `vec.Vec[int]`.
- **Embedding.** The methods of an embedded owner are promoted as in section 7.6, except `Drop`, which is
  never promoted: the outer type's own `Drop` (if any) runs, and then the embedded field is dropped like any
  other field.
- **Interfaces.** An owning interface is declared `type R owner interface{...}`. Its type set is the same as
  that of the interface without `owner`, and it holds and drops the value inside it (section 7.9).

The library's `vec.Vec[T]`, `hash.Map[K, V]`, `strings.Builder` and `os.File` are declared `owner`.

**Conversions and comparison.** A value of non-owner type converts into an owner type only in the package that
declares the owner type; an owner converts to a non-owner type by reading its value, without moving it; and
two owner types with the same underlying type convert by moving, only in the package that declares the result
type or in the package of an operand type defined from it. Pointer conversions that reach an owner and union
conversions that make one have the same limits (E3212; section 11.24, Conversions). An owner type is
comparable when its underlying type is, and comparing reads the operands without moving them (section 8.10,
Comparable types). No other operator applies to an owner, even one with a numeric underlying type: the
arithmetic is done on the value read out by a conversion, `int32(fd) + 1` (section 11.18, Operators).

**Literals.** A composite literal of an owner struct type makes a new owner, so its limit follows from the fields
the type exports. The exported fields of an owner struct make its literal public: in another package a literal
`p.Region{Base: b, Len: n}` is valid when every field of `Region` is exported, and is an error (E3212) when any
field is unexported, keyed or not, since the omitted unexported fields could hold what the type's invariants
need. The exported variants of an owner union are public in the same way. A package that wants its owner made
only through its own functions gives the type an unexported field.

**The zero value** of an owner holds nothing, and dropping it does nothing (section 15.1, The zero value). A
`Drop` method must treat the zero value as empty.

**Kind.** An owner type is an owner (section 8.7).

### 7.19 `owned`, `shared`, `synced` and `weak`

Pointers `*T` are views (section 7.7): they never free anything and may point at the stack, statics,
memory-mapped devices or the heap. Four predeclared types hold heap objects whose lifetime the compiler
manages. `owned[T]`, `shared[T]` and `synced[T]` are called **boxes**; `weak[P]` is a weak reference to the
object of a counted box. Their rules are in sections 18.5 to 18.7; this section describes the types.

```
BoxType  = ( "owned" | "shared" | "synced" ) "[" Type "]" .
WeakType = "weak" "[" Type "]" .
```

| Type | Kind (section 8.7) | Meaning |
|---|---|---|
| `owned[T]` | owner | The single owner of a heap `T`. Moves; when dropped, `T` is dropped and the memory freed. |
| `shared[T]` | counted | A reference-counted heap `T` used by one task. The count is not atomic. |
| `synced[T]` | counted | Like `shared[T]`, but the count is atomic (section 18.7), so references may cross tasks. |
| `weak[P]` | counted (weak) | A back reference to the object of `P`, a `shared[T]` or `synced[T]`, that does not keep it alive. |

`owned[T]` is one owner among many: `vec.Vec[T]`, `hash.Map[K, V]`, `strings.Builder` and `os.File` are owners
too, declared in the library. `owned[T]` is the one to use for a single object on the heap, such as a tree
node or a large struct that must keep its address.

**The type argument.** `T` is a concrete type: `owned[T]`, `shared[T]` and `synced[T]` cannot hold an
interface type (E3210). An interface value already refers to the value it holds, so a box around one would be
a second, slower way to own a value behind an interface. The error points to owning interfaces (section 7.9),
which own what they hold. `weak[P]` takes a `shared[T]` or a `synced[T]` and nothing else (E3211).

**Representation.** Each is one word, the address of the object. The allocation begins with a hidden header in
front of the object: for `owned[T]`, the allocator the object came from; for `shared[T]` and `synced[T]`, the
strong count, the weak count and the allocator (abi.md section 1.2). Freeing goes to the recorded allocator
even if the default has changed since; freeing into an arena does nothing (section 18.9, Allocators). The zero
value of each is `nil`; dropping a `nil` reference does nothing, and dereferencing one faults like a nil
pointer. A `weak[P]` is the same address as its `P`.

**Creation.** `new(T)` and `new(v)` return an `owned[T]` (section 13.9, Allocation). `alloc.Owned[T]()`,
`alloc.Shared[T]()` and `alloc.Synced[T]()` allocate a zeroed `T`, each with an optional allocator argument. A
`weak[P]` is made by converting a reference: `weak[shared[Node]](p)`. A `*T` view never becomes a box. The
conversions out of a box are `(*T)(p)`, `&*p` and, as unsafe code, `unsafe.Pointer(p)`; none of them moves or
counts.

**Use.** `owned[T]`, `shared[T]` and `synced[T]` dereference like `*T`: `p.field`, `*p`, `p[i]`, `len(p)` and
`range p` when `T` is an array, and method calls with `*T`'s methods. Each of these gives a view of the
object, which may not outlive the reference it was made through (section 18.3, Views): a view made through a
box held in a local variable is bounded by that variable, and one made through a temporary box by its
statement. A box is not a `*T`: storing it in a `*T` variable, returning it as a `*T` or passing it where a
`*T` is expected is a compile error. A `weak[P]` does not dereference; its one method is

```
func (w weak[P]) Upgrade() (P, bool)
```

which returns a new strong reference and `true` while the object is alive, and the zero `P` and `false` once
the last strong reference has been dropped (always `false` for the zero `weak`).

**Embedding.** A struct may embed `owned[T]`, `shared[T]` or `synced[T]` (section 7.6), promoting `T`'s
methods as an embedded `*T` does. A struct that embeds an `owned[T]` has an owner field and must be declared
`owner`. A struct that embeds a `shared[T]` or `synced[T]` copies, and counts each copy.

**Own methods.** Besides `*T`'s methods, `owned[T]` has a `Drop` method (drop the `T`, then free the memory,
ending the variable's life as `x.Drop()` does for any owner) and, when `T` is not an owner, a `Clone()` that
returns a new box from the current allocator holding a copy of the object. For an owner `T` with a `Clone`
method of its own, write `new(p.Clone())`. These methods act on the box, never satisfy an interface method,
and are never promoted from an embedded box. `shared[T]` and `synced[T]` have no methods of their own, so they
never hide one of `T`'s; their counts are changed only by copying and dropping.

**Counting.** Copying a `shared[T]` or `synced[T]` adds one to the strong count, dropping one subtracts one,
and when the strong count reaches zero `T` is dropped and its fields released. The memory is freed when the
weak count is also zero. A `weak[P]` holds the weak count. Section 18.7 (Shared ownership) gives the rules.

**Comparison.** Values of one of these types compare by address with values of the same type and with `nil`.
Comparing reads both operands and moves neither (section 8.10).

```go
type Tree owner struct {
	left, right owned[Tree]
	key         int
}

func build(depth int) owned[Tree] {
	t := alloc.Owned[Tree]()
	if depth > 0 {
		t.left = build(depth - 1)   // moves into the field
		t.right = build(depth - 1)
	}
	return t                        // moves to the caller
}                                   // nothing freed: t was moved
```

`Tree` is declared `owner` because it has owner fields (section 7.18).

**Kind.** `owned[T]` is an owner. `shared[T]`, `synced[T]` and `weak[P]` are counted values (section 8.7).

---

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
