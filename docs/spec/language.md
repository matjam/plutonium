# The Plutonium Language

**Version:** 0.3 (draft)
**Date:** 2026-10-15

Plutonium is a systems programming language for operating system kernels, drivers, boot code, allocators
and other code that runs without a managed runtime, and for the ordinary programs that run on top of them.
It is the language PlutoniumOS is written in. It has no garbage collector, and tasks and channels run on a
scheduler included only when a program or its dependencies request scheduler capabilities, and that a
kernel supplies itself (section 19.5).

The language is Go-familiar, not Go-compatible: it uses Go's syntax and idioms wherever they fit, and where
it behaves differently from Go in a way a Go programmer would not expect, the difference is visible in the
source (appendix A lists every difference). It gives ordinary application and kernel code
memory and race safety without lifetime or borrow annotations (owner types are declared with `owner`, section 7.18). When safety cannot be proved or
protected in the chosen environment, the operation is rejected. Deliberately unchecked execution is an
explicit opt-out (section 21.5). Allocation follows specified operation contracts and explicit lifetime
responsibility; locals never move to the heap implicitly. Chapter 1 states these goals, the four design rules and the four
standing principles that decide the cases the chapters leave open.

This specification is self-contained: it defines the whole language, and it never falls back on the Go
specification where it is silent. The standard library is documented with its packages (`pluto doc`);
appendix F names the few library names the language itself relies on. The x86-64 binary interface is in
[abi.md](abi.md), the memory model in [memory-model.md](memory-model.md), and debugging information in
[debugging.md](debugging.md).

Section numbers are unique across the whole specification, and each chapter is a file of its own in
[`language/`](language/). The diagnostics of the compiler cite sections by number and title.

## Contents

- [1. Introduction](language/01-introduction.md)
  1.1 Scope and goals; 1.2 Design rules; 1.3 Relation to Go, and how to read this specification; 1.4 Terms.
- [2. Notation](language/02-notation.md)
- [3. Source code representation](language/03-source-code-representation.md)
  3.1 Characters; 3.2 Letters and digits; 3.3 Source files.
- [4. Lexical elements](language/04-lexical-elements.md)
  4.1 Comments; 4.2 Tokens; 4.3 Semicolons; 4.4 Identifiers; 4.5 Keywords; 4.6 Contextual keywords; 4.7 Operators and punctuation; 4.8 Integer literals; 4.9 Floating-point literals; 4.10 Rune literals; 4.11 String literals.
- [5. Constants](language/05-constants.md)
- [6. Variables](language/06-variables.md)
- [7. Types](language/07-types.md)
  7.1 Boolean types; 7.2 Numeric types; 7.3 String types; 7.4 Array types; 7.5 Slice types; 7.6 Struct types; 7.7 Pointer types; 7.8 Function types; 7.9 Interface types; 7.10 Map types; 7.11 Channel types; 7.12 Enums; 7.13 Unions; 7.14 `packed` types; 7.15 `volatile[T]`; 7.16 `aligned[T, N]`; 7.17 `ring[T, N]`; 7.18 Owner types; 7.19 `owned`, `shared`, `synced` and `weak`.
- [8. Properties of types and values](language/08-properties-of-types-and-values.md)
  8.1 Representation of values; 8.2 Underlying types; 8.3 Type identity; 8.4 Assignability; 8.5 Representability; 8.6 Method sets; 8.7 Values, views, owners and counted values; 8.8 Sendable types; 8.9 Portable types; 8.10 Comparable types; 8.11 Convertible types.
- [9. Blocks](language/09-blocks.md)
- [10. Declarations and scope](language/10-declarations-and-scope.md)
  10.1 Label scopes; 10.2 Blank identifier; 10.3 Predeclared identifiers; 10.4 Exported identifiers; 10.5 Uniqueness of identifiers; 10.6 Constant declarations; 10.7 Iota; 10.8 Type declarations; 10.9 Type parameter declarations; 10.10 Variable declarations; 10.11 Short variable declarations; 10.12 Function declarations; 10.13 Default parameter values; 10.14 Method declarations; 10.15 Methods with type parameters.
- [11. Expressions](language/11-expressions.md)
  11.1 Operands; 11.2 Qualified identifiers; 11.3 Composite literals; 11.4 Function literals; 11.5 Closures and their regions; 11.6 Primary expressions; 11.7 Selectors; 11.8 Method expressions; 11.9 Method values; 11.10 Index expressions; 11.11 Indexing library containers; 11.12 Slice expressions; 11.13 Type assertions; 11.14 Calls; 11.15 Passing arguments to `...` parameters; 11.16 Instantiations; 11.17 Type inference; 11.18 Operators; 11.19 Arithmetic operators; 11.20 Comparison operators; 11.21 Logical operators; 11.22 Address operators; 11.23 Receive operator; 11.24 Conversions; 11.25 Constant expressions; 11.26 Order of evaluation; 11.27 Or clauses.
- [12. Statements](language/12-statements.md)
  12.1 Terminating statements; 12.2 Empty statements; 12.3 Labeled statements; 12.4 Expression statements; 12.5 Send statements; 12.6 IncDec statements; 12.7 Assignment statements; 12.8 If statements; 12.9 Switch statements; 12.10 For statements; 12.11 Range over functions; 12.12 Task statements; 12.13 Select statements; 12.14 Return statements; 12.15 Break statements; 12.16 Continue statements; 12.17 Goto statements; 12.18 Fallthrough statements; 12.19 Defer statements.
- [13. Built-in functions](language/13-built-in-functions.md)
  13.1 Built-ins Plutonium does not have; 13.2 Copying slices; 13.3 Clear; 13.4 Close; 13.5 Deletion of map elements; 13.6 Length and capacity; 13.7 Making channels and maps; 13.8 Min and max; 13.9 Allocation; 13.10 Moving out of a place; 13.11 Handling panics; 13.12 Bootstrapping.
- [14. Packages](language/14-packages.md)
  14.1 Source file organization; 14.2 Package clause; 14.3 Import declarations; 14.4 Build constraints; 14.5 An example package.
- [15. Program initialization and execution](language/15-program-initialization-and-execution.md)
  15.1 The zero value; 15.2 Package initialization; 15.3 Program initialization; 15.4 Program execution.
- [16. Errors](language/16-errors.md)
  16.1 The error interface; 16.2 Where an error lives; 16.3 Sentinels and `errors.New`; 16.4 Error codes; 16.5 Errors with context; 16.6 Wrapping, `Is` and `As`; 16.7 Allocated errors and ownership.
- [17. Run-time panics](language/17-run-time-panics.md)
  17.1 Panics; 17.2 Deferred calls on a panic; 17.3 Ending a task with `rt.ExitTask`; 17.4 Panics in freestanding programs; 17.5 Faults; 17.6 Use of freed memory.
- [18. Ownership and memory](language/18-ownership-and-memory.md)
  18.1 Values, views and owners; 18.2 Where variables live; 18.3 Views; 18.4 Loop iterations reuse their storage; 18.5 Moves; 18.6 Drops; 18.7 Shared ownership; 18.8 Strings; 18.9 Allocators; 18.10 No allocation without an owner or an arena; 18.11 Generation checks; 18.12 Safe and unsafe code; 18.13 Examples.
- [19. Concurrency](language/19-concurrency.md)
  19.1 Tasks; 19.2 The sendable rule; 19.3 Data race freedom; 19.4 Scoped tasks; 19.5 The scheduler contract; 19.6 Timers; 19.7 Memory model; 19.8 Channels.
- [20. System considerations](language/20-system-considerations.md)
  20.1 Package `unsafe`; 20.2 Size and alignment guarantees; 20.3 Directives; 20.4 Assembly and C; 20.5 Build modes and targets; 20.6 Kernel mode; 20.7 Floating point in kernel mode; 20.8 `pluto/arch/x86`; 20.9 Per-CPU variables; 20.10 Interrupts; 20.11 Allocation in kernel mode.
- [21. Implementation requirements](language/21-implementation-requirements.md)
  21.1 Diagnostics; 21.2 No silent failures; 21.3 Acceptance does not depend on optimization; 21.4 Implementation limits; 21.5 Run-time checks and the unchecked mode; 21.6 Layouts and calling conventions; 21.7 Reproducible builds; 21.8 Language version.
- [Appendix A. Differences from Go](language/a-differences-from-go.md)
  A.1 The no-false-friends rule; A.2 Removed; A.3 Changed; A.4 Added; A.5 No remaining exceptions.
- [Appendix B. Keywords and contextual keywords](language/b-keywords.md)
- [Appendix C. Operator precedence](language/c-operator-precedence.md)
- [Appendix D. Predeclared identifiers](language/d-predeclared-identifiers.md)
- [Appendix E. Grammar](language/e-grammar.md)
  E.1 Characters and tokens; E.2 Types; E.3 Blocks and declarations; E.4 Expressions; E.5 Statements; E.6 Packages; E.7 The predeclared type forms.
- [Appendix F. Library names the language relies on](language/f-library-names.md)
  F.1 The runtime, `pluto/rt`; F.2 Allocation, `pluto/alloc` and `pluto/arena`; F.3 Iterators and containers; F.4 Errors; F.5 Synchronization; F.6 Strings; F.7 Time; F.8 Machine access; F.9 Operating-system names, `pluto/os`.
- [Appendix G. Attribution](language/g-attribution.md)

## Attribution

Portions of this specification are adapted from *The Go Programming Language Specification* (language
version go1.27), <https://go.dev/ref/spec>, copyright 2009 The Go Authors, used under the Creative Commons
Attribution 4.0 International License (<https://creativecommons.org/licenses/by/4.0/>). The text has been
modified: it is reordered, rewritten and extended to define Plutonium, and many of its rules differ from
Go's. Code examples adapted from it are used under the BSD license in [LICENSE-go](LICENSE-go). The adapted
material is provided as is, without warranties, as those licenses state. The Go Authors do not endorse
Plutonium. Appendix G ([Attribution](language/g-attribution.md)) says which chapters adapt Go's text and
what the licenses require.
