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

## Rulings on audit F

The audit is [2026-11-09-spec-audit-f.md](2026-11-09-spec-audit-f.md).

| Finding | Ruling | Goal | Sections changed (or deciding) | Compiler change |
|---|---|---|---|---|
| F1 | Resolved. `New` and `Slice` of a type with drop glue record a drop entry; `Reset`, `Release` (entries after the mark) and `Drop` run the entries in reverse order, then check the count. Emptied places hold the zero value; a panicking entry leaves the rest recorded and nothing taken back; the epoch ends after the entries; a Fixed keeps entries in its own bookkeeping and a full table fails the allocation. The false 18.9 sentence is replaced (D74). | Safe, Plain, NoGC | 18.9, 18.6, 18.8, 18.10, 13.7, 13.9, 16.7, 17.1, 21.5, F.2; abi.md 1.3; memory-model.md 3.8 | Yes: drop entries in the arena library and `New`/`Slice`. Item 202. |
| F2 | Resolved. A type-parameter conversion to an interface is a frame copy for every instantiation; for `[T owner any]` it is also a view the D3 events end, with the stricter rule of each (D75). | Safe, Plain | 18.3, A.3 | Yes: E3313 and the frame rules in generic bodies. Item 204. |
| F3 | Resolved. 18.4 lists the C7 frame storage; a view kept across iterations is E3006, fixed with `new`. 11.3 and 11.15 agree; 18.8's release list already named all five (D76). | Safe, Go | 18.4, 11.3, 11.15, A.3 | Yes, if E3006 does not cover them today. Item 206. |
| F4 | Resolved. The atomic counter's cost is accepted; a plain counter is allowed where no other task can reach the arena or its allocations (D77). | Safe, Perf | 21.5, 18.9, abi.md 1.3 | Optional optimization. Item 203. |
| F5 | Resolved. The count rises only when `Alloc` succeeds. | Safe | abi.md 1.3 | No. |
| F6 | Resolved. The 18.13 arena example says the frees lower the count and keep the bytes. | Plain | 18.13 | No. |
| F7 | Resolved. 18.9 shows a deferred `Reset` with a frame variable still holding arena memory, which still panics under F1 (`msg` is a frame variable, not a value in a plain view), and the fix: a helper function or nested block (D74). | Plain | 18.9 | No. |
| F8 | Resolved. 18.11 says the rules prove it at compile time or the generation check catches it at run time, with the same-call alias as the example. | Plain | 18.11 | No. |
| F9 | Resolved. A deferred `s.Wait()` during a panic waits for the scoped tasks; the deadlock report names the panic in progress (D79). | Safe, Plain | 19.4, 19.1, debugging.md 5 | Yes: the report line. Item 205. |
| F10 | Resolved. "when the starting task panics". | Plain | 19.4 | No. |
| F11 | Resolved. The kernel row names the float printing of `pluto/fmt` and the missing field names of `%+v`. | Go, Kernel | A (kernel facilities row) | No. |
| F12 | Resolved without a code. A `//pu:stack` size out of range is "a compile error" (D78). | Plain | 20.3, 21.4 | Yes: a code. Item 207. |
| F13 | Resolved. 21.4 states the E2530 rule as 12.19 does. | Plain | 21.4 | No. |
| F14 | Resolved. The default task stack in freestanding and kernel builds is `rt.DefaultStackSize`, 64 KiB. 19.5 already required the backend guard of at least one page; it now names the stacks it covers (those not proven, as abi.md 12.1 says), and 19.1 includes freestanding builds (D80). | Kernel, Safe | 21.4, 19.1, 19.5 | No. |
| F15 | Resolved without a code. Exceeding an implementation limit is "a compile error" with a code not yet assigned (D78). | Plain | 21.4 | Yes: a code. Item 207. |
| E1 (verdict) | Completed by F2. | Safe | 18.3 | See F2. |
| E3 (verdict) | Completed by F1 (the false sentence) and F6 (the example). | Safe | 18.9, 18.13 | See F1. |

## Rulings on audit G

The audit is [2026-11-09-spec-audit-g.md](2026-11-09-spec-audit-g.md).

| Finding | Ruling | Goal | Sections changed (or deciding) | Compiler change |
|---|---|---|---|---|
| G1 | Resolved. The D3 view events apply to every conversion of a type-parameter value to an ordinary interface, for every type parameter, `[T any]` and `[T ~int]` included. The other rules that depend on the type argument take the strictest form any permitted type argument needs: a `T` local counts as having drop glue unless no type in its type set has it; a view obtained from a `T` value is bounded by its holder; a method call on a type parameter is checked against that method of every instantiating type (D81). | Safe, Plain | 18.3, 8.7, A | Yes. Item 208, extending item 204. |
| G1 sweep | The other places generic code meets a kind rule: assignment and copy (8.7 and lowering per instance, plus the view bullet); `&x` and the A1 scope rule, and 18.4 declaration and loop order (fixed by the drop-glue bullet); closures (11.5 per instance, E3305 through 8.7, views through 18.3); sending (19.2, checked per instantiation); composite literals, E3312 (8.7); comparison (8.10, 18.5); temporaries, E3009 (fixed by the view bullet). No other hole. | Safe | 18.3, 8.7, 8.10, 11.5, 18.4, 18.5, 19.2 | Covered by item 208. |
| G2 | Resolved. E2584, the race check and the view rules follow an arena's `Reset`, `Release` and `Drop` into the drop glue of every type passed to `New` or `Slice` of that arena, or of every arena where the compiler cannot tell which. The Fixed promise is restated: a Fixed arena never calls its parent for bookkeeping; the drops its entries run are ordinary drops, checked like any other (D82). | Safe, Kernel | 18.9, 18.3, 19.3, 20.10; abi.md 1.3 | Yes. Item 209, extending item 202. |
| G3 | Resolved. A checked kernel or freestanding build on a machine with 5-level paging panics before `main` with "generation checks need 4-level paging". 21.2 states the general rule: a requirement on the machine that only run time can see is checked at startup. The abi.md compile-error clause is removed (D83). | Kernel, Safe | 21.5, 21.2, 17.1, 20.5; abi.md | No: the runtime already panics. |
| G4 | Resolved. 20.5, 21.5 and F state the `pluto_gen_metadata` hook of checked freestanding builds: it returns page-aligned memory that is never taken back, or 0, and must not allocate; a checked build without it is E2588. Hosted and unchecked builds do not need it (D84). | Kernel, Plain | 20.5, 21.5, F | No: E2588 is implemented. |
| G5 | Resolved. An operation that may park the task, reachable inside a non-migrating region (preemption disabled, an `rt.Lock` held, or maskable interrupts disabled), is a compile error. The compiler follows calls as E2584 does. The run-time panic "blocking with preemption disabled" stays as the backstop for paths it cannot see, listed in 17.1 and 19.1 (D85). | Safe, Kernel | 19.3, 19.1, 17.1; abi.md 12.4 | Yes. Item 210, extending items 120 and 156. |
| G6 | Resolved. "No other task or interrupt handler". | Safe | 21.5; abi.md 1.3 | Item 203 text widened. |
| G7 | Resolved. "arena: out of memory" is listed in 17.1, and 18.9 says a failed `New` or `Slice` panics with it. | Plain | 17.1, 18.9 | No. |
| G8 | Resolved. An allocation still live when the entries finish, or an entry recorded while they run, makes the call panic; an allocation made and freed within the entries does not. Chapter and ABI now say the same. | Safe, Plain | 18.9; abi.md 1.3 | Part of item 209. |
| G9 | Resolved. One task waits on a Scope at a time; a second waiter panics with "sync: two tasks wait on one Scope". Listed in 19.4 and 17.1. | Plain | 19.4, 17.1 | No. |
| G10 | Resolved. 20.9 names E5007: an ELF kernel linked so that the runtime's first word of `.percpu` is not at offset 0 fails to build. | Plain | 20.9 | No. |
| G11 | Resolved. abi.md, memory-model.md, debugging.md and debug-allocator.md say version 0.4. | Plain | those documents | No. |
| G12 | Resolved. `rt.DefaultStackSize` is listed in F.1 beside `rt.MinStackSize` (the audit said F.2; both names are in F.1). | Plain | F.1 | No. |
| G13 | Resolved. A `Slice` of length zero records no entry. | Perf, Plain | 18.9; abi.md 1.3 | Part of item 209. |
| F1 (gaps) | Completed by G2, G8 and G13. | Safe | as those | See those. |
| F2 (gap) | Completed by G1. | Safe | 18.3 | See G1. |
| F4 (gap) | Completed by G6. | Safe | 21.5 | See G6. |
| F14 (gap) | Completed by G12. | Plain | F.1 | No. |
| Stale 1 | 18.11 "or freed it" removed (D46). | Plain | 18.11 | No. |
| Stale 2 | 7.19: freeing into a Fixed or Chunked arena takes no memory back and lowers the count of live allocations (D70). | Plain | 7.19 | No. |
| Stale 3 | 20.10 states the handler data rule as 19.3 does: only data another party may write while a handler may run must be atomic or locked (D52). | Plain | 20.10 | No. |
| Stale 4, 5 | The same handler rule in the skill's kernel-mode.md and concurrency.md. | Plain | skill references | No. |
| Stale 6 | The skill's kernel-mode.md lists every safe function of `pluto/arch/x86` that 20.8 names (D56). | Plain | skill references | No. |
| Stale 7 | The skill's language tour states the one-word copy rule of 7.9 (D71). | Plain | skill references | No. |
| Ambiguous 1 | Skill ownership-and-memory.md, the E3313 events: already complete against D61 (moved, dropped, reassigned, a call that may free, an owner field assigned, counted copies). The arena section of the same file states the `Reset` and `Release` event. | Plain | skill references | No. |
| Ambiguous 2 | Skill kernel-mode.md checklist "Handlers: atomics and `rt.Lock` only" restated as D52. | Plain | skill references | No. |
| Ambiguous 3 | Skill ownership-and-memory.md said all of `pluto/arch/x86` is unsafe; now names the safe functions. 20.1 said the same and is fixed. | Plain | 20.1, skill references | No. |
| Skill sweep | The skill references checked against D42 to D80. Fixed: E3212 conversions (D43, D72), arena drop entries and counts (D70, D74), the E2511 box rule (D50), kernel print (D58), Scope rules (D59, D73, G9), default stacks (D80), and the panic list. No example code needed a change. | Plain | skill references | No. |
| Cross-check 1 | Union equality, abi.md against 7.13 and 8.10: already consistent. | Plain | 7.13, 8.10 | No. |
| Cross-check 2 | String arguments, abi.md 3.3 against 18.8: already consistent. | Plain | 18.8 | No. |
| Cross-check 3 | abi.md 4.1 init-time closure regions now say the drop runs once in package cleanup, dropping owners and releasing counted references, as 15.4 says. | Plain | abi.md 4.1, 15.4 | No. |
| Cross-check 4 | abi.md 12.7 "from the requested allocator" is consistent with 12.1 and 12.6. 19.1 ignored the allocator given to `rt.Go` and now names it. | Plain | 19.1; abi.md 12.7 | No. |

## Rulings on audit H

The audit is [2026-11-09-spec-audit-h.md](2026-11-09-spec-audit-h.md).

| Finding | Ruling | Goal | Sections changed (or deciding) | Compiler change |
|---|---|---|---|---|
| H1 | Resolved. A `Reset`, `Release` or `Drop` of an arena, the implicit drop at scope end included, frees, and so writes, every object `New` or `Slice` allocated in it, of every type, for the race rules and the E3313 events. `Release` counts every type. A live `Alloc` allocation panics and is not freed (D86). | Safe | 19.3, 19.4, 18.3, 18.9, 12.12; memory-model.md 3.8, 4.1 | Yes. Item 212, extending items 168 and 209. |
| H2 | Resolved. The G5 compile error reports only exact paths. A call through an interface or func value where only some candidates wait, and an allocation or drop where only some installed allocators wait, compile and panic at run time if they wait. 19.3 says why E2584 stays conservative (D87). | Kernel, Plain | 19.3, 19.1, 17.1, 20.10 | Yes. Item 210 text narrowed. |
| H3 | Resolved. In kernel mode `rt.Park` also panics when interrupts are disabled; the test is on entry to `park`, before runtime locks, so the scheduler's switch, `Yield`, `Preempt` and `preemptPark` are not tested (D88). | Kernel, Safe | 19.3, 19.1, 17.1; abi.md 12.4 | Yes. Item 213, extending item 210. |
| H4 | Resolved. The code of `pluto/rt` is unsafe code; its non-migrating regions are not checked for waits. A path from a program's region into the runtime still ends at `park` (D89). | Kernel | 18.12, 19.3 | Part of item 210. |
| H5 | Resolved. "Borrow" is replaced by "view" in 19.3 and 8.8, with E3313, E3314 and E3501 named; 11.14 counts a pointer-receiver call as a view (D90). | Plain | 19.3, 8.8, 11.14 | No. |
| H6 | Resolved. Row 81 states the cost: `return x.Bytes()` for `x T` is E3001 even when every type argument returns heap memory; take `*T` or the interface type (D91). | Go, Plain | A | No. |
| H7 | Resolved. "A value whose type contains a type parameter (`x` of type `T`, or `Box[T]{x}`)" (D92). | Safe | 18.3, A; skill known gaps | No. |
| H8 | Resolved. An arena is named by its variable or field, and a pointer by where it came from, as locks are; an arena it cannot name counts as every arena it may be (D93). | Safe, Plain | 19.3, 20.10 | Part of item 212. |
| H9 | Resolved. Closure region, non-migrating region and interrupt-disabled region are always qualified; "region annotations" keeps D12's wording; chapter 1's glossary defines all three. Other memory once called a region is renamed (D94). | Plain | 1, 7, 11, 12, 17, 18, 19, 20, 21, A; abi.md; memory-model.md; skill references | No. |
| Spec examples | The 12 wrong examples are fixed without changing a rule, and the 7.9 example marks both lines of the clash (D95). | Plain | 7.9, 7.18, 10.10, 11.6, 11.7, 11.18, 11.24, 11.25, 11.27, 12.17, 15.1 | No. |
| `send(buf.Len(), buf)` | 11.14, 11.26 and 18.5 agree: a move and a use in one expression are an error in either order. The compiler accepts this order. | Safe | 11.14 (deciding) | Yes. Item 211, extending item 75. |

### Attacks on the audit H edits

Each edit was attacked with a program that might slip past it.

- H1, a plain struct: task A reads `p.x` of a `*P` from `ar.New[P]()`, `P` has no drop glue, and the
  starting function calls `ar.Reset()` while A may run. The reset is a write of every `P` in `ar`, so the
  type rule reports E3501. Closed.
- H1, two arenas: `ar1.Reset()` while a task reads an object of `ar2`. The arenas are named by variable,
  so no conflict is reported and none exists. With `arenas[i].Reset()` and a variable `i`, the call counts
  on every element: a false positive at worst. Closed.
- H1, the implicit drop: the arena is a local whose scope ends while a task uses an object from it. The
  scope-end drop is named as a `Drop` and counts. Closed.
- H1, a func value: `reset := ar.Reset` called later. The method value names the arena it was taken from;
  a func value the compiler cannot follow is held by 18.3's precision rule (counts as every arena it may
  be). Closed, conservatively.
- H1, an `Alloc` allocation: a task uses an owner allocated by `ar.Alloc` while `ar.Reset()` runs. The live
  count makes the reset panic and take nothing back, so nothing is freed under the task. Closed at run time,
  as D70 decided.
- H1, a scoped task: the same as the plain struct case inside `sync.Scope`. 19.4's type-based conflict rule
  counts the reset (the audit also checked this). Closed.
- H1, a local view: `p := ar.New[int](); ar.Reset(); _ = *p` in one task. E3313 ends `p` at the reset,
  whatever the type. Closed.
- H2, an interface call in a region: `mu.Lock(); dev.Read(buf); mu.Unlock()` where one `Device`
  implementation waits. It compiles; if that implementation runs, `park` sees the lock count and panics.
  Loud, not silent. Accepted by design.
- H2, an allocator: `alloc.Use` of an allocator that may wait, inside `rt.DisablePreemption`. If the window
  is exact (a known allocator), the error is reported; if only some installed allocators wait, the panic
  catches the wait. Closed.
- H2, with interrupts disabled and no lock: `x86.Cli(); dev.Read(buf)`. The preemption count is zero, so
  before H3 nothing caught it in kernel mode. H3 closes it in kernel mode; other freestanding builds stay a
  known gap. Recorded.
- H3, the switch path: the scheduler's `Park` switches with runtime locks held and interrupts disabled in
  kernel mode. The test runs on entry to `park`, before those locks, so the switch is not tested. A
  program that disables interrupts and then waits fails the test. Closed.
- H3, a handler that yields: a handler calls `rt.Yield()` with interrupts disabled. `Yield` does not call
  `park`, and 12.2 saves RFLAGS, so this stays allowed and the yielded task resumes with its own flag.
  Closed.
- H4, a user region into the runtime: `mu.Lock(); ch <- v`. The path from the user region reaches the
  runtime's `park`, which is a wait, so it is reported. The exemption covers only regions that start in
  `pluto/rt`. Closed.
- H4, a runtime region calling user code: a runtime callback (a hook or a drop) waits while the runtime
  holds a lock. The compile check does not see it; `park` sees the lock count and panics. Closed at run
  time.
- H5, a view after the move: `p := &c.n; task use(c); println(*p)`. `p` is a view of a local owner used
  after the move: E3313. A view stored in a field of a package variable: E3501. Closed.
- H7, a wrapper: `return Box[T]{x}.Get()` for `x T`. The type of `Box[T]{x}` contains `T`, so the bound
  applies. Closed.
- H9: no rule changed, only terms, so no program can slip past it.

### Known-gaps lines

Added to `skill/plutonium/references/known-gaps.md`:

- A use of a variable in an argument before the argument that moves it (`send(buf.Len(), buf)`) compiles; it
  should be an error, as the other order is (11.14, E3301). Item 211.
- The race check and E3313 do not yet treat a `Reset`, `Release` or `Drop` of an arena as a free of every
  object it takes back, of every type (19.3, 18.3, memory-model.md 3.8). Item 212.
- In kernel mode, `rt.Park` does not yet panic when interrupts are disabled, so a wait with interrupts
  disabled and no lock held is not caught at run time (19.3, abi.md 12.4). Item 213.

Changed: the item 210 line says "on an exact path inside a non-migrating region", and the G1 line (items 204
and 208) says "a value whose type contains a type parameter".

By design, and so not a gap: other freestanding builds do not test the interrupt flag in `park` (19.3,
abi.md 12.4); a wait there with interrupts disabled and no lock held is not caught.

## Rulings on audit I

The audit is [2026-11-09-spec-audit-i.md](2026-11-09-spec-audit-i.md).

| Finding | Ruling | Goal | Sections changed (or deciding) | Compiler change |
|---|---|---|---|---|
| I1 | Resolved. A load, by a task or interrupt handler, of a pointer or any value that holds a view, from a package variable (per-CPU included), the object of a `synced[T]` or a map, or memory reached through them, is a crossing; rule 2 checks the memory. Rules 1 and 2 count the memory a package variable owns as part of it. A package variable holding a `shared[T]` used by two tasks is E2010. `p = &*b; task reader(); b = new(Node)` is E3501 by rule 2 (D96). | Safe | 19.2, 19.3 (rules 1 and 2, Interrupt handlers), 20.9, A; memory-model.md 4.1; skill concurrency | Yes. Item 214, extending items 168 and 212. |
| I2 | Resolved. One definition by target set: the wait check follows only calls with a single known target (direct calls, concrete method calls, deferred calls of those, function literals called where written, inserted drops of known types with a named arena's drop entries), through any depth, and reports a wait every path to which lies inside the region, even one on only some paths of a followed callee. Interface, func-value, method-value and hook calls, unnamed arenas and allocator calls are not followed. The list-style definition is removed; 19.3 has a reported and a not-followed example (D97). | Plain, Kernel | 19.3, 19.1, 17.1, 20.10; skill concurrency, kernel-mode, errors-and-panics | Yes. Item 215, extending items 210 and 213. |
| I3 | Resolved. The compiler inserts the `rt.Park` test at the entry of every `//pu:blocking` function, and before each call of one without a body. The silent-hang claim is narrowed to waiting operations: an unmarked spin loop is the program's own bug, and unmarked waiting code outside Plutonium breaks its contract (D98). | Kernel, Safe | 19.3, 19.1, 17.1, 20.3, 20.10; abi.md 12.4; skill | Yes. Item 216, extending items 210 and 213. |
| I4 | Resolved. "Never accepts on doubt" holds for every safety analysis; the wait check of non-migrating regions is named in 1.2 and chapter 21 as the one check that accepts on doubt, because its failure is a loud panic, not a memory error or race (D99). | Plain | 1.2, 21 | No. |
| I5 | Resolved. The four bare uses of "region" now say "closure region" (debugging.md, 11.9) or "a stretch of code" (10.10, debug-allocator.md). | Plain | debugging.md, 11.9, 10.10, debug-allocator.md | No. |
| I6 | Resolved. 8.8 Views that cross names an arena's `Reset`, `Release` or `Drop` as a change; the 1.4 glossary entry names the code of `pluto/rt` as unsafe (18.12). | Plain, Safe | 8.8, 1.4 | No. |
| I7 | Resolved. Beyond the 18.3 limits a call on an arena counts on every arena of the program; an arena's name follows a move (`b := take(a)`) (D100). | Safe | 19.3 | Yes. Item 217, extending item 212. |
| I8 | Resolved. An implementation uses the 18.3 limits exactly and may not raise them; a later edition may. 18.3 cites 21.3 (D101). | Plain | 18.3 | No (a conformance test belongs to item 184). |
| Silent-hang claim (19.3) | Backed by I3 and narrowed to waiting operations. | Kernel | 19.3 | Item 216. |
| Race freedom through package variables (19.3, memory-model.md 4.1) | Backed by I1: every view a task loads from a package variable is checked by rule 2. | Safe | 19.2, 19.3, memory-model.md 4.1 | Item 214. |

### Attacks on the audit I edits

Each edit was attacked with a program that might slip past it.

- I1, the audit's program: `b = new(Node); p = &*b; task reader(); b = new(Node)` with `reader` reading
  `p.n`. The load of `p` in `reader` crosses; the `Node` is owned by `b`, which `main` replaces after tasks
  start, so it is not unchanged package memory, and the drop breaks the type rule. E3501. Closed.
- I1, through a helper: `reader` calls `get()`, which returns `p`. The load is in a callee of the task,
  which counts. Closed.
- I1, an atomic pointer: `var p atomic.Pointer[Node]`, `reader` calls `p.Load()`. An atomic load counts as
  a load; the access of `p` is atomic, the `Node` still follows rule 2. Closed.
- I1, an interface: `var cur Device = &dev0` where `dev0` is a heap object `main` later drops. Loading
  `cur` loads a value that holds a view: rule 2 on the object. Closed.
- I1, a func value: `var cb func()` assigned a plain function. A plain function holds no view, and a
  closure that captures variables, or a method value, cannot be stored in a package variable (11.5). A
  closure in a caller's closure region is a frame view too. Closed.
- I1, a per-CPU variable holding a pointer: the access is safe by 20.9, the load crosses (20.9 now says
  so), and the task may migrate while it uses the pointer, which rule 2 allows only for safe memory.
  Closed.
- I1, a `synced` box: task A loads `s.p` under `s.mu` and uses `*s.p` after unlocking; task B replaces the
  owner of that object under `s.mu`. The load from the object of a `synced[T]` crosses, and the replace
  frees the object while A may use it: E3501. Closed.
- I1, a map: `var m map[int]owned[Node]`; a task takes `&*m[k]` under the map's guard and uses the
  pointer after another task deletes the entry, which drops the node. The map contents follow rule 3, and the loaded pointer crosses: E3501. Closed.
  Corrected by audit J (R1): `owned[Node]` is not a legal map element, so this program does not reach the
  race check. The attack is redone with legal maps under "Rulings on audit J".
- I1, a `shared[T]` package variable copied by two tasks: before, no rule covered the count. Now E2010.
  Closed.
- I1, a handler table filled during initialization with `&devs[i]` (package memory) and with heap records
  that nothing frees and whose fields are guarded: the first is rule 2 case 2, the second the type rule.
  Accepted, correctly. A table of heap records that some code frees later: E3501. Closed.
- I1, a false positive: a slice of `int` built in a package variable by `make` and read by every task.
  A slice of a non-struct crosses only in the first three cases of rule 2, so this is safe only when the
  memory is owned by the package variable (an owner such as `vec.Vec`, or an `owned` array) and not written
  after tasks start. A raw slice of heap memory is rejected: a false positive at worst. Recorded, not a hole.
- I1, before the first task: `main` loads `p` and stores it in a local before any task starts. No other
  task exists then, so nothing crosses; a later task that loads `p` crosses on its own load. Closed.
- I2, a wait on some paths of a callee: `tick` calls `record(n)` directly and `record` sends only when
  `n > 0`. Reported (the 19.3 example). Closed.
- I2, a function literal called where written: `func() { ch <- 1 }()` inside the region. One target,
  followed, reported. Closed.
- I2, a hook: an unexported func variable assigned `waitForIt` only. Not followed, so it compiles; the
  park test panics if it waits. Accepted by design (D97).
- I2, an interface where every implementation waits: before, reported as exact; now not followed, and
  caught at run time. A narrowing of the compile check, not a hole, since the panic is loud. Accepted by
  design.
- I2, a region opened on some paths only: `if fast { rt.DisablePreemption() }; record(n)`. The region is
  not on every path, so the check does not start; if the wait runs with preemption disabled, the test
  panics. Closed at run time.
- I2, a drop in the region: `v = makeOwner()` over an owner of known type whose `Drop` takes a
  `sync.Mutex`. The inserted drop has one target and is followed: reported. Its `Free` into the installed
  allocator is not followed. Closed for the known part; the allocator part relies on the test.
- I3, a `//pu:blocking` function with a body that spins on a flag, called under an `rt.Lock`: the entry
  test sees the count and panics. Closed.
- I3, an assembly `hlt` loop marked `//pu:blocking`, called with interrupts disabled in kernel mode: the
  test before the call reads IF and panics. Closed.
- I3, the same assembly function not marked: a function without a body counts as not waiting, so nothing
  tests it, and the processor halts. That breaks the contract of unsafe code (18.12); the claim now says
  so. Narrowed, recorded.
- I3, `for {}` under an `rt.Lock`: not a waiting operation; the claim no longer covers it. Narrowed,
  recorded.
- I3, the inserted test in a function called legitimately with preemption disabled: the standard library
  marks only `park`, which makes the same test already. A program that marks a function it calls under a
  lock gets the panic, which is what the marking asks for. Closed.
- I4: no rule changed, only the statement of the principle.
- I5, I6: wording; no program can slip past them.
- I7, an arena moved to a new variable: `b := take(a); task use(p)` where `p` came from `a.New`, then
  `b.Reset()`. The name follows the move, so the reset frees the objects of `a`: E3501. Closed.
- I7, an arena reached through more loads than 18.3 allows: the call counts on every arena. A false
  positive at worst. Closed.
- I8, an implementation that tracks four loads: it would accept a program the stated limits reject; it is
  now not conforming. Closed.

### Known-gaps lines

Added to `skill/plutonium/references/known-gaps.md`:

- The race check does not yet treat a task's or handler's load of a pointer or other view from a package
  variable, a per-CPU variable, a `synced` object or a map as a crossing, so `p = &*b; task reader(); b =
  new(Node)` compiles (19.2, 19.3 rules 1 and 2). A package variable holding a `shared[T]` used by two tasks
  is not yet E2010. Item 214.
- The race check does not yet name an arena beyond the precision limits as every arena, or follow an
  arena's name through a move (19.3, Frees are writes). Item 217.
- A function marked `//pu:blocking` does not yet test preemption and the interrupt flag at entry, or before
  each call when it has no body, so calling one in a non-migrating region can hang instead of panicking
  (19.3, 20.3, abi.md 12.4). Item 216.

Changed: the item 210 line says "reached through calls with one known target" and names items 210 and 215.

By design, and so not a gap: a wait behind an interface, a func value, a hook or an allocator inside a
non-migrating region compiles and panics at run time (D97); a spin loop not marked `//pu:blocking` is not
caught (D98).

## Rulings on audit J

The audit is [2026-11-09-spec-audit-j.md](2026-11-09-spec-audit-j.md).

| Finding | Ruling | Goal | Sections changed (or deciding) | Compiler change |
|---|---|---|---|---|
| J1 | Resolved. Rule 2 gets a fourth case: memory a package variable owns, or that a `synced[T]` object owns through owners that hold one another, may be reached through a crossing view when every access holds the inferred guard (a write for writing) and the view is used only where a guard is held, on every path. A use with no guard held is E3501, including a view returned by a function that unlocks first. A copy whose uses cannot be followed counts as used with no lock; a deferred call's argument is used at the end of the function (D102). | Go, Plain, Safe | 19.3 (rule 2, two new examples), 8.8, A, memory-model.md 4.1; skill concurrency | Yes. Item 214. |
| J2 | Resolved. Rule 2 case 2 covers static objects: made by a composite literal whose address a package-level initializer takes, in static storage (18.2), and not moved, dropped or overwritten as a whole once tasks start. The free condition does not apply; the field conditions do. The compiler names the target of `take(p)` and `*p = v` by where `p` came from; one it cannot name makes every static object of the type not static. Reassigning the package variable is a rule 1 write; the old object stays static and the new one needs its own case (D103). | Go, Plain | 19.3 (rule 2, Interrupt handlers, new example), 8.8, 20.10, memory-model.md 4.1; skill concurrency | Yes. Item 214. |
| J3 | Resolved. A load of a view crosses once a task other than the main task, or an interrupt handler, may run; a handler's load crosses from the first install or interrupt enable, whether or not a task has started (D104). | Safe, Kernel | 19.2, 19.3 (The tasks), 20.10; skill kernel-mode | Yes. Item 214, with item 156. |
| R1 | Resolved. The I1 map attack used `map[int]owned[Node]`, which is not a legal map. Redone below with a map of pointers and a map of `synced` values. | Safe | this record | No. |
| R2 | Resolved. Items 198 to 217 are placed in "Order of work" (arena and view soundness in phase 1, the wait check in phase 3, 205 and 207 in phase 2, 203 in phase 4), with the new open items 218 and 219 in phase 0. "D42 to D69" now reads "D42 onward". Phases 0 and 1 of the completion plan list the new items (D107). | Plain | roadmap.md, completion plan | No. |
| R3 | Resolved. D85 says it was narrowed by D87 (and D97), and Superseded records the old D85 wording (D107). | Plain | decisions.md | No. |
| R4 | Resolved. D45 says D70 replaced its panic message, and Superseded records the old message (D107). | Plain | decisions.md | No. |
| `sync.Once` lazy initialization | Unresolved in this pass; decided later the same day (D105, see "Once and Cond"). The read after `Do` is E3503, since `Once.Do` holds its own mutex while `f` runs. Safe forms: a package-level initializer, or a `synced[T]` under a mutex. Proposal recorded (D105). | Go | 19.3 (deciding) | Item 218, phase 1. |
| `sync.Cond` wait loop | Unresolved in this pass; decided later the same day (D106, see "Once and Cond"). Data read after `c.Wait()` is E3503, since `c.L` is reached through an interface. Safe forms: a buffered channel, or a `vec.Vec` under a mutex with a `chan struct{}` wake. Proposal recorded (D106). | Go | 19.3 (deciding) | Item 219, with item 197, phase 1. |

### Eight Go programs

Each program is the shape a Go programmer would write first. The deciding sentence is in 19.3 unless named.

1. **A cache.** `var mu sync.Mutex; var cache = map[string]*Entry{}`, and `get` does `cache[k] = &Entry{}`
   under `mu`. Does not compile: `&Entry{}` in a function is a frame view stored in package memory, E3002
   (18.3). Safe form: `var cache = map[string]synced[Entry]{}` under `mu`, with `get` copying the
   `synced[Entry]` out under the lock. It compiles: the map follows rule 1 (guard `mu`), the entry's object
   follows rule 3, and fields of `Entry` that change need its own lock or atomics. A second safe form keeps
   the entries in a package-level `vec.Vec[Entry]` and the map holds `*Entry` into it, used only under `mu`:
   it compiles by rule 2 case 4 (J1), and a use of a pointer after `Unlock` is E3501.
2. **A logger.** `var std = &Logger{w: os.Stderr, prefix: "app: "}` with a `sync.Mutex` field taken by
   `Printf`. Compiles. `std` points to a static object (rule 2 case 2, J2); `mu` is a lock of the same
   object; `w` holds a view of the static `File` behind `os.Stderr`, which never changes; `prefix` never
   changes once tasks start.
3. **A registry.** `func init() { Register("echo", &echoHandler{}) }` storing into `var handlers
   map[string]Handler`. Does not compile: `&echoHandler{}` in `init` is a frame view kept in a package
   variable, E3002 (18.3). Safe form: `var handlers = map[string]Handler{"echo": &echoHandler{}}` as a
   package-level initializer. The composite literal is a static object (J2), the map is written only during
   initialization (rule 1), and its contents are filled before any task can see them (rule 3). A map of
   plain functions or small values compiles the same way.
4. **A config swap.** `var cfg atomic.Pointer[Config]` with `cfg.Store(&Config{...})` in a reload task.
   Does not compile: a frame view stored in package memory, E3003 (18.3). Keeping an owner in a package
   variable and replacing it frees the old `Config` while readers may hold it: E3501 (rule 2, Frees are
   writes). Safe form: `var cfg synced[Config]` behind a `sync.Mutex`, with readers copying the reference
   out under the lock and the reload task assigning a new one under it. It compiles by rules 1 and 3.
5. **A connection pool.** `var mu sync.Mutex; var free vec.Vec[owned[Conn]]`, with `Get` popping and
   `Put` pushing under `mu`. Compiles. `Pop` and `Push` load the backing slice of memory `free` owns, under
   `mu`, and the view does not outlive the lock (rule 2 case 4, J1). The `owned[Conn]` that `Pop` returns
   is moved out under the lock and is the task's own.
6. **An atomic counter struct.** `var stats Stats` with `atomic.Int64` fields, or `var stats = &Stats{}`.
   Compiles. Every access is atomic (rule 1); the pointer form points to a static object whose fields are
   used only atomically (rule 2 case 2, J2).
7. **A `sync.Once` singleton.** `var inst *DB; var once sync.Once`, with `once.Do(func() { inst =
   open() })` and a read of `inst` after `Do`. Does not compile: `Once.Do` calls `f` while it holds
   `once.m`, so `once.m` is the inferred guard of `inst`, and the read after `Do` is E3503 (rule 1). Safe
   forms: `var inst = &DB{...}` as a package-level initializer (a static object), or `synced[DB]` copied out
   under a mutex. No safe form keeps the lazy shape. Unresolved, with a proposal (D105, item 218). Redone under D105 in
   "Once and Cond": it compiles.
8. **A work queue with `sync.Cond`.** `q = append(q, job)` under `mu` with `c.Signal()`, and workers that
   run `for len(q) == 0 { c.Wait() }`. Does not compile: `append` on a package slice is E2013 (7.9), so the
   queue becomes a `vec.Vec`; then the read of the queue after `c.Wait()` is E3503, since `c.L` is reached
   through the `Locker` interface and no named lock counts as held after `Wait` (rule 1). Safe forms: a
   buffered channel of jobs, or a `vec.Vec` under a mutex with a buffered `chan struct{}` that wakes one
   worker per push. Unresolved for the `Cond` shape, with a proposal (D106, item 219, with item 197). Redone under D106
   in "Once and Cond": it compiles with `c.Wait(&mu)`.

Redo of the I1 map attack (R1), with legal maps:

- `var m map[int]synced[Node]` under `mu`. A task copies `m[k]` out under `mu` and uses the node under the
  node's own lock; another task deletes the entry. The copy keeps a count, so the delete frees nothing the
  first task uses. Compiles by rules 1 and 3. Accepted, correctly.
- `var m map[int]*Node`, where the nodes are owned by a package-level `vec.Vec[owned[Node]]` and both are
  guarded by `mu`. A task loads `m[k]` under `mu` and uses the pointer after `Unlock`, while another task
  removes the node from the vec, which drops it. The use is outside the guard: E3501 (rule 2 case 4). Used
  only under `mu`, it compiles by case 4. Nodes owned by something the guard does not cover follow the type
  rule, which fails once any task drops a `Node`: E3501. Closed.

### Attacks on the audit J edits

Each edit was attacked with a program that might slip past it. None was compiled (no builds in this pass);
the results follow from the rules as written.

- J1, a view kept by a function literal: `p := items.Ptr(0)` under `mu`, then `go func() { use(p) }()`.
  Passed to a task: a copy whose uses cannot be followed. E3501. Closed.
- J1, a view returned after `Unlock`: the `first` example of 19.3, which unlocks in a deferred call and
  returns `items.Ptr(0)`. Judged at the caller's use, where no lock is held: E3501. Closed.
- J1, a view stored in a package variable not guarded by `mu`, or in a field of a heap object: a copy
  whose uses cannot be followed. E3501. Closed.
- J1, a view sent on a channel or held in a method value: same rule. E3501. Closed.
- J1, a deferred use: `defer use(items.Ptr(0))` written after `defer mu.Unlock()`. The argument is used at
  the end of the function, where deferred calls run in reverse order, so `use` runs before `Unlock` and the
  guard is held. Written before `defer mu.Unlock()`, `use` runs after it: E3501. Closed.
- J1, a guard held on only some paths: `if fast { mu.Lock() }; p := items.Ptr(0)`. A lock counts only when
  held on every path. E3501. Closed.
- J1, two locks: `Push` under `a` in one task and under `b` in another. No lock is held by every write, so
  there is no guard: rule 1 reports E3502 or E3503. Closed.
- J1, `RLock`: a read through the view under `RLock` passes; a write through it fails the case, E3501.
  Closed.
- J1, a method value: `f := items.Push` called under `mu`. A method value's method counts as run with no
  lock held: E3502 (rule 1). Closed; a false positive at worst.
- J1, a view stored in memory under the same guard and freed later under the guard: ordered by the lock,
  so not a race; whether the stored view may still be used is decided by 18.3 and 18.11. Recorded, not a
  hole.
- J1, a `synced[T]` whose object owns a `vec.Vec`: a pointer into it loaded under the object's lock and
  used after `Unlock` while another task pushes: E3501. Used only under the lock: compiles. Closed.
- J2, reassigning the variable: `os.Stdout = &File{fd: 3}` in `main` after tasks start, with no lock. A
  rule 1 write with unguarded reads: E3502 or E3503. Under a guard, the old `File` stays static for a task
  that loaded it; the new one, made in a function, is a frame view stored in package memory (E3002) unless
  it is itself static or owned and follows a case. Closed.
- J2, a mutable field: a static object whose `count int` field a task writes without a lock. The field
  conditions fail: E3501. Closed.
- J2, `take(os.Stdout)` in one task while another prints: the object is no longer static, and the type
  rule fails for `File` once any `File` is dropped. E3501. Closed.
- J2, a `take(p)` whose `p` the compiler cannot name: counts as a move of every static object of the type.
  A false positive at worst. Closed.
- J2, dropping an unrelated `File`: the 19.3 example closes its own file and prints. The free condition
  does not apply to a static object: compiles. Accepted, correctly.
- J2, an object made by `new` in an initializer: not a composite literal, so not static; an owner holds
  it, and replacing the owner frees it. E3501 if a task loads it then. Closed.
- J3, a handler installed before any task: it loads `p` from a package variable, and `main` then replaces
  the owner of `*p`. The load crosses from the install: E3501. Closed.
- J3, a handler that loads from a static object or from unchanged package memory: rule 2 case 2. Compiles.
  Accepted, correctly.
- R1 to R4: records only; no program can slip past them.

### Known-gaps lines

Changed in `skill/plutonium/references/known-gaps.md`: the item 214 line also names a pointer into a guarded
package-level `vec.Vec` used after the `Unlock`, a static object moved with `take` while tasks read it, and a
handler's load before the first task, none of which is E3501 yet.

Not added: the `sync.Once` and `sync.Cond` shapes. The specification rejects them on purpose today (D105,
D106), so they are not gaps between the compiler and the specification. `skill/plutonium/references/concurrency.md`
gave their safe forms. D105 and D106 then decided them; the lines added are in "Once and Cond".

## Once and Cond

Later the same day, D105 and D106 were decided. The rules are in 19.3 (rule 1, Lazy initialization; the
Accesses-and-locks bullet for `Cond.Wait`; rule 2 cases 2 to 4), memory-model.md 3.4 and 4.1, F.5 and A.
Item 218 makes the compiler change for Once, item 219 for Cond, and item 197 makes `sync.Cond` a value.

The Once rule, in short:

- A package-level Once initializes package variables; a Once field initializes fields of the same object.
- A write counts as inside the Once when it is made in `f`, or in a function `f` calls, on `Do`'s call path.
  Writes by tasks `f` starts, and by method values, are outside. A write through a pointer the compiler cannot
  name counts as a write of all data of its type.
- A read is ordered after the writes when every write is inside, and the reading function, or a direct callee
  it calls on every path, has called `Do` on every path. A `Do` in another task, or reached through an
  interface, a func value or `CatchExitTask`, does not count.
- Otherwise the ordinary rules apply: E3502 names the write outside, E3503 notes the Once.
- `rt.ExitTask` in `f` leaves the Once not done; the lock is released and the next `Do` calls its own
  function. A panic stops the program.
- Two different functions passed to one Once are fine. A Once that is reset or moved initializes nothing, and
  a copy is another Once.
- A view of data a Once initializes follows rule 2 case 2 (package memory) or case 3 (a synced object); a use
  after the reference is dropped is E3313.

The Cond rule, in short: `sync.Cond` is a value with no `L` field, and `c.Wait(l)` takes the lock. It counts
as `l.Unlock()` then `l.Lock()` when the compiler names `l` (`&mu`, `&s.mu`, or a pointer named by where it
came from). Otherwise it counts as unlocking every lock and leaves none held, and E3503 for a later read names
the `Wait`. Signal and Broadcast need no lock. A stored `c.L` was dropped because a Cond beside its mutex in
one struct would hold a view of its own object.

Rule 2 case 4 was tightened while attacking it: a guarded view is used only while the lock it was loaded
under is still held, with no release on any path from load to use, even if the lock is taken again.

Programs 7 and 8, redone:

7. **A `sync.Once` singleton.** `&DB{}` made in a function is a frame view, E3002 (18.3), so the variable
   becomes `var inst owned[DB]`. With `once.Do(func() { inst = open() })` and a read of `inst` after `Do`, it
   compiles: every write of `inst` is inside the Once and a `Do` has returned on every path before the read
   (rule 1, Lazy initialization). A view of the `DB` follows rule 2 case 2. A write of `inst` anywhere else is
   E3502 naming that write.
8. **A work queue with `sync.Cond`.** `append` on a package slice is E2013 (7.9), so the queue is a
   `vec.Vec[Job]`, with `var c sync.Cond`. Producers push under `mu` and call `c.Signal()`, which needs no
   lock. Workers run `mu.Lock(); for q.Len() == 0 { c.Wait(&mu) }; j, _ := q.Pop(); mu.Unlock()`. It
   compiles: the `Wait` counts as `mu.Unlock()` then `mu.Lock()`, so `mu` is held at `q.Len()` and `q.Pop()`
   (rule 1), and `Pop` loads the backing memory under `mu` with no release before the use (rule 2 case 4).

### Attacks on the Once and Cond rules

None was compiled; the results follow from the rules as written.

- Once, a second task that reads before any `Do`: it has no `Do` of its own on every path. E3503 noting the
  Once. Closed.
- Once, an `f` that starts a reader task: the reader's own reads need its own `Do`. Without one, E3503; with
  one, its `Do` returns only after `f` has returned, so it is accepted, correctly. Closed.
- Once, a write the analysis might miss: writes through func values and interfaces reach every possible
  target, a method value's method counts as outside, and a write the compiler cannot name counts as a write
  of the type. E3502. Unsafe code is not checked. Closed.
- Once, a view kept after the object is dropped: E3313 (18.3). Closed.
- Once, a `Do` in a helper called only on some paths: the `Do` must be on every path. E3503. Closed.
- Once, `f` calls `rt.ExitTask` and another task calls `Do`: the Once is not done, so that `Do` runs its own
  function before it returns. No read sees data `f` left half written as initialized. Closed, with the
  library change in item 218.
- Once, a hand-written double check with an `atomic.Bool` and a mutex: the fast-path read has no guard and no
  Once. E3503. A false positive; the fix is `sync.Once`. Recorded.
- Cond, a view kept across `Wait`: the second block of 19.3. The guard was released between load and use.
  E3501. Closed.
- Cond, a view kept across `mu.Unlock(); mu.Lock()`: the same case 4 condition. E3501. Closed (the
  tightening above).
- Cond, `Wait` with the wrong lock: `c.Wait(&mu2)` while `mu1` guards the data. It counts as unlocking and
  locking `mu2`; `mu1` stays held, so the `mu1` data stays guarded. If `mu2` is not locked, the run-time check
  stops the program. Closed, except for the finding below.
- Cond, two Conds on one lock: each `Wait` counts as the same Unlock and Lock. Accepted, correctly.
- Cond, `c.Wait(rw.RLocker())`, or a `Wait` through an interface or a func value: the lock cannot be named, so
  no lock is held after it. Reads after it are E3503 naming the `Wait`. Closed; a false positive at worst.

Open finding, not fixed: an `Unlock`, or a `Wait`, of a lock the task does not hold can release another
task's lock without any error. `Mutex.Unlock` checks only that the mutex is locked, not by whom, and the guard
inference counts locks per task, so it does not see the release. This holds for a plain `Unlock` today, not
only for `Wait`. No rule here can close it without one of these:

- an owner check in `Mutex.Unlock` and `Cond.Wait`, which is a run-time cost (a task id stored on Lock and
  compared on Unlock);
- a compile rule that an `Unlock` or a `Wait(l)` needs `l` held on every path in the same function, with an
  annotation or a false positive for a lock taken in one function and released in another. It needs a code,
  so the spec would say "a compile error" until one is assigned, as for G5.

The coordinator decides; no roadmap item was added for it. (Decided later the same day: D108, a compile
rule across functions with no annotation, in the section "Releasing a lock" below; item 220. It also
makes the `RLocker` and unnamed-`Wait` cases above compile errors, and `c.Wait(&mu2)` with `mu2` not held.)

### Known-gaps lines for Once and Cond

Added to `skill/plutonium/references/known-gaps.md`: in section 3, a read after `Once.Do` is still E3503
(item 218), and `Cond.Wait` does not count as Unlock then Lock and a view across it is not yet E3501 (item
219); in section 2, `NewCond` still returns a raw pointer with an `L` field and `Wait` takes no lock (item
197), and `Once.doSlow` does not release its lock or leave the Once not done on `ExitTask` (item 218).

## Releasing a lock

D108 closes the open Unlock finding above. The rule is in 19.3 (Releasing a lock, and the Accesses-and-locks
bullets), with notes in 17.1, 18.12, 19.7, 20.10, F.5, A and memory-model.md 4.1. Item 220 makes the compiler
change.

The rule: every `Unlock`, `RUnlock` and `c.Wait(l)` must release a lock that the guard inference shows the
calling task holds, on every path to the call, for writing (`Unlock`, `Wait`) or for reading (`RUnlock`).
Otherwise it is a compile error. It covers `sync.Mutex`, `sync.RWMutex` and `rt.Lock`, in tasks and in
interrupt handlers. A lock the compiler cannot name cannot be shown held, so its release is an error; this
replaces "unlocking a lock the compiler cannot name counts as unlocking every lock".

The details settled:

- Names. A package variable that is not a pointer, or a local or parameter, then field selections and
  constant array indexes; only the variable itself may hold a pointer. A local that holds a pointer names
  nothing if its address is taken or a function literal assigns it. A pointer loaded from memory is not a
  name, since another task may change it between the `Lock` and the release (attack 3 below). At a call, a
  parameter names what its argument names. So the old "named by where the pointer came from" is enough at
  the release only when "where it came from" is a name in this sense at every call.
- Copies carry names (`cur = next`); a release through one name ends all names of that lock. Assigning a
  variable ends the names through it. Assigning a new value to a lock, or moving or assigning the memory
  that holds it, ends its name, in the function or a callee. A copy of a struct holds a different lock.
- `defer mu.Unlock()`: held at the `defer` and at every point after it, since a panic may run the
  deferred call anywhere after it. A `c.Wait(&mu)` there keeps it held: F.5 now requires `Wait` to make
  its checks that may panic before it unlocks. Deferred calls run in reverse order.
- `TryLock` and `TryRLock`: held in the branch where the call returned true, which includes the code after
  `if !mu.TryLock() { return }`. A result stored in a variable first is not followed.
- Loops: a lock taken and released in one iteration compiles. A lock released after a loop must be held
  on every exit, the one that skips the loop included.
- Lock in a callee, unlock in the caller: a function that returns with the lock held holds it for its
  caller, mapped back through the argument (`x.lock()` then `x.mu.Unlock()`). Lock in the caller, unlock in
  a callee: allowed when every call of the callee holds the lock its argument names; the caller then no
  longer holds it.
- `func (s *S) lock()`: the receiver is a parameter, so the above applies.
- Recursion: a release in a recursive function needs the lock held at every call, the recursive calls
  included, for chains of any depth (the largest consistent answer over every chain of calls).
- A pointer parameter: named at the release in the callee by the argument at each call. That is enough
  when the argument is itself a name; `rel(g.p)` passes a fresh load and is not.
- `locks[i].Unlock()`: a variable index is not a name. The form is `l := &locks[i]; l.Lock(); ...;
  l.Unlock()`. See the extension below.
- `sync.Locker` through an interface: not a name, so an error. Pass `*sync.Mutex`, or use a type
  parameter.
- A stored method value `u := mu.Unlock`: a method value counts as called with no lock held, so `u()` is
  an error.
- `Unlock` in a deferred function literal: a literal called where it is written, or deferred, is part of
  its function at the point it runs, so `defer func() { mu.Unlock() }()` compiles. The body of a
  range-over-func loop is part of its function too (12.11's `find`, with `defer it.mu.Unlock()` in the
  body, compiles).
- `withLock(&mu, func() {...})`: the helper locks and unlocks its own parameter, so it compiles. The func
  value runs with the lock held for the guard inference, but counts as called with nothing held for
  releases, so a literal that unlocks `mu` itself is an error.
- Generic code over `[L sync.Locker]`: checked per instantiation; a method of `L` is the type argument's
  method. It compiles for `*sync.Mutex` and is an error when `L` is the interface `sync.Locker`.
- Tasks, interrupt handlers, and functions that `s.Task`, `wg.Task` or a task statement start begin with no
  lock held.
- Library and runtime. `pluto/rt` (the scheduler, channels, `rt.Lock` itself) is already outside the race
  rules. In `pluto/sync`, `RWMutex.Unlock` releases its inner mutex, taken in `RWMutex.Lock`; the `RLocker`
  result and `Cond.Wait` release through a `Locker`. These bodies are added to 18.12's list as trusted, and
  the rule is checked at their calls. `Once.doSlow` and `WaitGroup.Wait` lock and unlock their own fields
  in one function and pass the rule. User code needs no exemption.
- The run-time check stays: `Mutex.Unlock` of an unlocked mutex calls `fail("sync: unlock of unlocked
  mutex")`, and `RWMutex` has the same for `Unlock` and `RUnlock`; 17.1 now lists it, reachable only from
  unsafe code. `rt.Lock.Unlock` has no such check. No owner is recorded.
- Interrupt handlers and `rt.Lock` (20.10): the same rule. A handler starts with nothing held, so it may
  release only an `rt.Lock` it took, after `Lock` or in the branch of a successful `TryLock`.

### Go idioms under the release rule

None was compiled (the compiler has no check yet); the results follow from 19.3 as written.

1. `mu.Lock(); defer mu.Unlock()`: compiles. The `defer` bullet: held at the `defer` and at every point
   after it.
2. Lock, then early-return branches that each unlock: compiles. Each `Unlock` is on a path where `mu` is
   held since the `Lock`, and the path returns after it. A branch that unlocks twice is an error.
3. `RLock`/`RUnlock` around a read: compiles; `RUnlock` needs the read hold. `rw.RLock(); rw.Unlock()` is
   an error (wrong mode).
4. Upgrade by `RUnlock` then `Lock`: compiles; each release matches its mode. The condition must be read
   again after `Lock`; a view kept across the gap is E3501 (rule 2 case 4).
5. A struct with an embedded `sync.Mutex`: compiles. `c.Lock()` is `c.Mutex.Lock()`, a field of the
   receiver.
6. Hand-over-hand locking in a linked list: compiles with locals, `prev := l.head; prev.mu.Lock(); for {
   cur := prev.next; ...; cur.mu.Lock(); prev.mu.Unlock(); prev = cur }`. The copy carries the name, so at
   the loop head `prev.mu` is held on both paths (Assignments and moves). `l.head.mu.Unlock()` written
   through the field is an error (a pointer loaded from memory); copy it into a local.
7. `stripes[h%n].Lock()` ... `stripes[h%n].Unlock()`: does not compile; a variable index is not a name. The
   form is `s := &stripes[h%n]; s.Lock(); ...; s.Unlock()` (Variable indexes). Locking every stripe in one
   loop and unlocking them in another does not compile in any form; use a table-wide `RWMutex` that the
   stripe users read-lock and the whole-table operation write-locks.
8. A `sync.Cond` wait loop: compiles, with or without `defer`: `mu.Lock(); for !ready { c.Wait(&mu) };
   ...; mu.Unlock()`. `Wait` needs `mu` held for writing and keeps it held.
9. `func (s *S) withLock(f func()) { s.mu.Lock(); defer s.mu.Unlock(); f() }`: compiles (Calls; the
   receiver is a name).

Also: a `Locker` parameter (error; pass the concrete type or use a type parameter), `u := mu.Unlock`
(error; call `mu.Unlock()`), `ok := mu.TryLock(); if ok {` (error; test the call in the `if`), and handing a
locked mutex to another task to unlock (error; a channel, a buffered channel as a semaphore, or a
`sync.Scope`).

### Extension considered for index expressions

The case is idiom 7. The smallest extension: two index expressions name the same element when the
array is a name, or the slice is a local that is a name, and the index expressions are written the same,
made of constants, operators, conversions, `len`, `cap`, and locals that are names. That is sound by the
same argument as for locals. Not adopted: the pointer form is one line longer, it names the lock and the
data of the same element for the guard inference, which the index form would not do without more rules,
and the extension would add a second notion of equal expressions to the inference. Recorded here for a
later decision if real programs need it.

### Attacks on the release rule

None was compiled; the results follow from the rules as written.

1. Aliasing: `p` and `q` point to one mutex, `p.Lock(); q.Unlock()`. `q` was never locked under its name,
   so it is not held. Error. With `q := p`, the copy names the same lock, and after `q.Unlock()` a
   `p.Unlock()` is an error since the release ended both names. Closed.
2. An alias the compiler cannot see, held twice: two `Lock`s of one mutex under different names wait for
   good, and a `TryLock` under the second name fails, so its branch does not run. Two `RLock`s succeed and
   each name is released once, which matches the read count. Closed.
3. A pointer loaded again: `g.p.mu.Lock(); ...; g.p.mu.Unlock()`, with another task replacing `g.p` under
   its guard in between and locking the new object's mutex. The release would unlock that task's lock.
   Closed by the definition of a name: a pointer loaded from memory is not one. Found while writing the
   rule.
4. A local pointer changed behind the rule's back: by a literal (`f := func() { p = &b }; p.Lock(); f();
   p.Unlock()`), through its address, or by a scoped task that captured it. Such a local is not a name.
   Error. Closed.
5. A lock inside a moved or copied struct: a move ends the names (`q := take(o); q.mu.Unlock()` is an
   error), and a copy holds a different lock that is not held. Closed.
6. A lock reset by assignment: in the holding task, `mu.Lock(); reset(); mu.Unlock()` with `reset` doing
   `mu = sync.Mutex{}` ends the name in the callee, so the release is an error. In another task, the reset
   would let a third task lock the new value while the first still runs its critical section. A guard
   held at each method call does not stop this, since a critical section lasts past the calls. Closed by
   making such an assignment E3502 when another task may use the lock. Found while writing the rule.
7. `Cond.Wait` with an aliasing name: `p.Lock(); c.Wait(q)` with `q` an alias the compiler cannot see is an
   error, `q` not being held; `c.Wait(q)` with `q := p` is the same lock. Closed.
8. `sync.Locker` values: a call through the interface is not a name, `c.Wait(rw.RLocker())` included, and
   a generic function instantiated with the interface type is checked as an interface call. Error. Closed.
9. A re-lock by another task between this task's `Lock` and `Unlock`: another task can take the lock only
   after a release. The first wrong release in any run would need a release that the rule accepted of a
   lock its task did not hold. A held name means the task took that hold and has not released it under
   any name known to be the same lock; unknown aliases cannot both hold for writing (attack 2); a reset is
   attack 6. Only unsafe code is left. Closed.
10. Closures run by another task: a function given to `s.Task`, `wg.Task` or a task statement starts with
    nothing held, so `mu.Lock(); s.Task(func() { mu.Unlock() })` is an error. A function given to
    `once.Do` is a func value and counts as called with nothing held, so it cannot release its caller's
    lock either. Closed.
11. A panic between a release and a retake after `defer mu.Unlock()`: the lock must be held at every point
    after the `defer`, so `mu.Unlock(); work(); mu.Lock()` there is an error. A panic inside `Cond.Wait` is
    covered by F.5's requirement. Closed.
12. An interrupt handler that releases an `rt.Lock` of the code it interrupted: the handler starts with
    nothing held. Error. Closed.
13. A method value: `u := mu.Unlock; mu.Lock(); u()` is an error. Closed.
14. Recursion with mismatched depth: `func f(n int) { if n > 0 { f(n - 1); mu.Unlock() } }` after one
    `mu.Lock()`: after the recursive call the lock is not held on every path, so the release is an error.
    Closed.
15. A callee that releases, called from a site that does not hold the lock: the release in the callee is
    an error, naming that call. Closed.
16. A parameter reassigned in the callee before the release: the assignment ends the name. Error. Closed.
17. A task that ends with a lock held: other tasks wait for good. A deadlock, not a race; the rule does not
    cover liveness. Recorded.
18. A stored `TryLock` result, and locking every element of a slice in a loop: rejected though safe. False
    positives with the forms above. Recorded.

## Diagnostics that need a code in phase 1

None of these rulings invents a code. They extend E3313 (A1), E3212 (C1), E3302 (C3), E2313 (C4) and E2511
(B7), whose catalog texts must widen. The new run-time panic "arena has live counted objects" (B1) needs no
code. Audit D adds no code either: it widens E3313 (D3 and the candidates), E3303 (D5), E3501 and E3404
(D1). Audit E adds no code: it widens E3313 (E1, E5), E3212 (E2) and E3402 and E3404 (E4), and renames the
arena panic to "arena has live allocations" (E3). Audit F adds two errors that need a code: a `//pu:stack` size
outside 64K to 64G (F12, section 20.3) and a program that exceeds an implementation limit (F15, section 21.4).
The spec calls each "a compile error" until the codes are assigned (item 207). Audit F also widens E3006 (F3) and
E3313 and E3314 (F2).

Audit G adds one error that needs a code: an operation that may park inside a non-migrating region (G5,
section 19.3). E2584 is defined only for interrupt handlers, so the spec calls it "a compile error" until a
code is assigned (item 210). Audit G also widens E3313, E3314 and E3009 (G1), and E2584, E3501 and E3404
(G2). The panics it lists in 17.1 ("arena: out of memory", "blocking with preemption disabled", "sync: two
tasks wait on one Scope", "generation checks need 4-level paging") need no code.

Audit H adds no code. It narrows the G5 error to exact paths (H2), widens E3313, E3501 and E3404 to arena
frees of every type (H1), and names E3313, E3314 and E3501 for views at an owner transfer (H5).

Audit I adds no code. It widens E3501 to views loaded from package variables, `synced` objects and maps
(I1) and E2010 to a package variable that holds a `shared[T]` used by two tasks (I1). The wait error of
G5 (item 210) still needs a code; I2 changes which calls it follows, not its form.

Audit J adds no code. It narrows E3501 for guarded owned memory and static objects (J1, J2), widens E3501 to
a view used after its guard is released (J1) and to a handler's load before the first task (J3), and uses
E3503 for the `sync.Once` and `sync.Cond` shapes it leaves unresolved.

The Once and Cond rulings (D105, D106) add no code. They narrow E3502 and E3503 for data a Once
initializes and for a wait loop with a named lock, give E3503 a note naming the Once or the `Wait` whose
lock could not be named, and widen E3501 to a view kept across a release of its guard, a `Cond.Wait`
included. The open Unlock finding would need a code if it becomes a compile rule.

The release rule (D108) adds one error that needs a code: an `Unlock`, `RUnlock` or `c.Wait(l)` of a lock
that the calling task is not shown to hold, in the matching mode, on every path. E3503 is a read without a
guard and E3502 a write without a common lock; neither covers a release. The spec calls it "a compile error"
until a code is assigned (item 220). It widens E3502 to the assignment of a new value to a lock that another
task may use. The E3503 that D106 gave reads after a `Wait` whose lock could not be named no longer arises,
since that `Wait` is now the new error.

## Race rules rewrite

On 2026-11-10 the coordinator asked for 19.2, 19.3 and the Sharing part of 19.4 to be written again as one
text: a model that defines each term once, then the rules, each stated once with its code, then a
conformance table. The same pass settles audit K ([2026-11-09-spec-audit-k.md](2026-11-09-spec-audit-k.md)).
D109 onward in [decisions.md](../decisions.md) record it.

### Checklist of the old rule sentences

Every rule sentence of 19.2, 19.3 and 19.4 Sharing at 85312df7, before the rewrite, by paragraph. The
mapping below gives each one its new place, or says why it was dropped.

19.2 The sendable rule:

- C1. A value crosses as an argument or receiver of a task statement, or a func value given to a function
  that starts a task with it.
- C2. A value crosses when sent on a channel or received from one.
- C3. A value crosses when held inside an interface, struct or array that crosses.
- C4. A variable a scoped task uses, taken or captured, crosses.
- C5. A load of a value that holds a view, from a package variable (per-CPU included), a `synced[T]`
  object, map contents, or memory reached through them, crosses to the loader; it counts in the task's
  code and callees, atomic loads included, once a task other than the main task, or a handler, may run.
- C6. What a value that holds a view is.
- C7. A handler that loads such a value is a party in the same way.
- C8. Every crossing value must be sendable and may not hold a view the other party could use while it
  changes.
- C9. A type holding `shared[T]` or `weak[shared[T]]` is not sendable: E2010 at the task statement, the
  channel type or the send.
- C10. Converting such a value to `error` or an interface embedding it: E2020.
- C11. A frame view: E3004 (task statement), E3005 (send), E2011 (seen by the type checker).
- C12. A view of heap or static memory that may change while both use it: E3501.
- C13. Moving an owner is safe; the owner takes its managed memory.
- C14. `synced[T]` and maps let several tasks reach memory, so 19.3 governs it.
- C15. Strings are sendable: bytes never change, counts are atomic once a task has started.
- C16. A string from `unsafe.String` is not counted; sending one is the unsafe code's responsibility.
- C17. The type part is checked by the type checker on task arguments, receivers and channel element
  types, looking through fields, elements, payloads, boxes, pointers, slices and the wrappers.
- C18. The view part is checked where values cross, and through interfaces for every converted type,
  except `error` and interfaces embedding it.
- C19. A conversion whose result is a frame view is not counted.
- C20. In generic code the rule is checked per instantiation.

19.3, introduction:

- R1. Definition of a data race.
- R2. Safe code has none; the compiler rejects every program that could make one.
- R3. Whole program, no annotations, three inferred rules.
- R4. Shared memory is owned by one task, unchanged, atomic, or guarded by a lock every access holds.

19.3, The tasks:

- T1. The tasks: task statements, func values given to task-starting functions, `s.Task` calls.
- T2. Installed interrupt handlers are parties.
- T3. The main task runs package initialization and `main`.
- T4. A task statement that may run more than once may start several tasks of the same code at once.
- T5. Package initialization, and `main` before the first statement that may start a task, run while no
  other task exists.
- T6. No handler is a party until the first statement that may install one or enable interrupts.
- T7. From then on, writes are checked against handlers and handler loads cross.

19.3, Accesses and locks:

- A1. Per task: reads and writes in its code and callees; an interface or func-value call reaches every
  candidate; the locks held at each access.
- A2. A lock is a `sync.Mutex`, `sync.RWMutex` or `rt.Lock`; a handler cannot take a `sync.Mutex` (E2584).
- A3. Held from `Lock`, `RLock` or a successful `TryLock`/`TryRLock` to the release, or to the end of the
  function after `defer`.
- A4. Held at a statement only if held on every path.
- A5. A function returning with a lock held holds it for its caller; a lock held around a call counts in
  the callee.
- A6. A lock reached through a pointer is named by where the pointer came from.
- A7. Tasks, handlers and functions started by `s.Task`, `wg.Task` or a task statement begin with no lock.
- A8. Every release must release a held lock; releasing an unnamed lock is a compile error.
- A9. `c.Wait(l)` counts as `l.Unlock()` then `l.Lock()`; `l` names a lock held for writing.
- A10. A view loaded before `Wait` has crossed a release.
- A11. A `Locker` in a variable, `RLocker()`, or `Wait` through an interface or func value names no lock:
  a compile error.
- A12. `Signal` and `Broadcast` need no lock.
- A13. A method value's method counts as run with no lock held.
- A14. Channel, `WaitGroup` and task-end ordering is not used; the Scope join and the return of `Once.Do`
  are.

19.3, Releasing a lock:

- L1. Every release must be of a lock held on every path, in the right mode; otherwise a compile error
  naming the call and a path.
- L2. Interrupt handlers follow the same rule.
- L3. No annotation, no run-time cost.
- L4. Each task releases only the read lock it took.
- L5. A locked lock cannot be handed to another task; use a channel, a Scope or a buffered-channel
  semaphore.
- L6. A name: a non-pointer package variable, or a local or parameter, then fields and constant indexes;
  only the variable may hold a pointer.
- L7. `s.next.mu` is not a name; copy the pointer into a local.
- L8. A local whose address is taken, or that a literal assigns, names nothing.
- L9. A parameter names, at each call, what its argument names.
- L10. Assigning a variable ends every name through it.
- L11. Copies carry names; a release through one ends all.
- L12. Assigning a new value to a lock, or moving or assigning the memory holding it, ends its name.
- L13. A copy of a struct holding a lock holds a different lock, not held.
- L14. Assigning a new value to a lock another task may use: E3502.
- L15. Moving or freeing memory that holds such a lock is a write.
- L16. A variable index is not a name; take a pointer once.
- L17. `TryLock`: held in the branch where it returned true.
- L18. `defer mu.Unlock()`: held at the `defer` and every point after; `Wait` keeps it; reverse order.
- L19. Loops: held between take and release in one iteration; a release after a loop needs every exit.
- L20. Calls: a callee returning holding maps through the argument; a release in a callee is allowed when
  every call holds the lock; the caller then no longer holds it.
- L21. Interface, func-value and method-value callees count as called with nothing held for this rule;
  `withLock(&mu, f)` compiles.
- L22. Recursion: every call, recursive ones included, must hold the lock.
- L23. Literals called in place or deferred, and range-over-func bodies, are part of their function; other
  literals are func values.
- L24. Generics: per instantiation; a method of a type parameter is the type argument's.
- L25. `Unlock` through an interface is a compile error.
- L26. The lock methods and `Cond.Wait` are trusted; the rule is checked at their calls.
- L27. `pluto/rt` is outside the rules.

19.3, Non-migrating and interrupt-disabled regions:

- G1. Regions are found as held locks are: every path, across calls, deferred releases at the end.
- G2. Definition of a non-migrating region.
- G3. Definition of an interrupt-disabled region, handler bodies and trap handlers.
- G4. Exceptions and NMIs are not excluded.
- G5. 20.9 uses both, also for a dependent store.
- G6. The region calls are not unsafe; a wrong pairing is a liveness failure.

19.3, Waiting in a non-migrating region:

- W1. An operation that may park, reached inside a non-migrating region, is a compile error.
- W2. The list of waiting operations.
- W3. Why: the task would hold its processor or a spun-on lock.
- W4. The calls followed: those with a single known target.
- W5. Reported when every path to the wait lies inside the region; a wait on some paths of a callee too.
- W6. Calls not followed: interface, func value, method value, hook, unnamed arena, allocator.
- W7. The check starts only outside `pluto/rt`.
- W8. The run-time test in `rt.Park` and at `//pu:blocking` entry, and what it tests.
- W9. The promise covers waiting operations, not spin loops.
- W10. Why this one check accepts on doubt.

19.3, Guards:

- U1. The guard: a lock every write holds for writing.
- U2. Data is safe when every access holds the guard; a read may use `RLock`.
- U3. Data accessed only atomically needs no lock.
- U4. Datum granularity: a field; all elements of an array or slice together, except constant indexes; a
  variable index is every element; a whole-struct write writes every field.

19.3, Frees are writes:

- F1. Dropping, freeing or moving an object, or assigning or dropping an owner holding it, writes the
  whole object.
- F2. An arena `Reset`, `Release` or `Drop` frees every object allocated by `New` or `Slice`, every type;
  drop entries count on top.
- F3. A live `Alloc` allocation makes the call panic, so nothing is freed.
- F4. An arena is named as a lock; an unnamed one counts as every arena it may be, beyond the limits every
  arena.
- F5. The name follows a move.
- F6. This holds for every rule and for scoped tasks.
- F7. So no task frees memory another task may use.
- F8. `pluto/sync` and atomic values synchronize themselves; assigning a whole value is a write.

19.3, Rule 1:

- P1. A package variable used by two tasks, or by the tasks of one statement, and written after tasks
  start, must be atomic or guarded.
- P2. Per-CPU variables follow 20.9.
- P3. It includes the memory it owns.
- P4. A view loaded from it crosses; the pointee follows rule 2.
- P5. A non-sendable package variable used by two tasks: E2010.
- P6. E3502: written with no common lock.
- P7. E3503: read without its guard.
- P8. Written only during initialization or before the first task: readable by all without a lock, with
  its owned memory; a loaded pointer still crosses.

19.3, Lazy initialization:

- O1. Data a Once initializes needs no lock; a package Once for package variables, a field Once for
  fields of its object.
- O2. The Once is named as a lock.
- O3. Readable without a lock; the return of `f` is synchronized before every `Do` return.
- O4. Writes during `f`: in `f` and callees on `Do`'s path; tasks `f` starts, method values, and other
  paths are outside; an unnamed target is all data of the type; initialization writes do not count.
- O5. A read after `Do`: in the reading function or a direct callee on every path; a `Do` by another task
  or through an interface, func value or method value does not count; a handler cannot call `Do`.
- O6. Otherwise rule 1; the Once's lock is the guard; E3502 and E3503; two Onces: E3502.
- O7. When `f` does not return.
- O8. Several functions on one Once.
- O9. A Once assigned, moved or dropped initializes nothing; a copy is another Once.
- O10. Views of the data; dropping an owner outside `f`; E3313 for a counted reference.

19.3, Rule 2:

- V1. A crossing pointer or slice must point to memory safe for both, wherever loaded.
- V2. Case 1: sync and atomic types, runtime records, channels, structs of them.
- V3. Case 2: package memory unchanged after tasks start, or Once-initialized and read after `Do`.
- V4. Case 2, static objects: definition, no free condition, field conditions, `take(p)` and `*p = v`
  named, reassigning the variable.
- V5. Case 3: a `synced[T]` object and its Once memory.
- V6. Case 4: memory a package variable or `synced[T]` object owns, every access under the guard, the view
  used only while it stays held with no release; lock around a call; copies that cannot be followed;
  deferred arguments.
- V7. Case 5, the type rule: no overwrite, free, drop or move of an object of the type once tasks start;
  field conditions; initialization writes; freed objects are not safe.
- V8. A non-struct crosses only in the first four cases.
- V9. Otherwise E3501 naming a write; the fixes.

19.3, Rule 3:

- S1. Changing fields of a `synced[T]` object: a lock of the object, atomic, or a Once of the object;
  E3502, E3503.
- S2. Writes before the reference is first copied do not count.
- S3. A pointer method on the box uses the object.
- S4. Map contents are shared by every copy; writes hold one guard, reads too; the guard is a package
  lock or a lock of the struct holding the map.
- S5. A map used by one task at a time needs no lock.
- S6. Scoped tasks writing a map need a guard.
- S7. All maps of one type are one datum once one crosses; maps filled before sharing and types that
  never cross need no lock.

19.3, Interrupt handlers:

- H1. A handler may run wherever interrupts are enabled, on any processor, on several at once.
- H2. Rules 1 to 3 apply with handlers as parties.
- H3. Data a handler shares must be atomic or guarded by an `rt.Lock`, or be per-CPU.
- H4. Data not written while a handler may run needs neither.
- H5. Handler loads cross; the record must be safe by rule 2.
- H6. An `rt.Lock` is a lock for the inference.
- H7. `volatile` and interrupt-disabled regions exclude only the same processor.
- H8. An allocator has no exemption.
- H9. Unsafe parts of an allocator are its own responsibility.

19.3, Owner transfer requires exclusivity:

- X1. A move is valid only with no other live view; E3313, E3314, E3501.
- X2. Generation checks alone do not prevent races.
- X3. `task c.bump()` hands over an `owned[Counter]` only under that proof.

19.3, What is not checked:

- N1. Unsafe code, `unsafe.Pointer`, `rt.Pinned`, bodyless functions, the runtime's variables, records and
  regions.
- N2. The runtime's regions are not checked for waits, nor its releases.
- N3. The trusted lock bodies are not checked; their callers are.
- N4. A race in unsafe code gives torn or stale values, never undefined behavior.

19.4, Sharing:

- Z1. A place a scoped task uses and anyone writes during an interval must be one of three forms.
- Z2. Frees are writes here too.
- Z3. Form 1: synchronized by its type; assigning a whole value is a write.
- Z4. Form 2: every access holds one lock, inferred as in 19.3.
- Z5. Form 3: partitioned.
- Z6. A place nobody writes is readable by all; anything else is E3402.

### Where each sentence went

New labels are the bold labels of 19.3 after the rewrite. "Kept" means the sentence keeps its meaning in
the new place; "changed" gives what changed and the audit K finding behind it.

19.2:

- C1 to C4: Crossings, cases 1 to 4. Kept.
- C5: Crossings, case 5 (static objects added). The "once a task other than the main task, or a handler,
  may run" condition is now The start of sharing, which enabling interrupts also starts (K4).
- C6: Crossings, "holds a view". Kept; unions added.
- C7: Crossings, case 5, which says "by a party", and handlers are parties. Kept.
- C8 to C11: 19.2, first list. Kept.
- C12: 19.2, third bullet, and rule 2. Kept.
- C13, C14: 19.2, second paragraph. Kept.
- C15: 19.2. Changed wording only: counts are atomic "once a program has started a task".
- C16 to C20: 19.2. Kept.

19.3 introduction and tasks:

- R1 to R4: 19.3, first paragraph. Kept. "Three inferred rules" became "the rules"; the count is now
  rules 1 to 3 plus the release, wait, copy and owner-transfer rules.
- T1, T2, T3: Parties. Kept; `wg.Task` added to the list.
- T4: Parties, "repeated". Kept; handlers are now repeated by definition.
- T5, T6, T7: The start of sharing (K4). Changed: one point for tasks, handler installs and enabling
  interrupts, instead of two. Data written after `x86.Sti` but before the first install now counts.

Accesses and locks:

- A1: Accesses. Kept.
- A2: Locks and names. Kept.
- A3, A4, A5: Held sets. Kept.
- A6: Locks and names. Kept.
- A7: Parties, last paragraph. Kept.
- A8: Releasing a lock. Kept.
- A9: Held sets. Kept. The mode requirement (write mode for `Wait`) is in Releasing a lock (K12).
- A10: Views loaded under a lock, ending events. Changed: any release ends the view, even one taken again
  after (K10), and a call that may release ends it too (K3).
- A11: Releasing a lock. Kept.
- A12: Held sets. Kept.
- A13: Accesses. Kept, and "outside every `Do`" added.
- A14: Ordering used. Changed: `WaitGroup` named alone where it was paired with `Scope` in F.5 (K12).

Releasing a lock:

- L1 to L5: Releasing a lock. Kept.
- L6 to L12: Locks and names. Kept. L8 adds "or if a scoped task captures it".
- L13: Copying a lock (K14). Changed: a copy is now a compile error, not "a different lock, not held".
- L14, L15: Rule 1, fourth error bullet. Kept.
- L16: Locks and names. Kept.
- L17: Held sets, first bullet. Kept.
- L18 to L22, L24 to L27: Releasing a lock, and Held sets for L21 and L24. Kept.
- L23: Part of the enclosing function (K1). Changed: a range-over-func body is part of its function only
  when the iterator calls `yield` on the loop's own party. Otherwise it is a func value that starts with
  nothing held, and a `defer` in it is a compile error.

Regions and waits:

- G1 to G6: Non-migrating and interrupt-disabled regions. Kept.
- W1 to W10: Waiting in a non-migrating region. Kept.

Guards and frees:

- U1: The guard set (K6). Changed: the guard set is over shared writes only, and only locks that guard
  the datum in place count. One writer that uses `L` at one write and `M` at another has no guard.
- U2: The guard set, read bullet. Changed: a read needs a lock of the guard set only when another party,
  or another instance of its own party, may write. A sole writer that is not repeated reads freely.
- U3: The guard set, "atomic". Kept as a pseudo-lock, which makes mixed atomic and plain writes E3502.
- U4: Memory and data, and Accesses for the whole-struct write. Kept.
- F1 to F8: Frees are writes. Kept.

Rule 1 (old):

- P1: Rule 1 (new). Changed: covers package variables, static object fields, `synced[T]` fields and map
  contents in one rule.
- P2: Rule 1, last sentence of the paragraph after the errors. Kept.
- P3: Memory and data, "The memory an object owns". Changed: defined once, to any depth (K2).
- P4: Crossings case 5 and rule 2. Kept.
- P5, P6, P7: Rule 1, errors. Kept.
- P8: Initialization and rule 2 case 2. Kept.

Lazy initialization:

- O1 to O3: Lazy initialization. Kept.
- O4: Lazy initialization, "During a call of `f`". Kept. "Initialization writes do not count" is now
  The start of sharing.
- O5: Lazy initialization, "After a returned `Do`" (K13). Changed: a callee whose summary shows `Do` on
  every path counts at any depth, not only a direct callee. Code that is part of the reading function
  counts too (K9).
- O6: Lazy initialization, "Otherwise". Kept, and it now says the Once's own lock is in the guard set.
- O7, O8: Kept.
- O9: Lazy initialization, "Resetting". Changed: "a copy is another Once" dropped, since a copy is a
  compile error (K14).
- O10: Lazy initialization, "Views". Kept.

Rule 2 (old):

- V1: Rule 2, first paragraph. Kept.
- V2: Rule 2, case 3, first sentence. Renumbered.
- V3: Rule 2, case 2. Renumbered. Unchanged memory that is atomic in part is no longer case 2: it is case
  3 (K11).
- V4: Memory and data (the static object definition, `take(p)`, `*p = v`), rule 2 case 3 (the field
  conditions) and the paragraph after the cases (reassigning the variable). Split, not dropped.
- V5: Rule 2, case 3 (`synced[T]`). Renumbered.
- V6: Rule 2, case 4, and Views loaded under a lock. Changed: valid views replace "held with no release"
  (K3, K10). It now covers memory owned by a static object or a case 3 object (K2).
- V7: Rule 3, the type rule, used by case 3. Renumbered.
- V8: Rule 2, paragraph after the cases. Kept.
- V9: Rule 2, the E3501 paragraph. Kept.

Rule 3 (old):

- S1: Rule 1, the paragraph after the errors (guards in place), and rule 2 case 3. Kept.
- S2: Same paragraph. Kept.
- S3: Memory and data, `synced[T]`. Kept.
- S4: Memory and data (map contents), and Locks and names, which says which lock guards map contents in
  place. Kept.
- S5, S7: Rule 1, the paragraph after the errors. Kept.
- S6: Dropped as a separate sentence. Scoped tasks are parties, and rule 1 already covers map contents
  that two parties write; the frame of the starter follows 19.4.

Interrupt handlers:

- H1 to H3, H5 to H9: Interrupt handlers. Kept.
- H4: Interrupt handlers, now worded through the start of sharing (K4).

The rest:

- X1 to X3: Owner transfer requires exclusivity. Kept.
- N1 to N4: What is not checked. Kept.
- Z1 to Z3, Z5, Z6: 19.4, Sharing. Kept.
- Z4: 19.4, Sharing, form 2 (K5). Changed: a view loaded under the frame lock must be valid at each use,
  so a use after `Unlock` and a new `Lock` is E3402.

New sentences with no old one: the guard set, valid views and their ending events, returned views in
summaries, the may-release summary, Copying a lock, the precision limits, Ordering used, and the
conformance table.

### Attacks on the new rules

Each attack tries to make the rules accept a race, or reject a program that cannot race.

1. One writer, two locks (K6). Task A writes `x` under `L` here and under `M` there; task B reads under
   `L`. Every write's lock set misses the other, so G(x) is empty and the write is E3502. Holds (row 60).
   The checker accepts this today (item 221).
2. Sole writer reading without a lock. Task A is the only writer of `x` and writes under `L`; it also
   reads `x` with nothing held, and task B reads under `L`. A's reads cannot race with A's writes, and B
   holds `L`. Accepted, and sound. If A's start is in a loop, A is repeated, the read needs `L`, and it is
   E3503. Holds.
3. A lock of another object. `a.mu.Lock(); b.items.Push(1)`. `a.mu` does not guard `b.items` in place, so
   it is not in the guard set, and G is empty: E3502. Holds.
4. A package-level lock for heap objects. Every write of `n.count`, for any `n`, holds a package `mu`.
   One lock orders all of them, so accepting is sound. Holds.
5. Write under `RLock`. Two tasks write `x` while holding `rw.RLock`. A write under `RLock` does not
   count as holding the lock, so G is empty: E3502. Holds.
6. Mixed atomic and plain writes. One task stores with `atomic.StoreInt64(&x, 1)`, another writes
   `x = 2`. The plain write does not hold "atomic", so G is empty: E3502. A plain read of data written
   only atomically needs "atomic" and is E3503. Holds.
7. Re-taking the lock (K10). `mu.Lock(); p := items.Ptr(0); mu.Unlock(); mu.Lock(); _ = *p`. The
   `Unlock` is an ending event even though `mu` is held again at the use. E3501. Holds (row 63, and
   8.8 says the same).
8. A hidden release (K3). The view is used after a call to a helper that runs `c.Wait(&q.mu)`. The
   helper's summary says it may release `q.mu`, so the call ends the view: E3501. Holds (peekWait).
9. A returned view. A callee loads `p` under `mu` and returns it; the caller holds `mu` before the call
   only, and the callee unlocks. The caller does not hold `mu` across the whole call, so the view is not
   valid at the caller: E3501. Holds.
10. A view smuggled out. A view loaded under `mu` is stored in a package variable, sent on a channel,
    captured by a task literal or held in a method value. Each counts as used with no lock held: E3501.
    Holds (row 38).
11. Writes between `x86.Sti` and the first install (K4). Enabling interrupts starts sharing, so the write
    counts and the handler's read meets it: E3502. Holds (row 57). The call hidden in a callee counts
    too, since the start of sharing includes calls the statement makes.
12. A function called before and after the start of sharing. Its accesses count as after it. Holds; this
    can only reject more.
13. A range body run elsewhere (K1). `par` calls `yield` in a scoped task. The body is a func value that
    starts with nothing held, so its `mu.Unlock()` is a compile error, and a `defer` in it is a compile
    error. An iterator that stores `yield` where the compiler cannot follow it counts as calling it on
    another party. Holds (rows 52, 54). An iterator called through an interface with an unknown candidate
    counts the same, which can reject some programs that are safe.
14. A range body at home (K1, K9). `vec.Vec.All` calls `yield` in its own loop, so the body is part of
    the function, holds the loop's locks, and a `Do` before the loop counts for reads in the body. Holds
    (rows 53, 62).
15. Once deeper than one call (K13). `cfg()` calls `load()`, which calls `once.Do(f)` on every path.
    The summary of `load` records the Do, so the read in `cfg()` counts as after a returned Do.
    Accepted, and sound, since the return of `Do` is ordered after `f`. If `load` calls `Do` on some
    paths only, the read is E3503 (row 47). Holds.
16. A Once through a func value. `rt.CatchExitTask(func() { o.Do(f) })`, then a read. The Do is behind
    a func value, so it does not count: E3503. Holds; this rejects a safe program, and the text says so.
17. Copying a lock (K14). `c2 := c` for a `sync.Cond`, a value receiver on a struct holding a
    `sync.Mutex`, and a conversion to an interface. Each is a compile error. Holds (row 67). Cost: Go
    code that prints a struct holding a mutex no longer compiles; A.2 records the difference.
18. Moving an owner that holds a locked mutex. A move is not a copy, the name ends, and the receiver
    starts with nothing held. Accepted, and sound: the lock is still one lock in one place. Holds (row
    68).
19. Owned memory of a static object (K2). `std.buf.Push(s)` under `std.mu` in two tasks. `std.mu`
    guards `std.buf` in place, and the storage `buf` owns is part of that datum. Accepted. A `take(std)`
    would end its static status, and an unnamed `take` counts as a move of every static object of the
    type. Holds (row 55).
20. Owned memory of a per-CPU variable (K7). `runq.Push(t)` inside an interrupt-disabled region. The
    storage is per processor, and the view of it is bounded like `&v` (20.9, E2022), so it cannot be
    used after a migration. Holds (row 61).
21. Map contents. Two maps of one type: one crosses; the other is filled by a task after it hands the
    map to a function. All maps of the type are one datum, so the fill needs the guard. This rejects
    some safe programs; it never accepts a race. Holds.
22. A scoped view across a release (K5). Form 2 now uses valid views, so the use after `Unlock` and a
    new `Lock` is E3402. Holds (row 58).
23. Raising the precision limits. An implementation that follows three pointer loads accepts a program
    that another rejects, which 21.3 forbids. The text says the limits are exact. The checker's
    `widenPass` lowers a limit for slow groups, and `maxGroups` drops tasks, so the checker differs here
    (item 226).
24. A task in an uncalled function. Parties are found from package initialization and `main`, so a task
    statement nothing calls starts no party and the program is accepted. This is sound: that code never
    runs. Holds (row 72). It is also why the spec examples need a `main` that starts their tasks.

No attack found a race that the rules accept. Attacks 13, 16 and 21 show programs that are safe and are
rejected; each is a deliberate cost of having no annotations.

### Diagnostics that need a code

- The release error: a release of a lock not held on every path, in the right mode (item 220).
- A `defer` in a range-over-func body that the iterator may run on another party (item 223).
- Copying a lock (item 225).
- The implementation-limit error for a func value followed through too many calls (item 226, 21.4).

### The CI run and the examples

The coordinator compiled every block with the real compiler. The Config and Limits blocks passed, but not
because the checker knows `sync.Once`. The race check finds parties from package initialization and
`main`, and the test wrapper adds an empty `main` to a block that has none. The old blocks started their
tasks in a `start` function that nothing called, so the check saw no party and accepted them.

Both blocks now start their tasks from `main`:

- Config: two `serve` tasks call `port()`. The checker has no rule for `sync.Once`. It sees the write of
  `cfg` inside `f`, which holds the Once's own mutex, and a read after `Do` with no lock, so it should
  report E3503 at `return cfg.port`. The entry stays `known: 218`, with that reason. If the checker
  instead accepts the block, the entry should be removed.
- Limits: `reload` runs as a task and `main` calls `maxConns()`. The write in `reload` holds no lock, the
  write in `f` holds the Once's mutex, so no lock is common: E3502 at the `reload` line. The checker
  reports one E3502 per place and prefers the write with no lock, so the marker should match. No entry.

The new blocks (Logger, peekWait, par) also start their tasks from `main`.

### Where the model differs from the checker

These are the compiler's work list. Each is a roadmap item.

- The guard set is not yet computed from shared writes and in-place locks. The checker intersects each
  write's locks with the locks of the writes it conflicts with, so one writer under `L` and `M` with a
  reader under `L` is accepted (K6, item 221).
- Summaries do not record locks a callee may release, nor the locks a returned view was loaded under
  (K3, item 222).
- Summaries do not record a `yield` called on another party, so a range body that runs elsewhere is
  treated as part of its function (K1, item 223).
- Enabling interrupts does not start sharing (K4, item 224).
- Copying a lock is not rejected (K14, item 225).
- `widenPass` lowers the load limit, and groups past `maxGroups` are dropped instead of rejected
  (item 226).
- Views in scoped sharing do not end at a release (K5, item 227).
- The owned memory of a per-CPU variable is not bounded (K7, item 228).
- Crossing loads from shared memory, with static and type-rule objects and their owned memory (item
  214), `sync.Once` (item 218, which has no rule at all today), `sync.Cond` and `Wait` as a release
  (items 197 and 219) and the release rule (item 220).
