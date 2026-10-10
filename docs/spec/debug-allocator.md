# The debug allocator

Plutonium's raw pointers are unchecked, so a program can write past the end of an allocation, use memory
after freeing it, free it twice or free something it never allocated. The debug allocator,
`pluto/alloc/debug`, is the backstop: an `alloc.Allocator` that turns each of these into an immediate,
deterministic stop with a message saying what went wrong and where, and that lists the allocations still
live at exit.

It is meant for testing and debugging. It is much slower than the slab allocator and uses far more memory
(see [Cost](#cost)).

## Turning it on

Build a hosted program with the flag:

```sh
pluto build --alloc=debug ./cmd/server
pluto run --alloc=debug ./cmd/server
```

The flag makes the debug allocator the default allocator before any package initialisation runs, with no
code changes. It sets two build tags: `allocdebug`, which links in `pluto/alloc/debug` and installs it,
and `allocsites`, which makes allocations record where they were made. Settings are read at startup from
the `PLUTO_DEBUGALLOC` environment variable (below).

A build flag rather than an environment variable turns it on because then a normal build carries none of
it: the package is not linked, and the call-site bookkeeping in `new`, `make` and the `pluto/alloc`
functions compiles away. Tuning is by environment variable so one debug build can be rerun with different
settings, as with `ASAN_OPTIONS`.

A program can also install it itself, in which case it should do so before allocating anything, since
memory from the previous allocator cannot be freed through it:

```go
o := debug.DefaultOptions()
o.Underflow = true
alloc.SetDefault(debug.New(o))
```

or scope it to a region with `alloc.Use`, or hand it to a container (`vec.New[T](d)`). Build with
`--tags allocsites` to get allocation sites in that case.

`--alloc=debug` is for hosted builds. A freestanding program creates the allocator over its own page
provider with `debug.New(debug.Options{Pages: pages, ...})`; see [Freestanding builds](#freestanding-builds).

## What it checks

| Mistake | How it is caught | When |
|---|---|---|
| Writing or reading past the end | the allocation ends right before a guard page that is inaccessible | at the access (a fault) |
| Writing before the start | with `underflow`, the allocation starts right after the guard page | at the access (a fault) |
| Writing just outside, without a fault | the rest of the allocation's pages, or its redzones, hold canary bytes | when it is freed, or at exit |
| Writing to freed memory | freed memory is made inaccessible, or poisoned and checked | at the access, or when it leaves quarantine, or at exit |
| Reading freed memory | freed memory is made inaccessible, or reads poison | at the access, or the value is `0xDD` bytes |
| Double free | the side table still holds the block, as freed, while it is in quarantine | at the second free |
| Free with the wrong size or alignment | the side table records both | at the free |
| Free of an interior or unknown pointer | the side table has no allocation there | at the free |
| Leaks | the side table lists every live allocation | at exit, or `alloc.ReportLeaks()` |

Every error stops the program the way a panic does: the message starts with `panic: `, deferred calls do
not run, and the exit status is 2.

### Overflow and underflow

Each allocation gets pages of its own, followed by a guard page that is mapped `PROT_NONE`. The allocation
is placed at the end of its pages, so the first byte past it is the first byte of the guard page and an
overrun faults on the spot:

```
panic: debug allocator: heap overflow: write at 0x7f3a1c3d5000, offset 10 of the 10-byte allocation at 0x7f3a1c3d4ff6 (allocation #1)
  faulting instruction at pc 0x40135a
  allocated at pc 0x401277
```

The end of the allocation is rounded down to its alignment, so an allocation whose size is not a multiple
of its alignment (a 10-byte block aligned to 8) leaves up to `align-1` bytes before the guard page. Those
bytes, and the unused start of the first page, hold `CanaryByte` (`0xFD`) and are checked when the
allocation is freed:

```
panic: debug allocator: heap underflow: the 10-byte allocation at 0x7f0e6a2c2ff6 (allocation #1) was written before its start, at offset -3; found when it was freed
  allocated at pc 0x401277
```

With the `underflow` setting the guard page comes first and the allocation starts on the next page, so
underruns fault and overruns are found by the canaries at free.

Guard pages cost at least two pages and two kernel mappings per allocation, and Linux allows about 65530
mappings per process, so the number of page-guarded blocks (live or in quarantine) is capped at 16384
(`guards=N`). Past the cap, the oldest page-guarded block in quarantine is released to make room, and if
every page-guarded block is live, new allocations get canaries instead. With `canary` or `canary=N`, all
allocations, or those smaller than N bytes, get canaries from the start: they are placed in a slab slot
between 16-byte redzones of `CanaryByte`, checked when the allocation is freed and again when it leaves
quarantine.

### Use after free

Freeing fills the allocation with `PoisonByte` (`0xDD`) and, for a page-guarded block, makes its pages
inaccessible, so any later access faults:

```
panic: debug allocator: use after free: write at 0x7fd24f8e7ff8, offset 16 of the 24-byte allocation at 0x7fd24f8e7fe8 (allocation #2, freed)
  faulting instruction at pc 0x401456
  allocated at pc 0x40125e
  freed at pc 0x401455
```

A canary-guarded block stays readable: a stale read sees poison (an `int64` reads `0xdddddddddddddddd`, a
pointer is non-canonical and faults if followed), and a stale write is found when the block leaves
quarantine or by the heap check at exit:

```
panic: debug allocator: write after free: the 24-byte allocation at 0x7c1b8e1f0010 (allocation #2) was written at offset 16 after it was freed
  allocated at pc 0x40125e
  freed at pc 0x401455
```

Freed blocks are held in a first-in, first-out quarantine, 64 MiB by default (`quarantine=N`), before their
memory is reused, so a stale pointer keeps faulting or reading poison for as long as possible. Page-guarded
blocks count their whole pages towards the limit.

### Double, wrong-size and invalid frees

A side table records every allocation, live or in quarantine, with its size, alignment, allocation number
and the call sites that allocated and freed it. `Free` checks the pointer against it:

```
panic: debug allocator: double free of 0x7f9a2e1b2fe8 (24-byte allocation #2)
  freed again at pc 0x40158a
  allocated at pc 0x40125e
  freed at pc 0x401582

panic: debug allocator: free of 0x7b6e1e331ff6 with size 16, align 1: it was allocated with size 10, align 1

panic: debug allocator: invalid free of 0x7a0d560d9ffa: it points at offset 4 of the 10-byte allocation at 0x7a0d560d9ff6

panic: debug allocator: invalid free of 0x410040: it was not allocated by this allocator, or was freed long enough ago to leave quarantine
```

A double free is recognised while the first free is still in quarantine; after that the pointer is simply
unknown. Size 0 is treated as 1, as `new` and `alloc.New` do, and alignment 0 as 1.

### Leaks

At normal exit (main returns, or `os.Exit`), hosted builds check the canaries and poison of every block
and then list the allocations still live:

```
debug allocator: 2 allocations still live, 34 bytes:
  10 bytes at 0x7f1d2c9e4ff6 (allocation #1), allocated at pc 0x401277
  24 bytes at 0x7f1d2c9e6fe8 (allocation #2), allocated at pc 0x40125e
debug allocator: exiting with status 2 because of leaks
```

By default leaks fail the program (status 2 instead of 0); `leaks=report` only lists them and
`leaks=off` says nothing. `alloc.ReportLeaks()` prints the same list on demand for the default allocator
and returns the count (0, printing nothing, for allocators that do not track allocations), and
`(*debug.Allocator).ReportLeaks` does it for a particular one. Panics and faults skip the report.

### Zeroing

The `Allocator` interface promises zeroed memory, and the debug allocator keeps that promise: fresh pages
come zeroed from the page provider, reused page-guarded blocks are zeroed explicitly, and canary-guarded
blocks come from a slab allocator that clears slots when they are freed. Only the bytes around an
allocation hold canaries.

## Allocation sites

With the `allocsites` tag (set by `--alloc=debug`), `new`, `make`, `alloc.New`, `alloc.NewIn`,
`alloc.Free` and `alloc.FreeIn` publish their caller's address through `rt.AllocSite` while the allocator
runs, and the debug allocator stores it with the block. The address comes from `rt.CallerPC`, which the
compiler implements as one instruction: a load of the return address, or, where the function is inlined,
the address of the inlined code. Code that calls an allocator's methods directly (`vec`, `hash`,
`strings.Builder`) reports "an unknown site".

Sites print as `pc 0x...`, the address of the call instruction. `addr2line` and `objdump` turn them into
source positions today. `rt.PrintPC` prints every program counter the allocator reports, and once a
symbolizer is registered with `rt.SetSymbolizer` (the stack trace support does this) it adds the function
and line: `pc 0x401277 in main.main (main.p:12)`.

## Settings

`PLUTO_DEBUGALLOC` is a comma-separated list; `debug.EnvOptions()` returns the options it describes.

| Setting | Option | Default | Meaning |
|---|---|---|---|
| `underflow` / `overflow` | `Underflow` | overflow | where the guard page goes |
| `canary`, `canary=N` | `CanaryBelow` | 0 | canaries for all allocations, or those under N bytes |
| `guards=N` | `MaxGuarded` | 16384 | most page-guarded blocks at once |
| `quarantine=N` | `Quarantine` | 64M | bytes of freed blocks held before reuse; N may end in K, M or G |
| `leaks=off\|report\|fail` | `Leaks` | fail | what to do with live allocations at exit |

An unknown setting stops the program at startup.

## Freestanding builds

The allocator takes its memory from `Options.Pages`, an `alloc.PageProvider`. If the provider also
implements `debug.PageProtector`,

```go
type PageProtector interface {
	ProtectPages(p unsafe.Pointer, n uintptr, access bool) bool
}
```

it gets guard pages and inaccessible freed blocks; otherwise every allocation is canary-guarded. A kernel
can call `debug.DescribeFault(addr, pc, write)` from its page fault handler: it prints the message for a
fault in one of the allocator's blocks and returns true, or returns false. `CheckHeap` and `ReportLeaks`
can be called whenever the kernel likes. Hosted builds wire up the equivalent automatically: `debug.New`
registers a fault handler with `rt.SetFaultHandler` (which catches `SIGSEGV`) and an exit hook with
`rt.SetExitHook`.

## Concurrency

The allocator is not safe for concurrent use yet. Every entry point takes the allocator's lock first
(`lock` and `unlock` in `debug.p`), which does nothing while puroutines do not run in parallel. When they
do, a single mutex there is enough: the side table, the quarantine and the block cache are all reached
only through it. The fault handler and the exit hook must take it as well.

## Cost

`bench/binarytrees` (about 70 million allocations of 16-byte nodes, up to a million live at once) runs
about 16 to 20 times slower with `--alloc=debug` than with the slab allocator, with or without `canary`:
after the first 16384 blocks it is canary-guarded either way, and its time goes on cache misses in the
side table, the quarantine and the redzones. In a loop that allocates and frees with only a few blocks
live, a canary-guarded cycle costs about 50 ns against 7 ns for the slab allocator, and a page-guarded
one costs two `mprotect` calls, a few microseconds. Memory use is at least two pages per page-guarded
block, plus the quarantine.

A normal build is unchanged: without the `allocsites` tag the site bookkeeping compiles away and the
generated code is identical.
