## 18. Ownership and memory

Plutonium has no garbage collector. Every piece of memory a program uses is released by something the
program names: the frame of the function that declares it, an owner that is dropped, the last of the
counted references that share it, or an arena that is reset. This chapter defines how memory is
classified, where variables live, how long a view of memory may be used, how owners move and are
dropped, how shared ownership is counted, where allocations come from, which uses of memory are checked
at run time, and which code the compiler does not check.

The rules are inferred from the program. Ordinary code needs no annotations and no knowledge of how the
compiler checks them; each rule that rejects a program does so with an error code (chapter 21,
Implementation requirements) whose explanation names the variables and lines involved and shows a fix.
Following section 1.2 (Design rules), every analysis in this chapter rejects what it cannot prove
safe, and whether a program is accepted never depends on optimization (section 21.3, Acceptance does not
depend on optimization).

### 18.1 Values, views and owners

Every type has one of four **kinds**, which decide what assignment, passing, returning and the end of a
variable's scope do to its values. Section 8.7 (Values, views, owners and counted values) gives the kind
of each type; in short:

- A **value** is only bits: booleans, numeric types, enums, packed types, function values that capture
  nothing, and arrays, structs and unions made only of values. Assignment copies it, and nothing happens
  when it goes out of scope.
- A **view** looks at memory that something else owns: a slice `[]T`, a pointer `*T`, `unsafe.Pointer`,
  a closure or method value (section 11.5, Closures and their regions), an interface that holds a copy in
  a frame or lends an owner (section 7.9, Interface types), a string made with `unsafe.String`, and any
  struct or array that contains a view and no owner. A view never owns or frees anything. Copying and
  passing one costs only the words copied. A view may not outlive the memory it looks at (section 18.3).
- An **owner** holds memory or another resource and releases it when it is dropped. A type is an owner
  only if its declaration says so with `owner` (section 7.18, Owner types), or if it is built from owner
  types in a way that section 7.18 lists (an array of owners, `owned[T]`, an owning interface, an owner
  union). Assigning, passing or returning an owner **moves** it (section 18.5); a copy is made only by an
  explicit call such as `Clone`. An owner is **dropped** at the end of its variable's scope unless it was
  moved away (section 18.6).
- A **counted value** shares memory among its copies and counts them: `string`, `map[K]V`, `chan T`,
  `shared[T]`, `synced[T]` and `weak[P]`, and any struct or array that contains one and no owner.
  Assignment copies it and adds one to the count; dropping a copy subtracts one; the last copy releases
  the memory (sections 18.7 and 18.8).

| To do this with memory | Use | Cost |
|---|---|---|
| let a function use it for a while | a view | none |
| give it to exactly one new owner | a move | none |
| let several places keep it | `shared[T]`, `synced[T]`, a map or a channel | a count update per copy |
| keep text | `string` | a count update per stored copy of a string made at run time; none for literals |

**Why three kinds and not one.** With no garbage collector, nothing frees memory that the program has
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
  that returns a closure builds it in a **region** of its caller's frame (section 11.5). The frame is
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
| Passing it to a function that does either of these with that parameter, or with something reached from it | E3003 |
| Passing it to a task statement, as an argument or receiver (section 12.12, Task statements) | E3004 |
| Sending it on a channel | E3005 |
| Keeping a view of a loop iteration's storage where the next iteration can see it (section 18.4) | E3006 |

Where the type checker sees a view of the frame passed to a task statement or sent on a channel
directly, it reports E2011 instead of E3004 or E3005. A closure that a call returns is a view of the
caller's frame, built in the region the caller provides (section 11.5): returning it again is allowed,
since the caller's caller then provides the region, and the closure-specific errors E3100 to E3102 are
defined there.

**A local owner's memory.** A view obtained from an owner held in a local variable (`v.Slice()`,
`b.Bytes()`, `&m`, `p.field` through an `owned[T]`, `&*p`) is bounded by that variable. It follows the
frame rules above, and in addition it may not be used after the variable has been moved, dropped or given
a new value in the same function (E3313). A view of a temporary owner (`makeVec().Slice()`) may not outlive the
statement that made the temporary (section 18.6). A view obtained through a counted reference held in a
local variable is bounded by that reference in the same way.

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

- The check covers the whole program and needs no annotations. Each function has a summary of which
  parameters it keeps, which it stores into memory another parameter points to, and which it returns,
  field by field. A call is checked against its callee's summary. A call through an interface is checked
  against every method of that name on a type the program converts to an interface, narrowed by type
  assertions on the way and by the dynamic type where the caller knows it. A call through a func value is
  checked against every function of that type whose value the program takes, narrowed to the closures of
  the current function where only those can be there.
- Summaries follow pointers up to three loads deep and eight field steps; beyond that, everything
  reachable is assumed kept.
- A local variable whose address is never taken is followed along the control flow, so reusing a
  variable (`err` above all) for a view of the frame and later for heap memory is accepted. Other
  storage is followed field by field, two levels deep, without regard to the order of statements.
- A generic function is checked once for all its instantiations. A conversion of a value of a type
  parameter to an interface counts as a frame copy if any instantiation is larger than a word.
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
makes, and for the records of the closures and method values the body builds. A view of that memory (a
closure that uses one of those variables, `&x`, a slice of a local array declared in the body, an
interface holding a copy made in the body) is therefore valid for one iteration.

- Using such a view within the iteration (calling the closure, passing the pointer down, storing it in a
  variable declared in the body) is allowed and behaves as if each iteration had variables of its own.
- Keeping it where the next iteration can see it is error E3006: in a variable, element or field
  declared outside the loop body or in the loop's header, or anywhere a view of the frame may not go at
  all (section 18.3).
- A local variable whose address is never taken is checked where the next iteration starts, so keeping
  a view in it and then leaving the loop with `break` or `return` is allowed.
- A closure that uses only variables declared outside the loop refers to the same addresses on every
  iteration and may be kept.
- Copying the variable into one declared in the body (`i := i`) does not help, since that copy is reused
  too. Call the closure within the iteration, store values instead of closures or pointers, or pass the
  value to the closure as a parameter when it is called.

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

The rule means that every loop that compiles behaves as if each iteration had variables of its own: no
program can observe that the storage is shared. A closure returned by a call in a loop is covered by the region rule of section
11.5 (E3101). Tasks started by a `sync.Scope` in a loop take their own copies of the iteration's
variables instead (section 19.4, Scoped tasks).

### 18.5 Moves

**What moves.** A value of an owner type is moved, not copied, when it is

- assigned to a variable or other place (`n := m`, `n = m`, `c.buf = b`, `s[i] = b`, `*p = b`);
- passed by value as an argument, including as the receiver of a method with a value receiver;
- returned, including as a named result by a bare `return`;
- sent on a channel (section 12.5, Send statements), or passed to a task statement (section 12.12, Task
  statements);
- stored in a composite literal (`Conn{buf: b}`);
- converted to an owning interface (section 7.9);
- given to `new(v)` (section 13.9, Allocation);
- dropped explicitly with `x.Drop()` (section 18.6).

A move copies the owner's bytes to the destination and clears the source variable to the zero value of
its type. The zero value of an owner holds nothing, and dropping it does nothing, so the cleared source
needs no separate flag. The compiler tracks that the source holds no value and rejects every later use of
it until the variable is given a new value.

**What does not move.** These read an owner and leave it where it is:

- taking its address (`&b`), calling a method with a pointer receiver (`b.Write(p)`, which passes `&b`),
  and selecting a field to read it;
- converting it to an ordinary interface such as `any` or `error`, which lends it: the interface is a
  view of the variable (section 7.9);
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
comma-ok form only where `ok` is true. A type switch with a binding on an owning interface moves it into
the binding in every clause (section 11.13, Type assertions).

**Conversions.** Converting a value into an owner type creates a new owner, and is allowed only in the
package that declares the owner type, where its invariants are kept (E3212). Converting between two owner
types with the same underlying type moves. Converting an owner to a non-owner type reads it (above).

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
  `sync.Scope` is the exception: the Scopes of a block are dropped before its other variables (section
  19.4).
- Parameters are dropped when the function returns, after its local variables. Results are moved to the
  caller and not dropped.
- A variable or place the compiler knows holds nothing (moved away on every path, never given a value,
  a part a composite literal or `new(T)` left zero, a place emptied by `take`) is not dropped, and an
  assignment to it does not drop its old value. Where a variable may have been moved on some paths only,
  it is dropped holding the zero value.
- Leaving several scopes at once drops what each holds, innermost first. A `goto` back to a label before
  a variable's declaration drops the variable first, since its declaration runs again.
- Deferred calls run before the function's own variables are dropped, so a deferred call may use them.
  A deferred call may not use an owner declared in an inner block, which is dropped when that block ends
  (E3311).
- `defer x.Drop()` drops `x` when the function returns, and `x` is not dropped again.
- `rt.Goexit` leaves every frame of the task through its return path, so each frame runs its deferred
  calls and drops what it holds (section 17.3, Ending a task with `rt.Goexit`).
- A panic drops nothing. In hosted builds the panicking task's deferred calls run first (section 17.2,
  Deferred calls on a panic), but no owner is dropped and no frame is reclaimed. `os.Exit` drops nothing.
- Package-level owners are never dropped, not even when `main` returns: the program's exit releases
  their memory and descriptors. A program that must flush a package-level writer does it in `main`, with
  `defer` (section 15.4, Program execution). The debug allocator reports memory still held by package
  variables apart from leaks.

**Assigning to a place that holds an owner** evaluates the new value, drops the old one, then moves the
new one in: `c.buf = b` frees the buffer `c.buf` held before. The same holds through a view: `*p = v` and
`s[i] = v` drop the old contents. `clear(s)` on a slice or array of owners assigns the zero value to each
element and so drops each in index order (section 13.3, Clear).

**Temporaries.** An owner that an expression produces and nothing stores is a **temporary**. It is
dropped at the end of the statement that made it, so a view of it may be passed to a call in that
statement: `os.Stdout.Write(readAll(f).Slice())` is valid. A temporary in the header of an `if`, `switch`
or `for ... range` statement lives until the whole statement ends; one in the condition or post
statement of a three-clause `for` is dropped after each evaluation.

**What dropping does.** Dropping an owner runs its `Drop` method if it has one, then drops its fields in
declaration order and its elements in index order. Fields that are values or views need nothing; counted
fields release their references. The compiler-generated part of this, which releases fields and
elements, is the type's **drop glue**; values that are not owners but contain counted values have drop
glue too (section 18.8). In particular:

- an `owned[T]` drops its `T` and frees the memory to the allocator recorded in its header (section
  18.7);
- an owning interface drops the value it holds through its dynamic type's drop code, whatever that type
  is (section 7.9);
- an owner union drops the payload of the variant it holds (section 7.13, Unions);
- an array of owners drops each element.

**The `Drop` method.** A method named `Drop` must have no parameters and no results (E3206) and may be
declared only on an owner type (E3201). It is called by the compiler; the program calls it only as
`x.Drop()` on a whole owner variable, which drops `x` completely (its `Drop` method, then its fields),
counts as a move, and leaves `x` not dropped again. Calling `Drop` any other way (through a pointer, a
method value or an ordinary interface) is error E3307, since the real owner would drop the value a second
time. `Drop` must treat the zero value as empty, since a moved-from variable, the result of `take`, a
variable that was never filled and memory from every allocator all hold the zero value. Since a
value known to hold nothing is not dropped (above), a variable never given anything but the zero value
is never dropped: an owner type whose `Drop` must run has to hold a value. `Drop` cannot
report failure: a type whose release can fail provides a method such as `Close() error` for callers that
care, after which `Drop` has nothing left to do.

**Memory the compiler does not drop.** Values in memory from `alloc.New` or from an arena's `New` and
`Slice` are not dropped by the compiler: that memory has no owner until the code that allocated it gives
it one. `alloc.Free(p)` drops `*p` before freeing it, and code that manages memory itself (an allocator,
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
   costs nothing. Passing a reference down to a call borrows it: the callee counts only the copies it
   keeps.
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
new value and releases the old one, a parameter is borrowed, and a result hands its reference to the
caller.

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
string's bytes while a copy can still reach them.

**Buffers and counts.**

- A string made at run time (by `+` and `+=`, `string(runes)`, `string(r)` for a rune that is not a
  constant, `strings.FromUTF8`, `fmt.Sprintf`, `strconv.Itoa`, `strings.Builder` and other library
  functions) has a **buffer**: its bytes, allocated from the current allocator (section 18.9) behind a
  header that records the allocator, the size and a count of the strings that use it. A buffer never
  changes once a string uses it.
- Copying a string adds one to the count. Dropping a copy subtracts one, and the last drop frees the
  buffer to the allocator recorded in its header. The count is plain or atomic by the rule of section 18.7
  (Shared ownership, When counts are atomic).
- A substring `s[i:j]` shares `s`'s buffer and keeps it alive.
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

- at the end of a variable's scope, on every path out of it, and at the end of each iteration for a
  variable declared in a loop body;
- when a place is overwritten: the new value takes its references first, then the old one is released,
  so `s = s[1:]` and `a, b = b, a` are safe;
- for a temporary, at the end of the statement that made it; in the condition or post statement of a
  three-clause `for`, after each evaluation;
- for storage that belongs to the frame but outlives a statement (an interface copy, the object of
  `&T{...}`, a slice literal's array, a method value's receiver, a deferred call's saved arguments, the
  region a call builds its closures in), when the function returns, after its deferred calls, or when the
  same expression runs again and refills it.

A panic and `os.Exit` release nothing, as for owners. A `[]string` is a view: its elements belong to the
array it looks at, and whoever owns that array releases them. Library containers such as `vec.Vec[string]`
release their elements when they are cleared, truncated or dropped. `alloc.Free(p)` releases the strings
in `*p` before freeing it.

**Passing and returning.** A string parameter is borrowed: the caller keeps the string alive for the
call, and the callee counts only the copies it keeps. A result is handed to the caller with one
reference.

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
(section 18.9). Declare the arena before the strings that use it, as for any owner that views it.

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
uses the current allocator panics with "no allocator" until the program calls `alloc.SetDefault`, and a
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
to a task statement is a frame-view error that names the arena. The window runs from the call of
`alloc.Use` to the end of its block, or to the end of the function for `defer alloc.Use(&a).Restore()`.
It covers `new` and `make` without an allocator argument, the operations that make strings, calls that
leave out a defaulted `alloc.Allocator` parameter (such as `vec.New[T]()` or `fmt.Sprintf`), and calls
whose results keep their allocator out of sight (a string, a box, an interface such as an error):

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
dropped. Freeing into an arena does nothing, so the same owner code works in an arena and on the general
heap: an owner allocated in an arena is dropped as usual, and its free costs nothing. The library provides
`arena.Fixed`, a fixed-capacity owner of private backing from its parent allocator, and `arena.Chunked`,
an owner that takes more chunks from a parent allocator as needed. The ordinary fixed constructor is
`arena.NewFixed(capacity uintptr, parent alloc.Allocator = alloc.Default()) Fixed`. It explicitly
allocates its private backing, never grows or relocates its payload, and returns backing to the parent
on drop. Its parent remains a lifetime dependency. `Fixed` moves rather than copies; borrowed pointer
aliases share one allocator identity and cursor. The constructor panics on unavailable backing or
invalid capacity rather than returning unchecked storage. Zero capacity gives an empty fixed arena.
Payload capacity excludes allocation bookkeeping. Each successful allocation additionally obtains
one bookkeeping record from the same parent, in checked and unchecked modes; reclaiming that allocation
returns its record, and dropping the arena also returns its payload. An allocation that cannot obtain
bookkeeping fails without advancing the cursor. Ordinary reads and writes allocate nothing. Stable
check-reader and address-history metadata uses the runtime directory storage described in abi.md
section 1.3; it is not payload or a hidden change of the arena's chosen parent.
An arena also hands out memory as
plain views (`a.New[T]()` returns a `*T`, `a.Slice[T](n)` a `[]T`) that are valid until the arena is
reset or dropped; the compiler does not drop the values in them. A string or counted box made in an arena
between `m := a.Mark()` and `a.Release(m)` must be dropped before the `Release`, since dropping it writes
its count.

Ordinary typed allocation, `Reset`, `Release` and drop must preserve this lifetime guarantee.
Marks identify their arena and the recorded allocation position. A foreign mark or one invalidated
by reclamation cannot reclaim unrelated current storage; it panics. Releasing a later mark does not
invalidate an earlier still-live mark.
`Release` invalidates only allocations it reclaims; earlier allocations remain valid. A fixed
manual `Arena` constructed over a caller-provided buffer continues to use that buffer; it must not
silently replace it with an allocated buffer or overwrite payload with metadata. The
raw allocation
layer and unsafe pointer operations remain available to code that deliberately manages lifetimes
itself (section 18.12); their obligations do not become an unstated precondition of an ordinary API.

`arena.NewUnsafe(buf []byte) Arena` is the explicit borrowed-buffer constructor. It allocates no
private payload and does not free the caller's buffer. Its copyable `Arena` representation is a manual
allocator: the caller must keep the backing alive and exclusively available, avoid overlapping cursor
copies, and end all views and count obligations before taking their memory back. It has no ordinary
safe-lifetime guarantee. There is no `arena.New(buf)` constructor that hides this obligation.

**The debug allocator.** `pluto/alloc/debug` catches overruns, uses after free and double frees
deterministically and reports leaks; it can be the default allocator of a hosted program without code
changes. It is described in [debug-allocator.md](../debug-allocator.md).

### 18.10 No allocation without an owner or an arena

Every allocation belongs to an owner, to an arena, or to the counted references that share it. There is
no memory that nothing frees:

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
  dropped.
- Memory a kernel or driver makes from an address (device registers, memory a boot loader mapped) is
  owned by nothing in the program and never freed; views of it may be kept anywhere (section 18.3).

**Containers.** A library container owns the storage of its elements and may move them when it grows or
rehashes. Index syntax is lent to containers through methods (section 11.11, Indexing library
containers), and `x[k]` is a copy that `Index` returns, never a variable: `&x[k]` is error E2454 and
assigning to a field or element of `x[k]` is error E2455, since a pointer into storage that moves would
be invalidated by any later call that grows the container, which nothing in the syntax would show.
Containers hand out such pointers from methods named for what they do (`vec.Vec.Ptr(i)`,
`hash.Map.Ptr(k)`), whose documentation says how long the pointer stays valid. A view obtained from a
container in a local variable follows the rule for a local owner's memory (section 18.3); one obtained
through a pointer is a heap view, checked by generation checks (section 18.11). Elements that are owners
are taken out with methods that move them (`Pop`, `Remove`), and iterators over a container of owners
yield views of the elements.

The **unchecked layer** is the one exception: `alloc.Raw`, `alloc.New`, `alloc.Free`, and the methods of
an `Allocator` called directly, allocate and free memory that has no owner until the code that called
them gives it one. They are what owner types are written with, and they are unsafe code (section 18.12).

### 18.11 Generation checks

The rules of section 18.3 prove at compile time that no view of a frame, or of a local owner, outlives
its memory. A view of heap memory reached through other heap memory has no such proof: the object may be
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

**Checked and unchecked builds.** Hosted builds are checked by default. Freestanding and kernel builds
are unchecked until a tag encoding that fits kernels exists, since a kernel uses the pointer bits the
hosted encoding takes. A build without the checks is the documented **unchecked mode**: a use of freed
memory is then not detected, and what it does is not defined by this specification. The implementation
lets each build choose the mode (section 21.5, Run-time checks and the unchecked mode), and
`//pu:nochecks` removes the checks from one function (section 18.12). The checks change only what a bug does: a program that never uses freed memory
behaves the same in both modes.

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

In the unchecked mode, the debug allocator (section 18.9) narrows the gap by catching uses of memory
returned to it.

### 18.12 Safe and unsafe code

**Safe code** is code for which the compiler guarantees that nothing it does can corrupt memory or
race, whatever its inputs: no view outlives its memory (sections 18.3, 18.4 and 18.11), every owner is
dropped exactly once (sections 18.5 and 18.6), every index is within bounds (section 11.10, Index
expressions), and no two tasks access the same memory at once with one of them writing, unless both use
atomic operations (section 19.3).

**Unsafe code** is code that uses any of the following:

- package `unsafe`: `unsafe.Pointer` and its conversions to and from `uintptr`, `unsafe.Add`,
  `unsafe.Slice`, `unsafe.String`, `unsafe.SliceData`, `unsafe.StringData`, `unsafe.Drop`,
  `unsafe.Sizeof`, `unsafe.Alignof` and `unsafe.Offsetof` (section 20.1);
- package `pluto/arch/x86`, and memory made from an integer, such as device registers (section 20.8);
- the unchecked allocation layer: `alloc.Raw`, `alloc.New`, `alloc.Free`, and an `Allocator`'s methods
  called directly (section 18.10);
- `arena.NewUnsafe` and the manual `Arena` methods `New[T]`, `Slice[T]`, `Reset` and `Release`, which
  require caller-managed backing and allocation lifetimes (section 18.9);
- functions without a body (assembly and `//pu:extern`), and the directives that state promises the
  compiler takes on trust: `//pu:noescape`, `//pu:resultholds` (standard library only) and
  `//pu:nochecks` (section 20.3);
- `rt.Pinned`, which hides a pointer from the frame rules.

Unsafe code is marked by what it imports and by its directives, with no keyword. A function that wraps
unsafe code behind a safe interface, as `vec.Vec` and `hash.Map` do, is responsible for keeping the
invariants that make its interface safe. Most unsafe code lives in the standard library (allocators,
containers, the runtime); applications should need none.

Manual arena lifetime access belongs to these existing unsafe mechanisms: direct `Allocator`
methods, unsafe pointer construction or conversion, or an explicitly unchecked function. Ordinary
ordinary `Fixed` and `Chunked` typed `New[T]`, `Slice[T]`, `Reset` and `Release` are not made unsafe merely because their implementation
uses such mechanisms. Their safe wrapper must keep the static or runtime lifetime guarantee;
raw or unchecked access makes the programmer responsible for avoiding stale use.

**`//pu:nochecks`** on a function removes its bounds checks and generation checks. It is an explicit
request to trade checks for speed in one function after measuring, and it makes the function unsafe
code. There is no switch for the compile-time checks of this chapter, which cost nothing at run time.

**Auditing.** `pluto vet -unsafe` lists, for each package, the functions that use unsafe code and the safe
functions that call them, so that a reviewer can find the whole unsafe surface of a program.

**What unsafe code may do.** Unsafe code is checked by the compiler where it can be (the type rules, the
move rules for owners it holds), and is otherwise the programmer's responsibility. A data race in unsafe
code is a bug with the limited consequences the memory model states, never undefined behavior of the
whole program, and the compiler may not assume that unsafe code is free of races (memory-model.md
section 4).

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
	name string // a counted field: copied with the struct
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
} // reply, then fields, are dropped: their frees go to the arena and do nothing.
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
