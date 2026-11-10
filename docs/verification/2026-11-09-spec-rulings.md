# Specification rulings, 2026-11-09

**Status:** applied to the specification, version 0.4 (draft), 2026-11-09. This is phase 0.1 and 0.2 of the
[completion plan](../plans/2026-11-09-completion-plan.md).

**Inputs:** the audits [A](2026-11-09-spec-audit-a.md) (A1 to A8), [B](2026-11-09-spec-audit-b.md) (B1 to B22)
and [C](2026-11-09-spec-audit-c.md) (C1 to C14). The user decided A1, C1, C2, B1 (option a) and B3. The
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
| B5 | **Unresolved.** The proposed ruling (`%w` of a field error copies it into a counted error) makes `errors.Unwrap(e) == err` false and makes the pointer that `errors.As` stores differ from the field. In Go a wrapped pointer error is the same pointer and sees later changes, so the copy would be a false friend, and the audit's premise ("in Go a wrapped error never changes") is wrong for pointer errors. The audit's option (b) matches Go: 16.5 says only the message is copied, and `%w` keeps a view bounded by the object, which the escape analysis already enforces. Needs the user's decision. | FF | 16.5, 16.7 rule 4 (no change made) | Depends on the decision. |
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

## Diagnostics that need a code in phase 1

None of these rulings invents a code. They extend E3313 (A1), E3212 (C1), E3302 (C3), E2313 (C4) and E2511
(B7), whose catalog texts must widen. The new run-time panic "arena has live counted objects" (B1) needs no
code.
