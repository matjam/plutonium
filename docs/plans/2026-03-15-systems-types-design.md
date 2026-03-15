# Systems Programming Types — Design

## Overview

Six additions to Plutonium for low-level systems programming: bit-level
access, volatile I/O, alignment control, packed data layout, lock-free ring
buffers, and explicit typed allocation. All designed around Plutonium's
constraints: no GC, no implicit heap allocation, compile-time safety.

## 1. `packed` types with `bits[T, N]` fields

### Problem

Hardware registers, protocol headers, and on-disk formats require exact
bit-level layout. Go has no bitfields. C has bitfields but their layout is
implementation-defined.

### Design

A `packed` type declaration specifies the backing storage up front. Fields
use `bits[T, N]` to declare N-bit fields with access type T.

**Three backing forms:**

```
type R packed uint32 { ... }      // exactly 32 bits, single register
type R packed [8]byte { ... }     // exactly 8 bytes, N must be power-of-2
type R packed []byte  { ... }     // auto-sized: compiler picks smallest [N]byte
```

**Field types:**

```
type ControlReg packed uint32 {
    mode     bits[uint, 4]        // 4-bit unsigned integer
    flags    bits {               // named 1-bit boolean flags
        CLK_ENABLE
        SMOKE
        IRQ_PENDING
        DMA_ACTIVE
    }
    priority bits[uint, 2]        // 2-bit unsigned integer
    // remaining bits are implicit padding
}
```

- `bits[T, N]` — N-bit field, read/write as type T. T must be wide enough
  to hold N bits. Compiler rejects `bits[uint8, 12]`.
- `bits { names... }` — sugar for a group of `bits[bool, 1]` fields with
  named access. Each flag is a bool.
- `_` (blank identifier) — explicit reserved/padding bits.

**Semantics:**

- Fields are laid out LSB-first in declaration order. No implementation-defined gaps.
- Reads zero-extend unsigned fields, sign-extend signed fields.
- Writes always mask to N bits. Overflow wraps silently at 2^N. This matches
  hardware register semantics and costs a single AND instruction per write.
  For full-width fields (e.g., `bits[uint64, 64]`), the mask is a no-op
  and the compiler elides it.
- Compound assignment (`+=`, `|=`, etc.) is read-mask-op-mask-write.

**Access:**

```
var reg ControlReg
reg.mode = 5                    // mask-and-shift into bits [0:3]
reg.flags.CLK_ENABLE = true     // set bit 4
m := reg.mode                   // m is uint, zero-extended
if reg.flags.IRQ_PENDING { ... }
```

**Codegen:**

For integer-backed types (`packed uint32`), the whole value lives in a single
register. Each field access compiles to one AND/OR/shift sequence. For
array-backed types (`packed [N]byte`), the compiler generates multi-byte
mask/shift sequences for fields that span byte boundaries.

### Endianness

Packed types default to native endianness (the target CPU's byte order).
An explicit `be` (big-endian) or `le` (little-endian) annotation overrides
the byte order:

```
type ControlReg packed uint32 { ... }        // native endian (default)
type TCPHeader packed be { ... }             // big-endian (network order)
type PCICmd packed le uint16 { ... }         // little-endian
type DiskBlock packed le [512]byte { ... }   // little-endian, 512-byte block
```

The annotation goes after `packed` (before the backing type or `[]T`).

**Codegen**: When the annotation doesn't match native endianness, the
compiler inserts `BSWAP` on load/store of the backing value. For
integer-backed types this is a single instruction. For byte-backed types,
each multi-byte field is byte-swapped individually on access.

**Bit-level fields**: `bits[T, N]` numbering is always LSB-first within
the logical (post-swap) integer value. The byte swap happens at the
memory boundary, not the register level. This matches hardware datasheets
which define bit positions in logical register order.

**Cost**: Zero overhead when the annotation matches native endianness
(the common case for hardware registers). One `BSWAP` per access when
it doesn't match (the network protocol case). The compiler can elide
redundant swaps when multiple fields are accessed from the same load.

### Misaligned access codegen

Packed structs produce misaligned field accesses. The codegen must never
emit a faulting instruction regardless of target architecture:

- **x86_64**: Misaligned MOV works in hardware (small throughput penalty
  on cache line splits). Single MOV instruction, no decomposition needed.
- **Strict-alignment architectures** (some ARM modes, RISC-V, MIPS):
  Misaligned word loads/stores fault. The codegen must decompose into
  byte-level loads with shift/OR to reconstruct the value, and byte-level
  stores with shift/mask to write it. For example, a `uint32` load at
  offset 3 on a strict-alignment target becomes 4 byte loads + 3 shifts
  + 3 ORs.
- **Configurable-alignment architectures** (ARM with UAL bit): The
  compiler can emit a single LDR/STR if the runtime alignment mode is
  known. Otherwise, it must use the safe byte-level decomposition.

The backend selects the strategy based on the target triple. This is a
code quality concern, not a correctness concern — the output is always
correct, just potentially slower on strict-alignment targets.

### Packing rules

Packed types have **zero implicit padding**. Fields are laid out
contiguously in declaration order with no gaps.

**Byte-level fields** (standard types like `uint32`, `uint8`) occupy their
natural size but are NOT padded to their natural alignment. A `uint32`
after a `uint8` starts at the very next byte, not the next 4-byte boundary:

```
type Header packed {
    version uint8     // 1 byte at offset 0
    flags   uint8     // 1 byte at offset 1
    length  uint32    // 4 bytes at offset 2 (misaligned — intentional)
}
// sizeof(Header) = 6 bytes

type Packet packed {
    hdr     Header    // 6 bytes at offset 0
    seq     uint32    // 4 bytes at offset 6 (misaligned — intentional)
}
// sizeof(Packet) = 10 bytes
```

**Bit-level fields** (`bits[T, N]` and `bits { ... }`) occupy exactly N
bits. Byte-level and bit-level fields can be mixed in the same packed type.

**Explicit alignment** via `aligned[T, N]` forces padding before a field
to reach N-byte alignment:

```
type AlignedPacket packed {
    hdr     Header              // 6 bytes at offset 0
    seq     aligned[uint32, 4]  // 4 bytes at offset 8 (2 bytes padding inserted)
}
// sizeof(AlignedPacket) = 12 bytes
```

**Embedded packed structs** inherit tight packing. Using `aligned` on an
embedded struct aligns the struct's starting position but does NOT add
alignment to fields within it — those remain packed. This is likely a
mistake and should produce a lint warning:

```
type Outer packed {
    x uint8
    y aligned[Header, 4]  // ⚠ lint: aligned struct inside packed —
}                          //   aligns Header's start but fields within
                           //   Header are still tightly packed. If you
                           //   want aligned fields, align them individually.
```

**Backing storage padding**: for `packed uint32 { ... }`, unused bits at
the end are implicit padding to fill the backing type. For `packed { ... }`
without a backing type, the struct is exactly as large as its fields with
no trailing padding.

## 2. `bits[V]` — bit-addressing view

### Problem

Sometimes you need raw bit-level access to an existing variable without
defining a packed struct schema. Testing individual bits, counting set bits,
finding the first set bit — these are common operations on flags, bitmaps,
and capability masks.

### Design

`bits[V]` creates a zero-cost lens over a variable's storage. It doesn't
own memory — it aliases the underlying variable. The number of addressable
bits is `sizeof(typeof(V)) * 8`.

```
var flags uint64
var b bits[flags]

b.Set(3)                  // flags |= (1 << 3)
b.Clear(3)                // flags &= ^(1 << 3)
ok := b.Test(3)           // (flags >> 3) & 1 != 0
b.Toggle(3)               // flags ^= (1 << 3)

val := b.Extract(4, 8)   // extract 8 bits starting at bit 4
b.Insert(4, 8, 0xAB)     // insert 0xAB into bits [4:11]

n := b.Len()              // number of bits (64 for uint64)
n := b.PopCount()         // count set bits → POPCNT
i := b.FirstSet()         // lowest set bit → BSF/TZCNT
i := b.FirstClear()       // lowest clear bit
```

**With a packed type as schema:**

```
var raw uint32
var cr bits[raw] as ControlReg

cr.mode = 5               // writes to raw's bits [0:3]
cr.flags.CLK_ENABLE = true // sets bit 4 of raw
```

`bits[raw] as ControlReg` means: view `raw`'s storage through the packed
layout of `ControlReg`. The total bit width of ControlReg must be <=
`sizeof(raw) * 8`.

**Codegen:**

`bits[V]` compiles to nothing — it's the same address as the underlying
variable. Methods compile to 1-3 x86 instructions: BT/BTS/BTR for
test/set/clear, BEXTR for extract, POPCNT for popcount, BSF/TZCNT for
first-set.

## 3. `volatile[T]`

### Problem

MMIO registers must not have their accesses reordered, coalesced, or elided
by the compiler. Go has no volatile qualifier. Using `atomic` operations is
semantically wrong (volatile != atomic).

### Design

`volatile[T]` is a type wrapper. Every read and write through a `volatile[T]`
variable is guaranteed to produce exactly one load/store instruction, in
program order, with no coalescing or elision.

```
type UARTRegs struct {
    data   volatile[uint8]
    status volatile[uint8]
    ctrl   volatile[uint8]
}

func readByte(uart *UARTRegs) uint8 {
    for uart.status & 0x01 == 0 {  // not optimized away
        // spin
    }
    return uart.data               // exactly one load
}

func writeByte(uart *UARTRegs, b uint8) {
    for uart.status & 0x02 == 0 {
        // spin
    }
    uart.data = b                  // exactly one store
}
```

**Constraints:**

- `T` must be a scalar type (integers, bool, pointer). No structs, arrays,
  or slices.
- Reads return `T`. Writes accept `T`. No implicit conversions.
- The compiler inserts a memory barrier (MFENCE or equivalent) around
  volatile accesses if needed for the target architecture. On x86, regular
  MOV to/from memory is sufficient for MMIO (x86 doesn't reorder
  loads/stores to the same address).
- Volatile accesses are never moved across function calls, loops, or
  branches by any future optimizer.

**Codegen:**

The compiler emits the access as a plain MOV but marks it as non-eliminable
and non-reorderable in the instruction stream. No intrinsic call overhead.

## 4. `aligned[T, N]`

### Problem

DMA buffers must be page-aligned. SIMD operands need 16/32-byte alignment.
Hardware descriptor rings need specific alignment. Go provides no alignment
guarantees beyond natural alignment.

### Design

`aligned[T, N]` is a type wrapper that guarantees the value is N-byte
aligned in memory. N must be a power of 2.

```
var pageTable aligned[[4096]byte, 4096]   // page-aligned page table
var dmaBuffer aligned[[256]byte, 256]     // 256-byte aligned DMA buffer
var simdVec   aligned[[4]float32, 16]     // 16-byte aligned for SSE

type IDTEntry aligned[packed uint64 {
    offsetLow  bits[uint, 16]
    selector   bits[uint, 16]
    ist        bits[uint, 3]
    _          bits[uint, 5]
    gateType   bits[uint, 4]
    _          bits[uint, 1]
    dpl        bits[uint, 2]
    present    bits[bool, 1]
    offsetHigh bits[uint, 48]
}, 8]  // 8-byte aligned IDT entry
```

**Semantics:**

- Stack allocation: the compiler emits `AND RSP, -N` to align the stack
  before the alloca.
- Global/static allocation: the compiler emits `.align N` in the assembly
  or sets the section alignment.
- Struct embedding: an `aligned` field forces the containing struct to have
  at least N-byte alignment.
- `unsafe.As[aligned[T, N]](ptr)` — the compiler can optionally verify
  that `ptr` is N-byte aligned at runtime (debug mode) or trust the
  programmer (release mode).
- `alloc.New[aligned[T, N]]()` — the allocator is told to provide N-byte
  aligned memory.

**Codegen:**

Alignment is a layout constraint, not a runtime operation. The only code
generated is the stack alignment adjustment when needed. Everything else is
handled by the assembler/linker.

## 5. `ring[T, N]`

### Problem

Lock-free ring buffers are the workhorse of kernel IPC, network packet
queues, trace buffers, and interrupt-to-process communication. Every kernel
reimplements them. Getting the wrapping arithmetic wrong is a common source
of bugs.

### Design

`ring[T, N]` is a built-in type. N must be a power of 2 (enforced at
compile time). Index wrapping uses AND masking instead of modulo.

**Memory model:**

- Local variable: lives on the stack (suitable for small N).
- Package-level variable or struct field: lives in static/BSS memory.
- The compiler can warn or error if a stack-allocated ring exceeds a
  configurable threshold (default 4KB).

**Layout:**

```
ring[T, N] = {
    head uint64       // write index (monotonically increasing)
    tail uint64       // read index (monotonically increasing)
    data [N]T         // backing array
}
// Total size: 16 + N * sizeof(T) bytes
```

Indexes are monotonically increasing uint64 values. The actual array index
is computed as `head & (N - 1)`. This avoids the full/empty ambiguity of
wrapped indexes — the ring is full when `head - tail == N`, empty when
`head == tail`.

**API:**

```
var r ring[int64, 256]

ok := r.Push(42)            // returns false if full
val, ok := r.Pop()          // returns (zero, false) if empty
val := r.Peek()             // read head without consuming
n := r.Len()                // head - tail (current count)
c := r.Cap()                // always N
full := r.Full()            // head - tail == N
empty := r.Empty()          // head == tail
r.Reset()                   // set head = tail = 0
```

**Overflow semantics:**

`Push` returns `false` when full. No data is lost and no blocking occurs.
The caller decides the policy (drop, backpressure, overwrite-oldest via
Pop+Push, etc.).

**Codegen:**

The compiler inlines all ring operations. `Push` compiles to roughly:
```
if head - tail == N { return false }
data[head & (N-1)] = val
head++
return true
```

The AND masking is a single instruction. No function call overhead, no
branching for modulo.

**Concurrency note:**

The base `ring[T, N]` is not thread-safe. For SPSC lock-free usage, the
user can combine with `volatile`:
```
type SPSCRing struct {
    head volatile[uint64]
    tail volatile[uint64]
    data [256]int64
}
```

A future `atomic[T]` type would enable MPSC/MPMC patterns. This is
intentionally left for the ownership/lifetime design phase.

## 6. Allocation primitives

### Problem

Plutonium forbids implicit heap allocation, but systems code needs explicit,
user-controlled allocation — both for building kernel allocators and for
userspace programs that need heap objects.

### Design

Two layers: unsafe primitives for building allocators, and a safe stdlib
package for using them.

**Layer 1: `unsafe` package primitives**

```
unsafe.As[T](p unsafe.Pointer) *T       // cast raw memory → typed pointer
unsafe.From[T](p *T) unsafe.Pointer     // cast typed pointer → raw memory
unsafe.FromUintptr(n uintptr) unsafe.Pointer
unsafe.ToUintptr(p unsafe.Pointer) uintptr
unsafe.Sizeof[T]() uintptr              // compile-time size of T
unsafe.Alignof[T]() uintptr             // compile-time alignment of T
```

`unsafe.As[T]` is the only way to create a typed pointer from raw memory.
It lives in the `unsafe` package, making every use auditable by grepping
for `"unsafe"` in import declarations.

**Layer 2: `pluto/alloc` package (userspace)**

```
import "pluto/alloc"

// Default allocator (mmap-backed on Linux)
ptr := alloc.New[MyStruct]()         // returns *MyStruct, zeroed
alloc.Free(ptr)

// Custom allocator
type Allocator interface {
    Alloc(size uintptr, align uintptr) unsafe.Pointer
    Dealloc(ptr unsafe.Pointer, size uintptr)
}

// Use a custom allocator
ptr := alloc.NewWith[MyStruct](pool)
alloc.FreeWith(pool, ptr)

// Swap the global default
alloc.SetDefault(myAllocator)
```

**Kernel usage:**

In kernel mode, there is no default allocator. The programmer builds one
from scratch using `unsafe` primitives and raw memory from the bootloader
or hardware memory map:

```
import "unsafe"

func kmalloc(size uintptr) unsafe.Pointer {
    // your allocator implementation here
    page := pageFrameAlloc()
    return unsafe.FromUintptr(page)
}

func newProcess() *ProcessDescriptor {
    raw := kmalloc(unsafe.Sizeof[ProcessDescriptor]())
    return unsafe.As[ProcessDescriptor](raw)
}
```

**Integration with `aligned[T, N]`:**

`alloc.New[aligned[T, N]]()` automatically requests N-byte aligned memory
from the allocator. The `Allocator.Alloc` method receives both size and
alignment, allowing the allocator to satisfy the constraint.

## Implementation order

These features have dependencies on compiler infrastructure that doesn't
fully exist yet (struct field access, method lowering, const generics).
Suggested implementation order based on prerequisite depth:

1. **`packed` + `bits[T, N]`** — new type kind, field layout computation,
   mask/shift codegen. No method calls needed (field access is compiler-
   generated). Foundation for everything else.

2. **`volatile[T]`** — type wrapper with codegen annotation. Small surface
   area. Requires the compiler to mark loads/stores as non-eliminable.

3. **`aligned[T, N]`** — layout constraint, stack/section alignment.
   Requires const generic parameter (N). Mostly affects alloca codegen
   and linker section attributes.

4. **`unsafe` allocation primitives** — `unsafe.As[T]`, `unsafe.Sizeof[T]`,
   etc. Requires generic builtins. Foundation for `alloc` package.

5. **`alloc` package** — stdlib package built on `unsafe` primitives.
   Requires import resolution, interface dispatch (for `Allocator`
   interface).

6. **`ring[T, N]`** — built-in type with compiler-generated methods.
   Requires const generics (N) and method lowering. Most complex.

7. **`bits[V]` lens** — bit-level view over a variable. Requires alias
   analysis (the view must track the underlying variable). Can be
   implemented after packed types prove out the bit manipulation codegen.

Prerequisites that must be finished first:
- Struct field access by index (currently hardcoded to field 0)
- Method declaration and call lowering
- Const generic parameters (for N in ring, aligned, bits)
