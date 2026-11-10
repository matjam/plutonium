# Specification rulings, 2026-11-09

**Status:** applied to the specification, version 0.4 (draft), 2026-11-09. This is phase 0.1 and 0.2 of the
[completion plan](../plans/2026-11-09-completion-plan.md).

**Inputs:** the audits [A](2026-11-09-spec-audit-a.md) (A1 to A8), [B](2026-11-09-spec-audit-b.md) (B1 to B22)
and [C](2026-11-09-spec-audit-c.md) (C1 to C14), and later [D](2026-11-09-spec-audit-d.md) (D1 to D14 and three
unverified candidates). The user decided A1, C1, C2, B1 (option a), B3, D1, D2 (option a) and D3 (option a). The
coordinator decided the others within the project goals. Decisions D42 onward in
[decisions.md](../decisions.md) record the rulings that add a rule a program can observe. Roadmap items 146
onward in [roadmap.md](../roadmap.md) hold the compiler work.

Goals named in the table: **Go** (Go-familiar, not Go-compatible), **FF** (no false friends), **Safe**
(Rust-level memory and race safety without a borrow checker or annotations), **Plain** (ordinary code needs
no annotations or implementation knowledge), **NoGC** (no GC and no hidden allocation), **Kernel** (suitable
for the PlutoniumOS kernel), **Perf** (performance near Rust).

Section numbers are those of the language specification unless another document is named.

## Rulings

| Finding | Ruling | Goal | Sections changed (or deciding) | Compiler change |
|---|---|---|---|---|
| A1 | Resolved (user). A view of any local that has cleanup (an owner, or a type with drop glue) may not be used after the local's scope ends. This extends E3313. | Safe, Plain | 18.3, A.3 | Yes: extend E3313 to every local with drop glue and update its catalog text. Item 146. |
| A2 | Resolved. In a type parameter declaration `owner` is the keyword only after the identifier list and before a constraint; `[owner any]` names a parameter `owner`. | Go, FF | 4.6 | Yes: the parser decides by position. Item 164. |
| A3 | Resolved. A conversion that moves its operand moves it when the enclosing call or assignment is performed, like a direct argument. | Plain | 11.26 | Check only: the move point of a conversion operand. Item 148. |
| A4 | Resolved. Appendix A.3 cites 12.10 for iterators over owners. | FF | A.3 | No. |
| A5 | Resolved. "section 7.12 (Enums)". | | 7.14 | No. |
| A6 | Resolved. Chapter 2 lists `error`, `illegal`, `invalid`, `panics` and `causes a run-time panic`; 1.3 refers to it. | | 2, 1.3 | No (example extraction tools follow chapter 2). |
| A7 | Resolved. The kind of `string` is counted. Borrowed backing stores (unsafe.String, local arena) are rules about the value, stated separately. | Plain | 8.7 | No. |
| A8 | Resolved. Row C12 of the 2026-10-15 record is marked superseded by D11. | | verification/2026-10-15-spec-consistency.md | No. |
| B1 | Resolved (user, option a). Each Fixed and Chunked arena counts its live counted objects (strings, boxes, maps, channels, allocated errors). `Reset`, `Release` and `Drop` panic with "arena has live counted objects" while the count is not zero. `Release` checks the whole count, not a per-mark count. The cost is one atomic counter per arena and one atomic increment and decrement per counted object. The check runs in the single-task form too, and unchecked mode keeps it. The manual `arena.NewUnsafe` Arena keeps no count. | Safe, NoGC, Kernel | 18.8, 18.9, 13.7, 16.7, 17.1, 17.6, 21.5, F.2; abi.md 1.3; memory-model.md 3.8 | Yes: runtime counter, increments at counted allocation and decrements at last release, checks in Reset, Release and Drop. Item 152. |
| B2 | Resolved. A Fixed arena keeps its bookkeeping in a table sized from its own backing. It never asks the parent for a per-allocation record. Allocation fails when the table is full. | NoGC, Kernel | 18.9, F.2; abi.md 1.3 | Yes: library change in pluto/arena. Item 153. |
| B3 | Resolved (user). An arena takes memory back in bulk only. `Free` of the latest allocation reclaims nothing, and the "arena allocation freed" panic is gone. | Safe, Plain | 18.9, F.2; abi.md 1.3 | Yes: library and epoch code. Item 154. |
| B4 | Resolved. The write check for cleaned places runs only once package cleanup has started, gated on a program-wide flag. Cost: one load and branch per store that may reach a static object with a cleanup obligation. Listed in 21.5, and unchecked mode keeps it. | Perf, Safe | 15.4, 21.5; abi.md 12.10 | Yes: gate the check on `rt.cleanupStarted`. Item 155. |
| B5 | Resolved (option b, D69), as in Go. `%w` of an error held in a field of a longer-lived object keeps the error it wraps: `errors.Unwrap(e) == err` holds and `errors.As` sees the field. Only the message text is copied. The wrapper is bounded by the object like the error it wraps, and the view rules of 18.3 apply. Code that needs an independent error formats it with `%v` or copies it into an `alloc.Synced` error. The proposed copy into a counted error was a false friend: in Go a wrapped pointer error is the same pointer. | FF | 16.5, 16.7 rule 4 | Check only: item 177 adds tests for a wrapper of a field error of a local owner (E3313, E3001); the analysis already bounds `Errorf` by its `%w` operand (review4 e03, e04). |
| B6 | Resolved. The handler data rule is stated as a consequence of Rules 1 to 3: data written while a handler may access it, or written by a handler, must be atomic or under an `rt.Lock`; data not written while a handler may run needs neither. The init exemption ends at the first statement that may install a handler or enable interrupts. | Safe, Kernel | 19.3; memory-model.md 3.1, 4.1, 6.1 | Yes: the race check treats handlers as parties from that statement and stops requiring locks for read-only data. Item 156. |
| B7 | Resolved. `owned`, `shared`, `synced` and `weak` are refused in by-value parameters and results at the C boundary, as strings are; pass `unsafe.Pointer(b)`. `//pu:export` functions are unsafe code for `pluto vet -unsafe`. `owned` is included with the counted boxes because C cannot drop an owner either. | Safe | 20.4, 18.12; abi.md 9.1 | Yes: E2511 for box types; vet lists exports. Item 157. |
| B8 | Resolved. Operands already evaluated are dropped as on a return from the enclosing statement. An owner moved into a call in progress belongs to the callee. `rt.ExitTask` inside a `Drop` method finishes that object's field drops and free first. | Safe | 17.3; abi.md 12.10 | Yes: cleanup of evaluated operands on the exit path; drop glue completes after a Drop method exits. Item 158. |
| B9 | Resolved. Existing text decides the `rt.Lock` half: a runtime lock raises the preemption-disable count (abi.md 12.4, 12.5 rule 6), and 17.2 runs no deferred calls while preemption is disabled. 17.2 now says so by name. New: the standard library's types, and any type that wraps unsafe code, restore their invariants before every check that can panic. Reading chosen: deferred calls run with outer state intact (debugging.md 5 and 17.2 agree). | Safe | 17.2, 18.12 (deciding: 17.2, abi.md 12.4, 12.5) | Library review only: check vec, hash and strings for half-updated state at panicking checks. Item 165. |
| B10 | Resolved. `copy` counts the source before it releases the destination, as assignment does (12.7). | Safe | 13.2 | Check only: the copy lowering's order. Item 159. |
| B11 | Resolved. 16.6 names a counted `synced[T]` error. | | 16.6 | No. |
| B12 | Resolved. F.3 lists vec `Ptr`, `Pop`, `Remove` and `Clone`; F.6 describes `strings.Builder`. Checked against lib/pluto/vec and lib/pluto/strings. | | F.3, F.6 | No. |
| B13 | Resolved. `alloc.Default()` is nil only before `SetDefault` in a freestanding or kernel build, in a task with no override. | Kernel | 18.9, F.2 | Check only. Item 167. |
| B14 | Resolved. `Pause`, `Cpuid`, `Rdtsc` and `Rdtscp` are safe. | Kernel, Plain | 18.12 | Yes: `pluto vet -unsafe` stops listing them. Item 161. |
| B15 | Resolved. A `print` in an exception or NMI handler that interrupts a `print` on its own processor drops its output instead of waiting. | Kernel | 13.12, 20.10 | Yes: the console lock records its holder's processor. Item 163. |
| B16 | Resolved. The panic message buffer is 1024 bytes; the message is cut there with no marker. This matches lib/pluto/rt/panic.p. | | 17.4 | No. |
| B17 | Resolved. The arena window is the extent during which the `Restore` is live, defined by where it is consumed or dropped, and covers callees. | Plain | 18.9 | Check only: the analysis uses the extent. Item 167. |
| B18 | Resolved. The first assignment to a borrowed string parameter releases nothing; only values the callee counted are released. | Safe | 18.8; abi.md 3.3 | Yes, if lowering releases the borrowed value. Item 159. |
| B19 | Resolved. The debug allocator registers generation tags; its fault example is the `--gencheck=false` or untagged-pointer case. | Safe | debug-allocator.md | Check only: the debug allocator registers tags. Item 167. |
| B20 | Already decided: 19.4 makes moving a Scope on which the function calls `Task` error E3405, and keeps the run-time stop for the cases the compiler cannot see. abi.md 12.10 now cites E3405. | Safe | 19.4 (deciding); abi.md 12.10 | No. |
| B21 | Resolved. A freestanding build outside kernel mode checks nil explicitly before every dereference it cannot prove non-nil. | Safe, Kernel | 17.5 | Yes: explicit nil checks for that build mode. Item 162. |
| B22 | Resolved. debugging.md names `alloc.Use`. | | debugging.md 5 | No. |
| C1 | Resolved (user), with two limits on the proposal. Conversions whose result is an owner type, a pointer to one, or an owner union are limited to the package that defines the result type (rule 1, E3212). The audit's extra exception "the operand's type is defined from T" would let any package forge an owner (`B(n)` then `A(b)`), so it holds only in the package of the defined type, and rule 1 is extended to defined-from owners. Also: a pointer conversion from `*Owner` to a non-owner pointer is limited, since a write through it forges; a keyed literal of an owner struct with any unexported field is E3212 in another package; exported variants of an owner union are public as fields are. | Safe | 7.13, 7.18, 8.11, 11.3, 11.24 | Yes: E3212 widened, with new catalog text. Item 147. |
| C2 | Resolved (user). A type-switch clause naming a single ordinary interface, and an assertion from an owning interface to an ordinary interface, lend from the operand and do not move. After the switch the operand is possibly moved if another clause moved it. | Safe, Plain | 7.9, 11.13, 12.9, 18.5 | Yes: move analysis of type switches and assertions. Item 148. |
| C3 | Resolved. The comma-ok move is followed through direct tests of `ok` only. On any other path the operand is possibly moved (E3302). | Plain | 11.13, 18.5, 7.13 | Yes: flow-sensitive tracking of `ok`; the E3302 message suggests `if !ok`. Item 149. |
| C4 | Resolved. An enum type is not an integer type for conversions: `Shade(color)` and a non-member constant in comma-ok form are E2313. | FF, Plain | 7.12, 11.24 | Yes: E2313 cases. Item 150. |
| C5 | Resolved. `type V U` for a union or packed type is a new union or packed type with the same variants or fields and layout, none of `U`'s methods, and explicit conversion. | Go | 7.13, 7.14, 8.2, 10.8 | Yes: variants and fields through the defined name. Item 151. |
| C6 | Resolved. Each instance of a type constructor is its own underlying type. | | 8.2 | Check only. Item 151. |
| C7 | Resolved. A slice literal's array, an `&T{...}` object and a variadic array are frame storage; only the value is a temporary. The variadic array is in the 18.8 release list. | Safe | 11 intro, 18.8 | Yes, if the variadic array's counted elements are released at the statement's end. Item 160. |
| C8 | Resolved. Slicing a type parameter whose type set has a string type is E2019. | | 11.12 | Check only. Item 166. |
| C9 | Resolved. Union types cannot be terms of a constraint. | | 11.16 | No. |
| C10 | Resolved. A packed type converts to and from a byte array of its length. | | 11.24 | Check only. Item 166. |
| C11 | Resolved. The constant conversion to a string is for an untyped integer or rune constant or a constant of type `rune`. | FF | 11.24 | Yes, if `string(n)` of a typed int constant is accepted. Item 166. |
| C12 | Resolved. The owner conversion bullet requires a valid conversion between the underlying types. | Safe | 11.24 | Check only. Item 166. |
| C13 | Resolved. `Operand` has no `[ TypeArgs ]`; `PrimaryExpr TypeArgs` is the one form. | | 11.1, E.4 | No (the parser already has one form). |
| C14 | Resolved. 12.12 names the array that holds the variadic arguments. | | 12.12 | No. |

## Rulings on audit D

| Finding | Ruling | Goal | Sections changed (or deciding) | Compiler change |
|---|---|---|---|---|
| D1 | Resolved (user). Dropping, freeing or moving an object, or assigning to or dropping an owner that holds it, is a write of the whole object for the race rules. A view of a struct's owned object that another task can reach while the owner is replaced is E3501 (unscoped) or E3404 (scoped). Such objects are shared through `synced[T]` or by moving the owner. | Safe | 19.3, 19.4, 12.12, 8.8, A.3; memory-model.md 3.7, 4.1 | Yes: the race check counts frees and owner writes. Item 168. |
| D2 | Resolved (user, option a). The closure region owns every captured variable that has cleanup and drops it on refill or when the caller's frame returns; parameters are copied into the region on entry. The region's drop function runs owner drop glue. | Safe | 11.4, 11.5, 18.6, 18.8, A.3; abi.md 4.1 | Yes: region drop glue for owners. Item 169. |
| D3 | Resolved (user, option a). E3313 also ends a view of a local owner's memory at a call that may free or move an owner out of that memory through a pointer (by the callee's summary) and at an assignment to an owner field on the view's path. A large owner lent to an ordinary interface is a view of the variable, not a copy. 18.10 and 18.11 now agree with 18.3. | Safe, Plain | 18.3, 18.10, 18.11, 7.9, A.3; abi.md 5 | Yes: summaries record freeing parameters, E3313 widened, interface data word. Items 170, 171. |
| D4 | Resolved. 12.19 and 18.6 stay. 18.5 names the exception: `defer x.Drop()` does not copy `x`; a later move cancels the deferred drop. | Plain | 18.5 | Yes: the move checker follows the deferred drop. Item 175. |
| D5 | Resolved, with an existing code. A moving assertion or bound type switch on a field, element or `*p` is E3303 (moving out of a place); `take(&d.state)` is the fix. No new code is needed. | Safe, Plain | 11.13, 12.9, 7.13 | Yes: E3303 cases. Item 172. |
| D6 | Resolved. One rule: for the region errors only, a closure passed through a func value or an interface counts as kept, without summaries. | Plain | 11.5 | Check only. Item 176. |
| D7 | Resolved. A range loop over a counted value ranges over a hidden temporary that holds its own count until the loop ends. | Safe | 12.10 | Yes: the range temporary is counted. Item 173. |
| D8 | Resolved. An owner variable declared in a for clause moves to the next iteration and is dropped once, when the loop ends. | Safe | 12.10, 18.6 | Yes: header owner drop. Item 174. |
| D9 | Resolved. Every `error` value that is not a view of a frame may be sent; a frame view is E3005 or E2011. | Plain | 16.7 | No. |
| D10 | Resolved. A race gives torn or stale values, not undefined behavior in the compiler's sense; a torn pointer or length can break memory safety. The same wording is in every place. | FF | 19.3, 19.7, 18.12, A.3; memory-model.md 4.2 | No. |
| D11 | Resolved. The 18.13 comment says the value moves with the struct. | | 18.13 | No. |
| D12 | Resolved. Chapter 9 says variables with cleanup are dropped or released, citing 18.8 and 18.3. | | 9 | No. |
| D13 | Resolved. 7.14 says "typed value". | | 7.14 | No. |
| D14 | Resolved. A closure may be stored in other memory of the same frame and written as the argument of `s.Task` on a local Scope. | Plain | 11.5 | Check only. Item 176. |
| 7.17 | Resolved under D3. For a ring in a local variable, a view from `Peek`, `All` or `Values` ends at a call of `Pop` or `Reset` (E3313). The summary rule now covers calls that move an owner out. | Safe | 7.17, 18.3 | Part of item 170. |
| 7.18 | Resolved. Converting an owner to a non-owner pointer or view (`(*T)(h)` for `type H owner *T`) gives a view bounded by the owner (E3313). | Safe | 11.24, 18.3 | Part of item 170. |
| 7.19 | Resolved under D3. A call that may free memory through another copy of the same counted reference ends a view obtained through the local reference, unless the compiler knows the two point to different objects. | Safe | 18.3 | Part of item 170. |

## Rulings on audit E

The audit is [2026-11-09-spec-audit-e.md](2026-11-09-spec-audit-e.md).

| Finding | Ruling | Goal | Sections changed (or deciding) | Compiler change |
|---|---|---|---|---|
| E1 | Resolved. Only `&v` and an owner larger than one word lent to an ordinary interface are views of the variable itself. An interface holding a one-word owner holds a copy and is a view obtained from the variable, ended by every D3 event (E3313). The lent result of an assertion or type-switch clause from an owning interface is such a view for every payload size (D71). abi.md 5 is unchanged. | Safe, Plain | 18.3, 18.5, 7.9, 11.13, 12.9 | Yes: E3313 for one-word interface views and lent assertions. Item 199. |
| E2 | Resolved. For `type T owner A`, `T(x)` and the pointer and union forms are allowed only where converting `x` to `A` is allowed too (E3212). 8.11, 7.18 and 18.5 cite 11.24. | Safe | 11.24, 18.5, 8.11, 7.18 | Yes: E3212 through defined-from types. Item 200. |
| E3 | Resolved. Fixed and Chunked arenas count every live allocation made through `Alloc`, owners included; only `New` and `Slice` views are not counted. The panic message is "arena has live allocations". `Release` checks the whole count. The local-arena window rule leaves the count a run-time check only for an earlier `Reset` or `Release` (D70). | Safe | 18.8, 18.9, 13.7, 13.9, 16.7, 17.1, 17.6, 21.5, F.2; abi.md 1.3; memory-model.md 3.8 | Yes: counter at every `Alloc` and `Free`; the compiler's own arenas must free before `Reset`. Item 198. |
| E4 | Resolved, with the fix corrected. A deferred call that can run while a scoped task is not joined (on a return before the join, or on a panic) counts as a statement of the interval for the Sharing rules and E3404, in every build mode. "Declare the Scope before the deferred call" does not work under the 19.4 drop order; the fix is to arm the deferred call after `s.Wait()`, or a deferred `s.Wait()` after it and before the first `s.Task` (D73). | Safe, Plain | 19.4, A.3 | Yes: deferred calls in the interval check. Item 201. |
| E5 | Resolved. The function-literal sentence and the E3314 list of 18.3 name every D3 event. | Safe | 18.3 | Part of item 199. |
| E6 | Resolved. 18.4 says a kept view of a variable with drop glue is E3313 once the loop is left. | Plain | 18.4 | No (item 146 covers E3313). |
| E7 | Resolved. 7.9 and appendix A row 80 say an owner larger than one word is viewed in place and a one-word value is copied, and that the copy of an owner ends at the D3 events. | FF | 7.9, A.3 | No. |
| E8 | Resolved. Appendix A cites 21.4 for the limit of 64 `defer` statements. | | A.3 | No. |
| E9 | Resolved under E3. | | 18.9 | No. |
| A1 | Resolved under E6. | | 18.4 | No. |
| B1 | Resolved under E3. | Safe | as E3 | Item 198. |
| C1 | Resolved under E2. | Safe | as E2 | Item 200. |
| C2 | Resolved under E1: the lent result is a view obtained from `x`. | Safe | 7.9, 11.13, 12.9, 18.3 | Item 199. |
| D3 | Resolved under E1 and E5. | Safe | 18.3 | Item 199. |

## Diagnostics that need a code in phase 1

None of these rulings invents a code. They extend E3313 (A1), E3212 (C1), E3302 (C3), E2313 (C4) and E2511
(B7), whose catalog texts must widen. The new run-time panic "arena has live counted objects" (B1) needs no
code. Audit D adds no code either: it widens E3313 (D3 and the candidates), E3303 (D5), E3501 and E3404
(D1). Audit E adds no code: it widens E3313 (E1, E5), E3212 (E2) and E3402 and E3404 (E4), and renames the
arena panic to "arena has live allocations" (E3).
