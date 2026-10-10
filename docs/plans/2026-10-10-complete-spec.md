# A complete Plutonium specification: outline and gap list

**Date:** 2026-10-10
**Status:** phase 1 of 2: the outline, the sources and status of every section, the questions the
language has not answered yet, licensing, file structure and the split of the writing work. The gap list
(section 3) was answered on 2026-10-10; the decisions are in section 3.10. The roadmap items (6.1 step 3,
roadmap items 60 to 76) and the chapter stubs (6.1 step 4, `docs/spec/language/`) are in place; the
memory-safety branch has merged. Phase 2 can start.

## 1. Why

`docs/spec/language.md` describes Plutonium as "Go 1.27 plus the removals, changes and additions below"
and says that where it is silent, the Go specification applies. A reader therefore has to read Go's
specification first and then work out which of its rules still hold. On 2026-10-10 the user decided that
this is not enough: Plutonium is Go-familiar, not Go-compatible, and its specification must define the
whole language on its own, as the Go specification does for Go.

The complete specification describes the **decided** language. What the compiler does not do yet is
tracked in `docs/roadmap.md`, not left as a hole in the spec. Writing every section forces every question
the language has not answered; section 3 lists them with a recommendation each. When every section is
decided and implemented, the language is complete: the specification doubles as that checklist.

Inputs read for this outline: `AGENTS.md`; `docs/spec/language.md`, `abi.md`, `memory-model.md`,
`debugging.md`, `debug-allocator.md`; `docs/roadmap.md`; the plans dated 2026-10-09 and 2026-10-10
(enums, errors, scheduler contract, sum types, remove-append, go-false-friends, scoped puroutines); the
memory-safety design on the unlanded branch `worktree-agent-a1cbe4e24983acabc` (with that branch's
`language.md`, which rewrites much of section 6); the project decisions log; and the Go 1.27.1
specification (`$GOROOT/doc/go_spec.html`).

## 2. Table of contents

The new structure follows the Go specification's order for everything Go also has, so that a Go
programmer finds each rule where they expect it, and adds chapters for what Go does not have (ownership,
concurrency beyond the `go` statement, kernel support). Section titles that the diagnostics catalog cites
today keep their wording where the content moves, so the renumbering is a change of numbers only
(section 5.3).

**Source** says where the text comes from:

- **Go: X** adapts section X of the Go 1.27 specification (the attribution of section 4 applies);
- **old N** is section N of today's `docs/spec/language.md`;
- **P** is new Plutonium text, written from the plan or decision named.

**Status** says how decided the content is:

- **main**: decided and implemented on `main`;
- **branch**: decided and implemented on the memory-safety branch, which has not merged yet;
- **decided (ref)**: decided, not implemented; `ref` is where the work is tracked;
- **open Gn**: not decided; see gap Gn in section 3.

A section can carry several statuses when its parts differ.

### Chapter 1. Introduction

| Section | Source | Status |
|---|---|---|
| 1.1 Scope and goals | old intro, old 1 | main |
| 1.2 Design rules: Go-familiar, no false friends, no annotations for ordinary code, Go as a test oracle | old intro; decisions of 2026-10-10 | decided (text only) |
| 1.3 Relation to Go, and how to read this specification | P (decision of 2026-10-10: self-contained) | decided |
| 1.4 Terms: task, view, owner, counted value, frame, region, move, drop, temporary | P, collected from old 4.5, 6, 6.10 | main and branch |

### Chapter 2. Notation

| Section | Source | Status |
|---|---|---|
| 2 Notation (EBNF, plus the productions Plutonium adds) | Go: Notation | main |

### Chapter 3. Source code representation

| Section | Source | Status |
|---|---|---|
| 3.1 Characters | Go: Characters | main |
| 3.2 Letters and digits | Go: Letters and digits | main |
| 3.3 Source files (`.p`, UTF-8, byte order mark) | old 2; Go: Source code representation | main |

### Chapter 4. Lexical elements

| Section | Source | Status |
|---|---|---|
| 4.1 Comments (directive comments `//pu:` are introduced here, defined in 20.3) | Go: Comments; old 9 | main; open G4 |
| 4.2 Tokens | Go: Tokens | main |
| 4.3 Semicolons | Go: Semicolons | main |
| 4.4 Identifiers | Go: Identifiers | main |
| 4.5 Keywords (Go's, plus `enum` and `packed`) | Go: Keywords | main; open G1, G3 |
| 4.6 Contextual keywords (`union`, `owner`, `task`; `bits`, `le`, `be` and `case` inside packed types) | P: old 5.5, 5.10; decisions (task rename) | `union`, packed words: main; `owner`: branch; `task`: decided (task rename sweep); open G2 |
| 4.7 Operators and punctuation | Go: Operators and punctuation | main |
| 4.8 Integer literals | Go: Integer literals | main |
| 4.9 Floating-point literals (imaginary literals are not part of the language) | Go: Floating-point literals, Imaginary literals | main |
| 4.10 Rune literals | Go: Rune literals | main |
| 4.11 String literals (every string literal is valid UTF-8) | Go: String literals; old 6.10 | main |

### Chapter 5. Constants

| Section | Source | Status |
|---|---|---|
| 5 Constants (no complex constants; precision requirements) | Go: Constants | main; open G6 |

### Chapter 6. Variables

| Section | Source | Status |
|---|---|---|
| 6 Variables (where a variable lives is fixed by its declaration) | Go: Variables; old 6.3 rule 2 | main |

### Chapter 7. Types

| Section | Source | Status |
|---|---|---|
| 7 Types (overview, type literals, named types) | Go: Types | main |
| 7.1 Boolean types | Go | main |
| 7.2 Numeric types (no complex types; `int`, `uint`, `uintptr` are 64 bits) | Go; old 4.6 | main; open G9 |
| 7.3 String types (immutable counted value, valid UTF-8, three-word representation, `len`, indexing) | Go: String types; old 6.10; abi.md 1.1 | main |
| 7.4 Array types (arrays of owners are owners) | Go; old 6.4 | main; branch |
| 7.5 Slice types (a slice is a view and never grows) | Go; old 6.3, 4.3; remove-append plan | main; `append` removal decided (remove-append plan, no roadmap item yet) |
| 7.6 Struct types (struct keys with field selectors; owner fields) | Go; old 6.4 | main; branch; open G12 |
| 7.7 Pointer types (a pointer is a view) | Go; old 6.3 | main |
| 7.8 Function types (a func value may be a closure record; defaults are not part of the type) | Go; old 4.5, 5.1 | main |
| 7.9 Interface types (basic, embedded, general, implementing; the word or frame box; ordinary interfaces lend, owning interfaces own) | Go: Interface types; old 4.4 | representation: main; owning interfaces: branch (declared, lending checked), drops decided (roadmap 20, 50); open G18 |
| 7.10 Map types (a counted reference to a hash table) | Go: Map types; decision of 2026-10-10 | decided (after the memory-safety merge; no roadmap item yet); open G15, G16, G17 |
| 7.11 Channel types (a counted reference) | Go: Channel types; old 4.8 | main; counting decided (roadmap 48) |
| 7.12 Enums | old 5.9; enums design | main; open G7 |
| 7.13 Unions | old 5.10; sum-types proposal | main; owner payloads decided (roadmap 25); in-place update deferred |
| 7.14 packed types | old 5.5 | main |
| 7.15 volatile[T] | old 5.2 | main |
| 7.16 aligned[T, N] | old 5.3 | main |
| 7.17 ring[T, N] | old 5.4 | main |
| 7.18 Owner types (the `owner` qualifier, where it may and must appear, `Drop`, generics, embedding) | old 6.4 (branch text) | branch; open G14 |
| 7.19 owned, shared, synced and weak | old 5.6 | decided (roadmap 48; memory-safety design 6.3, 6.4); open G13, G20 |

### Chapter 8. Properties of types and values

| Section | Source | Status |
|---|---|---|
| 8.1 Representation of values | Go; abi.md 1 | main |
| 8.2 Underlying types | Go | main |
| 8.3 Type identity (the owner qualifier, enums and unions are distinct types) | Go; old 5.9, 5.10, 6.4 | main; branch |
| 8.4 Assignability (no untyped constants to enums, no implicit conversion to unions) | Go; old 5.9, 5.10 | main |
| 8.5 Representability | Go | main |
| 8.6 Method sets (value receivers on owners consume; `Drop` is never promoted) | Go; old 6.4 | branch; open G18 |
| 8.7 Values, views, owners and counted values (the kind of every type) | old 6 intro, 6.2, 6.3 | main; branch |
| 8.8 Sendable types | old 4.7 | main; heap views across tasks decided (memory-safety D5); open G16 |
| 8.9 Portable types | decisions (portable); scheduler-contract plan 9 | decided (name only, not implemented, no roadmap item); open G19 |

### Chapter 9. Blocks

| Section | Source | Status |
|---|---|---|
| 9 Blocks | Go: Blocks | main |

### Chapter 10. Declarations and scope

| Section | Source | Status |
|---|---|---|
| 10.1 Label scopes | Go | main |
| 10.2 Blank identifier | Go | main; open G23 |
| 10.3 Predeclared identifiers (adds `owned`, `shared`, `synced`, `weak`, `volatile`, `aligned`, `ring`, `take`, `portable`; removes `complex64`, `complex128`, `complex`, `real`, `imag`, `recover`) | Go; old 3, 5 | main; branch (`take`); decided (`portable`) |
| 10.4 Exported identifiers | Go | main |
| 10.5 Uniqueness of identifiers | Go | main |
| 10.6 Constant declarations | Go | main |
| 10.7 Iota | Go | main; open G7 |
| 10.8 Type declarations (aliases, definitions; `owner`, `enum` and `union` only in definitions) | Go; old 5.9, 5.10, 6.4 | main; branch |
| 10.9 Type parameter declarations (constraints, `owner` constraints) | Go; old 6.4 generics | main; branch |
| 10.10 Variable declarations | Go | main |
| 10.11 Short variable declarations | Go | main |
| 10.12 Function declarations (the signatures of `main`) | Go; old 2 | main |
| 10.13 Default parameter values | old 5.1 | main |
| 10.14 Method declarations (`Drop`) | Go; old 6.4 | main; branch |
| 10.15 Methods with type parameters | old 5.7 | main |

### Chapter 11. Expressions

| Section | Source | Status |
|---|---|---|
| 11.1 Operands | Go | main |
| 11.2 Qualified identifiers | Go | main |
| 11.3 Composite literals (frame storage in functions, static storage in package initializers) | Go; old 4.4, 6.3 | main; escape errors branch |
| 11.4 Function literals (capture by reference; captured owners) | Go; old 4.5 | main; branch |
| 11.5 Closures and their regions | old 4.5 | main (E3100 to E3102) |
| 11.6 Primary expressions | Go | main |
| 11.7 Selectors (enum members, union variants) | Go; old 5.9, 5.10 | main |
| 11.8 Method expressions | Go | main |
| 11.9 Method values (closure records) | Go; old 4.5 | main; open G26 |
| 11.10 Index expressions (arrays, slices, strings, maps) | Go | main; maps decided |
| 11.11 Indexing library containers | old 6.7 | main |
| 11.12 Slice expressions (strings slice on rune boundaries) | Go; old 6.10; false-friends F12 | main; open G30 |
| 11.13 Type assertions (unions; owners out of owning interfaces) | Go; old 4.4, 5.10 | main; branch |
| 11.14 Calls (arguments move owners; omitted defaulted arguments) | Go; old 5.1, 6.4 | main; branch |
| 11.15 Passing arguments to `...` parameters | Go; old 6.3 | main; branch |
| 11.16 Instantiations | Go | main |
| 11.17 Type inference | Go (1.27 rules) | main |
| 11.18 Operators (precedence) | Go | main |
| 11.19 Arithmetic operators (overflow, shifts, division, string concatenation) | Go; old 4.6, 6.10 | main |
| 11.20 Comparison operators (strings, unions, enums, packed values, owners) | Go; old 5.5, 5.9, 5.10 | main; open G13 |
| 11.21 Logical operators | Go | main |
| 11.22 Address operators (`&x` is a view of `x` where it lives) | Go; old 6.3 | main; branch |
| 11.23 Receive operator (receiving an owner) | Go; old 4.8 | main |
| 11.24 Conversions (numeric, strings, slices to arrays, packed, enums, owners, `unsafe.Pointer`; the conversions Go has and Plutonium does not) | Go: Conversions; old 3, 5.5, 5.9, 6.10, 11 | main; `string([]byte)` (E2015), `[]byte(s)` and `[]rune(s)` (E2016) and `string(i)` of a non-rune integer (E2017) removed (roadmap 61); open G8, G14 |
| 11.25 Constant expressions | Go | main |
| 11.26 Order of evaluation (defaulted arguments, moves) | Go; old 5.1 | main; open G24, G25 |

### Chapter 12. Statements

| Section | Source | Status |
|---|---|---|
| 12.1 Terminating statements (exhaustive union switches) | Go; old 5.10 | main; open G27 |
| 12.2 Empty statements | Go | main |
| 12.3 Labeled statements | Go | main |
| 12.4 Expression statements (temporaries) | Go; old 6.4 | main; branch |
| 12.5 Send statements (sending moves an owner) | Go; old 4.8 | main; branch |
| 12.6 IncDec statements | Go | main |
| 12.7 Assignment statements (moves, drops of the old value, permutations) | Go; old 6.4, 6.10 | main (strings); branch (owners) |
| 12.8 If statements (header temporaries) | Go; old 6.4 | main |
| 12.9 Switch statements (enum cases and exhaustiveness; union type switches) | Go; old 5.9, 5.10 | main |
| 12.10 For statements (conditions, clauses, range over integers, strings, arrays, slices, maps, channels, enums and containers; iteration storage is reused) | Go; old 5.9, 6.7; false-friends F3, F11 | main; branch (loop storage); decided (F3 map guarantees, F11 vector snapshot; no roadmap items) |
| 12.11 Range over functions | old 4.10 | main; open G29 |
| 12.12 Task statements | Go: Go statements; old 4.7; task rename | main as `go`; rename decided (task rename sweep); open G1, G2 |
| 12.13 Select statements (owners in send cases) | Go; old 4.8 | main |
| 12.14 Return statements (named owner results) | Go; old 6.4 | main; branch |
| 12.15 Break statements | Go | main; drops branch |
| 12.16 Continue statements | Go | main; drops branch |
| 12.17 Goto statements | Go | main; drops branch |
| 12.18 Fallthrough statements | Go; old 5.10 | main |
| 12.19 Defer statements (one slot per statement, none in loops; deferred calls run before drops) | Go; old 4.10, 6.4 | main; `defer` in loops stays an error (roadmap 28) |

### Chapter 13. Built-in functions

| Section | Source | Status |
|---|---|---|
| 13.1 Built-ins Plutonium does not have (`append`, `recover`, `complex`, `real`, `imag`) | old 3, 4.3; remove-append plan | `recover` and complex: main; `append`: decided (remove-append plan, E2013) |
| 13.2 Copying slices | Go: Appending to and copying slices | main; owners branch |
| 13.3 Clear | Go | main; open G28 |
| 13.4 Close | Go | main |
| 13.5 Deletion of map elements | Go | decided (maps) |
| 13.6 Length and capacity (`len(E)` of an enum, `Len` of containers) | Go; old 5.9, 6.7 | main |
| 13.7 Making channels and maps (optional allocator; `make([]T, ...)` is an error) | Go; old 4.2; false-friends F16 | chan: main; maps decided; slice error decided (F16); open G17 |
| 13.8 Min and max | Go | main; strings planned (roadmap 29) |
| 13.9 Allocation (`new` returns `owned[T]`, optional allocator) | Go; old 4.2 | allocator argument main; `owned[T]` decided (roadmap 50) |
| 13.10 Moving out of a place (`take`) | old 6.4 | branch |
| 13.11 Handling panics (`panic`) | Go | main |
| 13.12 Bootstrapping (`print`, `println`) | Go; decisions (float printing) | main |

### Chapter 14. Packages

| Section | Source | Status |
|---|---|---|
| 14.1 Source file organization | Go | main |
| 14.2 Package clause | Go | main |
| 14.3 Import declarations (`pluto/` paths) | Go; old 2 | main |
| 14.4 Build constraints (`//pu:build`, file name suffixes, the tags each mode sets) | old 2, 12.1 | main; open G4 |
| 14.5 An example package | Go | main |

### Chapter 15. Program initialization and execution

| Section | Source | Status |
|---|---|---|
| 15.1 The zero value (owners hold nothing; enums have a zero member; a union holds its first variant) | Go; old 5.9, 5.10, 6.4 | main; branch |
| 15.2 Package initialization (static storage for initializer literals and boxes) | Go; old 4.4 | main; open G22 |
| 15.3 Program initialization | Go | main |
| 15.4 Program execution (`main` returning nothing or `int64`; exit status; other tasks; package-level owners) | Go; old 2 | main; open G21 |

### Chapter 16. Errors

| Section | Source | Status |
|---|---|---|
| 16.1 The error interface | Go: Errors | main |
| 16.2 Where an error lives | old 14 | main |
| 16.3 Sentinels and errors.New | old 14.1 | main |
| 16.4 Error codes | old 14.2 | main |
| 16.5 Errors with context | old 14.3 | main, replaced by 16.7 when it lands |
| 16.6 Wrapping, Is and As | old 14.4 | main |
| 16.7 Allocated errors and ownership (counted errors) | old 14.6 | decided (false-friends decision 12; roadmap 11) |

Old 14.5 (library differences) moves to the package documentation; false-friends F15 replaces it with
Go's error types once errors are counted.

### Chapter 17. Run-time panics

| Section | Source | Status |
|---|---|---|
| 17.1 Panics (what panics; no unwinding, no `recover`; the panic handler) | Go: Run-time panics, Handling panics; old 4.1, 8 | main |
| 17.2 Deferred calls on a panic | old 8.2 | main (roadmap 58) |
| 17.3 Ending a task with rt.Goexit | old 8.3 | main; owner drops on `Goexit` branch |
| 17.4 Panics in freestanding programs | old 8.1 | main |
| 17.5 Faults (nil dereference, stack overflow, division faults) | old 8 | main |

### Chapter 18. Ownership and memory

| Section | Source | Status |
|---|---|---|
| 18.1 Values, views and owners | old 6 intro, 6.1, 6.2 | main; branch |
| 18.2 Where variables live | old 6.3 rule 2, old 7 intro | main |
| 18.3 Views (frame, local owner, heap and static memory; the escape rules) | old 6.3 (branch text) | frame views: branch (E3001 to E3008); views of local owners after a move: decided (marked planned on the branch) |
| 18.4 Loop iterations reuse their storage | branch 4.5; decisions (loop variables) | branch |
| 18.5 Moves | old 6.4 | branch |
| 18.6 Drops | old 6.4 | branch; drops of `owned`, owning interfaces and owner unions decided (memory-safety 6.3) |
| 18.7 Shared ownership | old 6.5 | decided (roadmap 48) |
| 18.8 Strings (counting, drop glue, borrowing parameters, count elision as far as it is visible) | old 6.10, 6.10.1 | main |
| 18.9 Allocators (the interface, the default, `alloc.Use` and `Restore`, arenas) | old 7, 7.1, 7.2 | main; `Restore` as an owner: branch; open G34, G35 |
| 18.10 No allocation without an owner or an arena | old 6.6 | decided (roadmap 50, 51; maps; counted errors) |
| 18.11 Generation checks | memory-safety design 5 | branch (opt-in prototype); default and arena epochs decided (D3, D4); open G31 |
| 18.12 Safe and unsafe code | memory-safety design 2, D1, D6 | decided (`pluto vet -unsafe`, `//pu:nochecks` not implemented) |
| 18.13 Examples | old 6.8 | mixed |

Old 6.9 (the exact texts of ownership diagnostics) leaves the spec for the catalog and the golden tests
(gap G40). Old 7.3 (the debug allocator) is covered by `debug-allocator.md` and a sentence in 18.9.

### Chapter 19. Concurrency

| Section | Source | Status |
|---|---|---|
| 19.1 Tasks (stacks, preemption, parallelism, deadlock report) | old 4.7 | main (as puroutines; rename decided); stack sizes decided (false-friends F8, roadmap 18); open G36 |
| 19.2 The sendable rule | old 4.7 | main (views of changing memory: old 6.11) |
| 19.3 Data race freedom (heap views, `synced[T]` contents, inferred lock guarding, package variables) | memory-safety design 7, D5, D7; old 6.11 | main (roadmap 59); G32 decided (a) |
| 19.4 Scoped tasks (`sync.Scope`, `s.Task(f)`, joins, the sharing check) | scoped-puroutines plan (revised) | decided (no roadmap item); open G35 |
| 19.5 The scheduler contract (summary; abi.md 12 is normative) | old 4.7; abi.md 12; scheduler-contract plan | main |
| 19.6 Timers | old 4.7 | main; the channel owns its timer (false-friends F6, roadmap 48); the result type of `NewTimer` open |
| 19.7 Memory model (summary; memory-model.md is normative) | old 4.9; memory-model.md | main; open G33, G41 |

### Chapter 20. System considerations

| Section | Source | Status |
|---|---|---|
| 20.1 Package unsafe | Go: Package unsafe; old 11 | main; `unsafe.Drop` branch |
| 20.2 Size and alignment guarantees | Go; abi.md 1 | main |
| 20.3 Directives | old 9 | main; `noescape`, `resultholds`: branch; `nochecks`: decided (D6); open G37 |
| 20.4 Assembly and C | old 10 | main |
| 20.5 Build modes and targets | old 12, 12.1 | main |
| 20.6 Kernel mode | old 12.2 | main |
| 20.7 Floating point in kernel mode | old 12.3 | main |
| 20.8 pluto/arch/x86 | old 12.4 | main |
| 20.9 Per-CPU variables | old 12.5 | main |
| 20.10 Interrupts | old 12.6 | main |
| 20.11 Allocation in kernel mode | old 12.7 | main |

### Chapter 21. Implementation requirements

| Section | Source | Status |
|---|---|---|
| 21.1 Diagnostics (every error explains itself; codes; `pluto explain`) | AGENTS.md; decisions (diagnostics) | main |
| 21.2 No silent failures | AGENTS.md rule 4 | main |
| 21.3 Acceptance does not depend on optimization | memory-safety design 4 | main; branch |
| 21.4 Implementation limits (nesting, closure region sizes, stack) | old 4.5, 9; roadmap 56 | main |

### Appendices

| Section | Source | Status |
|---|---|---|
| A. Differences from Go: A.1 the no-false-friends rule; A.2 removed; A.3 changed; A.4 added; A.5 the one documented exception (string slicing) | old intro, old 3; false-friends audit | mixed, follows the chapters |
| B. Keywords and contextual keywords | Go: Keywords; P | main; open G1, G2, G3 |
| C. Operator precedence | Go: Operator precedence | main |
| D. Predeclared identifiers | Go: Predeclared identifiers; P | main; branch; decided |
| E. Grammar (all productions collected) | Go; P | written in phase 3 from the chapters |
| F. Library names the language relies on (`pluto/rt` hooks by name, `alloc.Allocator`, `iter.Seq`, the `container` constraints, `errors.New`, `sync.Scope`, `strings.FromUTF8`) | old 13; abi.md 12 | main; decided |
| G. Attribution | section 4 of this plan | decided |

Old 13 (the standard library table), 13.1 (names from the operating system) and 5.8 (`pluto/bits`) move
to the package documentation (gap G39); only the names the language itself depends on stay, in
appendix F.

## 3. The gap list

Forty-two questions that a complete specification has to answer and the decided language does not.
Questions the existing decisions already answer are not listed: owners in closures (captured by
reference, never moved inside a literal), owners sent on channels and in `select`, labels and `goto`
(leaving a scope drops what it holds), zero values of owners (`Drop` accepts the zero value, D2), `copy`
of owners (an error), generic code with owners (`[T owner any]`), interface boxes, enum and union
comparison, and integer overflow and shifts (Go's rules, verified by the false-friends probes).

The gaps keep their numbers (G1 to G42), which the table of contents cites. They are presented in nine
groups of related questions, so that each group can be decided together, and the groups are ordered by
how much of the spec waits on them: group 1 shapes the skeleton every writer starts from, groups 2 to 5
block whole chapters, and groups 8 and 9 block a few paragraphs.

Each gap gives the question in plain terms, each option with what it means in practice, and a
recommendation that names the goals each option serves or costs. The goals, with the short names used
below:

| Name | Goal |
|---|---|
| **Familiar** | Go-familiar, not Go-compatible: Go's syntax and idioms wherever they fit |
| **Visible** | No false friends: behavior that would surprise a Go developer shows in the source, as a different name, syntax or type, or a compile error |
| **Safe** | Rust-level memory and race safety, without a borrow checker or lifetime annotations |
| **Plain** | Ordinary (non-kernel) code needs no annotations and no knowledge of how the compiler works |
| **Owned** | No garbage collector and no hidden allocation: every allocation has an owner |
| **Kernel** | Suitable for writing the PlutoniumOS kernel |
| **Fast** | Performance near Rust |

Summary, in the order of the groups:

| Group | Gaps | Blocks |
|---|---|---|
| 1. The shape of the spec | G38 to G42 | the skeleton of every chapter |
| 2. Owners in the type system | G12, G13, G14, G18, G19, G20 | chapters 7, 8; 11.20, 11.24 |
| 3. Moves inside expressions and statements | G23 to G29 | chapters 11, 12, 13 |
| 4. Strings and bytes | G10, G11, G30 | 7.3, 11.12, 11.24 |
| 5. The built-in map | G15, G16, G17 | 7.10, 13.5, 13.7 |
| 6. What safe code is promised | G31 to G35 | chapters 18, 19 |
| 7. Programs and tasks at run time | G21, G22, G36 | chapter 15, 19.1 |
| 8. Keywords, comments and directives | G1 to G5, G37 | chapter 4, 20.3, appendix B |
| 9. Numbers and constants | G6 to G9 | chapter 5, 7.2, 10.7, 11.24 |

### 3.1 Group 1: the shape of the spec

These decide what the spec contains at all. Phase 2 cannot create its chapter skeletons until they are
answered.

**G39. Does the language spec describe the standard library?**
Today's section 13 lists every package and much of its behavior (fmt's verbs, `os`'s byte-path
functions, `strconv`'s errors).

- (a) Keep the library in the spec. One document holds everything, but the spec changes with every
  library change, and library behavior reads as if it were a language rule.
- (b) Move it to the package documentation (`pluto doc`), as Go does, and keep in appendix F only the
  names the language itself depends on: the `pluto/rt` hooks the compiler calls by name,
  `alloc.Allocator`, `iter.Seq`, the `container` constraints, `errors.New` (whose argument the compiler
  checks), `sync.Scope` (whose sharing rules the compiler checks), `strings.FromUTF8`.

**Recommendation: (b).** It serves **Familiar** (a Go programmer looks for `fmt`'s verbs in `fmt`'s
documentation, not the spec) and **Plain** (the spec stays about the language). It costs nothing.

**G40. Does the spec reproduce the exact text of compile errors?**
Old section 6.9 prints about twenty full messages; the memory-safety branch already says the golden
files in `cmd/pluto/testdata/diag` pin them, and the spec's copies differ in detail.

- (a) Keep the texts in the spec. Readers see what an error says, but the two copies drift.
- (b) The spec states each rule and its error code; the texts live in the catalog (`pluto explain E3001`)
  and the golden tests. Chapter 21 states what every message must contain.

**Recommendation: (b).** **Plain** is served by `pluto explain` and by the messages themselves, which
already explain in the program's own names; one copy of each text cannot drift.

**G41. Do the memory model and the ABI also stop deferring to Go?**
`memory-model.md` says "where this document is silent, Go's memory model applies", and `abi.md` says
struct layout follows "Go's rules".

- (a) Make both self-contained in phase 2.
- (b) Leave them; only the language spec changes.

**Recommendation: (a).** The user's reason for a self-contained spec applies to every normative
document: a reader should not need Go's documents to know what Plutonium does. **Familiar** is about how
the language reads, not about which documents it depends on.

**G42. Where do the diagnostics catalog's citations of the Go spec point?**
About seventy catalog entries cite a section of Go's spec ("Import declarations", "Defer statements"),
and the generated `catalog_go.go` quotes go/types' sentences from it.

- (a) Point every citation at the Plutonium section that now defines the rule, and keep a quoted Go
  sentence only where it is still true of Plutonium.
- (b) Keep the Go citations.

**Recommendation: (a).** A citation sends the reader to the rule that holds. Go's section can describe
behavior Plutonium does not have (Go's maps, `defer` in loops, `recover`), which is a false friend in
the documentation itself (**Visible**).

**G38. Does Plutonium have language versions?**
Go's spec has a section on them, and `go.mod` gates features by version.

- (a) One version: the spec has a version number in its header, and no module records a language
  version until a release needs one.
- (b) Go's scheme: `pluto.mod` names a language version, and features are enabled by it.

**Recommendation: (a)** until a release has users whose code must keep compiling. (b) costs every
feature a version check and buys nothing while there is one compiler and no released code.

### 3.2 Group 2: owners in the type system

These decide how owner types relate to the rest of the type system. Chapters 7 and 8 and the comparison
and conversion sections cannot be written without them.

**G18. Which methods does an owner have when it is put in an ordinary interface?**
Converting an owner to `any`, `error` or `io.Reader` lends it: the interface is a view of the variable,
and the variable keeps the owner. A method with a value receiver on an owner type consumes its receiver
(spec 6.4), so calling one through the interface would take the owner out of a variable that still
believes it holds it.

```go
type Buf owner struct{ data vec.Vec[byte] }
func (b *Buf) String() string   // pointer receiver: borrows
func (b Buf) Finish() string    // value receiver: consumes b

var b Buf
fmt.Println(b)                  // lends b to an any
var s fmt.Stringer = b          // does b satisfy Stringer?
var s fmt.Stringer = &b         // a *Buf: a view, with String
```

- (a) A lent owner satisfies interfaces with its pointer-receiver methods, as if `&b` had been
  converted. `var s fmt.Stringer = b` compiles, and `fmt.Println(b)` calls `String`. Go would print the
  fields here, since in Go a value does not have its pointer methods.
- (b) Go's rule: the value's method set has only value-receiver methods, and pointer methods need `&b`.
  Since value-receiver methods of owner types consume, they never satisfy an interface method (a compile
  error at the conversion says so and shows `&b`). `fmt.Println(b)` prints the fields, as Go does;
  `fmt.Println(&b)` calls `String`, as Go does.

**Recommendation: (b).** It keeps Go's method-set rule unchanged (**Familiar**), so `fmt.Println(b)` and
`fmt.Println(&b)` behave exactly as a Go programmer expects (**Visible**), and the consuming case is a
compile error at the conversion (**Safe**). (a) adds a rule that only owners follow, and it makes the
same `Println` call behave differently from Go. The rule is uniform: a value-receiver method of an owner
type never satisfies an interface method, ordinary or owning; code that needs to consume calls the
method directly or after a type assertion that moves the owner out.

**G13. Can owners be compared with `==`?**
The spec does not say, nor how `owned`, `shared`, `synced` and `weak` compare.

```go
type Fd owner int32
if f == g { ... }           // two descriptors
var root owned[Node]
if root == nil { ... }      // an empty box
```

- (a) An owner type is comparable when its underlying type is, as in Go. Comparing reads both operands
  and moves neither. `owned`, `shared`, `synced` and `weak` compare by address and with `nil`, like
  pointers.
- (b) Owners are never comparable; `root == nil` needs a method such as `root.IsNil()`.

**Recommendation: (a).** Comparing handles and checking a box for nil are everyday operations
(**Familiar**), written as Go programmers write them for pointers. A comparison only reads, so it never
duplicates ownership (**Safe**). (b) costs **Familiar** and gains nothing.

**G14. Can a value be converted into an owner type, or an owner into a plain value?**
A kernel declares owners over raw numbers, and needs the number back inside `Drop`.

```go
type Frame owner uintptr                       // a physical page frame
func (f *Frame) Drop() { freeFrame(uintptr(*f)) } // read the number
func Alloc() Frame { return Frame(nextFree()) }   // make an owner from a number
```

- (a) Converting a value into an owner type makes a new owner, but only in the package that declares the
  owner type, where its invariants are kept (as only that package can fill unexported fields).
  Converting an owner to a non-owner type reads the value and moves nothing: the result is a plain
  number with no `Drop`. Converting between two owner types with the same underlying type moves.
- (b) As (a), but conversion into an owner type is allowed in any package.
- (c) Only through `unsafe`, as Rust makes `File::from_raw_fd` unsafe.

**Recommendation: (a).** Reading the number out is harmless: a plain `uintptr` frees nothing, and
`Drop` methods need it (**Kernel**). Making an owner from a number is how a second owner of the same
frame could appear; (b) would let any package do that (**Safe** cost), and (c) would make every kernel
constructor unsafe code (**Plain** and **Kernel** cost). Restricting it to the declaring package puts
the responsibility where Go already puts constructors, with no new marking.

**G12. Can a struct embed `owned[T]`, `shared[T]` or `synced[T]`?**
Go allows embedding `*T`, which promotes `T`'s methods.

```go
type Widget owner struct {
	owned[Style]      // embedded box
}
w.Apply()             // promoted from *Style?
```

- (a) Allowed, promoting `T`'s methods as an embedded `*T` does. A struct that embeds an `owned[T]` must
  be declared `owner` (it has an owner field); one that embeds `shared[T]` copies and counts.
- (b) Not allowed; write a named field and forward the methods.

**Recommendation: (a).** These types already dereference like `*T`, so embedding them as Go embeds `*T`
is what a Go programmer will try (**Familiar**). The ownership rules apply to the field as to any other,
so nothing is lost (**Safe**).

**G20. Can `owned`, `shared` or `synced` hold an interface type?**
For example `owned[io.Reader]`.

- (a) No: a compile error that points to owning interfaces (`type R owner interface{...}`), which already
  own what they hold.
- (b) Yes: a box holding an interface value, two levels of indirection.

**Recommendation: (a).** One way to own a value behind an interface. (b) adds a second, slower way and
the question of what dropping it drops.

**G19. What may cross between address spaces (`portable`)?**
The name is decided, for typed IPC between PlutoniumOS processes; the definition is not. The draft says
"values without pointers, and types marked transferable".

- (a) Structural only: a type is portable if it contains no pointers, views, counted values (strings
  included, since their buffer lives in the sender's address space), interfaces, func values or
  channels, at any depth.
- (b) As (a), plus types that implement a library interface that says how to transfer them (handles,
  page-owning owners), defined by the PlutoniumOS IPC design.
- (c) As (a), plus types marked with a directive.

**Recommendation: (a) now, (b) when the IPC design needs handles.** (a) is inferred like sendability
(**Plain**) and enough to start (**Kernel**). (c) is ruled out by the rule against annotations for
facts the compiler can work out or a library can express (**Plain**).

### 3.3 Group 3: moves inside expressions and statements

These decide what moving an owner means inside a single expression or statement. Chapters 11 to 13 need
them.

**G25. Can an expression move a variable and also use it?**

```go
send(buf, buf.Len())       // moves buf, then reads it?
send(buf.Len(), buf)       // reads buf, then moves it?
```

- (a) A compile error whenever one expression moves a variable and uses it anywhere else, whatever the
  order: "buf is moved by this call and used in another argument". The fix is a line before:
  `n := buf.Len(); send(buf, n)`.
- (b) Follow the evaluation order: the second line compiles, the first does not.

**Recommendation: (a).** The reader does not need to know the evaluation order to know whether the code
compiles (**Plain**), and the rule holds whatever G24 decides. Rust rejects the same programs. The cost
is an extra line in rare code (**Familiar**, slightly). Permuting assignments (`a, b = b, a`) stay
allowed, since they neither lose nor duplicate a value.

**G24. Is the order of evaluation specified?**
Go fixes the order of calls, method calls, receives and logical operators, and leaves the rest open:

```go
a := 1
f := func() int { a++; return a }
x := []int{a, f()}   // Go allows [1 2] or [2 2]; gc gives [2 2]
```

- (a) Keep Go's wording: the order of other operands is unspecified.
- (b) Specify strict left to right: `[1 2]`.
- (c) Specify the order gc uses: calls, method calls and receives first, left to right, then the other
  operands: `[2 2]`.

**Recommendation: (c), if a short survey confirms that lowering already evaluates this way.** Go
programmers who unknowingly rely on gc's order then get the same result (**Visible**), and the spec
leaves nothing open. (b) is the simplest rule to state, but it gives `[1 2]` where every Go program has
seen `[2 2]`, which is a false friend (**Visible** cost). If lowering does not match gc today, that is
already a false friend, and the fix is to match gc.

**G23. What does `_ = x` do to an owner?**
Go programmers write `_ = x` to silence "declared and not used".

```go
f, err := os.Open(path)
_ = f          // (a): nothing happens; f is still open
               // (b): f is moved and dropped here: the file closes
```

- (a) A read that moves nothing, as Rust's `let _ = x`. To drop early, `f.Drop()`.
- (b) A move into nothing, so the owner is dropped at once.

**Recommendation: (a).** In Go `_ = x` has no effect, and closing a file there would surprise every Go
reader (**Visible**). `_ = open()` still drops the temporary result at the end of the statement, as any
unstored temporary is.

**G26. Can a consuming method be used as a method value?**

```go
type Conn owner struct{ ... }
func (c Conn) Close() error   // value receiver: consumes c
done := c.Close               // a method value
done(); done()                // ?
```

- (a) A compile error: the record would own `c`, and every call would consume it again. The method
  expression `Conn.Close(c)` and the direct call `c.Close()` still work.
- (b) The method value moves `c` into its record and may be called once; a second call panics.

**Recommendation: (a).** A compile error is free and certain (**Safe**, **Fast**); (b) needs a run-time
check and fails in production (**Safe** cost).

**G27. Which statements end a function for the missing-return rule?**
Go lists the terminating statements (`return`, `panic(...)`, an endless `for`, and some `if`, `switch`
and `select` forms). Plutonium already adds an exhaustive union type switch.

```go
func name(c Color) string {
	switch c {               // every member listed, no default
	case Red:   return "red"
	case Green: return "green"
	case Blue:  return "blue"
	}
}                            // (i): is a return needed here?

func fail() { os.Exit(1) }
func f() int { fail() }      // (ii): is a return needed here?
```

- (i) (a) An exhaustive enum switch also terminates, as a union switch does; (b) only unions.
- (ii) (a) A call of a function that the compiler infers never returns also terminates; (b) Go's list
  only.

**Recommendation: (i)(a) and (ii)(b).** Enums and unions should behave alike (**Familiar** within
Plutonium), and the enum case adds no surprise because the switch is visibly complete. For calls, (a)
would make whether `f` compiles depend on the body of `fail`, possibly in another package, so editing
one function could break callers far away (**Plain** cost). The `//pu:panic` check keeps using the
inference, where it is the point.

**G28. What does `clear` do to owners and to maps?**

```go
var files [8]os.File
clear(files[:])      // ?
clear(m)             // m is a map[string]string
```

- (a) `clear(s)` assigns the zero value to each element, and assigning to a place that holds an owner
  drops the old value first, so every file is closed. `clear(m)` deletes every entry and releases
  counted keys and values.
- (b) `clear` on a slice of owners is a compile error.

**Recommendation: (a).** It is the assignment rule the spec already has, applied element by element, so
nothing leaks (**Owned**) and `clear` keeps Go's meaning (**Familiar**).

**G29. Can an iterator yield owners?**
`iter.Seq[V]` has `V any`, which does not admit owners. Allowing it would mean every iterator adaptor
must move its values: `iter.Filter`'s predicate takes `V` by value, so calling it on an owner would
consume the value before it could be yielded.

```go
for f := range dir.Files() { ... }   // (a): f is an os.File, owned by the loop body
for f := range dir.Files() { ... }   // (b): f is a *os.File, a view of the directory's file
```

- (a) Declare `Seq[V owner any]`: each yielded owner moves into the loop variable and is dropped at the
  end of the iteration unless the body moves it. Every generic function over iterators must then be
  written for owners (predicates take `*V`, adaptors move).
- (b) Iterators over owners yield views (`*T`), as a range over a slice of owners already requires
  `&s[i]`. A container that hands its owners out does it with a method (`v.Pop()`, `m.Remove(k)`) in a
  loop.

**Recommendation: (b).** It matches the rule for slices of owners, keeps every iterator adaptor simple
and copying (**Plain**), and needs no new rule (**Familiar**). A draining iterator can be added later if
real code asks for one.

### 3.4 Group 4: strings and bytes

**G10. What do `[]byte(s)` and `[]rune(s)` become?**
Today they return "what `make` returns". The accepted false-friends decisions remove `string(b)` (F2)
and make `make([]T, n)` a compile error pointing to `vec.Make` (F16), so the current text contradicts
both.

```go
b := []byte(s)   // today: a slice from the current allocator, freed by nobody
```

- (a) The conversions return a `vec.Vec[byte]` (or `vec.Vec[rune]`), an owner. But the source says
  `[]byte` and the value is not one: `var b []byte = []byte(s)` fails with "cannot use vec.Vec[byte] as
  []byte", about a type the line never names.
- (b) Remove both conversions, with a compile error that shows the replacement:
  `b := strings.Bytes(s)` (a `vec.Vec[byte]`), `n := copy(buf[:], s)` into a caller's buffer (no
  allocation), and `for i, r := range s` or `strings.Runes(s)` for runes.
- (c) Keep returning a `[]byte` that nothing owns.

**Recommendation: (b).** It follows the reasoning of F2 and F16: the source names the type it gets
(**Visible**) and every allocation has an owner (**Owned**). The cost is that a common Go idiom stops
compiling (**Familiar**), with the fix in the message. (c) leaks (**Owned** cost), and (a) is the
confusing type error F16 was accepted to avoid. `string(runes)` stays, as F2 decided.

**Decided and implemented (b)** (E2016, language spec sections 3 and 6.10, roadmap item 61).

**G11. What does `string(65)` mean?**
Go accepts a conversion from any integer type to `string`, giving the character with that code
(`string(65)` is `"A"`), a known trap that `go vet` reports since programmers expect `"65"`.

- (a) Keep Go's rule.
- (b) Accept only values of type `rune` and untyped constants; other integer types are a compile error
  that shows `string(rune(i))` and `strconv.Itoa(i)`.

**Recommendation: (b).** It is a compile error, so it is not a false friend (**Visible**), and it
removes a trap that Go itself warns about. The cost to **Familiar** is limited to code `go vet` already
flags.

**Decided and implemented (b)** (E2017, language spec sections 3 and 6.10, roadmap item 61). A named
type with underlying type `int32` is not `rune`: it converts through `rune(x)` first.

**G30. What is the helper for cutting a string on a rune boundary called?**
False-friends F12 keeps the panic for `s[i:j]` off a rune boundary and names a helper in the panic's
message.

```go
title = strings.Truncate(title, 40) + "..."   // never splits a rune
```

**Recommendation:** `strings.Truncate(s string, n int) string`, the longest prefix of at most `n` bytes
that ends on a rune boundary, and `utf8.RuneStartBefore(s string, i int) int` for a byte offset. Plain
names that say what they do (**Plain**).

### 3.5 Group 5: the built-in map

The decision of 2026-10-10 brings `map[K]V` back as a counted reference with Go's semantics. Three
details remain.

**G15. Can a built-in map hold owners?**

```go
m := map[string]os.File{}
m["log"] = f            // moves f in?
g := m["log"]           // a copy of an owner: a second owner of one file
```

- (a) Neither keys nor values may be owners; the error points to `hash.Map`, whose `Ptr`, `Remove` and
  `Pop` methods look at or take out an owner. Counted values (strings, `shared`, `synced`, channels,
  maps) are allowed and count as they are copied.
- (b) Values may be owners: `m[k] = v` moves `v` in, `delete` drops, reading `m[k]` is a compile error,
  and a new built-in takes a value out.

**Recommendation: (a).** The built-in map keeps Go's meaning, where reading an element copies it
(**Familiar**, **Visible**); a map type whose reads are compile errors would be a different thing under
Go's name. Owner containers already exist as library types (**Safe**).

**G16. Can a map be passed to another task?**

```go
counts := map[string]int{}
task count(words, counts)   // the new task writes counts
counts["x"]++               // and so does this one
```

- (a) Yes: a map is sendable, and its contents fall under the race rules of section 19.3, as the
  contents of a `synced[T]` do. Writes from two tasks must be guarded by a lock the compiler infers,
  partitioned in a scope, or confined to one task; otherwise it is a compile error naming both writes.
- (b) No: a map cannot cross tasks; shared tables go through `synced[hash.Map[K, V]]`.

**Recommendation: (a).** Passing a map to a goroutine is ordinary Go (**Familiar**), and the race rules
make it safe without annotations (**Safe**, **Plain**). Until those rules are implemented, a map shared
between tasks can race as it does in Go; the roadmap item for 19.3 covers maps.

**G17. Can `make` for a map take an allocator?**
`make(chan T, n, a)` does.

- (a) Yes: `make(map[K]V)`, `make(map[K]V, n)`, `make(map[K]V, n, a)`, with the rule channels use for
  telling a capacity from an allocator. A map literal allocates from the current allocator.
- (b) No; code that wants another allocator wraps the `make` in `alloc.Use`.

**Recommendation: (a).** Kernels and arena-based code choose where memory comes from at the call
(**Kernel**, **Owned**), and the form is the one channels already have.

### 3.6 Group 6: what safe code is promised

**G31. Does the language promise that a use of freed heap memory is caught?**
Views of heap memory are checked at run time by generation checks, which are opt-in today (D3) and do
not fit kernels that use the pointer's top bits (known hole 9).

```go
first := &ix.words.Slice()[0]
ix.Add("x")             // may grow words and free the old array
println(*first)         // (a): panics "use of freed memory"
```

- (a) The spec defines such a use as a run-time panic. A build without the checks is a documented
  unchecked mode, as `//pu:nochecks` is for one function; whether a build mode enables them by default
  follows D3 (hosted first, kernels once a tag encoding fits them).
- (b) The spec leaves such a use unspecified, as C does.

**Recommendation: (a).** **Safe** is the stated goal, and the spec is where it is stated. **Fast** is
handled by check hoisting and `//pu:nochecks` where measured, **Kernel** by the unchecked mode until a
kernel encoding exists. (b) gives up the goal in the definition of the language.

**G32. Can several tasks write a package variable?**
The memory-safety design's section 7.3 says no, unless the variable is atomic or guarded, but that part
is not among the signed-off decisions (D7, lock inference, is).

```go
var hits int
func handle(r *Request) { hits++ }   // called from many tasks
// (a): error: hits is written by tasks started at srv.p:10 and srv.p:31 with no lock held;
//      use atomic.Int64, or hold the same sync.Mutex at every access
```

- (a) Adopt it with D7: a package variable written after initialization by code that more than one task
  can reach must have a `pluto/sync/atomic` type or be guarded by an inferred lock.
- (b) Leave it as a data race, as in Go.

**Recommendation: (a).** Without it, 19.3 does not give race freedom, and **Safe** is not met. The
inference needs no annotation (**Plain**). Go code with racy globals stops compiling (**Familiar**
cost), and that code is wrong in Go too.

**G33. What does the memory model say about races?**
`memory-model.md` takes Go's position: a race is a bug with limited consequences, not undefined
behavior. With 19.3, safe code cannot race at all.

**Recommendation:** keep Go's limited-consequences rule for unsafe code, and state that safe code is
free of data races. It records what **Safe** delivers and keeps the rule that the compiler may not
assume race freedom where unsafe code can still race.

**G34. What stops an allocation from a local arena outliving the arena?**
Known holes 3 and 14 of the memory-safety design: an owner or string allocated through `Default()`
while a local arena is installed records the arena, and can be returned past it.

```go
func parse(src []byte) vec.Vec[Tok] {
	var mem [64 << 10]byte
	a := arena.New(mem[:])
	defer alloc.Use(&a).Restore()
	toks := vec.New[Tok]()    // allocates from a, through Default()
	...
	return toks               // (a): compile error: toks allocates from a, a local arena
}
```

- (a) The escape analysis treats what is allocated through `Default()` while a `Restore` that views a
  local arena is live as a view of that arena, so returning it is the usual frame-view error.
- (b) Rely on the arena epochs of D4 to catch the use at run time.
- (c) Document it as a hole.

**Recommendation: (a), with (b) for what (a) cannot see.** The `Restore` is already a view of the arena
(`//pu:resultholds`), so the analysis has the facts (**Safe**, **Plain**). (c) leaves a dangling
pointer in safe code.

**G35. Which allocator does a scoped task use?**
A new task starts with the program-wide default, not with the starting task's `alloc.Use`. A scoped task
(`s.Task(f)`) is joined before its scope ends, so a local arena would outlive it.

```go
defer alloc.Use(&a).Restore()
var s sync.Scope
for i := range parts {
	s.Task(func() { out[i] = render(parts[i]) })   // where does render allocate?
}
```

- (a) Scoped tasks inherit the starting task's override, so `render` allocates from `a`. But
  `arena.Arena` is not safe for concurrent use (memory-model.md 3.8), and the compiler cannot tell in
  general whether an installed allocator is, so parallel tasks could corrupt the arena.
- (b) Every task, scoped or not, starts with the program-wide default. A task that wants the arena
  installs one of its own: `s.Task(func() { defer alloc.Use(&ta[i]).Restore(); ... })`.

**Recommendation: (b).** (a) risks memory corruption in safe code (**Safe** cost). Under (b) the worst
case is that a scoped task allocates from the general heap where the reader expected the arena, a cost
in speed, not in correctness, and the rule is the one that already holds for every task (**Plain**).

### 3.7 Group 7: programs and tasks at run time

**G21. Are package-level owners dropped when the program ends?**
Go has no destructors, so the question is new.

```go
var out = bufio.NewWriter(os.Stdout)   // an owner at package level
func main() { out.WriteString("hi\n") }
// (a): nothing is flushed unless main calls out.Flush(), as in Go
// (b): out's Drop runs when main returns, and flushes
```

- (a) Never dropped, like Rust's statics. The process exit reclaims memory and descriptors, and the debug
  allocator reports memory still held by package variables apart from leaks.
- (b) Dropped in reverse order of initialization when `main` returns (not on `os.Exit` or a panic).

**Recommendation: (a).** It is what Go does at exit (**Visible**: no code runs that Go would not run),
costs no time tearing down large structures (**Fast**), and has no order problems between packages, where
a `Drop` could use another package's variable that was already dropped (**Safe**). Kernels never exit.
A program that must flush does it in `main` with `defer`, as in Go.

**G22. What happens to package initializers that allocate in a freestanding program?**
`var cache = make(map[string]int)` and `var root = new(Node)` run before `main`, when a kernel has no
allocator yet.

- (a) Allocate from the default allocator during initialization; a freestanding program panics "no
  allocator" before it has a console to report it on.
- (b) A compile error in freestanding builds, with the fix: initialize the variable in the kernel's
  entry function after `alloc.SetDefault`. Hosted builds allocate as in (a), where an allocator always
  exists.
- (c) Static storage for such allocations, never freed.

**Recommendation: (b).** A kernel learns about it at compile time instead of in a silent early crash
(**Kernel**), and hosted code keeps Go's behavior (**Familiar**). (c) needs an allocator kind that
never frees, used only here, and growing maps would still need a real one.

**G36. How large is the stack of a hosted task whose size cannot be computed?**
False-friends F8 accepts computed sizes where the call graph allows, and large lazily mapped stacks for
the rest, with the size left open. Go grows goroutine stacks up to 1 GiB.

- (a) 64 MiB of address space, with a guard region below, committed as it is touched: about two million
  such tasks fit in a 47-bit address space.
- (b) 1 GiB, Go's limit: deep recursion behaves as in Go, but only about 130,000 tasks fit.
- (c) 8 MiB, a thread's usual stack.

**Recommendation: (a).** Recursion a thousand times deeper than today's 64 KiB works (**Familiar**),
with no stack growth checks in function prologues (**Fast**), and enough tasks for servers. Kernel tasks
keep computed or explicit sizes (**Kernel**). The spec states that hosted tasks have at least this much
and that overflow is a panic; the number lives in the toolchain section.

### 3.8 Group 8: keywords, comments and directives

**G1. What does `go` become after the rename to `task`?**
The decision makes `task f(x)` the statement and `go f()` a compile error that points to `task`.

- (a) `go` stays a reserved keyword whose only use is that error.
- (b) `go` becomes an ordinary identifier, usable as a variable name.

**Recommendation: (a).** Go code fails at the statement with the fix in the message (**Visible**). With
(b), `go` could name a variable in Plutonium code, which a Go reader would misread.

**G2. When does `task` start a statement?**
`task` is contextual, so that it stays usable as an identifier (`rt.Task`, a variable named `task`). The
parser needs a rule.

```go
task worker(jobs)        // a task statement
task func() { ... }()    // a task statement
task.Run()               // a method call on a variable named task
task = next              // an assignment
```

- (a) `task` begins a task statement at the start of a statement when the next token is an identifier or
  `func`; otherwise it is the identifier.
- (b) It begins a task statement whenever the statement starts with `task` followed by a call
  expression, which needs the parser to look ahead past the whole call.

**Recommendation: (a).** One token of lookahead decides, the rule is easy to state (**Plain**), and the
only form it gives up, `task (f)(x)`, is written `task f(x)`.

**G3. Which new words are reserved?**
`enum` and `packed` are reserved keywords today; `union`, `owner` and `task` are contextual.

- (a) Keep the split and document it.
- (b) Make all five contextual.
- (c) Reserve all five.

**Recommendation: (a).** `union`, `owner` and `task` are common identifiers in Go code (set code,
ownership fields, job queues), so reserving them would break translated code for no gain (**Familiar**).
`enum` and `packed` are rare as identifiers, and reserving them keeps the grammar simple where they
begin type literals.

**G4. What happens to `//go:` directives in Plutonium source?**
`//go:build linux` and `//go:noinline` are ordinary comments today: the file is built everywhere and the
function may be inlined, with no message.

- (a) A `//go:` line comment is a compile error that names the `//pu:` equivalent, in the family of
  E2600 (unknown directive).
- (b) Leave them as comments.

**Recommendation: (a).** Code that looks like Go and silently does something else is the definition of a
false friend (**Visible**), and it is the reasoning that already made unknown `//pu:` directives errors.

**G5. Are struct tags allowed?**
Plutonium has no reflection, so nothing in the language reads a tag (`json:"name"`).

- (a) Keep tags as Go has them: part of type identity, otherwise ignored, and available to type
  descriptors for a future encoding library.
- (b) Make them a compile error.

**Recommendation: (a).** Tags cost nothing at run time, translated Go code keeps compiling (**Familiar**),
and a later `encoding/json` can read them without a language change. A tag never changes behavior, so it
is not a false friend.

**G37. Which directives exist, and in which group?**
AGENTS.md lists the ordinary requests (`build`, `inline`, `noinline`, `stack`, `extern`, `export`) and
the kernel and binding group (`interrupt`, `panic`, `scheduler`, `percpu`, `fpu`, `blocking`). The spec
also has `nobounds`, `section`, `nosimd` and `noreturn` (on functions without a body); the memory-safety
branch adds `noescape` (without a body) and `resultholds`; D6 adds `nochecks`.

**Recommendation:** `section` joins the linkage requests (it says where code goes, which the compiler
cannot know); `noreturn` and `noescape` join the group for bodies the compiler cannot see; `nosimd` joins
the kernel group; `resultholds` is listed as standard library only (E3008 elsewhere); `nobounds` retires
into `nochecks` when that lands, with E2601 naming the replacement. Every directive then states a fact
the compiler cannot derive, as the rule requires (**Plain**). AGENTS.md, spec section 20.3 and
`internal/lsp/hover.go` change together.

### 3.9 Group 9: numbers and constants

**G6. How precise are untyped constants?**
Go's spec sets minimums: integers of at least 256 bits, floats with at least a 256-bit mantissa and a
16-bit exponent. `internal/constant` exceeds them.

**Recommendation:** state Go's minimums as the requirement and leave the exact precision to the
implementation, as Go does. Constant arithmetic then gives Go's results (**Familiar**).

**G7. Can an enum member's value use `iota`?**
Member values are constant expressions evaluated in the scope around the declaration, where `iota` is
not defined.

```go
type Level enum {
	Debug = iota * 10   // (a): error
	Info                // already Debug + 1 without iota
}
```

- (a) `iota` is an error in an enum; a member without a value is already one more than the previous.
- (b) `iota` is the member's index.

**Recommendation: (a).** One way to number members (**Plain**); `iota` stays a property of `const`
declarations, as a Go reader knows it.

**G8. What does converting an out-of-range float to an integer give?**
`int8(300.0)` with a variable, or `int(math.NaN())`. Go says the value is implementation-dependent.

- (a) Go's wording, plus a promise that the conversion never faults.
- (b) Define the amd64 result (the "integer indefinite" value, `0x8000...`).
- (c) Saturate to the nearest representable value, as Rust does, at a cost of several instructions.
- (d) Panic.

**Recommendation: (a).** No correct program depends on the value, the conversion stays one instruction
(**Fast**), nothing is undefined (**Safe**), and Go programmers already live with the rule
(**Familiar**). (b) would tie the language to one processor (arm64 differs); (c) costs speed in numeric
code; (d) adds a hidden check to a conversion.

**G9. How wide are `int`, `uint` and `uintptr`?**
The spec says 64 bits "on all current targets", like Go's "implementation-specific size".

- (a) A language rule: 64 bits everywhere.
- (b) Go's rule: 32 or 64 bits, depending on the target.

**Recommendation: (a).** Both planned targets are 64-bit, and a fixed width removes a portability
question from every program (**Plain**). A 32-bit target would then be a deliberate language change.

### 3.10 Decisions

**Date:** 2026-10-10. The user decided all forty-two gaps, each as the recommendation given above, with
two notes: G24 is conditional on confirming that lowering matches gc (writers state gc's order, and the
roadmap has a verification item, 73), and G19 is structural now with a library interface later. This
table is the answer sheet: a writer reads it instead of the gap texts, and goes back to section 3 only
for the reasoning. A gap with several parts lists each part. Where the recommendation names no option
letter, the table gives the recommendation itself. Roadmap items that track the implementation are in
`docs/roadmap.md` (items 60 to 76).

| Gap | Decision |
|---|---|
| G1 | (a) `go` stays a reserved keyword whose only use is a compile error that points to `task`. |
| G2 | (a) `task` begins a task statement when it starts a statement and the next token is an identifier or `func`; otherwise it is the identifier. `task (f)(x)` is written `task f(x)`. |
| G3 | (a) `enum` and `packed` are reserved keywords; `union`, `owner` and `task` are contextual. |
| G4 | (a) A `//go:` line comment is a compile error that names the `//pu:` equivalent, in the family of E2600. |
| G5 | (a) Struct tags stay: part of type identity, otherwise ignored, available to type descriptors. |
| G6 | State Go's minimums as the requirement (integers of at least 256 bits, floats with at least a 256-bit mantissa and a 16-bit exponent) and leave the exact precision to the implementation. |
| G7 | (a) `iota` is an error in an enum; a member without a value is one more than the previous member. |
| G8 | (a) An out-of-range float to integer conversion gives an implementation-dependent value, as in Go, and never faults. |
| G9 | (a) `int`, `uint` and `uintptr` are 64 bits everywhere, by a language rule. |
| G10 | (b) `[]byte(s)` and `[]rune(s)` are removed; the error shows `strings.Bytes(s)`, `copy(buf[:], s)`, `for i, r := range s` or `strings.Runes(s)`. `string(runes)` stays. |
| G11 | (b) A conversion to `string` accepts only `rune` values and untyped constants; other integer types are an error that shows `string(rune(i))` and `strconv.Itoa(i)`. |
| G12 | (a) A struct may embed `owned[T]`, `shared[T]` or `synced[T]`, promoting `T`'s methods as an embedded `*T` does; a struct embedding `owned[T]` must be declared `owner`. |
| G13 | (a) An owner type is comparable when its underlying type is; comparing reads and moves nothing. `owned`, `shared`, `synced` and `weak` compare by address and with `nil`. |
| G14 | (a) Converting a value into an owner type is allowed only in the package that declares the owner type; converting an owner to a non-owner type reads the value and moves nothing; converting between owner types with the same underlying type moves. |
| G15 | (a) Keys and values of a built-in map may not be owners (the error points to `hash.Map`); counted values are allowed and count as they are copied. |
| G16 | (a) A map is sendable, and its contents fall under the race rules of section 19.3 as the contents of a `synced[T]` do. |
| G17 | (a) `make(map[K]V)`, `make(map[K]V, n)` and `make(map[K]V, n, a)`, with the rule channels use to tell a capacity from an allocator; a map literal allocates from the current allocator. |
| G18 | (b) Go's method-set rule: a value-receiver method of an owner type consumes its receiver and never satisfies an interface method, ordinary or owning; the error at the conversion shows `&b`. |
| G19 | Structural now (a): portable means no pointers, views, counted values (strings included), interfaces, func values or channels at any depth. A library interface for transferring handles and page-owning owners (b) follows with the PlutoniumOS IPC design; the spec says structural only and notes the extension point. |
| G20 | (a) `owned`, `shared` and `synced` cannot hold an interface type; the error points to owning interfaces. |
| G21 | (a) Package-level owners are never dropped when the program ends; the debug allocator reports memory still held by package variables apart from leaks. |
| G22 | (b) A package initializer that allocates is a compile error in freestanding builds, with the fix of initializing in the entry function after `alloc.SetDefault`; hosted builds allocate from the default allocator. |
| G23 | (a) `_ = x` is a read that moves nothing; `_ = open()` drops the temporary result at the end of the statement. |
| G24 | (c), conditional: state gc's order (calls, method calls and receives first, left to right, then the other operands), once a survey confirms that lowering evaluates this way. Writers state gc's order; roadmap item 73 verifies it, and a mismatch is fixed by matching gc. |
| G25 | (a) An expression that moves a variable and uses it anywhere else is a compile error, whatever the order; permuting assignments stay allowed. |
| G26 | (a) A consuming method used as a method value is a compile error; the method expression and the direct call still work. |
| G27 | (i)(a) An exhaustive enum switch terminates, as a union switch does. (ii)(b) A call of an inferred no-return function does not terminate; Go's list only. |
| G28 | (a) `clear(s)` assigns the zero value to each element, dropping owners first; `clear(m)` deletes every entry and releases counted keys and values. |
| G29 | (b) Iterators over owners yield views (`*T`); a container hands owners out with methods (`Pop`, `Remove`); `Seq[V any]` is unchanged. |
| G30 | `strings.Truncate(s string, n int) string` (longest prefix of at most `n` bytes ending on a rune boundary) and `utf8.RuneStartBefore(s string, i int) int`. |
| G31 | (a) A use of freed heap memory is defined as a run-time panic; a build without the checks is a documented unchecked mode; whether a mode enables them by default follows D3 (hosted first, kernels once a tag encoding fits them). |
| G32 | (a) A package variable written after initialization by code that more than one task can reach must have a `pluto/sync/atomic` type or be guarded by an inferred lock (landed, roadmap 59). |
| G33 | Keep Go's limited-consequences rule for unsafe code, and state that safe code is free of data races; the compiler may not assume race freedom where unsafe code can still race. |
| G34 | (a), with (b) for what (a) cannot see: allocations through `Default()` while a `Restore` that views a local arena is live are views of that arena for the escape analysis; arena epochs catch the rest. |
| G35 | (b) Every task, scoped or not, starts with the program-wide default allocator; a task that wants an arena installs its own. |
| G36 | (a) Hosted tasks whose stack size cannot be computed get 64 MiB of address space with a guard region, committed as it is touched. The spec states that hosted tasks have at least this much and that overflow is a panic; the number lives in the toolchain section. |
| G37 | `section` joins the linkage requests; `noreturn` and `noescape` join the group for bodies the compiler cannot see; `nosimd` joins the kernel group; `resultholds` is standard library only (E3008 elsewhere); `nobounds` retires into `nochecks` when that lands, with E2601 naming the replacement. AGENTS.md, section 20.3 and `internal/lsp/hover.go` change together. |
| G38 | (a) One language version: the spec header carries a version number, and no module records a language version until a release needs one. |
| G39 | (b) The standard library moves to the package documentation; appendix F keeps only the names the language depends on. |
| G40 | (b) The spec states each rule and its error code; the message texts live in the catalog and the golden tests; chapter 21 states what every message must contain. |
| G41 | (a) The memory model and the ABI become self-contained in phase 2, with no deference to Go's documents. |
| G42 | (a) Catalog citations of the Go spec point to the Plutonium section that defines the rule; a quoted Go sentence stays only where it is still true of Plutonium. |

**Design principles, inputs for every writer.** The four rules and four standing principles of
`docs/spec/language.md` (introduction and section 1.1) decide the cases a chapter leaves open. Each
chapter states its rules in their terms, and a writer who finds a rule that conflicts with one raises it
instead of resolving it silently.

The four rules (the goals table of section 3 gives the short names):

1. **Go-familiar, not Go-compatible.** Go's syntax and idioms wherever they fit; compiling Go source
   unchanged is not a goal.
2. **No false friends.** A difference from Go that a Go programmer would not expect is visible in the
   source (a different name, syntax or type, or a compile error that explains it). Code that looks like Go
   and compiles does what Go would do.
3. **No annotations for ordinary code.** What the compiler can work out, it works out; directives state
   only what it cannot see.
4. **Go as a test oracle.** Programs in the common subset of Go and Plutonium are kept as tests; the subset
   checks the compiler and is not a design constraint.

The four standing principles of section 1.1:

1. **Doubt resolves to a compile error.** Every static analysis rejects what it cannot prove safe; a false
   positive is fixed by improving the analysis, not by weakening the rule.
2. **Run-time checks change what a bug does, never what correct code means.** Generation and arena-epoch
   checks are on by default for hosted builds once their cost is acceptable and off for kernels until a
   kernel tag encoding exists; correct programs behave identically with and without them.
3. **Mechanism in the language, policy in the kernel.** The runtime provides mechanisms with hooks; the
   kernel supplies policy. Application code never sees the hooks.
4. **Bounded checks fail loudly, never silently.** At the end of a check's range the memory is retired or
   the field widened; a stale pointer never passes.

## 4. Licensing

**What the Go specification is licensed under.** The page https://go.dev/ref/spec links to
https://go.dev/copyright, which says (checked 2026-10-10): "Except as noted, the contents of this site are
licensed under the Creative Commons Attribution 4.0 License, and code is licensed under a BSD license."
The license is CC BY 4.0, not 3.0: the Go project moved its site content to 4.0. The source of the page,
`doc/go_spec.html` in the Go repository, has no license header of its own; the repository's `LICENSE` is
the 3-clause BSD license, "Copyright 2009 The Go Authors". The prose is therefore used under CC BY 4.0,
and code examples taken from it under BSD.

**What CC BY 4.0 requires of adapted text** (section 3(a) of the license):

1. Identify the creator ("The Go Authors"), keep the copyright notice, refer to the license and to its
   disclaimer of warranties, and give a link to the original, as far as reasonably practicable.
2. Say that the material was modified (and keep any earlier notice of modification).
3. Say that the original is licensed under CC BY 4.0 and link to the license.
4. Do it in any reasonable manner for the medium; one notice for the whole document is enough.
5. Impose no terms or technical measures that stop others from using the adapted material under CC BY
   4.0, and imply no endorsement by The Go Authors (section 2(a)(6)).

The license of Plutonium's own text is unaffected; the repository being private does not change any of
this.

**What BSD requires of code examples:** keep the copyright notice, the conditions and the disclaimer
with redistributed source. A copy of Go's `LICENSE` beside the spec does that.

**Proposed attribution**, at the end of `docs/spec/language.md` under a heading "Attribution":

> Portions of this specification are adapted from *The Go Programming Language Specification* (language
> version go1.27), https://go.dev/ref/spec, copyright 2009 The Go Authors, used under the Creative
> Commons Attribution 4.0 International License (https://creativecommons.org/licenses/by/4.0/). The text
> has been modified: it is reordered, rewritten and extended to define Plutonium, and many of its rules
> differ from Go's. Code examples adapted from it are used under the BSD license in
> [LICENSE-go](LICENSE-go). The adapted material is provided as is, without warranties, as those licenses
> state. The Go Authors do not endorse Plutonium.

Each chapter file that adapts Go text ends with one line:

> Adapted in part from the Go specification, copyright The Go Authors, CC BY 4.0; see
> [Attribution](../language.md#attribution).

Phase 2 adds `docs/spec/LICENSE-go`, a copy of Go's `LICENSE`.

Found while checking: `internal/types2` is a fork of go/types whose file headers point to "the LICENSE
file", but that directory has no copy of it (`internal/syntax` and `internal/diff` do). A copy should be
added there.

## 5. File structure

### 5.1 One file or several

The complete spec will be roughly three times today's 3,400 lines. The considerations:

- **References by number.** The catalog cites sections as `pspec("5.10", "Unions")`, and
  `TestSpecReferences` checks each against the numbered headings of `docs/spec/language.md`. About 330
  other places cite "spec section N" in compiler messages, golden outputs, library comments and docs.
  They cite numbers, not files, so either layout works as long as numbers are unique across the spec.
- **Links.** Only a few links point at anchors (`language.md#51-default-parameter-values` in
  debugging.md). The LSP hover cites sections in comments only.
- **Writing.** Six writers in parallel each editing one 10,000-line file would conflict on every rebase;
  later changes to the language also touch one chapter at a time.
- **Reading.** The Go spec is one page, and one page is easy to search. A collection of chapter pages
  with a contents page reads like the Rust reference.

**Recommendation: one file per chapter, with section numbers unique across the whole spec.**

```
docs/spec/language.md               the contents, the version, the attribution; no numbered sections
docs/spec/LICENSE-go                Go's BSD license, for adapted code examples
docs/spec/language/01-introduction.md
docs/spec/language/02-notation.md
...
docs/spec/language/21-implementation.md
docs/spec/language/a-differences-from-go.md
...
docs/spec/language/g-attribution.md   (or the attribution stays in language.md)
```

- The file prefix is the chapter number, so section 11.12 is in `11-expressions.md`, found without a
  lookup.
- `pspec` keeps its form (`"Slice expressions" in the Plutonium spec, section 11.12`), so diagnostics read
  as before.
- `TestSpecReferences` reads every file in `docs/spec/language/`, checks that each section number appears
  once and in the file its chapter prefix names, and accepts lettered appendix sections (`A.2`), which
  needs a small change to its heading pattern.
- `docs/spec/language.md` stays, so existing links to it still land on the contents.
- A single-page version, if wanted later, is a concatenation, since numbering is already global.

### 5.2 Numbering

Chapters and sections are numbered as in section 2. The numbers are fixed by this plan: a writer may add
a section at the end of a chapter they own, and renumbers nothing. Cross-references use numbers and
titles from section 2 (`section 11.12`), so a writer can cite a chapter that another writer has not
finished.

### 5.3 Old sections to new

For the phase 3 sweep (catalog `pspec` calls, "spec section N" in compiler messages, golden files, library
comments, abi.md, memory-model.md, debugging.md and the plans). Where one old section splits, the sweep
picks the new section by what the citing text is about.

| Old | New |
|---|---|
| 1 Goals | 1.1 |
| 2 Source files and packages | 3.3, 14.3, 14.4, 10.12 (`main`) |
| 3 What Plutonium removes from Go | the defining section (13.1, 11.24, 7.2), else A.2 |
| 4.1 panic aborts | 17.1 |
| 4.2 new and make allocate from an allocator | 13.9, 13.7 |
| 4.3 append works within capacity | 13.1 |
| 4.4 Interfaces hold a word or a frame pointer | 7.9 |
| 4.5 Closures do not outlive their frame | 11.5, 11.4, 18.4 |
| 4.6 Integer behaviour | 11.19, 7.2 |
| 4.7 go starts a puroutine | 12.12, 19.1, 19.2, 19.5, 19.6 |
| 4.8 Channels | 7.11, 12.13 |
| 4.9 Memory model | 19.7 |
| 4.10 Range over functions | 12.11 |
| 5.1 Default parameter values | 10.13 (calls: 11.14) |
| 5.2 volatile[T] | 7.15 |
| 5.3 aligned[T, N] | 7.16 |
| 5.4 ring[T, N] | 7.17 |
| 5.5 packed types | 7.14 |
| 5.6 owned, shared, synced and weak | 7.19 |
| 5.7 Methods with type parameters | 10.15 |
| 5.8 Standard bit operations | package documentation |
| 5.9 Enums | 7.12 |
| 5.10 Unions | 7.13 |
| 6 Ownership | 18 |
| 6.1 Why three kinds | 18.1 |
| 6.2 Values | 8.7, 18.1 |
| 6.3 Views | 18.3 |
| 6.4 Owners | 7.18, 18.5, 18.6 |
| 6.5 Shared ownership | 18.7 |
| 6.6 No allocation without an owner or an arena | 18.10 |
| 6.7 Indexing library containers | 11.11 (range: 12.10) |
| 6.8 Examples | 18.13 |
| 6.9 Diagnostics | the catalog (G40) |
| 6.10 Strings, 6.10.1 Count elision | 7.3, 18.8 |
| 7 Memory and allocation, 7.1 Allocators, 7.2 The system allocator | 18.9 |
| 7.3 The debug allocator | debug-allocator.md |
| 8 Runtime faults and panic | 17.1, 17.5 |
| 8.1 Panics in freestanding programs | 17.4 |
| 8.2 Deferred calls on a panic | 17.2 |
| 8.3 Ending a task with rt.Goexit | 17.3 |
| 9 Directives | 20.3 |
| 10 Assembly and C | 20.4 |
| 11 The unsafe package | 20.1 |
| 12 Build modes and targets, 12.1 Modes | 20.5 |
| 12.2 to 12.7 | 20.6 to 20.11 |
| 13 Standard library, 13.1 | appendix F, package documentation |
| 14 Errors, 14.1 to 14.4 | 16, 16.3 to 16.6 |
| 14.5 Differences in the standard library | package documentation |
| 14.6 Allocated errors and ownership | 16.7 |

## 6. The writing work

### 6.1 Before phase 2 starts

1. The user answers the gap list (section 3). Gaps still open when a writer reaches them are written as a
   block `> **Open (Gn).** ...` that states the question, so that nothing undecided reads as decided;
   phase 3 checks that none remain before the spec is called complete.
2. The memory-safety branch merges. Its `language.md` rewrites old sections 4.5, 6.3, 6.4, 6.9 and 7.1,
   and parts 3 and 5 take their text from it.
3. The roadmap gets an item for each decided feature that has none: the counted `map[K]V`, the removal of
   `append`, the removal of `string([]byte)` and of `make([]T, ...)`, `new` returning `owned[T]` (item 50
   becomes "accepted"), the `task` rename, `sync.Scope`, Go's map iteration guarantees and per-map seeds
   (F3, F17), the vector range snapshot (F11), large task stacks (F8, with item 18), the channel owning
   its timer (F6), `bytes.Buffer` (F9), Go's error types (F15), data race freedom (D5, D7, G32),
   `portable`, and whatever the gap answers add. Each section's status then names an item.
4. Phase 2 creates `docs/spec/language/` with one stub per chapter holding its numbered headings from
   section 2, so writers start from the same skeleton and the test can run from the first commit.

### 6.2 Rules for every writer

- Define the language as decided. Implementation gaps go in the roadmap, never in the text ("today the
  compiler..." does not appear).
- Self-contained: no rule is defined by reference to Go. "As in Go" may follow a complete rule as a note
  for Go readers, never replace it.
- Adapt Go's text where Go's rule holds, from `$GOROOT/doc/go_spec.html` of Go 1.27.1, and end the file
  with the attribution line of section 4.
- Give an EBNF production for every syntax Plutonium adds, in the chapter that defines it.
- Use "task" for what the current text calls a puroutine, and `task` for the statement.
- Keep the section titles of section 2, which the catalog cites.
- Short sentences, no em dashes, American spelling (behavior, initialization), terms defined at first
  use. Examples in fenced `go` blocks that compile, or are marked as errors.
- Cite other chapters by section number and title.

### 6.3 The parts

Six parts, each owning whole chapter files, so that writers never edit the same file.

**Part 1. Foundations and declarations.** Chapters 1 to 6, 9 and 10; appendices B, C and D.
Inputs: Go spec (Introduction to Variables, Blocks, Declarations and scope); old intro, 1, 2, 5.1, 5.7;
enums design (for `iota`, G7); the decisions log (design rules, task rename); gaps G1 to G7, G23.
Coordinates with part 2 on 10.8 (declarations refer to 7.12, 7.13 and 7.18 for what `enum`, `union`
and `owner` mean).

**Part 2. Types and their properties.** Chapters 7 and 8.
Inputs: Go spec (Types, Properties of types and values); old 4.4, 4.6, 4.8 (the type), 5.2 to 5.6, 5.9,
5.10, 6 intro, 6.2, 6.4 (branch text: the qualifier and placement), 6.10 (the type and its
representation); abi.md 1 and 1.1; enums design; sum-types proposal; the map decision; memory-safety
D5; gaps G8 to G20. Defines the kinds table (8.7) that parts 3 and 5 refer to.

**Part 3. Expressions and statements.** Chapters 11 and 12.
Inputs: Go spec (Expressions, Statements); old 4.5 (branch text), 4.10, 5.1 (calls), 5.9 and 5.10
(selectors, switches, assertions), 6.7, 6.10 (slicing, conversions, concatenation), 4.8 (send, receive,
`select`); remove-append plan; false-friends F2, F3, F11, F12, F16; gaps G10, G11, G13, G14, G24 to G30.
Coordinates with part 5: 11.5 owns closure regions, 18.4 owns loop storage reuse.

**Part 4. Built-ins, packages, programs, errors and panics.** Chapters 13 to 17.
Inputs: Go spec (Built-in functions, Packages, Program initialization and execution, Errors, Run-time
panics); old 2, 3, 4.1 to 4.3, 8, 8.1 to 8.3, 14; errors design and old 14.6; remove-append plan;
false-friends F4, F5, F13, F15, F16; gaps G17, G21, G22, G28, G38.

**Part 5. Ownership, memory and concurrency.** Chapters 18 and 19; making `memory-model.md`
self-contained (G41).
Inputs: old 4.7, 4.8 (counting), 4.9, 6 (branch text), 6.5, 6.6, 6.8, 6.10, 6.10.1, 7, 7.1, 7.2;
memory-safety design (all, with D1 to D9); scoped-puroutines plan (revised, with `s.Task`);
scheduler-contract plan; memory-model.md; abi.md 12; false-friends F6 to F8, F10, F14; gaps G16, G31 to
G36. The largest part; it can split in two (18 and 19) if a sixth writer is free.

**Part 6. System considerations, requirements and appendices.** Chapters 20 and 21; appendices A, F and
G; the contents page `docs/spec/language.md`; `docs/spec/LICENSE-go`; making abi.md's layout rules
self-contained (G41).
Inputs: Go spec (System considerations); old intro, 3, 9 to 12.7, 13; AGENTS.md (directives,
diagnostics, rule 4); false-friends audit (for appendix A); abi.md; debugging.md; gaps G4, G37, G39,
G40.

### 6.4 Phase 3: integration

One agent, after every part has landed:

1. Appendix E, the grammar, collected from the chapters.
2. `TestSpecReferences` over the chapter files (section 5.1), plus a check that no `> **Open` block
   remains when the spec is declared complete.
3. The renumbering sweep of section 5.3: `pspec` calls, compiler messages that cite "spec section N",
   `go test ./cmd/pluto -run TestDiagnostics -update` with the diffs read, library comments, abi.md,
   memory-model.md, debugging.md, debug-allocator.md and the roadmap.
4. Catalog entries that cite the Go spec move to Plutonium sections (G42).
5. Old `language.md` text that moved to package documentation (G39) lands in the packages' doc comments.
6. The full verification checklist of AGENTS.md.
