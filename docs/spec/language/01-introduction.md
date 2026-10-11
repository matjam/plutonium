## 1. Introduction

This is the reference manual for the Plutonium programming language. It defines the whole language: a
reader needs no other document about the language to know what a program means. The standard library is
documented with its packages (`pluto doc`), not here. This manual names only the library names that the
language itself depends on (appendix F, Library names the language relies on).

Plutonium is a general-purpose systems programming language. It is strongly typed, has no garbage
collector, and has explicit support for concurrent programming. Programs are constructed from packages,
whose properties allow efficient management of dependencies. The syntax is compact and simple to parse,
which allows easy analysis by tools such as editors.

### 1.1 Scope and goals

Plutonium is a language for operating system kernels, drivers, boot code, allocators and other code that
runs without a managed runtime. It is the language PlutoniumOS is written in. It is equally meant for
ordinary application programs: servers, command line tools and libraries. The language has two audiences,
and every rule in this manual has to work for both:

- **Kernel programmers**, who need bit-exact layouts, control of every allocation, and rules that can be
  checked at compile time because a run-time check may be unavailable or unaffordable.
- **Application programmers**, who need strong guarantees about memory and data races without writing
  annotations or learning how the compiler works.

The language has no garbage collector. Tasks and channels run on a scheduler that only programs using
them carry, and that a kernel supplies itself (section 19.5, The scheduler contract).

The goals are:

- **Read like Go, and never mislead a Go reader.** Code that differs from Go looks different (section
  1.2, Design rules).
- **Compile like C.** There is no garbage collector, no hidden allocation and no runtime the program did
  not ask for. A program that never starts a task or makes a channel contains no scheduler.
- **Make costs visible.** Every allocation has an owner or an arena, and the source says which (section
  18.10, No allocation without an owner or an arena). Every memory-mapped register access is marked in its
  type (section 7.15, `volatile[T]`).
- **Make ownership visible.** Every piece of memory has one owner, named by its type, and is freed when
  that owner is dropped. Looking, moving and sharing are written differently (chapter 18, Ownership and
  memory).
- **Give kernel code the tools it needs in the language:** bit-exact layouts (section 7.14, `packed`
  types), volatile access, alignment control (section 7.16, `aligned[T, N]`), ring buffers (section 7.17,
  `ring[T, N]`), and a fatal-error path that works before any allocator exists (section 17.4, Panics in
  freestanding programs).
- **Generate fast code.** The language is designed so that an optimizing compiler can match the
  performance of Rust, and a conforming implementation is measured against it.

**What this manual covers.** It specifies the source text and its tokens (chapters 3 and 4), constants,
variables and types (chapters 5 to 8), the scope rules and declarations (chapters 9 and 10), expressions
and statements (chapters 11 and 12), the built-in functions (chapter 13), packages and program execution
(chapters 14 and 15), errors and run-time panics (chapters 16 and 17), ownership, memory and concurrency
(chapters 18 and 19), the parts of the language that exist for systems code, such as directives, assembly
and C, build modes and kernel mode (chapter 20), and what an implementation must do (chapter 21).
Appendix A lists the differences from Go.

**What it does not cover.** The behavior of standard library packages belongs to their documentation. The
exact text of compile error messages belongs to the diagnostics catalog (`pluto explain`) and its tests.
Chapter 21 states what every message must contain. The command line of the compiler is not part of the
language.

### 1.2 Design rules

Four rules shape the language. They decide the cases that a chapter leaves open, and a rule in this
manual that conflicts with one of them is a defect in the manual.

1. **Go-familiar, not Go-compatible.** Plutonium uses Go's syntax and idioms wherever they fit, so a Go
   programmer can read Plutonium code without a manual. Compiling Go source unchanged is not a goal.
   Where Go's meaning depends on a garbage collector, a growing stack or hidden allocation, Plutonium
   changes the language, and Go code may need edits before it compiles.

2. **No false friends.** Where Plutonium behaves differently from Go in a way a Go programmer would not
   expect, the difference is visible in the source: a different name, a different syntax, a different
   type, or a compile error that explains it. Code that looks like Go and compiles does what Go would do.
   The one documented exception is slicing a string at a position that is not on a character boundary
   (appendix A, section A.5, The one documented exception).

3. **No annotations for ordinary code.** Ordinary code needs no annotations and no knowledge of how the
   compiler works. What the compiler can work out for itself (what escapes, what moves, which functions
   never return or may wait, which lock guards which data) it works out, and there is no `//pu:`
   directive for such a fact. Directives state only what the compiler cannot see: linkage to assembly and
   C, hardware state, bindings to the runtime, and explicit requests such as `//pu:inline` (section 20.3,
   Directives).

4. **Go as a test oracle.** Programs in the common subset of Go and Plutonium are kept as tests. Their
   expected output comes from running them with Go, and generated programs are run under both compilers
   and compared. The subset is how an implementation is checked against Go's behavior. It is not a
   design constraint: a change that moves a program out of the subset is judged on its own merits.

Four standing principles decide the cases the rules leave open. They serve the two audiences: kernel code,
where a run-time check may be unavailable or unaffordable, and application code, which should get strong
guarantees without writing annotations.

1. **Doubt resolves to a compile error.** Every static analysis (escape, moves, sendable types, lock
   guarding, closure regions) rejects what it cannot prove safe and never accepts on doubt. A false
   positive is fixed by improving the analysis, not by weakening the rule. Applications get Rust-level
   guarantees without annotations, and kernels get them at compile time.

2. **Run-time checks change what a bug does, never what correct code means.** Generation and arena-epoch
   checks (a use of freed memory is a panic, section 18.11, Generation checks) are on by default for
   hosted builds, and off for kernel and freestanding builds until a kernel tag encoding exists. A build
   without them is a documented unchecked mode, not a dialect: correct programs behave identically under
   both. A program that is correct must not depend on whether the checks are present, so each audience can
   choose its build without rewriting code.

3. **Mechanism in the language, policy in the kernel.** The runtime provides mechanisms with hooks, and the
   kernel supplies policy. The runtime owns the heap, and the kernel supplies the clock and the timer for
   timers. The scheduler contract and the panic handler are hooks. New tasks start with interrupts
   enabled, so the kernel installs its table first. Allocators are chosen per call. Application code never
   sees the hooks and needs no directives. Kernel directives exist only for facts about hardware the
   compiler cannot see. A kernel must decide for itself how time, scheduling and failure work, while an
   application should not have to know that these decisions exist.

4. **Bounded checks fail loudly, never silently.** Where a check has a finite range (slab generations and
   arena epochs, which use one program-wide counter so that a reused chunk cannot match a stale pointer),
   reaching the end of the range retires the memory or widens the field. It never lets a stale pointer
   pass. A check that can start passing stale pointers by accident gives a false assurance, which is worse
   for a kernel than no check.

### 1.3 Relation to Go, and how to read this specification

**Plutonium is defined here, not by reference to Go.** Plutonium descends from Go, and many of its rules
are Go's. This manual states each of them in full. Where it is silent, the language has no rule: it does
not fall back on Go's specification. Go is named only in this chapter, in appendix A (Differences from
Go), which lists every difference for readers who know Go, and in appendix G (Attribution). The other
chapters state the Plutonium rule alone.

**One language version.** The manual carries a version number in its header. Every feature in it is part
of the language. A module does not record a language version, and no feature is enabled by one. A release
that needs versions will introduce them in a later edition of this manual.

**Conformance.** A *program* is a set of packages (chapter 14). A program is *valid* if it satisfies every
rule of this manual. An implementation must reject an invalid program with a compile error and must not
produce an executable for it (section 21.2, No silent failures). Where this manual says that something "is
an error" or "is a compile error", an implementation reports it before running anything. Where it says
that something "panics", the program stops at run time as section 17.1 (Panics) describes. Where it says
that a value is "implementation-dependent", any value of the type may result, and the operation neither
faults nor has any other effect. Where it says that behavior is "unchecked", only code that this manual
calls unsafe (section 18.12, Safe and unsafe code) can reach it.

**Notation.** Syntax is given in the extended Backus-Naur form of chapter 2 (Notation). Each chapter
gives the productions for the syntax it defines, and appendix E collects them.

**Examples.** Examples are fenced `go` blocks. An example compiles unless a comment on the offending line
begins with `error:`. A comment that begins `panics` marks a line that panics when it runs. Names such as
`fmt`, `vec` and `alloc` in examples are packages of the standard library.

**References.** A reference such as "section 11.12" or "chapter 18" names a section or chapter of this
manual by number. A reference to a compile error gives its code, such as E2013, which `pluto explain`
describes. This manual states each rule and its code. The text of the message is in the catalog.

**Words with a fixed meaning.** *Task* is the unit of concurrent execution (section 19.1, Tasks), and
`task` is the statement that starts one (section 12.12, Task statements). *Reserved* and *contextual*
words are defined in section 4.5 (Keywords) and section 4.6 (Contextual keywords).

### 1.4 Terms

These terms are used throughout. Each is defined in full in the section cited.

- **Task.** A lightweight thread of execution that the program's scheduler runs. A task has its own stack.
  Tasks run in parallel and are preempted (section 19.1, Tasks).
- **Value.** Data that copies on assignment, such as an integer, a float, a boolean, an enum, a packed
  value, and an array, struct or union made of values. Copying a value never gives a second owner of
  anything (section 8.7, Values, views, owners and counted values).
- **View.** A value that looks at memory something else owns, without owning or freeing it. Pointers
  (`*T`), slices (`[]T`) and `unsafe.Pointer` are views, and so are closures and method values that
  refer to a frame. Creating, copying and passing a view is free, and a view may not outlive what it
  looks at (section 18.3, Views).
- **Owner.** A value that holds memory or another resource and releases it when it is dropped. A type is
  an owner only if its declaration says so with `owner`. `owned[T]` is the built-in owner of a single heap
  object. An owner is moved, not copied (section 7.18, Owner types).
- **Counted value.** A value that refers to memory shared with its copies and counts the references.
  Copying it adds a reference, dropping a copy releases one, and the memory is freed when the last
  reference goes. Strings, channels, maps, and `shared[T]`, `synced[T]` and `weak[P]` references are
  counted values (section 8.7, Values, views, owners and counted values; section 18.7, Shared ownership).
- **Box.** One of `owned[T]`, `shared[T]` and `synced[T]`: one word, the address of a heap object that the
  box owns (`owned[T]`) or counts (`shared[T]`, `synced[T]`). A `weak[P]` is a weak reference to the
  object of a counted box, not a box itself (section 7.19, `owned`, `shared`, `synced` and `weak`).
- **Kind.** Whether a type is a value, a view, an owner or a counted value. The kind of a type decides how
  its values are copied, moved and released (section 8.7).
- **Frame.** The memory of one call of a function: its parameters, results, local variables and
  temporaries. A frame ends when the call returns (section 18.2, Where variables live).
- **Region.** Memory in a caller's frame that the caller reserves for the closures a callee returns, so
  that a returned closure lives as long as the caller's frame and costs no allocation (section 11.5,
  Closures and their regions).
- **Move.** Passing an owner from one place to another. The source then holds nothing, and using it before
  it is assigned again is a compile error (section 18.5, Moves).
- **Drop.** The end of an owner's life: its `Drop` method runs if it has one, its fields are dropped, and
  its memory is released. Dropping a counted value releases its reference. The compiler drops every owner
  and counted value a variable still holds when the variable's scope ends (section 18.6, Drops).
- **Scope.** The part of the source in which a declared identifier denotes what it declares (chapter 10,
  Declarations and scope). The end of a variable's scope is where what it holds is dropped. The library
  type `sync.Scope` is named for this: its tasks are joined when its own scope ends (section 19.4, Scoped
  tasks).
- **Temporary.** A value that an expression produces and nothing stores. A temporary owner is dropped at
  the end of the statement that made it (section 12.4, Expression statements; section 18.6).
- **Place.** A variable, a field of an addressable struct, an element of an array or slice, or the
  target of a pointer indirection: something that can be assigned to and whose address can be taken
  (section 11.22, Address operators).
- **Allocator.** A value of the type `alloc.Allocator` that supplies memory. An arena is an allocator that
  frees everything at once (section 18.9, Allocators).
- **Sendable.** A type whose values may cross from one task to another, as arguments of a task statement
  or elements of a channel (section 8.8, Sendable types; section 19.2, The sendable rule).
- **Portable.** A type whose values may be sent between address spaces, because they refer to nothing in
  the sender's memory (section 8.9, Portable types).
- **Hosted, freestanding and kernel builds.** Hosted programs run under an operating system. Freestanding
  and kernel programs run without one and supply their own entry point (section 20.5, Build modes and
  targets).
- **Directive.** A line comment that begins `//pu:` and states a fact the compiler cannot work out for
  itself (section 20.3, Directives).
- **Safe and unsafe code.** Code that the compiler checks against data races and the misuse of memory is
  safe. Code that uses package `unsafe`, the unchecked layer of the allocator interface, or functions
  written outside Plutonium is unsafe (section 18.12, Safe and unsafe code).
- **Checked and unchecked mode.** A checked build makes a use of freed heap or arena memory a run-time
  panic. A build without those checks is the unchecked mode, in which such a use has no defined effect.
  Correct programs behave the same in both (section 18.11, Generation checks).

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
