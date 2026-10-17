# Design decisions DD1 to DD19: resolved

Each problem was checked against the current specification text before deciding; every one exists as
the triage described it. Section numbers and headings are the specification's. Error codes that exist are
cited by number; new or changed errors are described by meaning, and the catalog assigns numbers.

Goals used below: (G1) easy to use: the compiler works out freeing and proves uses safe, allocation sites
stay visible, ordinary code needs no annotations; (G2) Go-familiar, not Go-compatible: code that looks like
Go and compiles does what Go does, every other difference is visible or a compile error; (G3) kernel code
works like hosted code except for explicit kernel features. Standing principle 1 (doubt resolves to a
compile error) applies throughout.

Summary:

| DD | Decision | Same as triage? |
|---|---|---|
| 1 | Keep the `owner` keyword on declarations; define "annotation" so the claims are true | yes (A) |
| 2 | Compile error, through the existing frame-view analysis; delete the run-time panic | yes (A) |
| 3 | `error` is always sendable; a non-sendable type cannot become an `error`; other interfaces stay whole-program with a precision fix | yes (A), extended |
| 4 | Uniform rule: deferred calls evaluate arguments and receivers at the `defer`; `defer x.Drop()` saves `&x`, so `x` stays usable | yes (B), reframed |
| 5 | `time.AfterFunc` returns a counted handle; dropping it never stops the timer, so the Go statement form works as in Go | no |
| 6 | Accept the elementwise loop with one overlap check per task start | yes (A), no kernel caveat |
| 7 | One rule for `defer` in every kind of loop | yes (B) |
| 8 | `print`/`println` format floats exactly as Go does | no (B) |
| 9 | A type defined from an owner drops as that owner (drop glue), and `x.Drop()` works on every owner | no (B) |
| 10 | `Drop` takes a pointer receiver only | yes (A) |
| 11 | `defer` accepts exactly the built-ins allowed as statements | yes (A) |
| 12 | Only `rune` itself (and slices whose element type is `rune`) converts to a string | yes (B) |
| 13 | Per-CPU variables leave rule 1; compound access and `&v` need a visible non-migrating region | yes (A), refined |
| 14 | Interrupt handlers are parties in the race rules; allocators get no exemption | yes (A), allocators too |
| 15 | The interrupt-context check follows inserted drops | yes (A) |
| 16 | Keep `//pu:blocking` on functions with a body, with its reason written | yes (B) |
| 17 | Bodyless functions are trusted not to wait; the wait check is a liveness check | yes (A), reframed |
| 18 | Conversions keep reserved bits; comparison ignores them | yes (A) |
| 19 | Byte numbering for array-backed packed types; big-endian bit fields may not cross a byte | yes (A), with a crossing rule |

---

## Group 1. Owners and `Drop` (DD1, DD9, DD10, DD4)

These four were decided together. DD1 keeps `owner` as the place a type's kind is written. DD10 makes
`Drop` a pointer-receiver method, which turns DD4 into a consequence of the ordinary defer rule (a deferred
pointer-receiver call saves `&x`), not a special case. DD9 makes a type defined from an owner drop as that
owner, which needs `x.Drop()` to be the language's drop operation on every owner, since such a type has no
`Drop` method of its own.

### DD1. `owner` on structs and generic containers

**Decision.** Keep the keyword (A): a struct with an owner field, and a generic type that stores an
`owner` type parameter, must be declared `owner` (E3202). Change the wording of the "no annotations"
claims so that they say what is true: no lifetime, borrow or region annotations, and no `//pu:` directive
for a fact the compiler can infer; the kind of a type is part of its declaration.

**Why.** G2: a type whose assignment moves instead of copying is the largest behavioral difference from Go,
and the keyword shows it where the type is declared. With inference, changing a field's type in another
package would turn copies into moves in every user, reported as E3301 far from the cause; with the keyword
the error is E3202 at the one declaration. G1 pays one word per type declaration, written once by the
type's author and never at a use; code that uses `vec.Vec` or `os.File` writes nothing extra.

**Spec changes.**
- 1.1 Scope and goals, bullet "Application programmers": "without writing annotations" becomes "without
  writing lifetime, borrow or region annotations".
- 1.2 Design rules, rule 3: in the list of facts the compiler works out, replace "what moves" with "which
  uses of a value move it". Add after the first sentence: the kind of a type is part of its declaration,
  not an annotation; a type whose values own a resource is declared with `owner` (section 7.18) as a
  struct is declared with `struct`, and code that uses such a type writes nothing extra.
- `docs/spec/language.md`, opening description: "without programmer-written borrow annotations" becomes
  "without lifetime or borrow annotations (owner types are declared with `owner`, section 7.18)".
- 18 Ownership and memory, second paragraph: "Ordinary code needs no annotations" becomes "Ordinary code
  needs no lifetime, borrow or region annotations"; add: owner types are declared (section 7.18), and the
  compiler infers everything else in this chapter.
- 7.18 Owner types, bullet "Never inferred for a declared type": add the reason: the keyword makes a type
  whose values move visible where it is declared, so that a change to a field's type elsewhere is reported
  at this declaration (E3202) rather than as move errors at its uses. The E3202 explanation shows the
  declaration with `owner` inserted.

### DD10. `Drop` receiver

**Decision.** Pointer receiver only (A). `func (f File) Drop()` is an error.

**Why.** A value receiver on an owner consumes the receiver, which would then be dropped a second time
(G1 safety, and principle 1). Every example already uses `*T`. It also makes DD4 uniform.

**Spec changes.**
- 7.18 Owner types, bullet "`Drop`": "(on `T` or `*T`, E3201)" becomes "with a pointer receiver `*T`".
  E3201's meaning widens to: `Drop` declared on a type that is not declared `owner`, or declared with a
  value receiver; the explanation shows `func (f *File) Drop()`.
- 10.14 Method declarations, paragraph "The method `Drop`": "with receiver `T` or `*T` (E3201)" becomes
  "with a pointer receiver (E3201)".
- 18.6 Drops, paragraph "The `Drop` method": add that it has a pointer receiver (section 7.18).

### DD9. `type B owner A` where `A` has a `Drop`

**Decision.** Not the triage choice. The value of `B` holds an `A`, and dropping a `B` drops it as an `A`:
`B`'s own `Drop` runs first if `B` declares one, then `A`'s full drop (`A`'s `Drop`, then `A`'s fields).
This is drop glue, not method inheritance: `B` still gets none of `A`'s methods. Since `B` may then have
no `Drop` method, `x.Drop()` on a whole owner variable becomes the language's early-drop operation for
every owner type, whether or not the type declares `Drop`.

**Why.** G1: freeing is worked out by the compiler, and the program is correct with nothing written. The
triage choice (a compile error unless `B` declares `Drop`) leaves the programmer no clean way to write
that `Drop`: calling `A`'s `Drop` through a pointer is E3307, and a conversion out of a place is E3303.
The decision matches two existing rules: an embedded owner field is dropped after the outer `Drop` (7.18,
Embedding), and `type B owned[T]` already keeps the drop of `owned` (10.8). No Go expectation is affected,
since Go has no destructors.

**Spec changes.**
- 7.18 Owner types: add a bullet "Defined from an owner": in `type B owner A`, where `A` is an owner type,
  a value of `B` holds an `A`; dropping it runs `B`'s `Drop` if declared, then drops the value as an `A`
  (its `Drop`, then its fields). `B` has none of `A`'s methods (section 10.8); the drop of `A` is part of
  `B`'s drop glue. Add to the example: `type Log owner File  // dropping a Log closes the file`.
- 7.18 Owner types, bullet "`Drop`": replace "Programs do not call it except as `x.Drop()` on an owner
  variable, which ends the variable's life early" with: `x.Drop()` on a whole owner variable drops it now,
  for every owner type, whether or not the type declares a `Drop` method; it is not a method value and
  does not put `Drop` in the method set.
- 8.7 Values, views, owners and counted values, the sentence defining "dropped": add the case of a type
  defined from an owner.
- 10.8 Type declarations, owner bullet (after "must carry `owner` too: `type B owner A` (E3202)"): add that
  such a `B` drops as an `A` after its own `Drop` (section 7.18). Near the top of 10.8 (the paragraph ending
  "To drop an owner variable early, call its `Drop` method"): change to "call `x.Drop()` (section 18.6)".
- 18.6 Drops, "What dropping does" list: add "a value of a type defined from an owner type: its own `Drop`,
  then the drop of the owner type it was defined from". In "The `Drop` method", state that `x.Drop()` is
  valid on every owner variable, as in 7.18.

### DD4. `defer` with owners

**Decision.** B, written as one rule with no special case. A deferred call evaluates its function value,
arguments and receiver at the `defer` statement, so: an owner passed by value, or as the receiver of a
value-receiver (consuming) method, moves at the `defer` statement; a pointer-receiver method saves `&x`,
and `x` stays usable. `defer x.Drop()` is the pointer-receiver case (DD10): `x` stays usable after it, the
deferred call drops whatever `x` holds when it runs (nothing, if `x` was moved), and `x` is not dropped
again at the end of its scope.

**Why.** G1 and G2: `defer f.Drop()` (or `defer f.Close()` with a pointer receiver) right after the open
is the idiom Go programmers write, and it works. Moves happen where Go evaluates arguments, so nothing is
hidden.

**Spec changes.**
- 12.19 Defer statements, paragraph "Owners": rewrite with the three cases above and this example:
  ```go
  f := open(name)
  defer f.Drop()   // saves &f; f stays usable
  f.Read(buf)      // valid
  defer consume(g) // g moves here
  g.Len()          // error: E3301, g was moved by the defer statement
  ```
  Keep the E3311 sentence (an owner of an inner block).
- 18.5 Moves, list "What moves": the item "passed by value as an argument" adds "including an argument of a
  deferred call, which moves at the `defer` statement"; the item "dropped explicitly with `x.Drop()`" adds
  "(for `defer x.Drop()`, when the deferred call runs; section 18.6)".
- 18.6 Drops, bullet "`defer x.Drop()` drops `x` when the function returns": replace with the rule above
  (usable until the deferred call runs; drops what `x` holds then; not dropped again at scope end). In "The
  `Drop` method", "counts as a move" adds "at the point the call runs".

---

## Group 2. Tasks, closures and timers (DD2, DD5, DD6)

DD2 and DD5 were decided together: the function given to `time.AfterFunc` is checked statically by DD2,
and DD5 changes only what the returned value means. DD6 uses the same chapter.

### DD2. Func values that may be closures, given to `task`, `wg.Task`, `rt.Go`

**Decision.** Compile error (A), with no run-time check. The mechanism already exists: section 18.3 calls
a closure whose record is in a frame a view of that frame, and its whole-program summaries follow views
through parameters and fields. A task statement whose function (or func argument) may be a closure of a
frame is E3004; passing such a closure to a function whose summary starts a task with it is E3003 at the
call that passes it, with the path. E2590 and E2594 stay for the forms the type checker sees directly.

**Why.** G1 and principle 1: the specification promises a compile error in 1.2, 11.5 and chapter 21. G3:
a kernel may have no use for a panic here, and the present run-time check (`rt.checkGoFunc`, abi.md 12.6)
looks only at the starting task's own stack, so it is incomplete anyway. False rejections are limited to
the precision limits 18.3 already publishes; a func value loaded from heap or package memory can never be a
frame closure, since storing one there is E3002.

**Spec changes.**
- 12.12 Task statements, paragraph "Closures": replace from "The compiler rejects a `task` statement whose
  function..." through "...the statement panics when it runs." with: a func value whose closure record may
  be in a frame is a view of that frame (section 18.3). E2590 and E2594 report a capturing literal or
  method value written in the statement, or a local variable whose only assignment is its declaration
  from one. Any other func value (from a parameter, field, element, channel, or a variable assigned more
  than once) is checked by section 18.3: E3004 where it reaches the task start, E3003 at a call that
  passes a closure to a function that starts a task with it. Keep the sentences about `wg.Task`,
  `time.AfterFunc`, `rt.Go` and functions that pass a func parameter on, and about following func
  parameters through bodies.
- 11.5 Closures and their regions, "Uses within a frame": "Starting it with a `task` statement is an error"
  adds "(E2590, E2594, or E3004 where it reaches the statement through other code; section 12.12)".
- 18.3 Views, table under "Frame memory": the E3003 row adds "or starts a task with it"; the E3004 row
  becomes "Passing it to a task statement as an argument, receiver or the function started, or as a func
  argument of a function that starts a task with it (section 12.12)". Add after the table: since a closure
  of a frame can never be stored in heap or package memory (E3002), a func value loaded from such memory
  is never a view of a frame.
- 17.1 Panics, list "Control": delete the bullet "A `task` statement whose function value ... has its
  closure record in the starting task's stack ...".
- 19.4 Scoped tasks, "Which tasks are scoped": "a capturing literal is an error where the compiler can
  tell, and a run-time panic otherwise" becomes "a func value that may be a closure of a frame is a
  compile error (section 12.12)".
- abi.md 12.6 What the compiler generates: delete the sentence on `rt.checkGoFunc`.

### DD5. `time.AfterFunc(d, f)` as a statement

**Decision.** Not the triage choice. `time.AfterFunc` returns a counted handle, `synced[Timer]`, instead
of an `owned[Timer]`. While the timer is armed, the runtime holds a reference of its own, released when
`f` is started or the timer is stopped. Dropping every handle never stops the timer. `time.NewTimer` and
`time.NewTicker` keep `owned[...]`, stopped when dropped. No new error and no new function.

**Why.** G2 in full: the statement `time.AfterFunc(d, cleanup)` then does what it does in Go (Go also keeps
an `AfterFunc` timer alive until it fires), and `t := time.AfterFunc(...)`, `t.Stop()`, `t.Reset(d)` read as
in Go. The triage choice makes the line a compile error and needs a second function for the same idiom.
G1: the allocation is at the visible call, and its lifetime responsibility is counted (section 18.10
allows that). G3: releasing a count never waits. For `NewTimer` and `NewTicker`, stop-on-drop matches what
Go does with an unreachable timer, so they need no change.

**Spec changes.**
- 19.6 Timers, paragraph "Who owns a timer": `time.NewTimer` and `time.NewTicker` return `owned[Timer]` and
  `owned[Ticker]` as now. `time.AfterFunc` returns a `synced[Timer]` handle; while the timer is armed the
  runtime holds a strong reference, released when `f` is started or `Stop` succeeds; dropping handles does
  not stop it, and the timer is freed after the last reference is released.
- 19.6 Timers, paragraph on `time.AfterFunc`: delete "Dropping the `owned[Timer]` it returns before the
  timer fires stops it, so `f` never runs." and add: discarding the result, as in the statement
  `time.AfterFunc(d, f)`, leaves the timer armed; `Stop` through any handle cancels it. Keep the
  capture-free rule (now checked statically by DD2).
- F.7 Time: change the return-type sentence to match.
- A.3 Changed, the timer row: `NewTimer` and `NewTicker` return `owned[Timer]`/`owned[Ticker]` (stopped when
  dropped); `AfterFunc` returns `synced[Timer]`, which keeps running when dropped, as in Go; how it shows:
  a different type, with `Stop` and `Reset` called the same way.
- abi.md 12.9 Timers: state the runtime's reference to an armed `AfterFunc` timer.
- 15.4 Program execution, item 3 (pending timer callbacks canceled at normal termination) is unchanged.

### DD6. Elementwise parallel loop over two slices

**Decision.** Accept it (A). When, in every task of an `s.Task` loop, two views that may overlap are each
accessed only in the same partition form with the same loop variable and parameters (`out[i]` and
`in[i]`), the program checks before each `s.Task` call that the two views either start at the same
element or do not overlap, and panics otherwise. Every other access to two views that may overlap stays
E3402. The check is always performed, in kernel and unchecked builds too, as the existing check that a
variable step is positive already is: it is a race check, not a lifetime check, and section 21.5's
unchecked mode removes only generation and epoch checks.

**Why.** G1 and Rust-level speed: the commonest parallel pattern compiles as written, for one comparison
per task start. Principle 1 allows automatic run-time protection where static proof is incomplete, and 19.4
already panics on a non-positive step, so the kernel exception the triage suggested is not needed.

**Spec changes.**
- 19.4 Scoped tasks, paragraph "Memory reached through views": replace the sentence ending "`out[i] =
  f(in[i])` with `in, out []int` is E3402: the caller could pass overlapping slices" with the rule above
  and an example:
  ```go
  for i := range in {
  	s.Task(func() { out[i] = f(in[i]) }) // checked when each task starts: in and out
  }                                       // start at the same element or do not overlap
  ```
  State that `in[i+1]` with `out[i]` remains E3402, and that the check is not removed by the unchecked mode.
- 17.1 Panics, list "Control": add "Two slices that the tasks of a `sync.Scope` use in the same partition
  form overlap without starting at the same element (section 19.4)".

---

## Group 3. Statements and built-ins (DD7, DD11, DD8, DD12, DD3)

### DD7. `defer` in a loop

**Decision.** One rule for every loop (B). A `defer` inside a loop (a `for` statement, the body of a
range-over-func loop, or a loop made by a backward `goto`) is allowed if every path from it leaves every
loop that contains it before it can reach the `defer` statement again. Leaving means: `return`; a call of
`panic` or of a function that never returns (section 20.3); a `goto` to a label outside the outermost
containing loop; a `break` that ends that outermost loop (an unlabeled `break` when there is one loop, or a
labeled `break` naming it or a statement around it); a labeled `continue` naming a loop outside it. An
unlabeled `continue`, or a `continue` naming a loop that contains the `defer`, does not leave. Otherwise
E2530. E2533 is folded into E2530 and retired. E2534 (a deferred literal in a range-over-func body that
captures the body's variables) stays, because that body is a separate frame.

**Why.** G2: the lock-and-return loop Go programmers write compiles, and changing a slice to a `vec.Vec`
no longer changes whether a loop compiles. The one-slot reason is kept exactly.

**Spec changes.**
- 12.19 Defer statements, "One slot per statement": replace "A `defer` statement inside a loop ... is an
  error (E2530). The body of a range-over-func loop is a loop, and section 12.11 gives the one case that is
  allowed." with the rule above. Add the example from the triage (`for _, it := range items { if it.id ==
  id { it.mu.Lock(); defer it.mu.Unlock(); return it.value } }`) as valid. Keep the `process(name)` example
  as the fix for a `defer` that runs every iteration.
- 12.11 Range over functions, bullet "`defer` in the body": replace the leaving-the-loop sentence and E2533
  with a reference to the rule of 12.19; keep the E2534 sentence.
- A.2 Removed: merge the two `defer` rows into one: "`defer` in a loop where a path can reach the `defer`
  statement again | E2530: each `defer` statement has one slot | call a function that defers, once per
  iteration | 12.19". Keep the E2534 part as its own row.
- Catalog: E2533 retired into E2530 (implementation follow-up).

### DD11. Built-ins after `defer`

**Decision.** A (as in Go): a deferred built-in call is allowed exactly when the call may stand as an
expression statement: `clear`, `close`, `copy`, `delete`, `panic`, `print`, `println`. Deferring `cap`,
`len`, `make`, `max`, `min`, `new`, `take` or a function of package `unsafe` is E2532, whose fix is a
function literal.

**Why.** G2, and the circular reference between chapter 13 and 12.19 disappears.

**Spec changes.**
- 12.19 Defer statements, second paragraph: replace "Calls of built-in functions are restricted as for
  expression statements, and some built-in functions cannot be deferred directly. Wrap the call in a
  function literal and defer that (E2532)." with the rule and list above.
- 13 Built-in functions, introduction: replace "Which of them a `defer` statement accepts is defined in
  section 12.19" with the same list, citing 12.19.

### DD8. `print` and `println` of floating-point values

**Decision.** Not the triage choice: copy Go's format exactly (B). A floating-point argument (a `float32`
is widened to `float64` first) prints as a sign, one digit, a point, six digits, `e`, an exponent sign and
three exponent digits: `println(1.5)` prints `+1.500000e+000`; NaN prints `NaN`, infinities `+Inf` and
`-Inf`, negative zero `-0.000000e+000`. Types: a value whose underlying type is boolean, integer
(including defined integer types and enums), floating-point or string prints as its underlying type; an
owner type cannot be printed even when its underlying type is an integer (write `int32(fd)`), so printing
never raises the question of a move. Kernel-mode floats keep the bit-pattern form.

**Why.** G2 and design rule 4: `println(x)` is common in the small programs kept as Go-oracle tests, and
any other format makes code that looks like Go and compiles print something else. A table row would not
satisfy rule 2, which asks for a difference visible in the source. The cost is less readable float output
from a function the specification itself calls a bootstrapping aid; readable output belongs to `fmt`.

**Spec changes.**
- 13.12 Bootstrapping, table "What they print", float row: replace with the format above, and state the
  digit procedure in words so the digits match exactly: normalize the magnitude to [1, 10) by repeated
  division or multiplication by 10, counting the exponent; add 5e-7 and renormalize if the result reached
  10; then emit seven digits by repeatedly taking the integer part and multiplying the remainder by 10.
  Editors check this against Go's `runtime/print.go` (`printfloat`) before writing it, and credit it in
  appendix G.
- 13.12, same table: the boolean, integer and string rows say "a value whose underlying type is ...", the
  integer row names enums; add "An owner type cannot be printed".
- A.2 Removed: add a row "`print`/`println` of a slice, map, interface, struct or other type Go prints
  in its own form | compile error | `fmt` | 13.12", if the existing error for an unprintable argument is
  not already listed there.
- A.4 Added, kernel facilities row: mention that kernel-mode `print` of a float writes its bit pattern
  (13.12).

### DD12. What counts as a rune in `string(x)`

**Decision.** Only `rune` itself (B). The single-value conversion accepts a value whose type is `rune`
(identical to `int32`, since `rune` is an alias) or an untyped integer or rune constant; the slice
conversion accepts a slice whose element type is `rune`, including a defined slice type such as `type
runes []rune`. A defined type over `int32`, such as `type myRune rune` or `type Fd int32`, is an error in
both forms.

**Why.** G2: `string(Fd(3))` is the false friend appendix A guards against, and a defined type over
`int32` cannot be told apart from one meant as a rune. A Go programmer who writes `string(myRune(x))`
gets a compile error that names `string(rune(x))`.

**Spec changes.**
- 11.24 Conversions, the list of non-constant conversions ("`x` is a value of type `rune`, or a slice of
  runes"): "or a slice whose element type is `rune`".
- 11.24, "Conversions to and from a string type", single-value bullet: after "A value of any other integer
  type is an error", add "including a defined type whose underlying type is `int32`, such as `type myRune
  rune`". Slice bullet: "A slice whose element type is `rune` ...; a slice of any other element type, such
  as `[]myRune`, is an error."
- 11.24 examples: replace `type myRune rune` / `string([]myRune{0x266b, 0x266c})` / `myString([]myRune{...})`
  with `myString([]rune{0x1f30e}) // "\U0001f30e" == "🌎"` and `string([]myRune{0x266b}) // error: the
  element type is myRune, not rune`.
- 8.11 Convertible types, the rune bullet: "a value of type `rune`, or a slice whose element type is
  `rune`".
- A.2 Removed, row "`string(i)` for an integer `i`": "unless `i` has type `rune` (`int32`) or is an untyped
  constant; a defined type over `int32` is an error too".

### DD3. `error` and other interfaces crossing tasks

**Decision.** A, extended. `error`, and every interface that embeds it, is always sendable: converting a
value whose type is not sendable (section 8.8; in practice one that holds a `shared[T]`) to such an
interface is a compile error at the conversion, which names the `shared[T]` and shows `alloc.Synced`. Other
interfaces keep the whole-program rule, with two changes: a conversion whose result section 18.3 proves is
a view of the frame (a lending conversion of a local, such as `fmt.Println(sh)`) does not count, since such
a value can never cross; and the crossing error names a conversion that made the interface non-sendable,
with its package and line.

**Why.** G1: errors cross tasks in ordinary code, and a `chan error` must not break because an unrelated
package converted a `shared` value. Race safety is unchanged (`synced` counts are atomic). The frame-view
exclusion removes the most common far-away cause for `any` (printing a `shared` value) without weakening
anything: E3004/E3005 already stop such a value from crossing.

**Spec changes.**
- 8.8 Sendable types, interface bullet: replace with: `error` and interfaces embedding it are sendable, and
  a value of a non-sendable type cannot be converted to them (new error: a non-sendable type converted to
  `error`); any other interface type is sendable if every type the program converts to it is, not
  counting conversions whose result is a view of the frame that made it (section 18.3); the error at a
  crossing names one such conversion.
- 8.8, owner bullet, after "never on which packages its package imports": add "Interface types other than
  `error` are the one whole-program case (below)."
- 7.9 Interface types, "ordinary interface" bullet: "it counts a `shared[T]` or `synced[T]` reference it
  holds" becomes "it counts a `synced[T]` reference it holds; a `shared[T]` cannot be converted to it
  (section 8.8)".
- 16.7 Allocated errors and ownership: item 2, "counts a `shared` or `synced` reference" becomes "counts a
  `synced` reference"; add to item 6 that a type that is not sendable cannot be converted to `error`;
  item 7 ends "so every `error` value may be sent between tasks".
- 19.2 The sendable rule, the sentence on interface values ("through interface values, for every type the
  program converts to that interface"): add "except `error` (section 8.8)".

---

## Group 4. Packed types (DD18, DD19)

### DD18. Reserved bits

**Decision.** A. Reserved bits are the bits of `_` fields and the bits of the backing no field covers.
Converting an integer or byte array to a packed type keeps every bit, and converting back returns the same
value; writing a field changes only that field's bits. Reserved bits are zero in a literal and in the zero
value. Two packed values are equal when their bits outside the reserved bits are equal, compared bit by bit
(so a floating-point field compares by its bits); hashing for map keys ignores reserved bits too.

**Why.** G3: register read-modify-write must round-trip bits the hardware defines. Ignoring reserved bits
in comparison matches Go's struct rule, which ignores blank fields.

**Spec changes.**
- 7.14 `packed` types: define "reserved bits" under the `_` bullet; rewrite "Conversions" to say every bit
  is kept both ways; "Literals" adds that reserved bits are zero in a literal and the zero value; replace
  "Comparison ... reserved bits are always zero" with the rule above. In the `demo` example, comment that
  the reserved bits of `raw` are returned unchanged.
- 8.10 Comparable types, packed bullet: the same comparison rule, and that a map hashes a packed key
  without its reserved bits.

### DD19. Byte order and bit numbering with an array backing

**Decision.** A, with a crossing rule. For a backing `[N]byte`, `[]byte` or none, bit position `p` is bit
`p mod 8` (0 the least significant) of byte `p / 8`. A bit field occupies consecutive positions, its least
significant bit at the lowest. A byte-level field of `k` bytes is stored in its bytes in the stated order:
`be` most significant byte first, `le` least significant first, neither the target's order. With `be`
(or no order on a big-endian target) a bit field may not cross a byte boundary and a byte-level field must
start on one; the fix is a nested packed field with an integer backing. Integer backings keep today's rule
(positions count from the least significant bit of the value after byte-swapping).

**Why.** G3: protocol headers and on-disk formats are a stated purpose of packed types (7.14's first
sentence). The crossing rule keeps big-endian layouts unambiguous: network fields such as IPv4's
flags-and-fragment-offset are written as a nested `packed be uint16`, which the integer-backing rule
already defines.

**Spec changes.**
- 7.14 `packed` types, bullet "Byte order": split into the integer-backing rule (unchanged) and the
  array-backing rule above. Add the new error: a bit field crosses a byte boundary, or a byte-level field
  is not byte-aligned, in a big-endian packed type with a byte-array, slice or no backing; the explanation
  shows the nested-field fix.
- 7.14, bullet "Fields", byte-level field: "it need not be byte-aligned" adds "except in a big-endian
  array-backed type".
- 7.14 example: check that `IPv4Header` matches (it does: `ihl` is the low nibble of byte 0, `version` the
  high nibble, `length` bytes 2 and 3 most significant first) and add
  ```go
  type Frag packed be uint16 {
  	offset bits[uint16, 13]
  	flags  bits[uint8, 3]
  }
  ```
  used as a field `frag Frag` at bytes 6 and 7 in place of part of the `_ [16]byte`.

---

## Group 5. Kernel: per-CPU data, interrupt handlers, waits (DD13, DD14, DD15, DD16, DD17)

DD13, DD14 and DD15 were decided together around one definition, added to 19.3: an interrupt handler is
a party to the race rules, and two kinds of region are recognized the way held locks are.

- A **non-migrating region** is a stretch of code where, on every path, preemption is disabled
  (`rt.DisablePreemption` to `rt.EnablePreemption`), maskable interrupts are disabled (`x86.Cli` to
  `x86.Sti`, `x86.SaveFlagsCli` to `x86.RestoreFlags`), or an `rt.Lock` is held. Regions are inferred like
  held locks in 19.3: on every path, across calls, with deferred release counting.
- An **interrupt-disabled region** is one where maskable interrupts are disabled by those calls, or an
  `rt.Lock` is held in kernel mode (abi.md 12.5 says runtime locks disable interrupts there), or the code
  is the body of a `//pu:interrupt` handler up to a call that may switch tasks (`rt.Preempt`, `rt.Yield`).

`x86.Cli`, `x86.Sti`, `x86.SaveFlagsCli` and `x86.RestoreFlags` stop being unsafe code: they cannot make
an access invalid, and the analysis follows them. (A wrong pairing can hang a processor, which is liveness,
not safety.) DD16 and DD17 concern the interrupt-context wait check and were decided together: it is a
check against deadlock, complete over Plutonium source, and it trusts bodyless declarations.

### DD13. Per-CPU variables

**Decision.** A, refined. Rule 1 of 19.3 does not apply to `//pu:percpu` variables. A lone load or store
of a whole per-CPU variable of at most a machine word is allowed anywhere (one instruction, atomic with
respect to the same processor, memory model 6.4). Every other access (a compound assignment or `++`, a
read and write in one expression, any access to a variable larger than a word or to part of one, a
pointer-receiver method call, and `&v`) must be inside a non-migrating region; if an interrupt handler may
also access the variable, inside an interrupt-disabled region. A pointer from `&v` is bounded by its
region: it may be kept only in local variables and may not be used after the region ends, sent, or passed
to a task. Two new errors: a per-CPU access outside the region it needs (naming the access, and whether a
handler makes interrupts necessary); a pointer to a per-CPU variable used outside the region it was taken
in.

**Why.** G3 and safety: the kernel's most common idiom (`current`) stays safe code and is checked. A lone
load followed by a lone store in separate statements may act on two processors' copies after a migration;
that is defined behavior, not a race, and 20.9 says so.

**Spec changes.**
- 19.3 Data race freedom, "Rule 1: package variables": add "Per-CPU variables follow section 20.9 instead."
- 20.9 Per-CPU variables: replace the bullet "Code that reads and then writes a per-CPU variable, or uses
  its address, must not migrate ..." with the rule and the two errors above, citing the region definitions
  in 19.3. Add that two lone accesses in separate statements may reach different processors' copies.
  The `var current *Thread` example then compiles; add a short example of `v++` inside
  `rt.DisablePreemption()` / `defer rt.EnablePreemption()`.
- memory-model.md 6.4 Per-CPU data: state the same rule as the checked one.

### DD14. Handlers sharing data with other code; allocators

**Decision.** A, for allocators as well. The definition of a data race in 19.3 covers interrupt handlers:
each handler the program installs is a party that may run at any point where interrupts are enabled, on
any processor, and on several at once. Rules 1 to 3 apply with handlers as parties. Data a handler
accesses (other than per-CPU variables, DD13) must be atomic or guarded by an `rt.Lock`, which the guard
inference recognizes like `sync.Mutex` (a `sync.Mutex` cannot be taken in a handler, E2584).
`volatile[T]` alone, and an interrupt-disabled region alone, exclude only the same processor, so they
satisfy the rules only for per-CPU variables. Errors are E3502 and E3503, naming the handler as the party.
Allocators get no exemption: delete "or no lock"; an allocator's state is data like any other, and if a
handler can reach its `Alloc` or `Free` the state they share with other code must meet these rules (for
example per-CPU caches touched only in interrupt-disabled regions). Parts of an allocator written in unsafe
code are its own responsibility, as 18.12 already says of all unsafe code.

**Why.** Memory and race safety (18.12 promises safe code has no races) and G3. The triage accepted
treating allocators as trusted; that is unnecessary, because an allocator written in safe code is checked
by the same rule, and one written with unsafe code is already outside the check.

**Spec changes.**
- 19.3 Data race freedom, first paragraph: "two accesses ... by different tasks" becomes "by different
  tasks or interrupt handlers"; add the region definitions above and a paragraph "Interrupt handlers" with
  the party rule and the guards allowed. "Accesses and locks": `rt.Lock` is a lock.
- 20.10 Interrupts: replace "Inference checks supported protocols without routine annotations; opaque
  assembly implementations retain their explicit trusted contracts." and the paragraph "Sharing with a
  same-processor handler ..." with a reference to 19.3 and 20.9 naming the accepted forms. In the
  "Allocation" bullet, delete "or no lock" and add the allocator sentence above.
- 20.11 Allocation in kernel mode, bullet "In interrupt context": add that an allocator's shared state
  follows section 19.3.
- 18.12 Safe and unsafe code: in the safe-code paragraph, "Conflicting accesses are excluded" adds
  "including those of interrupt handlers (section 19.3)"; in the unsafe list, `pluto/arch/x86` adds
  "except `Cli`, `Sti`, `SaveFlagsCli` and `RestoreFlags`".
- F.5 Synchronization: `rt.Lock` is a lock the race analysis follows, usable in interrupt context.
- memory-model.md 4.1 Safe code is free of data races: include handlers. 6.1 Interrupt handlers on the same
  processor: replace "The compiler checks supported source protocols without routine annotations" with
  the checked forms (atomics or `rt.Lock` for shared data; volatile or interrupt-disabled regions only for
  per-CPU data).

### DD15. Inserted drops in the interrupt-context check

**Decision.** A. The check of 20.10 follows the drops the compiler inserts like any call: at scope end, on
assignment over an owner, for temporaries, and the release of a counted value whose count reaches zero,
into the type's `Drop` and drop glue and into the `Free` of every allocator the program installs. It is an
error (E2584) only where one of these may wait.

**Why.** Principle 1, and the check's result no longer depends on whether cleanup is written or inserted.
20.10 already names "an allocator whose `Alloc` or `Free` may wait", which only makes sense if drops are
followed.

**Spec changes.**
- 20.10 Interrupts, bullet "What is followed": add the inserted drops and releases, citing 18.6.
- 20.11 Allocation in kernel mode: replace "Copying and dropping a counted string made elsewhere is allowed,
  and a drop may free a buffer: a kernel that cannot free in some context keeps the strings it uses there
  static, or made in an arena." with: copying a counted string never waits; dropping one may free its
  buffer, and the drop is followed like a call (section 20.10), an error only if the `Free` of an
  installed allocator may wait; a kernel whose allocator may wait keeps the strings it drops there static
  or in an arena.

### DD16. `//pu:blocking` on a function with a body

**Decision.** Keep it (B). On a function with a body it states a wait the compiler cannot see, such as a
spin until an interrupt handler or another processor sets a flag. It only adds the restriction of 20.10
and never makes a program accepted, so it is not unsafe code and not a trusted promise.

**Why.** Rule 3 allows directives for what the compiler cannot see; who will set a flag is such a fact.
Retiring it would leave such spins unmarkable.

**Spec changes.**
- 1.2 Design rules, rule 3, the sentence "Directives state only what the compiler cannot see": add "waits
  on progress that only another execution context makes".
- 20.3 Directives, table "Kernel and binding", `//pu:blocking` row: keep "with or without a body"; add the
  body case and that it only adds a restriction.
- 20.10 Interrupts, bullet "`//pu:blocking`": add the same reason.

### DD17. Bodyless functions and the wait check

**Decision.** A, reframed. The interrupt-context wait check is a check against deadlock, not a safety
analysis: principle 1's final sentence already says safety does not promise freedom from deadlock. It
follows all Plutonium source (including inserted drops, DD15) and takes a bodyless function's declaration
as its contract: not waiting unless marked `//pu:blocking`. Bodyless functions are already unsafe code
(18.12) and listed by `pluto vet -unsafe`.

**Why.** G1 and G3: no new directive for ordinary kernel assembly. Writing the check's scope in 1.2 and
chapter 21 removes the contradiction without calling it an exception to the safety rule.

**Spec changes.**
- 1.2 Design rules, principle 1: add at the end: the check that interrupt context never waits (section
  20.10) guards against deadlock; it follows all Plutonium source and takes the declaration of a function
  without a body as that function's contract.
- 20.10 Interrupts, bullet "Assembly and C": add that this is a contract taken on trust because a function
  without a body is unsafe code (section 18.12).
- 21 Implementation requirements, introduction, third bullet: the parenthesized list keeps "waits in
  interrupt context" and adds "(which takes the declarations of functions without a body on trust,
  section 20.10)".
- 18.12 Safe and unsafe code, bullet on functions without a body: add that such a function is trusted for
  what it keeps (18.3) and whether it waits (20.10).

---

## Editor assignment by file

| File | DDs | Sections |
|---|---|---|
| `language.md`, 01-introduction | 1, 16, 17 | intro; 1.1; 1.2 rule 3 and principle 1 |
| 07-types | 1, 3, 9, 10, 18, 19 | 7.9, 7.14, 7.18 |
| 08-properties-of-types-and-values | 3, 9, 12, 18 | 8.7, 8.8, 8.10, 8.11 |
| 10-declarations-and-scope | 9, 10 | 10.8, 10.14 |
| 11-expressions | 2, 12 | 11.5, 11.24 |
| 12-statements | 2, 4, 7, 11 | 12.11, 12.12, 12.19 |
| 13-built-in-functions | 8, 11 | 13 introduction, 13.12 |
| 16-errors, 17-run-time-panics | 2, 3, 6 | 16.7, 17.1 |
| 18-ownership-and-memory | 1, 2, 4, 9, 10, 14, 17 | 18 intro, 18.3, 18.5, 18.6, 18.12 |
| 19-concurrency | 2, 3, 5, 6, 13, 14 | 19.2, 19.3, 19.4, 19.6 |
| 20-system-considerations | 13, 14, 15, 16, 17 | 20.3, 20.9, 20.10, 20.11 |
| 21-implementation-requirements | 17 | introduction |
| a-differences-from-go, f-library-names | 5, 7, 8, 12, 14 | A.2, A.3, A.4; F.5, F.7 |
| abi.md, memory-model.md | 2, 5, 13, 14 | abi 12.6, 12.9; memory model 4.1, 6.1, 6.4 |

Appendix A row positions are moving while the mechanical edits land; editors locate rows by content.

## Needs the owner

1. DD8: Go's float format (`+1.500000e+000`) in exchange for exact agreement with Go and the oracle. If
   readable output matters more, the fallback is a compile error for float arguments that names `fmt`
   (triage option C), not a table row.
2. DD5: the handle type for `AfterFunc`. `synced[Timer]` is chosen; a dedicated named handle type would
   read better in signatures but adds a library name.
3. DD13/DD14: handler bodies are treated as interrupt-disabled regions, which assumes handlers are installed
   through interrupt gates. Exceptions and NMIs that fire inside a handler stay outside the check, as a
   kernel contract (memory model 6.1 already says they need their own protocol). The owner should confirm
   that this contract is acceptable or ask for a stricter rule.
4. DD13: lone word-sized loads and stores of a per-CPU variable are accepted outside any region, so a load
   and a later store in separate statements may reach two processors' copies. That is defined behavior,
   not a race, but the owner may want such pairs rejected as well.
