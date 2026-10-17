## 10. Declarations and scope

A *declaration* binds a non-blank identifier to a constant, type, type parameter, variable, function, label
or package. Every identifier in a program must be declared. No identifier may be declared twice in the same
block, and no identifier may be declared in both the file and package block.

The blank identifier may be used like any other identifier in a declaration, but it does not introduce a
binding and thus is not declared. In the package block, the identifier `init` may only be used for `init`
function declarations, and like the blank identifier it does not introduce a new binding.

```
Declaration  = ConstDecl | TypeDecl | VarDecl .
TopLevelDecl = Declaration | FunctionDecl | MethodDecl .
```

The *scope* of a declared identifier is the extent of source text in which the identifier denotes the
specified constant, type, variable, function, label or package.

Plutonium is lexically scoped using blocks (chapter 9, Blocks):

1. The scope of a predeclared identifier is the universe block.
2. The scope of an identifier denoting a constant, type, variable or function (but not method) declared at
   top level (outside any function) is the package block.
3. The scope of the package name of an imported package is the file block of the file containing the import
   declaration.
4. The scope of an identifier denoting a method receiver, function parameter or result variable is the
   function body.
5. The scope of an identifier denoting a type parameter of a function or declared by a method receiver
   begins after the name of the function and ends at the end of the function body.
6. The scope of an identifier denoting a type parameter of a type begins after the name of the type and
   ends at the end of the TypeSpec.
7. The scope of a constant or variable identifier declared inside a function begins at the end of the
   ConstSpec or VarSpec (ShortVarDecl for short variable declarations) and ends at the end of the innermost
   containing block.
8. The scope of a type identifier declared inside a function begins at the identifier in the TypeSpec and
   ends at the end of the innermost containing block.
9. The scope of the variable `err` that an or clause declares is the clause (section 11.27, Or clauses).

An identifier declared in a block may be redeclared in an inner block. While the identifier of the inner
declaration is in scope, it denotes the entity declared by the inner declaration.

The package clause is not a declaration; the package name does not appear in any scope. Its purpose is to
identify the files belonging to the same package and to specify the default package name for import
declarations (section 14.2, Package clause).

The members of an enum and the variants of a union are not declared in any block. They are reached through
the name of their type, as in `Color.Red`, and enter no scope (section 7.12, Enums; section 7.13, Unions).
Two enums in one package can therefore each have a member `None`.

### 10.1 Label scopes

Labels are declared by labeled statements (section 12.3, Labeled statements) and are used in the `break`,
`continue` and `goto` statements (sections 12.15, 12.16 and 12.17). It is illegal to define a label that is
never used (E0122). A label is declared once in the body of its function: two labeled statements in one function body, in the same block or in different blocks, may not have the same label (the redeclaration error). In contrast to other identifiers, labels are not block scoped and do not conflict with
identifiers that are not labels. The scope of a label is the body of the function in which it is declared
and excludes the body of any nested function.

### 10.2 Blank identifier

The *blank identifier* is represented by the underscore character `_`. It serves as an anonymous
placeholder instead of a regular (non-blank) identifier and has special meaning in declarations, as an
operand, and in assignment statements. It is also the name of a reserved field in a packed type (section
7.14, `packed` types). It is not a member name of an enum (section 7.12) or a variant name of a union
(section 7.13).

Assigning to the blank identifier evaluates the right-hand side and discards the value. It never moves
anything. If the right-hand side is an owner variable, `_ = x` only reads it: `x` still holds its value
and is dropped when its scope ends. If the right-hand side produces an owner temporary, as in
`_ = open()`, the temporary is dropped at the end of the statement, as any temporary that nothing stores
(section 12.7, Assignment statements; section 18.6, Drops). To drop an owner variable early, call
`x.Drop()` (section 18.6).

```go
type Res owner struct{ id int }

func (r *Res) Drop() {
	if r.id != 0 { // Drop accepts the zero value, which holds nothing
		println("drop", r.id)
	}
}

func f() {
	r := Res{1}
	_ = r      // reads r; r is not moved and not dropped
	_ = Res{2} // the temporary is dropped at the end of the statement
	r.Drop()   // drops r now
}
```

`_ = x` is the way to mark a variable as used.

### 10.3 Predeclared identifiers

The following identifiers are implicitly declared in the universe block:

```
Types:
	any bool byte comparable error float32 float64
	int int8 int16 int32 int64 portable rune string
	uint uint8 uint16 uint32 uint64 uintptr

Type constructors:
	aligned owned ring shared synced volatile weak

Constants:
	true false iota

Zero value:
	nil

Functions:
	cap clear close copy delete len make max min new panic print println take
```

`byte` is an alias for `uint8`, and `rune` is an alias for `int32`. The types `int`, `uint` and `uintptr`
are 64 bits wide (section 7.2, Numeric types). `error` is the interface of section 16.1 (The error
interface). `comparable` and `portable` are interface types that may be used only as type constraints
(section 10.9, Type parameter declarations; section 8.9, Portable types).

The type constructors `volatile`, `aligned`, `ring`, `owned`, `shared`, `synced` and `weak` are generic and
are used with type arguments in square brackets: `volatile[T]`, `aligned[T, N]`, `ring[T, N]`, `owned[T]`,
`shared[T]`, `synced[T]` and `weak[P]` (sections 7.15 to 7.19). They are not types on their own. The
functions are the built-in functions of chapter 13: `cap` and `len` (section 13.6, Length and capacity),
`clear` (13.3), `close` (13.4), `copy` (13.2), `delete` (13.5), `make` (13.7), `max` and `min` (13.8), `new`
(13.9), `panic` (13.11), `print` and `println` (13.12) and `take` (13.10).

A predeclared identifier is not a keyword. A program may declare an identifier with the same name in the
package block or an inner block, and the declaration hides the predeclared one there.

**Names that are not declared.** The functions `append`, `recover`, `complex`, `real` and `imag` and the
types `complex64` and `complex128` do not exist and are not declared in the universe block. A program may use these names for its own declarations. A use of one of them without a declaration
is a compile error that says what to write instead (section 13.1, Built-ins Plutonium does not have). The
words `owner`, `union`, `task`, `or`, `bits`, `le` and `be` are contextual keywords, not predeclared identifiers
(section 4.6, Contextual keywords).

Appendix D lists the predeclared identifiers with the section that defines each.

### 10.4 Exported identifiers

An identifier may be *exported* to permit access to it from another package. An identifier is exported if
both:

1. the first character of the identifier's name is a Unicode uppercase letter (Unicode character category
   Lu); and
2. the identifier is declared in the package block, or it is a field name, a method name, a member of an
   enum or a variant of a union.

All other identifiers are not exported. An unexported member of an enum or variant of a union is not
visible in other packages (section 7.12; section 7.13).

### 10.5 Uniqueness of identifiers

Given a set of identifiers, an identifier is called *unique* if it is different from every other in the
set. Two identifiers are different if they are spelled differently, or if they appear in different
packages and are not exported. Otherwise, they are the same.

### 10.6 Constant declarations

A constant declaration binds a list of identifiers (the names of the constants) to the values of a list of
constant expressions. The number of identifiers must be equal to the number of expressions, and the *n*th
identifier on the left is bound to the value of the *n*th expression on the right.

```
ConstDecl      = "const" ( ConstSpec | "(" { ConstSpec ";" } ")" ) .
ConstSpec      = IdentifierList [ [ Type ] "=" ExpressionList ] .

IdentifierList = identifier { "," identifier } .
ExpressionList = Expression { "," Expression } .
```

If the type is present, all constants take the type specified, and the expressions must be assignable to
that type (section 8.4, Assignability), which must not be a type parameter or an owner type (a constant never needs an owner, chapter 5). If the type is omitted, the
constants take the individual types of the corresponding expressions. If the expression values are untyped
constants, the declared constants remain untyped and the constant identifiers denote the constant values.
For instance, if the expression is a floating-point literal, the constant identifier denotes a
floating-point constant, even if the literal's fractional part is zero.

```go
const Pi float64 = 3.14159265358979323846
const zero = 0.0 // untyped floating-point constant
const (
	size int64 = 1024
	eof        = -1 // untyped integer constant
)
const a, b, c = 3, 4, "foo" // a = 3, b = 4, c = "foo", untyped integer and string constants
const u, v float32 = 0, 3   // u = 0.0, v = 3.0
```

Within a parenthesized `const` declaration list the expression list may be omitted from any but the first
ConstSpec. Such an empty list is equivalent to the textual substitution of the first preceding non-empty
expression list and its type if any. Omitting the list of expressions is therefore equivalent to repeating
the previous list. The number of identifiers must be equal to the number of expressions in the previous
list. Together with the `iota` constant generator (section 10.7) this mechanism permits light-weight
declaration of sequential values:

```go
const (
	Sunday = iota
	Monday
	Tuesday
	Wednesday
	Thursday
	Friday
	Partyday
	numberOfDays // this constant is not exported
)
```

A closed set of named integer values that the compiler should check is better written as an enum (section
7.12), which adds exhaustive switches and generated operations. A `const` block with `iota` stays the
right form for bit masks and for values that must have no type of their own.

### 10.7 Iota

Within a constant declaration, the predeclared identifier `iota` represents successive untyped integer
constants. Its value is the index of the respective ConstSpec in that constant declaration, starting at
zero. It can be used to construct a set of related constants:

```go
const (
	c0 = iota // c0 == 0
	c1 = iota // c1 == 1
	c2 = iota // c2 == 2
)

const (
	a = 1 << iota // a == 1  (iota == 0)
	b = 1 << iota // b == 2  (iota == 1)
	c = 3         // c == 3  (iota == 2, unused)
	d = 1 << iota // d == 8  (iota == 3)
)

const (
	u         = iota * 42 // u == 0     (untyped integer constant)
	v float64 = iota * 42 // v == 42.0  (float64 constant)
	w         = iota * 42 // w == 84    (untyped integer constant)
)

const x = iota // x == 0
const y = iota // y == 0
```

By definition, multiple uses of `iota` in the same ConstSpec all have the same value:

```go
const (
	bit0, mask0 = 1 << iota, 1<<iota - 1 // bit0 == 1, mask0 == 0  (iota == 0)
	bit1, mask1                          // bit1 == 2, mask1 == 1  (iota == 1)
	_, _                                 //                        (iota == 2, unused)
	bit3, mask3                          // bit3 == 8, mask3 == 7  (iota == 3)
)
```

This last example exploits the implicit repetition of the last non-empty expression list.

Using `iota` outside a constant declaration is a compile error (E0036).

**Iota in enums.** `iota` is not defined in an enum. The values of the members of an enum are constant
expressions that are evaluated in the scope around the declaration, where `iota` has no meaning, so using
`iota` in a member's value is a compile error. A member without a value is one more than the previous
member (section 7.12, Enums), which numbers members without `iota`.

```go
type Level enum {
	Debug = iota * 10 // error: iota is not defined in an enum
	Info
}

type Fixed enum {
	Debug = 0
	Info  = 10 // write the values; a member with no value is one more than the last
	Warn  = 20
}
```

### 10.8 Type declarations

A type declaration binds an identifier, the *type name*, to a type. Type declarations come in two forms:
alias declarations and type definitions.

```
TypeDecl  = "type" ( TypeSpec | "(" { TypeSpec ";" } ")" ) .
TypeSpec  = AliasDecl | TypeDef .
AliasDecl = identifier [ TypeParameters ] "=" Type .
TypeDef   = identifier [ TypeParameters ] DefinedType .
```

`DefinedType`, the type of a definition with its optional `owner` qualifier, is given at the start of
chapter 7 (Types).

**Alias declarations.** An alias declaration binds an identifier to the given type. Within the scope of the
identifier, it serves as an alias for the given type.

```go
type Node struct{ next *Node }
type polar struct{ r, theta float64 }

type (
	nodeList = []*Node // nodeList and []*Node are identical types
	Polar    = polar   // Polar and polar denote identical types
)
```

If the alias declaration specifies type parameters, the type name denotes a *generic alias*. Generic
aliases must be instantiated when they are used.

```go
type pointers[P any] = []*P
```

In an alias declaration the given type cannot be a type parameter declared in the same declaration.

```go
type A[P any] = P // error: P is a type parameter declared in the declaration of A

func f[P any]() {
	type A = P // ok: P is a type parameter declared by the enclosing function
}
```

An alias denotes the same type, so it has that type's kind and every other property of it. `owner` cannot
qualify an alias (E3205). An existing named enum or union type may be the given type of an alias; the alias
preserves its identity, members and variants. The `enum { ... }` and `union { ... }` declaration forms
themselves appear only in type definitions (E2301, E2401), not directly as alias right-hand sides.

**Type definitions.** A type definition creates a new, distinct type with the same underlying type and
operations as the given type and binds an identifier, the type name, to it. The new type is called a
*defined type*. It is different from any other type, including the type it is created from.

```go
type (
	Point struct{ x, y float64 } // Point and struct{ x, y float64 } are different types
	polar Point                  // polar and Point denote different types
)

type TreeNode struct {
	left, right *TreeNode
	value       any
}

type Block interface {
	BlockSize() int
	Encrypt(src, dst []byte)
	Decrypt(src, dst []byte)
}
```

A defined type may have methods associated with it. It does not inherit any methods bound to the given
type, but the method set of an interface type or of elements of a composite type remains unchanged:

```go
// A Mutex is a data type with two methods, Lock and Unlock.
type Mutex struct { /* Mutex fields */ }

func (m *Mutex) Lock()   { /* Lock implementation */ }
func (m *Mutex) Unlock() { /* Unlock implementation */ }

// NewMutex has the same composition as Mutex but its method set is empty.
type NewMutex Mutex

// The method set of PtrMutex's underlying type *Mutex remains unchanged,
// but the method set of PtrMutex is empty.
type PtrMutex *Mutex

// The method set of *PrintableMutex contains the methods
// Lock and Unlock bound to its embedded field Mutex.
type PrintableMutex struct {
	Mutex
}

// MyBlock is an interface type that has the same method set as Block.
type MyBlock Block
```

Type definitions may be used to define different boolean, numeric or string types and associate methods
with them:

```go
type TimeZone int

const (
	EST TimeZone = -(5 + iota)
	CST
	MST
	PST
)

func (tz TimeZone) String() string {
	return "GMT" // the offset formatting is left out of this example
}
```

If the type definition specifies type parameters, the type name denotes a *generic type*. Generic types
must be instantiated when they are used.

```go
type List[T any] struct {
	next  *List[T]
	value T
}
```

In a type definition the given type cannot be a type parameter.

```go
type T[P any] P // error: P is a type parameter

func f[P any]() {
	type L P // error: P is a type parameter declared by the enclosing function
}
```

A generic type may also have methods associated with it. In this case, the method receivers must declare
the same number of type parameters as present in the generic type definition (section 10.14, Method
declarations).

```go
// The method Len returns the number of elements in the linked list l.
func (l *List[T]) Len() int { return 0 }
```

**What only a definition can say.** Four forms of type may appear only as the type of a type definition,
because the names of their members, or their kind, are reached through the type's name:

- An *enum type* `type E enum { ... }` (section 7.12, Enums). An enum cannot have type parameters
  (E2302).
- A *union type* `type U union { ... }` (section 7.13, Unions). A union may have type parameters, and a
  packed union may not (E2402).
- A *packed type* `type P packed ... { ... }` or `packed union` (sections 7.14 and 7.13). One written anywhere else is a compile error (E2104).
- An *owner type* `type T owner ...`, where the keyword `owner` precedes the type (section 7.18, Owner
  types). It declares that the values of `T` own what they hold: they move instead of copying and are
  dropped when their variable's scope ends. Which types `owner` may qualify, which types must carry it, and
  what follows from it are the subject of section 7.18. A type definition from an owner type other than `owned[T]` must carry
  `owner` too: `type B owner A` (E3202); one from `owned[T]` defines an owner by itself and may not carry it (section 7.18).
  A value of such a `B` drops as an `A` after `B`'s own `Drop` (section 7.18); `B` has none of `A`'s methods.

```go
type File owner struct {
	fd   int32
	open bool
}

type Frame owner uintptr // a physical page frame

type Color enum {
	Red
	Green
	Blue
}

type Slot owner union {
	Empty
	Open File
	Code int
}
```

An enum, union or packed type written anywhere else, as in `var x enum { A }`, is a compile error (E2301, E2401, E2104).
The word `owner` has a place only in a type definition and in a type parameter declaration (section 4.6,
Contextual keywords).

### 10.9 Type parameter declarations

A *type parameter list* declares the type parameters of a generic function, method or type declaration.
The type parameter list looks like an ordinary function parameter list except that the type parameter
names must all be present and the list is enclosed in square brackets rather than parentheses.

```
TypeParameters = "[" TypeParamList [ "," ] "]" .
TypeParamList  = TypeParamDecl { "," TypeParamDecl } .
TypeParamDecl  = IdentifierList [ "owner" ] TypeConstraint .
```

All non-blank names in the list must be unique. Each name declares a type parameter, which is a new and
different named type that acts as a placeholder for an (as of yet) unknown type in the declaration. The
type parameter is replaced with a *type argument* upon instantiation of the generic function, method or
type (section 11.16, Instantiations).

```
[P any]
[S interface{ ~[]byte | string }]
[S ~[]E, E any]
[P Constraint[int]]
[_ any]
[T owner any]
```

Just as each ordinary function parameter has a parameter type, each type parameter has a corresponding
(meta-)type which is called its *type constraint*.

A parsing ambiguity arises when the type parameter list for a generic type declares a single type
parameter `P` with a constraint `C` such that the text `P C` forms a valid expression:

```
type T[P *C] …
type T[P (C)] …
type T[P *C|Q] …
…
```

In these rare cases, the type parameter list is indistinguishable from an expression and the type
declaration is parsed as an array type declaration. To resolve the ambiguity, embed the constraint in an
interface or use a trailing comma:

```
type T[P interface{ *C }] …
type T[P *C,] …
```

Type parameters may also be declared by the receiver specification of a method declaration associated with
a generic type (section 10.14).

**Owner type parameters.** A type parameter admits owner types as type arguments only if its declaration
carries `owner`: `[T owner any]`, `[T owner io.Reader]`. Inside such code, values of type `T` move.
Assignment, passing and returning move them, using one after a move is an error, and a variable of type `T`
that still holds a value at the end of its scope is dropped (which does nothing for an instantiation with
a value type). A type parameter without `owner` cannot be instantiated with an owner type (E3207), so
ordinary generic code such as `Max[T cmp.Ordered]` or `Fill[T any]` keeps copying freely. A generic type
that stores values of an owner type parameter must be declared `owner`, and all of its instantiations then
move, including `vec.Vec[int]` (section 7.18, Owner types).

**Type constraints.** A *type constraint* is an interface that defines the set of permissible type
arguments for the respective type parameter and controls the operations supported by values of that type
parameter.

```
TypeConstraint = TypeElem .
```

If the constraint is an interface literal of the form `interface{E}` where `E` is an embedded type element
(not a method), in a type parameter list the enclosing `interface{ … }` may be omitted for convenience:

```
[T []P]                  // = [T interface{[]P}]
[T ~int]                 // = [T interface{~int}]
[T int|string]           // = [T interface{int|string}]
type Constraint ~int     // illegal: ~int is not in a type parameter list
```

The predeclared interface type `comparable` denotes the set of all non-interface types that are *strictly
comparable* (section 8.10, Comparable types). The predeclared interface type `portable` denotes the types
that may cross between address spaces (section 8.9, Portable types).

Even though interfaces that are not type parameters are comparable, they are not strictly comparable and
therefore they do not implement `comparable`. However, they satisfy `comparable`.

```
int                         // implements comparable (int is strictly comparable)
[]byte                      // does not implement comparable (slices cannot be compared)
interface{}                 // does not implement comparable (see above)
interface{ ~int | ~string } // type parameter only: implements comparable (int, string types are strictly comparable)
interface{ comparable }     // type parameter only: implements comparable (comparable implements itself)
interface{ ~int | ~[]byte } // type parameter only: does not implement comparable (slices are not comparable)
interface{ ~struct{ any } } // type parameter only: does not implement comparable (field any is not strictly comparable)
```

The `comparable` interface and interfaces that (directly or indirectly) embed `comparable` may only be
used as type constraints. They cannot be the types of values or variables, or components of other,
non-interface types.

**Satisfying a type constraint.** A type argument `T` *satisfies* a type constraint `C` if `T` is an
element of the type set defined by `C`; in other words, if `T` implements `C` (section 7.9, Interface
types). As an exception, a strictly comparable type constraint may also be satisfied by a comparable (not
necessarily strictly comparable) type argument. More precisely, a type `T` satisfies a constraint `C` if

- `T` implements `C`; or
- `C` can be written in the form `interface{ comparable; E }`, where `E` is a basic interface and `T` is
  comparable and implements `E`.

| Type argument | Type constraint | Constraint satisfaction |
|---|---|---|
| `int` | `interface{ ~int }` | satisfied: `int` implements `interface{ ~int }` |
| `string` | `comparable` | satisfied: `string` implements `comparable` (`string` is strictly comparable) |
| `[]byte` | `comparable` | not satisfied: slices are not comparable |
| `any` | `interface{ comparable; int }` | not satisfied: `any` does not implement `interface{ int }` |
| `any` | `comparable` | satisfied: `any` is comparable and implements the basic interface `any` |
| `struct{f any}` | `comparable` | satisfied: `struct{f any}` is comparable and implements the basic interface `any` |
| `any` | `interface{ comparable; m() }` | not satisfied: `any` does not implement the basic interface `interface{ m() }` |
| `interface{ m() }` | `interface{ comparable; m() }` | satisfied: `interface{ m() }` is comparable and implements the basic interface `interface{ m() }` |

Because of the exception in the constraint satisfaction rule, comparing operands of type parameter type may
panic at run time (even though comparable type parameters are always strictly comparable).

### 10.10 Variable declarations

A variable declaration creates one or more variables, binds corresponding identifiers to them, and gives
each a type and an initial value.

```
VarDecl = "var" ( VarSpec | "(" { VarSpec ";" } ")" ) .
VarSpec = IdentifierList ( Type [ "=" ExpressionList ] | "=" ExpressionList ) .
```

```go
var i int
var U, V, W float64
var k = 0
var x, y float32 = -1, -2
var (
	i       int
	u, v, s = 2.0, 3.0, "bar"
)
var _, found = entries[name] // map lookup; only interested in "found"
```

If a list of expressions is given, the variables are initialized with the expressions following the rules
for assignment statements (section 12.7, Assignment statements). Otherwise, each variable is initialized to
its zero value (section 15.1, The zero value). The zero value of an owner holds nothing, and dropping it
does nothing, so `var f os.File` declares a file that is not open.

If a type is present, each variable is given that type. Otherwise, each variable is given the type of the
corresponding initialization value in the assignment. If that value is an untyped constant, it is first
implicitly converted to its default type (chapter 5, Constants); if it is an untyped boolean value, it is
first implicitly converted to type `bool`. The predeclared identifier `nil` cannot be used to initialize a
variable with no explicit type.

```go
var d = math.Sin(0.5) // d is float64
var i = 42            // i is int
var t, ok = x.(T)     // t is T, ok is bool
var n = nil           // error: nil has no type
```

Initializing a variable with an owner moves the owner into it (section 18.5, Moves). The initializer of a
package-level variable runs before `main` (section 15.2, Package initialization). In a freestanding build,
a package-level initializer that allocates is a compile error (section 15.2).

It is an error to declare a local variable and never use it (E0101). Assigning to a variable does not use
it.

### 10.11 Short variable declarations

A *short variable declaration* uses the syntax:

```
ShortVarDecl = IdentifierList ":=" ExpressionList .
```

It is shorthand for a regular variable declaration with initializer expressions but no types:

```
"var" IdentifierList "=" ExpressionList .
```

```go
i, j := 0, 10
f := func() int { return 7 }
ch := make(chan int)
r, w, _ := os.Pipe()  // os.Pipe() returns a connected pair of Files and an error, if any
_, y, _ := coord(p)   // coord() returns three values; only interested in y coordinate
```

Unlike regular variable declarations, a short variable declaration may redeclare variables provided they
were originally declared earlier in the same block (or the parameter lists if the block is the function
body) with the same type, and at least one of the non-blank variables is new. As a consequence,
redeclaration can only appear in a multi-variable short declaration. Redeclaration does not introduce a
new variable; it just assigns a new value to the original. If the original variable holds an owner, the
assignment drops the old value first (section 12.7). The non-blank variable names on the left side of `:=`
must be unique.

```go
field1, offset := nextField(str, 0)
field2, offset := nextField(str, offset) // redeclares offset
x, y, x := 1, 2, 3                       // error: x repeated on left side of :=
```

Short variable declarations may appear only inside functions. In some contexts such as the initializers for
`if`, `for` or `switch` statements, they can be used to declare local temporary variables.

### 10.12 Function declarations

A function declaration binds an identifier, the *function name*, to a function.

```
FunctionDecl = "func" FunctionName [ TypeParameters ] Signature [ FunctionBody ] .
FunctionName = identifier .
FunctionBody = Block .
```

The signature may give default values to its trailing parameters (section 10.13).

If the function's signature declares result parameters, the function body's statement list must end in a
terminating statement (section 12.1, Terminating statements).

```go
func IndexRune(s string, r rune) int {
	for i, c := range s {
		if c == r {
			return i
		}
	}
	// error: missing return
}
```

If the function declaration specifies type parameters, the function name denotes a *generic function*. A
generic function must be instantiated before it can be called or used as a value (section 11.16,
Instantiations).

```go
func min[T ~int | ~float64](x, y T) T {
	if x < y {
		return x
	}
	return y
}
```

A function declaration without type parameters may omit the body. Such a declaration provides the
signature for a function implemented outside Plutonium, such as an assembly routine or a C function
(section 20.4, Assembly and C).

```go
func flushICache(begin, end uintptr) // implemented externally
```

**The function `main`.** The program's entry point is the function `main` in package `main` (section 15.4,
Program execution). It has no parameters and either no result or one result of type `int64`. In a hosted
build the `int64` becomes the process exit status. Any other signature is a compile error (E2009).

```go
package main

func main() int64 {
	return 0
}
```

**The function `init`.** A function named `init` has no parameters and no results, takes no type
parameters, and is called by the system before `main` as section 15.2 (Package initialization) describes.
It cannot be referred to from a program. A package may declare several functions named `init`.

### 10.13 Default parameter values

The trailing parameters of a function or method may have *default values*. A call may omit any suffix of
the defaulted parameters. The default expressions are evaluated at each call, in the package scope of the
callee, after the explicit arguments.

```
DefaultValue = "=" Expression .
```

A `DefaultValue` follows the type in a `ParameterDecl` (section 7.8, Function types).

A defaulted parameter is written `name T = expr`, or `T = expr` in a list of unnamed parameters. A
`ParameterDecl` with a default value declares one parameter.

```go
func Grow(n int, factor int = 2, limit int = 1 << 20) int {
	return min(n*factor, limit)
}

func main() {
	println(Grow(10))         // factor = 2, limit = 1 << 20
	println(Grow(10, 3))      // limit = 1 << 20
	println(Grow(10, 3, 16))  // no defaults used
	g := Grow                 // g has type func(int, int, int) int
	println(g(10, 2, 64))     // a function value takes every argument
}
```

The rules in full:

- **Declaration.** A defaulted parameter has a type of its own: `a, b int = 1` is an error (E2204). Once a
  parameter has a default, every later parameter must have one (E2203). Defaults are allowed only in
  function and method declarations: not on receivers or results, and not in function types, function
  literals or interface methods (E2201).
- **Scope.** A default is checked where the function is declared. It may use the names of that package,
  exported or not, the imports of that file, and the type parameters of the function and of its receiver
  type. It may not refer to the function's parameters or receiver (E2205): a parameter name inside a
  default is an error even where a package-level name of the same spelling exists. The default must be
  assignable to the parameter's type (section 8.4, Assignability).
- **Evaluation.** Each call that omits a defaulted parameter evaluates its default after all explicit
  arguments, left to right, as if the expression were written at the call site (its temporaries live in the
  caller's frame) but with its names resolved in the callee's declaration (section 11.26, Order of
  evaluation). A call from another package may therefore run the callee's unexported helpers. For a
  deferred call and for a task statement, the defaults are evaluated with the other arguments when the
  statement runs (section 12.19, Defer statements; section 12.12, Task statements). The package-level
  variables a default uses are dependencies of the function for initialization order (section 15.2).
- **Which calls.** Defaults apply to calls that name a declared function or method directly: `f(x)`,
  `pkg.F(x)`, `F[T](x)` and `v.M(x)`, including promoted methods and methods with type parameters (section
  11.14, Calls). A function type does not record defaults, so a function value, method value or method
  expression has all of the parameters: after `g := pkg.F` or `m := v.M`, `g` and `m` are called with every
  argument. Interface methods cannot declare defaults, and a call through an interface passes every
  argument. A method with defaults implements an interface method with the same parameter types, since
  defaults are not part of the signature and signature identity ignores them (section 8.3, Type identity).
- **Generics.** A default may use type parameters. It is evaluated with the type arguments of the instance
  called. Omitted parameters take no part in type inference (section 11.17, Type inference), so a type
  parameter that appears only in omitted parameters must be given explicitly.
- **Multiple values.** In `f(g())`, where `g` returns *k* values and `f` has more parameters, the *k*
  values fill the first *k* parameters and the rest take their defaults, provided they all have one.
- **Variadic functions** cannot have defaults (E2202): a function with a variadic parameter and any defaulted parameter is E2202, and E2203 is not reported for it. The variadic parameter is last, so a defaulted
  parameter before it would have to be given whenever any variadic argument is, and an argument after the
  variadic ones could not be told apart from them. The variadic constructors of the standard library
  therefore allocate from the current default allocator, which `alloc.Use` replaces for a region of code
  (section 18.9, Allocators).

```go
func Fill(dst []byte, b byte = 0) {
	for i := range dst {
		dst[i] = b
	}
}

func Printf(format string, w int = 0, args ...any) {} // error: a variadic function has no defaults (E2202)
```

### 10.14 Method declarations

A *method* is a function with a receiver. A method declaration binds an identifier, the *method name*, to a
method, and associates the method with the receiver's *base type*.

```
MethodDecl = "func" Receiver MethodName [ TypeParameters ] Signature [ FunctionBody ] .
Receiver   = Parameters .
```

The receiver is specified via an extra parameter section preceding the method name. That parameter section
must declare a single non-variadic parameter, the receiver. Its type must be a defined type `T` or a
pointer to a defined type `T`, possibly followed by a list of type parameter names `[P1, P2, …]` enclosed
in square brackets. `T` is called the receiver base type. A receiver base type cannot be a pointer or
interface type and it must be declared in the same package as the method. The method is said to be *bound*
to its receiver base type, and the method name is visible only within selectors for type `T` or `*T`
(section 11.7, Selectors).

A non-blank receiver identifier must be unique in the method signature. If the receiver's value is not
referenced inside the body of the method, its identifier may be omitted in the declaration. The same
applies in general to parameters of functions and methods.

A method without type parameters may omit its body, as a function may (section 10.12), and is then implemented outside Plutonium (section 20.4, Assembly and C); a method with type parameters must have a body. For a base type, the non-blank names of methods bound to it must be unique. If the base type is a struct
type, the non-blank method and field names must be distinct.

Given a defined type `Point` the declarations

```go
func (p *Point) Length() float64 {
	return math.Sqrt(p.x*p.x + p.y*p.y)
}

func (p *Point) Scale(factor float64) {
	p.x *= factor
	p.y *= factor
}
```

bind the methods `Length` and `Scale`, with receiver type `*Point`, to the base type `Point`.

If the receiver base type is a generic type, the receiver specification must declare corresponding type
parameters for the method to use. This makes the receiver type parameters available to the method.
Syntactically, this type parameter declaration looks like an instantiation of the receiver base type: the
type arguments must be identifiers denoting the type parameters being declared, one for each type parameter
of the receiver base type. The type parameter names do not need to match their corresponding parameter
names in the receiver base type definition, and all non-blank parameter names must be unique in the
receiver parameter section and the method signature. The receiver type parameter constraints are implied
by the receiver base type definition: corresponding type parameters have corresponding constraints.

```go
type Pair[A, B any] struct {
	a A
	b B
}

func (p Pair[A, B]) Swap() Pair[B, A] { return Pair[B, A]{p.b, p.a} } // receiver declares A, B
func (p Pair[First, _]) First() First { return p.a }                  // receiver declares First, corresponds to A in Pair
```

If the receiver type is denoted by (a pointer to) an alias, the alias must not be generic and it must not
denote an instantiated generic type, neither directly nor indirectly via another alias, and irrespective
of pointer indirections.

```go
type GPoint[P any] = Point
type HPoint = *GPoint[int]
type IPair = Pair[int, int]

func (*GPoint[P]) Draw(P)  {} // error: alias must not be generic
func (HPoint) Draw(P)      {} // error: alias must not denote instantiated type GPoint[int]
func (*IPair) Second() int {} // error: alias must not denote instantiated type Pair[int, int]
```

**Receivers of owner types.** A method with a pointer receiver borrows its receiver: the receiver is a view
of the variable, and the variable keeps its owner. A method with a value receiver on an owner type
*consumes* the receiver: calling it moves the owner into the method, which drops it when it returns.
Because of that, a value-receiver method of an owner type never satisfies an interface method (section 8.6,
Method sets), and cannot be used as a method value (section 11.9, Method values). Owner types should
declare their methods on pointer receivers unless the method's purpose is to consume.

**The method `Drop`.** A method named `Drop` is the destructor of an owner type (section 18.6, Drops). It
has no parameters and no results (E3206), and it may be declared only on a type declared `owner`, with
a pointer receiver (E3201). The embedded fields of a struct promote the methods of the embedded type as
usual, except `Drop`, which is never promoted. Section 7.18 (Owner types) defines the full rules.

```go
type File owner struct {
	fd   int32
	open bool
}

func (f *File) Drop() {
	if f.open {
		closeFD(f.fd)
	}
}

func closeFD(fd int32) {}
```

A method may have default parameter values (section 10.13) and type parameters of its own (section
10.15).

### 10.15 Methods with type parameters

A method may declare type parameters of its own, in addition to those of its receiver's type. The method
name then denotes a *generic method*. Like a generic function, a generic method must be instantiated before
it can be called or used as a value.

```go
type Pair[T any] struct{ a, b T }

func (p Pair[T]) Map[U any](f func(T) U) Pair[U] {
	return Pair[U]{f(p.a), f(p.b)}
}

func f() {
	p := Pair[int]{1, 2}
	q := p.Map[string](func(n int) string { return "n" }) // U given explicitly
	r := p.Map(func(n int) float64 { return float64(n) }) // U inferred
	_, _ = q, r
}
```

A generic method is instantiated for each set of type arguments used, like a generic function. Each
instantiation is an ordinary function in the program. The rules are:

- It is called directly and never through an interface. A method with type parameters of its own does not
  count toward implementing an interface, and an interface cannot declare one (section 7.9, Interface
  types).
- As with a generic function, a method value `x.M` or method expression `T.M` of a generic method must be
  instantiated, either explicitly (`p.Map[string]`) or from the function type it is assigned to (section
  11.8, Method expressions; section 11.9, Method values).
- The type arguments of a call are inferred as for a generic function (section 11.17, Type inference).
- A generic method may have default parameter values (section 10.13).

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
