# Memory Lifecycle and Allocator Architecture — Design

## Overview

Plutonium manages memory without a garbage collector. The compiler
automatically inserts allocation and deallocation code using a hybrid
strategy: static RAII for the common case, automatic reference counting
when pointers enter dynamic containers. A swappable slab allocator
provides efficient sized-object allocation for both userspace and kernel
code. The programmer never writes `Free()`.

## Core principle

**Allocations are implicit. Deallocation is automatic. The programmer
writes code like Go; the compiler generates code like C.**

## 1. Lifetime model

### Stack values (zero cost)

Variables that don't escape the function live on the stack. No heap
allocation, no tracking, no cleanup cost.

```
func process() {
    x := int64(42)          // stack
    buf := [256]byte{}      // stack
}   // gone — stack frame reclaimed on return
```

### Heap values with RAII (zero cost beyond alloc/free)

When the compiler knows a single variable owns a heap allocation, it
inserts a free at the owning scope's exit. No reference count, no
atomic operations.

```
func handle() {
    conn := alloc.New[Connection]()
    conn.fd = accept(listenFd)
    // ... use conn ...
}   // compiler inserts: conn.Drop() then Dealloc(conn)
```

Ownership transfers on return:

```
func makeConn() *Connection {
    conn := alloc.New[Connection]()
    conn.fd = accept(listenFd)
    return conn     // ownership transfers to caller — no free here
}

func caller() {
    c := makeConn()     // caller now owns c
    // ...
}   // compiler frees c here
```

### Escape analysis (automatic heap promotion)

If a local variable's address escapes the function (returned, stored
in a struct field), the compiler promotes it from stack to heap
automatically. The caller owns the allocation.

```
func makeValue() *int64 {
    x := int64(42)      // compiler sees &x escapes via return
    return &x            // x heap-allocated, caller owns it
}

func user() {
    ptr := makeValue()   // owns the allocation
    fmt.Printf("%d\n", *ptr)
}   // freed here
```

This matches Go's escape analysis behavior. The programmer doesn't
think about stack vs heap — the compiler decides.

### Dynamic containers (automatic reference counting)

When a pointer is stored in a slice, map, or any dynamically-sized
container, the compiler can no longer prove single ownership at compile
time. It automatically wraps the pointer with a reference count.

```
func buildList() {
    var nodes []*Node
    for i := 0; i < 100; i++ {
        n := alloc.New[Node]()
        nodes = append(nodes, n)    // enters slice → refcounted
    }
    root := nodes[0]                // refcount incremented (now 2)
}   // nodes dies → all refcounts decremented
    // root dies → refcount hits 0 → Drop() + Dealloc()
```

The programmer never sees the reference count. There's no `Rc<T>` or
`shared_ptr<T>` wrapper — the compiler makes the decision.

### Compiler strategy selection

| Situation | Strategy | Runtime cost |
|-----------|----------|-------------|
| Local variable, address never taken | Stack | Zero |
| Local variable, address escapes | Heap + RAII | One alloc + one free |
| Pointer stored in struct field | Struct owns it, RAII | Zero extra |
| Pointer enters slice/dynamic container | Refcounted | Atomic inc/dec per copy |
| Single-reference refcounted pointer | Compiler elides atomics | Zero |

### What is NOT enforced

Plutonium does not have Rust-style borrow checking. There are no
immutability rules, no exclusivity constraints on mutable references.
Multiple pointers to the same data can coexist and any of them can
read or write.

The compiler catches:
- Returning a pointer to a stack variable that wasn't promoted (bug)
- Use-after-move (pointer used after ownership was transferred)

The compiler does NOT catch:
- Data races (no concurrent runtime, so not a language concern)
- Iterator invalidation (same as C, Go, C++, Zig)
- Dangling pointers from storing borrowed pointers past owner's lifetime

This is the same safety level as Go (minus the GC safety net) and
better than C (automatic deallocation prevents most leaks and
use-after-free).

## 2. Drop method (custom cleanup)

If a type defines a `Drop()` method, the compiler calls it before
freeing the allocation. This handles resource cleanup: closing file
descriptors, releasing locks, unmapping pages, etc.

```
type FileHandle struct {
    fd int64
}

func (f *FileHandle) Drop() {
    syscall.Close(f.fd)
}

func readConfig() {
    f := alloc.New[FileHandle]()
    f.fd = syscall.Open("/etc/config", 0)
    // ... read from f.fd ...
}   // compiler calls f.Drop() → closes fd, then frees f
```

Rules:
- `Drop()` is called exactly once, when the last owner releases.
- For RAII: called at scope exit of the owning variable.
- For refcounted: called when refcount hits zero.
- `Drop()` is called BEFORE the memory is freed.
- `Drop()` must not panic (Plutonium has no panic, so this is
  enforced by the language).
- `Drop()` on a struct recursively drops pointer fields that are
  owned by the struct.

`defer` still exists for ad-hoc scope-bound cleanup (releasing a
mutex, restoring a flag). `Drop` is for type-bound resource ownership
that follows the value, not the scope.

## 3. Allocator architecture

Three layers, bottom to top:

```
┌─────────────────────────────────────────┐
│  User code                              │
│  alloc.New[T]()  ←  compiler-generated  │
│  RAII / refcount    Drop() calls        │
├─────────────────────────────────────────┤
│  Allocator interface                    │
│  Default: slab allocator                │
│  Swappable via alloc.SetAllocator()     │
├─────────────────────────────────────────┤
│  PageProvider interface                 │
│  Default: mmap/munmap on Linux          │
│  Swappable via alloc.SetPageProvider()  │
└─────────────────────────────────────────┘
```

### PageProvider — raw page source

```
type PageProvider interface {
    AllocPages(n int64) *byte
    FreePages(ptr *byte, n int64)
}
```

The PageProvider gives the allocator contiguous pages of memory.

- **Userspace default**: wraps mmap(MAP_ANONYMOUS) / munmap.
- **Kernel**: implement with your physical page frame allocator.
  Call `alloc.SetPageProvider(myPFA)` during early boot.

Page size is assumed to be 4096 bytes. The `n` parameter is a page
count, not byte count. `AllocPages(1)` returns 4096 bytes.

### Allocator — sized object allocation

```
type Allocator interface {
    Alloc(size uintptr, align uintptr) *byte
    AllocZeroed(size uintptr, align uintptr) *byte
    Dealloc(ptr *byte, size uintptr, align uintptr)
}
```

The compiler-generated RAII and refcount code calls through this
interface. `alloc.New[T]()` calls `AllocZeroed(Sizeof[T], Alignof[T])`.

- `Alloc`: returns uninitialized memory.
- `AllocZeroed`: returns zeroed memory. Allocators may implement this
  more efficiently than alloc+memset (e.g., mmap returns zeroed pages
  for free).
- `Dealloc`: releases memory. Receives size and alignment so the
  allocator can find the right slab/pool.

### Default slab allocator

The shipped implementation uses power-of-2 size-class slabs:

```
Size classes: 8, 16, 32, 64, 128, 256, 512, 1024, 2048
```

Each size class maintains a free list of slots carved from pages
obtained via the PageProvider.

**Allocation**:
1. Round requested size up to the next size class.
2. Pop a slot from that class's free list.
3. If the free list is empty, request a page from the PageProvider
   and carve it into slots (e.g., one 4096-byte page → 512 slots
   of 8 bytes for the 8-byte class).

**Deallocation**:
1. Determine the size class from the provided size.
2. Push the slot back onto the free list.
3. If all slots in a page are free, optionally return the page to
   the PageProvider (lazy reclamation — not required).

**Large allocations** (> 2048 bytes): go directly to the PageProvider.
The slab allocator rounds up to a page boundary and delegates.

**Thread safety**: the v0.1 slab allocator is single-threaded. For
multi-threaded userspace, a future version would use per-thread caches
(like TCMalloc/jemalloc). For kernel SMP, per-CPU slabs.

### Swapping allocators

```
// Replace just the page source (kernel bootstrap)
alloc.SetPageProvider(myPageFrameAllocator)

// Replace the entire allocator (custom memory management)
alloc.SetAllocator(myAllocator)
```

After calling `SetPageProvider`, the default slab allocator uses the
new page source. Existing allocations from the old source remain valid
until freed.

After calling `SetAllocator`, all new allocations go through the new
allocator. The compiler-generated RAII/refcount code calls through
whatever allocator is current.

### Kernel bootstrap sequence

```
// Early kernel init — before any heap allocation
func kernelMain() {
    // Step 1: Initialize page frame allocator from bootloader memory map
    pfa := initPageFrameAllocator(bootInfo.memoryMap)
    
    // Step 2: Tell the runtime where to get pages
    alloc.SetPageProvider(pfa)
    
    // Step 3: Everything works. alloc.New[T]() is now available.
    // The slab allocator initializes lazily on first allocation.
    gdt := alloc.New[GDTTable]()
    idt := alloc.New[IDTTable]()
    proc := alloc.New[ProcessDescriptor]()
    // ...
}   // gdt, idt, proc freed here — but in a kernel, main never returns
```

### Userspace — zero configuration

```
func main() int64 {
    // No setup needed. Default PageProvider uses mmap.
    // Default Allocator is the slab allocator.
    x := alloc.New[int64]()
    *x = 42
    fmt.Printf("value=%d\n", *x)
}   // freed automatically
```

## 4. How it all fits together

A complete example showing escape analysis, RAII, refcounting, Drop,
and the allocator working together:

```
type Connection struct {
    fd     int64
    buffer *[4096]byte
}

func (c *Connection) Drop() {
    syscall.Close(c.fd)
    // buffer is a pointer field owned by Connection —
    // compiler frees it after Drop returns
}

func accept(listenFd int64) *Connection {
    c := alloc.New[Connection]()                // slab: 16-byte class
    c.fd = syscall.Accept(listenFd)
    c.buffer = alloc.New[[4096]byte]()          // slab: direct page
    return c                                     // ownership to caller
}

func serve(listenFd int64) {
    var connections []*Connection

    for {
        conn := accept(listenFd)                 // caller owns conn
        connections = append(connections, conn)   // enters slice → refcounted
        
        if tooManyConnections(connections) {
            old := connections[0]
            connections = connections[1:]         // old's refcount decremented
            // old still alive if held elsewhere; freed when last ref dies
        }
    }
}   // connections dies → all refcounts decremented → Drop + Dealloc for each
```

## 5. Implementation phases

This design requires significant compiler and runtime work:

### Phase A: Slab allocator (runtime, no compiler changes)

Replace the current one-mmap-per-allocation with a real slab allocator
in `pluto/alloc/`. Wire up the PageProvider interface and slab free
lists. This is pure Plutonium + assembly code, no compiler changes.

### Phase B: Escape analysis (compiler)

Add an escape analysis pass after type checking. Walk the AST to
determine which local variables have their address taken and whether
that address escapes the function. Mark escaping variables for heap
promotion. The lowerer uses this information to emit `alloc.New`
instead of stack alloca for escaping variables.

### Phase C: RAII insertion (compiler)

Add a pass that inserts `Dealloc` calls (and `Drop` calls if the type
has a `Drop` method) at scope exits for heap-allocated variables.
Handle all exit paths: normal return, early return, break/continue
past the owning scope.

### Phase D: Automatic reference counting (compiler)

When the escape analysis determines that a pointer enters a dynamic
container (slice append, map insert), wrap it with a refcount. The
refcount is a hidden field prepended to the allocation (the pointer
the user sees is offset by 8 bytes from the real allocation start).
Increment on copy into a container. Decrement on scope exit or
container element removal. Call Drop + Dealloc on zero.

### Phase E: Optimizer elision

Add compiler optimizations to reduce refcount overhead:
- Elide inc/dec pairs that cancel (copy then immediate drop)
- Use non-atomic operations when the compiler proves single-thread
  access
- Move decrements to the latest possible point (reduce refcount
  traffic)

## 6. Open questions for future design

- **Weak references**: needed to break reference cycles in graph
  structures. Likely `weak[*T]` type wrapper that doesn't prevent
  deallocation but can be checked for validity.
- **Arenas**: bulk allocation/deallocation for request-scoped work.
  `var a alloc.Arena; defer a.Reset()`. All allocations from the arena
  are freed at once. Complementary to per-object RAII.
- **Per-thread/per-CPU allocator caches**: needed for multi-threaded
  userspace and SMP kernels. The slab allocator would maintain per-CPU
  free lists with occasional balancing.
- **Ownership transfer syntax**: should there be an explicit `move`
  keyword, or is the compiler smart enough to infer moves from context?
  Current design: compiler infers, no syntax needed.
