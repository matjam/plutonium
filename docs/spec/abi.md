# Plutonium x86-64 ABI

**Version:** 0.2 (draft)
**Date:** 2026-10-10

This document describes how Plutonium code is laid out and called on x86-64. The language
specification ([language.md](language.md) and its chapters) is the source of truth: this document refines
it for the x86-64 binary interface, as section 21.6 (Layouts and calling conventions) of the
specification requires, and where the two disagree the specification wins. Sections cited as "language
spec section N.M" are sections of the specification. The compiler implements this interface in
`internal/lower` (signature lowering, data layout, symbols) and `internal/arch/amd64` (registers, frames,
encoding). Sections marked **planned** or **proposed** describe parts of the interface that are not
fixed yet.

There are two calling conventions:

- the **internal ABI**, used for every call between Plutonium functions, including calls through func
  values and interface method tables;
- the **C ABI** (System V AMD64), for `//pu:extern` and `//pu:export` functions.

The internal ABI is a superset of System V for signatures made only of scalars, so most `//pu:extern`
declarations need no translation. Where the two differ, the table in
[section 9](#9-compatibility-with-system-v) says so, and section 9.1 gives the classification lowering
applies to C calls and entry points.

## Contents

1. [Data layout](#1-data-layout)
2. [Registers](#2-registers)
3. [Internal calling convention](#3-internal-calling-convention)
4. [Func values and closures](#4-func-values-and-closures)
5. [Interfaces](#5-interfaces)
6. [Stack frames](#6-stack-frames)
7. [Code model and relocations](#7-code-model-and-relocations)
8. [Symbols and sections](#8-symbols-and-sections)
9. [Compatibility with System V](#9-compatibility-with-system-v)
10. [Kernel mode](#10-kernel-mode)
11. [Linux system calls](#11-linux-system-calls)
12. [Tasks and the scheduler](#12-tasks-and-the-scheduler)

## 1. Data layout

Sizes are in bytes. `int`, `uint`, `uintptr` and pointers are 8 bytes, and the machine word is 8 bytes. The
alignment of a scalar is its size. The layout of an aggregate follows from the layout of its parts by the
rules that come after the table. The sizes and alignments are those that language spec section 20.2 (Size
and alignment guarantees) guarantees; this section adds the order of the words and the field offsets.

| Type | Size | Align | Layout |
|---|---|---|---|
| `bool`, `int8`, `uint8` | 1 | 1 | |
| `int16`, `uint16` | 2 | 2 | |
| `int32`, `uint32`, `float32`, `rune` | 4 | 4 | |
| `int64`, `uint64`, `int`, `uint`, `uintptr`, `float64` | 8 | 8 | |
| `*T`, `unsafe.Pointer`, `owned[T]`, `shared[T]`, `synced[T]`, `weak[P]` | 8 | 8 | address |
| func value | 8 | 8 | address of a closure record (section 4) |
| `string` | 24 | 8 | `{ptr *byte, len int, ctl uintptr}` (section 1.1) |
| `[]T` | 24 | 8 | `{ptr *T, len int, cap int}` |
| interface | 16 | 8 | `{tab, data}` (section 5) |
| `[N]T` | N × size(T) | align(T) | |
| struct | the end of the last field, rounded up to the struct's alignment (below) | the largest field alignment, at least 1 | fields in declaration order, each at the next multiple of its alignment (below) |
| `volatile[T]` | size(T) | align(T) | as `T` |
| `aligned[T, N]` | size(T) rounded up to N | max(N, align(T)) | as `T` |
| `ring[T, N]` | 16 + N × size(T), rounded up to the alignment | max(8, align(T)) | `{head, tail uint64; buf [N]T}` |
| packed type | its backing, or its field width rounded up to bytes | backing's; 1 when the backing is `[]byte`, a byte array or omitted | bit-exact, language spec section 7.14 |
| enum type | its underlying integer's | its underlying integer's | the member's value, language spec section 7.12 |
| union | tag, then the largest payload at its alignment, rounded up to the alignment | max(tag size, largest payload alignment) | `{tag uint8 (uint16 above 256 variants); payload}`, below |
| packed union | its backing | backing's | a packed type whose tag field selects the variant, language spec section 7.13 |
| `chan T` | 8 | 8 | address of the channel object (`rt.hchan`, section 12.6) |
| `map[K]V` | 8 | 8 | address of the map's table, `rt.MapTable[K, V]`, in a counted box; 0 for a nil map (section 1.4; language spec section 7.10) |

**Layout rules.** An interface is two words, and its representation is the subject of section 5. A string
and a slice are three words, laid out as the table shows.

- **Struct.** Fields appear in declaration order. Each field is placed at the lowest offset, not less than
  the end of the previous field, that is a multiple of the field's alignment. The first field is at
  offset 0. The alignment of the struct is the largest alignment of its fields, and at least 1. The size
  of the struct is the end of the last field, rounded up to a multiple of the struct's alignment. The
  bytes between fields and after the last field are padding, and their contents are unspecified.
- **Zero-size fields.** A type has size 0 when it is an empty struct, an array of length 0, or a struct or
  array made only of such parts. Its alignment is that of its parts (1 for an empty struct). A zero-size
  field is placed like any other field. If the last field of a struct has size 0 and starts at an offset
  above 0, one byte is counted after it before the size is rounded up, so that a pointer to the last
  field never points at the object that follows the struct. Two distinct zero-size objects may have the
  same address. A struct with no field of non-zero size has size 0.
- **Array.** `[N]T` has `N` elements of size(T) bytes each, one after the other, with no padding between
  them, because the size of a type is always a multiple of its alignment. Its alignment is that of `T`,
  and its size is `N` times size(T).
- **Wrappers.** `volatile[T]` is laid out as `T`. `aligned[T, N]` is laid out as `T`, placed at a multiple
  of `N` bytes (or of `T`'s alignment, if that is larger), with a size that is size(T) rounded up to a
  multiple of `N`, so an array of them has no padding between its elements. `ring[T, N]` is laid out as
  `struct { head, tail uint64; buf [N]T }`, so `buf` starts at offset 16, or at `T`'s alignment if that
  is larger, and the size is rounded up to the alignment.
- **Named types.** A named type, an owner type and an enum type have the layout of their underlying type.

For example, `struct { a int8; b int64; c int8 }` has `b` at offset 8 and `c` at offset 16, an alignment
of 8, and a size of 24; `struct { a, c int8; b int64 }` has `b` at offset 8 and a size of 16; and
`struct { a int8; z struct{} }` has `z` at offset 1 and a size of 2 (one byte is counted for the
zero-size last field).

`owned[T]`, `shared[T]`, `synced[T]` and `weak[P]` point at the object, with a header in front of it
(section 1.2). Owner types declared with `owner` (language spec section 7.18) have the layout of their
underlying type; ownership adds no bytes.

A union (language spec section 7.13) is its tag at offset 0, numbering the variants from 0 in declaration
order, and the payload of the variant it holds at the first offset after the tag that satisfies the
largest payload alignment. The bytes after a shorter payload, and between the tag and the payload, are
padding: their contents are unspecified, and equality compares the tag and then the payload. For example
`union { Off; Probing struct{ attempt int }; Ready struct{ irq uint8; base uintptr } }` is 24 bytes, with
the tag at 0 and the payload at 8, and `union { None; Some uint8 }` is 2 bytes with the payload at 1. The
layout of ordinary unions is not part of the C interface, and a program may not depend on it (language
spec sections 20.2 and 21.6): unions are not allowed in `//pu:extern` and `//pu:export` signatures
(section 9.1). Packed unions are exactly their backing.

Package-level variables are in `.data` (constant initializer, written into the image; this includes
arrays and structs whose elements are constants), `.bss` (no initializer, or one computed by the package
initializer), `.percpu` (`//pu:percpu`, section 10.6) or the section named by `//pu:section`. Read-only
data (string bytes, type descriptors, itabs, closure records) is in `.rodata`.

### 1.1 Strings

A string (language spec section 18.8) is three words:

| Offset | Field | |
|---|---|---|
| 0 | `ptr *byte` | the first byte; may be nil for the empty string |
| 8 | `len int` | the length in bytes |
| 16 | `ctl uintptr` | the control word: the address of the buffer's header, or'ed with the flags |

The flags are the control word's low three bits. Bit 0 (`nonASCII`) is set when the string may hold a
byte at or above `0x80`; when it is clear every byte is ASCII. Bits 1 and 2 are reserved and zero. A
control word below 8 has no header: the string is not counted (a literal, a constant, the zero string,
a view made with `unsafe.String`, or a string the runtime keeps in static storage), and copying or
dropping it does nothing. The zero string has a zero control word.

A counted string's bytes follow a 32-byte header, 8-aligned, at the start of an allocation from the
allocator that was current when the string was made:

| Offset | Field | |
|---|---|---|
| 0 | `count int64` | references: one per string value that uses the buffer |
| 8 | `a Allocator` | the allocator the buffer came from (an interface, two words) |
| 24 | `size uintptr` | the allocation's size, the header included |

Substrings share the header: `strings.Slice(s, i, j)` keeps `ctl` and moves `ptr`. Copying a counted string adds one to
`count` and dropping it subtracts one (`pluto/rt.strRetain` and `strRelease`, expanded inline); the
release that reaches zero frees the allocation to `a`. The count is updated with plain arithmetic until
`rt.SetThreaded` runs (when the program starts its first task or thread), and with `lock xadd`
after; freestanding programs use `lock xadd` from the start (language spec section 18.7; memory-model.md
section 3.7).

Static strings in data (a package variable initialized with a constant, a type name in a descriptor, an
enum's names) are written as all three words, with a control word that holds only the flags, computed
when compiling.

### 1.2 Boxes

The object of an `owned[T]`, `shared[T]` or `synced[T]` (language spec section 7.19) is preceded by a
header, 8-aligned, at the start of an allocation from the allocator that was current when the box was
made, or the one passed to `new` or `alloc.Owned`/`Shared`/`Synced`:

| Box | Header, at negative offsets from the object | Size |
|---|---|---|
| `owned[T]` | `-16 a Allocator` (an interface, two words) | 16 |
| `shared[T]`, `synced[T]` | `-32 strong int64`, `-24 weak int64`, `-16 a Allocator` | 32 |

The allocation starts at the object's address minus the header size rounded up to `T`'s alignment (at
least 8), is aligned to that alignment, and is that offset plus `size(T)` bytes long, so freeing needs only
the address and `T`. The strong count counts `shared` or `synced` references; the weak count counts
`weak` references plus one held by all the strong references together. Copying a strong reference adds
one to the strong count and dropping one subtracts one (`pluto/rt.boxRetain`, `boxReleaseStrong`); the
release that reaches zero drops `T` and then releases the weak reference the strong ones held
(`weakRelease`), which frees the memory when the weak count reaches zero. `synced` counts are updated with
`lock xadd` once `rt.SetThreaded` has been called, and from the start in freestanding programs (language
spec section 18.7; memory-model.md section 3.7), and `Upgrade` with `lock cmpxchg`; `shared` counts never
are. A `weak[P]` is the same address as its `P`. Dropping an
`owned[T]` drops `T` and frees the memory (`pluto/rt.boxFree`).

### 1.3 Generation tags and arena epochs

Ordinary builds require static lifetime proof or automatic protection in every mode (language spec
sections 18.11 and 21.5). The following hosted encoding tags heap pointers so that a use of memory that has been freed, or that an
arena has taken back, panics instead of reading or writing whatever the memory holds by then. Bits
63..48 of a pointer hold a 16-bit tag (user-space addresses use 47 bits):

| Tag | Meaning |
|---|---|
| `0` | not runtime checked: ordinary stack/static data, devices, pointers made from a `uintptr`, manual unsafe arena views, and arena views whose complete lifetime is statically proved |
| `0x0001` to `0x7fff` | an allocation's generation (`alloc.Slab`, `alloc.SyncSlab`, and every other heap allocation, whatever its size) |
| `0x8000` to `0xffff` | an arena allocation incarnation (fixed or Chunked) |

**Shared metadata directory.** The runtime maintains one directory for the program, indexed by the
untagged address's 64 KiB interval. Each entry reaches the registered allocation or arena tables that
cover that interval. A registration records its payload bounds, the address from which its granules
are numbered, its `uint16` tag table, and the registration, tag and bounds of any backing allocation
whose lifetime it depends on. Each table entry describes a 16-byte granule. Registrations can overlap
when one allocator takes backing
memory from another; their identities and tables remain distinct.

Every load and store through a pointer that is not statically a frame slot or a symbol checks it in
the same sequence for both kinds of tag (`opt.GenCheck`). The following defines the lookup's meaning;
`registeredLive` reads a consistent metadata snapshot whose tables remain valid for the lookup:

```
tag := p >> 48
a := p & (1<<48 - 1)
if tag != 0 {
	if !registeredLive(a, tag) { panicStale(p) }
}
access a
```

`registeredLive(a, tag)` is true exactly when a live registration reached from `directory[a >> 16]`
covers `a`, every backing allocation is still valid at `a` under its saved tag, and its table entry
`(a - granuleOrigin) >> 4` equals `tag`. A missing registration or a dead backing allocation fails the
check. Backing validity includes the backing table's granule at `a`, so a partial arena release makes
dependent views into the reclaimed part stale even if other granules of the backing chunk remain
live. Backing metadata is validated before reading a child table stored in that backing memory.
The lookup never obtains a tag from unregistered payload bytes. A stale pointer therefore
cannot match by reading data at an old table address after the raw address is reused for a different
allocation layout. Pointer offsets, fields, elements and slices keep the allocation's tag; the
directory reaches the right table for interior addresses as well as for the first byte.

**Contiguous allocations.** Small slab chunks and `Chunked` chunks of at most 56 KiB of data may keep
their tables in the first 8 KiB of a 64 KiB-aligned chunk. A large slab allocation or a large `Chunked`
chunk has its own 64 KiB-aligned metadata base, with its header and granule table before its aligned
payload. The table has enough entries for the complete payload, whatever its size. Its offsets and
granule origin are recorded in its registration. Payload is contiguous: later 64 KiB boundaries are
ordinary data, with no inserted headers or tables. Every directory interval touched by the payload
reaches this registration. A custom allocator may use a different physical metadata layout, but must
provide the same lookup and lifetime contract. Table storage and alignment padding do not change the
requested size or alignment of the payload.

`copy`, `clear` and `print` of a string check their pointer at the call when the length is not zero (a
pointer just past an allocation may point into another one's granule). Metadata is not payload of its
own registration; table entries for the prefix are never used to validate that registration's views.
Loads of itabs and type descriptors, which are static data, are not checked. Generated checks and the
runtime's explicit checks (`rt.checkLive`) use this same directory contract.

**Publication and metadata lifetime.** Before a tagged pointer is returned or published, its table
and all directory entries needed by its payload are initialized and visible to readers. Reclaiming
memory invalidates the affected registration or granules before handing the memory to another user,
returning it to a parent, or unmapping it. A reader ordered after that invalidation fails its check;
it never faults while reading a freed table. Mutations on unrelated allocations cannot give a reader
partially initialized bounds, an inconsistent table address, or reclaimed metadata.

Directory nodes, registration records and tables stay valid for every reader that could still hold
them. Atomic publication of a record alone does not satisfy this requirement if its table can be
freed while a reader uses it. The implementation may retain stable metadata or protect readers while
reclaiming it; the contract does not require a lock on every access. Returning payload pages does not
erase address history. Metadata allocation and publication must not recursively enter an unfinished
registration or call a parent allocator while holding a lock that such a call can acquire again.

**Allocator composition.** A child registration keeps its backing allocation's registration valid.
The child allocator saves the original tagged pointer returned by its parent, together with the
size and alignment needed to free it; an aligned interior address is not a substitute. Dropping or
resetting the child invalidates only the memory it takes back. Returning a backing allocation to its
parent first invalidates its dependent child registrations, then frees it through the saved parent
pointer. Invalidating a parent registration or its backing granules makes every affected dependent
child view stale, even if the child's table still holds the old tag. A heap child and its overlapping
heap ancestor have different generations;
arena children use the noncolliding epoch rule below. Registering or removing a child must never
overwrite the metadata needed to validate a still-live parent or sibling.

**When a check stays valid.** A check is a statement about state the current task can see: the
pointer's memory had not been freed when the task last could have caused or observed a free. It stays
valid until the task reaches a point where it could have: a call to a function that may free (one that
changes a granule's generation or epoch, `pluto/alloc.genRelease` and `pluto/arena.(*Chunked).endEpoch`,
directly, through code inlined from them, or through its own calls), an indirect call, a call to a
function without a body here (assembly or C, other than a few runtime routines that only read the
thread's registers), an intrinsic, or a volatile access. Another task's free becomes visible to this
one only through synchronization (atomics, locks, channels, the scheduler), all of which are such
points, so preemption between a check and an access changes nothing: a free that races with an access,
unsynchronized, is a data race the check could never have caught, since no check is atomic with the
access it guards. A call that never returns, and code from which a function cannot return (a panic's
path), is not such a point for its caller. `opt.GenFrees` computes which functions may free;
`opt.GenCheck` places the checks:

- **Redundant checks.** An access needs no check of its own if its base pointer was checked on every
  path to it since the last such point (a forward dataflow over the function); accesses through the same
  base in one block share the first one's check. Each access uses the base pointer's address without
  its tag, computed once where the pointer is defined.
- **Loops.** In a loop with no such point, the check of a base pointer defined outside the loop moves to
  the loop's preheader when the first iteration surely reaches an access through it (the access
  dominates every back edge, and the loop leaves only through its header's test or a runtime panic
  before it), guarded by the header's test as the first iteration evaluates it. A loop that runs no
  iteration therefore checks nothing, and no program panics over a stale pointer it would not have
  used. A loop that would leave by a runtime panic before the access may report the stale pointer
  instead. A loop whose first iteration could run forever without reaching the access keeps the check
  at the access, since a program that never uses freed memory behaves the same with and without the
  checks (language spec section 18.11).
- **Memory the function holds.** A new allocation (the result of an `Allocator`'s `Alloc`, `new`,
  `alloc.New`, `alloc.Owned`) needs no check until something could free it, and an `owned[T]` parameter
  needs one; until a pointer to it or into it is passed to a call or stored in memory, only a reset of
  the arena it may live in can free it, so its check survives calls that may free other memory but
  reset no arena and synchronize with no other task. An `Allocator`'s `Alloc` takes back nothing it
  handed out and its `Free` only the memory it is given (language spec section 18.9); the arena's bulk
  release (`pluto/arena.(*Chunked).release`, behind `Reset`, `Release` and `Drop`) is the reset. Loops
  whose only such points free but do not reset keep a held pointer's check outside them too.

- **Heap generations and address history.** Each payload granule holds its allocation's generation.
  A never-used address starts at generation 1. `Free` checks the allocation's pointer (a double free
  panics), invalidates its old generation, and advances the generation for reuse. Heap generations
  never wrap: before an address would require `0x8000`, the affected address range is retired from
  future allocations. Existing live backing allocations are not freed by that retirement.

  Generation history belongs to raw address ranges, not to an allocator instance or a mapping. It
  survives an allocator's destruction, directory removal, unmapping, a change of size class or
  layout, and reuse by another allocator or page provider. A new heap registration must use a
  generation greater than every heap generation previously assigned at any raw address in its
  payload. This also distinguishes overlapping live parent and child heap registrations. The
  implementation may retain history per granule or conservatively for larger intervals, or retain
  reservations whose histories cannot be lost. It must prevent a retired range being allocated
  again, rather than restarting its history when pages return at that address. Arena epochs are in
  the disjoint range below and do not erase heap-generation history.
- **Arena epochs.** A `Chunked` takes each chunk of at most 56 KiB of data as a 64 KiB-aligned epoch
  chunk (asking its parent for 64 KiB alignment, or aligning within a larger block): the header
  `{prev *chunk, size uintptr, raw unsafe.Pointer}` at offset 0, data from offset 8192. Larger chunks
  use the contiguous layout above. Both layouts are registered in the directory and retain their
  backing allocation's identity. Allocations start on a granule, so each granule in the arena's table
  belongs to one allocation. The arena's current epoch, taken from a
  noncolliding incarnation source in the range `0x8000` to `0xffff`, goes into the granules
  of each allocation and the tag of its pointer. `Reset`, `Release`, `Drop` and a `Free` of the latest
  allocation zero the granules they take back (no pointer has tag 0) and end the epoch: the next
  allocation takes a new one, so a stale pointer does not match the memory once it is handed out again.
  Incarnation history survives arena destruction and reuse of its backing, since a new arena may
  receive a dropped arena's addresses. No modulo wrap may make a stale pointer match a later
  incarnation. Before exhaustion, the implementation retires affected storage or widens the
  identity; if neither is available, it fails loudly before publishing another allocation. This
  requirement applies to hosted and future kernel encodings (language spec sections 18.11 and 21.5).

  Fixed arenas obey the same lifetime rule. When static lifetime proof does not prevent every
  stale use, their returned views carry a registered incarnation and their reclaimed extents are
  invalidated before reuse. Frame or static backing is not an exemption from allocation lifetime.
  Registration must preserve caller payload, requested alignment, backing identity and the validity
  of still-live prefix allocations after a partial release. The fixed arena's metadata layout need
  not be the Chunked layout; its storage and ownership must follow the explicit allocation contract
  rather than silently using caller payload or a different payload allocator.
- **Disjoint ranges.** A stale arena pointer into memory a slab now uses, or a stale slab pointer into
  an arena chunk, never matches.
- **Counted strings and boxes.** A string's control word (section 1.1) keeps the tag of its buffer, and
  a box's address the tag of its allocation, so retaining and releasing them is checked like any other
  access: the runtime's out-of-line count paths check explicitly (`rt.checkLive`), since the runtime
  and the allocators otherwise only remove tags.
- **Panics.** A failed check calls `pluto/rt.panicStale`: "use of freed memory" for a slab tag;
  "use of memory from an arena that was reset at F (file:line)" for an arena tag, naming the call that
  last took back memory of that epoch (`rt.EpochEnded` records it); "use of freed memory: an arena
  allocation freed at F (file:line)" when that was a `Free`.
  The recorded site identifies the reclaimed address and incarnation together. The same numeric
  tag at an unrelated address must not replace the origin of a stale pointer's diagnostic.

**Heap and arena coverage.** A use of freed heap memory, or of ordinary arena memory after the
arena took it back, panics in a checked build, whatever the allocation's size (language spec section
18.11). Every ordinary heap allocation therefore carries a registered generation and all
arena memory not completely covered by static lifetime proof a registered incarnation, including
fixed-buffer allocations, slab allocations over 2 KiB and arena chunks
over 56 KiB. The contract applies to custom allocators as well as to `Slab` and `SyncSlab` (language
spec section 18.9). A fixed buffer cannot supply an unproved ordinary view with tag `0` merely
because its backing is frame or static storage. Ordinary stack/static and device pointers and
pointers made from a `uintptr` have tag `0`; unsafe code that
creates an untagged pointer has the responsibility stated in language spec section 18.12.

Converting a pointer to `uintptr`, and passing it to a function implemented outside Plutonium, removes
its tag; `unsafe.Pointer` keeps it (language spec sections 18.11 and 20.1). A function marked
`//pu:nochecks` makes no checks (language spec section 18.12). With `--gencheck=false`
(`PLUTO_GENCHECK=0` explicitly selects it for a test run), runtime generation/epoch checks are
omitted: this is the documented unchecked opt-out, and stale access has no defined behavior (language
spec sections 18.11 and 21.5). Freestanding and kernel builds do not select it automatically. If this
hosted pointer encoding cannot represent a target's addresses, the implementation must supply another
suitable protection or statically prove safety; otherwise the operation is a compile error. Required
early-boot/interrupt protection cannot allocate, wait or fail contrary to those contexts' contracts.
This ABI does not prescribe a new kernel encoding or directive.

`Chunked.SetPoison` changes bytes and may make a stale read conspicuous; it does not guarantee detection.
The debug allocator as parent can diagnose accesses to returned chunks only while its protection or
retained evidence permits it; it does not detect every access to suballocations kept by Reset/Release.

### 1.4 Maps

A map value (language spec section 7.10) is the address of a `pluto/rt.MapTable[K, V]` that is the object
of a box with the header of a `synced[T]` (section 1.2): copying a map retains it and dropping one
releases it exactly as for a `synced[rt.MapTable[K, V]]`, with atomic counts once `rt.SetThreaded` has
been called, and the last release drops the table (`MapTable.Drop`, which frees the slots and releases
the keys and elements) and frees the box. A nil map is 0.

The table starts with these fields, whatever `K` and `V` are (`rt.MapSlots` reads them):

| Offset | Field | |
|---|---|---|
| 0 | `slots []slot` | `size` slots of `struct{ key K; val V }`, a power of two, or none |
| 24 | `meta []uint8` | one metadata byte per slot, in the same allocation right after the slots |
| 48 | `size int` | the number of slots |
| 56 | `n int` | the number of entries |

followed by the count of entries and deleted marks, the rebuild limit, the allocator, the hash seed, how
keys are hashed (by their type, or by a function for `pluto/hash.Map`), the key's type descriptor, and the
walks running with the tables kept for them. A metadata byte is 0 for an empty slot, 1 for a deleted
entry, and `0x80 | h>>57` for a full one, where `h` is the key's 64-bit hash. Probing is linear from
`h & (size-1)`. Empty and deleted slots hold zero bytes. The table is rebuilt when full and deleted slots
reach three quarters of `size`: twice as large if at least half of them are entries, otherwise the same
size without the deleted marks.

Keys are hashed with a seed per map: in hosted builds the seeds come from the 16 bytes of `AT_RANDOM` the
kernel puts on the initial stack, mixed with a counter; in freestanding builds from a fixed value unless
the program calls `rt.SetMapSeed`. Integers, booleans, pointers and channels hash their bits; floats their
bits with `-0` taken as `0`; strings their bytes; structs and arrays of plain memory their bytes; other
keys (structs and arrays with strings, floats, interfaces, padding or blank fields, and interfaces) walk
their type descriptor (section 5) and hash each part.

## 2. Registers

| Register | Internal ABI | System V |
|---|---|---|
| RDI, RSI, RDX, RCX, R8, R9 | integer arguments 1 to 6 | same |
| XMM0 to XMM7 | float arguments 1 to 8 | same |
| RAX | 1st integer result; closure context on entry to a func-value call | 1st result; AL = vector register count for variadic calls |
| RDX | 2nd integer result | 2nd result |
| RCX, RSI | 3rd and 4th integer results | not result registers |
| XMM0 to XMM3 | float results 1 to 4 | XMM0, XMM1 only |
| RBX, RBP, R12 to R15 | callee-saved | same |
| RAX, RCX, RDX, RSI, RDI, R8 to R11, all XMM | caller-saved | same |
| RSP | 16-byte aligned at every `call` | same |

The direction flag is clear on entry to and exit from every function, as in System V. The compiler uses
`rep movsb` and `rep stosb` for block copies and clears, which rely on it.

The backend targets baseline x86-64 (SSE2). Integers and pointers live in general-purpose registers;
floats live in XMM registers and use scalar SSE instructions, except in loops the optimizer vectorizes,
which compute two `float64` lanes at once with SSE2 packed doubles (each lane exactly as the scalar code
would). Packed values never cross a call or a function boundary. Memory copies and clears never use XMM
registers: they are sequences of 1 to 8 byte `MOV`s through a general-purpose register up to 64 bytes, and
`rep movsb` / `rep stosb` above that.

## 3. Internal calling convention

### 3.1 Flattening

Lowering turns every Plutonium parameter and result into a list of **scalar leaves**: integers,
booleans, pointers and floats, each with its offset in the value.

| Type | Leaves |
|---|---|
| integer, boolean, pointer, func value, `owned`/`shared`/`synced` | 1 |
| `float32`, `float64` | 1 (float class) |
| `string` | 3: pointer, length, control word |
| slice | 3: pointer, length, capacity |
| interface | 2: table, data |
| struct | the leaves of its fields, in order |
| array of at most 4 elements | the leaves of each element |
| array of more than 4 elements | none: held in memory |
| `volatile[T]`, `aligned[T, N]` | those of `T` |
| packed type of 1, 2, 4 or 8 bytes | 1 integer |
| union | integers whatever the payload holds: the bytes before the payload (the tag and padding), then the payload's bytes, each as 8-byte words and then a 4-, 2- and 1-byte piece for what remains; none (held in memory) for more than 16. `union { Ok int; Err int }` is two leaves, the tag word and the payload |
| `ring[T, N]`, other packed types | none: held in memory |

A value **fits in registers** when it has between 1 and 4 leaves (`maxRegLeaves` in
`internal/lower/layout.go`).

### 3.2 Parameters

Parameters are assigned in this order: the hidden result pointer, if any (section 3.3), then the hidden
region pointer, if any (section 4.1), then the receiver, then the declared parameters.

- A parameter that fits in registers is passed as its leaves, each one as a separate scalar argument.
- Any other parameter is passed **by reference**: the caller makes a copy in its own frame and passes the
  copy's address as one pointer argument. The callee may modify the copy.

The resulting scalar list is assigned to registers by class, in order: integer-class scalars to RDI, RSI,
RDX, RCX, R8, R9, float scalars to XMM0 to XMM7. A scalar that finds no register of its class goes to the
stack, in an 8-byte slot, in argument order: the first stack scalar is at `8(%rsp)` on entry (just above
the return address), the next at `16(%rsp)`, and so on. Narrow scalars occupy a whole slot; the upper bits
are unspecified. The leaves of one aggregate may be split between registers and the stack. The caller
reserves the outgoing area in its frame (no pushes) and owns it after the call.

### 3.3 Results

If the results together have at most 4 leaves, and none of them is held in memory, they are returned in
registers: integer-class leaves in RAX, RDX, RCX, RSI and float leaves in XMM0 to XMM3, each class in
order.

Otherwise the function returns through a hidden **result pointer** (sret), passed as the first scalar
argument (RDI). It points at a caller-owned buffer laid out as a struct whose fields are the results in
order. The callee writes the results there and returns nothing in registers; unlike System V, RAX does
not hold the buffer address on return.

**Strings across calls.** A string argument is borrowed: the caller keeps its buffer alive for the
call, and the callee takes a reference only for a copy it keeps. A string result carries one reference,
which passes to the caller (language spec section 18.8).

### 3.4 Examples

```go
func f(s string, n int) (int, bool)    // RDI=s.ptr RSI=s.len RDX=s.ctl RCX=n -> RAX=int, RDX=bool
func j(s string) (string, error)       // RDI,RSI,RDX=s; results have 5 leaves: RDI=sret, RSI,RDX,RCX=s
                                       //                                  -> nothing
func g(x, y float64, p *T) float64     // XMM0=x XMM1=y RDI=p             -> XMM0
func h(b []byte, v Vec3) error         // RDI,RSI,RDX=b; v={x,y,z float64}: XMM0,XMM1,XMM2
                                       //                                  -> RAX=tab, RDX=data
func k(m [8]int) [8]int                // RDI=sret, RSI=&copy of m          -> nothing
func (r *Reader) Read(p []byte) (int, error)
                                       // RDI=r RSI,RDX,RCX=p               -> RAX=n RDX=tab RCX=data
```

## 4. Func values and closures

A func value is the address of a **closure record**. Word 0 of the record is the code pointer.

- A declared function used as a value, or a function literal that captures nothing, uses a static,
  read-only one-word record named `sym·f`.
- A function literal that captures variables gets a record in the creating function's frame: word 0 is
  the code pointer, word *i*+1 is the address of captured variable *i* (captures are by reference). A
  record the function may return is built in its caller's region instead (section 4.1).
- A method value `x.M` gets a record in the frame holding a wrapper's code pointer (`sym-fm`) followed by
  a copy of the receiver; an interface method value holds the interface value. These too go in the
  caller's region when returned.

A call through a func value loads the code pointer from word 0 and calls it with the **record address in
RAX**, in addition to the ordinary arguments. Functions that do not need it ignore RAX, so a declared
function can be called directly or through its record with the same code. Calls to C functions never
pass a context.

### 4.1 Closure regions

A function whose results can hold a func value without a pointer in between (a func result, an
interface, or a struct, array or union holding one) takes a hidden **region pointer**, a scalar argument
after the result pointer and before the receiver (section 3.2). The caller provides the region, and the
function builds there the closures it may return (language spec section 11.5):

- the records of the function literals and method values it makes and may return;
- the variables those literals capture, which therefore live in the region rather than the function's
  frame, for the function and the closures alike (parameters are copied there on entry);
- the regions of the calls whose closures it may return in turn, after its own variables and records.

The region's layout is fixed per function when compiling: a **drop word** at offset 0, its own variables
and records at offsets assigned in the order the function's code makes them, then each nested region
aligned for its contents. A function that builds nothing has an empty region, without a drop word, and
ignores the pointer, which the caller still passes. The callee reads and writes the region only through
the pointer; it may hold the address of a variable in the region and of a record in it, and returns func
values that point into it.

The region retains every counted component it stores, including those in captured variables and
copied method receivers: strings, maps, channels, counted boxes, weak references and counted errors,
recursively through aggregates (language spec sections 18.7 and 18.8). Pointer-receiver records retain
their existing borrowing semantics. The drop word is 0, or the closure record of the function's **drop
function** (`<function symbol>·rdrop`), installed when there is typed counted cleanup or a nested region.
That function takes the region address, releases each stored counted component exactly once, drops
nested regions and clears the word. This introduces no hidden heap allocation.
The caller zeroes the regions it reserves when it is entered, drops a region
before each call that refills it, and drops its regions when it returns: a call of a known function calls
that function's drop function, and a call through a func value or an interface calls the record in the
drop word, if there is one. The drop before a call comes after its arguments are evaluated; nothing the
call built in the region on its previous run can reach those arguments (language spec section 11.5,
E3101), so dropping and refilling the region cannot change them. A region that turns out to hold nothing
to release has these drops left out.
Calls made while initializing packages get regions in static storage. Their stored references are
released once during normal package cleanup, after their dependent static clients (language spec
section 15.4); the storage itself retains its program lifetime.

The caller reserves the region in its frame, one per call site, reused on every execution of the call,
or passes a part of its own region when it may return the call's closures in turn. A call of a known
function reserves that function's region; a call through a func value or an interface reserves the
largest region of any function of the program with the same signature (receiver aside, for interface
methods; as the first parameter, for method expressions). A function implemented outside Plutonium
(assembly, `//pu:extern`) whose results can hold a func value would have to accept the region pointer
too; C functions cannot return one (section 9).

At `-O2`, once a call is inlined, its region is split into one slot per variable, record and nested
region, so that closure records are seen through and the variables promoted to registers as if the
closure had been built in the caller.

## 5. Interfaces

An interface value is `{tab, data}`.

- For the empty interface, `tab` is the dynamic type's **type descriptor**. For other interfaces it is
  an **itab**, whose first word is the type descriptor. A nil interface has `tab == nil`.
- `data` holds the value's bits when the dynamic type is **direct** (integers, floats, booleans, enums,
  pointers, `unsafe.Pointer`, func values, channels, maps, `owned`/`shared`/`synced`, `weak`, packed
  values of up to 8 bytes, and structs with one field or arrays of length 1 whose field or element is one
  of these pointer-shaped types or, recursively, such a struct or array): zero-extended integers and
  booleans, float bits, or the address. For every other type `data` points at a copy of the value in the
  frame of the function that made the conversion, or in static storage for a conversion in the
  initializer of a package-level variable (language spec section 7.9).

Type descriptor, 88 bytes, read-only, named `type:<type>` (mirrored by `pluto/rt.typeDesc`, read through
`rt.Type`). Its strings are static (section 1.1):

| Offset | Field | |
|---|---|---|
| 0 | `size uintptr` | 16 for interface types |
| 8 | `hash uint32` | FNV-1a of the type string |
| 12 | `kind uint8` | the kind of the type (below) |
| 13 | `flags uint8` | 1 = direct, 2 = interface type with methods (its values hold an itab), 4 = the type has methods, 8 = shared reference, 16 = synced reference |
| 14 | `align uint8` | alignment in bytes (0 if 256 or more) |
| 16 | `name string` | the type's name, written as described below |
| 40 | `equal func(a, b unsafe.Pointer) bool` | closure record, or nil if not comparable or direct, or if the type is only part of a converted type |
| 48 | `elem *typeDesc` | element type of an array, slice, ring or map; the type a pointer, `owned`, `shared`, `synced` or `weak` points at; nil otherwise |
| 56 | `len uintptr` | array length, ring capacity, number of struct fields, or a map's slot size |
| 64 | `fields *fieldDesc` | the struct's field descriptors, `len` of them; for a map, two: the key and the element as fields of a table slot (section 1.4); nil otherwise |
| 72 | `ptrTo *typeDesc` | the descriptor of `*T` when `*T` has methods that `T` lacks; nil otherwise |
| 80 | `drop uintptr` | code address of the drop adapter of `T`, set for shared and synced references and for every type converted into an owning interface (with or without methods) when a `T` needs a drop; 0 otherwise |

The kind numbers are:

| Kind | Type | Kind | Type |
|---|---|---|---|
| 1 | `bool` | 15, 16 | not used |
| 2 | `int` | 17 | array |
| 3 | `int8` | 18 | channel |
| 4 | `int16` | 19 | func |
| 5 | `int32` | 20 | interface |
| 6 | `int64` | 21 | map |
| 7 | `uint` | 22 | pointer |
| 8 | `uint8` | 23 | slice |
| 9 | `uint16` | 24 | string |
| 10 | `uint32` | 25 | struct |
| 11 | `uint64` | 26 | `unsafe.Pointer` |
| 12 | `uintptr` | 27 | packed type |
| 13 | `float32` | 28 | `ring` |
| 14 | `float64` | 29 | `owned`, `shared`, `synced` or `weak` |
| | | 30 | union |

A named type has the kind of its underlying type; an enum has the kind of its underlying integer type;
`volatile[T]` and `aligned[T, N]` have the kind of `T`.

The name of a type is written from the outside in, with no spaces except where shown. A named type is its
package's name, a dot and its name (`main.T`), followed, if it is an instance of a generic type, by its
type arguments in brackets, separated by commas (`hash.Map[string,int]`). A predeclared type is its
name, except that `byte` is written `uint8` and `rune` is written `int32`. An unnamed type is written in
the syntax of the language with its parts named in the same way: `*main.T`, `[]uint8`, `[4]int`,
`interface {}` for the empty interface, and `struct { A int }` for a struct. The name is for printing and
has no part in the identity of a type, which is the address of the descriptor. A descriptor has kind 0
when no kind above describes its type.

Field descriptor, 48 bytes, an array of them per struct type named `typefields:<type>`:

| Offset | Field | |
|---|---|---|
| 0 | `typ *typeDesc` | the field's type (without `volatile` or `aligned`) |
| 8 | `offset uintptr` | byte offset in the struct |
| 16 | `name string` | the field's name; empty when the build strips names |
| 40 | `flags uint8` | 1 = exported, 2 = embedded, 4 = named `_` (comparison and map hashing skip it) |

Descriptors are emitted only for types converted to an interface and for the types they are made of: the
element, pointee and field types of a descriptor, and `*T` when `ptrTo` is set, have descriptors too, so
that `pluto/fmt` can walk any value it is given. The concrete types among them take part in the itab
table like converted types, so a field of a type with a `String` method can be printed with it.

Field names are kept unless the build passes `--fieldnames=false`; they are left out by default in kernel
mode (`--mode kernel`, where `--fieldnames=true` keeps them), since a kernel image rarely prints `%+v`.
Without names, `%+v` and `%#v` print fields as `%v` does.

The descriptor of a union (kind 30) is 112 bytes, continuing with the payload's offset (`uintptr` at 88),
a pointer to a table of the variants in tag order (at 96) and their number (`int` at 104). Each table entry
is 40 bytes: the variant's name (`string`), its payload's type descriptor (nil for a variant without a
payload) and a word that is 1 when the payload is an empty interface and 2 when it is another interface.
`pluto/rt.UnionInfo` reads them, and `fmt` prints a union with them.

Itab, named `itab:<type>,<interface>`: the type descriptor, the interface's descriptor, then one code
pointer per interface method in method order. The itab of an owning interface (language spec section 7.9)
has one more word after the methods: the dynamic type's drop adapter, which takes the data word and drops
the value it holds (an owner's drop, or the release of a `shared`, `synced` or `weak` reference), or 0 when
values of the type need no drop. An owning interface with no methods has no itab (its `tab` is the type
descriptor, as for any empty interface), so its drop adapter is the `drop` word of the descriptor; dropping
the interface calls it with the data word, unless `tab` or the word is 0. Each code pointer is an adapter that takes the data word as
its first argument (in place of the receiver) followed by the method's arguments, and is called with RAX
set to 0. `pluto.itabs` holds every itab for the run-time lookup that conversions between non-empty
interfaces and type assertions to them use. It is an open-addressing hash table: a word holding the
number of slots `n`, a power of two at least twice the number of itabs, then `n` slots, each an itab
address or 0. With `t` and `i` the `hash` words of the concrete type's and the interface's descriptors,
the lookup computes, in 32-bit arithmetic,

```
h = t*0x9E3779B1 ^ i;  h ^= h >> 15;  h *= 0x85EBCA6B;  h ^= h >> 13
```

and probes slots `h & (n-1)`, `h+1 & (n-1)`, ... until it finds the itab or a 0 slot.

## 6. Stack frames

The backend does not keep a frame pointer unless it needs one.

**Without a frame pointer** (the usual case):

```
higher addresses
  incoming stack arguments           8(%rsp_entry), 16(%rsp_entry), ...
  return address                     0(%rsp_entry)
  saved callee-saved registers       pushed in the prologue
  stack slots                        locals, temporaries, spills, interface boxes, closure records
  outgoing stack arguments           0(%rsp), 8(%rsp), ...
lower addresses                      <- %rsp
```

The prologue pushes the callee-saved registers the function uses, then subtracts the frame size; the
epilogue adds it back, pops and returns. Incoming stack argument *k* is at
`frameSize + 8*nSaved + 8 + 8k` from RSP. The frame size is chosen so that RSP is 16-byte aligned at
every call.

**Slots** are sorted by alignment. A slot of 16 bytes or more is placed 16-aligned whatever its type's
alignment, so block copies and clears of it are aligned.

**Leaf functions** that make no calls and have no stack slots get no frame at all: no `sub`, no `add`,
only pushes of any callee-saved registers they use.

**With a frame pointer**: a function with a slot aligned to more than 16 bytes (`aligned[T, 64]` local)
uses RBP:

```
push %rbp; mov %rsp, %rbp; push <saved>; and $-align, %rsp; sub $frameSize, %rsp
```

Incoming stack arguments are then at `16(%rbp)`, `24(%rbp)`, and so on, and the epilogue restores RSP from
RBP.

**No red zone.** Generated code never writes below RSP: a function that needs stack memory
subtracts from RSP first, and a leaf without a frame uses only registers. This holds in every build
mode, which is why kernel mode needs no change to frame layout (section 10.1). The one read below RSP
is a stack probe.

**Stack probes.** The smallest guard region below a stack is one page (language spec section 19.1), so a
function whose frame (the bytes the prologue subtracts, plus the realignment of a frame-pointer frame)
leaves its first call's return address more than a page below the last register it pushed probes the
frame before it subtracts: it reads one word in each page, from the top down, each read within a page
of the one before, so that the first read past the end of the stack faults in the guard and is reported
as a stack overflow (language spec section 17.5). Up to four probes are written out as
`test %rsp, -4096k(%rsp)`; more use a loop in R10 and R11, which hold no argument at entry:

```
lea -size(%rsp), %r11; mov %rsp, %r10
1: sub $4096, %r10; test %r10, (%r10); cmp %r11, %r10; ja 1b
```

An interrupt handler always writes its probes out, since its prologue saves only the registers it uses.
The probes change only the flags, and leave RSP alone, so the call frame information needs no rule for
them.

The prologue pushes the saved registers in the order RBX, RBP, R12, R13, R14, R15. There is no
frame-pointer chain: debuggers walk these frames with the DWARF call frame information in
`.debug_frame`, and the runtime's stack traces with the compiler's trace table, both of which describe
every prologue and epilogue (see [debugging.md](debugging.md)).

## 7. Code model and relocations

Plutonium generates non-position-independent code. Every program is linked statically (`ld -static
-nostdlib`) and code and data must lie within ±2 GiB of each other.

| Use | Encoding | Relocation |
|---|---|---|
| address of a symbol, `sym+disp` | RIP-relative `disp32(%rip)` | `R_X86_64_PC32` |
| direct call | `call rel32` | `R_X86_64_PLT32` |
| symbol plus scaled index, `sym+disp32(,%idx,scale)` | absolute 32-bit displacement, sign-extended | `R_X86_64_32S` |
| pointer stored in data | 64-bit absolute | `R_X86_64_64` |

The `R_X86_64_32S` form folds a global array's address into the instruction's addressing mode and is
correct only when every symbol's absolute address fits in a sign-extended 32-bit value: the low 2 GiB or
the top 2 GiB of the address space. That gives two supported code models:

- **Small** (hosted builds, and freestanding builds linked low): the image is in the low 2 GiB. The
  default `ld` layout for a static executable (0x400000) satisfies it.
- **Kernel** (kernel mode, section 10.3): the image is linked in the top 2 GiB, at or above
  `0xFFFFFFFF80000000`. RIP-relative addressing works unchanged and `R_X86_64_32S` sign-extends to the
  right address.

Position-independent output (PIE, KASLR beyond the top 2 GiB, and PE32+ images that UEFI may load
anywhere) is not supported; it needs a mode that never emits `R_X86_64_32S` and loads addresses with
`lea` instead (planned with PE32+).

**Constant pool.** Float constants and the sign masks used for float negation and absolute value are
16-byte, 16-aligned read-only objects in `.rodata`, addressed RIP-relative. Each is named
`pluto.const.<hi><lo>` after its bits in hexadecimal, so identical constants from different functions
are emitted once. Integer constants are immediates. The pool exists only for float code, so kernel-mode
functions without `//pu:fpu` (no floats, section 10.2) never use it.

## 8. Symbols and sections

| Entity | Symbol |
|---|---|
| function | `path.Name`, e.g. `pluto/fmt.Fprintf` |
| method, value receiver | `path.Type.Method` |
| method, pointer receiver | `path.(*Type).Method` |
| generic instance | `path.Name[T1,T2]`, type arguments package-qualified by import path |
| method of a generic type instance | `pluto/vec.(*Vec).Push[int]`: the receiver's type arguments, then the method's own, in the brackets |
| function literal | `<enclosing symbol>.func<N>` |
| a closure a function returns (language spec section 11.5) | `<function symbol>.func<N>`, e.g. `main.Count.func1` or `pluto/vec.(*Vec).All[int].func1`: named after the function that makes it, not the caller whose region it is built in, and shared by every call |
| the closure of a `ring` iterator (language spec section 7.17) | `builtin.(*ring[T,N]).All.func1`, or `.Values.func1` |
| task statement thunk (section 12.6) | `<enclosing symbol>.go<N>` |
| bound method wrapper | `<method symbol>-fm` |
| a region's drop function (section 4.1) | `<function symbol>·rdrop`; `pluto.noRegionDrop` for the regions that turned out to need none |
| static closure record | `<function symbol>·f` |
| package-level variable | `path.Name` |
| package initializer | `path.init·pkg`; user `init` functions are `path.init.0`, `path.init.1`, ... |
| program entry (runs every initializer, then `main.main`) | `pluto.main` |
| type descriptor | `type:<type>` |
| itab | `itab:<type>,<interface>` |
| itab table | `pluto.itabs`, `pluto.itabtable` |
| main's stack size from `//pu:stack` (0 if none), a function returning a `uintptr` | `pluto.mainstack` |
| string data | `.str.N` (local) |
| static object (a `&T{}` in a package initializer) | `.static.N` (local) |
| float constant | `pluto.const.<hex>` |
| per-CPU block's self word (offset 0 of `.percpu`) | `pluto.percpu.self` |

Symbols contain `/`, `*`, `(`, `[` and `·`, so assembly quotes them: `"pluto/syscall.Syscall"`.
A `//pu:extern name` function refers to the symbol `name`; a body-less function without it refers to its
own Plutonium symbol, implemented in a `.s` file of the same package. `//pu:export name` makes `name` a
C entry point: a second global symbol for the same code, or a wrapper when the signature needs System V
translation (section 9.1); `//pu:export` with no name only keeps the function even if nothing in
Plutonium calls it.

Hosted programs start at `_start` (`lib/pluto/rt/sys_linux_amd64.s`), which aligns the stack and calls
`pluto/rt.start`; that calls `pluto.main` and exits with its result. Freestanding programs provide their
own entry point, which must call `pluto.main` (or the package initializers and `main.main`) on a 16-byte
aligned stack with the direction flag clear.

## 9. Compatibility with System V

| Aspect | Internal ABI | System V | Compatible? |
|---|---|---|---|
| scalar arguments, up to 6 integer and 8 float | registers as in section 3.2 | same | yes |
| further scalar arguments | 8-byte stack slots in order | same | yes |
| one scalar result | RAX / XMM0 | same | yes |
| two scalar results | RAX, RDX / XMM0, XMM1 | a struct returned by value, classed by eightbyte (section 9.1) | **internal only** (the registers agree only for two 8-byte results of one class) |
| three or four scalar results | RCX, RSI / XMM2, XMM3 | memory | **internal only** |
| strings, slices, interfaces, small structs as arguments | flattened to leaves | classified by eightbyte | **internal only** |
| aggregates with more than 4 leaves | pointer to caller copy | copied onto the stack | **internal only** |
| aggregate results | leaves in up to 4 registers, or sret | eightbyte classes, or sret returned in RAX | **internal only** |
| closure context | RAX | none | **internal only** |
| callee-saved registers, stack alignment, direction flag | as System V | | yes |
| red zone | never used | may be used | callers of Plutonium are unaffected; Plutonium never relies on one |

### 9.1 The C convention for `//pu:extern` and `//pu:export`

Calls to a `//pu:extern` function and entries through a `//pu:export name` symbol follow System V. When
the internal ABI already passes a signature exactly as System V does (every parameter a scalar of 4 or 8
bytes, a float or a pointer, and at most one such result), the call is an ordinary internal-ABI call and
an export is a second symbol on the same code, as before. Otherwise lowering classifies the signature:

- **Type mapping.** Integers, enum types, `uintptr`, pointers, `unsafe.Pointer` and `owned`/`shared`/`synced`/`weak` map to
  the C integer or pointer type of the same size; `bool` is C `_Bool` (1 byte); `float32` and `float64` are
  `float` and `double`; structs and arrays inside structs are laid out as in section 1, which matches C
  for these members. `volatile[T]` and `aligned[T, N]` (N at most 8) are `T`. A packed type crosses as its
  storage: `packed uint32 {...}` is a `uint32_t`, and a byte-backed `packed {...}` of n bytes is a
  `uint8_t[n]` (as a parameter, a struct holding that array). It is not a C `__attribute__((packed))`
  struct with misaligned members, which System V passes in memory.
- **Scalars** are passed in registers or 8-byte stack slots as in section 3.2. Integers and booleans
  narrower than 32 bits are zero- or sign-extended to 32 bits by the caller for arguments and by the
  callee for results, as GCC and Clang do; values Plutonium receives are not assumed to be extended.
- **Structs of up to 16 bytes** are split into eightbytes, each classed INTEGER if any scalar in it is an
  integer, pointer or boolean, otherwise SSE. Each eightbyte travels as one 8-byte value in the next free
  register of its class (RDI to R9, XMM0 to XMM7). If the registers left cannot hold every eightbyte, the
  whole struct goes on the stack and later scalars still take the registers that remain.
- **Larger structs** are MEMORY: an argument is copied onto the stack, in argument order, in 8-byte
  slots; a result is written through a hidden pointer the caller passes in RDI (shifting the other
  integer arguments by one), and the callee returns that pointer in RAX.
- **Results.** A scalar is returned in RAX or XMM0. A struct of up to 16 bytes is returned by eightbyte
  class in RAX then RDX for INTEGER eightbytes and XMM0 then XMM1 for SSE ones, so a mixed struct uses
  RAX and XMM0 in either order. **Several results** are returned as a C struct whose members are the
  results in order: `(q, r int64)` is `struct { int64_t q, r; }` in RAX:RDX, `(a, b int32)` is one
  eightbyte in RAX, and three `int64` results are returned through a hidden pointer.
- **Receivers.** A method with `//pu:extern` or `//pu:export` takes its receiver as the first C
  parameter.
- **Rejected,** at the declaration, even if nothing calls it (language spec section 20.4; E2511 unless
  noted): `string` and slices (no C type; pass
  `unsafe.StringData(s)` or `unsafe.SliceData(b)` and the length), interfaces, func values (a func value
  points at a closure record, not at code), maps, channels, rings and unions (their layout is not part of the
  ABI; pass a struct of a tag and the payload), anywhere in a by-value parameter or result,
  including inside structs; arrays as a whole parameter or result (C cannot pass them by value; wrap them
  in a struct or pass a pointer); zero-size types; types aligned to more than 8 bytes; variadic functions
  (C varargs are not supported); generic functions (E2513 with `//pu:export`); `//pu:extern` on a
  function with a body (E2510); and two functions exported under one symbol (E2512).

Strings and slices were rejected rather than passed as their structs because no C declaration of a
function taking `const char *, size_t` would match: both are 24-byte MEMORY structs copied to the stack,
which would make a mistake in the C prototype silent. Strings stay rejected now that they are counted:
C code that kept a string, or a copy of its words, would hold a reference the count does not know about.
Pass `unsafe.StringData(s)` and `len(s)`, and keep the string alive in Plutonium for as long as C uses the
bytes. Behind a pointer, a struct containing slices may be shared with C, with the layout of section 1; one
containing strings may be read by C but not copied or stored by it.

An export whose signature needs translating gets a **wrapper** named by the export: an `ABIC` function
that takes System V arguments, calls the Plutonium function with the internal ABI and returns System V
results. The Plutonium function keeps its own symbol for Plutonium callers. A `//pu:extern` function used
as a func value whose signature needs translating is reached through a **thunk**, an internal-ABI
function named by the declaration's Plutonium symbol that makes the C call. `//pu:export` with no name and
`//pu:export` on a `//pu:interrupt` handler only add a symbol; they never wrap.

C calls never pass a closure context. The backend supports variadic C calls (AL holds the number of
vector registers used), but lowering does not generate them.

### 9.2 Linking C

A package may contain `.c` files, compiled with the system C compiler (`$CC`, default `cc`) as
`cc -c -O2 -ffreestanding -fno-stack-protector`, plus `-mno-red-zone` in freestanding builds, with the
package directory on the include path, and `.o` files, linked as they are. Both follow the file-name
tag rules of `.p` files (`name_linux.c`, `name_amd64.o`), and `.c` files may start with a `//pu:build`
line. Programs are linked with `ld -static -nostdlib`, so there is no C library: C code must not call
libc, and must provide `memcpy` and `memset` itself if the C compiler emits calls to them for large
copies.

## 10. Kernel mode

Kernel mode (`--mode kernel`, language spec section 20.6) is a freestanding build with these differences
in generated code. It sets the build tags `freestanding`, `none` and `kernel`.

### 10.1 No red zone

Plutonium code never uses the red zone (section 6), so kernel mode changes nothing here. Assembly files
and C objects linked into a kernel must also not use it (`-mno-red-zone` for C), because an interrupt
taken without a stack switch pushes its frame directly below the interrupted RSP.

### 10.2 No SSE, x87 or other vector state

Kernel-mode code uses only general-purpose registers, so an interrupt or system call entry need not save
vector state.

- Any operation on `float32` or `float64` values (arithmetic, comparisons, conversion to or from
  integers or between float sizes, float constants, float parameters, results and call arguments, and
  converting a float to an interface) in a function compiled in kernel mode without `//pu:fpu` is a
  compile error (E2520, language spec section 20.7), reported once per function at the first such use,
  for example `float64 arithmetic is not
  allowed in kernel mode (no FPU state is saved; mark the function //pu:fpu to use floats)`. Parameters,
  results and arguments are judged after flattening (section 3.1): a float leaf passed in an XMM register
  is an error, a struct passed by reference is not. Floats may still appear in types, and copying a
  float-typed value as bits is allowed: the backend keeps such values (loads, stores, phis, copies and the
  zero constants that scalar replacement makes) in general-purpose registers.
- The check applies to functions the program uses. Lowering only compiles functions that are reachable
  from the program's roots, so an unused float function in an imported package costs nothing. Package
  variables initialized with constants, including arrays and structs of them, are written into the image
  rather than by the package initializer, so importing `pluto/math` adds no float code. `pluto/fmt`
  passes floats as bits and keeps its float formatting under `//pu:build !kernel`; a kernel build prints a
  float argument as `%!v(float64=0x...)`.
- Block copies and clears avoid XMM registers in every mode (section 2: `MOV` through a general-purpose
  register up to 64 bytes, `rep movsb`/`rep stosb` above), so kernel mode needs no change there. There is
  no constant pool (section 7).
- A function marked `//pu:fpu` may use floats in kernel mode, as may the function literals inside it. Its
  callers must have saved the vector state and enabled the FPU (with `x86.FPUBegin`/`x86.FPUEnd`, section
  10.5); the compiler does not check this.

**The `NoSSE` property.** Lowering marks every function compiled in kernel mode without `//pu:fpu` with
`ssa.Func.NoSSE`. It is the contract between the passes:

- lowering rejects float operations in such a function, as above;
- no optimization pass may introduce float or vector operations into a `NoSSE` function. A vectorizer
  must skip `NoSSE` functions entirely (block copies, clears and fills included). The loop vectorizer
  (`opt.Vectorize`) asks `ssa.Func.MayVectorize`, which is false for `NoSSE` functions and for those
  marked `//pu:nosimd` (`ssa.PragmaNoSIMD`) or `//pu:interrupt`. `//pu:fpu` functions may be vectorized:
  `FPUBegin` saves the whole vector state;
- the inliner never inlines a function without `NoSSE` (a `//pu:fpu` function) into one with it, so FPU
  code stays in functions the caller has to bracket with `FPUBegin`/`FPUEnd`. It may inline in the other
  direction;
- the backend (`internal/arch/amd64`) gives float-typed values general-purpose registers, refuses any
  operation that needs SSE with an error naming the function, and after instruction selection rejects any
  instruction that is an SSE instruction or names an XMM register. A `NoSSE` function therefore cannot be
  emitted with vector instructions, whatever the earlier passes did.

Soft-float was considered and rejected: it would add an IEEE-754 library to every kernel, hide a large
cost behind ordinary arithmetic, and still be the wrong answer for the rare kernel code that wants floats,
which is better served by `//pu:fpu` regions that use the real unit.

### 10.3 Kernel code model

The image is linked in the top 2 GiB (`0xFFFFFFFF80000000` and up) by the kernel's linker script.
Relocations are as in section 7: RIP-relative for symbol addresses, `R_X86_64_32S` for indexed globals and
per-CPU variables (section 10.6), `R_X86_64_64` for pointers in data. The compiler emits nothing that
assumes the low 2 GiB. With `--emit-relocs` a boot loader can slide the image within the top 2 GiB
(KASLR); sliding outside it needs position-independent code (section 7).

`--ldscript` names the kernel's own script. Without it, kernel builds link with a default script
(`build.KernelLinkerScript` in `internal/build/kernel.go`, with `-z max-page-size=0x1000`):

| Output section | Virtual address | Load (physical) address | Holds |
|---|---|---|---|
| `.boot` | `0x100000` | `0x100000` | input sections `.multiboot` (first), `.boot`, `.boot.*`: a multiboot header and the code that runs before paging maps the kernel high |
| `.text`, `.rodata`, `.data` | physical + `0xFFFFFFFF80000000`, from the next 4 KiB boundary | after `.boot` | the kernel |
| `.percpu` | `0` | after `.data`, 64-aligned | the per-CPU image (section 10.6) |
| `.bss` | physical + `0xFFFFFFFF80000000` | after `.percpu` (not loaded) | zeroed data |

It defines `__kernel_phys_start`, `__kernel_load_end` (the physical end of the loaded image),
`__kernel_phys_end` (after `.bss`), `__bss_start`, `__bss_end`, `__percpu_image` (the virtual address of
the per-CPU image), `__percpu_load` (its physical address) and `__percpu_size`; the entry symbol is
`_start`, which the kernel provides. The ELF output has a `PT_LOAD` segment at virtual address 0 for
`.percpu`, as Linux's `vmlinux` once did, so it is meant for loaders that load by physical address
(multiboot, or `--format binary`). `testdata/kernel/smoke` boots a kernel linked this way.

### 10.4 Interrupt handlers

`//pu:interrupt` makes a function an x86-64 interrupt or exception handler. It is available in every
build mode (a hosted program can compile one but has no way to install it).

```go
package x86

type InterruptFrame struct {
	RIP, CS, RFLAGS, RSP, SS uint64
}
```

```go
//pu:interrupt
func timer(f *x86.InterruptFrame) { x86.Outb(0x20, 0x20) }

//pu:interrupt
func pageFault(f *x86.InterruptFrame, code uint64) { rt.PanicFault(rt.MemoryFault, uintptr(x86.ReadCR2()), uintptr(f.RIP), uintptr(f.RSP), 0) } // vectors that push an error code
```

A handler is a plain function (not a method, not generic) with a body, no results, and either one
parameter, the address of the interrupt frame, or two, the frame and the error code. The frame parameter
may be any pointer type, `unsafe.Pointer` or `uintptr`; the error code is `uint64` or `uintptr`. The
signature decides the stack layout, so a handler with an error code must be installed only on the
vectors whose exceptions push one (8, 10 to 14, 17, 21, 29 and 30), and a handler without one on every
other vector, including software `int $n`. This follows GCC's and Clang's
`__attribute__((interrupt))`. Anything else is a compile error (E2570, E2571; language spec section
20.10), as is `//pu:interrupt` combined with `//pu:inline`.

The compiler generates the entry and exit:

1. On entry RSP points at the error code, if the signature has one, or at the saved RIP. In 64-bit mode
   the CPU aligns RSP to 16 bytes before pushing the frame, so RSP is 8 modulo 16 on entry without an
   error code (five words pushed, the same as after a `call`) and 0 with one (six words). The compiler
   uses this to size the frame; it does not realign at run time.
2. The prologue pushes every general-purpose register the handler changes. A handler that makes a call
   also saves every caller-saved register (RAX, RCX, RDX, RSI, RDI, R8 to R11), since the callee may
   change them; callees preserve the callee-saved ones. It then allocates the frame, padded so that RSP
   is 16-byte aligned at every call, and executes `cld`.
3. Outside kernel mode, the prologue saves, with `movups` into the frame, the XMM registers the handler
   changes: the ones its own code uses, and all of XMM0 to XMM15 if it makes a call, because any
   Plutonium, C or assembly callee may use them. A handler that neither calls nor uses floats touches no
   vector register. Saving XMM registers needs SSE enabled (CR4.OSFXSR). x87, AVX upper halves and MXCSR
   are not saved: Plutonium code does not change them, and legacy SSE instructions leave the upper halves
   of the YMM registers alone.

   In kernel mode a handler without `//pu:fpu` saves no XMM registers, even when it makes calls. It is
   `NoSSE` (section 10.2), and so is every function it can reach without `//pu:fpu`; a `//pu:fpu`
   function may only be called between `x86.FPUBegin` and `x86.FPUEnd`, which save and restore the
   interrupted state themselves. So nothing the handler runs changes a vector register except inside such
   a bracket. Assembly and C code called from a kernel-mode handler must likewise leave the vector
   registers alone (`-mgeneral-regs-only` for C) or bracket its use. A `//pu:fpu` handler saves the XMM
   registers it uses, and all sixteen if it calls, as outside kernel mode; it needs SSE enabled.
4. The handler's parameters are the frame's address (the saved RIP, `8(%rsp)` on entry when there is an
   error code, `(%rsp)` otherwise) and the error code (`(%rsp)` on entry). They are not passed in
   registers.
5. The epilogue restores the XMM registers, frees the frame, pops the saved registers in reverse order,
   discards the error code (`add $8, %rsp`) if there is one, and returns with `iretq`. Changes the handler
   makes to `*f` (for example advancing `RIP` past a faulting instruction) take effect then.

Handlers are never inlined, cannot be called directly (including with `defer`) and cannot be used as
func values, all compile errors (E2573); their address is taken with `x86.HandlerAddr(timer)`, which
yields a `uintptr` for the IDT gate. `x86.HandlerAddr` with anything other than the name of a
`//pu:interrupt` function is a compile error (E2572). `//pu:export name` on a handler gives its code a second global symbol, for
assembly stubs that refer to it.

The handler may call ordinary functions: they run on a 16-byte aligned stack with the direction flag
clear, as section 2 requires, and every register they may change is restored before `iretq`. Nothing it
can reach may wait or start a task: no blocking channel operation, no call that reaches the
runtime's `park` or a `//pu:blocking` function, no allocator that may wait (E2584), and no task
statement or other start of a task (E2585). The compiler checks this over the whole program (language
spec section 20.10), and the same for the panic handler, which can run on top of a handler.

A panic inside a handler, or a fault an exception handler reports with `rt.PanicFault`, goes to the
program's `//pu:panic` handler (language spec section 17.4), with a stack trace that starts at the
faulting instruction and ends at the handler's frame.

### 10.5 `pluto/arch/x86` intrinsics

Each function in `pluto/arch/x86` (language spec section 20.8) is expanded inline to the instructions
shown. Except for `HandlerAddr` and `NewDescriptorTablePointer` (an ordinary function with no side effect), lowering turns a call into one `ssa.OpIntrinsic` (named, for example,
`x86.Cli`), which takes the current memory state and produces the next one: no load or store moves,
merges or disappears across it. These instruction intrinsics are compiler barriers (memory model
section 5.2) with side effects, so unused results do not remove or merge their operations. `HandlerAddr`
is only an address computation, orders no memory and may be eliminated when unused. The backend expands it with the fixed
registers the instructions need. Each instruction function (all but `HandlerAddr` and `NewDescriptorTablePointer`) also has an assembly body
(`x86_amd64.s`) for when it is called through a func value. Privileged instructions fault outside ring 0.
Encodings are tested against GNU as.

| Function | Expansion |
|---|---|
| `Cli()`, `Sti()`, `Hlt()`, `Pause()` | `cli`, `sti`, `hlt`, `pause` |
| `StiHlt()` | `sti; hlt` as one machine instruction of the backend, so nothing is scheduled between them and an interrupt cannot arrive in between |
| `SaveFlagsCli() uint64` | `pushfq; pop r; cli` |
| `RestoreFlags(f uint64)` | `push r; popfq` |
| `Inb(port uint16) uint8`, `Inw(port uint16) uint16`, `Inl(port uint16) uint32` | `in %dx, %al/%ax/%eax`, or `in $port` for a constant port below 256 |
| `Outb(port uint16, v uint8)`, `Outw(port uint16, v uint16)`, `Outl(port uint16, v uint32)` | `out %al/%ax/%eax, %dx`, or `out ..., $port` for a constant port below 256 |
| `Rdmsr(msr uint32) uint64` | `rdmsr`, EDX:EAX combined |
| `Wrmsr(msr uint32, v uint64)` | `wrmsr`, with `v` split into EDX:EAX |
| `ReadCR0() uint64`, `ReadCR2() uint64`, `ReadCR3() uint64`, `ReadCR4() uint64` | `mov %crN, r` |
| `WriteCR0(v uint64)`, `WriteCR3(v uint64)`, `WriteCR4(v uint64)` | `mov r, %crN` |
| `Invlpg(addr uintptr)` | `invlpg (r)` |
| `Lgdt(p *DescriptorTablePointer)`, `Lidt(p *DescriptorTablePointer)` | `lgdt`, `lidt` of `*p`, with `p`'s address folded into the operand |
| `Ltr(sel uint16)` | `ltr r16` |
| `Cpuid(leaf, subleaf uint32) (eax, ebx, ecx, edx uint32)` | `cpuid` (the function saves RBX, which it changes) |
| `Rdtsc() uint64`, `Rdtscp() (tsc uint64, aux uint32)` | `rdtsc`, `rdtscp` |
| `Swapgs()` | `swapgs` |
| `ReadGSBase() uintptr`, `WriteGSBase(v uintptr)` | `rdmsr`/`wrmsr` of `IA32_GS_BASE` (`GSBaseMSR`) |
| `GSLoad64(off uintptr) uint64`, `GSLoad32`, `GSStore64(off uintptr, v uint64)`, `GSStore32` | `mov %gs:(r), ...`, or `%gs:off` for a constant offset |
| `FPUBegin(area *FPUState)` | `clts; fxsave64 (area)` |
| `FPUEnd(area *FPUState)` | `fxrstor64 (area)` |
| `HandlerAddr(h any) uintptr` | `lea handler(%rip), r`; `h` must name a `//pu:interrupt` function, checked at compile time. a plain address ordering no memory; invalid or indirect/function-value uses are compile errors (E2572) |

```go
type DescriptorTablePointer packed [10]byte {
	limit uint16
	base  uint64
}

func NewDescriptorTablePointer(base uintptr, size int) DescriptorTablePointer

type FPUState aligned[[512]byte, 16]
```

`FPUBegin` leaves the FPU in the interrupted context's state (its MXCSR and x87 control word included);
it does not enable SSE, which the kernel does once by setting CR4.OSFXSR and CR4.OSXMMEXCPT. A
`//pu:fpu` function called from a function without it must run between `FPUBegin` and `FPUEnd` on the
same area. `rdgsbase`/`wrgsbase` (FSGSBASE) are not used; there is no `--cpu` option yet.

`pluto/arch/x86` is available on amd64 in every build mode; only `Pause`, `Cpuid`, `Rdtsc` and `Rdtscp`
are useful outside ring 0.

### 10.6 Per-CPU variables

A package-level variable marked `//pu:percpu` has one copy per processor (language spec section 20.9):

```go
//pu:percpu
var current *Thread

//pu:percpu
var runQueue RunQueue
```

- The compiler places such variables in the section `.percpu` (`SHT_PROGBITS`, writable, holding their
  initial values), which the kernel's linker script links at virtual address 0 with a load address
  elsewhere, so each symbol's address is its offset in the per-CPU block. The default kernel script does
  this (section 10.3).
- The first word of every block holds the block's own address. The compiler reserves it as
  `pluto.percpu.self`, emitted first in `.percpu` of the Plutonium object; `pluto build` links that
  object first, so a script with `*(.percpu)` puts it at offset 0. After linking an ELF kernel, the build
  checks that `pluto.percpu.self` is at address 0 and fails otherwise.
- A load or store of a per-CPU variable, a field or an element of one, compiles to one `GS`-relative
  instruction with the symbol's offset as an absolute `disp32` (`mov %gs:sym+off, r`, or
  `%gs:sym(,%idx,scale)` for a variable index, relocated with `R_X86_64_32S`), and so cannot be split by
  an interrupt on the same processor. A load used once by an arithmetic instruction is folded into it
  (`add %gs:sym, r`), which reads it once; a read-modify-write such as `v++` is a separate load and store.
  Copies and clears above 64 bytes take the address as below.
- Taking the address (`&v`, slicing an array variable, calling a pointer method) reads the self word at
  that point and adds the offset (`mov %gs:pluto.percpu.self, r; lea sym(r), r`): an ordinary pointer into
  the current processor's copy, which keeps referring to that copy after GS changes (a migration, or
  `Swapgs`).
- Only constant initializers are allowed (constants, and arrays and structs of them): the package
  initializer runs once, before any processor has its own block, so it could only initialize one copy.
  Anything else is a compile error (E2550), as is `//pu:percpu` together with `//pu:section` (E2552).
- Outside kernel mode `//pu:percpu` is a compile error (E2551).
- The runtime has two per-CPU variables of its own: the processor's number that `rt.SetCPU` records and
  panics report (language spec section 17.4), and the current-task slot `pluto/rt.currentTask` (section
  12.3), which `pluto build` places at offset 8, right after the self word, so that a kernel's assembly
  can reach it at `%gs:8`. Every kernel-mode image therefore has a `.percpu` section, and its linker
  script must place it as below; the build checks the slot's offset as it checks the self word's.

**Setting up GS.** The kernel gives each processor its block and points GS at it, before that processor
touches a per-CPU variable:

1. allocate a block of `__percpu_size` bytes (rounded up to the alignment the kernel wants) per
   processor;
2. copy `__percpu_size` bytes from `__percpu_image` (the initial image, mapped with the rest of the
   kernel) into it;
3. store the block's own address in its first word (`pluto.percpu.self`);
4. load the block's address into the GS base: `x86.WriteGSBase(block)` (`wrmsr` of `IA32_GS_BASE`) on
   that processor. A kernel that enters from user space with `swapgs` keeps the block's address in
   `IA32_KERNEL_GS_BASE` (`x86.Wrmsr(x86.KernelGSBaseMSR, block)`) while user code runs, and swaps it in
   on entry.

`testdata/kernel/smoke` does this for two blocks under QEMU, and `testdata/kernel/gsuser` does the same as
a Linux process, setting the GS base with `arch_prctl(ARCH_SET_GS)`.

A lone load or store of a whole per-CPU variable of at most a word needs no region. Any other access, and
every use of the address, is checked to lie in a non-migrating region (language spec sections 19.3 and 20.9;
memory model section 6.4).

## 11. Linux system calls

Linux system calls use their own convention, implemented in assembly in `pluto/syscall` and `pluto/rt`:

| Purpose | Register |
|---|---|
| system call number | RAX |
| arguments 1 to 6 | RDI, RSI, RDX, R10, R8, R9 |
| result | RAX; a value in [-4095, -1] is a negated errno |

`syscall` clobbers RCX and R11.

## 12. Tasks and the scheduler

How tasks are represented and switched, and what a scheduler backend implements (language spec
chapter 19, Concurrency; section 19.5, The scheduler contract, summarizes this section, which is
normative). The runtime side is `lib/pluto/rt/sched.p`, `chan.p`, `timer.p` and `ctx_amd64.s`; the
hosted backend is `sched_linux.p` and `timer_linux.p`; `testdata/kernel/sched` and
`testdata/plutonium/customsched.p` are complete small
backends. The design is in [the scheduler contract](../plans/2026-10-09-scheduler-contract.md); where
its working names differ, the names here are the implemented ones (section 12.8).

### 12.1 The task record

A task's record, `rt.Task`, is allocated by the backend at offset 0 of the backend's
own record (`NewTask`), together with its stack, and belongs to the runtime: a backend reads it through
methods and writes only `Link` and `Data`, two words for its own use (a run-queue link, its own task).

| Offset | Field | |
|---|---|---|
| 0 | `sp` | the saved stack pointer while the task is not running (section 12.2) |
| 24 | `onCPU` (`uint32`) | set while a processor runs the task or is still saving it |

These two offsets are fixed, for a kernel's assembly; the rest of the record (the task's ID, the permit
word, the preemption count and pending flag, the stack bounds, the `alloc.Use` override, channel and
select bookkeeping, the fields of scoped tasks, `join` and `joins`, and the count of drops in progress,
`drops` and `dropCounted`, section 12.10)
is private and may change. `t.SetStack(lo, hi)` records the stack in a record
`NewTask` returns; the runtime places the task statement's arguments at the top of it, then the first
frame below them.

A stack request (`rt.StackRequest`) carries the size the task may use, `Proven` when the compiler has
computed the stack of a bounded call graph and the task needs no more (then no guard page is needed;
language spec section 19.1; roadmap item 18), `Unsized` for a task statement whose stack the compiler
could not size (the size is then `rt.DefaultStackSize`, and a backend that can reserve address space
lazily gives the task a large stack instead, as the hosted one must), and the allocator the stack comes
from: the one given to `rt.Go`, or else the program-wide default allocator, never an override that the
starting task installed with `alloc.Use` (language spec section 19.1).
`t.SetStackGuard(n)` records that the `n` bytes below the stack are an inaccessible guard region, so that
a fault anywhere in it is reported as a stack overflow; without it the guard is taken to be one page.

### 12.2 Contexts and the switch frame

An `rt.Context` is the saved state of code that is not running: a task (`t.Context()`) or a backend's own
loop (an idle loop, a worker's scheduling loop), made with `rt.MakeContext(c, stack, size, fn)`.
`rt.Switch(from, to)` saves the running code in `from` and resumes `to`:

1. It waits while `to`'s `onCPU` flag is set (another processor is still saving it), then sets it.
2. It stores `to`'s task (nil for a backend context) in the processor's current-task slot (section
   12.3).
3. `swapcontext` pushes RBP, RBX, R12 to R15 and RFLAGS, stores RSP in `from`, loads `to`'s RSP, pops
   the same and returns. The caller-saved registers, XMM included, are dead across the call under the
   internal ABI; the direction flag is clear at every call, and Plutonium code does not change MXCSR or
   the x87 control word. RFLAGS is saved so that a task switched away from inside an interrupt handler
   (interrupts disabled) and one that parked with interrupts enabled each resume with their own flag.
4. Back in the context that was switched to, the runtime clears the old context's `onCPU` (a release)
   and finishes with its task: it completes a park, wakes a task that yielded or was preempted, or calls
   `FreeTask` for one that exited.

The saved frame, from the saved RSP upwards:

```
sp+0   RFLAGS
sp+8   R15
sp+16  R14
sp+24  R13
sp+32  R12
sp+40  RBX
sp+48  RBP
sp+56  return address
```

A new context's frame has RFLAGS `0x202` (interrupts enabled), R12 the closure record of the entry
function, RBX and R13 its two arguments, and as return address `rt.goStart`, entered with RSP 16-byte
aligned; it calls the entry through R12's record (RAX = R12) with RDI = RBX and RSI = R13. The entry is
`rt.taskMain(t)` for a task, which runs the task statement's thunk and then exits the task, or
`rt.ctxMain(c, fn)` for `MakeContext`. `goStart`'s call frame information marks it as the outermost
frame, so debuggers stop there, and stack traces stop at `taskMain`.

### 12.3 The current task

The runtime reads the running task from a slot, with no call into the backend:

| Build | Slot |
|---|---|
| hosted, built-in backend | `%fs:8`: each worker thread's FS base points at its worker record, whose first word is its own address and second the current task |
| kernel mode | `%gs:8`: the per-CPU variable `pluto/rt.currentTask`, right after `pluto.percpu.self` (section 10.6) |
| other freestanding builds, and hosted programs with their own backend | one variable: such programs run tasks on one processor |

`rt.Switch` writes the slot. Until the first task operation (the first task statement or the first
operation that may park), the runtime does not read it: the running code is `rt.BootTask()`, the task
that runs package initialization and `main`. At that point it calls the backend's `Init`, if it has one,
and adopts the running code as `BootTask` on this processor; a kernel points GS at the processor's
per-CPU block before then.

### 12.4 Preemption

A task is preempted after a time slice wherever it is (language spec section 19.1). Each task has a
preemption-disable count, raised by `rt.DisablePreemption`, by runtime locks, by
`pluto/alloc`'s per-processor caches (`rt.ProcPin`) and while the task is switched out. A preemption
that arrives while it is non-zero sets a pending flag, and the task yields when the count returns to
zero. `rt.Park` with a non-zero count panics with "blocking with preemption disabled".

- **From an interrupt handler on the task's stack** (a kernel's timer): the handler acknowledges the
  interrupt and calls `rt.Preempt()`, which yields; the handler's own frame holds the interrupted
  state, and when the task is resumed `Preempt` returns and the handler returns with `iretq`.
- **From a signal handler on another stack** (hosted: the monitor thread sends `SIGURG` to a worker
  whose task has run for 10 ms): the handler, on the worker's alternate signal stack, rewrites the
  interrupted RIP in the signal's saved context to `rt.asyncPreempt` and records the old one. When the
  signal returns, `asyncPreempt` runs on the task's stack: it steps over 128 bytes of red zone (which C
  code may use), leaves a slot for the resume address, pushes RFLAGS and the fifteen general-purpose
  registers, saves the x87, MXCSR and XMM state with `fxsave64` in a 16-byte-aligned 512-byte area (not
  in kernel mode, where no vector state is live), and calls `preemptPark`, which yields and returns the
  resume address. It then restores everything and returns with `ret $128`. Every instruction is a safe
  point.

### 12.5 The backend

```go
type Scheduler interface {
	NewTask(req StackRequest) *Task // a record with Task at offset 0, and a stack (SetStack)
	FreeTask(t *Task)               // free an exited task, after the switch away from it
	Spawn(t *Task)                  // make a new task runnable
	Park(t *Task)                   // switch away from t, the running task, until it is woken
	Wake(t *Task)                   // make a switched-out task runnable; interrupt-safe
	Yield(t *Task)                  // switch away from t, which stays runnable
}
```

Optional methods, called if the type has them: `Init()`, once before the first task operation, and
`InInterrupt() bool`, which lets the runtime refuse operations that would park in interrupt context.
Two more, `Now() int64` and `ArmTimer(deadline int64)`, give the runtime a clock and a timer (section
12.9); a program that uses timers needs both, and a build whose scheduler lacks either fails with E2595.
The backend is bound with `//pu:scheduler`, a package-level variable whose type (not an interface)
implements `rt.Scheduler` (language spec sections 19.5 and 20.3; E2592 for two bindings or one of the
wrong type, E2593 for a freestanding program that needs a scheduler and binds none): the compiler gives each of the
runtime's hooks (`schedNewTask`, `schedPark` and the rest, body-less functions of `sched.p` that the
compiler knows by name) a body that calls the bound variable's method directly.

The rules a backend follows (language spec section 19.5; memory-model.md sections 3.6 and 6.3):

1. Every task passed to `Spawn`, `Wake` or `FreeTask` is switched out. The runtime implements the park
   permit: `rt.Ready` that comes before `rt.Park` is never lost, and `Wake` is called only for a task
   that is fully parked. `Park` and `Yield` for a task that yields, is preempted or exits all just
   switch away; the runtime calls `Wake` (or `FreeTask`) for it once the switch is complete.
2. `Park(t)` is also called once for an exited task (`t.Exited()`); after switching away the backend
   must forget `t`.
3. Putting a task on a run queue is a release and taking it off an acquire (a lock or atomics), on the
   same processor or another.
4. `Wake` may be called in interrupt context and from threads that run no tasks: it must not block,
   allocate or switch. The compiler checks it like an interrupt handler (language spec section 20.10).
5. Switch only with `rt.Switch`, which keeps the current-task slot and `onCPU` right, and preempt only
   through `rt.Preempt` (section 12.4).
6. The backend's own locks disable preemption, and also interrupts where `Wake` takes them from an
   interrupt handler; runtime locks (`rt.Lock`) do both in kernel mode already.

### 12.6 What the compiler generates

**Task statements.** `task f(args)` evaluates the function value or receiver and the arguments (with
defaults, and a copy of the array behind variadic arguments) into an argument block in the starting
frame, laid out like a struct, and calls `rt.spawnGo(thunk, block, size, align, bound)`, where `thunk`
is the closure record of a generated `func(p unsafe.Pointer)` named `<function>.go<N>` that loads the
values from `p` and makes the call, and `bound` is the stack size the compiler computed for the call graph
of the function started, or 0 when that graph is not bounded (language spec section 19.1; roadmap item
18). `spawnGo` asks the backend for a task with a stack of `bound` bytes (`Proven`), or, when `bound` is
0, an unsized one (`Unsized`, `rt.DefaultStackSize`); either comes from the program-wide default
allocator, never from an override that the starting task installed with `alloc.Use`. It copies the
block to the top of the stack and calls `Spawn`.

**Channels.** A channel is the address of an `rt.hchan`: a runtime lock, the buffer's count, capacity,
indices and element size, the buffer, queues of waiting senders and receivers, the allocator it came
from, then the reference count (at offset 136) and the channel's timer or nil. The count follows the single rule of language spec section 18.7 (When counts are atomic): plain
arithmetic before hosted threading, atomic operations before another thread can run, and atomic from
freestanding startup. A task waiting on a channel holds a reference to it while it waits: `chanRetain(c)` takes a
reference, and a release calls the
release glue the compiler makes for the element type, `chan T·release(c)`, which calls
`chanRelease(c) bool` and, for the last reference, drops the values still buffered (`chanBuffered(c)`
gives the buffer, the first index, the count and the capacity) and calls `chanFree(c)`, which stops and
frees the timer and frees the channel; for element types with nothing to drop the compiler calls
`chanDrop(c)` instead. `makechan` returns the channel with one reference, the caller's. The operations
call the runtime with the address of a temporary holding (or receiving) the element:

| Operation | Runtime call |
|---|---|
| `make(chan T, n)`, `make(chan T, n, a)` | `makechan(size, align, n)`, `makechanWith(a, size, align, n)` |
| `ch <- v` | `chansend1(c, &v)` (may park) |
| `<-ch`, `v, ok := <-ch`, `for v := range ch` | `chanrecv1(c, &v)`, `chanrecv2(c, &v) bool` (may park) |
| `close(ch)`, `len(ch)`, `cap(ch)` | `closechan(c)`, `chanlen(c)`, `chancap(c)` |
| `select` without `default` | `selectgo(cases, order, n) (int, bool)` (may park) |
| `select` with `default` | `selectnb(cases, order, n) (int, bool)`, -1 for the default |

A select's cases are an array of `n` `rt.scase` in the selecting frame, `{c, elem, kind, sudog}` with
kind 1 for a receive and 2 for a send, whose embedded waiting record the runtime uses while the select
waits; `order` is `2n` `uint16` of scratch for the random poll order and the lock order (by channel
address). The result is the index of the case that ran and, for a receive, whether a value was received.
A blocked operation's waiting record (`sudog`) lives in the waiting task's frame.

### 12.7 The hosted backend

| Part | |
|---|---|
| processors (P) | one per CPU, or `$PLUTO_MAXPROCS`: a 256-entry lock-free run queue that its worker pushes to and pops from and idle workers steal half of, and a cache of free stacks; a global queue behind a lock takes the overflow and tasks readied by threads without a P |
| workers (m) | threads made with `clone(2)` (`CLONE_VM`, `CLONE_FS`, `CLONE_FILES`, `CLONE_SIGHAND`, `CLONE_THREAD`, `CLONE_SYSVSEM`, `CLONE_SETTLS` with the worker record as FS base), each with a 256 KiB scheduling stack and a 32 KiB alternate signal stack; idle workers sleep on a futex |
| monitor | a thread that preempts a task after 10 ms (`SIGURG`) and hands the P of a worker that has been in a system call (`rt.EnterSyscall` to `rt.ExitSyscall`) for 20 µs, when other work waits, to another worker |
| poller | a thread blocked in `epoll_wait` on descriptors registered once, edge-triggered, for reading and writing. `rt.PollOpen(fd)` makes a `rt.PollDesc` (kept on a free list, never unmapped) holding a queue of read waiters, a queue of write waiters and a flag for each direction for an event that arrived with nobody waiting; `Wait(write)` parks until an event (registering the descriptor on the first wait), `Evict` wakes the waiters with `rt.PollClosed` and `Release` deregisters it. While timers are armed it waits at most until the earliest deadline of every P (`epoll_pwait2`, or `epoll_wait` in milliseconds rounded up before Linux 5.11), then runs the expired timers itself; an eventfd in the epoll set wakes it when an earlier timer is armed |
| timers | a heap per P (section 12.9), which its worker runs each time it looks for a task; a spinning worker runs the expired timers of the other Ps too, and their tasks come to its own P. `rt.Now` reads `CLOCK_MONOTONIC` through the vDSO's `__vdso_clock_gettime` (found from `AT_SYSINFO_EHDR`), or with the system call if there is none |
| stacks | an unsized request gets a reserved stack: `rt.HostedStackSize` (64 MiB) mapped `MAP_NORESERVE \| MAP_STACK` with a 1 MiB guard region (`PROT_NONE`) below and one more page above, whose end holds the backend's task record, normally reserving at least 64 MiB of address space. If address-space limits prevent that reservation, the runtime tries a smaller reservation, never below 64 KiB, with the same overflow protection (language spec section 19.1). This virtual reservation is not physical-memory commitment; explicit stack requests retain their stated sizes. Reserved stacks are cached (8 per P, 64 shared); one going into a cache gets `madvise(MADV_DONTNEED)` below its top 16 KiB if its task was seen below that when it parked, yielded or was preempted, or on every 16th use. Any other request gets a guard page, the stack, then the task record, from the requested allocator; stacks of one size from the program-wide default allocator are cached rather than freed |
| deadlock | when every worker is idle, nothing waits in the poller or a system call and no timer is armed: "fatal error: all tasks are asleep - deadlock!", then each task's number, wait reason and trace (`rt.TraceTask`; the backend keeps a list of its tasks in creation order for this, and `BootTask` comes first), status 2. The poller checks again after it runs timers, since a timer that fires may leave nothing to wake |
| `SIGPIPE` | caught at startup (every hosted program, not only those that start tasks) by a handler that returns, so a write to a pipe with no reader fails with `EPIPE`; `pluto/os` calls `rt.DieFromSigpipe` (default action, then `tgkill` of the calling thread) for `EPIPE` on descriptors 1 and 2 |

The program's thread becomes the first worker when the first task operation runs (`Init`); the main
task keeps running on the original stack, and the worker's scheduling loop gets a stack of its own.

### 12.8 Names in the scheduler contract note

| Contract note | Implemented |
|---|---|
| `Start(t)` | `Spawn(t)` |
| `Block()`, `Resched()` | `Park(t)`, `Yield(t)`: the runtime records what the switch is for |
| `Exit()` | `Park(t)` with `t.Exited()`, then `FreeTask(t)` from the runtime after the switch |
| `rt.Switch(from, to *Task)` | `rt.Switch(from, to *Context)`, which also serves idle and scheduling loops |
| `rt.InitStack` | done by the runtime between `NewTask` and `Spawn` |
| `DisablePreempt`, `EnablePreempt` | `rt.DisablePreemption`, `rt.EnablePreemption` |
| `BlockUntil(deadline)` | none: the runtime keeps the timers, and `rt.ParkUntil` is `Park` with a runtime timer that readies the task (section 12.9); a backend provides only `Now` and `ArmTimer` |
| `Now()` | `Now()`, an optional method, needed by programs that use timers; `rt.Now()` calls it |
| `ParkUntil(deadline)` | `rt.ParkUntil(deadline) bool`, true when readied, false on timeout |
| `rt.TraceTask(t, w)` | `rt.TraceTask[W Writer](t *Task, w W)`: the trace of a switched-out task from `t.Context()`'s saved stack pointer (debugging.md section 5); `Task.Parked` and `Task.WaitReason` (set by `rt.ParkReason`) say whether and why it waits |
| `rt.StackOverflow` | not yet; a kernel reports an overflow with `rt.PanicFault` |

### 12.9 Timers

The runtime implements timers on the backend's clock (language spec section 19.6). `rt.Now()` is the clock: monotonic nanoseconds,
the same on every processor, from the backend's `Now` method. An `rt.Timer` calls a function once its
deadline has passed, and again every period if it has one:

```go
func (t *Timer) Init(f func(arg unsafe.Pointer, now int64), arg unsafe.Pointer)
func (t *Timer) Reset(when, period int64) bool // arm; reports whether it was armed
func (t *Timer) Stop() bool                    // disarm; reports whether it was armed
func ParkUntil(deadline int64) bool            // Park, or false once deadline has passed
func RunTimers()                               // run the expired timers of the shared heap
const NoDeadline int64 = 1<<63 - 1
```

A `Timer` is a node of an intrusive pairing heap (child, sibling and back links in the record), so arming,
stopping and firing never allocate. Each heap has a runtime lock, and a timer's function runs with it
held: `Stop` therefore waits for a function that is running, and once it returns the function's memory
may be freed. A timer's function runs wherever timers run (a hosted worker between tasks, the
hosted poller thread, or a kernel's timer interrupt), so it must not block, allocate or use timers; when
an interrupt handler calls `rt.RunTimers`, the interrupt-context check (language spec section 20.10)
follows the call into every function of type `func(unsafe.Pointer, int64)` the program takes. The
library's functions ready a parked task (`ParkUntil`), send on a channel without waiting or counting
(`rt.ChanTrySend`, for the channels of `time.After`, `time.Tick`, `time.Timer` and `time.Ticker`) or queue
an `AfterFunc` for a task that starts it. While an `AfterFunc` timer is armed, the runtime holds one
reference to its `synced[Timer]` object; it is released when the function is started or when `Stop` succeeds,
and the object is freed after the last reference is released (language spec section 19.6). A channel's timer (`rt.ChanTimer(c, f)`) belongs to the
channel, which stops and frees it when it is freed; the timer's pointer to the channel is not counted.

`ParkUntil(deadline)` arms a timer in its frame whose function readies the task, and parks. A ready and
the timeout race on a word in the frame, and the loser does nothing: the timer's function readies the
task only if no other `Ready` came first. A `Ready` that loses to the timeout still grants its permit,
so a caller that put the task on a waker's list removes it, and if the waker already took it, consumes
that wakeup with `Park`. A permit granted before the call is consumed at once (true).

**Kernels.** A backend that keeps no heaps of its own shares the runtime's one heap and provides two
methods:

| Method | Called | Must |
|---|---|---|
| `Now() int64` | by `rt.Now`, on any processor, in any context | return monotonic nanoseconds, the same on every processor; never block |
| `ArmTimer(deadline int64)` | when the heap's earliest deadline becomes earlier than the last one given, with the heap's lock held (interrupts disabled in kernel mode) | arrange for `rt.RunTimers()` to be called once `Now()` has reached `deadline`; not call `RunTimers` itself, block or allocate |

`rt.RunTimers()` runs the expired timers and gives the next deadline to `ArmTimer`. It never blocks and
may be called from an interrupt handler: a kernel calls it from its timer interrupt once the armed deadline
has passed (or on every tick, which costs a lock and a comparison), and with a tickless timer reprograms
the hardware in `ArmTimer`. `Timer.Reset` and `Timer.Stop` take only runtime locks, so they work in
interrupt context too. `testdata/kernel/sched` counts PIT ticks as its clock and runs the timers from
the PIT handler; `testdata/plutonium/customsched.p` sleeps in its idle loop until the armed deadline.

**Hosted.** The built-in backend keeps a heap per P instead (section 12.7): a timer goes into the heap of
the P that arms it, and stays there until it fires or is stopped, whichever worker runs the heap. Its
`ArmTimer` does nothing.

**Sleeping without a scheduler.** `rt.Sleep(ns)` is declared without a body. Lowering
(`internal/lower/sched.go`), which knows it by name, gives it its body after every other function has
been lowered: a call of `sleepDirect` if the backend is the default
hosted one (the `//pu:scheduler` variable of `pluto/rt`) and `rt.startTasks` was never reached, otherwise
a call of `sleepParked`. `startTasks` is what a task statement, a blocking channel operation or `pluto/sync`
wait (through `Park`), `ParkUntil`, an armed `Timer` and the poller all call before they use the
scheduler, so reaching it means the program starts one. `sleepDirect` sleeps until an absolute
`CLOCK_MONOTONIC` time with `clock_nanosleep`, repeated if a signal interrupts it, so a program that only
sleeps has one thread and links `startTasks`, `startHosted`, `sysmonMain` and `startPoller` out.
`sleepParked` is `ParkUntil` in a loop. A late hook that finds the scheduler started after it chose the
direct body is a compiler failure.

### 12.10 Scoped tasks

A `sync.Scope` (language spec section 19.4) holds one `rt.Join`, which counts the tasks started for it that
have not yet returned. Only the runtime changes the count, under a lock in the `Join`:

| Part | |
|---|---|
| `rt.Join` | the count `n`, a lock, the task parked in `Wait` (`waiter`), and the links of its owner's chain (`next`, `owner`). It lives in the frame of the function that declares the `Scope`. |
| `rt.spawnScoped(j, ...)` | what a scoped `s.Task(f)` call becomes. Lowering builds the argument block and thunk of a task statement in place, with the address of each shared captured variable and the value of each taken one (language spec section 19.4), and calls this instead of `spawnGo`. It counts the task in `j` before the task can run. `sync.Scope.Task`, for a `Scope` the compiler cannot see in the starting frame, calls `rt.TaskJoined(j, f)`, which checks `f` as a task statement does (language spec section 12.12). |
| `rt.taskMain` | after the thunk returns, it uncounts the task with `joinExit`, which wakes the waiter when the count reaches zero and touches nothing of the `Join` afterwards, since the waiter's frame may be reused at once. |
| `rt.JoinWait(j)` | `Wait` and `Drop`: returns at once (after one lock) when the count is zero, otherwise parks with the reason `sync.Scope.Wait` (`rt.WaitScope`). A second task waiting on the same `Join` panics with "sync: two tasks wait on one Scope". |

**The join chain.** Each task record keeps the chain of `Join`s whose tasks it started (`rt.Task.joins`), and
the `Join` that counts the task itself (`rt.Task.join`). A `Join` is put on its owner's chain when its first
scoped task starts, and taken off when a `Wait` or drop finds it at zero. A kernel calls
`rt.WaitScopes(t *rt.Task)` for a task `t` that is not running: it parks the caller until every task started
for a `Join` on `t`'s chain has returned, and empties the chain.

**The rule for backends.** A backend must not free or reuse the stack of a task that has not returned
normally until `rt.WaitScopes` has returned for it, since scoped tasks may still use variables in that
stack. A task that returns normally has joined its `Scope`s through their drops and needs nothing. Hosted
builds never need the call: `rt.ExitTask` and `testing`'s `FailNow` leave frames through their ordinary
return paths, so each drop joins (language spec section 17.3), and a hosted panic runs deferred calls (not
drops) and then ends the process, so no frame is reclaimed while tasks run.

**Normal termination.** After `pluto.main` returns normally and its own cleanup is complete, the runtime
makes one transition (language spec section 15.4, When `main` returns): it closes admission of tasks and
application callbacks and observes whether any other application task is still running (started and not
yet finished with its function's deferred calls and drops) or any application callback is in flight. A
failed start retains the caller's provisional argument ownership. The runtime does not wait for a task
that is running.

- If none is running, pending application timer callbacks are canceled as `Stop` cancels them, which
  retires their saved arguments/captures and backend registrations. Internal service timers and timers
  that only deliver channel values are not application callbacks. The rest of this section applies:
  package cleanup, then final accounting.
- Otherwise the runtime ends the program at once with `main`'s status, as `os.Exit` does, after `main`'s
  own cleanup. No package cleanup runs, and no owner held by the remaining tasks is dropped. The debug
  allocator is told how many tasks are running (`debug-allocator.md`).

An application task that has ended is not retired until its frame cleanup, stack release, task-record
release and their allocator callbacks complete. The runtime waits for that retirement of ended tasks, which
is bounded runtime work, before package cleanup. A backend must report this completion after those
obligations, not at `joinExit`. Completion publication must establish release/acquire ordering, or an
equivalent synchronization, so all operations of every finished task and callback, mandatory cleanup,
stack/task-record releases and their allocator callbacks happen before package cleanup begins, across
every processor. A backend cannot satisfy this by observing a count without the required visibility
(memory-model.md section 3.9). Deadlock detection remains active until the program ends.

The compiler supplies cleanup for named package variables in reverse actual initialization order,
including zero-initialized variables and hidden static owning/counting objects (language spec section
15.4). Cleanup uses existing drop and reference-release glue in place, preserving static receiver
addresses, and zeroes each completed place. A static object's storage remains live. Compiler-emitted
static-object bounds and cleanup progress must reject typed writes overlapping a completed
resource-bearing place, including aliases, scalar fields and zero writes, with a run-time panic. Compiler
mandatory zeroing remains part of cleanup. The check precedes old-value
release or ownership transfer. Completed fields/elements are protected while their enclosing object
is still being finalized. Metadata is bounded by the compiler-known static objects and types; no
application heap allocation or general ownership tracing is introduced by this bookkeeping.

Runtime scheduler, timer, poller, allocator and reporting facilities remain usable until their clients
finish. Backend-specific queued application work must be retired before package cleanup; essential
internal services are quiesced and reclaimed afterward in dependency order. Final debug accounting
runs after application cleanup and reclaimable runtime storage release, while its allocator and output
dependencies remain alive. Private permanent metadata has an explicit runtime lifetime and is not an
application allocation. Custom backends obey the same completion/admission contract.

The named `rt.faultPanic` record of type `rt.Panic` belongs to the runtime's
fault-reporting facilities. It is empty on normal execution and filled only
while entering the abrupt signal-fault reporting path. It remains available
through cleanup and is not an application package destruction obligation.

An exit raised by a required cleanup is collected while remaining mandatory releases run. Package
cleanup accumulates `rt.ExitTask`, completes the remaining cleanup schedule and preserves the normal
return status; it does not resume subsequent ordinary statements in the exiting destructor. Panic and
explicit `os.Exit`/`rt.Exit` retain their abrupt behavior. The main-task `ExitTask` path is not normal
return and does not enter this schedule (language spec section 17.3).

**Drop bookkeeping.** The compiler calls `rt.dropBegin` at the start of every drop glue function and
of every last release of a `shared` or `synced` object (before its drop, until its memory is returned), and
`rt.dropEnd` at the end; the runtime does the same around the free of a string's buffer. `rt.Task.drops`
counts the drops a task is inside, and a global count the tasks that are inside one once tasks have
started (`rt.taskMain` uncounts a task that leaves one with `rt.ExitTask`). This bookkeeping does not
substitute for full task retirement at normal termination. The main task may park with the reason
`exit` (`rt.WaitExit`) while ended tasks complete their retirement; a program without tasks carries no
scheduler solely for shutdown. `os.Exit` and `rt.Exit` do not perform that retirement or package cleanup.
A `sync.Scope` that moves while tasks it started may run stops the program (`rt.joinMoved`, which the
compiler calls before any move of a value that holds a `Scope`).

Wait reasons in a deadlock trace: `sync.Scope.Wait` for a task parked in `Wait` or a drop, and `exit` for
`main` waiting for ended tasks to retire at normal termination.
