## 18. Ownership and memory

Plutonium has no garbage collector. Memory has a defined lifetime-management responsibility: frame
or static storage, an owner, counted references or an arena. This does not promise eventual reclamation:
strong cycles, nontermination, abrupt exit and a `main` that returns while tasks run can retain resources. This chapter defines how memory is
classified, where variables live, how long a view of memory may be used, how owners move and are
dropped, how shared ownership is counted, where allocations come from, which uses of memory are checked
at run time, and which code the compiler does not check.

The rules are inferred from the program. Ordinary code needs no lifetime, borrow or region annotations and
no knowledge of how the compiler checks them. Owner types are declared (section 7.18, Owner types), and the
compiler infers everything else in this chapter; each rule that rejects a program does so with an error code (chapter 21,
Implementation requirements) whose explanation names the variables and lines involved and shows a fix.
Following section 1.2 (Design rules), every analysis in this chapter rejects what it cannot prove
safe, and whether a program is accepted never depends on optimization (section 21.3, Acceptance does not
depend on optimization).

### 18.1 Values, views and owners

Every type has one of four **kinds**, which decide what assignment, passing, returning and the end of a
variable's scope do to its values. Section 8.7 (Values, views, owners and counted values) gives the kind
of each type; in short:

- A **value** is only bits: booleans, numeric types, enums, packed types,
  and arrays, structs and unions made only of values. Assignment copies it, and nothing happens
  when it goes out of scope.
- A **view** looks at memory that something else owns: a slice `[]T`, a pointer `*T`, `unsafe.Pointer`,
  a closure or method value (section 11.5, Closures and their regions), an interface that holds a copy in
  a frame or lends an owner (section 7.9, Interface types), a string made with `unsafe.String`, and any
  struct, array or union that contains a view and no owner. A pure view does not release borrowed memory.
  Composite views still retain/release counted components, so copying them need not cost only the
  words copied. A view may not outlive the memory it looks at (section 18.3).
- An **owner** holds memory or another resource and releases it when it is dropped. A type is an owner
  only if its declaration says so with `owner` (section 7.18, Owner types), or if it is built from owner
  types in a way that section 7.18 lists (`owned[T]`, and an array, a `ring` or an `aligned` of owners); an owning interface and
  an owner union are owners because they are declared `owner`. Assigning, passing or returning an owner **moves** it (section 18.5); a copy is made only by an
  explicit call such as `Clone`. An owner is **dropped** at the end of its variable's scope unless it was
  moved away (section 18.6).
- A **counted value** shares memory among its copies and counts them: `string`, `map[K]V`, `chan T`,
  `shared[T]`, `synced[T]` and `weak[P]`, and any struct, array or union that contains one and no
  owner or view.
  Assignment copies it and adds one to the count; dropping a copy releases a reference. Destruction at
  the last strong release and reclamation after weak references follow sections 18.7 and 18.8.

| To do this with memory | Use | Cost |
|---|---|---|
| let a function use it for a while | a pure view | no retain/release; composite counted components still follow their rules |
| give it to exactly one new owner | a move | none |
| let several places keep it | `shared[T]`, `synced[T]`, a map or a channel | a count update per copy |
| keep text | `string` | a count update per stored copy of a string made at run time; none for literals |

**Why distinct kinds.** With no garbage collector, nothing frees memory that the program has
stopped using unless the program says who is responsible for it. So the program says what each value
does with memory: a view only looks, an owner only owns, and sharing is chosen by type. Counting everything, as some languages do, costs an update on
every copy and makes the place where memory is freed depend on which copy happens to go last, which a
kernel cannot accept; Plutonium counts only the types that ask for it (section 18.7) and strings
(section 18.8).

Editors and `pluto doc` show each type's kind next to its name: `value`, `view`, `owner (moves)` or
`counted`.

### 18.2 Where variables live

Where a variable lives is fixed by its declaration, never by how the program uses it later:

- A local variable, a parameter, a result variable and a temporary live in the **frame** of the function
  that declares them. So do the copies that interface conversions make in that function (section 7.9),
  the records of closures and method values it makes, the objects of `&T{...}` composite literals and
  the arrays of slice literals it evaluates (section 11.3, Composite literals), and the array that holds
  the arguments of a variadic call (section 11.15, Passing arguments to `...` parameters). A function
  that returns a closure builds it, and the variables it captures, in a **region** of its caller's
frame (section 11.5). The frame is
  reclaimed when the function returns.
- A package-level variable lives in **static storage** for the whole program. So do the composite
  literals, interface copies and closure records of package-level initializers, which run once and whose
  results must outlive initialization (section 15.2, Package initialization).
- A field lives inside its struct, and an element inside its array, wherever the struct or array lives.
- **Heap** memory exists only where the program allocates it: through `new`, `make`, the `pluto/alloc`
  functions, the operations that make strings, maps and channels, and library code that allocates
  (section 18.10). Every allocation belongs to an owner, an arena, or the counted references that share
  it.

The compiler never moves a variable to the heap because its address is taken or because it is used by a
closure, and `return &x` does not copy `x` anywhere: `&x` is a view of `x` where `x` lives (section
11.22, Address operators). A task's frames are on its stack (section 19.1, Tasks).

### 18.3 Views

A view is valid as long as the memory it looks at holds what it held when the view was made. The rules
that keep it so depend on where the memory is.

**Passing a view down is always allowed.** A callee's frame ends before its caller's, so `f(&x)`,
`f(buf[:n])` and `f(v.Slice())` are valid whatever `x`, `buf` and `v` are. A function may return a view
derived from its parameters, such as `return b[4:]`, since the caller's memory outlives the call.

**Frame memory.** A **view of the frame** is a view of a local variable, parameter, temporary,
interface copy, composite literal object, slice literal array or variadic argument array of the current
function, a closure or method value whose record is in the frame, or a struct, array or interface that
holds one. It may be used, copied into local variables, stored in other memory of the same frame and
passed to calls. The compiler rejects every use that could outlive the frame:

| Rule | Error |
|---|---|
| Returning a view of the frame, directly or inside a struct, array, interface or closure | E3001 |
| Storing it in a package-level variable, in memory that may be on the heap, or through a pointer the function received from its caller | E3002 |
| Passing it to a function that does either of these with that parameter, or with something reached from it, or that starts a task with it | E3003 |
| Passing it to a task statement as an argument, receiver or the function started, or as a func argument of a function that starts a task with it (section 12.12, Task statements) | E3004 |
| Sending it on a channel | E3005 |
| Keeping a view of a loop iteration's storage where the next iteration can see it (section 18.4) | E3006 |

Where the type checker sees a view of the frame passed to a task statement or sent on a channel
directly, it reports E2011 instead of E3004 or E3005. A closure that a call returns is a view of the
caller's frame, built in the region the caller provides (section 11.5): returning it again is allowed,
since the caller's caller then provides the region, and the closure-specific errors E3100 to E3102 are
defined there. Since a closure of a frame can never be stored in heap or package memory (E3002), a func
value loaded from such memory is never a view of a frame.

**A local owner's memory.** A view obtained from an owner held in a local variable (`v.Slice()`,
`b.Bytes()`, `&m`, `p.field` through an `owned[T]`, `&*p`, `(*T)(h)` for `type H owner *T`) is bounded by that variable. It follows the
frame rules above, and in addition it may not be used, in the same function, after any of these (E3313):

- the variable is moved, dropped or given a new value;
- a call that takes the variable, or memory it owns, by pointer and may free memory the view looks at,
  or move an owner out of it. The callee's summary records which of its pointer parameters it may free
  or move out of memory through (below), so `s := v.Slice(); v.Push(1); println(s[0])` is E3313: `Push`
  may free the old array. So is `p, _ := r.Peek(); r.Pop(); use(p)` on a local ring of owners
  (section 7.17);
- an assignment to an owner field on the path from the variable to the memory the view looks at, which
  drops the old value: after `p := &*n.child`, `n.child = new(Leaf)` ends `p`.

A view of the variable itself is not ended by a call or field assignment, since the variable is still
where it was and holds the current value. Only two views are views of the variable itself: `&v`, and an
owner larger than one word lent to an ordinary interface, whose data word points at the variable (section
7.9). Such a view may be stored in memory the same variable owns, which is released with what it views.
When the variable is moved whole, its memory goes with the value, and a view of it kept in that value is
then a view of the new owner.

An ordinary interface that holds a one-word value lent from the variable holds a copy of that word, not
the variable's address: an `owned`, `shared` or `synced` box, a map, a channel, a `weak` reference, an
owner defined from a pointer type, or a struct with one field or an array of length 1 that holds one of
these (section 7.9). Such an interface is a view obtained from the variable, and each event of the list
above ends it (E3313). So is the result of an assertion from an owning interface to an ordinary
interface, or the binding of a type switch clause that names one, which lends from the asserted variable
(section 11.13): it holds the payload's word or points at the payload's storage, not at the variable.

```go
func reset(p *owned[T]) { *p = new(T) } // drops the old box

b := new(T)
x := any(b)    // x holds a copy of the box pointer
reset(&b)      // may free the box x holds
fmt.Println(x) // error E3313: x was ended by the call reset(&b)
```

A variable whose drop releases or writes through a
view of another local variable (a string, vector or other value that holds a view of a local arena
or owner) must be declared after that variable, so that it is dropped first (section 18.6, Drops);
otherwise the declaration of the holder is E3313, and the error names both variables. A view of a temporary owner (`makeVec().Slice()`) may not outlive the
statement that made the temporary (E3009; section 18.6). A view obtained through a counted reference held in a
local variable is bounded by that reference in the same way. Other copies of the reference reach the same
object, so a call that may free memory of the object through any reference that may point to it (a copy in
another local, a field, a parameter of the same box type) ends the view too, unless the compiler knows the
two point to different objects: after `b := a` and `s := a.Slice()` for a `shared[vec.Vec[int]]` `a`,
`b.Push(1)` ends `s` (E3313). A function literal holds the views that the local variables it uses hold
where it is made, so calling it after any event of the list above that ends one of those views is E3313
too: the owner is moved, dropped or given a new value, a call may free or move an owner out of the viewed
memory, or an owner field on the view's path is assigned.

**A local's scope.** A view of a local variable whose type has drop glue (an owner, or a value that
holds a counted value: a string, a box, a map, a channel, an allocated error, or a struct, array or
union that holds one; sections 18.6 and 18.8) may not be used after the variable's scope ends (E3313).
The frame slot remains until the function returns, but the variable was dropped, or its references
released, when its scope ended. The rule covers a view of the variable itself (`&s`, a slice of a
local array of strings) and every view obtained from it. A local whose type has no drop glue holds
only bits, and a view of it is bounded by the frame alone.

```go
var p *string
{
	s := strconv.Itoa(12345) // a counted string
	p = &s
} // s is released here
println(*p) // error E3313: s's scope ended at the end of its block
```

The compiler follows views through local variables, their fields, and the calls that store them into
other arguments. A view stored anywhere else (through a pointer, in a variable that a function literal
uses or whose address is kept, or by a call the compiler cannot see into) cannot be followed, so the owner
may not be moved, dropped, given a new value, passed by pointer to a call that may free the viewed memory
or move an owner out of it, or have an owner field on the view's path assigned, while that view could
still be used (E3314). These are the events of the list above.

**Heap memory reached any other way** (through a pointer that a field holds, through a `*T` parameter,
through a box reached from another object) is checked at run time by generation checks (section 18.11):
a use of such a view after its memory has been freed is a run-time panic, or, in the documented
unchecked mode, behavior this specification does not define.

**Static memory, and memory nobody in the program owns** (package variables, string literals, device
registers, memory made from a `uintptr`) is never freed. A view of it may be kept anywhere.

**Facts the compiler cannot see.** A function implemented outside Plutonium (assembly, or `//pu:extern`)
is assumed to keep every pointer argument. `//pu:noescape` on its declaration states that it keeps none
past the call, except by storing one argument into memory another argument points to, which is still
followed (section 20.3, Directives). An `unsafe.Pointer` carries the facts of the pointer it was made
from. A pointer made from a `uintptr` is assumed to point outside every frame, and a pointer converted to
a `uintptr` is no longer followed (section 20.1, Package `unsafe`). `//pu:resultholds p` on a standard
library function that returns an owner states that the function keeps `p` only inside the result, so
the call counts as returning `p` and the result is a view of what `p` points to; the compiler checks that
the result does hold `p` (E3007), and the directive is an error outside the standard library (E3008).
`alloc.Use` is declared this way (section 18.9).

**How precise the frame check is.** So that a programmer can tell what is accepted:

- The check covers the whole program and needs no lifetime, borrow or region annotations. Each function has a summary of which
  parameters it keeps, which it stores into memory another parameter points to, which it returns, and
  through which pointer parameters it may free memory or move an owner out (by dropping, replacing or
  moving an owner reached from the parameter, directly or through the calls it makes), field by field. A call is checked against its callee's summary. A call through an interface is checked
  against every method of that name on a type the program converts to an interface, narrowed by type
  assertions on the way and by the dynamic type where the caller knows it. A call through a func value is
  checked against every function of that type whose value the program takes, narrowed to the closures of
  the current function where only those can be there.
- A value converted to an interface and then asserted, by a type assertion or a type switch case, to an
  interface or to a type other than a pointer is followed after the assertion only for the dynamic types
  the assertion admits; so is the receiver of a method called through an interface, for the type whose
  method is called. A function that keeps what passes an assertion does not keep a caller's value of a
  type the assertion rejects.
- Summaries follow pointers up to three loads deep and eight field steps; beyond that, everything
  reachable is assumed kept.
- A local variable whose address is never taken is followed along the control flow, so reusing a
  variable (`err` above all) for a view of the frame and later for heap memory is accepted. Other
  storage is followed field by field, two levels deep, without regard to the order of statements.
- A generic function is checked once for all its instantiations, and the result is the same for every
  instantiation. A conversion of a value of a type parameter to an interface counts as a frame copy,
  whatever the size of the type argument (section 7.9). For a `[T owner any]` parameter it counts as
  both kinds of view at once: a frame copy, and a view obtained from the variable that each event of
  the list in A local owner's memory (above) ends (E3313, E3314). The checker applies the stricter rule
  of each kind, so the interface may not leave the frame, and it may not be used after the variable is
  moved, dropped or given a new value, after a call that may free or move an owner out of the variable's
  memory, or after an owner field on the path is assigned.
- A struct field is **heap-only** when, in the whole program, its address (or that of anything inside
  it) is never taken and everything stored in it is heap memory, or a parameter for which every call
  passes heap memory. Loading a heap-only field yields heap memory, so following a list's `next` or an
  arena's current chunk does not make a function keep its caller's pointers. Writes through memory
  reinterpreted with `unsafe.Pointer` are not seen (section 18.12).
- A pointer-shaped value in an interface (a struct with one pointer-shaped field, or an array of one,
  section 7.9) is the pointer it holds, not a copy in the frame.

These limits are part of the language: a program that needs more precision is rejected with the path
that could not be proved, and the fix is to allocate, to make a field heap-only, or to restructure the
code. The limits may be raised; they are never lowered.

### 18.4 Loop iterations reuse their storage

Every iteration of a loop reuses the same frame memory for the variables declared in the loop's header
(`for i := ...`, `for k, v := range ...`) and body, for the temporaries and interface copies the body
makes, for the records of the closures and method values the body builds, and for the other frame
storage the body makes (section 11, Expressions): the object of `&T{...}`, the array of a slice literal,
the array of a call's variadic arguments, a method value's receiver and a deferred call's saved
arguments. A view of that memory (a closure that uses one of those variables, `&x`, `&T{...}`, a slice
literal, a slice of a local array declared in the body, an interface holding a copy made in the body) is
therefore valid for one iteration.

- Using such a view within the iteration (calling the closure, passing the pointer down, storing it in a
  variable declared in the body) is allowed and behaves as if each iteration had variables of its own.
- Keeping it where the next iteration can see it is error E3006: in a variable, element or field
  declared outside the loop body or in the loop's header, or anywhere a view of the frame may not go at
  all (section 18.3).
- A local variable whose address is never taken is checked where the next iteration starts, so keeping
  a view in it and then leaving the loop with `break` or `return` is allowed, unless the viewed variable
  has drop glue: its scope ends when the loop is left, and a use of the view after that is E3313
  (section 18.3, A local's scope).
- A closure that uses only variables declared outside the loop refers to the same addresses on every
  iteration and may be kept.
- Copying the variable into one declared in the body (`i := i`) does not help, since that copy is reused
  too. Call the closure within the iteration, store values instead of closures or pointers, or pass the
  value to the closure as a parameter when it is called.
- An object made by `&T{...}`, or the array of a slice literal, is refilled when the next iteration
  evaluates the same expression, so keeping a view of it past the iteration is E3006 too. The fix is to
  allocate a new object each iteration with `new(T{...})`, which returns an owner, or to keep the values
  in an owner such as a `vec.Vec`.

```go
var first func() int
for i := 0; i < 3; i++ {
	f := func() int { return i * 10 }
	println(f()) // allowed: used within the iteration
	if i == 0 {
		first = f // error E3006: the next iteration reuses i
	}
}
```

```go
var ps [3]*T
for i := 0; i < 3; i++ {
	ps[i] = &T{n: i} // error E3006: the next iteration refills the same object
}

var bs [3]owned[T]
for i := 0; i < 3; i++ {
	bs[i] = new(T{n: i}) // allowed: a new object each iteration, owned by bs[i]
}
```

The rule means that every loop that compiles behaves as if each iteration had variables of its own: no
program can observe that the storage is shared. A closure returned by a call in a loop is covered by the region rule of section
11.5 (E3101). Tasks started by a `sync.Scope` in a loop take their own copies of the iteration's
variables instead (section 19.4, Scoped tasks).

### 18.5 Moves

**What moves.** A value of an owner type is moved, not copied, when it is

- assigned to a variable or other place (`n := m`, `n = m`, `c.buf = b`, `s[i] = b`, `*p = b`);
- passed by value as an argument, including as the receiver of a method with a value receiver and
  including an argument of a deferred call, which moves at the `defer` statement (section 12.19, Defer
  statements);
- returned, including as a named result by a bare `return`;
- sent on a channel (section 12.5, Send statements), or passed to a task statement (section 12.12, Task
  statements);
- stored in a composite literal (`Conn{buf: b}`);
- converted to an owning interface (section 7.9), converted to another owner type with the same
  underlying type (section 11.24), or asserted out of an owning interface (section 11.13);
- given to `new(v)` (section 13.9, Allocation);
- dropped explicitly with `x.Drop()` (section 18.6; for `defer x.Drop()`, when the deferred call runs).

A move copies the owner's bytes to the destination and clears the source variable to the zero value of
its type. The zero value of an owner holds nothing, and dropping it does nothing, so the cleared source
needs no separate flag. The compiler tracks that the source holds no value and rejects every later use of
it until the variable is given a new value.

**What does not move.** These read an owner and leave it where it is:

- taking its address (`&b`), calling a method with a pointer receiver (`b.Write(p)`, which passes `&b`),
  and selecting a field to read it;
- converting it to an ordinary interface such as `any` or `error`, which lends it: the interface is a
  view of the variable, or, for a one-word owner, a view that holds a copy of its word (section 7.9;
  section 18.3 says when each ends);
- comparing it with `==` or `!=`, where its type is comparable (section 8.10, Comparable types);
- converting it to a type that is not an owner type, which yields a plain value with no `Drop` (section
  11.24, Conversions);
- `len`, `cap` and other operations that only read;
- the assignment `_ = x`, which reads `x` and moves nothing. To drop an owner early, call `x.Drop()`.
  `_ = f()` drops the unstored result of `f` at the end of the statement, as any temporary is dropped.

**Use after a move.** Using a variable after it was moved (reading it, calling a method on it, taking
its address, moving it again) is error E3301. Using it where it may have been moved on some path,
including a move in a previous iteration of a loop, is error E3302. Assigning a new value to a moved-from
variable makes it usable again. The error names the move and its line and suggests a fix from the later
use: `Clone` when both copies are needed, a pointer when the later use only looks, or reordering when the
later use could come first.

A deferred function literal that uses an owner runs when the function returns. The use counts at every
exit that the `defer` statement reaches, so moving or dropping the owner on a path from the `defer`
statement to an exit is the same error (E3301, or E3302 where only some paths move it), reported at the use
in the literal. Giving the owner a new value before the exit makes the use valid, and a move on a path that
returns before the `defer` statement does not conflict.

The same holds for the other ways a deferred call comes to use an owner. A view of the owner (`&o`, a slice
or field of it, a pointer receiver `o.M`, or a variable that holds such a view) among the receiver and
arguments of the `defer` statement is kept until the call runs, so it counts as a use of the owner at the
exit. A function literal that uses the owner counts as well when it is passed as an argument or is held in a
local variable that is deferred; the compiler follows the variable and its copies in other local variables,
ignores the order of statements, and rejects when a literal that uses the owner may be the one deferred.
Arguments that are copies (`o.n` of an integer field) are not uses of the owner.

The one exception is `defer x.Drop()` on a whole owner variable `x`: it drops whatever `x` holds when it
runs, and nothing if `x` was moved meanwhile (section 18.6; section 12.19). It is not a use of `x` at the
exit, so moving `x` after it is allowed:

```go
f := os.Open(name) or return
defer f.Drop()
send(take(&f)) // allowed: the deferred Drop finds f empty and does nothing
```

**One expression may not move a variable and also use it.** An expression or statement that moves a
variable and uses the same variable anywhere else is a compile error, whatever the order of evaluation:
`send(buf, buf.Len())` and `send(buf.Len(), buf)` are both rejected, and the fix is a separate statement
(`n := buf.Len(); send(buf, n)`). An assignment that permutes places, such as `a, b = b, a`, neither loses
nor duplicates a value and is allowed.

**Places.** An owner can be moved only out of a whole variable: a local variable or a parameter. Moving
one out of a field, an element, the target of a pointer or a package-level variable is error E3303, since
that place would still hold the value and drop it again. To take an owner out of such a place:

- `take(&place)` returns the value and leaves the zero value behind (section 13.10, Moving out of a
  place);
- an assignment that permutes places (`s[i], s[j] = s[j], s[i]`) is allowed;
- `place.Clone()` copies instead, where the type has a `Clone` method.

For the same reason these duplicate owners and are compile errors: a `for _, x := range s` loop whose
element is an owner (E3304; range over the indexes and use `&s[i]`), `copy` of owner elements (E3306),
owners as the variadic arguments of a call (E3310), owners in a slice literal (E3312), and reading `x[k]`
from a library container of owners (E3309; section 11.11, Indexing library containers).

**Captured owners.** A function literal may use a captured owner, call its methods, take its address and
assign it a new value, which drops the old one. Moving a captured owner inside the literal is error
E3305: the literal may run more than once, or never, so the enclosing function could not tell whether the
variable still holds a value. Pass the owner to the literal as an argument instead. The one exception is
an owner that a task started by a `sync.Scope` takes: the task has its own, and its literal may move it
(section 19.4, Scoped tasks, which states the rule for scoped tasks and owners).

**Methods.** A method with a pointer receiver borrows its receiver. A method with a value receiver on an
owner type consumes its receiver: calling it moves the owner into the method, which drops it when it
returns unless it moves it on. Owner types declare their methods on pointer receivers unless a method's
purpose is to consume. Because calling it would consume the value, a value-receiver method of an owner
type never satisfies an interface method, ordinary or owning (section 8.6, Method sets): converting such
an owner to an interface that needs the method is a compile error that shows the pointer receiver as the
fix. A method value of a consuming method (`done := c.Close`) is a compile error, since every call of it
would consume the receiver again; the method expression `Conn.Close(c)` and the call `c.Close()` work.

**Interfaces.** Converting an owner to an ordinary interface lends it, and asserting an owner type back
out of an ordinary interface is error E3308, since the result would be a second owner. Converting an
owner to an owning interface moves it in; a type assertion to an owner type moves it out, and in the
comma-ok form only where `ok` is true, as the compiler follows it through direct tests of `ok`. An
assertion from an owning interface to an ordinary interface moves nothing and lends. A type switch with a
binding on an owning interface moves it into the binding in every clause except one that names a single
ordinary interface type, where the binding lends; after the switch the variable counts as possibly moved
if some paths moved it and others did not (section 11.13, Type assertions; section 12.9, Switch
statements).

**Conversions.** Converting a value into an owner type creates a new owner, and converting between two
owner types with the same underlying type moves. Both are allowed only where section 11.24 (Conversions,
Owners, rules 1 and 3) allows them: in the package that declares the result type, where its invariants are
kept, or, for a move, in the package of an operand type defined from the result type; and when the result
type is defined from another owner type, only where a conversion to that type is allowed too (E3212).
Otherwise any package could forge an owner of another package's type through a look-alike owner of its
own. A pointer conversion that reaches an owner, a union conversion that makes an owner union, and a
composite literal of an owner struct with unexported fields are limited the same way (section 11.24,
Conversions; section 7.18, Owner types). Converting an owner to a non-owner type reads it (above).

**Generic code.** A type parameter admits owner types only if its constraint is marked `owner` (section
10.9, Type parameter declarations); instantiating any other type parameter with an owner type is error
E3207. Inside generic code, values of an `owner` type parameter move as owners do, and a variable of such
a type still holding a value at the end of its scope is dropped, which does nothing for an instantiation
with a type that is not an owner.

**Select.** In a `select` statement, the value of a send case is evaluated on entry, but an owner is moved
only if that case proceeds. Inside the other clauses the variable still holds its value, and after the
`select` it counts as possibly moved (section 12.13, Select statements).

### 18.6 Drops

**When an owner is dropped.** An owner held by a variable when the variable's scope ends is dropped, on
every path out of the scope: falling off the end of a block, `return`, `break`, `continue`, `goto`, and
the end of each iteration for a variable declared in a loop body. In detail:

- Within one block, variables are dropped in reverse order of declaration, so an owner that holds a view
  of an earlier variable (a vector that allocates from a local arena) is dropped before it. A
  `sync.Scope` is the exception: the Scopes of a block, and the `sync.Scope` parameters of a
  function, are dropped before its other variables (section 19.4).
- Parameters are dropped when the function returns, after its local variables, except
  `sync.Scope` parameters, which are dropped before them. Results are moved to the
  caller and not dropped.
- A variable or parameter that a closure the function returns captures lives in the caller's region and
  is not dropped when the function returns. The region drops it when the call runs again and refills
  the region, and when the caller's frame returns (section 11.5, Closures and their regions).
- A variable or place the compiler knows holds nothing (moved away on every path, never given a value,
  a part a composite literal or `new(T)` left zero, a place emptied by `take`) is not dropped, and an
  assignment to it does not drop its old value. Where a variable may have been moved on some paths only,
  it is dropped holding the zero value.
- An owner variable declared in a `for` clause moves from each iteration to the next and is dropped
  once, when the loop ends (section 12.10).
- Leaving several scopes at once drops what each holds, innermost first. A `goto` back to a label before
  a variable's declaration drops the variable first, since its declaration runs again.
- Deferred calls run before the function's own variables are dropped, so a deferred call may use them.
  A deferred call may not use an owner declared in an inner block, which is dropped when that block ends
  (E3311).
- `defer x.Drop()` saves `&x` at the `defer` statement (the `Drop` method has a pointer receiver), so `x`
  stays usable until the deferred call runs. The deferred call then drops whatever `x` holds at that point
  (nothing, if `x` was moved), and `x` is not dropped again at the end of its scope. Any other owner
  passed to a deferred call by value moves at the `defer` statement (section 12.19).
- `rt.ExitTask` leaves every frame of the task through its return path, so each frame runs its deferred
  calls and drops what it holds (section 17.3, Ending a task with `rt.ExitTask`).
- A panic drops nothing. In hosted builds the panicking task's deferred calls run first (section 17.2,
  Deferred calls on a panic), but no owner is dropped and no frame is reclaimed. `os.Exit` drops nothing.
- When `main` returns and no other task is running, package owners and
  counted references are released in reverse initialization order (section 15.4, Program execution).
  Cleanup runs in place and leaves the place zero. Static storage remains live, but a place whose
  cleanup has finished cannot be written through typed operations during shutdown, including through
  an alias, a zero assignment, `take` or `clear`. This protects scalar fields of a cleaned owner too.
  Each field or element becomes subject to that rule when its own cleanup finishes.
  Runtime services remain available until their clients' releases complete. Panic and `os.Exit` do not
  acquire this normal-termination cleanup, and neither does a program whose `main` returns while another
  task is running.

**Assigning to a place that holds an owner** evaluates the new value, drops the old one, then moves the
new one in: `c.buf = b` frees the buffer `c.buf` held before. The same holds through a view: `*p = v` and
`s[i] = v` drop the old contents. `clear(s)` on a slice of owners assigns the zero value to each
element and so drops each in index order (section 13.3, Clear).

**Temporaries.** An owner that an expression produces and nothing stores is a **temporary**. It is
dropped at the end of the statement that made it, so a view of it may be passed to a call in that
statement: `os.Stdout.Write(readAll(f).Slice())` is valid. A temporary in the header of an `if`, `switch`
or `for ... range` statement lives until the whole statement ends; one in the condition or post
statement of a three-clause `for` is dropped after each evaluation.

**What dropping does.** Dropping an owner runs its `Drop` method if it has one, then drops its fields in
declaration order and its elements in index order. Pure values and pure views need no cleanup;
counted components are released even inside a composite whose kind is view. The compiler-generated part of this, which releases fields and
elements, is the type's **drop glue**; values that are not owners but contain counted values have drop
glue too (section 18.8). In particular:

- an `owned[T]` drops its `T` and frees the memory to the allocator recorded in its header (section
  18.7);
- an owning interface drops the value it holds through its dynamic type's drop code, whatever that type
  is (section 7.9);
- an owner union drops the payload of the variant it holds (section 7.13, Unions);
- an array of owners drops each element;
- a value of a type defined from an owner type (`type Log owner File`): its own `Drop` method if it
  declares one, then the drop of the owner type it was defined from (its `Drop`, then its fields; section
  7.18, Owner types).

**The `Drop` method.** A method named `Drop` must have no parameters and no results (E3206) and may be
declared only on an owner type (E3201), with a pointer receiver (section 7.18). It is called by the
compiler; the program calls it only as `x.Drop()` on a whole owner variable. That call is valid on every
owner variable, whether or not its type declares a `Drop` method (a type defined from an owner has none of
its own, section 7.18). It drops `x` completely (its `Drop` method, then its fields), counts as a move at
the point the call runs, and leaves `x` not dropped again. Calling `Drop` any other way (through a pointer, a
method value or an ordinary interface) is error E3307, since the real owner would drop the value a second
time. `Drop` must treat the zero value as empty, since a moved-from variable, the result of `take`, a
variable that was never filled and memory from every allocator all hold the zero value. Since a
value known to hold nothing is not dropped (above), a variable never given anything but the zero value
is never dropped: an owner type whose `Drop` must run has to hold a value. `Drop` cannot
report failure: a type whose release can fail provides a method such as `Close() error` for callers that
care, after which `Drop` has nothing left to do.

**Memory the compiler does not drop.** Values in memory from `alloc.New` or from an arena's `New` and
`Slice` are not dropped by the compiler: that memory has no owner until the code that allocated it gives
it one. An `arena.Fixed` or `arena.Chunked` drops the values of its `New` and `Slice` allocations itself,
in reverse order of allocation, when it takes their memory back (section 18.9, Drop entries).
`alloc.Free(p)` drops `*p` before freeing it, and code that manages memory itself (an allocator,
a container dropping its elements) drops a value through a pointer with `unsafe.Drop(p)` (section 20.1).

**Which task drops.** An owner is dropped by the task that holds it when its scope ends. The last
reference to a counted value is released by whichever task drops it last, and that task runs the drop of
the shared object (section 18.7).

### 18.7 Shared ownership

**`owned[T]`.** The box `owned[T]` is the single owner of one heap object of type `T` (section 7.19,
`owned`, `shared`, `synced` and `weak`). `new(T)` and `alloc.Owned[T]()` return one. It moves like any
owner, and dropping it drops the `T` and frees the memory.

**Counted references.** Shared ownership is chosen by type, never by default:

- `shared[T]` counts its references with plain arithmetic. It may be used by one task only: it is not
  sendable (section 19.2, The sendable rule).
- `synced[T]` counts its references atomically and may cross between tasks. Its count is safe to share;
  the fields of `T` that change are subject to the race rules of section 19.3 (Data race freedom).
- `weak[P]`, where `P` is `shared[T]` or `synced[T]`, refers to the same object without keeping it alive
  (`weak` of any other type is E3211). `w.Upgrade()` returns a new strong reference and `true` while the
  object is alive, and the zero `P` and `false` once the last strong reference is gone, or for the zero
  `weak`. A `weak[synced[T]]` is sendable; a `weak[shared[T]]` is not.

`alloc.Shared[T]()` and `alloc.Synced[T]()` make a counted box holding a zeroed `T`; a `weak[P]` is made
by converting a strong reference, `weak[shared[Node]](p)`. The rules:

1. Copying a strong reference (assigning it, passing it, storing it, returning it while the source is
   still used, sending a `synced` reference) adds one to the strong count. Copying a weak reference adds
   one to the weak count.
2. Dropping a reference (its variable's scope ends, it is overwritten, the object holding it is dropped)
   subtracts one from its count.
3. When the strong count reaches zero, the object is dropped: `T`'s `Drop` method runs, then `T`'s fields
   are dropped. All the strong references together hold one weak reference, which is released after the
   object is dropped; when the weak count reaches zero, the memory is freed to the allocator recorded in
   the box's header. Until then the memory stays allocated so that `Upgrade` can see that the object is
   gone.
4. The compiler removes a count update and the matching release when nothing in between can observe the
   count. `return p` as the last use of `p`, or passing `p` as its last use, moves the reference and
   costs nothing. Passing a reference down to a call counts it like any copy, and the compiler removes the
   update when the caller keeps the value alive for the whole call, so that the callee counts only
   the copies it keeps.
5. A view obtained through a counted reference (`p.field`, `&*p`, `(*T)(p)`) is bounded by that
   reference as a view of a local owner is (section 18.3).
6. Cycles of strong references are never freed. A structure with back pointers makes them `weak`.

**When counts are atomic.** Every reference count except a `shared` count follows one rule: the count of a
`synced` object, of a string buffer, of a map table, of a channel and of an allocated error is plain
arithmetic until the program starts its first task or thread, and atomic from then on. A hosted program
runs on one thread until it starts its first task, so the first task is the first moment a second
processor, or a task that preempts the running one between the load and the store of a plain increment,
could meet a count. The runtime makes the switch before the new task or thread can run, whatever the
scheduler, and the switch cannot be undone (`rt.SetThreaded`, appendix F). A freestanding program counts
atomically from the start, since its kernel may run code on other processors without telling the runtime.
The switch is invisible to the program (section 19.7, Memory model; memory-model.md section 3.7). A
`shared` count is never atomic. This is the only statement of the rule: the other sections cite it.
`Upgrade` of a `weak[synced[T]]` is an atomic compare-and-swap once counts are atomic, and fails once the
strong count is zero, so it never revives an object whose drop has begun.

The representation of the boxes and of `weak[P]`, how they are used through `*T`'s methods and fields,
embedding, comparison, and the rule that a box cannot hold an interface type are defined with the types
(section 7.19, `owned`, `shared`, `synced` and `weak`).

**Counted reference types.** Besides `shared[T]` and `synced[T]`, four built-in kinds of value share
memory by counting. Their counts are atomic exactly when a `synced` count is (above):

| Type | What is shared | Count | Rules |
|---|---|---|---|
| `string` | an immutable buffer of bytes | as for `synced` | section 18.8 |
| `map[K]V` | a hash table | as for `synced` | section 7.10 (Map types) |
| `chan T` | a channel and its buffer | as for `synced` | section 19.8 (Channels) |
| an allocated error | the error object | as for `synced` | section 16.7 (Allocated errors and ownership) |

Copies of a map share its table, so `m2 := m` and a function that fills its map parameter change the
same table; the table is freed when its last reference goes. Keys and values of a built-in map may not be
owners (section 7.10, Map types: the error shows a `synced[T]` element instead); counted keys and values
are counted as they are copied in and out, and released when entries are deleted, by `clear(m)`, or with the table. A
channel's last reference drops the values still in its buffer and frees the channel (section 19.8).
Counted values are released by the same rules as `shared` references: a store takes a reference for the
new value and releases the old one, a parameter is counted like any copy, and the update is removed when the caller keeps the value
alive for the call (rule 4), and a result hands its reference to the caller.

**Why not count everything.** Every copy and drop of a counted reference writes the object's header, and
every drop tests for zero. A `synced` count is an atomic operation that costs tens of cycles, and when
several processors copy one reference, the cache line holding the count moves between them on every copy.
With counts, memory is freed wherever the last reference happens to go, possibly in a latency-sensitive
path or an interrupt handler, possibly a whole graph at once. With views and moves, the place where
memory is freed is written in the source. So Plutonium counts only the types that ask for it, and
strings, whose buffers never refer to other memory (releasing one never frees a graph) and which are
copied far more often than changed.

### 18.8 Strings

A `string` is an immutable sequence of bytes that holds valid UTF-8 (section 7.3, String types). Strings
copy as freely as integers: assignment, passing, returning and storing copy them, and nothing frees a
string's bytes while a copy can still reach them, except that a string whose buffer is in a local
arena is a view of the arena (below).

**Buffers and counts.**

- A string made at run time (by `+` and `+=`, `string(runes)`, `string(r)` for a rune that is not a
  constant, `strings.FromUTF8`, `fmt.Sprintf`, `strconv.Itoa`, `strings.Builder` and other library
  functions) has a **buffer**: its bytes, allocated from the current allocator (section 18.9) behind a
  header that records the allocator, the size and a count of the strings that use it. A buffer never
  changes once a string uses it.
- Copying a string adds one to the count. Dropping a copy subtracts one, and the last drop frees the
  buffer to the allocator recorded in its header. The count is plain or atomic by the rule of section 18.7
  (Shared ownership, When counts are atomic).
- A substring made by `strings.Slice` or `strings.SliceFrom` shares `s`'s buffer and keeps it alive.
- String literals, constants and every other string the compiler places in static storage are never
  counted: copying or dropping one costs nothing. The zero string is static.
- A string made with `unsafe.String(p, n)` is not counted. It is a view of the memory at `p`, which must
  outlive every copy of it (section 18.3); section 20.1 (Package `unsafe`) says what follows if the bytes
  are not valid UTF-8.

**Strings and ownership.** A string is a counted value, not an owner: it never moves, and copying is
always allowed. A struct or array with string fields is a counted value too: it copies freely, each copy
counting its strings, and needs no `owner` declaration. When such a value is dropped, its drop glue
releases its strings. A counted value is dropped at the points where an owner would be (section 18.6,
Drops):

- at the end of a variable's scope, on every path out of it (except a variable a returned closure captures,
  which its region releases, below), and at the end of each iteration for a
  variable declared in a loop body;
- when a place is overwritten: the new value takes its references first, then the old one is released,
  so `s = strings.SliceFrom(s, 1)` and `a, b = b, a` are safe;
- for a temporary, at the end of the statement that made it; in the condition or post statement of a
  three-clause `for`, after each evaluation;
- for storage that belongs to the frame but outlives a statement (an interface copy, the object of
  `&T{...}`, a slice literal's array, the array of a call's variadic arguments, a method value's receiver, a deferred call's saved arguments, the
  region a call builds its closures in, with every captured variable in it), when the function returns, after its deferred calls, or when the
  same expression runs again and refills it.

A panic and `os.Exit` release nothing, as for owners. A `[]string` is a view: its elements belong to the
array it looks at, and whoever owns that array releases them. Library containers such as `vec.Vec[string]`
release their elements when they are cleared, truncated or dropped. `alloc.Free(p)` releases the strings
in `*p` before freeing it.

**Passing and returning.** A string parameter is borrowed: the caller keeps the string alive for the
call, and the callee counts only the copies it keeps. Since the callee holds no reference for the value
it was passed, the first assignment of a new value to the parameter releases nothing; the new value is
counted as for any local, and released at the end of its scope or by a later assignment. A result is
handed to the caller with one reference.

**What can be observed.** When a string's buffer is freed is not observable through the language, only
through the memory the program uses. The compiler removes count updates that cannot be observed: passing
a string down costs nothing; a string read from memory a call could change is held for the statement
unless no call made while it is held can release a string; copying a temporary into a variable hands its
reference over; `return x` hands over `x`'s reference. One consequence is visible in memory use: a string
converted to an interface is copied into the converting function's frame, which holds a
reference until the function returns or the same conversion runs again (section 7.9).

**Arenas.** A string built while an arena is the current allocator has its buffer in the arena. Dropping
the string writes its header, so the string must not be used, or dropped, after the arena is reset or
dropped. For an arena in a local variable the compiler checks this: such a string is a view of the arena
(section 18.9). A string must be declared after the arena it views, and is dropped before it (section 18.3, A
local owner's memory). For every other arena the arena checks it at run time: it counts its live
allocations, and `Reset`, `Release` and `Drop` panic while a string made in it is still referenced anywhere,
in this task or another (section 18.9, Live allocations). They first run the arena's drop entries, so a
string stored in a plain view of the arena (`a.Slice[string](n)`) is released before the count is checked
(section 18.9, Drop entries).

**Races.** A string's bytes never change, so reading a shared string needs no lock. Writing a string
variable that another task may read at the same time is a data race on a value of three words; safe code
cannot do it (section 19.3).

### 18.9 Allocators

**The interface.** An allocator is any value that implements `alloc.Allocator` (appendix F, Library
names the language relies on):

```go
package alloc

type Allocator interface {
	Alloc(size, align uintptr) unsafe.Pointer // zeroed memory, or nil
	Free(p unsafe.Pointer, size, align uintptr)
}

func Default() Allocator     // the current allocator of the calling task
func SetDefault(a Allocator) // replaces the program-wide default
func Use(a Allocator) Restore
func (r Restore) Restore()
```

`Alloc` returns zeroed memory of at least `size` bytes aligned to `align`, or nil when it has none.
Every allocation the language makes and finds nil panics with "out of memory". An implementation of
`Allocator`, which is unsafe code (section 18.12), must not take back in `Alloc` memory it has handed
out, and must take back in `Free` only the memory it is given; an arena takes memory back in bulk only
through its own methods (`Reset`, `Release`, `Drop`). Generation checks rely on this to keep a check of
memory a function holds valid across calls that allocate or free other memory (section 18.11).

**Checked allocator contract.** In a checked build, an allocator used for ordinary heap allocation
must provide the generation and metadata lifetime contract of section 18.11 and abi.md section 1.3.
This applies to custom allocators, to direct constructors and to allocations made through the current
allocator, including its scoped overrides. The compiler and runtime provide the registration
mechanism; its use by an allocator does not require a directive on ordinary program code. An
implementation must not accept such an allocation and silently leave its views unchecked merely
because the allocator is custom. Both `arena.Fixed` and `arena.Chunked` ordinary allocations
must meet the arena lifetime guarantee below; static proof may replace a run-time check when it
prevents every stale use. Unsafe code that makes or exposes an untagged
pointer remains responsible for its lifetime (section 18.12).

**Choosing an allocator.** Every operation that allocates takes its memory from an allocator given
explicitly, or from the **current allocator** that `alloc.Default()` returns:

- `new`, `make` of a channel or map, and the `pluto/alloc` constructors accept an optional final
  allocator argument (section 13.9, Allocation; section 13.7, Making channels and maps). Library functions
  that allocate take one as a defaulted parameter, `a alloc.Allocator = alloc.Default()` (section 10.13,
  Default parameter values).
- The operations that make strings, map literals, and library code that allocates without an allocator
  parameter use the current allocator.
- Owners record the allocator they came from (in a field, or in the header of a box, string buffer, map
  table or channel), and free to it when they are dropped, whichever allocator is current by then.

**The program-wide default.** In hosted builds the program starts with a default allocator that is safe
to use from several tasks at once. In freestanding and kernel builds there is none: every allocation that
uses the current allocator panics with "no allocator" until the program calls `alloc.SetDefault`, and
`alloc.Default()` returns nil until then, in a task with no override. A
package initializer that allocates is a compile error (section 15.2). `SetDefault` replaces the default
for every task; the allocator given to it must be safe for concurrent use once tasks run in parallel.

**Scoped overrides.** `alloc.Use(a)` makes `a` the current allocator of the calling task until the
`Restore` it returns is dropped, or its `Restore` method, which consumes it, is called. The previous
current allocator is then put back. `defer alloc.Use(a).Restore()` overrides it for the rest of the
function:

```go
func handle(req *Request) {
	defer alloc.Use(req.arena).Restore()
	parse(req) // every allocation through Default() lands in the arena
}
```

The override belongs to the task that called `Use`: `Default()` returns it there and the program-wide
default in every other task. **Every task, scoped or not, starts with the program-wide default
allocator**, never with an override of the task that started it. An arena is in general not safe for
concurrent use, so a task that should allocate from an arena installs one of its own:
`s.Task(func() { defer alloc.Use(&arenas[i]).Restore(); ... })`.

`Restore` is an owner, and `alloc.Use` is declared `//pu:resultholds a` (section 18.3), so the `Restore`
is a view of what `a` points to. An arena in a local variable may therefore be installed, and returning
the `Restore`, or storing it anywhere that outlives the arena, is a frame-view error.

**Allocations from a local arena.** So that nothing allocated from a local arena outlives it, a value
allocated through the current allocator while a `Restore` that views a local arena is live is a view of
that arena for the rules of section 18.3: returning it, storing it in a package variable, or passing it
to a task statement is a frame-view error that names the arena. The window is the extent during which
the `Restore` is live: it starts at the call of `alloc.Use` and ends where the `Restore` is consumed by
its method or dropped. For `r := alloc.Use(&a)` that is the call `r.Restore()`, or else the end of the
block that declares `r`. For `defer alloc.Use(&a).Restore()` it is the end of the function. A `Restore`
that nothing stores is a temporary, so its window ends with the statement that made it (Temporaries,
section 18.6). The window is a stretch of execution, not of source text: an allocation that a callee
makes through the current allocator while it is open also lands in the arena.
It covers `new` and `make` without an allocator argument, the operations that make strings, calls that
leave out a defaulted `alloc.Allocator` parameter (such as `vec.New[T]()` or `fmt.Sprintf`), and calls
whose results keep their allocator out of sight (a string, a box, an interface such as
an error). A call is in the last group when the summary of its callee says that the result holds
memory the callee allocated from the current allocator, directly or through a field; where the
summary cannot show otherwise, the compiler assumes it does (section 18.3, Views):

```go
func name(n int) string {
	a := arena.NewFixed(256)
	defer alloc.Use(&a).Restore()
	return fmt.Sprintf("item %d", n) // error E3001: the string is in a, which is gone when name returns
}
```

To keep something past the arena, allocate it from an allocator that outlives it, passed explicitly
(`heap := alloc.Default()` before `Use`, then `vec.New[T](heap)`), or install an arena the caller
provides. A view stored in heap memory does not lose its arena lifetime. Where the compiler cannot
follow such retention, a checked build uses the runtime lifetime checks of section 18.11. The same
rule applies to fixed and Chunked arenas after reset, release past the view, drop or backing reuse.
An implementation must reject an unproved use if it cannot provide the required runtime check;
it must not silently accept an unchecked ordinary arena view. Such a precision or implementation
limit remains a gap for valid safe arena programs, not a restriction on the specified API.

**Arenas.** An arena owns everything allocated in it and reclaims it all at once when it is reset or
dropped. Freeing into an arena takes no memory back, so the same owner code works in an arena and on the
general heap: an owner allocated in an arena is dropped as usual, and its free takes no memory back. Freeing
any allocation, an owner's or a counted object's, only lowers the arena's count of live allocations
(below). The library provides
`arena.Fixed`, a fixed-capacity owner of private backing from its parent allocator, and `arena.Chunked`,
an owner that takes more chunks from a parent allocator as needed. The ordinary fixed constructor is
`arena.NewFixed(capacity uintptr, parent alloc.Allocator = alloc.Default()) Fixed`. It explicitly
allocates its private backing, never grows or relocates its payload, and returns backing to the parent
on drop. Its parent remains a lifetime dependency. `Fixed` moves rather than copies; borrowed pointer
aliases share one allocator identity and cursor. The constructor panics on unavailable backing or
invalid capacity rather than returning unchecked storage. Zero capacity gives an empty fixed arena.
Payload capacity excludes allocation bookkeeping. The constructor sizes a bookkeeping table from the
arena's own backing, in the same request to the parent, in checked and unchecked modes. An allocation
takes a record from that table and nothing from the parent, so allocating, `Reset` and `Release` never
call the parent, cost no more than for an arena without bookkeeping, and may run where the parent may not
(section 20.10, Interrupts). An allocation that finds the table full fails without advancing the cursor,
as one that finds the payload full does. Dropping the arena returns its backing, table included. Ordinary reads and writes allocate nothing. Stable
check-reader and address-history metadata uses the runtime directory storage described in abi.md
section 1.3; it is not payload or a hidden change of the arena's chosen parent.
An arena also hands out memory as
plain views (`a.New[T]()` returns a `*T`, `a.Slice[T](n)` a `[]T`) that are valid until the arena is
reset or dropped. The compiler does not drop the values in them; the arena does, through its drop
entries (below).

**Drop entries.** When `New` or `Slice` of an `arena.Fixed` or `arena.Chunked` allocates a type with
drop glue (section 18.6, What dropping does), the arena records a **drop entry** in its bookkeeping: the
address, the number of elements and the type's drop function. `Reset` and `Drop` run every entry, and
`Release(m)` runs the entries recorded after the mark, in reverse order of allocation. Each entry drops
its values in place, as drop glue does, and leaves the zero value in each place. Then the call checks the
count of live allocations (below). A type without drop glue records nothing and costs nothing.

- A place the program already emptied holds the zero value, and its entry's drop does nothing there: an
  owner moved out with `take`, or a place assigned the zero value, which dropped the old value (section
  18.6). Unsafe code that drops a value in arena memory with `unsafe.Drop`, which does not clear the
  place, must leave the zero value there, or the entry drops the value a second time (section 18.12).
- The program's views of the memory being taken back are dead from the start of the call, under the
  rules that already apply: for an arena in a local variable the compiler rejects a use after the
  `Reset` or `Release` (E3313, below), and the epoch check catches the rest (section 18.11). The arena
  ends its epoch only after the entries have run, so a drop function that reaches an earlier value
  through a view stored in a later one finds valid memory, holding the zero value once its entry has run.
- The arena takes an entry off its table before running it. A drop function that panics leaves the call
  at once, since a panic drops nothing (section 18.6): the entries not yet run stay recorded, the count
  is not checked, the epoch does not end and the arena takes nothing back. A deferred `Reset` that runs
  for that panic starts again from the entries that are left.
- An allocation from the arena made while its entries run is counted as usual, and an entry it records
  is not run by this call. Either one makes the call panic with "arena has live allocations".
- A `Fixed` keeps its drop entries in the bookkeeping it sizes from its own backing (above), and a `New`
  or `Slice` that finds no room there for its entry fails, without advancing the cursor, as an allocation
  that finds the table full does. So the payload capacity holds in full only for allocations that record
  no entry. A `Chunked` keeps its entries in blocks taken from its chunks, and a `New` or `Slice` fails
  when the parent has no memory for a new block. A failed `New` or `Slice` panics (section 17.1). The
  manual `Arena` of `arena.NewUnsafe` records no entries: its caller drops what it stored (below).

**Live allocations.** Everything allocated in an arena, except its plain views, must be freed before the
arena takes its memory back. An owner made in the arena (a box from `new(T, &a)`, the array of a `vec.Vec` that grows while the
arena is the current allocator, a map table, a channel) can be moved to another task, and a counted object
(a string, a `shared[T]` or `synced[T]` box, an allocated error, section 16.6) can be copied to one; freeing
either writes memory the arena handed out. So `arena.Fixed` and `arena.Chunked` each count their **live
allocations**: every allocation made through the arena's `Alloc` that is not freed yet, owners and counted
objects alike. The count rises when `Alloc` hands out memory, and falls when that memory is freed to the
arena: when an owner is dropped, or a counted object's last reference is released. Only the plain views
`a.New[T]()` and `a.Slice[T](n)` (above) are not counted; they are bounded views of the arena, which the
frame and arena rules keep from outliving it. `Reset`, `Release` and `Drop` run their drop entries, then
panic with "arena has live allocations" while the count is not zero (section 17.1, Panics), and take
nothing back. `Release` checks the
whole count, so an allocation made before the mark and not yet freed also stops it: an owner that must
outlive a `Release` is allocated from the parent or from another arena. The cost is one counter per arena
and one increment and one decrement per allocation made through `Alloc`; the counter is atomic, since the
last reference to a string can be released, and an owner dropped, in another task. That cost is accepted
(section 21.5). An owner or counted value stored in a plain view of the arena is not dropped by the
compiler; the arena's drop entry drops it at the start of the call that takes that memory back, so it does
not stop that call. One stored in memory the program manages itself (a plain view of another arena,
memory from `alloc.New`) keeps the count up until that memory is dropped. For an arena in a local
variable the compiler also checks, at compile time, that nothing allocated in it while it is installed
outlives it (Allocations from a local arena, above; section 18.3). The count catches what those rules do
not see: a `Reset` or `Release` that comes before the program has dropped what it allocated, an arena that
is not a local variable, and an allocation the program leaked, as a cycle of `shared` boxes does (section
18.7). The manual `Arena` of `arena.NewUnsafe` keeps no count; its
caller frees every allocation and ends every count obligation before taking memory back (below).

```go
func (s *Server) step(n int) {
	r := alloc.Use(&s.arena)
	s.last = fmt.Sprintf("item %d", n) // a counted string in s.arena, kept in a field
	r.Restore()
	s.arena.Reset() // panics: s.last still references a string in the arena
}

func (s *Server) handoff(ch chan owned[Job]) {
	j := new(Job, &s.arena) // an owner in s.arena: counted
	ch <- j                 // moved to another task
	s.arena.Reset()         // panics while the other task has not dropped j
}

func (s *Server) batch(names []string) {
	defer alloc.Use(&s.arena).Restore()
	ns := s.arena.Slice[string](len(names)) // a plain view; the arena records a drop entry
	for i, n := range names {
		ns[i] = strings.ToUpper(n) // counted strings in s.arena, kept in arena memory
	}
	s.report(ns)
} // the caller's s.arena.Reset() drops the strings in ns first, so it does not panic
```

A deferred `Reset` runs before the function's variables are dropped (section 18.6), so it panics when one
of them still holds memory of the arena. The drop entries do not help, since `msg` is a variable of the
frame and not a value in a plain view:

```go
func (s *Server) handle() {
	defer s.arena.Reset()                    // panics: msg still references a string in the arena
	defer alloc.Use(&s.arena).Restore()
	msg := fmt.Sprintf("request %d", s.n)    // a counted string in s.arena
	s.send(msg)
}
```

The fix is to drop such variables before the `Reset` runs: do the work in a helper function, or in a
nested block, whose variables are dropped when it ends. A `Reset` written as the last statement of
`handle` panics too, since `msg` is still in scope there.

```go
func (s *Server) handle() {
	defer s.arena.Reset() // runs after work has returned and dropped msg
	s.work()
}

func (s *Server) work() {
	defer alloc.Use(&s.arena).Restore()
	msg := fmt.Sprintf("request %d", s.n)
	s.send(msg)
}
```

Ordinary typed allocation, `Reset`, `Release` and drop must preserve this lifetime guarantee.
Marks identify their arena and the recorded allocation position. A foreign mark or one invalidated
by reclamation cannot reclaim unrelated current storage; it panics. Releasing a later mark does not
invalidate an earlier still-live mark.
`Release` invalidates only allocations it reclaims; earlier allocations remain valid.
For a fixed or chunked arena held in a local variable, the compiler checks this (E3313): a view that
`New`, `Slice` or `Alloc` returned may not be used after a `Reset` of the arena, or after a
`Release(m)` that reclaims it. The compiler takes `Release(m)` to reclaim the allocations made after
the statement `m := a.Mark()` when that is the only statement that gives the local variable `m` a
value and the function has no `goto`; for any other `m` it takes every allocation as reclaimed.
A fixed manual `Arena` constructed over a caller-provided buffer continues to use that buffer; it must not
silently replace it with an allocated buffer or overwrite payload with metadata. The
raw allocation
layer and unsafe pointer operations remain available to code that deliberately manages lifetimes
itself (section 18.12); their obligations do not become an unstated precondition of an ordinary API.

`arena.NewUnsafe(buf []byte) Arena` is the explicit borrowed-buffer constructor. It allocates no
private payload and does not free the caller's buffer. Its copyable `Arena` representation is a manual
allocator: the caller must keep the backing alive and exclusively available, avoid overlapping cursor
copies, and end all views and count obligations before taking their memory back. It has no ordinary
safe-lifetime guarantee. There is no `arena.New(buf)` constructor that hides this obligation.

**The debug allocator.** `pluto/alloc/debug` diagnoses corruption detectable by its page protection,
canaries, poison and retained allocation records, and reports tracked surviving allocations. Protected
accesses fault while protection remains; poison/canary corruption is diagnosed when inspected, and
finite quarantine limits detection after reuse. These mechanisms supplement the ordinary safety
contract, not replace it. It can be the default allocator of a hosted program without code changes;
see [debug-allocator.md](../debug-allocator.md).

### 18.10 No allocation without an owner or an arena

Every ordinary allocation has a specified owner, arena or counted-reference lifetime. Allocating
operations and APIs document their behavior; the rule does not require an annotation at every call
site and never silently promotes a local to the heap. Defined cleanup responsibility does not guarantee
eventual reclamation: the program must avoid or break strong-reference cycles (section 18.7).

- `new(T)` and `new(v)` return an `owned[T]` (section 13.9). There is no `make` of a slice: growing
  sequences are `vec.Vec[T]`, an owner, made with `vec.Make` or `vec.WithCapacity` (section 13.7). A slice
  is a view and never grows (section 7.5, Slice types).
- `make(chan T, ...)` and `make(map[K]V, ...)` return counted references, freed with their last
  reference.
- The operations that make strings return counted strings. Allocated errors (`fmt.Errorf`,
  `errors.Join`) are counted objects (section 16.7).
- Library types that hold memory (`vec.Vec`, `hash.Map`, `strings.Builder`, `bytes.Buffer`, `os.File`)
  are owners and record their allocator.
- Arenas own what is allocated in them, and hand out views that are valid until the arena is reset or
  dropped. The arena drops the values in those views when it takes their memory back (section 18.9, Drop
  entries).
- Memory a kernel or driver makes from an address (device registers, memory a boot loader mapped) is
  owned by nothing in the program and never freed; views of it may be kept anywhere (section 18.3).

**Containers.** A library container owns the storage of its elements and may move them when it grows or
rehashes. Index syntax is lent to containers through methods (section 11.11, Indexing library
containers), and `x[k]` is a copy that `Index` returns, never a variable: `&x[k]` is error E2454 and
assigning to a field or element of `x[k]` is error E2455, since a pointer into storage that moves would
be invalidated by any later call that grows the container, which nothing in the syntax would show.
Containers hand out such pointers from methods named for what they do (`vec.Vec.Ptr(i)`,
`hash.Map.Ptr(k)`), whose documentation says how long the pointer stays valid. A view obtained from a
container in a local variable follows the rule for a local owner's memory (section 18.3): a call that may
grow or rehash the container, such as `v.Push(x)`, ends it at compile time (E3313). One obtained
through a pointer is a heap view, checked by generation checks (section 18.11). Elements that are owners
are taken out with methods that move them (`Pop`, `Remove`), and iterators over a container of owners
yield views of the elements.

The **unchecked layer** is the one exception: `alloc.Raw`, `alloc.New`, `alloc.Free`, and the methods of
an `Allocator` called directly, allocate and free memory that has no owner until the code that called
them gives it one. They are what owner types are written with, and they are unsafe code (section 18.12).

### 18.11 Generation checks

The rules of section 18.3 make sure that no view of a frame, or of a local owner, outlives its memory: a
move, drop, new value, call that may free, or owner field assignment ends such a view (E3313, E3314).
Either they prove it at compile time, or the generation check below catches the case at run time: a view
lent to an interface and freed through a pointer passed in the same call, as in `f(any(b), &b)` where `f`
frees through the pointer, is caught by the tag check when `f` uses the interface. A view of heap memory reached through other heap memory has no such proof: the object may be
freed while the view exists, for example when the vector it points into grows.

```go
first := &ix.words.Slice()[0] // ix is a *Index; words is a vec.Vec[string]
ix.Add("x")                   // may grow words and free the old array
println(*first)               // panic: use of freed memory
```

**The rule.** A use of heap memory through a view after the memory has been freed (a load, a store, or
the release of a count it holds) is a run-time panic, "use of freed memory" (section 17.1, Panics). The
same holds for fixed or Chunked arena memory after the arena has taken it back with `Reset`,
`Release` or `Drop`, or freed it: the panic names the call that
took it back. Freeing memory a second time through the
unchecked layer is a use too.

**How it is checked.** In a checked build, a heap pointer carries a tag that names the generation of its
allocation, and the allocator gives the memory a new generation when it frees it; each access through a
pointer that may point to the heap compares the two. Arenas use an allocation incarnation, represented
by an epoch when runtime checking is required, which ends for the memory the arena takes back.
Static lifetime proof may replace that check when it prevents every stale use. The rule covers all
ordinary heap and arena memory, whatever its
size: large allocations and large arena chunks carry tags or epochs too, with contiguous payload.
A shared runtime metadata directory reaches the right tag table for every interior address, so
checks do not depend on a table being stored at each boundary within the payload. Metadata remains
valid while a check reads it, even after payload memory is returned to its allocator. A child
allocator's views also depend on its backing allocation remaining live. This includes a fixed arena
over frame, static or heap backing: the backing storage's lifetime alone does not keep a reclaimed
arena allocation valid. The escape rules of section 18.3 still apply. The encoding is in abi.md
section 1.3. Ordinary pointers to frames, static
storage and devices carry no tag and are not checked, since the rules of section 18.3 already cover them.
A pointer converted to `uintptr` or passed to a function implemented outside Plutonium loses its tag.
One check covers later accesses through the same pointer until the task does something that could free
the memory or let it see another task's free: a call that may free, or an operation that synchronizes
with another task. A free that races with an access, with no synchronization between them, is a data race
(section 19.3), which no check could catch reliably. The implementation places the checks as sparingly as
that allows, outside loops where it can (abi.md section 1.3).

**Checked and unchecked builds.** Ordinary hosted, freestanding and kernel builds require the same
lifetime guarantee. Where static proof is incomplete, suitable runtime protection is supplied
automatically. If the environment cannot provide it, the unproved operation is a compile error with a
code-level remedy. Kernel mode alone never disables protection. Early-boot and interrupt checks obey
their allocation, waiting and failure constraints, including specified metadata allocation contracts.
An implementation that cannot yet supply the protection for a whole build mode rejects such a build
unless it selects the unchecked mode, and says so; it never builds one unchecked by default (section
21.5).

A build explicitly selecting the documented **unchecked mode** opts out of runtime generation and
epoch protection. A stale access then has no behavior defined by this specification. Static rules
remain in force, and invalid access does not become correct. `//pu:nochecks` is a deliberate unsafe
request for one function, not an ordinary remedy for a failed proof (sections 18.12 and 21.5). A
program that fulfills its lifetime obligations behaves the same in both modes.

**Bounded checks fail loudly.** Reaching the end of a
finite generation or epoch range must retire the memory (never hand it out again) or widen the field,
so that a stale pointer never matches memory that was reused. Heap allocations retire an address range
before its generation wraps. Generation history survives address reuse by another allocator, a different
allocation layout or a new mapping; reuse cannot restart the generation or make a stale view match
payload bytes. Arena incarnation history likewise survives reset, release, arena destruction and
reuse of the backing buffer by another arena or allocator. A finite epoch must not wrap and make an
old view valid again. Hosted and kernel encodings must retire affected storage or widen the identity
before a collision; if no safe further incarnation is available, allocation fails loudly rather
than issuing a matching stale tag (abi.md section 1.3).

In unchecked mode the debug allocator can diagnose some invalid uses while its protection or retained
records permit it; it cannot restore the ordinary lifetime guarantee (section 18.9).

### 18.12 Safe and unsafe code

**Safe code** prevents invalid memory access and data races, whatever its inputs. An attempted
access through an expired view, or an out-of-bounds index, is rejected or checked before invalid memory
is accessed (sections 18.3, 18.4, 18.11 and 11.10). Holding a stale view does not itself access it.
Every required owner cleanup runs exactly once on its specified lifetime-ending or cleanup path
(sections 18.5 and 18.6); nontermination and abrupt-exit exceptions remain. Conflicting accesses, including
those of interrupt handlers (section 19.3), are excluded or use the synchronization required by section 19.3.

This is the default contract for applications and kernels alike. When proof is incomplete the
implementation supplies suitable protection or rejects the operation; ordinary code needs no safety
annotations. A deliberately unchecked build is an explicit opt-out from the full lifetime guarantee,
independent of whether an individual source operation is unsafe (section 21.5). Memory/race safety does
not imply freedom from deadlock or resource exhaustion.

**Unsafe code** is code that uses any of the following:

- package `unsafe`: `unsafe.Pointer` and its conversions to and from `uintptr`, `unsafe.Add`,
  `unsafe.Slice`, `unsafe.String`, `unsafe.SliceData`, `unsafe.StringData`, `unsafe.Drop`,
  `unsafe.Sizeof`, `unsafe.Alignof` and `unsafe.Offsetof` (section 20.1);
- package `pluto/arch/x86`, except `Cli`, `Sti`, `SaveFlagsCli`, `RestoreFlags`, `Pause`, `Cpuid`, `Rdtsc` and `Rdtscp`, and memory made from an integer, such as device registers (section 20.8);
- the unchecked allocation layer: `alloc.Raw`, `alloc.New`, `alloc.Free`, and an `Allocator`'s methods
  called directly (section 18.10);
- `arena.NewUnsafe` and the manual `Arena` methods `New[T]`, `Slice[T]`, `Reset` and `Release`, which
  require caller-managed backing and allocation lifetimes (section 18.9);
- functions without a body (assembly and `//pu:extern`), which are trusted for what they keep (section
  18.3) and for whether they wait (section 20.10), functions marked `//pu:export`, whose pointer
  parameters come from C unchecked (section 20.4), and the directives that state promises the
  compiler takes on trust: `//pu:noescape`, `//pu:resultholds` (standard library only) and
  `//pu:nochecks` (section 20.3);
- `rt.Pinned`, which hides a pointer from the frame rules.

Unsafe code is marked by what it imports and by its directives, with no keyword. A function that wraps
unsafe code behind a safe interface, as `vec.Vec` and `hash.Map` do, is responsible for keeping the
invariants that make its interface safe, including at every check that can panic, since deferred calls
in outer frames may use the object afterwards (section 17.2). Most unsafe code lives in the standard library (allocators,
containers, the runtime); applications should need none.

Manual arena lifetime access belongs to these existing unsafe mechanisms: direct `Allocator`
methods, unsafe pointer construction or conversion, or an explicitly unchecked function. Ordinary
`Fixed` and `Chunked` typed `New[T]`, `Slice[T]`, `Reset` and `Release` are not made unsafe merely because their implementation
uses such mechanisms. Their safe wrapper must keep the static or runtime lifetime guarantee;
raw or unchecked access makes the programmer responsible for avoiding stale use.

**`//pu:nochecks`** on a function removes its bounds checks and generation checks. It is an explicit
request for reviewed low-level use or to trade checks for speed in one function after measuring, and
it makes the function unsafe code. It is not a routine ownership, borrowing or allocation requirement. There is no switch for the compile-time checks of this chapter, which cost nothing at run time.

**Auditing.** `pluto vet -unsafe` lists, for each package, the functions that use unsafe code and the safe
functions that call them, so that a reviewer can find the whole unsafe surface of a program.

**What unsafe code may do.** Unsafe code is checked by the compiler where it can be (the type rules, the
move rules for owners it holds), and is otherwise the programmer's responsibility. A data race in unsafe
code is a bug that gives torn or stale values as the memory model states, never undefined behavior in the
compiler's sense, and the compiler may not assume that unsafe code is free of races (memory-model.md
section 4). A torn pointer or length can then break memory safety.

### 18.13 Examples

**Views passed down.**

```go
func sum(xs []int) int { // xs is a view: sum never frees it
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

func count(words *hash.Map[string, int], w string) {
	n, _ := words.Get(w)
	words.Set(w, n+1) // changes the caller's map through the view
}

func main() {
	nums := [4]int{1, 2, 3, 4}
	fmt.Println(sum(nums[:])) // a view of a local array, passed down

	words := hash.NewMap[string, int](16)
	count(&words, "go") // lends the map; main still owns it
	count(&words, "go")
} // words is dropped here and its table freed
```

**Slicing a buffer.**

```go
func splitHeader(b []byte) (magic, rest []byte) {
	return b[:4], b[4:] // views of the caller's buffer: allowed
}

func readHeader(f *os.File) error {
	var buf [512]byte // lives in readHeader's frame
	n, err := f.Read(buf[:])
	if err != nil {
		return err
	}
	magic, rest := splitHeader(buf[:n])
	return check(magic, rest)
	// return buf[:n] here would be error E3001: buf ends with this frame
}
```

**Returning an owner.**

```go
func render(items []Item) vec.Vec[byte] {
	out := vec.New[byte]() // empty; allocates on its first write
	for i := range items {
		out.Append(items[i].line...)
	}
	return out // moves to the caller: nothing copied, nothing freed
}

func main() {
	out := render(items) // main owns the bytes now
	os.Stdout.Write(out.Slice())
} // and they are freed here
```

**An owner in a struct, and drops on early return.**

```go
type Conn owner struct { // owner: it has owner fields
	file os.File
	in   vec.Vec[byte]
	name string // a counted field: moves with the struct
}

func dial(path string) (Conn, error) {
	f, err := os.Open(path) // f is an os.File, an owner
	if err != nil {
		return Conn{}, err // f is the zero File: dropping it does nothing
	}
	var c Conn
	c.file = f // moves f into the field
	c.name = path
	return c, nil // moves c, and with it the file, to the caller
}

func copyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err // in is dropped here: its file is closed
	}
	var buf [4096]byte
	for {
		n, err := in.Read(buf[:])
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr // out, then in, are dropped here
			}
		}
		if err == io.EOF {
			return out.Close() // Close reports the error; out's Drop then does nothing
		}
		if err != nil {
			return err
		}
	}
}
```

**A use after a move, and `Clone`.**

```go
a := vec.New[int]()
a.Push(1)
b := a    // moves: b owns the vector, a holds nothing
b.Push(2)
a.Push(3) // error E3301: a was moved to b

c := b.Clone() // an explicit copy: c and b own separate arrays
c.Push(4)      // b is unchanged
```

**A singly linked list.**

```go
type Node owner struct {
	val  int
	next owned[Node]
}

type List owner struct{ head owned[Node] }

func (l *List) Push(v int) {
	n := alloc.Owned[Node]()
	n.val, n.next = v, take(&l.head)
	l.head = n // moves the box in; the old head moved into n.next
}
```

The list frees itself: dropping `head` drops each node's `next` in turn. `n.next = l.head` would be error
E3303, whose fix is the `take` above.

**A cache with `shared[T]`.**

```go
type Image owner struct {
	w, h   int
	pixels vec.Vec[uint32]
}

type Cache owner struct {
	images hash.Map[uint64, shared[Image]]
}

func (c *Cache) Get(id uint64) shared[Image] {
	if img, ok := c.images.Get(id); ok {
		return img // a copy out of the map: the cache keeps its own
	}
	img := alloc.Shared[Image]() // count 1
	img.decode(id)               // fills it in through a *Image method
	c.images.Set(id, img)        // a copy into the map: count 2
	return img                   // last use: a move, the count stays 2
}

func show(c *Cache, id uint64) {
	img := c.Get(id)
	draw(&*img) // lends a *Image
} // count -1: freed here if it was evicted meanwhile
```

**A tree with weak parent references.**

```go
type Tree owner struct {
	name     string
	parent   weak[shared[Tree]]     // does not keep the parent alive
	children vec.Vec[shared[Tree]] // keeps the children alive
}

func depth(n shared[Tree]) int {
	d := 0
	for {
		p, ok := n.parent.Upgrade()
		if !ok {
			return d
		}
		n = p // releases the old reference and moves p in
		d++
	}
}
```

When the last reference to the root is dropped, dropping it drops its `children`, whose counts reach zero
in turn, down the tree. With `parent` a `shared[Tree]`, every child would keep its parent alive and the
tree would never be freed.

**An arena.**

```go
func handle(req *Request) error {
	a := arena.NewFixed(16 << 10) // explicit private fixed-capacity backing
	fields := vec.New[Field](&a) // allocates in the arena; holds a view of a
	parseFields(req.body, &fields)
	var reply strings.Builder
	reply.Init(&a)
	for i := 0; i < fields.Len(); i++ {
		f := fields.Ptr(i)
		fmt.Fprintf(&reply, "%s=%s\n", f.key, f.val)
	}
	return send(req, reply.Bytes())
} // reply, then fields, are dropped: their frees go to the arena, which lowers its count and keeps
  // the bytes. Then a is dropped, with a count of zero, and returns its backing.
  // Returning fields would be error E3001: it holds a view of a.
```

**A device register block.** Nothing in the program owns device registers, so a view of them has no
lifetime limit and may live in a package variable. Making it is unsafe code (section 18.12).

```go
type LAPIC struct {
	_   [0xB0]byte
	eoi volatile[uint32]
}

var lapic = (*LAPIC)(unsafe.Pointer(uintptr(0xFFFF8000FEE00000)))
```

The concurrent counterparts of these examples, `synced[T]` with a lock, scoped tasks and channels of
owners, are in chapter 19 (Concurrency).
