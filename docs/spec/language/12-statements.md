## 12. Statements

Statements control execution.

```
Statement  = Declaration | LabeledStmt | SimpleStmt |
             TaskStmt | ReturnStmt | BreakStmt | ContinueStmt | GotoStmt |
             FallthroughStmt | Block | IfStmt | SwitchStmt | SelectStmt | ForStmt |
             DeferStmt .

SimpleStmt = EmptyStmt | ExpressionStmt | SendStmt | IncDecStmt | Assignment | ShortVarDecl | OrStmt .
```

The or statement is a call followed by a clause that runs when the call returns an error. It is defined with the calls in section 11.27 (Or clauses).

The statement that starts a task is the `task` statement (section 12.12, Task statements). The reserved word `go` starts no statement: a statement that begins with it is an error (E1012) that names `task` (section 4.5, Keywords).

Two rules of the language run through all statements. Each statement that makes a temporary of an owner or counted type drops it at the end of the statement, unless the statement says otherwise (section 18.6, Drops). And each block drops the owners and counted values of its variables when control leaves the block, on every path out of it, in reverse order of declaration (section 18.6, Drops).

### 12.1 Terminating statements

A terminating statement interrupts the regular flow of control in a block. The following statements are terminating:

1. A `return` or `goto` statement.
2. A call to the built-in function `panic`.
3. A block in which the statement list ends in a terminating statement.
4. An `if` statement in which the `else` branch is present, and both branches are terminating statements.
5. A `for` statement in which there are no `break` statements referring to the `for` statement, the loop condition is absent, and the `for` statement does not use a range clause.
6. A `switch` statement with a `default` case in which there are no `break` statements referring to the `switch` statement, and the statement lists in each case, including the default, end in a terminating statement, or a possibly labeled `fallthrough` statement. This holds for expression switches and for type switches.
7. An expression `switch` on a value of an enum type, with no `default` case, in which there are no `break` statements referring to the `switch` statement, every member of the enum is listed among the case values, and the statement list in each case ends in a terminating statement or a possibly labeled `fallthrough` statement. A value that is not a member makes the switch panic (section 12.9, Switch statements), so control never runs past it.
8. A type `switch` on a value of a union type, with no `default` case, in which there are no `break` statements referring to the `switch` statement, every variant of the union is named by a case, and the statement list in each case ends in a terminating statement. A tag that names no variant, which only unsafe code can make, makes the switch panic (section 12.9).
9. A `select` statement in which there are no `break` statements referring to the `select` statement, and the statement lists in each case, including the default if present, end in a terminating statement.
10. A labeled statement labeling a terminating statement.

All other statements are not terminating. A statement list ends in a terminating statement if the list is not empty and its final non-empty statement is terminating.

The clause of an or statement must leave (section 11.27, The clause must leave). The or statement itself is not a terminating statement: control continues after it when the call returns no error.

A call of a function that never returns is not a terminating statement, whatever can be inferred about that function. The terminating-statement rule is syntactic: it looks at the statement and never at the body of a function that is called, so `os.Exit(1)` at the end of a function that has results needs a `return` or a `panic` after it. (Other rules do use the bodies of callees: sections 11.14, 12.12 and 17.4.)

```go
func name(c Color) string {
	switch c { // every member listed, no default: terminating
	case Red:
		return "red"
	case Green:
		return "green"
	case Blue:
		return "blue"
	}
}

func fail() { os.Exit(1) }

func f() int {
	fail()
	panic("unreachable") // required: a call of fail does not terminate f
}
```

### 12.2 Empty statements

The empty statement does nothing.

```
EmptyStmt = .
```

### 12.3 Labeled statements

A labeled statement may be the target of a `goto`, `break` or `continue` statement. The scope of a label is the body of the function that declares it (section 10.1, Label scopes).

```
LabeledStmt = Label ":" Statement .
Label       = identifier .
```

```go
goto Error
...
Error: log.Panic("error encountered")
```

### 12.4 Expression statements

With the exception of specific built-in functions, function and method calls and receive operations can appear in statement context. Such statements may be parenthesized.

```
ExpressionStmt = Expression .
```

The following built-in functions are not permitted in statement context:

```
cap len make max min new take
unsafe.Add unsafe.Alignof unsafe.Offsetof unsafe.Sizeof unsafe.Slice unsafe.SliceData unsafe.String unsafe.StringData
```

```go
h(x + y)
f.Close()
<-ch
(<-ch)
len("foo") // illegal if len is the built-in function
```

**Temporaries.** A value that an expression statement produces and nothing stores is a temporary. An owner is dropped at the end of the statement, so a call that returns an owner and is used as a statement drops the result at once. A file returned by `os.Open` and ignored is closed. Strings and other counted values are released. A view of a temporary owner may be passed to calls in the same statement and may not outlive it (section 18.3, Views; section 18.6, Drops).

```go
os.Stdout.Write(readAll(f).Slice()) // the vector readAll returns is dropped after Write returns
makeBuffer()                         // the result is dropped at the end of the statement
```

The built-in functions `clear`, `close`, `copy`, `delete`, `panic`, `print` and `println` are permitted in statement context (chapter 13). `clear(s)` on a slice assigns the zero value to each element, and assigning to an element that holds an owner drops the old value first (section 12.7, Assignment statements). `clear(m)` on a map deletes every entry and releases the counted keys and values (section 13.3, Clear).

### 12.5 Send statements

A send statement sends a value on a channel. The channel expression must be of channel type, the channel direction must permit send operations, and the type of the value to be sent must be assignable to the channel's element type.

```
SendStmt = Channel "<-" Expression .
Channel  = Expression .
```

Both the channel and the value expression are evaluated before communication begins (section 11.26, Order of evaluation). Communication blocks until the send can proceed. A send on an unbuffered channel can proceed if a receiver is ready. A send on a buffered channel can proceed if there is room in the buffer. A send on a closed channel proceeds by causing a run-time panic (section 17.1, Panics). A send on a nil channel blocks forever.

```go
ch <- 3 // send value 3 to channel ch
```

If the type of the channel expression is a type parameter, all types in its type set must be channel types that permit send operations, they must all have the same element type, and the type of the value to be sent must be assignable to that element type.

**Ownership.** The element type of a channel must be sendable (section 19.2, The sendable rule). Sending an owner moves it. An owner that is a whole variable is moved into the channel when the send proceeds, and the variable holds nothing afterward. The receiver of the value owns it (section 11.23, Receive operator). A value that is not a whole variable, such as a call result, is a temporary that the channel takes over. A string or other counted value is copied, and the channel holds a reference of its own.

```go
func produce(out chan<- vec.Vec[int]) {
	for i := 0; i < 3; i++ {
		v := vec.New[int]()
		v.Push(i)
		out <- v // moves v to whoever receives it
	}
	close(out)
}
```

A send may wait. It is not allowed in code that must not wait, such as an interrupt handler (section 20.10, Interrupts). A send in a `select` statement with a `default` case never waits (section 12.13, Select statements).

### 12.6 IncDec statements

The `++` and `--` statements increment or decrement their operands by the untyped constant 1. As with an assignment, the operand must be addressable, a map index expression, or a container index expression (section 11.11, Indexing library containers).

```
IncDecStmt = Expression ( "++" | "--" ) .
```

The following assignment statements are semantically equivalent:

```
IncDec statement    Assignment
x++                 x += 1
x--                 x -= 1
```

For a container index expression, `x[k]++` calls `x.Index(k)` and then `x.SetIndex(k, result)`, evaluating `x` and `k` once (section 11.11, Indexing library containers). The operand cannot be an enum value (E2316).

### 12.7 Assignment statements

An assignment replaces the current value stored in a variable with a new value specified by an expression. An assignment statement may assign a single value to a single variable, or multiple values to a matching number of variables.

```
Assignment = ExpressionList assign_op ExpressionList .

assign_op  = [ add_op | mul_op ] "=" .
```

Each left-hand side operand must be addressable, a map index expression, a container index expression (section 11.11, Indexing library containers), or (for `=` assignments only) the blank identifier. Operands may be parenthesized.

```go
x = 1
*p = f()
a[i] = 23
(k) = <-ch // same as: k = <-ch
```

An assignment operation `x op= y`, where `op` is a binary arithmetic operator, is equivalent to `x = x op (y)` but evaluates `x` only once. The `op=` construct is a single token. In assignment operations, both the left- and right-hand expression lists must contain exactly one single-valued expression, and the left-hand expression must not be the blank identifier.

```go
a[i] <<= 2
i &^= 1 << n
s += "x" // string concatenation (11.19)
```

A tuple assignment assigns the individual elements of a multi-valued operation to a list of variables. There are two forms. In the first, the right-hand operand is a single multi-valued expression such as a function call, a channel or map operation, or a type assertion. The number of operands on the left-hand side must match the number of values. For instance, if `f` is a function returning two values,

```go
x, y = f()
```

assigns the first value to `x` and the second to `y`. In the second form, the number of operands on the left must equal the number of expressions on the right, each of which must be single-valued, and the n-th expression on the right is assigned to the n-th operand on the left:

```go
one, two, three = '一', '二', '三'
```

The blank identifier provides a way to ignore right-hand side values in an assignment:

```go
_ = x       // evaluate x but ignore it
x, _ = f()  // evaluate f() but ignore second result value
```

**The blank identifier and owners.** Assigning to the blank identifier is a read that moves nothing. After `_ = f`, where `f` is an owner variable, `f` still holds its value and is still dropped at the end of its scope. It does not close a file. To end the life of an owner early, call `f.Drop()` (section 18.6, Drops). If the right-hand side is a temporary, as in `_ = open()`, the temporary is dropped at the end of the statement, as any unstored temporary is (section 12.4, Expression statements).

**Order.** The assignment proceeds in two phases. First, the operands of index expressions and pointer indirections (including implicit pointer indirections in selectors) on the left and the expressions on the right are all evaluated in the order of section 11.26. Second, the assignments are carried out in left-to-right order.

```go
a, b = b, a // exchange a and b

x := []int{1, 2, 3}
i := 0
i, x[i] = 1, 2 // set i = 1, x[0] = 2

i = 0
x[i], i = 2, 1 // set x[0] = 2, i = 1

x[0], x[0] = 1, 2 // set x[0] = 1, then x[0] = 2 (so x[0] == 2 at end)

x[1], x[3] = 4, 5 // set x[1] = 4, then panic setting x[3] = 5.

type Point struct{ x, y int }
var p *Point
x[2], p.x = 6, 7 // set x[2] = 6, then panic setting p.x = 7

i = 2
x = []int{3, 5, 7}
for i, x[i] = range x { // set i, x[2] = 0, x[0]
	break
}
// after this loop, i == 0 and x is []int{3, 5, 3}
```

**Assignability.** In assignments, each value must be assignable to the type of the operand to which it is assigned (section 8.4, Assignability), with the following special cases:

1. Any typed value may be assigned to the blank identifier.
2. If an untyped constant is assigned to a variable of interface type or the blank identifier, the constant is first implicitly converted to its default type.
3. If an untyped boolean value is assigned to a variable of interface type or the blank identifier, it is first implicitly converted to type `bool`.

**Copies and references.** When a value is assigned to a variable, only the data that is stored in the variable is replaced. If the value contains a view or a counted reference, the assignment copies the reference but does not make a copy of the referenced data (such as the underlying array of a slice).

```go
var s1 = []int{1, 2, 3}
var s2 = s1         // s2 stores the slice descriptor of s1
s1 = s1[:1]         // s1's length is 1 but it still shares its underlying array with s2
s2[0] = 42          // setting s2[0] changes s1[0] as well
fmt.Println(s1, s2) // prints [42] [42 2 3]

var m1 = make(map[string]int)
var m2 = m1            // m2 stores the reference to the map of m1
m1["foo"] = 42         // setting m1["foo"] changes m2["foo"] as well
fmt.Println(m2["foo"]) // prints 42
```

**Moves.** An owner that is a whole variable on the right-hand side is moved to the place on the left-hand side (section 18.5, Moves). The variable holds nothing afterward and cannot be used until it is assigned again. An owner on the right-hand side that is not a whole variable is an error (E3303) unless it is a temporary. To move an owner out of a field, an element or a pointer target, use `take(&place)` (section 13.10, Moving out of a place).

**Drops.** A place that holds an owner drops its old value before the new value is moved in, then holds the new value. This holds for variables, fields, elements and targets of pointers: `c.buf = b` frees the buffer that `c.buf` held, and `*p = v` and `s[i] = v` drop the old contents. For a container, `x[k] = v` moves `v` into the container, and `SetIndex` drops the value it replaces (section 11.11, Indexing library containers). A variable that was moved and is assigned again holds nothing, so there is no old value to drop.

For a string or another counted value, the new value takes its reference first, and then the old value is released, so that `s = strings.SliceFrom(s, 1)` and `a, b = b, a` are safe.

`x = f(x)` moves `x` into the call and assigns the result back. The assignment to `x` is not a use of the old value (section 11.14, Calls).

**Permutations.** A tuple assignment may permute owners. In `a, b = b, a` and `s[i], s[j] = s[j], s[i]`, the owner operands on the right are the places on the left, each once, written as the same expressions in a different order. Such an assignment reads all the right-hand places, then writes the left-hand places without dropping the old values, because each old value appears again on the left. Nothing is lost or duplicated. Any other assignment that moves an owner out of a place that is not a whole variable is an error.

**Views.** A view is copied like any value. Storing a view of a frame where it can outlive the frame (a package variable, memory reached through a pointer that the function received, memory that may be on the heap) is an error (E3002), and so is keeping a view of a loop variable past the iteration (E3006) (section 18.3, Views; section 18.4, Loop iterations reuse their storage).

### 12.8 If statements

`if` statements specify the conditional execution of two branches according to the value of a boolean expression. If the expression evaluates to true, the `if` branch is executed. Otherwise, if present, the `else` branch is executed.

```
IfStmt = "if" [ SimpleStmt ";" ] Expression Block [ "else" ( IfStmt | Block ) ] .
```

```go
if x > max {
	x = max
}
```

The expression may be preceded by a simple statement, which executes before the expression is evaluated.

```go
if x := f(); x < y {
	return x
} else if x > z {
	return z
} else {
	return y
}
```

The variables that the simple statement declares are in scope in both branches, and are dropped when the `if` statement ends (section 18.6, Drops). A temporary in the header, such as an owner that a call returns, lives until the whole `if` statement ends, `else` branches included.

### 12.9 Switch statements

`switch` statements provide multi-way execution. An expression or type is compared to the cases inside the `switch` to determine which branch to execute.

```
SwitchStmt = ExprSwitchStmt | TypeSwitchStmt .
```

There are two forms: expression switches and type switches. In an expression switch, the cases contain expressions that are compared against the value of the switch expression. In a type switch, the cases contain types that are compared against the type of a specially annotated switch expression. The switch expression is evaluated exactly once in a `switch` statement. Temporaries in the header live until the whole statement ends.

**Expression switches.** In an expression switch, the switch expression is evaluated and the case expressions, which need not be constants, are evaluated left-to-right and top-to-bottom. The first one that equals the switch expression triggers execution of the statements of the associated case. The other cases are skipped. If no case matches and there is a `default` case, its statements are executed. There can be at most one default case, and it may appear anywhere in the `switch` statement. A missing switch expression is equivalent to the boolean value `true`.

```
ExprSwitchStmt = "switch" [ SimpleStmt ";" ] [ Expression ] "{" { ExprCaseClause } "}" .
ExprCaseClause = ExprSwitchCase ":" StatementList .
ExprSwitchCase = "case" ExpressionList | "default" .
```

If the switch expression evaluates to an untyped constant, it is first implicitly converted to its default type. The predeclared untyped value `nil` cannot be used as a switch expression. The switch expression type must be comparable (section 8.10, Comparable types).

If a case expression is untyped, it is first implicitly converted to the type of the switch expression. For each (possibly converted) case expression `x` and the value `t` of the switch expression, `x == t` must be a valid comparison. An untyped constant does not convert to an enum type (section 8.4, Assignability), so `case 1:` in a switch on an enum value is an error (E2315).

In other words, the switch expression is treated as if it were used to declare and initialize a temporary variable `t` without explicit type. It is that value of `t` against which each case expression `x` is tested for equality. If the switch expression has an owner type, it is compared in place and is not moved.

Two case expressions in one switch may not evaluate to the same constant. Duplicate integer, floating-point, string and enum constants are errors.

In a case or default clause, the last non-empty statement may be a (possibly labeled) `fallthrough` statement (section 12.18, Fallthrough statements) to indicate that control should flow from the end of this clause to the first statement of the next clause. Otherwise control flows to the end of the `switch` statement. The switch expression may be preceded by a simple statement, which executes before the expression is evaluated.

```go
switch tag {
default:
	s3()
case 0, 1, 2, 3:
	s1()
case 4, 5, 6, 7:
	s2()
}

switch x := f(); { // missing switch expression means "true"
case x < 0:
	return -x
default:
	return x
}

switch {
case x < y:
	f1()
case x < z:
	f2()
case x == 4:
	f3()
}
```

**Enum switches.** In an expression switch whose switch expression has enum type `E`, a case expression that is a single identifier naming a member of `E` denotes that member, whatever else the name denotes in scope. This bare member name is accepted only there. Elsewhere a member is written `E.M` (section 11.7, Selectors). A switch on an enum value that has no `default` case must list every member of `E` among its constant case values. Otherwise it is an error (E2312) that names the missing members. That way, adding a member to the enum shows every switch that needs to handle it. A switch with a `default` case need not list every member. An exhaustive switch with no `default` case is a terminating statement when its clauses are (section 12.1, Terminating statements).

A value of an enum type that is not a member (it can arise only through unsafe code, a packed value converted from raw bits, or generic code that works on the underlying integer type, section 7.12) matches no member's case. A switch with a `default` case runs the default. An exhaustive switch with no `default` case panics, in every function, so that control never leaves it without running a clause. The same holds for a union switch with no `default` case whose tag names no variant (below).

```go
type Color enum {
	Red
	Green
	Blue
}

func (c Color) Warm() bool {
	switch c {
	case Red, Green: // Red and Green name members of Color here
		return true
	case Blue:
		return false
	}
}
```

**Type switches.** A type switch compares types rather than values. It is otherwise similar to an expression switch. It is marked by a special switch expression that has the form of a type assertion using the keyword `type` rather than an actual type:

```go
switch x.(type) {
// cases
}
```

Cases then match actual types `T` against the dynamic type of the expression `x`. As with type assertions (section 11.13, Type assertions), `x` must be of interface type or of union type, but not a type parameter. Each non-interface type `T` listed in a case must implement the type of `x`. The types listed in the cases of a type switch must all be different.

```
TypeSwitchStmt  = "switch" [ SimpleStmt ";" ] TypeSwitchGuard "{" { TypeCaseClause } "}" .
TypeSwitchGuard = [ identifier ":=" ] PrimaryExpr "." "(" "type" ")" .
TypeCaseClause  = TypeSwitchCase ":" StatementList .
TypeSwitchCase  = "case" TypeList | "default" .
TypeList        = CaseItem { "," CaseItem } .
CaseItem        = Type | VariantName .
```

The `TypeSwitchGuard` may include a short variable declaration. When that form is used, the variable is declared at the end of the `TypeSwitchCase` in the implicit block of each clause. In clauses with a case listing exactly one type, the variable has that type. Otherwise, the variable has the type of the expression in the `TypeSwitchGuard`. The variable counts as used if any clause uses it; it is an error (E0101) if no clause does.

Instead of a type, a case may use the predeclared identifier `nil`. That case is selected when the expression in the `TypeSwitchGuard` is a nil interface value. There may be at most one `nil` case.

Given an expression `x` of type `interface{}`, the following type switch:

```go
switch i := x.(type) {
case nil:
	printString("x is nil") // type of i is type of x (interface{})
case int:
	printInt(i) // type of i is int
case float64:
	printFloat64(i) // type of i is float64
case func(int) float64:
	printFunction(i) // type of i is func(int) float64
case bool, string:
	printString("type is bool or string") // type of i is type of x (interface{})
default:
	printString("don't know the type") // type of i is type of x (interface{})
}
```

could be rewritten:

```go
v := x // x is evaluated exactly once
if v == nil {
	i := v // type of i is type of x (interface{})
	_ = i
	printString("x is nil")
} else if i, isInt := v.(int); isInt {
	printInt(i) // type of i is int
} else if i, isFloat64 := v.(float64); isFloat64 {
	printFloat64(i) // type of i is float64
} else if i, isFunc := v.(func(int) float64); isFunc {
	printFunction(i) // type of i is func(int) float64
} else {
	_, isBool := v.(bool)
	_, isString := v.(string)
	if isBool || isString {
		i := v // type of i is type of x (interface{})
		_ = i
		printString("type is bool or string")
	} else {
		i := v // type of i is type of x (interface{})
		_ = i
		printString("don't know the type")
	}
}
```

A type parameter or a generic type may be used as a type in a case. If upon instantiation that type turns out to duplicate another entry in the switch, the first matching case is chosen.

```go
func f[P any](x any) int {
	switch x.(type) {
	case P:
		return 0
	case string:
		return 1
	case []P:
		return 2
	case []byte:
		return 3
	default:
		return 4
	}
}

var v1 = f[string]("foo")  // v1 == 0
var v2 = f[byte]([]byte{}) // v2 == 2
```

The type switch guard may be preceded by a simple statement, which executes before the guard is evaluated. The `fallthrough` statement is not permitted in a type switch.

**Owners in type switches.** A case that names an owner type is an error when the switch expression has an ordinary interface type (E3308), because an ordinary interface only lends the value it holds (section 11.13, Type assertions). When the switch expression is a whole variable of an owning interface type and the guard declares a variable, `switch v := x.(type)` moves the value out of `x` into `v` in every clause except one whose single case names an ordinary interface type. The default clause and a clause with several cases move too. The variable `v` is dropped at the end of its clause unless the clause moves it on, and `x` holds nothing afterward on that path. In a clause whose single case names an ordinary interface type, nothing moves: `v` lends from `x`, as an assertion to an ordinary interface does, and `x` keeps the value. The path where no case matches, in a switch without a default clause, moves nothing either. After the switch, `x` holds nothing if every path moved it, holds its value if none did, and otherwise counts as possibly moved (E3302; section 18.5, Moves). Without a declared variable nothing moves.

**Switching on a place.** A switch with a declared variable whose expression is an owning interface or an owner union held in a field, an element or `*p`, rather than in a whole variable or a temporary, would move out of a place that still holds the value. That is error E3303 (section 18.5, Places), as for a moving assertion (section 11.13). Take the value out first and put back what the clauses keep, which is the usual shape of a driver's state machine:

```go
func (d *Dev) step() {
	switch s := take(&d.state).(type) { // d.state holds the zero value meanwhile
	case State.Busy:
		d.state = State.Busy(s) // put the payload back
	default:
		d.state = s // in the default clause s has the union type
	}
}
```

A switch without a declared variable reads the place and moves nothing.

```go
type Resource owner interface{ Read(p []byte) (int, error) }

func f(r Resource) {
	switch v := r.(type) {
	case io.Writer: // an ordinary interface: v lends from r
		v.Write(nil)
	case owned[File]: // an owner type: r moves into v, and v is dropped at the end of the clause
		v.Sync()
	}
	use(r) // error E3302: the owned[File] clause moved r
}
```

**Union switches.** A type switch on a union value `u` of type `U` selects the variant that `u` holds. Each case names variants: `U.V` (or `pkg.U.V`, `U[T].V`), or the bare name `V`, which in a case of such a switch denotes the variant whatever else the name denotes in scope. Bare names are accepted nowhere else. In a clause naming one variant, the declared variable has that variant's payload type (`struct{}` for a variant without a payload). In a clause naming several variants, and in the default clause, it has type `U`. A union value is never nil, so there is no `nil` case. Naming a variant twice is an error, and `fallthrough` is not allowed. A union switch with no `default` case must name every variant. Otherwise it is an error (E2414) that lists the missing ones. A union value whose tag names no variant can be made only by unsafe code; an exhaustive switch with no `default` case panics on it. A packed union's tag may hold any value its bits allow, so a switch on a packed union with no `default` case needs variants whose tag values together cover every value the tag field can hold; otherwise the switch is error E2415, reported at the switch (section 7.13, Unions).

```go
switch s := st.(type) {
case Off:
	start()
case Probing:
	retry(s.attempt) // s is the payload, a struct{ attempt int }
case Ready:
	enableIRQ(s.irq)
case Failed:
	log("device failed:", s) // s is a syscall.Errno
} // a missing case is an error
```

For an owner union, `switch v := s.(type)` moves the payload into the binding of the clause that runs. It is dropped at the end of the clause unless the clause moves it on, and the union variable is empty afterward (section 7.13, Unions).

### 12.10 For statements

A `for` statement specifies repeated execution of a block. There are three forms: the iteration may be controlled by a single condition, a `for` clause, or a `range` clause.

```
ForStmt   = "for" [ Condition | ForClause | RangeClause ] Block .
Condition = Expression .
```

**For statements with a single condition.** In its simplest form, a `for` statement specifies the repeated execution of a block as long as a boolean condition evaluates to true. The condition is evaluated before each iteration. If the condition is absent, it is equivalent to the boolean value `true`.

```go
for a < b {
	a *= 2
}
```

**For statements with a `for` clause.** A `for` statement with a `ForClause` is also controlled by its condition, but additionally it may specify an init and a post statement, such as an assignment, an increment or decrement statement. The init statement may be a short variable declaration, but the post statement must not. The init statement may be an or statement (section 11.27, Or clauses); the post statement must not (E2465).

```
ForClause = [ InitStmt ] ";" [ Condition ] ";" [ PostStmt ] .
InitStmt  = SimpleStmt .
PostStmt  = SimpleStmt .
```

```go
for i := 0; i < 10; i++ {
	f(i)
}
```

If non-empty, the init statement is executed once before evaluating the condition for the first iteration. The post statement is executed after each execution of the block (and only if the block was executed). Any element of the `ForClause` may be empty, but the semicolons are required unless there is only a condition. If the condition is absent, it is equivalent to the boolean value `true`.

```go
for cond { S() }    // is the same as    for ; cond ; { S() }
for      { S() }    // is the same as    for true     { S() }
```

A temporary in the condition or in the post statement is dropped after each evaluation of it (section 18.6, Drops).

**Iteration variables.** The variables that a `for` clause or a `range` clause declares, and the variables and temporaries that the body declares, are stored in the same memory in every iteration (section 18.4, Loop iterations reuse their storage). The variable of the first iteration is declared by the init statement. A program that compiles behaves as if each iteration had variables of its own: the variable of each subsequent iteration is initialized, before the post statement, to the value of the previous iteration's variable at that moment. The two descriptions agree because a view of an iteration's variable (a closure that uses it, `&x`, a slice of a local array) may not be kept where the next iteration can see it (E3006). Within the iteration, views behave exactly as they would with separate variables. Owners declared in the body are dropped at the end of each iteration (section 18.6, Drops). A variable of owner type declared in the `for` clause is not copied from one iteration to the next: its value moves to the next iteration's variable, and it is dropped once, when the loop ends, on every path out of it.

```go
var fs [3]func() int
for i := 0; i < 3; i++ {
	println(func() int { return i }()) // allowed: used within the iteration
	fs[i] = func() int { return i }    // error: the next iteration reuses i
}
```

**For statements with a `range` clause.** A `for` statement with a `range` clause iterates through all entries of an array, slice, string or map, values received on a channel, integer values from zero to an upper limit, the members of an enum type, the values passed to an iterator function's `yield` function (section 12.11, Range over functions), or the entries of a library container. For each entry it assigns iteration values to corresponding iteration variables if present and then executes the block.

```
RangeClause = [ ExpressionList "=" | IdentifierList ":=" ] "range" Expression .
```

The expression on the right in the `range` clause is called the range expression. It may be an array, pointer to an array, slice, string, map, channel permitting receive operations, an integer, an enum type, a function with specific signature (section 12.11, Range over functions), or a value of a library container type (below). As with an assignment, if present the operands on the left must be addressable, map index expressions or container index expressions (section 11.11, Indexing library containers). They denote the iteration variables. If the range expression is a function, the maximum number of iteration variables depends on the function signature. If the range expression is a channel, an integer or an enum type, at most one iteration variable is permitted. Otherwise there may be up to two. If the last iteration variable is the blank identifier, the range clause is equivalent to the same clause without that identifier.

The range expression `x` is evaluated before beginning the loop, with one exception: if at most one iteration variable is present and `x` or `len(x)` is constant, the range expression is not evaluated. The loop ranges over the value of that one evaluation, held in a hidden temporary. When the value is counted (a string, a map, a channel, or a value that holds one), the temporary holds a count of its own until the loop ends, so a body that assigns to the variable named in the range expression (`for k := range m { m = nil }`, `for i, r := range s { s = "" }`) does not end the iteration or free what the loop reads: the loop continues over the original value, as in Go. This costs one count and one release per loop. Function calls on the left are evaluated once per iteration. For each iteration, iteration values are produced as follows if the respective iteration variables are present:

```
Range expression                                       1st value                2nd value

array or slice      a  [n]E, *[n]E, or []E             index    i  int          a[i]       E
string              s  string type                     index    i  int          see below  rune
map                 m  map[K]V                         key      k  K            m[k]       V
channel             c  chan E, <-chan E                element  e  E
integer value       n  integer type, or untyped int    value    i  see below
enum type           E  an enum type                    member   v  E
function, 0 values  f  func(yield func() bool)
function, 1 value   f  func(yield func(V) bool)        value    v  V
function, 2 values  f  func(yield func(K, V) bool)     key      k  K            v          V
container           x  a type with an All method       as the result of All (below)
```

**Arrays and slices.** For an array, pointer to array or slice value `a`, the index iteration values are produced in increasing order, starting at element index 0. If at most one iteration variable is present, the range loop produces iteration values from 0 up to `len(a)-1` and does not index into the array or slice itself. For a nil slice, the number of iterations is 0. The length is read once, when the loop begins. If the value variable is present and `a` is an array, the values come from the array as it was when the loop began, so assignments to `a` in the body are not seen. A slice and a pointer to an array are not copied: the loop sees changes to the elements.

A range with a value variable over an array or slice of owners is an error (E3304), since each iteration would copy an element. Range over the indexes and use `&s[i]`:

```go
for i := range files {
	files[i].Sync() // a method with a pointer receiver, on the element
}
```

**Strings.** For a string value, the `range` clause iterates over the Unicode code points in the string starting at byte index 0. On successive iterations, the index value is the index of the first byte of successive UTF-8-encoded code points in the string, and the second value, of type `rune`, is the value of the corresponding code point. A string holds valid UTF-8 (section 7.3, String types), so every iteration decodes a whole rune. A string made with `unsafe.String` may hold bytes that are not valid UTF-8. If the iteration encounters an invalid UTF-8 sequence there, the second value is `0xFFFD`, the Unicode replacement character, and the next iteration advances a single byte in the string.

```go
for i, r := range "añb" { // 0 'a', 1 'ñ', 3 'b'
	println(i, r)
}
```

**Maps.** For a map, the key and value are produced as copies, and counted values are counted. The iteration order over maps is not specified (section 7.10, Map types, says what the implementation does and how each map's hash seed is chosen). These guarantees hold for every map, whatever the body does to it:

- Every entry that is present when the loop begins and is not removed before the loop reaches it is produced exactly once.
- An entry that is removed before the loop reaches it is not produced. Removing the entry that was just produced, or any other entry, is allowed and never causes another entry to be skipped or produced twice.
- An entry that is created during the iteration may be produced or may be skipped. The choice may vary for each entry created and from one iteration to the next.
- If the map is nil, the number of iterations is 0.

```go
for k := range m {
	if expired(k) {
		delete(m, k) // allowed: every other entry is still visited once
	}
}
```

**Channels.** For channels, the iteration values produced are the successive values sent on the channel until the channel is closed. If the channel is nil, the range expression blocks forever. A value that is an owner moves into the iteration variable and is dropped at the end of the iteration unless the body moves it on. The loop waits for each value, so it is not allowed in code that must not wait (section 20.10, Interrupts).

**Integers.** For an integer value `n`, where `n` is of integer type or an untyped integer constant, the iteration values 0 through `n-1` are produced in increasing order. If `n` is of integer type, the iteration values have that same type. Otherwise, the type of `n` is determined as if it were assigned to the iteration variable. Specifically: if the iteration variable is preexisting, the type of the iteration values is the type of the iteration variable, which must be of integer type. Otherwise, if the iteration variable is declared by the `range` clause or is absent, the type of the iteration values is the default type for `n`. If `n <= 0`, the loop does not run any iterations.

**Enums.** For an enum type `E` as the range expression, `for v := range E` produces each member of `E` in declaration order, as a value of type `E`. At most one iteration variable is permitted. Ranging over a value of an enum type, instead of the type, is an error (E2317) (section 7.12, Enums).

```go
for c := range Color { // Red, Green, Blue
	println(c.String())
}
```

**Library containers.** If the type of the range expression `x` is none of the types above, `x` must have an `All` method, and the loop ranges over the iterator that `x.All()` returns (section 12.11, Range over functions). Then `x` is evaluated once and `All` is called once, before the loop. `All` is found in the method set of `x`'s type as `Index` is (section 11.11, Indexing library containers), so a pointer receiver needs an addressable `x` or a pointer (E2459). Its form is `All() F`, with no parameters, type parameters or variadic parameter, where `F` is a function type that `range` accepts, such as `iter.Seq[V]` or `iter.Seq2[K, V]` (E2458). On an owner container `All` must have a pointer receiver, since a value receiver would consume the container (E2458). The iteration variables follow `F`. A struct, or a type with an `Index` method, that has no `All` is an error (E2456).

`vec.Vec[T]`, whose `All` returns `iter.Seq2[int, T]` (`iter.Seq2[int, *T]` when `T` is an owner, so the loop yields views), ranges like a slice. `hash.Map[K, V]` ranges like a map, in the order of its table, and `hash.Set[K]` ranges over its elements. A `ring[T, N]` ranges over its built-in `All` (section 7.17, `ring[T, N]`).

```go
for i, x := range v { ... }   // v.All(): index and element
for k, n := range counts { ... }
for k := range counts { ... } // keys only
```

A range over a `vec.Vec` reads the length of the vector once, when the loop begins, as a range over a slice does. Elements that the body pushes are not visited. Each element is read when the loop reaches it, and the loop stops early if the body shrinks the vector below the next index. A worklist that must see its own pushes is an index loop, `for i := 0; i < v.Len(); i++`.

In generic code, the choice is made from the constraint, as for indexing (section 11.11, Indexing library containers). A type parameter whose type set is made of types that the language ranges over is ranged over by the language, and one constrained only by methods ranges over its `All` method.

**Owners in iterators.** The iterators of the library have the types `iter.Seq[V]` and `iter.Seq2[K, V]`, whose type parameters are not owners. An iterator over owners yields views (`*T`), as a range over a slice of owners needs `&s[i]`. A container hands its owners out with methods in an ordinary loop (`v.Pop()`, `m.Remove(k)`).

**Iteration variables.** The iteration variables may be declared by the `range` clause using a form of short variable declaration (`:=`). In this case their scope is the block of the `for` statement. The variables have the types of their respective iteration values. If the iteration variables are not explicitly declared by the `range` clause, they must be preexisting. In this case, the iteration values are assigned to the respective variables as in an assignment statement (section 12.7, Assignment statements).

If the type of the range expression is a type parameter, all types in its type set must have the same underlying type and the range expression must be valid for that type, or, if the type set contains channel types, it must only contain channel types with identical element types, and all channel types must permit receive operations.

```go
var testdata *struct {
	a *[7]int
}
for i, _ := range testdata.a {
	// testdata.a is never evaluated; len(testdata.a) is constant
	// i ranges from 0 to 6
	f(i)
}

var a [10]string
for i, s := range a {
	// type of i is int
	// type of s is string
	// s == a[i]
	g(i, s)
}

var key string
var val interface{} // element type of m is assignable to val
m := map[string]int{"mon": 0, "tue": 1, "wed": 2, "thu": 3, "fri": 4, "sat": 5, "sun": 6}
for key, val = range m {
	h(key, val)
}
// key == last map key encountered in iteration
// val == m[key]

var ch chan Work = producer()
for w := range ch {
	doWork(w)
}

// empty a channel
for range ch {
}

// call f(0), f(1), ... f(9)
for i := range 10 {
	// type of i is int (default type for untyped constant 10)
	f(i)
}

// invalid: 256 cannot be assigned to uint8
var u uint8
for u = range 256 {
}

// invalid: 1e3 is a floating-point constant
for range 1e3 {
}

// fibo generates the Fibonacci sequence
fibo := func(yield func(x int) bool) {
	f0, f1 := 0, 1
	for yield(f0) {
		f0, f1 = f1, f0+f1
	}
}

// print the Fibonacci numbers below 1000:
for x := range fibo {
	if x >= 1000 {
		break
	}
	fmt.Printf("%d ", x)
}
// output: 0 1 1 2 3 5 8 13 21 34 55 89 144 233 377 610 987

// iteration support for a recursive tree data structure
type Tree[K cmp.Ordered, V any] struct {
	left, right *Tree[K, V]
	key         K
	value       V
}

func (t *Tree[K, V]) walk(yield func(key K, val V) bool) bool {
	return t == nil || t.left.walk(yield) && yield(t.key, t.value) && t.right.walk(yield)
}

func (t *Tree[K, V]) Walk(yield func(key K, val V) bool) {
	t.walk(yield)
}

// walk tree t in-order
var t Tree[string, int]
for k, v := range t.Walk {
	process(k, v)
}

// xor returns the xor-ed bytes of S
func xor[S ~[]byte](s S) byte {
	var r byte
	for _, b := range s {
		r ^= b
	}
	return r
}
```

### 12.11 Range over functions

A `range` clause whose range expression is a function `f` of one of the types

```go
func(yield func() bool)
func(yield func(V) bool)
func(yield func(K, V) bool)
```

iterates over the values that `f` passes to its `yield` function. The parameter of `yield` cannot be variadic. The library names these shapes `iter.Seq[V]` and `iter.Seq2[K, V]` (appendix F).

The iteration proceeds by calling `f` with a new, synthesized `yield` function as its argument. If `yield` is called before `f` returns, the arguments to `yield` become the iteration values for executing the loop body once. After each successive loop iteration, `yield` returns `true` and may be called again to continue the loop. As long as the loop body does not terminate, the `range` clause continues to generate iteration values this way for each `yield` call until `f` returns. If the loop body terminates (such as by a `break` statement), `yield` returns `false` and must not be called again.

The loop body becomes the body of the `yield` function, a closure built in the frame of the enclosing function (section 11.5, Closures and their regions). These rules follow:

- **`yield` may not be kept.** `f` may call `yield` as often as it likes while it runs, but it may not keep it. `yield` is a closure of the enclosing frame, so storing it where it outlives the call of `f` is an escape (section 18.3, Views).
- **Control flow.** `break`, `continue`, `return`, `goto` and labeled `break` and `continue` out of the body work as in an ordinary loop. An ordinary `continue`, including a labeled continue targeting this range loop, completes the
current iteration and returns `true` to request the next. A transfer leaving this range returns
`false`; once `f` returns, execution continues at that transfer's target. A `return` in the body returns from the enclosing function. Owners and counted values that the scopes left by these statements hold are dropped (section 18.6, Drops).
- **Misuse of `yield`.** An iterator that calls `yield` when it may not causes a run-time panic with one of these messages: "range function continued iteration after function for loop body returned false" after `yield` returned false, "range function continued iteration after whole loop exit" after the loop ended, and "range function continued iteration after loop body panic" while a panic in the body is in progress.
- **Panics.** A panic in the body ends the program (section 17.1, Panics), after the deferred calls of the iterator and of the function the loop is in have run (hosted builds, 17.2). No iterator can recover it.
- **`defer` in the body.** A `defer` in the body defers a call of the enclosing function. It runs when that function returns. Each `defer` statement has one slot in the function's frame (section 12.19, Defer statements), and the body of a range-over-func loop runs once per iteration, so the rule of section 12.19 (Defer statements) for a `defer` in a loop applies to it. A function literal deferred there may not capture the body's own variables, the loop variables included, since the body's frame is gone when it runs (E2534). Pass them as arguments.
- **Iterators that return closures.** An iterator method such as `items.All()` builds its closure in a region of the caller's frame (section 11.5, Closures and their regions), so a loop over one does not allocate. The loop over `v.All()` has the meaning of the loop that `All` runs.
- **Owners.** An iterator yields values. An iterator over owners yields views of them (section 12.10, For statements).

```go
func find(items *vec.Vec[*Item], id int) int {
	for _, it := range items.All() {
		if it.id == id {
			it.mu.Lock()
			defer it.mu.Unlock() // runs when find returns
			return it.value
		}
	}
	return -1
}
```

### 12.12 Task statements

A `task` statement starts the execution of a function call as an independent concurrent thread of control, or **task**, within the same address space.

```
TaskStmt = "task" Expression .
```

`task` is a contextual keyword (section 4.6, Contextual keywords). It begins a task statement when it is the first token of a statement and the next token is an identifier or the keyword `func`. In every other position `task` is an ordinary identifier, so `task.Run()` and `task = next` are a method call and an assignment on a variable named `task`, and `rt.Task` is a type. The form `task (f)(x)` is not a task statement. Write `task f(x)`. The word `go` is reserved and starts no statement: `go f()` is a compile error that names `task` (section 4.5, Keywords).

```go
task worker(jobs)
task func(ch chan<- bool) {
	for {
		sleep(10)
		ch <- true
	}
}(c)
```

The expression must be a function or method call. It cannot be parenthesized. A `task` statement cannot start a call of a built-in function (E2591). Wrap it in a function and start that.

The function value and arguments are evaluated as usual in the starting task (section 11.26, Order of evaluation), but unlike with a regular call, program execution does not wait for the invoked function to complete. Instead, the function begins executing independently in a new task. When the function terminates, its task also terminates. If the function has any return values, they are discarded when the function completes, which drops those that hold owners or counted values. When `main` returns, tasks that are still running are not waited for (section 15.4, Program execution).

**Arguments.** Each argument is copied into the new task when the statement runs, as an argument is assigned to a parameter (section 11.14, Calls).

- A value is copied.
- An argument of owner type is moved into the new task. The task drops it when the function returns, unless the function moves it on.
- A `synced` reference is copied, and the count goes up by one. A string gets a reference of its own.
- The array that holds the variadic arguments of the call (section 11.15) is copied into the new task, so
  the slice the function receives is not a view of the starting frame, and each argument crosses as a value.
- Every argument, and the receiver, must be sendable (section 19.2, The sendable rule), or the statement is an error. A `shared[T]` is not sendable.
- A view of the starting frame cannot be passed at all (E3004, or E2011 where the type checker sees it directly; a capturing function literal or method value passed as a func-typed argument is E2590 or E2594, as under Closures below), because the task may outlive the frame (section 18.3, Views). Passing the address of a local `sync.WaitGroup` is this error. Use a package-level variable, or a `sync.Scope` (below).
- A view of other memory crosses only when what it points to cannot change while both tasks may use it (section 19.2, The sendable rule; section 19.3, Data race freedom). Freeing, dropping or moving the object, or replacing or dropping an owner that holds it, is a change (section 19.3, Frees are writes).

**A method of a box.** `task p.Serve()`, where `Serve` has a pointer receiver and `p` is an `owned[T]` or a `synced[T]`, hands the box itself to the new task, so the object lives as long as the task needs it and no view of the starting frame is involved.

- An `owned[T]` moves into the task, as an owner argument does. `p` holds nothing afterward, and using it is a use after a move (section 18.5, Moves). The task drops the box when `Serve` returns, or when `Serve` ends it with `rt.ExitTask` (section 17.3, Ending a task with `rt.ExitTask`). The receiver must be a whole variable or a temporary, since an owner moves only out of a whole variable. `task newServer().Serve()` is allowed. `task s.box.Serve()` is not.
- A `synced[T]` is copied as an argument is. The new task holds a reference of its own, taken before it starts and released when the method returns, so the object is dropped by whichever task releases the last one. A field or element may be the receiver.
- A `shared[T]` is not sendable, so `task z.Serve()` is an error (E2010), as is a box whose `T` contains one.
- The method must be one of `T`, not one promoted from a field, and the box type must not be a named type. Any other pointer receiver is a view into an object, and crosses under the rules of section 19.3 (Data race freedom).

**Closures.** A function literal that captures variables cannot be started with a `task` statement (E2590). Its captures live in the starting frame, which may be gone before the task runs (section 11.5, Closures and their regions). Pass the values as arguments: `task func(n int) { work(n) }(n)`. The same holds for a method value `x.M`, whose record holds a copy of the receiver in the frame that made it (E2594). A func value whose closure record may lie in a frame is a view of that frame (section 18.3, Views). E2590 and E2594 report a capturing literal or method value written in the statement, or a local variable whose only assignment is its declaration from one of them, directly or through other such variables. Any other func value (one that reached the statement through a parameter, a field, an element, a channel, or a variable assigned more than once) is checked by section 18.3: it is an error (E3004) where it reaches the task start, and an error at a call that passes a closure of a frame to a function that starts a task with it (E3003). A func value loaded from heap or package memory is never a closure of a frame, since storing one there is an error (E3002). The same rules apply to a call of a function that starts a task with one of its func arguments (`wg.Task`, `time.AfterFunc`, `rt.Go`, or a function of the program that passes a func parameter on to a `task` statement or to one of these). The compiler finds these functions by following each func parameter through the function's body.

**The new task.** Section 19.1 (Tasks) defines the state a new task starts in: its own fixed-size stack, the program-wide default allocator rather than the override that the starting task installed with `alloc.Use`, preemption enabled, and parallel execution with the other tasks. A panic in it follows the rules of section 17.1 (Panics).

**Scoped tasks.** A `task` statement provides no lexical join in its starting function. If `main` returns while the task is
running, the program ends without waiting for it (section 15.4). The tasks that share the starting function's variables are started with the `Task` method of a `sync.Scope`, `s.Task(f)`, which joins them before the scope ends (section 19.4, Scoped tasks). In an interrupt handler or a panic handler a `task` statement is an error (E2585) (section 20.10, Interrupts).

### 12.13 Select statements

A `select` statement chooses which of a set of possible send or receive operations will proceed. It looks similar to a `switch` statement but with the cases all referring to communication operations.

```
SelectStmt = "select" "{" { CommClause } "}" .
CommClause = CommCase ":" StatementList .
CommCase   = "case" ( SendStmt | RecvStmt ) | "default" .
RecvStmt   = [ ExpressionList "=" | IdentifierList ":=" ] RecvExpr .
RecvExpr   = Expression .
```

A case with a `RecvStmt` may assign the result of a `RecvExpr` to one or two variables, which may be declared using a short variable declaration. The `RecvExpr` must be a (possibly parenthesized) receive operation. There can be at most one default case and it may appear anywhere in the list of cases.

Execution of a `select` statement proceeds in several steps:

1. For all the cases in the statement, the channel operands of receive operations and the channel and right-hand-side expressions of send statements are evaluated exactly once, in source order from case to case and each by the rules of section 11.26 (Order of evaluation), upon entering the `select` statement. The result is a set of channels to receive from or send to, and the corresponding values to send. Any side effects in that evaluation occur irrespective of which (if any) communication operation is selected to proceed. Expressions on the left-hand side of a `RecvStmt` with a short variable declaration or assignment are not yet evaluated.
2. If one or more of the communications can proceed, a single one that can proceed is chosen via a uniform pseudo-random selection. Otherwise, if there is a default case, that case is chosen. If there is no default case, the `select` statement blocks until at least one of the communications can proceed.
3. Unless the selected case is the default case, the respective communication operation is executed.
4. If the selected case is a `RecvStmt` with a short variable declaration or an assignment, the left-hand side expressions are evaluated and the received value (or values) are assigned.
5. The statement list of the selected case is executed.

Since communication on nil channels can never proceed, a `select` with only nil channels and no default case blocks forever.

```go
var a []int
var c, c1, c2, c3, c4 chan int
var i1, i2 int
select {
case i1 = <-c1:
	print("received ", i1, " from c1\n")
case c2 <- i2:
	print("sent ", i2, " to c2\n")
case i3, ok := (<-c3): // same as: i3, ok := <-c3
	if ok {
		print("received ", i3, " from c3\n")
	} else {
		print("c3 is closed\n")
	}
case a[f()] = <-c4:
	// same as:
	// case t := <-c4
	//	a[f()] = t
default:
	print("no communication\n")
}

for { // send random sequence of bits to c
	select {
	case c <- 0: // note: no statement, no fallthrough, no folding of cases
	case c <- 1:
	}
}

select {} // block forever
```

**Owners.** The value of a send case is evaluated on entry, as above. An owner in a send case is moved only if that case proceeds. In the other clauses the variable still holds its value, and after the `select` statement it counts as possibly moved, so using it is an error (E3302) until it is assigned again (section 18.5, Moves). A receive case moves the received owner into the variable of the case.

```go
select {
case out <- v: // v is moved only if this case is chosen
	count++
default:
	v.Drop() // v is still usable here
}
```

**Waiting.** A `select` without a `default` case may wait. With a `default` case it never waits, so it may be used where waiting is not allowed (section 20.10, Interrupts). A timeout is a case that receives from a timer's channel, `time.After(d)` or a `time.Timer`'s `C` (section 19.6, Timers):

```go
t := time.NewTimer(100 * time.Millisecond)
select {
case v := <-results:
	use(v)
case <-t.C:
	println("timed out")
}
```

`select {}` with no cases blocks forever. A task blocked in a `select` is shown as waiting in a `select` in a deadlock report (section 19.1, Tasks); the wording of the report is not part of the language.

### 12.14 Return statements

A `return` statement in a function `F` terminates the execution of `F`, and optionally provides one or more result values. Any functions deferred by `F` are executed before `F` returns to its caller.

```
ReturnStmt = "return" [ ExpressionList ] .
```

In a function without a result type, a `return` statement must not specify any result values.

```go
func noResult() {
	return
}
```

There are three ways to return values from a function with a result type:

1. The return value or values may be explicitly listed in the `return` statement. Each expression must be single-valued and assignable to the corresponding element of the function's result type.

```go
func simpleF() int {
	return 2
}

func complexF1() (re float64, im float64) {
	return -7.0, -4.0
}
```

2. The expression list in the `return` statement may be a single call to a multi-valued function. The effect is as if each value returned from that function were assigned to a temporary variable with the type of the respective value, followed by a `return` statement listing these variables, at which point the rules of the previous case apply.

```go
func complexF2() (re float64, im float64) {
	return complexF1()
}
```

3. The expression list may be empty if the function's result type specifies names for its result parameters. The result parameters act as ordinary local variables and the function may assign values to them as necessary. The `return` statement returns the values of these variables.

```go
func complexF3() (re float64, im float64) {
	re = 7.0
	im = 4.0
	return
}

func (devnull) Write(p []byte) (n int, _ error) {
	n = len(p)
	return
}
```

Regardless of how they are declared, all the result values are initialized to the zero values for their type upon entry to the function. A `return` statement that specifies results sets the result parameters before any deferred functions are executed.

An empty expression list in a `return` statement is an error (E0104) if a different entity (constant, type or variable) with the same name as a result parameter is in scope at the place of the return.

```go
func f(n int) (res int, err error) {
	if _, err := f(n-1); err != nil {
		return // invalid return statement: err is shadowed
	}
	return
}
```

**In an or clause.** A `return` that is the whole of an unbraced or clause has two short forms that return the error of the call (section 11.27, Or clauses). In a block clause, `return` is an ordinary return statement.

**Owners.** A result of owner type is handed to the caller. `return x`, where `x` is an owner variable, moves `x` to the caller, and `x` is not dropped. A bare `return` moves the named results. Returning an owner that is not a whole variable is an error (E3303); use `take`. The other variables of the function are dropped when it returns, after the deferred calls have run, parameters after locals (section 18.6, Drops). A result that is a view of the function's frame is an error (E3001), and so is a result that is a view of a local owner, which is dropped on return (section 18.3, Views).

```go
func load() hash.Map[string, string] {
	x := hash.NewMap[string, string](8)
	x.Set("timeout", "30s")
	return x // moves the map to the caller; the table is not copied
}
```

### 12.15 Break statements

A `break` statement terminates execution of the innermost `for`, `switch` or `select` statement within the same function.

```
BreakStmt = "break" [ Label ] .
```

If there is a label, it must be that of an enclosing `for`, `switch` or `select` statement, and that is the one whose execution terminates.

```go
OuterLoop:
	for i = 0; i < n; i++ {
		for j = 0; j < m; j++ {
			switch a[i][j] {
			case nil:
				state = Error
				break OuterLoop
			case item:
				state = Found
				break OuterLoop
			}
		}
	}
```

The owners and counted values held by the scopes that the `break` leaves are dropped first (section 18.6, Drops). In the body of a range-over-func loop, `break` ends the loop by making `yield` return false (section 12.11, Range over functions).

### 12.16 Continue statements

A `continue` statement begins the next iteration of the innermost enclosing `for` loop by advancing control to the end of the loop block. The `for` loop must be within the same function.

```
ContinueStmt = "continue" [ Label ] .
```

If there is a label, it must be that of an enclosing `for` statement, and that is the one whose execution advances.

```go
RowLoop:
	for y, row := range rows {
		for x, data := range row {
			if data == endOfRow {
				continue RowLoop
			}
			row[x] = data + bias(x, y)
		}
	}
```

The owners and counted values held by the scopes that the `continue` leaves, the iteration's own variables included, are dropped first (section 18.6, Drops).

### 12.17 Goto statements

A `goto` statement transfers control to the statement with the corresponding label within the same function.

```
GotoStmt = "goto" Label .
```

```go
goto Error
```

Executing the `goto` statement must not cause any variables to come into scope that were not already in scope at the point of the `goto`. For instance, this example:

```go
	goto L // BAD
	v := 3
L:
```

is erroneous because the jump to label `L` skips the creation of `v`.

A `goto` statement outside a block cannot jump to a label inside that block. For instance, this example:

```go
if n%2 == 1 {
	goto L1
}
for n > 0 {
	f()
	n--
L1:
	f()
	n--
}
```

is erroneous because the label `L1` is inside the `for` statement's block but the `goto` is not.

**Drops.** Leaving a scope by `goto` drops the owners and counted values that the scope holds (section 18.6, Drops). A `goto` back to a label before a variable's declaration drops the variable first, since the declaration runs again.

**A backward `goto` is a loop.** A `goto` to a label that comes before it in the function makes a loop. Every rule that depends on loops applies to it: the variables and temporaries between the label and the `goto` reuse their storage in every pass, so a view of them may not be kept where the next pass can see it (E3006; section 18.4, Loop iterations reuse their storage); a closure that a call in the loop returns may not be kept past the pass (E3101; section 11.5, Closures and their regions); and a `defer` statement in the loop follows the rule for loops (E2530; section 12.19, Defer statements).

```go
i := 0
again:
	defer cleanup() // error: a path reaches this defer again through the goto
	i++
	if i < 3 {
		goto again
	}
```

### 12.18 Fallthrough statements

A `fallthrough` statement transfers control to the first statement of the next case clause in an expression `switch` statement. It may be used only as the final non-empty statement in such a clause, and not in the last clause.

```
FallthroughStmt = "fallthrough" .
```

It is an error in a type switch, in a union switch and in a `select` statement. In an enum switch it works as in any expression switch.

### 12.19 Defer statements

A `defer` statement invokes a function whose execution is deferred to the moment the surrounding function returns, either because the surrounding function executed a return statement, reached the end of its function body, or because the corresponding task is panicking (hosted builds, 17.2) or ends with `rt.ExitTask` (section 17.3, Ending a task with `rt.ExitTask`).

```
DeferStmt = "defer" Expression .
```

The expression must be a function or method call. It cannot be parenthesized. A call of a built-in function may be deferred exactly when it may stand as an expression statement (section 12.4): `clear`, `close`, `copy`, `delete`, `panic`, `print` and `println`. Deferring `cap`, `len`, `make`, `max`, `min`, `new`, `take` or a function of package `unsafe` is an error (E2532). Wrap the call in a function literal and defer that.

Each time a `defer` statement executes, the function value and parameters to the call are evaluated as usual (section 11.26, Order of evaluation) and saved anew, but the actual function is not invoked. A parameter that the call omits because it has a default value is evaluated then too (section 11.14, Calls). Instead, deferred functions are invoked immediately before the surrounding function returns, in the reverse order they were deferred. That is, if the surrounding function returns through an explicit return statement, deferred functions are executed after any result parameters are set by that return statement but before the function returns to its caller. If a deferred function value evaluates to nil, execution panics when the function is invoked, not when the `defer` statement is executed.

For instance, if the deferred function is a function literal and the surrounding function has named result parameters that are in scope within the literal, the deferred function may access and modify the result parameters before they are returned. If the deferred function has any return values, they are discarded when the function completes.

```go
lock(l)
defer unlock(l) // unlocking happens before surrounding function returns

// f returns 42
func f() (result int) {
	defer func() {
		// result is accessed after it was set to 6 by the return statement
		result *= 7
	}()
	return 6
}
```

**One slot per statement.** Each `defer` statement has one slot in the frame of its function, which holds the values it saved. A `defer` statement can therefore run at most once per call of the function. The rule for a loop is the same for every kind of loop: a `for` statement, the body of a range-over-func loop (section 12.11), and a loop made by a backward `goto` (section 12.17). A `defer` statement inside a loop is allowed if every path from it leaves every loop that contains it before the path can reach the `defer` statement again. Otherwise it is an error (E2530). A path leaves the loops by:

- a `return`;
- a call of `panic`, or of a function that never returns (section 20.3, Directives);
- a `goto` to a label outside the outermost loop that contains the `defer`;
- a `break` that ends that outermost loop: an unlabeled `break` when there is one loop, or a labeled `break` naming it or a statement around it;
- a labeled `continue` naming a loop outside it.

An unlabeled `continue`, or a `continue` naming a loop that contains the `defer`, does not leave. The range-over-func body is a frame of its own, which gives one more error: a function literal deferred there may not capture the body's variables (E2534; section 12.11). A function may contain only a bounded number of `defer` statements (E2531; section 21.4, Implementation limits). The following loop is valid, because the path leaves the loop at the `return` before it can reach the `defer` again:

```go
for _, it := range items {
	if it.id == id {
		it.mu.Lock()
		defer it.mu.Unlock()
		return it.value
	}
}
```

The fix for a `defer` that must run at the end of every iteration is to move the body into a function of its own:

```go
for _, name := range names {
	process(name)
}

func process(name string) {
	f := open(name)
	defer f.Close()
	...
}
```

**Panics and `ExitTask`.** In hosted builds, a panic runs the pending deferred calls of the panicking task before the panic handler is called, and `rt.ExitTask` runs the deferred calls of every frame of the task (section 17.2, Deferred calls on a panic; section 17.3, Ending a task with `rt.ExitTask`). There is no `recover`: a deferred call cannot stop a panic. Freestanding and kernel builds run no deferred calls on a panic (section 17.4, Panics in freestanding programs).

**Owners.** Deferred calls run before the function's own variables are dropped, so a deferred call may use them. A deferred call may not use an owner declared in an inner block, which is dropped when that block ends (E3311). The function value, the arguments and the receiver are evaluated at the `defer` statement, so three cases follow from the ordinary rules:

- An owner passed by value moves at the `defer` statement.
- An owner that is the receiver of a method with a value receiver moves there too, since the method consumes it.
- A method with a pointer receiver saves `&x`, and `x` stays usable.

`defer x.Drop()` is the pointer-receiver case (section 7.18, Owner types): `x` stays usable after it, the deferred call drops whatever `x` holds when it runs (nothing, if `x` was moved meanwhile), and `x` is not dropped again at the end of its scope.

```go
f := open(name)
defer f.Drop()   // saves &f; f stays usable
f.Read(buf)      // valid
defer consume(g) // g moves here
g.Len()          // error: E3301, g was moved by the defer statement
```

A function literal that is deferred is a closure of the frame (section 11.5, Closures and their regions), and it may use the captured owners as any literal may (section 11.4, Function literals).

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
