## 21. Implementation requirements

This chapter says what every conforming implementation must provide, beyond accepting the programs the
other chapters define. A conforming implementation:

- accepts every program that satisfies the rules of chapters 1 to 20, within the limits of section 21.4,
  and gives it the meaning those chapters define;
- reports a compile error for every program that breaks a rule of those chapters, as section 21.1
  requires, and does not run it;
- never accepts a program because it could not decide whether the program is safe. Every analysis the
  language defines (escapes, moves, closure regions, races, waits in interrupt context, which takes the declarations of functions without a body on trust,
  section 20.10) rejects what it
  cannot prove, and a false rejection is fixed by improving the analysis, never by weakening the rule
  (section 1.2, Design rules).

### 21.1 Diagnostics

Every compile error an implementation reports about a program is a **diagnostic**. A diagnostic has:

1. **A position**: the file, line and column where the error is, in the form `file:line:col`. An error
   that has no place in the source, such as a missing panic handler, has no position and names the
   program or package.
2. **A code**: `E` and four digits. The code names the rule that the program broke. The codes are grouped
   by where the rule comes from:

   | Codes | Rules |
   |---|---|
   | E0001 to E0999 | the general rules of types, declarations, expressions and statements |
   | E1000 to E1999 | syntax |
   | E2000 to E2499 | Plutonium rules checked together with types |
   | E2500 to E2999 | Plutonium rules checked later: calls, closures, directives, kernel mode, tasks |
   | E3000 to E3999 | ownership, moves, lifetimes and data races |
   | E4000 to E4999 | finding and loading packages |
   | E5000 to E5999 | assembling and linking |
   | E9000 to E9999 | errors in the implementation itself |

   A code never changes its meaning. A rule that is retired keeps its code.
3. **A one-line message** that says what is wrong, in the program's own terms: its identifiers, its types,
   its lines. The message never needs knowledge of how the implementation works. It does not mention
   intermediate representations, passes, registers or optimizations.
4. **An explanation** of its own, in the same terms, that says why the rule exists where the reason is not
   evident from the rule (no hidden allocation, no garbage collector, kernel safety), what the program did, and, for an
   error that follows a path (an escape, a move, a wait in interrupt context), the path itself, one
   step at a time.
5. **A fix**: code that the programmer could write instead, with the program's names where they are known.
   An error that has several ways out shows each.
6. **A pointer to the specification**: the number and title of the section that defines the rule, in the
   form `"Slice expressions" in the Plutonium spec, section 11.12`. Every rule points to the section of
   this specification that states it, and to no other document.

The implementation also provides:

- a command that prints a longer explanation of any code, with an example of the error and of its fix
  (`pluto explain E3001`);
- a brief form of its output, for tools that read errors, in which each error is only its position and
  one-line message: `file:line:col: message`;
- the same text in editors, wherever the implementation offers editor support.

The one-line message and the explanation are written in short sentences, with no term used before it is
defined, and in the spelling of this specification. The wording of the messages is not part of this
specification. Each rule is stated by the chapter that defines it, together with its code where it has one (section 1.3); the exact text
lives with the implementation, in one place, and its tests pin it. That keeps a single copy of each text
and lets the wording improve without changing the language.

Every code has an entry that holds an explanation and the section it points to. An implementation that
reports a code without such an entry is not conforming.

### 21.2 No silent failures

A construct either has the meaning this specification gives it, or it is a compile error. An
implementation never generates code that does something else.

- A construct that the implementation does not support yet is a compile error that says so (E2500). It
  is never ignored and never compiled to approximate code. An implementation that reports E2500 for a construct this manual defines is not yet conforming (introduction of this chapter).
- A directive that the implementation would not act on is a compile error (section 20.3, Directives), as
  is a `//go:` comment. A misspelled or misplaced directive never silently does nothing.
- A feature that a target cannot provide is a compile error that names the target and the feature, for
  example floating point in kernel mode (section 20.7).
- A required safety condition is proved or checked, or the operation is rejected. Omitting runtime
  lifetime protection requires the explicit unchecked choice of section 21.5; kernel mode alone does
  not select it. An implementation never performs a check wrongly, or half of it.
- A constant that does not fit its type, a constant shift out of range, and every other error that the
  chapters on constants, types, expressions and statements define as a compile error, is reported at
  compile time and never deferred to run time.

### 21.3 Acceptance does not depend on optimization

Whether a program is accepted, and which errors an implementation reports for it, does not depend on the
optimization level, on the inlining it performs, or on any other decision of the optimizer. An analysis
that rejects programs works on the program as written. It does not rely on the optimizer to remove a
construct before it decides. The reason is that a program that compiles under one setting and not under
another cannot be moved between them, and an error that appears only when the optimizer fails to
simplify something is an error that the programmer cannot explain.

Every optimization level preserves the language's permitted observable behavior, ordering, safety
and logical panic information. For a deterministic program with fixed inputs and sufficient resources,
output, exit status and the logical message/frames of the same panic path are exact; inlining must
preserve those source frames, not machine addresses. Unspecified map order, permitted schedules and
address-dependent output need not coincide across runs. Nondeterministic programs are checked against
permitted outcomes and invariants, not a single arbitrary run's output.

Specified resource-use differences, including stack consumption and resulting exhaustion, may vary;
this does not permit removing observable cleanup or side effects. Source acceptance is independent of
optimization. Debugging information does not change the generated code.

### 21.4 Implementation limits

An implementation may limit the size of the programs it accepts, such as the depth of nested expressions
and types, the size of a function, the number of packages, or the size of a closure region. It documents
its limits, and a program that exceeds one is a compile error that names the limit. It is never a crash or
wrong code.

The language itself fixes these limits, which every implementation provides:

- A function contains at most 64 `defer` statements, because each has a slot in the function's frame
  (E2531). A `defer` statement inside a loop is an error (section 12.19, Defer statements).
- A task's stack is at least `rt.MinStackSize` bytes, 8 KiB. A hosted task whose needs cannot be computed
  has at least 64 MiB of stack where the address space allows; where it does not, the runtime reserves
  smaller stacks, never below 64 KiB (section 19.1, Tasks). Overflowing a task's stack is a panic with
  a trace.
- The size of a closure region is fixed at compile time (section 11.5, Closures and their regions). A
  program whose region cannot have a fixed size is an error, never a region that grows.
- `//pu:stack` accepts sizes from 64 KiB to 64 GiB (section 20.3).

### 21.5 Run-time checks and the unchecked mode

Run-time checks change what a bug does. They never change what a correct program means. A conforming
implementation checks, at run time, everything that chapter 17 (Run-time panics) says is a panic: bounds,
slicing, division, type assertions, channel misuse, string slicing at a position that is not a rune
boundary, stack overflow, and the use of memory that has been freed. Unless statically prevented, a
use of freed heap memory is a run-time panic by the definition of the language: the views of heap memory are checked by **generation
checks**, and views into fixed and Chunked arenas by static lifetime proof or **arena epochs**.
An ordinary arena API must not silently leave its returned views unchecked when static proof is
incomplete (section 18.11, Generation checks).

An implementation provides a documented **unchecked mode**, in which the generation checks and the arena
epoch checks are not performed. The unchecked mode is not a dialect: a correct program behaves
identically with and without the checks, and a program that uses freed memory has no defined behavior
without them. An implementation provides the choice per build, and `//pu:nochecks` per function (section
20.3), which also removes that function's bounds checks. Unchecked execution must be explicitly selected, including for freestanding and kernel builds;
it opts out of the full lifetime guarantee while preserving all static rules. Kernel mode is not an
implicit opt-out. Every normal build requires static proof or suitable automatic protection. If the
target cannot provide protection and proof is incomplete, compilation rejects the operation with its
reason and a code-level remedy. A routine remedy must not be a safety-disabling annotation. An
implementation that cannot yet provide the protection for any build of a mode rejects each build of
that mode that does not select the unchecked mode, with a message that says how to select it; it does
not build such a program unchecked. The per-build unchecked mode and `//pu:nochecks` are the only
opt-outs.

Checks and metadata used during early boot or interrupts obey those contexts' allocation, waiting and
failure constraints and their specified allocation contracts. An implementation cannot silently
insert a forbidden operation or accept an unprotected access because its hosted pointer encoding
cannot represent kernel addresses (abi.md section 1.3). The representation and optimization of such
protection are implementation choices; this rule defines no new directive or hardware API.

Every check with a finite range fails loudly, never silently:
when the range ends, the implementation retires the memory or widens the field. Heap generations never
wrap. Arena epochs must not collide with stale views, including after reset, destruction or reuse by
another arena. This applies to hosted and kernel encodings; safe exhaustion handling occurs before
issuing an incarnation that could match a stale view (section 18.11;
abi.md section 1.3).

Package cleanup adds one check of its own: a typed write to a static place whose cleanup has finished is a
run-time panic (section 15.4). It runs only once cleanup has started. Before that, a store that may reach a
static object with a cleanup obligation tests one program-wide flag, a load and a branch; a store the
compiler proves cannot reach one has no test. The unchecked mode keeps this check, since it guards owners
and counts and not freed memory.

An `arena.Fixed` or `arena.Chunked` counts its live allocations (every allocation made through its `Alloc`
and not freed yet, owners and counted objects alike; not the plain views of `New` and `Slice`), and its
`Reset`, `Release` and `Drop` panic while the count is not zero (section 18.9, Allocators, Live
allocations). The cost is one atomic counter per arena and one increment and one decrement per allocation
made through `Alloc`. The unchecked mode keeps this check too: without it a reset could free the bytes of
a string, or the memory of an owner, that another task still uses, which no epoch check can catch.

The implementation provides a tool that lists unsafe code (section 20.1, Package `unsafe`): for each
package, the functions that use unsafe code, and the safe functions that call them (`pluto vet -unsafe`).

### 21.6 Layouts and calling conventions

An implementation documents the layout of every type, the calling conventions it uses, and the names of
symbols, for each target. For x86-64 this is the ABI document, [abi.md](../abi.md), which is part of this
specification. These facts are the same on every target:

- the sizes and alignments of section 20.2 (Size and alignment guarantees);
- the meaning of a program, which never depends on the layout of a union, the control word of a string,
  the contents of padding, or the order in which an implementation lays out its own tables;
- the C convention of `//pu:extern` and `//pu:export` functions, which is the platform's C convention
  (section 20.4).

### 21.7 Reproducible builds

The same source, the same implementation, the same flags and the same assembler, linker and C compiler
produce a byte-identical executable in any directory, on any machine, for any user. To make that so:

- file names in debugging information and in stack traces are relative to two fixed roots: the module
  root for the program's own files (`./cmd/tool/main.p`), and the import path for the standard library
  (`pluto/fmt/print.p`). A build can ask for absolute paths, and is then reproducible only in the same
  directory;
- object files carry no timestamps and no names of temporary files;
- symbols, sections and relocations are emitted in an order that the program fixes, never one that depends
  on the iteration order of a table;
- the linker is told to add no build identifier;
- compile error messages are not affected by the paths of the build.

Debugging information describes the program and changes nothing in the code that is generated.

### 21.8 Language version

There is one version of the language. The header of this specification carries its version number. A
module records no language version, and a feature is not enabled or disabled by one, until a release has
users whose code must keep compiling. An implementation states which version of the specification it
implements.
