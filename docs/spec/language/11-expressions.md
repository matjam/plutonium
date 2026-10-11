## 11. Expressions

An expression specifies the computation of a value by applying operators and functions to operands.

Every expression is either a **place** or a **temporary**. A place is a variable, a field of a place, an element of an array place or of a slice, or the variable a pointer points to. A temporary is a value that an expression makes and that is not stored anywhere yet: the result of a call, a conversion, a concatenation, a composite literal or a receive. A temporary lives until the end of the statement that makes it (section 18.6, Drops).

An expression whose type is an owner type (section 7.18, Owner types) never copies its value. When the expression is a whole variable and its value is passed on, the variable is **moved** (section 18.5, Moves). The contexts that pass a value on are assignment, passing as an argument, returning, sending, storing in a field, element or composite literal, and converting to an owning interface. In every other context the owner is read in place: as the operand of a comparison, a selector, an index expression or a method call with a pointer receiver. Values of counted types (strings, `shared`, `synced`, channels, maps) are copied freely and count each copy (section 18.7, Shared ownership; section 18.8, Strings).

### 11.1 Operands

Operands denote the elementary values in an expression. An operand may be a literal, a (possibly qualified) non-blank identifier denoting a constant, variable or function, or a parenthesized expression.

```
Operand     = Literal | OperandName [ TypeArgs ] | "(" Expression ")" .
Literal     = BasicLit | CompositeLit | FunctionLit .
BasicLit    = int_lit | float_lit | rune_lit | string_lit .
OperandName = identifier | QualifiedIdent .
```

An operand name denoting a generic function may be followed by a list of type arguments (section 11.16, Instantiations). The resulting operand is an instantiated function.

The blank identifier may appear as an operand only on the left-hand side of an assignment (section 12.7, Assignment statements).

There are no imaginary literals (section 4.9, Floating-point literals), so `BasicLit` has no `imaginary_lit`.

An operand that names a variable of owner type is a read or a move, depending on the context in which it appears (the introduction of this chapter). Using a variable after it was moved is an error (section 18.5, Moves).

An implementation need not report an error if an operand's type is a type parameter with an empty type set. Functions with such type parameters cannot be instantiated, and any attempt is an error at the instantiation site.

### 11.2 Qualified identifiers

A qualified identifier is an identifier qualified with a package name prefix. Both the package name and the identifier must not be blank.

```
QualifiedIdent = PackageName "." identifier .
```

A qualified identifier accesses an identifier in a different package, which must be imported (section 14.3, Import declarations). The identifier must be exported (section 10.4, Exported identifiers) and declared in the package block of that package.

```go
math.Sin // denotes the Sin function in package math
```

### 11.3 Composite literals

Composite literals construct new values for structs, arrays, slices and maps each time they are evaluated. They also construct values of packed types and rings. A composite literal consists of the type of the literal followed by a (possibly empty) brace-bound list of elements. Each element may optionally be preceded by a corresponding key.

```
CompositeLit = LiteralType LiteralValue .
LiteralType  = StructType | ArrayType | "[" "..." "]" ElementType |
               SliceType | MapType | TypeName [ TypeArgs ] .
LiteralValue = "{" [ ElementList [ "," ] ] "}" .
ElementList  = KeyedElement { "," KeyedElement } .
KeyedElement = [ Key ":" ] Element .
Key          = FieldName | Expression | LiteralValue .
FieldName    = identifier .
Element      = Expression | LiteralValue .
```

Unless the `LiteralType` is a type parameter, its underlying type must be a struct, array, slice, map, packed or ring type (the syntax enforces this constraint except when the type is given as a `TypeName`). If the `LiteralType` is a type parameter, all types in its type set must have the same underlying type, which must be a valid composite literal type. A value of an enum type is not written as a composite literal (section 7.12, Enums). A value of a union type is written with one of its variants, `U.V(x)` or `U.V{...}` (section 11.7, Selectors; section 7.13, Unions); `U{...}` is an error.

The types of the elements and keys must be assignable to the respective field, element and key types of the `LiteralType`. There is no additional conversion. The key is interpreted as a field selector for struct literals, an index for array and slice literals, and a key for map literals. It is an error to specify multiple elements with the same field selector or constant key value.

A literal may omit the element list. For a struct, array, packed or ring type, such a literal evaluates to the zero value of its type (section 15.1, The zero value). For a slice or map type it evaluates to an empty value that is not nil: `[]int{}` is a slice of length 0 that is not `nil`, and `map[string]int{}` is a new empty map.

**Parsing ambiguity.** A parsing ambiguity arises when a composite literal using the `TypeName` form of the `LiteralType` appears as an operand between the keyword and the opening brace of the block of an `if`, `for` or `switch` statement, and the composite literal is not enclosed in parentheses, square brackets or curly braces. In this rare case, the opening brace of the literal is parsed as the one that begins the block of statements. To resolve the ambiguity, the composite literal must appear within parentheses.

```go
if x == (T{a, b, c}[i]) { … }
if (x == T{a, b, c}[i]) { … }
```

**Struct literals.** For struct literals without keys, the element list must contain an element for each struct field, in the order in which the fields are declared. An unkeyed literal of a struct type from another package is an error if the struct has unexported fields.

For struct literals with keys, these rules apply:

- Every element must have a key.
- Each key must be a valid field selector for a (possibly promoted) field of the struct. The key selects that field.
- The types of the embedded fields (if any) traversed to reach a selected field must not be pointer types.
- A key must not denote a promoted field inside an embedded struct if that struct is also specified by another key.
- The element list does not need to have an element for each struct field. Omitted fields get the zero value for that field.

Given the declarations

```go
type Object struct{ name, color string }
type Point3D struct {
	Object
	x, y, z float64
}
type Line struct {
	Object
	p, q Point3D
}
```

one may write

```go
origin := Point3D{}                                               // zero value for Point3D
line1 := Line{Object{}, origin, Point3D{y: -4, z: 12.3}}      // zero value for line1.q.x
line2 := Line{name: "diagonal", q: Point3D{x: 1, y: 1, z: 1}} // zero value for line2.Object.color, line2.p
```

but field selectors may not denote overlapping fields:

```go
obj := Object{"edge", "black"}
line3 := Line{Object: obj, name: "diagonal"} // error: name denotes a field inside Object
```

An element of owner type that is a whole variable is moved into the literal. A struct literal that holds an owner therefore has an owner type, which must be declared with `owner` (section 7.18, Owner types). A literal of a struct type with counted fields (strings, `shared`, `synced`, channels, maps) copies them and counts each copy.

```go
c := Conn{buf: b} // moves b into the literal; Conn is declared owner
```

**Array and slice literals.** For array and slice literals these rules apply:

- Each element has an associated integer index marking its position in the array.
- An element with a key uses the key as its index. The key must be a non-negative constant representable by a value of type `int`. If it is typed, it must be of integer type.
- An element without a key uses the previous element's index plus one. If the first element has no key, its index is zero.

The length of an array literal is the length specified in the literal type. If fewer elements than the length are provided, the missing elements are set to the zero value of the array element type. It is an error to provide elements with index values outside the index range of the array. The notation `...` specifies an array length equal to the maximum element index plus one.

```go
buffer := [10]string{}            // len(buffer) == 10
intSet := [6]int{1, 2, 3, 5}      // len(intSet) == 6
days := [...]string{"Sat", "Sun"} // len(days) == 2
```

An array literal is a value. An array literal whose elements are owners is an owner (section 7.4, Array types).

A slice literal describes the entire underlying array literal. Thus the length and capacity of a slice literal are the maximum element index plus one. A slice literal of the form `[]T{x1, x2, … xn}` is shorthand for a slice operation applied to an array:

```go
tmp := [n]T{x1, x2, … xn}
tmp[0:n]
```

The array of a slice literal is a temporary variable of the frame of the function that evaluates the literal, and the slice is a view of it (section 18.3, Views). The slice may be used within the function and passed down to calls. Returning it, or keeping it where it can outlive the frame, is an error (E3001, E3002). In the initializer of a package-level variable the array is static storage, so the slice may be kept. To keep a slice of values beyond the frame, store the values in an owner such as `vec.Vec[T]` (section 13.1, Built-ins Plutonium does not have). A slice literal whose elements are owners is an error (E3312), because a slice is a view and owns nothing; write an array literal instead.

**Taking the address of a literal.** Taking the address of a composite literal generates a pointer to a unique variable initialized with the literal's value. The variable lives where the literal is evaluated: in the frame, or in static storage in a package-level initializer. The pointer is a view (section 18.3, Views) and is subject to its rules; `return &T{}` is an error (E3001). To make a variable that outlives the frame, write `new(T{...})`, which returns an `owned[T]` (section 13.9, Allocation).

```go
var pointer *Point3D = &Point3D{y: 1000}   // a view of a variable in this frame
box := new(Point3D{y: 1000})               // an owned[Point3D] on the heap
```

**Map literals.** For map literals, each element must have a key. A map literal makes a new map each time it is evaluated, allocating its table from the current allocator (section 18.9, Allocators). The map is a counted reference (section 7.10, Map types). The keys and the values of a map may not be owners (section 7.10, Map types); counted values are allowed and are counted as the literal copies them. For non-constant map keys, see section 11.26.

**Packed and ring literals.** A literal of a packed type names the fields it sets, `ControlReg{mode: 5, enable: true}` (E2105 otherwise), and a literal of a ring type is empty (E2110) (section 7.14, `packed` types; section 7.17, `ring[T, N]`).

**Elision of element types.** Within a composite literal of array, slice or map type `T`, elements or map keys that are themselves composite literals may elide the respective literal type if it is identical to the element or key type of `T`. Similarly, elements or keys that are addresses of composite literals may elide the `&T` when the element or key type is `*T`.

```go
type Point struct{ x, y float64 }
type PPoint *Point

[...]Point{{1.5, -3.5}, {0, 0}}   // same as [...]Point{Point{1.5, -3.5}, Point{0, 0}}
[][]int{{1, 2, 3}, {4, 5}}        // same as [][]int{[]int{1, 2, 3}, []int{4, 5}}
[][]Point{{{0, 1}, {1, 2}}}       // same as [][]Point{[]Point{Point{0, 1}, Point{1, 2}}}
map[string]Point{"orig": {0, 0}}  // same as map[string]Point{"orig": Point{0, 0}}
map[Point]string{{0, 0}: "orig"}  // same as map[Point]string{Point{0, 0}: "orig"}
[2]*Point{{1.5, -3.5}, {}}        // same as [2]*Point{&Point{1.5, -3.5}, &Point{}}
[2]PPoint{{1.5, -3.5}, {}}        // same as [2]PPoint{PPoint(&Point{1.5, -3.5}), PPoint(&Point{})}
```

Examples of valid array, slice and map literals:

```go
// list of prime numbers
primes := []int{2, 3, 5, 7, 9, 2147483647}

// vowels[ch] is true if ch is a vowel
vowels := [128]bool{'a': true, 'e': true, 'i': true, 'o': true, 'u': true, 'y': true}

// the array [10]float32{-1, 0, 0, 0, -0.1, -0.1, 0, 0, 0, -1}
filter := [10]float32{-1, 4: -0.1, -0.1, 9: -1}

// frequencies in Hz for equal-tempered scale (A4 = 440Hz)
noteFrequency := map[string]float32{
	"C0": 16.35, "D0": 18.35, "E0": 20.60, "F0": 21.83,
	"G0": 24.50, "A0": 27.50, "B0": 30.87,
}
```

### 11.4 Function literals

A function literal represents an anonymous function. Function literals cannot declare type parameters, and their parameters cannot have default values (section 10.13, Default parameter values).

```
FunctionLit = "func" Signature FunctionBody .
```

```go
func(a, b int, z float64) bool { return a*b < int(z) }
```

A function literal can be assigned to a variable or invoked directly.

```go
f := func(x, y int) int { return x + y }
func(ch chan int) { ch <- ACK }(replyChan)
```

Function literals are closures: they may refer to variables declared in a surrounding function. Those variables are shared between the surrounding function and the function literal, which means the literal refers to them where they live, by reference. A captured variable stays in the frame of the function that declares it. Nothing is moved to the heap because a literal captures it (section 18.2, Where variables live).

A function literal that captures no variable is an ordinary function value and may be used anywhere. A function literal that captures variables is a **closure record**: the code to run together with where each captured variable is. The record is built in the frame of the function that evaluates the literal, and it is a view of that frame (section 18.3, Views). Section 11.5 states where such a record may go.

**Captured owners.** Captured owners stay where they are declared. The literal may use a captured owner, call its methods, take its address and assign a new value to it, which drops the old value (section 18.6, Drops). Moving a captured owner inside the literal (passing it by value, returning it, sending it) is an error (E3305). The literal may run several times, or not at all, so the enclosing function could not tell whether the variable still holds a value. The one exception is an owner that a task started by a `sync.Scope` takes: the task has its own, and its literal may move it (section 19.4, Scoped tasks, which states the rule for scoped tasks and owners). Pass the owner to the literal as an argument, or move it before or after the literal runs. The enclosing function still owns the captured variable and drops it at the end of its scope. A deferred literal runs before the function's own variables are dropped, so it may use them. It may not use an owner declared in an inner block, which is dropped when that block ends (E3311).

```go
func process(items []Item) vec.Vec[int] {
	sizes := vec.New[int]()
	each(items, func(it *Item) {
		sizes.Push(it.size) // uses the captured owner: allowed
	})
	return sizes // moves it to the caller after the literal is done
}
```

**Loop variables.** Every iteration of a loop reuses the memory of the variables declared in the loop's header and body (section 18.4, Loop iterations reuse their storage). A function literal that uses such a variable is valid for one iteration. Calling it, passing it down and storing it in a variable declared in the body are allowed. Keeping it where the next iteration can see it is an error (E3006). Section 18.4 (Loop iterations reuse their storage) gives the rule in full.

```go
var first func() int
for i := 0; i < 3; i++ {
	f := func() int { return i * 10 }
	println(f()) // allowed: used within the iteration
	if i == 0 {
		first = f // error: the next iteration reuses i
	}
}
```

### 11.5 Closures and their regions

This section defines where closure records may live. The captured variables of a closure record stay in a frame (section 11.4, Function literals), so a closure is valid while that frame is.

**Uses within a frame.** These uses of a closure are allowed:

- calling it;
- passing it as an argument;
- assigning it to a local variable;
- deferring it;
- returning it, as described below.

Storing it in a package-level variable, or through a pointer into memory that may outlive the frame, is an error (E3002). Starting it with a `task` statement is an error (section 12.12, Task statements).

**Returned closures.** Any function may return a closure. A function whose results can hold a closure (a func result, an interface, or a struct or array that holds one) builds the closures it may return in a **region** of memory that its caller provides. The region holds the closure records, the variables they capture, and the regions of the calls whose closures the function returns in turn.

The caller reserves the region in its own frame, like a local variable. The closure therefore lives as long as the caller's frame, captures by reference, and costs no allocation. A caller that returns the closure in turn passes part of its own region down instead, so a chain of such calls reserves the space once, in the frame where the closure is last used. Iterators are the common case, written with whatever checks and early returns they need:

```go
func Chunk[Slice ~[]E, E any](s Slice, n int) iter.Seq[Slice] {
	if n < 1 {
		panic("cannot be less than 1")
	}
	return func(yield func(Slice) bool) {
		for i := 0; i < len(s); i += n {
			end := min(n, len(s[i:]))
			if !yield(s[i : i+end : i+end]) {
				return
			}
		}
	}
}
```

The region holds only what is listed above. A returned closure that points to another variable of the function through a pointer it captures (`p := &x`, where the closure uses `p` but not `x`) would point into a frame that is gone. That is an error (E3001). What calling the closure returns, or passes to `yield`, may be in the region too (a closure built there, or the address of a variable it captures), and is valid as long as the caller's frame, like the closure itself.

The size of a region is fixed when compiling. A call through a func value or an interface reserves the largest region of any function of its signature. A function that returns closures is an ordinary function: it can be used as a func value, a method value or an interface method, and it may be inlined. A loop over `v.All()` can therefore compile to the loop that `All` runs. Strings that the closures capture are counted as in any variable (section 18.8, Strings). The region holds references to them, released when the call runs again and refills the region, and when the caller returns.

**What a region cannot hold.** The compiler follows closures through variables, fields, elements, interfaces, pointers and calls, without regard to the order of statements. It reports these cases:

- A function that returns a closure made by a function literal nested in it, or in the body of a range-over-func loop (section 12.11, Range over functions). The literal is a function of its own, whose frame is gone when it returns. Also a function whose results hold a closure only through a pointer, for which its callers provide no region (E3100). The fix is to make the closure in the function that returns it, and to return it by value.
- A closure that a call in a loop returns, kept past the iteration in a variable declared outside the loop, in a variable of the `for` statement (which passes its value on to the next iteration), or in memory reached through a pointer (E3101). Every iteration's call reuses the call's region, so the next iteration would overwrite it. A loop made by a `goto` back to a label before the call counts too (section 12.17, Goto statements). This rules out composing closures in a loop, `it = Filter(it, f)`: each call's closure would hold the one the same call made before, in the region that the call is about to refill. That is a chain as long as the loop runs, which no region of fixed size can hold. Compose a fixed number of closures, or keep what each step needs in a slice and return one closure that applies every step. Composing closures within the iteration, and passing them to functions that call or range over them, is allowed. The compiler follows what each function does with the closures it is given, and what those closures do when called. A closure is kept if any of them stores it, or what it holds, in a package-level variable or through a pointer, or passes it to a func value or an interface.
- A function that returns the closures of a call that can reach the same function again, directly, through other functions, or through a func value or interface whose implementations do, when any of them builds a closure (E3102). Its region would have to hold itself, and no fixed size fits. Build the closures in a loop, or keep their state in a value the caller owns.

A function literal in a loop is built the same way: one record per literal, reused on every iteration.

A call through a func value or an interface is assumed to return and keep anything passed to it. This follows the principle that doubt resolves to a compile error (section 1.2, Design rules), so the check may reject a function that would never return or keep the record at run time.

**Refilling.** No closure that a call builds in its region reaches the next run of that call, by the rules above. The region is therefore released and refilled when the call runs again, and the arguments of that run cannot refer to it. The address of a variable in a region (a variable that a returned closure captures, whose address the function also returns or stores) is a view of the caller's frame at that call, like a loop variable's. It must not be kept where the next run of the call can see it. That is checked with the other frame views (section 18.3, Views).

### 11.6 Primary expressions

Primary expressions are the operands for unary and binary expressions.

```
PrimaryExpr   = Operand |
                Conversion |
                MethodExpr |
                PrimaryExpr Selector |
                PrimaryExpr Index |
                PrimaryExpr Slice |
                PrimaryExpr TypeAssertion |
                PrimaryExpr Arguments .

Selector      = "." identifier .
Index         = "[" Expression [ "," ] "]" .
Slice         = "[" [ Expression ] ":" [ Expression ] "]" |
                "[" [ Expression ] ":" Expression ":" Expression "]" .
TypeAssertion = "." "(" Type ")" .
Arguments     = "(" [ ( ExpressionList | Type [ "," ExpressionList ] ) [ "..." ] [ "," ] ] ")" .
```

```go
x
2
(s + ".txt")
f(3.1415, true)
Point{1, 2}
m["foo"]
s[i : j + 1]
obj.color
f.p[i].x()
```

An `Arguments` list may begin with a type only for the built-in functions that take one, such as `new` and `make` (chapter 13).

### 11.7 Selectors

For a primary expression `x` that is not a package name, the selector expression

```go
x.f
```

denotes the field or method `f` of the value `x` (or sometimes `*x`; see below). The identifier `f` is called the (field or method) selector. It must not be the blank identifier. The type of the selector expression is the type of `f`. If `x` is a package name, see section 11.2.

A selector `f` may denote a field or method `f` of a type `T`, or it may refer to a field or method `f` of a nested embedded field of `T`. The number of embedded fields traversed to reach `f` is called its depth in `T`. The depth of a field or method `f` declared in `T` is zero. The depth of a field or method `f` declared in an embedded field `A` in `T` is the depth of `f` in `A` plus one.

The following rules apply to selectors:

1. For a value `x` of type `T` or `*T` where `T` is not a pointer or interface type, `x.f` denotes the field or method at the shallowest depth in `T` where there is such an `f`. If there is not exactly one `f` with shallowest depth, the selector expression is illegal.
2. For a value `x` of type `I` where `I` is an interface type, `x.f` denotes the actual method with name `f` of the dynamic value of `x`. If there is no method with name `f` in the method set of `I` (section 8.6, Method sets), the selector expression is illegal.
3. As an exception, if the type of `x` is a defined pointer type and `(*x).f` is a valid selector expression denoting a field (but not a method), `x.f` is shorthand for `(*x).f`.
4. For a value `x` of type `owned[T]`, `shared[T]` or `synced[T]` (section 7.19, `owned`, `shared`, `synced` and `weak`), `x.f` is shorthand for `(*x).f`, where `*x` is the object of the box, and `x.f` may denote a method with a pointer receiver of `T`. A struct may embed one of these types. Its selectors then promote the fields and methods of `T` as an embedded `*T` does (section 7.6, Struct types).
5. In all other cases, `x.f` is illegal.

The `Drop` method of an embedded owner is never promoted (section 10.14, Method declarations).

If `x` is of pointer type and has the value `nil` and `x.f` denotes a struct field, assigning to or evaluating `x.f` is a run-time panic (section 17.5, Faults). If `x` is of interface type and has the value `nil`, calling or evaluating the method `x.f` is a run-time panic. The same holds for a nil box.

**Selecting a field of an owner.** Reading a field of a variable of owner type reads it in place. If the field is a value, the selector yields a copy of the field. If the field is itself an owner, the selector cannot move it out (E3303). Use `take(&x.f)` to move the value out and leave the zero value behind (section 13.10, Moving out of a place).

**Enum members and union variants.** If `x` is the name of an enum type `E`, then `E.M` denotes the member `M` of `E`, a typed constant of type `E` (section 7.12, Enums). If `x` is the name of a union type `U`, then `U.V` denotes the variant `V` of `U`. It is used as `U.V` for a variant without a payload, as the call `U.V(p)` or the literal `U.V{...}` for a variant with a payload, and as the function value `U.V` of type `func(P) U` (section 7.13, Unions). In both cases `x` may be qualified by a package (`pkg.E.M`, `pkg.U.V`), instantiated (`Result[int].Ok`), or an alias. On a type name, a member or variant takes precedence over a method of the same name. Members and variants enter no scope, and an unexported member or variant is not visible in other packages. A variant is not a type, so `var v U.V` is an error (E2409).

For example, given the declarations

```go
type T0 struct {
	x int
}

func (*T0) M0() {}

type T1 struct {
	y int
}

func (T1) M1() {}

type T2 struct {
	z int
	T1
	*T0
}

func (*T2) M2() {}

type Q *T2

var t T2     // with t.T0 != nil
var p *T2    // with p != nil and (*p).T0 != nil
var q Q = p
```

one may write:

```go
t.z // t.z
t.y // t.T1.y
t.x // (*t.T0).x

p.z // (*p).z
p.y // (*p).T1.y
p.x // (*(*p).T0).x

q.x // (*(*q).T0).x        (*q).x is a valid field selector

p.M0() // ((*p).T0).M0()      M0 expects *T0 receiver
p.M1() // ((*p).T1).M1()      M1 expects T1 receiver
p.M2() // p.M2()              M2 expects *T2 receiver
t.M2() // (&t).M2()           M2 expects *T2 receiver, see section 11.14
```

but the following is invalid:

```go
q.M0() // (*q).M0 is valid but not a field selector
```

### 11.8 Method expressions

If `M` is in the method set of type `T` (section 8.6, Method sets), `T.M` is a function that is callable as a regular function with the same arguments as `M`, prefixed by an additional argument that is the receiver of the method.

```
MethodExpr   = ReceiverType "." MethodName .
ReceiverType = Type .
```

Consider a struct type `T` with two methods, `Mv`, whose receiver is of type `T`, and `Mp`, whose receiver is of type `*T`.

```go
type T struct {
	a int
}

func (tv T) Mv(a int) int         { return 0 } // value receiver
func (tp *T) Mp(f float32) float32 { return 1 } // pointer receiver

var t T
```

The expression `T.Mv` yields a function equivalent to `Mv` but with an explicit receiver as its first argument. It has the signature `func(tv T, a int) int`. That function may be called normally with an explicit receiver, so these five invocations are equivalent:

```go
t.Mv(7)
T.Mv(t, 7)
(T).Mv(t, 7)
f1 := T.Mv; f1(t, 7)
f2 := (T).Mv; f2(t, 7)
```

Similarly, the expression `(*T).Mp` yields a function value representing `Mp` with signature `func(tp *T, f float32) float32`.

For a method with a value receiver, one can derive a function with an explicit pointer receiver, so `(*T).Mv` yields a function value representing `Mv` with signature `func(tv *T, a int) int`. Such a function indirects through the receiver to create a value to pass as the receiver to the underlying method. The method does not overwrite the value whose address is passed in the function call. If `T` is an owner type, `(*T).Mv` is an error (E3303), because indirecting through a pointer would copy the owner.

The final case, a value-receiver function for a pointer-receiver method, is illegal because pointer-receiver methods are not in the method set of the value type.

Function values derived from methods are called with function call syntax. The receiver is provided as the first argument to the call. That is, given `f := T.Mv`, `f` is invoked as `f(t, 7)`, not `t.f(7)`. To construct a function that binds the receiver, use a function literal or a method value (section 11.9, Method values).

If `T` is an owner type and `Mv` has a value receiver, `Mv` consumes its receiver (section 8.6, Method sets). The function `T.Mv` then consumes its first argument, which is moved into the call like any other owner argument (section 11.14, Calls). For `Conn.Close(c)` the variable `c` is moved.

It is legal to derive a function value from a method of an interface type. The resulting function takes an explicit receiver of that interface type.

A function value derived from a method has every parameter of the method. Default parameter values are not part of a function type, so none are applied (section 10.13, Default parameter values).

### 11.9 Method values

If the expression `x` has static type `T` and `M` is in the method set of type `T`, `x.M` is called a method value. The method value `x.M` is a function value that is callable with the same arguments as a method call of `x.M`. The expression `x` is evaluated and saved during the evaluation of the method value. The saved copy is then used as the receiver in any calls, which may be executed later.

```go
type S struct{ *T }
type T int

func (t T) M() { print(t) }

var v T
t := &v
s := S{T: t}
f := t.M // receiver *t is evaluated and stored in f
g := s.M // receiver *(s.T) is evaluated and stored in g
*t = 42  // does not affect stored receivers in f and g
```

The type `T` may be an interface or non-interface type.

As in 11.8, consider a struct type `T` with two methods, `Mv`, whose receiver is of type `T`, and `Mp`, whose receiver is of type `*T`.

```go
type T struct {
	a int
}

func (tv T) Mv(a int) int         { return 0 } // value receiver
func (tp *T) Mp(f float32) float32 { return 1 } // pointer receiver

var t T
var pt *T

func makeT() T
```

The expression `t.Mv` yields a function value of type `func(int) int`. These two invocations are equivalent:

```go
t.Mv(7)
f := t.Mv; f(7)
```

Similarly, the expression `pt.Mp` yields a function value of type `func(float32) float32`.

As with selectors, a reference to a non-interface method with a value receiver using a pointer automatically dereferences that pointer: `pt.Mv` is equivalent to `(*pt).Mv`.

As with method calls, a reference to a non-interface method with a pointer receiver using an addressable value automatically takes the address of that value: `t.Mp` is equivalent to `(&t).Mp`.

```go
f := t.Mv; f(7)  // like t.Mv(7)
f := pt.Mp; f(7) // like pt.Mp(7)
f := pt.Mv; f(7) // like (*pt).Mv(7)
f := t.Mp; f(7)  // like (&t).Mp(7)
f := makeT().Mp  // invalid: result of makeT() is not addressable
```

Although the examples above use non-interface types, it is also legal to create a method value from a value of interface type.

```go
var i interface{ M(int) } = myVal
f := i.M; f(7) // like i.M(7)
```

**Where the record lives.** A method value is a closure record (section 11.4, Function literals). It holds a copy of the receiver, or the address of the receiver variable for a pointer method of an addressable value. The record is built in the frame of the function that evaluates the method value, and it is a view of that frame (section 18.3, Views). The rules of section 11.5 apply to it. A copy of the receiver that holds strings counts them, and releases them when the record's storage is released. A method value cannot be started with a `task` statement (section 12.12, Task statements). Default parameter values are not applied to a call of a method value (section 10.13, Default parameter values). A method value of a method with its own type parameters must be instantiated (section 10.15, Methods with type parameters).

**Consuming methods.** A method with a value receiver on an owner type consumes its receiver (section 8.6, Method sets). Such a method has no method value. `c.Close`, where `Close` consumes `c`, is an error. A record that held `c` would own it, and every call of the record would consume it again. Call the method directly, `c.Close()`, or use the method expression, `Conn.Close(c)` (section 11.8, Method expressions).

```go
type Conn owner struct{ fd int32 }

func (c Conn) Close() error { return nil } // value receiver: consumes c
func (c *Conn) Fd() int32   { return c.fd } // pointer receiver: borrows c

var c Conn
fd := c.Fd  // allowed: the record holds the address of c
done := c.Close // error: Close consumes c, so it has no method value
_ = Conn.Close  // allowed: a function that takes the Conn as its argument
```

### 11.10 Index expressions

A primary expression of the form

```go
a[x]
```

denotes the element of the array, pointer to array, slice, string or map `a` indexed by `x`. The value `x` is called the index or map key, respectively. Indexing a value of any other type is possible only for library containers (section 11.11, Indexing library containers). The following rules apply.

If `a` is neither a map nor a type parameter:

- the index `x` must be an untyped constant, or its type must be an integer or a type parameter whose type set contains only integer types (an enum value is an integer for this purpose, 7.12);
- a constant index must be non-negative and representable by a value of type `int`;
- a constant index that is untyped is given type `int`;
- the index `x` is in range if `0 <= x < len(a)`, otherwise it is out of range.

For `a` of array type `A`:

- a constant index must be in range;
- if `x` is out of range at run time, a run-time panic occurs (section 17.1, Panics);
- `a[x]` is the array element at index `x`, and the type of `a[x]` is the element type of `A`.

For `a` of pointer to array type, `a[x]` is shorthand for `(*a)[x]`.

For `a` of slice type `S`:

- if `x` is out of range at run time, a run-time panic occurs;
- `a[x]` is the slice element at index `x`, and the type of `a[x]` is the element type of `S`.

For `a` of string type:

- a constant index must be in range if the string `a` is also constant;
- if `x` is out of range at run time, a run-time panic occurs;
- `a[x]` is the non-constant byte value at index `x`, and the type of `a[x]` is `byte`;
- `a[x]` may not be assigned to.

For `a` of map type `M`:

- `x`'s type must be assignable to the key type of `M`;
- if the map contains an entry with key `x`, `a[x]` is the map element with key `x`, and the type of `a[x]` is the element type of `M`;
- if the map is `nil` or does not contain such an entry, `a[x]` is the zero value for the element type of `M`.

For `a` of type parameter type `P`:

- the index expression `a[x]` must be valid for values of all types in `P`'s type set;
- the element types of all types in `P`'s type set must be identical. In this context, the element type of a string type is `byte`;
- if there is a map type in the type set of `P`, all types in that type set must be map types, and the respective key types must be all identical;
- `a[x]` is the array, slice or string element at index `x`, or the map element with key `x` of the type argument that `P` is instantiated with, and the type of `a[x]` is the type of the (identical) element types;
- `a[x]` may not be assigned to if `P`'s type set includes string types.

Otherwise `a[x]` is illegal.

An index expression on a map `a` of type `map[K]V` used in an assignment statement or initialization of the special form

```go
v, ok = a[x]
v, ok := a[x]
var v, ok = a[x]
```

yields an additional untyped boolean value. The value of `ok` is `true` if the key `x` is present in the map, and `false` otherwise.

Assigning to an element of a `nil` map causes a run-time panic. A map element is not addressable: `&m[k]` and the assignment `m[k].f = v` are errors. The statements `m[k] = v`, `m[k] += v` and `m[k]++` are allowed (section 12.6, IncDec statements; section 12.7, Assignment statements).

**Elements that are owners.** An index expression yields a copy of the element when it is read. For an element of owner type that would be a second owner, so reading such an element as a value is an error (E3303), as moving an owner out of any place other than a whole variable is (section 18.5, Moves). The elements of an array or slice of owners are used in place: `a[x].M()` calls a method with a pointer receiver on the element, and `&a[x]` is a view of it. To move an element out, use `take(&a[x])` (section 13.10, Moving out of a place). Assigning to such an element, `a[x] = v`, drops the old value first and moves `v` in (section 12.7, Assignment statements).

**Counted elements.** Reading an element that is a string, `shared`, `synced`, channel or map copies it and counts the copy (section 18.7, Shared ownership; section 18.8, Strings).

### 11.11 Indexing library containers

The growable containers of the standard library (`vec.Vec[T]`, `hash.Map[K, V]`) are owners, not built-in types. The index syntax is lent to them through methods. On an operand `x` whose type is not a string, array, pointer to array, slice or map, these forms call methods:

| Form | Calls | Notes |
|---|---|---|
| `x[k]` | `x.Index(k)` | a value of type `V` |
| `x[k] = v` | `x.SetIndex(k, v)` | also as a target of `a, x[k] = ...` and `for x[k] = range ...` |
| `v, ok := x[k]` | `x.IndexOk(k)` | also with `=` and `var v, ok = x[k]` |
| `x[k] op= v`, `x[k]++` | `x.Index(k)`, then `x.SetIndex(k, result)` | `x` and `k` are evaluated once |
| `len(x)` | `x.Len()` | |

```go
counts := hash.NewMap[string, int](0) // dropped, and its table freed, when the function returns
counts["a"] = 1      // counts.SetIndex("a", 1)
counts["a"] += 2     // counts.SetIndex("a", counts.Index("a")+2)
n := counts["zzz"]   // 0: hash.Map's Index returns the zero value for a missing key
n, ok := counts["a"] // 3, true: counts.IndexOk("a")

v := vec.New[int]()
v.Push(1)
v[0] *= 10 // bounds checked by Vec's methods: v[5] panics "index out of range [5] with length 1"
for i := 0; i < len(v); i++ { ... }
```

**The methods.** The methods are found in the method set of `x`'s type as for a call `x.Index(k)`. Methods with pointer receivers need an addressable `x` or a pointer (E2457), and indexing through a pointer works: `p := &counts; p["b"] = 2`. Their forms are fixed: `Index(k K) V`, `SetIndex(k K, v V)`, `IndexOk(k K) (V, bool)` and `Len() int`, with no type parameters of their own and no variadic parameter (E2452). A type may have any subset. Each use needs its own method (E2451), and the methods a type has must agree on `K` and `V` (E2453). The key is assigned to `K` as an argument is. A field named `Index` does not count.

**Built-in types are unaffected.** A string, array, pointer to array, slice or map is always indexed by the language, even when its named type declares an `Index` method. So is `len` of those types and of rings. In generic code the choice is made from the type parameter's constraint. A type parameter whose type set is made of indexable built-in types is indexed by the language. One constrained only by methods calls them, whatever its type argument.

**The result is a value.** `x[k]` is a copy returned by `Index`, never a variable, as a map element is not:

- `&x[k]` is an error (E2454). A pointer into a container's storage would be invalidated by any call that grows or rehashes it, which nothing in the syntax would show. Containers return such pointers from methods named for what they do, such as `vec.Vec.Ptr(i)`, `hash.Map.Ptr(k)` and `Upsert(k)`, whose documentation says how long the pointer stays valid.
- Assigning to a field or element of `x[k]` (`v[i].count = 1`) is an error (E2455). Calling a method with a pointer receiver on it is an error too. Read, change and store back, or go through `Ptr`.

**Ownership.** The receiver is borrowed for the call (a view, 18.3), so indexing never moves the container. `x[k] = v` moves `v` into the container when `V` is an owner, and `SetIndex` drops the value it replaces. `Index` returns a copy, which for an owner `V` would be a second owner. Reading `x[k]` from a container of owners is therefore an error (E3309). Such code uses `Ptr` to look at an element, or a method that takes it out (`Remove`, `Pop`).

**Deleting** from a container is a method, `m.Delete(k)`. The predeclared `delete` is for the built-in map (section 13.5, Deletion of map elements).

**Range.** A `for` statement over a value of such a type ranges over its `All` method (section 12.10, For statements).

**Generics.** `pluto/container` declares the constraints: `Indexable[K, V]` (`Index` and `SetIndex`), `Reader[K, V]`, `Lookup[K, V]` (`IndexOk`), `Sized` (`Len`) and `Iterable[K, V]` (`All`, for range). A value of a type parameter so constrained supports the corresponding syntax. The library's containers declare their methods on pointer receivers, so their pointers satisfy the constraints:

```go
func Count[M container.Indexable[K, int], K any](m M, keys []K) {
	for _, k := range keys {
		m[k] += 1
	}
}

Count(&counts, words)
```

### 11.12 Slice expressions

Slice expressions construct a substring or slice from a string, array, pointer to array or slice operand. There are two variants: a simple form that specifies a low and high bound, and a full form that also specifies a bound on the capacity.

If the operand type is a type parameter, unless its type set contains string types, all types in the type set must have the same underlying type, and the slice expression must be valid for an operand of that type. If the type set contains string types it may also contain byte slices with underlying type `[]byte`. In this case, the slice expression must be valid for an operand of string type.

**Simple slice expressions.** For a string, array, pointer to array or slice `a`, the primary expression

```go
a[low : high]
```

constructs a substring or slice. The indices `low` and `high` select which elements of operand `a` appear in the result. The result has indices starting at 0 and length equal to `high - low`. After slicing the array `a`

```go
a := [5]int{1, 2, 3, 4, 5}
s := a[1:4]
```

the slice `s` has type `[]int`, length 3, capacity 4, and elements

```go
s[0] == 2
s[1] == 3
s[2] == 4
```

For convenience, any of the indices may be omitted. A missing `low` index defaults to zero. A missing `high` index defaults to the length of the sliced operand:

```go
a[2:] // same as a[2 : len(a)]
a[:3] // same as a[0 : 3]
a[:]  // same as a[0 : len(a)]
```

If `a` is a pointer to an array, `a[low : high]` is shorthand for `(*a)[low : high]`.

For arrays or strings, the indices are in range if `0 <= low <= high <= len(a)`, otherwise they are out of range. For slices, the upper index bound is the slice capacity `cap(a)` rather than the length. A constant index must be non-negative and representable by a value of type `int`. For arrays or constant strings, constant indices must also be in range. If both indices are constant, they must satisfy `low <= high`. If the indices are out of range at run time, a run-time panic occurs.

Except for untyped strings, if the sliced operand is a string or slice, the result of the slice operation is a non-constant value of the same type as the operand. For untyped string operands the result is a non-constant value of type `string`. If the sliced operand is an array, it must be addressable, and the result of the slice operation is a slice with the same element type as the array.

If the sliced operand of a valid slice expression is a nil slice, the result is a nil slice. Otherwise, if the result is a slice, it shares its underlying array with the operand.

```go
var a [10]int
s1 := a[3:7]  // underlying array of s1 is array a; &s1[2] == &a[5]
s2 := s1[1:4] // underlying array of s2 is underlying array of s1 which is array a; &s2[1] == &a[5]
s2[1] = 42    // s2[1] == s1[2] == a[5] == 42; they all refer to the same underlying array element

var s []int
s3 := s[:0] // s3 == nil
```

**Slices are views.** A slice that a slice expression makes is a view (section 18.3, Views) of the array it shares. If the array is a local variable, the slice is a view of the frame. A slice of a string shares the string's buffer and keeps it alive, as a copy of the string does (section 18.8, Strings). A slice of an array of owners is a view of the owners and does not own them.

**Full slice expressions.** For an array, pointer to array or slice `a` (but not a string), the primary expression

```go
a[low : high : max]
```

constructs a slice of the same type, and with the same length and elements as the simple slice expression `a[low : high]`. Additionally, it controls the resulting slice's capacity by setting it to `max - low`. Only the first index may be omitted. It defaults to 0. After slicing the array `a`

```go
a := [5]int{1, 2, 3, 4, 5}
t := a[1:3:5]
```

the slice `t` has type `[]int`, length 2, capacity 4, and elements

```go
t[0] == 2
t[1] == 3
```

As for simple slice expressions, if `a` is a pointer to an array, `a[low : high : max]` is shorthand for `(*a)[low : high : max]`. If the sliced operand is an array, it must be addressable.

The indices are in range if `0 <= low <= high <= max <= cap(a)`, otherwise they are out of range. A constant index must be non-negative and representable by a value of type `int`. For arrays, constant indices must also be in range. If multiple indices are constant, the constants that are present must be in range relative to each other. If the indices are out of range at run time, a run-time panic occurs.

**Strings slice on rune boundaries.** Every string holds valid UTF-8 (section 7.3, String types). A string is therefore sliced only between runes. A byte offset `k` of a string `s` is a **rune boundary** if `k` is 0, if `k` is `len(s)`, or if the byte `s[k]` is not a UTF-8 continuation byte (a byte of the form `10xxxxxx`). In `s[low : high]`, both `low` and `high` must be rune boundaries of `s`. If either is not, a run-time panic occurs (section 17.1, Panics). The message names the offset and the helper function to use, for example:

```
panic: slice bound 2 is not at the start of a rune: a string holds valid UTF-8, so it can only be sliced between runes (use strings.Truncate to cut at a byte count)
```

If `s` and both indices are constant, the check is made when compiling and a failure is an error. An ASCII string has a rune boundary at every offset, so slicing it never panics for this reason. The check is part of the meaning of the expression. An implementation may leave it out only where the bound is known to be a rune boundary.

This is the one documented exception to the no-false-friends rule (section 1.2, Design rules; appendix A, section A.5). The standard library provides the fix. `strings.Truncate(s string, n int) string` returns the longest prefix of `s` of at most `n` bytes that ends on a rune boundary, and `utf8.RuneStartBefore(s string, i int) int` returns the greatest rune boundary of `s` that is at most `i`:

```go
if len(title) > 40 {
	title = strings.Truncate(title, 40) + "..." // never splits a rune
}
```

A string made with `unsafe.String` (section 20.1, Package `unsafe`) may hold bytes that are not valid UTF-8. Slicing such a string uses the same boundary rule on its bytes.

### 11.13 Type assertions

For an expression `x` of interface type, but not a type parameter, and a type `T`, the primary expression

```go
x.(T)
```

asserts that `x` is not `nil` and that the value stored in `x` is of type `T`. The notation `x.(T)` is called a type assertion.

More precisely, if `T` is not an interface type, `x.(T)` asserts that the dynamic type of `x` is identical to the type `T`. In this case, `T` must implement the (interface) type of `x`. Otherwise the type assertion is invalid, since it is not possible for `x` to store a value of type `T`. If `T` is an interface type, `x.(T)` asserts that the dynamic type of `x` implements the interface `T`.

If the type assertion holds, the value of the expression is the value stored in `x`, and its type is `T`. If the type assertion is false, a run-time panic occurs (section 17.1, Panics). In other words, even though the dynamic type of `x` is known only at run time, the type of `x.(T)` is known to be `T` in a correct program.

```go
var x interface{} = 7 // x has dynamic type int and value 7
i := x.(int)          // i has type int and value 7

type I interface{ m() }

func f(y I) {
	s := y.(string)    // illegal: string does not implement I (missing method m)
	r := y.(io.Reader) // r has type io.Reader and the dynamic type of y must implement both I and io.Reader
	…
}
```

A type assertion used in an assignment statement or initialization of the special form

```go
v, ok = x.(T)
v, ok := x.(T)
var v, ok = x.(T)
var v, ok interface{} = x.(T) // dynamic types of v and ok are T and bool
```

yields an additional untyped boolean value. The value of `ok` is `true` if the assertion holds. Otherwise it is `false` and the value of `v` is the zero value for type `T`. No run-time panic occurs in this case.

**Unions.** For an expression `u` of union type `U` and a variant `V` of `U`, `u.(U.V)` yields the payload of `V` and panics if `u` holds another variant. The form `p, ok := u.(U.V)` reports instead, with `p` the zero payload when `ok` is false. A bare variant name is not accepted in an assertion (section 7.13, Unions).

**Owners.** An ordinary interface (`any`, `error`, `io.Reader`, any interface type not declared `owner`) only lends the value it holds (section 7.9, Interface types). Asserting an ordinary interface to an owner type is an error (E3308), since the result would be a second owner of the lent value. Assert it to an interface type and use the value's methods instead.

A type assertion from an **owning interface** to an owner type moves the value out of the interface. The operand `x` must be a whole variable or a temporary, and it holds nothing afterward. In the comma-ok form `v, ok := x.(T)`, `x` is moved only where `ok` is true, and it is still usable on the path where `ok` is false. The same holds for an owner union: `f, ok := s.(Slot.Open)` moves the payload out only where `ok` is true, and the single-value form `f := s.(Slot.Open)` moves it out and leaves `s` empty (section 7.13, Unions; section 18.5, Moves).

**Frame copies.** An interface that holds a value larger than a word holds a copy of it in the frame (section 7.9, Interface types). Asserting it to its type yields a copy of that value, which is subject to the rules of its own type.

### 11.14 Calls

Given an expression `f` of function type `F`,

```go
f(a1, a2, … an)
```

calls `f` with arguments `a1, a2, … an`. Except for one special case, arguments must be single-valued expressions assignable to the parameter types of `F`, and are evaluated before the function is called (section 11.26, Order of evaluation). The type of the expression is the result type of `F`. A method invocation is similar, but the method itself is specified as a selector upon a value of the receiver type for the method.

```go
math.Atan2(x, y) // function call
var pt *Point
pt.Scale(3.5) // method call with receiver pt
```

If `f` denotes a generic function, it must be instantiated (section 11.16, Instantiations) before it can be called or used as a function value. If the type of `f` is a type parameter, all types in its type set must have the same underlying type, which must be a function type, and the function call must be valid for that type.

In a function call, the function value and the arguments are evaluated in the order of section 11.26. After they are evaluated, new storage is allocated for the function's variables, which includes its parameters and results. Then the arguments of the call are passed to the function, which means that they are assigned to their corresponding function parameters, and the called function begins execution. The return parameters of the function are passed back to the caller when the function returns. Calling a nil function value causes a run-time panic.

**The special case.** As a special case, if the return values of a function or method `g` are equal in number and individually assignable to the parameters of another function or method `f`, then the call `f(g(parameters_of_g))` invokes `f` after passing the return values of `g` to the parameters of `f` in order. The call of `f` must contain no parameters other than the call of `g`, and `g` must have at least one return value. If `f` has a final `...` parameter, it is assigned the return values of `g` that remain after assignment of regular parameters. If `g` returns fewer values than `f` has parameters, the remaining parameters of `f` take their default values, provided they all have one.

```go
func Split(s string, pos int) (string, string) {
	return s[0:pos], s[pos:]
}

func Join(s, t string) string {
	return s + t
}

if Join(Split(value, len(value)/2)) != value {
	log.Panic("test fails")
}
```

**Default parameter values.** A call may omit any suffix of the parameters that have default values (section 10.13, Default parameter values). Each omitted parameter takes its default as follows:

- The default expression is evaluated by this call, after all explicit arguments, in the order of the parameters. Its temporaries live in the caller's frame.
- The names in the default are resolved in the package that declares the callee, so a call from another package may run the callee's unexported helpers.
- For a `defer` statement, the defaults are evaluated with the other arguments when the statement runs (section 12.19, Defer statements).
- The type arguments of the callee's instance are the type arguments the default sees. Omitted parameters take no part in type inference (section 11.17, Type inference), so a type parameter that appears only in omitted parameters must be given explicitly.
- Defaults apply to calls that name a declared function or method directly: `f(x)`, `pkg.F(x)`, `F[T](x)` and `v.M(x)`, including promoted methods and methods with type parameters. A function value, a method value, a method expression and a call through an interface have every parameter, since function types do not record defaults. After `g := pkg.F`, the value `g` is called with every argument.

```go
func Grow(n int, factor int = 2, limit int = 1 << 20) int

Grow(10)     // factor = 2, limit = 1 << 20
Grow(10, 3)  // limit = 1 << 20
g := Grow    // g has type func(int, int, int) int
g(10, 2, 64) // a function value takes every argument
```

**Method calls.** A method call `x.m()` is valid if the method set of (the type of) `x` contains `m` and the argument list can be assigned to the parameter list of `m`. If `x` is addressable and `&x`'s method set contains `m`, `x.m()` is shorthand for `(&x).m()`:

```go
var p Point
p.Scale(3.5)
```

The receiver of a pointer-receiver method may also be a temporary of owner type, such as the result of a call. The temporary is the receiver for the duration of the statement. A view of it, such as the slice that `readAll(f).Slice()` returns, may be passed to calls in the same statement and may not be kept (section 18.3, Views; section 18.6, Drops). For a temporary of any other type, a pointer-receiver method needs an addressable operand, and the result of a call is not addressable.

There is no distinct method type and there are no method literals.

**Passing arguments.** An argument is assigned to its parameter (section 12.7, Assignment statements), with these consequences for the kinds of section 8.7:

- A value is copied.
- An owner that is a whole variable is **moved** into the parameter (section 18.5, Moves). The callee drops it when it returns, unless it moves it on. An owner that is not a whole variable (a field, an element, `*p`) cannot be moved out (E3303). Use `take` (section 13.10, Moving out of a place).
- A string is **borrowed**: the caller keeps it alive for the call, and the callee counts only the copies it keeps (section 18.8, Strings). A `shared`, `synced` or `weak` reference, a channel or a map is copied, and the copy is counted.
- A view is passed unchanged. Passing a view down to a call is always allowed (section 18.3, Views). If the callee keeps it, the callee's summary decides whether the view may outlive what it looks at.
- A receiver with a value receiver on an owner type is moved into the call like an argument (section 8.6, Method sets).

**Results.** A result of owner type is handed to the caller. A result that the call expression does not store is a temporary. It is dropped at the end of the statement that evaluates the call (section 12.4, Expression statements; section 18.6, Drops).

**Moves and uses in one expression.** An expression that moves a variable and uses the same variable anywhere else in the expression is an error, whatever the order of evaluation. A use is any read, borrow or move, including a use inside a function literal in the expression. The fix is a line before the expression:

```go
send(buf, buf.Len()) // error: buf is moved by this call and used in another argument
send(buf.Len(), buf) // error: the same, in the other order
n := buf.Len()
send(buf, n)         // allowed
```

The rule does not depend on 11.26, so a reader need not know the order of evaluation to know whether the code compiles. Assigning the result back to the moved variable is not a use (`v = grow(v)`). Permuting assignments (`a, b = b, a`) are not affected (section 12.7, Assignment statements).

An explicit call `x.Drop()` on an owner variable ends its life (section 18.6, Drops). Calling `Drop` in any other way is an error (E3307).

### 11.15 Passing arguments to `...` parameters

If `f` is variadic with a final parameter `p` of type `...T`, then within `f` the type of `p` is equivalent to type `[]T`. If `f` is invoked with no actual arguments for `p`, the value passed to `p` is `nil`. Otherwise, the value passed is a new slice of type `[]T` with a new underlying array whose successive elements are the actual arguments, which all must be assignable to `T`. The length and capacity of the slice is therefore the number of arguments bound to `p`, and may differ for each call site.

Given the function and calls

```go
func Greeting(prefix string, who ...string)
Greeting("nobody")
Greeting("hello:", "Joe", "Anna", "Eileen")
```

within `Greeting`, `who` has the value `nil` in the first call, and `[]string{"Joe", "Anna", "Eileen"}` in the second.

If the final argument is assignable to a slice type `[]T` and is followed by `...`, it is passed unchanged as the value for a `...T` parameter. In this case no new slice is created.

Given the slice `s` and call

```go
s := []string{"James", "Jasmine"}
Greeting("goodbye:", s...)
```

within `Greeting`, `who` has the same value as `s` with the same underlying array.

The array that holds the actual arguments is a temporary in the frame of the caller (static storage in a package-level initializer), and the slice is a view of it (section 18.3, Views). The callee may use it and pass it down. It may not keep it. Owners cannot be passed as variadic arguments, since the slice would be a view that owns nothing (E3310). A function with a `...` parameter cannot have parameters with default values (section 10.13, Default parameter values).

### 11.16 Instantiations

A generic function, method or type is instantiated by substituting type arguments for the type parameters. Instantiation proceeds in two steps:

1. Each type argument is substituted for its corresponding type parameter in the generic declaration. This substitution happens across the entire function or type declaration, including the type parameter list itself and any types in that list.
2. After substitution, each type argument must satisfy the constraint (instantiated, if necessary) of the corresponding type parameter. Otherwise instantiation fails.

Instantiating a generic type, function or method results in a non-generic type, function or method, respectively. An instantiated function is an ordinary function in the program.

```
type parameter list    type arguments    after substitution

[P any]                int               int satisfies any
[S ~[]E, E any]        []int, int        []int satisfies ~[]int, int satisfies any
[P io.Writer]          string            illegal: string doesn't satisfy io.Writer
[P comparable]         any               any satisfies (but does not implement) comparable
```

An owner type satisfies a constraint only if the constraint is marked `owner` (section 10.9, Type parameter declarations). A type parameter without `owner` cannot be instantiated with an owner type (E3207). A union type is a valid type argument, and unions cannot appear in constraints. A method with type parameters of its own is instantiated with them after its receiver's type arguments (section 10.15, Methods with type parameters).

When using a generic function or method, type arguments may be provided explicitly, or they may be partially or completely inferred (section 11.17, Type inference) from the context in which the function is used. Provided that they can be inferred, type argument lists may be omitted entirely if the function is

- called with ordinary arguments,
- assigned to a variable with a known type,
- passed as an argument to another function, or
- returned as a result.

In all other cases, a (possibly partial) type argument list must be present. If a type argument list is absent or partial, all missing type arguments must be inferable from the context in which the function is used.

```go
// sum returns the sum (concatenation, for strings) of its arguments.
func sum[T ~int | ~float64 | ~string](x ...T) T { … }

x := sum                  // illegal: the type of x is unknown
intSum := sum[int]        // intSum has type func(x ...int) int
a := intSum(2, 3)         // a has value 5 of type int
b := sum[float64](2.0, 3) // b has value 5.0 of type float64
c := sum(b, -1)           // c has value 4.0 of type float64

type sumFunc func(x ...string) string
var f sumFunc = sum // same as var f sumFunc = sum[string]
f = sum             // same as f = sum[string]
```

A partial type argument list cannot be empty. At least the first argument must be present. The list is a prefix of the full list of type arguments, leaving the remaining arguments to be inferred. Loosely speaking, type arguments may be omitted from right to left.

```go
func apply[S ~[]E, E any](s S, f func(E) E) S { … }

f0 := apply[]                 // illegal: type argument list cannot be empty
f1 := apply[[]int]            // type argument for S explicitly provided, type argument for E inferred
f2 := apply[[]string, string] // both type arguments explicitly provided

var bytes []byte
r := apply(bytes, func(byte) byte { … }) // both type arguments inferred from the function arguments
```

For a generic type, all type arguments must always be provided explicitly.

### 11.17 Type inference

A use of a generic function may omit some or all type arguments if they can be inferred from the context within which the function is used, including the constraints of the function's type parameters. Type inference succeeds if it can infer the missing type arguments and instantiation succeeds with the inferred type arguments. Otherwise, type inference fails and the program is invalid.

Type inference uses the type relationships between pairs of types. For instance, a function argument must be assignable to its respective function parameter. This establishes a relationship between the type of the argument and the type of the parameter. If either of these two types contains type parameters, type inference looks for the type arguments to substitute the type parameters with, such that the assignability relationship is satisfied. Similarly, type inference uses the fact that a type argument must satisfy the constraint of its respective type parameter.

Each such pair of matched types corresponds to a type equation containing one or multiple type parameters, from one or possibly multiple generic functions. Inferring the missing type arguments means solving the resulting set of type equations for the respective type parameters.

For example, given

```go
// dedup returns a copy of the argument slice with any duplicate entries removed.
func dedup[S ~[]E, E comparable](S) S { … }

type Slice []int
var s Slice
s = dedup(s) // same as s = dedup[Slice, int](s)
```

the variable `s` of type `Slice` must be assignable to the function parameter type `S` for the program to be valid. To reduce complexity, type inference ignores the directionality of assignments, so the type relationship between `Slice` and `S` can be expressed via the (symmetric) type equation `Slice ≡A S` (or `S ≡A Slice` for that matter), where the `A` in `≡A` indicates that the left and right types must match per assignability rules. Similarly, the type parameter `S` must satisfy its constraint `~[]E`. This can be expressed as `S ≡C ~[]E`, where `X ≡C Y` stands for "`X` satisfies constraint `Y`". These observations lead to a set of two equations

```
Slice ≡A S     (1)
S     ≡C ~[]E  (2)
```

which can be solved for the type parameters `S` and `E`. From (1) a compiler can infer that the type argument for `S` is `Slice`. Similarly, because the underlying type of `Slice` is `[]int` and `[]int` must match `[]E` of the constraint, a compiler can infer that `E` must be `int`. Thus, for these two equations, type inference infers

```
S ➞ Slice
E ➞ int
```

Given a set of type equations, the type parameters to solve for are the type parameters of the functions that need to be instantiated and for which no explicit type argument is provided. These type parameters are called bound type parameters. For instance, in the `dedup` example, the type parameters `S` and `E` are bound to `dedup`. An argument to a generic function call may be a generic function itself. The type parameters of that function are included in the set of bound type parameters. The types of function arguments may contain type parameters from other functions (such as a generic function enclosing a function call). Those type parameters may also appear in type equations, but they are not bound in that context. Type equations are always solved for the bound type parameters only.

Type inference supports calls of generic functions and any use of a generic function in a context where the function must be assignable to a (non-generic) function type. The latter includes assigning a generic function to a variable (including passing it as an argument to another function), converting a generic function to a function type, returning it as a result, and others.

Type inference operates on a set of equations specific to each of these cases. The equations are as follows (type argument lists are omitted for clarity):

- In a function call `f(a0, a1, …)` where `f` or a function argument `ai` is a generic function: each pair `(ai, pi)` of corresponding function arguments and parameters of `f`, where `ai` is not an untyped constant, yields an equation `typeof(pi) ≡A typeof(ai)`. If `ai` is an untyped constant `cj`, and `typeof(pi)` is a bound type parameter `Pk`, the pair `(cj, Pk)` is collected separately from the type equations. Parameters that the call omits because they have default values (section 10.13, Default parameter values) yield no equation.
- In a context where a generic function `f` must be assignable to a (non-generic) function type `T`: `typeof(f) ≡A T`.

Additionally, each type parameter `Pk` and corresponding type constraint `Ck` yields the type equation `Pk ≡C Ck`.

Type inference gives precedence to type information obtained from typed operands before considering untyped constants. Therefore, inference proceeds in two phases:

1. The type equations are solved for the bound type parameters using type unification. If unification fails, type inference fails.
2. For each bound type parameter `Pk` for which no type argument has been inferred yet and for which one or more pairs `(cj, Pk)` with that same type parameter were collected, determine the constant kind of the constants `cj` in all those pairs the same way as for constant expressions (section 11.25, Constant expressions). The type argument for `Pk` is the default type for the determined constant kind. If a constant kind cannot be determined due to conflicting constant kinds, type inference fails.

If not all type arguments have been found after these two phases, type inference fails.

If the two phases are successful, type inference determined a type argument for each bound type parameter, `Pk ➞ Ak`. A type argument `Ak` may be a composite type, containing other bound type parameters `Pk` as element types (or even be just another bound type parameter). In a process of repeated simplification, the bound type parameters in each type argument are substituted with the respective type arguments for those type parameters until each type argument is free of bound type parameters. If type arguments contain cyclic references to themselves through bound type parameters, simplification and thus type inference fails. Otherwise, type inference succeeds.

**Type unification.** Type inference solves type equations through type unification. Type unification recursively compares the left and right types of an equation, where either or both types may be or contain bound type parameters, and looks for type arguments for those type parameters such that the left and right types match (become identical or assignment-compatible, depending on context). To that effect, type inference maintains a map of bound type parameters to inferred type arguments. This map is consulted and updated during type unification. Initially, the bound type parameters are known but the map is empty. During type unification, if a new type argument `A` is inferred, the respective mapping `P ➞ A` from type parameter to argument is added to the map. Conversely, when comparing types, a known type argument (a type argument for which a map entry already exists) takes the place of its corresponding type parameter. As type inference progresses, the map is populated more and more until all equations have been considered, or until unification fails. Type inference succeeds if no unification step fails and the map has an entry for each type parameter.

For example, given the type equation with the bound type parameter `P`

```
[10]struct{ elem P, list []P } ≡A [10]struct{ elem string; list []string }
```

type inference starts with an empty map. Unification first compares the top-level structure of the left and right types. Both are arrays of the same length, so they unify if the element types unify. Both element types are structs. They unify if they have the same number of fields with the same names and if the field types unify. The type argument for `P` is not known yet (there is no map entry), so unifying `P` with `string` adds the mapping `P ➞ string` to the map. Unifying the types of the `list` field requires unifying `[]P` and `[]string` and thus `P` and `string`. Since the type argument for `P` is known at this point, its type argument `string` takes the place of `P`. And since `string` is identical to `string`, this unification step succeeds as well. Unification of the left and right sides of the equation is now finished. Type inference succeeds because there is only one type equation, no unification step failed, and the map is fully populated.

Unification uses a combination of **exact** and **loose** unification, depending on whether two types have to be identical, assignment-compatible, or only structurally equal. As unification recursively descends a composite type structure, the matching mode used for elements of the type, the element matching mode, remains the same as the matching mode, except when two types are unified for assignability (`≡A`). In this case, the matching mode is loose at the top level but then changes to exact for element types, reflecting the fact that types do not have to be identical to be assignable.

For an equation of the form `P ≡C C`, where `P` is a type parameter and `C` its corresponding constraint, the unification rules are these:

- If all types in `C`'s type set have the same underlying type `U`, and `P` has a known type argument `A`, `U` and `A` must unify loosely.
- Similarly, if all types in `C`'s type set are channel types with the same element type and non-conflicting channel directions, and `P` has a known type argument `A`, the most restrictive channel type in `C`'s type set and `A` must unify loosely.
- If `P` does not have a known type argument and `C` contains exactly one type term `T` that is not an underlying (tilde) type, unification adds the mapping `P ➞ T` to the map.
- If `C` does not have a type `U` as described above and `P` has a known type argument `A`, `A` must have all methods of `C`, if any, and corresponding method types must unify exactly.

When solving type equations from type constraints, solving one equation may infer additional type arguments, which in turn may enable solving other equations that depend on those type arguments. Type inference repeats type unification as long as new type arguments are inferred.

The rules for when two types unify follow.

Two types that are not bound type parameters **unify exactly** if any of the following conditions is true:

- Both types are identical (section 8.3, Type identity).
- Both types have identical structure and their element types unify exactly.
- Exactly one type is an unbound type parameter, and all the types in its type set unify with the other type per the unification rules for `≡A` (loose unification at the top level and exact unification for element types).

If both types are bound type parameters, they unify per the given matching modes if:

- Both type parameters are identical.
- At most one of the type parameters has a known type argument. In this case, the type parameters are joined: they both stand for the same type argument. If neither type parameter has a known type argument yet, a future type argument inferred for one of the type parameters is simultaneously inferred for both of them.
- Both type parameters have a known type argument and the type arguments unify per the given matching modes.

A single bound type parameter `P` and another type `T` unify per the given matching modes if:

- `P` does not have a known type argument. In this case, `T` is inferred as the type argument for `P`.
- `P` does have a known type argument `A`, `A` and `T` unify per the given matching modes, and one of the following conditions is true:
  - Both `A` and `T` are interface types. In this case, if both `A` and `T` are also defined types, they must be identical. Otherwise, if neither of them is a defined type, they must have the same number of methods (unification of `A` and `T` already established that the methods match).
  - Neither `A` nor `T` is an interface type. In this case, if `T` is a defined type, `T` replaces `A` as the inferred type argument for `P`.

Finally, two types that are not bound type parameters **unify loosely** (and per the element matching mode) if:

- Both types unify exactly.
- One type is a defined type, the other type is a type literal but not an interface, and their underlying types unify per the element matching mode.
- Both types are interfaces (but not type parameters) with identical type terms, both or neither embed the predeclared type `comparable`, corresponding method types unify exactly, and the method set of one of the interfaces is a subset of the method set of the other interface.
- Only one type is an interface (but not a type parameter), corresponding methods of the two types unify per the element matching mode, and the method set of the interface is a subset of the method set of the other type.
- Both types have the same structure and their element types unify per the element matching mode.

### 11.18 Operators

Operators combine operands into expressions.

```
Expression = UnaryExpr | Expression binary_op Expression .
UnaryExpr  = PrimaryExpr | unary_op UnaryExpr .

binary_op  = "||" | "&&" | rel_op | add_op | mul_op .
rel_op     = "==" | "!=" | "<" | "<=" | ">" | ">=" .
add_op     = "+" | "-" | "|" | "^" .
mul_op     = "*" | "/" | "%" | "<<" | ">>" | "&" | "&^" .

unary_op   = "+" | "-" | "!" | "^" | "*" | "&" | "<-" .
```

Comparisons are discussed in section 11.20. For other binary operators, the operand types must be identical unless the operation involves shifts or untyped constants. For operations involving constants only, see section 11.25.

Except for shift operations, if one operand is an untyped constant and the other operand is not, the constant is implicitly converted to the type of the other operand.

The right operand in a shift expression must have integer type or be an untyped constant representable by a value of type `uint`. If the left operand of a non-constant shift expression is an untyped constant, it is first implicitly converted to the type it would assume if the shift expression were replaced by its left operand alone.

```go
var a [1024]byte
var s uint = 33

var i = 1 << s          // 1 has type int
var j int32 = 1 << s    // 1 has type int32; j == 0
var k = uint64(1 << s)  // 1 has type uint64; k == 1<<33
var m int = 1.0 << s    // 1.0 has type int; m == 1<<33
var n = 1.0<<s == j     // 1.0 has type int32; n == true
var o = 1<<s == 2<<s    // 1 and 2 have type int; o == false
var p = 1<<s == 1<<33   // 1 has type int; p == true
var u = 1.0 << s        // illegal: 1.0 has type float64, cannot shift
var u1 = 1.0<<s != 0    // illegal: 1.0 has type float64, cannot shift
var u2 = 1<<s != 1.0    // illegal: 1 has type float64, cannot shift
var v1 float32 = 1 << s // illegal: 1 has type float32, cannot shift
var v2 = string(1 << s) // illegal: 1 is converted to a string, cannot shift
var w int64 = 1.0 << 33 // 1.0<<33 is a constant shift expression; w == 1<<33
var x = a[1.0<<s]       // panics: 1.0 has type int, but 1<<33 overflows array bounds
```

The results are given for 64-bit `int`, which is the width of `int` on every target (section 7.2, Numeric types).

**Owners.** Operands of an owner type may be compared (section 11.20, Comparison operators), and no other binary or unary operator applies to them, whatever the underlying type, except that a variable of one may be addressed (section 11.22, Address operators): `f + 1` for `type Fd owner int32` is an error, since its result would be a new owner made by arithmetic. The arithmetic is written on the value read out by a conversion, which moves nothing (section 11.24, Conversions):

```go
type Fd owner int32

func next(f *Fd) int32 {
	// return *f + 1      // error: + does not apply to an owner
	return int32(*f) + 1 // reads the number; f still holds its owner
}
```

**Operator precedence.** Unary operators have the highest precedence. As the `++` and `--` operators form statements, not expressions (section 12.6, IncDec statements), they fall outside the operator hierarchy. As a consequence, the statement `*p++` is the same as `(*p)++`.

There are five precedence levels for binary operators. Multiplication operators bind strongest, followed by addition operators, comparison operators, `&&` (logical AND), and finally `||` (logical OR):

```
Precedence    Operator
    5             *  /  %  <<  >>  &  &^
    4             +  -  |  ^
    3             ==  !=  <  <=  >  >=
    2             &&
    1             ||
```

Binary operators of the same precedence associate from left to right. For instance, `x / y * z` is the same as `(x / y) * z`.

```go
+x                         // x
42 + a - b                 // (42 + a) - b
23 + 3*x[i]                // 23 + (3 * x[i])
x <= f()                   // x <= f()
^a >> b                    // (^a) >> b
f() || g()                 // f() || g()
x == y+1 && <-chanInt > 0  // (x == (y+1)) && ((<-chanInt) > 0)
```

### 11.19 Arithmetic operators

Arithmetic operators apply to numeric values and yield a result of the same type as the first operand. The four standard arithmetic operators (`+`, `-`, `*`, `/`) apply to integer and floating-point types. `+` also applies to strings. The bitwise logical and shift operators apply to integers only.

```
+    sum                    integers, floats, strings
-    difference             integers, floats
*    product                integers, floats
/    quotient               integers, floats
%    remainder              integers

&    bitwise AND            integers
|    bitwise OR             integers
^    bitwise XOR            integers
&^   bit clear (AND NOT)    integers

<<   left shift             integer << integer >= 0
>>   right shift            integer >> integer >= 0
```

There are no complex types (section 7.2, Numeric types), so no arithmetic operator applies to one. The arithmetic, bitwise and shift operators do not apply to enum values (E2316). An enum value may be a shift count or an index. Convert an enum to an integer type first (section 11.24, Conversions).

If the operand type is a type parameter, the operator must apply to each type in that type set. The operands are represented as values of the type argument that the type parameter is instantiated with, and the operation is computed with the precision of that type argument. For example, given the function

```go
func dotProduct[F ~float32 | ~float64](v1, v2 []F) F {
	var s F
	for i, x := range v1 {
		y := v2[i]
		s += x * y
	}
	return s
}
```

the product `x * y` and the addition `s += x * y` are computed with `float32` or `float64` precision, respectively, depending on the type argument for `F`.

**Integer operators.** For two integer values `x` and `y`, the integer quotient `q = x / y` and remainder `r = x % y` satisfy the following relationships:

```
x = q*y + r  and  |r| < |y|
```

with `x / y` truncated towards zero ("truncated division").

```
 x     y     x / y     x % y
 5     3       1         2
-5     3      -1        -2
 5    -3      -1         2
-5    -3       1        -2
```

The one exception to this rule is that if the dividend `x` is the most negative value for the `int` type of `x`, the quotient `q = x / -1` is equal to `x` (and `r = 0`) due to two's-complement integer overflow:

```
                         x, q
int8                     -128
int16                  -32768
int32             -2147483648
int64    -9223372036854775808
```

If the divisor is a constant, it must not be zero. If the divisor is zero at run time, a run-time panic occurs (section 17.1, Panics). If the dividend is non-negative and the divisor is a constant power of 2, the division may be replaced by a right shift, and computing the remainder may be replaced by a bitwise AND operation:

```
 x     x / 4     x % 4     x >> 2     x & 3
 11      2         3         2          3
-11     -2        -3        -3          1
```

The shift operators shift the left operand by the shift count specified by the right operand, which must be non-negative. If the shift count is negative at run time, a run-time panic occurs. The shift operators implement arithmetic shifts if the left operand is a signed integer and logical shifts if it is an unsigned integer. There is no upper limit on the shift count. Shifts behave as if the left operand is shifted `n` times by 1 for a shift count of `n`. A shift by at least the width of the left operand gives 0, or -1 for a negative signed value shifted right. As a result, `x << 1` is the same as `x*2`, and `x >> 1` is the same as `x/2` but truncated towards negative infinity.

For integer operands, the unary operators `+`, `-` and `^` are defined as follows:

```
+x                          is 0 + x
-x    negation              is 0 - x
^x    bitwise complement    is m ^ x  with m = "all bits set to 1" for unsigned x
                                      and  m = -1 for signed x
```

**Integer overflow.** For unsigned integer values, the operations `+`, `-`, `*` and `<<` are computed modulo 2<sup>n</sup>, where n is the bit width of the unsigned integer's type. Loosely speaking, these unsigned integer operations discard high bits upon overflow, and programs may rely on "wrap around".

For signed integers, the operations `+`, `-`, `*`, `/` and `<<` may legally overflow, and the resulting value exists and is deterministically defined by the signed integer representation, the operation and its operands. Overflow does not cause a run-time panic. An implementation may not optimize code under the assumption that overflow does not occur. For instance, it may not assume that `x < x + 1` is always true.

**Floating-point operators.** For floating-point numbers, `+x` is the same as `x`, while `-x` is the negation of `x`. The result of a floating-point division by zero follows IEEE 754 and does not panic.

An implementation may combine multiple floating-point operations into a single fused operation, possibly across statements, and produce a result that differs from the value obtained by executing and rounding the instructions individually. An explicit floating-point type conversion rounds to the precision of the target type, preventing fusion that would discard that rounding.

For instance, some architectures provide a "fused multiply and add" (FMA) instruction that computes `x*y + z` without rounding the intermediate result `x*y`. These examples show when an implementation can use that instruction:

```go
// FMA allowed for computing r, because x*y is not explicitly rounded:
r = x*y + z
r = z; r += x*y
t = x*y; r = t + z
*p = x*y; r = *p + z
r = x*y + float64(z)

// FMA disallowed for computing r, because it would omit rounding of x*y:
r = float64(x*y) + z
r = z; r += float64(x*y)
t = float64(x*y); r = t + z
```

**String concatenation.** Strings can be concatenated using the `+` operator or the `+=` assignment operator:

```go
s := "hi" + string(c)
s += " and good bye"
```

String addition creates a new string by concatenating the operands. If both operands are constants, the result is a constant in static storage (section 11.25, Constant expressions). Otherwise the new string is a counted string (section 18.8, Strings) whose bytes are allocated from the current allocator (section 18.9, Allocators). It is freed when its last copy goes. The result of concatenating valid UTF-8 is valid UTF-8. In kernel mode the allocation follows the rules of any allocation (section 20.11, Allocation in kernel mode). With no allocator set, the operation panics (section 18.9, Allocators).

### 11.20 Comparison operators

Comparison operators compare two operands and yield an untyped boolean value.

```
==    equal
!=    not equal
<     less
<=    less or equal
>     greater
>=    greater or equal
```

In any comparison, the first operand must be assignable to the type of the second operand, or vice versa.

The equality operators `==` and `!=` apply to operands of comparable types, and the ordering operators `<`, `<=`, `>` and `>=` to operands of ordered types. Which types are comparable and ordered, how values of each are compared, and when a comparison panics are defined in section 8.10 (Comparable types). Comparing operands of an owner type reads both in place and moves neither (section 18.5, Moves). A slice, map or function value may be compared only to the predeclared identifier `nil`.

```go
const c = 3 < 4 // c is the untyped boolean constant true

type MyBool bool
var x, y int
var (
	// The result of a comparison is an untyped boolean.
	// The usual assignment rules apply.
	b3        = x == y // b3 has type bool
	b4 bool   = x == y // b4 has type bool
	b5 MyBool = x == y // b5 has type MyBool
)
```

Strict comparability, which the constraint `comparable` uses, is defined in section 8.10.

### 11.21 Logical operators

Logical operators apply to boolean values and yield a result of the same type as the operands. The left operand is evaluated, and then the right if the condition requires it.

```
&&    conditional AND    p && q  is  "if p then q else false"
||    conditional OR     p || q  is  "if p then true else q"
!     NOT                !p      is  "not p"
```

### 11.22 Address operators

For an operand `x` of type `T`, the address operation `&x` generates a pointer of type `*T` to `x`. The operand must be **addressable**, that is, either a variable, pointer indirection, or slice indexing operation; or a field selector of an addressable struct operand; or an array indexing operation of an addressable array. The object of a box (`*p` for a `p` of type `owned[T]`, `shared[T]` or `synced[T]`) is a variable. As an exception to the addressability requirement, `x` may also be a (possibly parenthesized) composite literal. If the evaluation of `x` would cause a run-time panic, then the evaluation of `&x` does too.

These are not addressable: a map element (E2454 for a container element), the bytes of a string, a constant, a call result, and a payload selected from a union. A temporary of owner type is the receiver of a pointer-receiver method (section 11.14, Calls) and is not otherwise addressable.

For an operand `x` of pointer type `*T`, the pointer indirection `*x` denotes the variable of type `T` pointed to by `x`. If `x` is `nil`, an attempt to evaluate `*x` causes a run-time panic (section 17.5, Faults).

```go
&x
&a[f(2)]
&Point{2, 3}
*p
*pf(x)

var x *int = nil
*x  // causes a run-time panic
&*x // causes a run-time panic
```

**`&x` is a view of `x` where it lives.** A pointer is a view (section 18.3, Views). Where a variable lives is decided by its declaration: a local variable or parameter lives in its function's frame, a package variable in static storage, a field in its struct, and an element in its array (section 18.2, Where variables live). Taking an address never moves a variable to the heap, and `return &x` does not copy `x` to the caller. For a local variable it is an error (E3001), because the view would outlive the frame. The fixes are to return `x` itself, which moves or copies it to the caller, or to allocate it with `new` (section 13.9, Allocation). A pointer to a variable declared in a loop is valid for one iteration (section 18.4, Loop iterations reuse their storage). A pointer to a volatile or aligned variable keeps the qualifier: `&u.data` has type `*volatile[uint8]` (section 7.15, `volatile[T]`).

**Owners.** `*p` of a pointer to an owner is a place, so it cannot be moved out of (E3303). Assigning to it, `*p = v`, drops the old value and moves `v` in (section 12.7, Assignment statements). The conversion `(*T)(p)` and the expression `&*p` give a `*T` view of the object of a box (section 7.19, `owned`, `shared`, `synced` and `weak`).

### 11.23 Receive operator

For an operand `ch` of channel type, the value of the receive operation `<-ch` is the value received from the channel `ch`. The channel direction must permit receive operations, and the type of the receive operation is the element type of the channel. The expression blocks until a value is available. Receiving from a nil channel blocks forever. A receive operation on a closed channel can always proceed immediately, yielding the element type's zero value after any previously sent values have been received.

```go
v1 := <-ch
v2 = <-ch
f(<-ch)
<-strobe // wait until clock pulse and discard received value
```

If the operand type is a type parameter, all types in its type set must be channel types that permit receive operations, and they must all have the same element type, which is the type of the receive operation.

A receive expression used in an assignment statement or initialization of the special form

```go
x, ok = <-ch
x, ok := <-ch
var x, ok = <-ch
var x, ok T = <-ch
```

yields an additional untyped boolean result reporting whether the communication succeeded. The value of `ok` is `true` if the value received was delivered by a successful send operation to the channel, or `false` if it is a zero value generated because the channel is closed and empty.

**Owners.** Receiving an owner moves it to the receiver: after `ch <- b` in one task, the receiver of `v := <-ch` owns the value (section 12.5, Send statements; section 19.2, The sendable rule). A receive whose value is not stored is a temporary, and the owner is dropped at the end of the statement. A zero value that a closed channel yields for an owner element type holds nothing.

A receive may wait. It is not allowed in code that must not wait, such as an interrupt handler (section 20.10, Interrupts). A receive in a `select` with a `default` case never waits (section 12.13, Select statements).

### 11.24 Conversions

A conversion changes the type of an expression to the type specified by the conversion. A conversion may appear literally in the source, or it may be implied by the context in which an expression appears.

An explicit conversion is an expression of the form `T(x)` where `T` is a type and `x` is an expression that can be converted to type `T`.

```
Conversion = Type "(" Expression [ "," ] ")" .
```

If the type starts with the operator `*` or `<-`, or if the type starts with the keyword `func` and has no result list, it must be parenthesized when necessary to avoid ambiguity:

```go
*Point(p)       // same as *(Point(p))
(*Point)(p)     // p is converted to *Point
<-chan int(c)   // same as <-(chan int(c))
(<-chan int)(c) // c is converted to <-chan int
func()(x)       // function signature func() x
(func())(x)     // x is converted to func()
(func() int)(x) // x is converted to func() int
func() int(x)   // x is converted to func() int (unambiguous)
```

**Constant conversions.** A constant value `x` can be converted to type `T` if `x` is representable by a value of `T` (section 8.5, Representability). Converting a constant to a type that is not a type parameter yields a typed constant.

```go
uint(iota)              // iota value of type uint
float32(2.718281828)    // 2.718281828 of type float32
float32(0.49999999)     // 0.5 of type float32
float64(-1e-1000)       // 0.0 of type float64
string('x')             // "x" of type string
string(0x266c)          // "♬" of type string
myString("foo" + "bar") // "foobar" of type myString
(*int)(nil)             // not a constant: nil is not a constant, *int is not a boolean, numeric, or string type
int(1.2)                // illegal: 1.2 cannot be represented as an int
string(65.0)            // illegal: 65.0 is not an integer constant
```

Converting a constant to a type parameter yields a non-constant value of that type, with the value represented as a value of the type argument that the type parameter is instantiated with. For example, given the function

```go
func f[P ~float32 | ~float64]() {
	… P(1.1) …
}
```

the conversion `P(1.1)` results in a non-constant value of type `P`, and the value 1.1 is represented as a `float32` or a `float64` depending on the type argument for `f`. Accordingly, if `f` is instantiated with a `float32` type, the numeric value of the expression `P(1.1) + 1.2` is computed with the same precision as the corresponding non-constant `float32` addition.

**Non-constant conversions.** A non-constant value `x` can be converted to type `T` in any of these cases:

- `x` is assignable to `T` (section 8.4, Assignability).
- Ignoring struct tags (below), `x`'s type and `T` are not type parameters but have identical underlying types (section 8.2, Underlying types).
- Ignoring struct tags, `x`'s type and `T` are pointer types that are not named types, and their pointer base types are not type parameters but have identical underlying types.
- `x`'s type and `T` are both integer or floating-point types.
- `x` is a value of type `rune`, or a slice of runes, and `T` is a string type (below).
- `x` is a slice, `T` is an array or a pointer to an array, and the slice and array types have identical element types (below).
- `x` is an integer or an enum value, and `T` is an enum type (below).
- `x` is an enum value, and `T` is an integer or floating-point type.
- `x` and `T` are related by the rules for packed types (section 7.14, `packed` types) or union types (section 7.13, Unions).
- `x` is a value, and `T` is an owner type, or `x` is an owner and `T` is not (below).

Additionally, if `T` or `x`'s type `V` are type parameters, `x` can also be converted to type `T` if one of the following conditions applies:

- Both `V` and `T` are type parameters and a value of each type in `V`'s type set can be converted to each type in `T`'s type set.
- Only `V` is a type parameter and a value of each type in `V`'s type set can be converted to `T`.
- Only `T` is a type parameter and `x` can be converted to each type in `T`'s type set.

Struct tags are ignored when comparing struct types for identity for the purpose of conversion:

```go
type Person struct {
	Name    string
	Address *struct {
		Street string
		City   string
	}
}

var data *struct {
	Name    string `json:"name"`
	Address *struct {
		Street string `json:"street"`
		City   string `json:"city"`
	} `json:"address"`
}

var person = (*Person)(data) // ignoring tags, the underlying types are identical
```

Specific rules apply to (non-constant) conversions between numeric types or to and from a string type. These conversions may change the representation of `x` and incur a run-time cost. All other conversions only change the type but not the representation of `x`.

There is no linguistic mechanism to convert between pointers and integers. The package `unsafe` implements this functionality under restricted circumstances (section 20.1, Package `unsafe`).

**Conversions between numeric types.** For the conversion of non-constant numeric values, these rules apply:

1. When converting between integer types, if the value is a signed integer, it is sign extended to implicit infinite precision. Otherwise it is zero extended. It is then truncated to fit in the result type's size. For example, if `v := uint16(0x10F0)`, then `uint32(int8(v)) == 0xFFFFFFF0`. The conversion always yields a valid value. There is no indication of overflow.
2. When converting a floating-point number to an integer, the fraction is discarded (truncation towards zero).
3. When converting an integer or floating-point number to a floating-point type, the result value is rounded to the precision specified by the destination type. For instance, the value of a variable `x` of type `float32` may be stored using additional precision beyond that of an IEEE 754 32-bit number, but `float32(x)` represents the result of rounding `x`'s value to 32-bit precision. Similarly, `x + 0.1` may use more than 32 bits of precision, but `float32(x + 0.1)` does not.
4. In all non-constant conversions involving floating-point values, if the result type cannot represent the value, the conversion succeeds but the result value is implementation-dependent. This includes the conversion of a NaN or an infinity to an integer. The conversion never faults and never panics.

**Conversions to and from a string type.** A conversion to a string type allocates a counted string (section 18.8, Strings) from the current allocator (section 18.9, Allocators), except where it is constant or yields a one-byte ASCII string, which is static. The string is valid UTF-8.

- A value of type `rune`, or an untyped constant, converts to a string that holds the UTF-8 encoding of the code point. Values outside the range of valid Unicode code points, and surrogate halves, are converted to `"�"`. A value of any other integer type is an error. Write `string(rune(i))` to convert the code point `i`, or `strconv.Itoa(i)` to write the number.

```go
string('a')        // "a"
string(65)         // "A": an untyped constant
string('\xf8')     // "ø" == "ø" == "\xc3\xb8"
string(-1)         // "�" == "\xef\xbf\xbd"
string(rune(i))    // for a variable i of type int
string(i)          // error: i is an int, not a rune

type myString string
myString('日') // "日" == "日" == "\xe6\x97\xa5"
```

- A slice of runes converts to a string that is the concatenation of the individual rune values converted to strings.

```go
string([]rune{0x767d, 0x9d6c, 0x7fd4}) // "白鵬翔" == "白鵬翔"
string([]rune{})                       // ""
string([]rune(nil))                    // ""

type runes []rune
string(runes{0x767d, 0x9d6c, 0x7fd4}) // "白鵬翔" == "白鵬翔"

type myRune rune
string([]myRune{0x266b, 0x266c}) // "♫♬" == "♫♬"
myString([]myRune{0x1f30e})      // "\U0001f30e" == "🌎"
```

- A conversion from a slice of bytes to a string, `string(b)`, is an error. A string holds valid UTF-8 (section 7.3, String types), and a slice of bytes may hold any bytes, so the conversion would have to change them. Use `strings.FromUTF8(b)`, which returns a string and an error, when the bytes should be text. Use `strings.FromUTF8Lossy(b)`, which replaces each byte that does not begin a valid UTF-8 sequence with U+FFFD, where replacement is acceptable. For binary data keep the bytes in a `[]byte`, a `vec.Vec[byte]` or an array: a `[32]byte` is a valid map key.
- Conversions of a string to a slice, `[]byte(s)` and `[]rune(s)`, are errors. A slice is a view, and the conversion would have to make an array that nothing owns. The fixes are `strings.Bytes(s)`, which returns a `vec.Vec[byte]`; `copy(buf[:], s)`, which fills a buffer the caller owns without allocating; and `for i, r := range s` or `strings.Runes(s)` for runes (section 12.10, For statements).

**Conversions from slice to array or array pointer.** Converting a slice to an array yields an array containing the elements of the underlying array of the slice. Similarly, converting a slice to an array pointer yields a pointer to the underlying array of the slice, which is a view of it (section 18.3, Views). In both cases, if the length of the slice is less than the length of the array, a run-time panic occurs. A conversion to an array copies the elements, so it is an error when the element type is an owner, which would be duplicated.

```go
var buf [4]byte
s := buf[:2]

a0 := [0]byte(s)
a1 := [1]byte(s[1:]) // a1[0] == s[1]
a2 := [2]byte(s)     // a2[0] == s[0]
a4 := [4]byte(s)     // panics: len([4]byte) > len(s)

s0 := (*[0]byte)(s)     // s0 != nil
s1 := (*[1]byte)(s[1:]) // &s1[0] == &s[1]
s2 := (*[2]byte)(s)     // &s2[0] == &s[0]
s4 := (*[4]byte)(s)     // panics: len([4]byte) > len(s)

var t []string
t0 := [0]string(t)    // ok for nil slice t
t1 := (*[0]string)(t) // t1 == nil
t2 := (*[1]string)(t) // panics: len([1]string) > len(t)
```

**Enums.** `E(c)` for an integer constant `c` is valid if `c` is the value of a member of `E`, and gives that member. For a non-constant integer `n`, `E(n)` is valid only in the comma-ok form `v, ok := E(n)` (or with `=`, or `var v, ok = E(n)`). The value `ok` reports whether `n` equals a member's value, and `v` is that member, or the zero value if `ok` is false (E2314 for the form without `ok`). Floats and other non-integer types do not convert to an enum (E2313). An enum converts to any integer or floating-point type as its underlying integer does. Conversions to and from an enum with the same underlying type go through the integer type. See 7.12.

**Owners.** Three rules decide the conversions that involve an owner type (section 7.18, Owner types).

1. Converting a value to an owner type makes a new owner. It is allowed only in the package that declares the owner type, where the invariants of the type are kept. In any other package it is an error (E3212).
2. Converting an owner to a type that is not an owner reads the value and moves nothing. The result is a plain value with no `Drop`.
3. Converting an owner to another owner type with the same underlying type moves it.

```go
type Frame owner uintptr                          // a physical page frame

func (f *Frame) Drop() { freeFrame(uintptr(*f)) } // rule 2: reads the number
func Alloc() Frame     { return Frame(nextFree()) } // rule 1: makes an owner from a number
```

Converting an owner to an interface type lends it when the interface is ordinary, and moves it when the interface is owning (section 7.9, Interface types). A value-receiver method of an owner type never satisfies an interface method (section 8.6, Method sets), so the conversion is an error when the interface needs one.

**Packed types and unions.** A packed type converts to and from an integer type of its size and from a byte array of its length (section 7.14, `packed` types). A packed union converts to and from its backing type, and a payload converts back to its union (section 7.13, Unions). A union does not convert implicitly from anything; a value of another union type with identical variants converts explicitly.

### 11.25 Constant expressions

Constant expressions may contain only constant operands and are evaluated at compile time. The constant operands are literals, constant identifiers, `iota`, enum members, the results of constant conversions, and calls of the built-in functions that have constant results: `len` and `cap` of an array or of a constant string that contain no channel receives and no function calls, `len(E)` of an enum type `E` (section 7.12, Enums), `min` and `max` of constants, and the functions of package `unsafe` that report sizes and offsets (section 20.1, Package `unsafe`).

Untyped boolean, numeric and string constants may be used as operands wherever it is legal to use an operand of boolean, numeric or string type, respectively.

A constant comparison always yields an untyped boolean constant. If the left operand of a constant shift expression is an untyped constant, the result is an integer constant. Otherwise it is a constant of the same type as the left operand, which must be of integer type.

Any other operation on untyped constants results in an untyped constant of the same kind. That is, a boolean, integer, floating-point or string constant. If the untyped operands of a binary operation (other than a shift) are of different kinds, the result is of the operand's kind that appears later in this list: integer, rune, floating-point.

```go
const a = 2 + 3.0         // a == 5.0   (untyped floating-point constant)
const b = 15 / 4          // b == 3     (untyped integer constant)
const c = 15 / 4.0        // c == 3.75  (untyped floating-point constant)
const Θ float64 = 3 / 2   // Θ == 1.0   (type float64, 3/2 is integer division)
const Π float64 = 3 / 2.  // Π == 1.5   (type float64, 3/2. is float division)
const d = 1 << 3.0        // d == 8     (untyped integer constant)
const e = 1.0 << 3        // e == 8     (untyped integer constant)
const f = int32(1) << 33  // illegal    (constant 8589934592 overflows int32)
const g = float64(2) >> 1 // illegal    (float64(2) is a typed floating-point constant)
const h = "foo" > "bar"   // h == true  (untyped boolean constant)
const j = true            // j == true  (untyped boolean constant)
const k = 'w' + 1         // k == 'x'   (untyped rune constant)
const l = "hi"            // l == "hi"  (untyped string constant)
const m = string(k)       // m == "x"   (type string)
```

Constant expressions are always evaluated exactly. Intermediate values and the constants themselves may require precision significantly larger than supported by any predeclared type in the language. An implementation represents integer constants with at least 256 bits and floating-point constants with a mantissa of at least 256 bits and a signed binary exponent of at least 16 bits (chapter 5). The following are legal declarations:

```go
const Huge = 1 << 100        // Huge == 1267650600228229401496703205376  (untyped integer constant)
const Four int8 = Huge >> 98 // Four == 4                                (type int8)
```

The divisor of a constant division or remainder operation must not be zero:

```go
3.14 / 0.0 // illegal: division by zero
```

The values of typed constants must always be accurately representable by values of the constant type. The following constant expressions are illegal:

```go
uint(-1)    // -1 cannot be represented as a uint
int(3.14)   // 3.14 cannot be represented as an int
int64(Huge) // 1267650600228229401496703205376 cannot be represented as an int64
Four * 300  // operand 300 cannot be represented as an int8 (type of Four)
Four * 100  // product 400 cannot be represented as an int8 (type of Four)
```

The mask used by the unary bitwise complement operator `^` matches the rule for non-constants: the mask is all 1s for unsigned constants and -1 for signed and untyped constants.

```go
^1        // untyped integer constant, equal to -2
uint8(^1) // illegal: same as uint8(-2), -2 cannot be represented as a uint8
^uint8(1) // typed uint8 constant, same as 0xFF ^ uint8(1) = uint8(0xFE)
int8(^1)  // same as int8(-2)
^int8(1)  // same as -1 ^ int8(1) = -2
```

An implementation may use rounding while computing untyped floating-point constant expressions. This rounding may cause a floating-point constant expression to be invalid in an integer context, even if it would be integral when calculated using infinite precision, and vice versa.

### 11.26 Order of evaluation

At package level, initialization dependencies determine the evaluation order of individual initialization expressions in variable declarations (section 15.2, Package initialization). Otherwise, the operands of an expression, assignment or return statement are evaluated in two steps.

1. All function calls, method calls, receive operations and binary logical operations are evaluated first, in lexical left-to-right order. A call happens after the calls in its own operands: the function value and the arguments are evaluated, then the call is made. A binary logical operation (`&&`, `||`) is a sequence point. Its left operand is evaluated completely before the right operand is evaluated, and the right operand is evaluated only if the operator requires it.
2. Then the remaining operands are evaluated in lexical left-to-right order, using the values the calls left behind. These are the reads of variables, field selectors, index expressions, slice expressions, pointer indirections, conversions, and the arithmetic, comparison and other operators.

For example, in the (function-local) assignment

```go
y[f()], ok = g(z || h(), i()+x[j()], <-c), k()
```

the function calls and communication happen in the order `f()`, `h()` (if `z` evaluates to false), `i()`, `j()`, `<-c`, `g()` and `k()`. The evaluation and indexing of `x`, the evaluation of `y` and the evaluation of `z` happen after them, except that `z` is evaluated before `h()` because `||` is a sequence point. A function cannot be called before its arguments are evaluated.

A variable that an expression reads and a call in the same expression changes is therefore read after the change:

```go
a := 1
f := func() int { a++; return a }
x := []int{a, f()}       // x is [2 2]: f() is called first, then a is read
n := a + f()             // a is 3 after the call, so n is 6
m := map[int]int{a: f()} // key and value: f() is called first, so m is {4: 4}
```

The same rule applies to the operands of the left-hand side of an assignment. In `s[i], i = n(), 0`, where `n` increments `i`, the index `i` is read after `n()` has run.

**Other rules.**

- The explicit arguments of a call are evaluated first. The default expressions of omitted parameters are evaluated after them, left to right, and before the call is made (section 11.14, Calls).
- In a `task` or `defer` statement, the function value and the arguments are evaluated when the statement runs, by the same rule (section 12.12, Task statements; section 12.19, Defer statements).
- An operand that panics when it is evaluated (an index out of range, a nil dereference, a division by zero) panics at its place in this order, that is, after the calls of the expression.
- An owner variable that an expression moves is moved when the call or assignment that takes it is performed. A moved variable cannot be used elsewhere in the same expression (section 11.14, Calls), so the time of the move cannot be observed. The temporaries of an expression are dropped at the end of the statement (section 18.6, Drops).
- Floating-point operations within a single expression are evaluated according to the associativity of the operators. Explicit parentheses affect the evaluation by overriding the default associativity. In the expression `x + (y + z)` the addition `y + z` is performed before adding `x`.

At package level, initialization dependencies override the left-to-right rule for individual initialization expressions, but not for operands within each expression:

```go
var a, b, c = f() + v(), g(), sqr(u()) + v()

func f() int        { return c }
func g() int        { return a }
func sqr(x int) int { return x * x }

// functions u and v are independent of all other variables and functions
```

The function calls happen in the order `u()`, `sqr()`, `v()`, `f()`, `v()` and `g()`.

### 11.27 Or clauses

A function that can fail returns an `error` as its last result (section 16.1, The error interface). Its caller tests the result and leaves when it finds an error. An *or clause* is the short form of that test and exit. It follows the call on the same line and says what to do when the call returns an error.

```
Call     = PrimaryExpr Arguments .
OrStmt   = [ ExpressionList "=" | IdentifierList ":=" ] Call "or" OrClause .
OrClause = Block | ReturnStmt | BreakStmt | ContinueStmt | GotoStmt | Expression .
```

An *or statement* is a simple statement (chapter 12, Statements). It may stand alone, or be the init statement of an `if`, `switch` or `for` statement (below). It is defined here, next to the calls it applies to. The word `or` is a contextual keyword (section 4.6, Contextual keywords). It is not an operator. It never appears inside an expression and has no precedence (appendix C).

```go
cfg := load(path) or return                                  // pass the error up unchanged
cfg := load(path) or return fmt.Errorf("loading %v: %w", path, err)
n := strconv.Atoi(line) or continue                          // skip this line
f := os.Open(p) or panic(err)
cfg := load(path) or {
	log.Printf("loading %v: %v", path, err)
	return err
}
```

In each statement the call is made. If its last result is not `nil`, the clause runs, and in the clause `err` is that result. Otherwise the other results are assigned or declared, and the statement is done. In a function with the results `(int, error)`, the first statement does what this does, except that `err` is not visible after the statement:

```go
cfg, err := load(path)
if err != nil {
	return 0, err
}
```

**The operand.** The operand of `or` is a call: a primary expression followed by arguments, written directly before `or`. It may call a function, a method, a method value or a function value (section 11.14, Calls), including a generic function after instantiation. Any other operand is a syntax error (E1013). An operator, a parenthesis or a selector in front of `or` could hide which call's error the clause tests. A conversion and a call of a built-in function have the form of a call and are not calls of functions: they are error E2461. The call, `or` and the first token of the clause are on one line: a newline after the closing parenthesis ends the statement, and a newline after `or` inserts a semicolon after it, as it does after any identifier (section 4.3, Semicolons). Both leave the statement unfinished and are syntax errors. The statements of a block clause may span lines.

**The error result.** The last result of the call must have the type `error`, identical to it. A type that implements `error` is not accepted, nor is a defined type whose underlying type is the `error` interface, nor a type parameter whose constraint has an `Error` method. A call with no result is not accepted either (E2462). The type is exactly `error` so that the test is always the nil test of one interface type and `err` is the result itself, not a conversion of it.

**Values.** The values of an or statement are the results of the call other than the last. With a left side, the number of operands must equal the number of values (E0017 otherwise). Each value is assigned to its operand in order (section 12.7, Assignment statements), or declares its variable (section 10.11, Short variable declarations, redeclaration included), as if the call returned only those values. A call with three results gives two values, so `a, b := f() or return` is the form for it. A call whose only result is the error has no values, and a left side is an error (E0017). Without a left side the values are discarded and dropped as the results of an expression statement are (section 12.4, Expression statements). A call that returns a value which matters together with an error, such as the count of bytes read before a failure, cannot use `or`, because the clause never sees the values. It uses an `if` statement.

**The name `err`.** The clause is in an implicit block (chapter 9, Blocks). In it, `err` is declared as a variable of type `error` that holds the last result. It hides any `err` of an enclosing block. It is not in scope in the call, whose operands are evaluated before the clause runs, and not after the statement. It is an ordinary variable: the clause may assign it, and a block clause may declare an `err` of its own in the block, which hides this one. The compiler does not report an unused `err`. The variables that a short variable declaration declares come into scope after the whole statement, clause included (chapter 10, Declarations and scope, rule 7), so the clause cannot use them. A name in the clause that is the name of a new variable denotes what it denoted before the statement.

```go
func f(path string) error {
	cfg := load(path) or {
		println(cfg.name) // error: cfg is not declared here; the clause runs when load failed
		return err
	}
	return nil
}
```

**Order of evaluation.** An or statement is evaluated in four steps (section 11.26, Order of evaluation).

1. The calls of the statement are evaluated first, from left to right, as section 11.26 says. The call that `or` follows is evaluated completely: the function value, the arguments, then the call.
2. The last result is compared with `nil`.
3. If it is `nil`, the remaining operands of the left side are evaluated, and the values are assigned or declared, as in the statement without `or`. The clause does not run.
4. If it is not `nil`, no other operand of the left side is evaluated, no variable is declared or assigned, and the old value of an owner operand is not dropped. The temporaries of the statement are dropped as at the end of a statement: the results of the call other than the last, and the temporaries of its operands (section 18.6, Drops). Then `err` is declared with the last result, and the clause runs.

An operand of the left side that panics, such as an index out of range, therefore panics only when the call succeeded. A variable that the call moved (section 11.14, Calls) is moved in the clause too.

**The clause must leave.** When the clause runs, control must not reach the code after the or statement. A clause that can complete is a compile error (E2463). A clause is accepted when it is a `return`, `break`, `continue` or `goto` statement, a call of the built-in function `panic`, or a block whose statement list ends in a statement that *leaves*. A statement leaves when it is:

1. a terminating statement (section 12.1, Terminating statements);
2. a `break` or `continue` statement;
3. a block whose statement list ends in a statement that leaves; or
4. an `if` statement with an `else` branch in which both branches leave.

A `break` or `continue` is not a terminating statement, because terminating statements decide whether a function may end without a `return`. A clause may end in one because it too never reaches the next statement. As section 12.1 says, a call of a function that never returns does not leave, whatever can be inferred about that function. A clause that calls `os.Exit(1)` or `log.Fatal(err)` ends with a `return` or `panic` after it.

An or statement is not a terminating statement, although its clause leaves, because control continues after it when the call succeeds.

**Return in an unbraced clause.** An unbraced `return` clause has two short forms. Both make the `error` result of the function out of the clause.

| The clause | When | It returns |
|---|---|---|
| `return` | the last result of the function is `error` | `err` as the last result. Every other result is its zero value if the results are unnamed, and keeps its current value if they are named. |
| `return E` | the function has more than one result, the last is `error`, and `E` is one single-valued expression | the same, with the value of `E` as the last result |

A `return` of any other form in an unbraced clause is an ordinary return statement (section 12.14, Return statements): `or return 0, err` in a function with the results `(int, error)`, and `or return E` in a function with one result, of any type. A `return` with no operands in a function whose last result is not exactly `error`, or that has no result, is error E2464. There is no error result to carry `err`, and returning without it would drop the error. A `return` inside a block clause is never a short form. It is an ordinary return statement with the meaning it has everywhere, so `or { return }` in a function with unnamed results is the usual error for missing results.

The short form sets the last result by its position, whatever its name, so it works when the function has a named result `err` that the clause's `err` hides. Named results keep their values because they are variables of the function. The function may have set them for the caller, such as a count of items done before the failure, and a deferred call may read them. Replacing them with zeros would discard what an ordinary `return` in such a function never discards.

```go
func parseAll(fields []string) (parsed int, err error) {
	for _, f := range fields {
		strconv.Atoi(f) or return // parsed keeps the count so far, and err is set
		parsed++
	}
	return
}
```

The error is handed over unchanged: the same interface value, with the same dynamic type, and no copy (section 16.7, Allocated errors and ownership). `return E` evaluates `E` with `err` in scope, so `or return fmt.Errorf("loading %v: %w", path, err)` wraps it.

**Leaving the statement.** The clause is an ordinary exit from the blocks that enclose it. Leaving it drops the owners and counted values of the scopes it leaves, innermost first, starting with `err` unless the clause moved it out (section 18.6, Drops). A `sync.Scope` of a block it leaves is joined (section 19.4, Scoped tasks). A `return` sets the results and then runs the deferred calls, as every `return` does (section 12.14; section 12.19, Defer statements). In the body of a range-over-func loop, `break`, `continue` and `return` in the clause act as they do anywhere in that body (section 12.11, Range over functions). The rules for `break`, `continue` and `goto` (sections 12.15 to 12.17) apply at the place of the clause, where the variables that the statement declares are not in scope yet, so `goto` over the declaration of one of them is an error.

**Where an or statement may appear.** An or statement may be a statement of a block, and the init statement of an `if`, `switch` or `for` statement. It may not be the post statement of a `for` statement (E2465), since a `break` or `continue` there would end or repeat the iteration that is ending. It is not a `var` declaration, a `select` case, a `defer` statement or a `task` statement, and in those places `or` is an ordinary identifier, so its position is a syntax error (E1001). In an init statement the clause is outside the statement that the header belongs to. A `break` or `continue` in it refers to the innermost `for`, `switch` or `select` statement that contains the whole statement, and a label must name one of those. The variables that the init statement declares are in scope in the rest of the statement, not in the clause.

```go
if v := strconv.Atoi(s) or return; v > 0 { // v is in scope in the if and the else
	use(v)
}
for _, line := range lines {
	switch n := strconv.Atoi(line) or continue; { // continues the for statement
	case n > 0:
		use(n)
	}
}
```

**Other uses.**

- **Panics and `rt.Goexit`.** `or panic(err)` panics as every call of `panic` does (section 17.1, Panics). A clause that calls `rt.Goexit` ends the task as it does anywhere else (section 17.3, Ending a task with `rt.Goexit`). An or statement adds no other way to end a task.
- **Generic functions.** The type of the last result is the type after instantiation. An or statement may be in a generic function, and may call one.
- **Kernel mode.** An or statement makes no call and no allocation of its own. It is a comparison with `nil`, the assignments of the values, and the code of the clause. It is valid in freestanding and kernel builds. In code that an interrupt handler or the panic handler can reach, the code of the clause must be valid there, since the rules about code that waits or starts tasks apply to it (section 20.10, Interrupts).
- **Counted errors.** `err` holds a reference to the error object when the error is counted (section 16.7), and it is released when the clause is left, unless the clause hands it over.

**Errors.** The or statement has these errors. Each is reported at the `or` or at the part it names.

| Code | The program |
|---|---|
| E1013 | has an operand of `or` that is not a call |
| E2461 | uses `or` on a conversion or a call of a built-in function |
| E2462 | uses `or` on a call whose last result is not exactly `error`, or that has no result |
| E2463 | has a clause that can complete |
| E2464 | has an unbraced `return` with no operands in a function whose last result is not `error` |
| E2465 | uses an or statement as the post statement of a `for` statement |

```go
func f() (int, error)

func g() int

func uses(s string) {
	a := g() + g() or return          // error: the operand of or is not a single call (E1013)
	b := (f()) or return              // error: the call is in parentheses (E1013)
	c := int64(g()) or return         // error: a conversion is not a call of a function (E2461)
	d := strconv.Itoa(5) or return    // error: Itoa returns no error (E2462)
	e := f() or println(err)          // error: the clause can complete (E2463)
	n, err := strconv.Atoi(s) or return // error: Atoi has one value besides the error (E0017)
	for i := 0; i < 3; i = f() or break { // error: not a post statement (E2465)
	}
}

func main() {
	f() or return // error: main has no error result to carry err (E2464)
}
```

A complete program:

```go
package main

import (
	"pluto/errors"
	"pluto/fmt"
	"pluto/strconv"
	"pluto/strings"
)

var errEmpty = errors.New("no numbers")

// sum adds the comma-separated numbers in text.
func sum(text string) (int, error) {
	total, count := 0, 0
	for field := range strings.SplitSeq(text, ",") {
		n := strconv.Atoi(strings.TrimSpace(field)) or return fmt.Errorf("number %d: %w", count+1, err)
		total += n
		count++
	}
	if count == 0 {
		return 0, errEmpty
	}
	return total, nil
}

// skipBad adds the numbers of text that parse and counts the rest.
func skipBad(text string) (total, bad int) {
	for field := range strings.SplitSeq(text, ",") {
		n := strconv.Atoi(strings.TrimSpace(field)) or {
			bad++
			continue
		}
		total += n
	}
	return
}

func main() int64 {
	total := sum("1, 2, 3") or panic(err)
	fmt.Println("total", total)
	good, bad := skipBad("1, x, 3, y")
	fmt.Println("skipBad", good, bad) // skipBad 4 2
	sum("4, five") or {
		fmt.Println("error:", err)
		return 1
	}
	return 0
}
```

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
