# Memory Safety in Plutonium: Design

**Date:** 2026-10-09 (updated 2026-10-10)
**Status:** sections 4 and 6.1 to 6.3 are implemented and enforced; section 5 is a measured prototype
behind `pluto build --gencheck`, off by default; sections 6.4, 7 and 8 are proposals. Section 12 lists
the decisions that need sign-off.

## 1. The goal

Safe Plutonium code must not be able to corrupt memory or race, as safe Rust cannot, while reading like
Go: no lifetime annotations, no borrow checker that rejects programs for reasons the programmer cannot
see, and no garbage collector. Where a compile-time proof would need lifetimes, the compiler inserts a
cheap, deterministic run-time check instead, and every compile error explains itself in the program's
own names (spec section 6.9).

Memory safety splits into four properties, each handled by a different mechanism:

| Property | Threat | Mechanism | State |
|---|---|---|---|
| Spatial | out-of-bounds index, slice or pointer arithmetic | bounds checks | enforced (existing) |
| Stack temporal | a pointer into a frame used after the frame returns | whole-program escape check at compile time | enforced (section 4) |
| Heap temporal | a pointer into freed heap memory | ownership (moves, drops) at compile time, generation checks at run time | moves and drops enforced (section 6); generation checks prototyped (section 5) |
| Concurrency | data races; torn multi-word values | sendable types, restricted sharing | spec rule exists; proposals in sections 7 and 8 |

Rust gets all four from one mechanism, borrowing with lifetimes. Plutonium gets the first and third
mostly from run-time checks and the second and fourth from whole-program analyses that need no
annotations. The price is a run-time cost on heap pointer accesses (measured in section 5) and a few
patterns that are compile errors where Go accepts them (each listed with its fix in spec section 6.9).

## 2. Safe and unsafe code

**Definition.** Code is *safe* when nothing it does can corrupt memory or race, whatever its inputs. The
compiler guarantees this for every function that uses none of the following, which make up *unsafe
code*:

- package `unsafe` (`unsafe.Pointer`, `Add`, `Slice`, `String`, `Drop`, conversions to and from
  `uintptr`);
- the unchecked allocation layer: `alloc.Raw`, `alloc.New`, `alloc.Free` and an `Allocator`'s methods
  called directly;
- functions without a body (assembly, `//pu:extern`) and the directives that make promises the
  compiler takes on trust: `//pu:noescape`, `//pu:resultholds`, `//pu:nobounds` and the proposed
  `//pu:nochecks` (section 10);
- `volatile[T]` at a fixed address, `pluto/arch/x86`, and memory made from an integer (device
  registers, DMA buffers).

**Marking.** Unsafe code is visible without a new keyword: every use above is either an import of
`unsafe` or `pluto/arch/x86`, a call into `pluto/alloc`'s unchecked functions, or a `//pu:` directive.
That is how Go marks it too. I propose one addition: `pluto vet -unsafe` lists, per package, the
functions that use unsafe code and the safe functions that call them, so that a reviewer can audit the
unsafe surface (decision D1). A function that wraps unsafe code in a safe interface, as `vec.Vec` and
`hash.Map` do, is responsible for upholding the invariants that make it safe, exactly as in Rust.

The standard library is where most unsafe code lives: allocators, containers, `fmt`'s type-descriptor
walker, the runtime. Applications built on it should need none.

## 3. Spatial safety

Implemented before this work and unchanged:

- Indexing an array, slice, string or ring, slicing (including three-index `s[i:j:k]`, which checks
  `i <= j <= k <= cap`), and conversions of slices to arrays are bounds checked and panic on failure.
  The optimizer removes checks it can prove redundant (`TestProveBoundsChecks`).
- Library containers index through methods (`x[k]` calls `Index`, spec section 6.7) that check their own
  bounds; `&x[k]` on a container is a compile error, because a pointer into storage that grows would
  dangle.
- `packed` fields are offsets and widths fixed at compile time; there is nothing to check at run time.
- `ring[T, N]` indexes modulo its capacity.

What is not checked, and therefore unsafe: `unsafe.Slice` and `unsafe.String` trust their length;
`unsafe.Add` trusts its offset; `//pu:nobounds` removes every bounds check in a function, so it counts as
unsafe code (section 2).

## 4. Stack temporal safety (implemented)

**Rule (spec section 6.3).** Every local variable, parameter, temporary, interface box of a value larger
than a word, closure record, method value record, slice literal's array and variadic argument array
lives in its function's frame, never on the heap. A pointer into a frame (a *view of the frame*) may be
used, copied into locals, stored in the same frame and passed to calls. It may not be returned, stored in
a package variable, stored in memory that may be on the heap, stored through a pointer the function
received, sent on a channel, passed to a `go` statement, or passed to a function that does any of these
with it. A view of a loop's variables may not be kept where the next iteration can see it (spec section
4.5): every iteration reuses the same frame memory, where Go gives each its own.

**Why this rule.** It is conservative, but every rejection is explainable in one sentence with a
concrete path, and it never depends on optimizer decisions: whether a program compiles does not change
with `-O`. Go's escape analysis decides the same question to place variables; Plutonium decides it to
reject programs, so the analysis must be predictable rather than clever.

**Implementation (`internal/escape`).** A whole-program analysis on the typed AST, after lowering and
before optimization:

- *Abstract values.* A pointer-carrying value is a set of tokens: the heap (anything not in a frame),
  a frame location with a field path, or a parameter followed along an access path of field steps and
  loads (at most three loads and eight steps; a longer path is cut to "this or anything reachable").
  Struct values carry a field tag per token, so a struct's pointer fields are told apart.
- *Summaries.* For each function: which parameter paths it keeps (escapes), which it stores into memory
  another parameter reaches (store edges), and which it returns, per result field. A call applies the
  callee's summary to its arguments. Summaries are computed to a whole-program fixpoint; recursion and
  mutual recursion converge because summaries only grow.
- *Dynamic calls.* A call through an interface uses the union of the summaries of every method of that
  name on a type converted to an interface somewhere in the program (a type parameter converted to an
  interface counts as each type it is instantiated with). Each candidate's receiver is marked with its
  type, so a caller that knows the dynamic type drops the others. Calls through function values use
  every function of that signature used as a value; a value that can only be closures made in this
  function narrows to those.
- *Flow.* Locals whose address is never taken are followed along the control flow (assignment replaces,
  paths join), so reusing a variable such as `err` for a frame view and later for heap memory is fine.
  Other storage is flow-insensitive. Loop back edges check that no followed local still holds a view of
  the iteration's storage.
- *Heap-only fields.* A struct field whose address is never taken, and into which every store in the
  program puts heap memory or a parameter that every call passes heap memory for, loads as heap. This is
  what lets an arena's chunk list or a linked list's `next` be followed without making the caller's
  pointers escape. Writes through memory reinterpreted with `unsafe.Pointer` are not seen.
- *Escape hatches with checks.* A function without a body keeps every pointer argument unless marked
  `//pu:noescape`. `//pu:resultholds p` says a function keeps `p` only inside the owner it returns
  (`alloc.Use` returns a `Restore` that holds the allocator); the compiler checks that the result does
  hold `p`.
- *Messages.* Every error names the expression, what it points into and where that was declared, why the
  place outlives it (with the callee chain, file and line of each step, for a call), and fixes as code
  (codes E3001 to E3007, spec section 6.9).

**What it found.** Turning the check on found real dangling pointers in the tree: test programs that
returned `&T{...}` or stored frame pointers in package variables, `fmt`'s printer keeping the address of
the value being printed in a field, `os.File` keeping a view of a path in the caller's buffer (now `File`
owns a copy of its name), `os.DirEntry.Info` returning a `FileInfo` that viewed a local buffer, the
self-hosting translations passing variadic argument arrays to functions that keep them, and Go code whose
closures keep loop variables. Each is fixed in the tree.

## 5. Heap temporal safety at run time: generational references (prototype)

Ownership (section 6) makes the compiler free heap memory exactly once, at a known place. What it cannot
prove is that no view of that memory (`*T`, `[]T`) is used afterwards, because views have no lifetimes.
Generation checks catch such a use when it happens.

**Representation.** A heap pointer carries a 16-bit generation in its top bits (63..48; x86-64 user
addresses use 47 bits). Small allocations come from 64 KiB-aligned slab chunks whose first 8 KiB hold one
16-bit generation per 16-byte granule of the chunk; every granule of a slot holds the slot's
generation. `Free` checks the pointer's tag against the slot (catching double frees), then increments the
slot's generation. Generations start at 1; tag 0 means "not checked", which is what pointers to the
stack, static data, devices and large allocations carry.

**Which accesses are checked.** Every load and store through a pointer whose value is not statically a
frame slot or a symbol. The check is: if the tag is non-zero, load the generation of the granule the
address falls in (one shift and mask from the address) and compare; on a mismatch, panic with "use of
freed memory". The pass (`opt.GenCheck`) runs last, after inlining and scalar replacement, so it checks
only the memory accesses that survive optimization, and within a straight run of code it checks each
base pointer once (the check is redone after a call, which may free). Converting a pointer to `uintptr`
removes its tag (so a pointer rebuilt from an integer is unchecked, as unsafe code is), as does passing
it to a function implemented outside Plutonium. `unsafe.Pointer` keeps the tag. The runtime and the
allocator remove tags without checking.

**Wraparound.** A slot whose generation would wrap from 65535 back to 1 is retired and never handed out
again: a stale pointer can then never match by accident. The cost is one slot lost per 65535 reuses.

**Arenas.** Arena allocations are untagged in the prototype: an arena frees everything at once, and views
of arena memory are checked only by the arena's own discipline. A design that covers them gives each
arena an epoch in its chunk headers, bumped by `Reset`, with the same tag check (decision D4).

**Measurements.** Hosted x86-64, `bench/run.go` programs built with and without `--gencheck`, minimum of
five runs, instructions and cycles counted with `perf_event_open`:

| benchmark | instructions | cycles | notes |
|---|---:|---:|---|
| binarytrees | 1.75x | 1.81x | allocation and pointer chasing: every node access is checked |
| strings | 1.36x | 1.24x | builder and slice accesses in inner loops |
| spectralnorm | 1.33x | 1.19x | slices of heap vectors, checks not hoisted out of loops |
| fasta-lcg | 1.62x | 1.02x | checks overlap with other work |
| sha256 | 1.09x | 1.15x | |
| quicksort, heapsort, nbody, structs, fannkuch, mandelbrot, fib, crc32, collatz | 1.00x | 0.99x to 1.02x | no heap pointers in the hot loops |
| sieve, matmul | 1.00x | 0.89x, 0.75x | identical instructions: code layout noise |

The geometric mean over the sixteen programs is about 1.05x in cycles and 1.11x in instructions;
pointer-heavy code pays up to 1.8x. Memory: the generation table is 12.5% of each slab chunk, and the prototype over-allocates to
align chunks. The main remaining costs are checks inside loops that a loop-invariant hoist would remove
(the base pointer does not change; only a call can free), and binarytrees' per-node checks, which a
"checked once per function entry for pointers loaded from a non-escaping owner" rule would cut further.

**Recommendation (decision D3).** Keep generation checks as an opt-in build mode until loop hoisting
lands, then measure again and decide whether to make them the default for hosted builds. They are not
needed for memory the compiler can prove: values in owners never freed early, and the frame (section 4).

## 6. Ownership: owners, moves, drops, sharing

### 6.1 Owners and moves (implemented)

A type is an owner only if declared `type T owner ...` (spec section 6.4); the checker enforces where the
mark may and must appear (a struct with an owner field must say `owner`, `Drop` needs `owner`, a type
parameter admits owners only with `[T owner any]`; codes E3201 to E3209). The move checker
(`internal/moves`) follows each function's control flow: assigning, passing by value, returning,
sending, storing an owner, calling its value-receiver method or `x.Drop()` moves it; the variable it left
holds nothing until it is given a new value. Uses after a move, uses where a value may have moved on
some path (including a previous loop iteration), moves out of fields, elements, pointers and package
variables (use `take(&place)`), range loops and `copy` that would duplicate owners, moves of captured
variables inside function literals, `Drop` through a view, owners asserted out of ordinary interfaces,
`x[k]` on a container of owners, owners as variadic arguments or in slice literals, and deferred calls
using owners of inner blocks are compile errors, each explained (codes E3101 to E3112).

### 6.2 Drops (implemented)

Lowering destroys owners where their scope ends, on every path: end of block, `return` (after deferred
calls), `break`, `continue`, `goto`. Assigning to a place holding an owner drops the old value first.
Temporaries are dropped at the end of their statement. A move clears its source variable, which serves as
the drop flag: dropping the zero value does nothing, which every `Drop` must allow (spec section 6.4).
Dropping runs the type's drop glue: its `Drop` method, then its fields in order. `x.Drop()` and
`defer x.Drop()` run the whole glue and clear `x`. Measured on the benchmarks that use library owners
(strings, fasta-lcg), the drops are within run-to-run noise.

The library types that own memory are declared owners: `vec.Vec`, `hash.Map`, `hash.Set`,
`strings.Builder`, `arena.Chunked`, `os.File`, `os.DirEntries`, `exec.Cmd`, `flag.FlagSet`,
`alloc.Restore`.

### 6.3 Not yet implemented

`owned[T]` (the box), owning interfaces and owner unions cannot be dropped yet; declaring a variable of
such a type is a compile error that says so, never a leak. `new(T)` still returns a `*T` and `make` a
slice; the accepted change to `owned[T]` and `vec.Vec[T]` (spec section 4.2) changes many call sites and
go2pu's output, and is the next step.

### 6.4 Shared, synced and weak (proposal, spec section 6.5)

- `shared[T]`: one heap allocation with a header (allocator, strong count, weak count). Copying a
  reference increments the strong count with a plain add; dropping one decrements it, and at zero runs
  `T`'s drop glue; the memory is returned when the weak count is also zero. Not sendable.
- `synced[T]`: the same with `lock xadd`. Sendable. `T`'s fields reached through a `synced[T]` are
  read-only except fields of `pluto/sync` and `pluto/sync/atomic` types (section 7), so that sharing never
  races.
- `weak[P]`: a pointer to the same allocation and a weak count; `Upgrade` increments the strong count if
  it is not zero.
- Count elision: the move checker already knows each variable's last use; a copy that is the source's
  last use is a move and needs no count update (spec rule 4 of section 6.5). The same machinery serves
  the counted strings being implemented separately: their drop glue decrements at the places this work
  drops owners.
- Cycles of strong references are never freed; structures with back pointers use `weak` (examples in
  section 9). A cycle collector is not proposed: it would reintroduce unpredictable pauses.

## 7. Data race freedom (proposal)

Spec section 4.7 already defines **sendable**: values, owners (moved, so the sender loses them),
`synced[T]` and `weak[synced[T]]`, and channels may cross puroutines; `shared[T]` and views of the
sending frame may not. Sendability is inferred structurally from the type, never declared. Three gaps
remain, and I propose closing them as follows:

1. **Views of heap memory** (`*T`, `[]T`) are sendable today and "not checked for races". Proposal
   (decision D5): a view is sendable only if what it points to is immutable for the rest of the program
   (a string literal, a package variable never written after initialization, which the compiler can see)
   or reached through a `synced[T]`. Otherwise share memory by moving an owner or through `synced[T]`.
2. **`synced[T]` contents.** Mutation through a `synced[T]` is allowed only on fields whose type is a
   `pluto/sync` or `pluto/sync/atomic` type, or inside a method whose receiver holds a `sync.Mutex` it
   locks (the compiler checks the lock is held with the same flow analysis as the move checker, the way
   Clang's thread-safety analysis does, with a `//pu:guardedby mu` field annotation). This is Rust's
   `Arc<T>` plus `Mutex<T>`, without lifetimes.
3. **Package variables.** A package variable written after package initialization by code reachable
   from more than one puroutine must have a `pluto/sync/atomic` type or be `//pu:guardedby` a mutex. The
   compiler knows which functions a `go` statement can reach (the same call graph as section 4) and which
   variables they write.

With these, safe code has no data races: every piece of memory is either owned by one puroutine (moved
there), immutable, atomic, or guarded.

## 8. Multi-word tearing

Strings, slices and interfaces are two or three words. A racy read can see the pointer of one value and
the length of another, which breaks spatial safety, not just correctness: a slice with a short array and
a long length reads out of bounds. Go has this hole. With section 7 in place, safe code cannot race, so it
cannot tear; tearing is then confined to unsafe code. The alternative of making every multi-word access
atomic (16-byte `cmpxchg` or a seqlock) costs every slice and string access and is not proposed. Kernel
code that shares multi-word values with interrupt handlers uses `volatile[T]` of word-sized fields, as
spec section 4.9 already says.

## 9. Ergonomics side by side

Each example shows the idiomatic Go, the Rust a careful programmer writes, and the Plutonium these
rules call for. The Plutonium examples use `owned[T]`, `shared[T]` and `alloc.Owned` as specified
(spec sections 5.6 and 6.5); those are not implemented yet (section 6.3).

**Singly linked list with push and pop.**

```go
// Go
type Node struct { val int; next *Node }
func (l *List) Push(v int) { l.head = &Node{v, l.head} }
```

```rust
// Rust
struct Node { val: i32, next: Option<Box<Node>> }
fn push(&mut self, v: i32) { let next = self.head.take(); self.head = Some(Box::new(Node { val: v, next })); }
```

```go
// Plutonium
type Node owner struct { val int; next owned[Node] }
func (l *List) Push(v int) {
	n := alloc.Owned[Node]()
	n.val, n.next = v, take(&l.head)
	l.head = n // moves the box in; the old head moved into n.next
}
```

The list frees itself: dropping `l.head` drops each node's `next` in turn. `take` is the one new idea,
and the compiler suggests it when `n.next = l.head` is written (E3103).

**Tree with parent pointers.**

```go
// Go
type Node struct { parent *Node; kids []*Node }
```

```rust
// Rust
struct Node { parent: Weak<RefCell<Node>>, kids: Vec<Rc<RefCell<Node>>> }
```

```go
// Plutonium, one owner per node: the parent pointer is a plain view of heap memory
type Node owner struct { parent *Node; kids vec.Vec[owned[Node]] }
```

The parent is a view (`*Node`): it never frees anything, and a use after the parent is gone is caught by
a generation check (section 5) rather than prevented at compile time. When nodes must be shared, use
`shared[Node]` children and `weak[shared[Node]]` parents, which is Rust's design without `RefCell`.

**Cache with shared entries.**

```go
// Go
type Cache struct { m map[string]*Entry }
func (c *Cache) Get(k string) *Entry { return c.m[k] }
```

```rust
// Rust
struct Cache { m: HashMap<String, Rc<Entry>> }
fn get(&self, k: &str) -> Option<Rc<Entry>> { self.m.get(k).cloned() }
```

```go
// Plutonium
type Cache owner struct { m hash.Map[string, shared[Entry]] }
func (c *Cache) Get(k string) shared[Entry] { v, _ := c.m.IndexOk(k); return v } // count +1
```

An entry outlives eviction while any caller holds it. A caller that only looks during the call takes a
`*Entry` from `c.m.Ptr(k)` instead and pays nothing.

**Worker pool.**

```go
// Go
for i := 0; i < n; i++ { go worker(jobs, results) }
```

```rust
// Rust
for _ in 0..n { let rx = rx.clone(); let tx = tx.clone(); thread::spawn(move || worker(rx, tx)); }
```

```go
// Plutonium: identical to Go. Channels are sendable; jobs that are owners move through them.
for i := 0; i < n; i++ { go worker(jobs, results) }
```

Under the sendable rule, a job holding a `shared[T]` is rejected at the send ("shared[Stats] can only
be used by one puroutine", spec section 6.9; not implemented yet); the fix is `synced[T]`.

**Device driver with MMIO and DMA.**

```go
// Plutonium
type Regs packed struct { ctrl uint32; status uint32; dma uint64 }
type NIC owner struct {
	regs *volatile[Regs]      // device memory: made from an address, nobody frees it
	ring DMARing              // the driver's own owner type for DMA-able memory, freed on Drop
}
func (n *NIC) Drop() { n.regs.ctrl = 0 } // stop the device before the ring is freed
```

The register block is unsafe to create (`(*volatile[Regs])(unsafe.Pointer(uintptr(base)))`) once, in
the driver's constructor; everything after is safe. Drop order (the `Drop` method, then fields) makes
"stop DMA before freeing its buffer" the natural code. Rust needs the same `unsafe` constructor and a
`Drop` impl; Go has no way to express it.

**Interrupt handler.**

```go
// Plutonium
var rxCount volatile[uint64] // shared with the handler on this processor

//pu:interrupt
func onRx(f *x86.InterruptFrame) { rxCount.Store(rxCount.Load() + 1); nic.Ack() }
```

The handler may not block, allocate or take a lock (checked: spec section 12.6), and data it shares
with interrupted code is `volatile` word-sized or atomic, so it cannot tear (section 8).

## 10. Costs, and which checks can be turned off

| Check | When | Cost | Off switch |
|---|---|---|---|
| Bounds | run time | a compare and branch per unproven index | `//pu:nobounds` (unsafe) |
| Escape (frame views) | compile time | none at run time | none |
| Moves | compile time | none | none |
| Drops | run time, where the program would free anyway | a call per owner per scope exit; within noise on the benchmarks | none (it is the program's own `Drop`) |
| Move clearing (drop flags) | run time | a store per move, removed when dead | none |
| Generation checks | run time, prototype | 1.0x to 1.8x, about 1.05x geometric mean in cycles | build flag (section 5) |
| Reference counts | run time, proposal | an add per copy, a sub and test per drop; atomic for `synced` | none; choose `shared` or a view |

I propose `//pu:nochecks` on a function to remove bounds and generation checks together, counting as
unsafe code (section 2), and no switch for the compile-time checks, which cost nothing at run time
(decision D6).

## 11. Known holes

1. Views of heap memory are checked only by the opt-in generation checks (section 5); without them a
   use after free through a `*T` or `[]T` into freed heap memory is undetected, as in C.
2. Arena allocations are never generation checked; a view into an arena after `Reset` is undetected.
3. An owner allocated from a local arena installed with `alloc.Use` and returned from the function
   refers to an allocator that no longer exists (spec section 7.1).
4. Data races: section 7 is a proposal. Today heap views may be shared between puroutines, and package
   variables may be written from several.
5. Writes through memory reinterpreted with `unsafe.Pointer` bypass the heap-only field inference
   (section 4): unsafe code that stores a frame pointer that way is not caught.
6. A function value or interface method called from outside Plutonium (passed to C) is assumed to be
   called only by Plutonium code when inferring heap-only fields.
7. `owned`, `shared`, `synced`, `weak`, owning interfaces and owner unions cannot be dropped yet (a
   compile error, not a leak). Values in memory from `alloc.New` are never dropped by the compiler.
8. The escape analysis is conservative: programs that layer allocators through interfaces in unusual
   ways, or that keep frame pointers in heap memory freed before the frame ends, are rejected. The
   error names the path; the fix is usually to allocate or to make a field heap-only.
9. Generation tags occupy pointer bits 63..48, which conflicts with 5-level paging and with hardware
   pointer tagging (LAM, TBI); a kernel build would need another encoding.

## 12. Decisions for sign-off

| # | Decision | Options | Recommendation |
|---|---|---|---|
| D1 | How unsafe code is marked | (a) as now: imports and directives, plus `pluto vet -unsafe` to list it; (b) an `unsafe` block or function keyword | (a): it is Go's model, and the audit list gives what a keyword would |
| D2 | Drop flags | (a) clear the moved-from variable (implemented); (b) separate flag bits per variable, as Rust | (a): simpler, and the optimizer removes dead clears; requires `Drop` to accept the zero value, which the spec already demands |
| D3 | Generation checks by default | (a) opt-in flag; (b) default for hosted builds; (c) default everywhere | (a) now, (b) after loop hoisting and a second measurement |
| D4 | Arena views | (a) unchecked (prototype); (b) per-arena epoch in the tag | (b), with the same check sequence |
| D5 | Heap views across puroutines | (a) allowed, unchecked (current spec); (b) only immutable or through `synced` | (b), it closes the last race and tearing hole in safe code |
| D6 | Turning checks off | (a) `//pu:nobounds` only; (b) add `//pu:nochecks` for bounds and generations | (b), counted as unsafe |
| D7 | Guarded data | (a) only atomics in `synced[T]`; (b) atomics plus `//pu:guardedby mu` checked by flow analysis | (b) |
| D8 | Heap-only field inference | (a) keep (implemented); (b) require an annotation instead | (a): it needs no annotation and is what makes arenas usable |
| D9 | `string` | counted, immutable value (approved; implemented on another branch) | as approved: `unsafe.String` stays a view |
