# Kernel lifetime protection (roadmap item 101)

**Status:** approved 2026-10-28 (decision D41; section 6). Phases 0, 1 and 2
are done (section 7). It adds no language rule. Sections
1.2 (Design principles), 18.11 (Generation checks), 18.12 (Safe and unsafe
code), 20.5, 20.6, 20.11 and 21.5 (Kernel and freestanding protection) define
the subject; `docs/spec/abi.md` section 1.3 defines the encoding, canonical XOR since phase 1,
and decision D8 (2026-10-15) withdrew the unchecked kernel default. This plan
supersedes nothing. It records the chosen representation (C1) and phases;
section 6 records the owner's answers to its questions.

## 1. What the specification requires

### Promised

- **Same guarantee in every ordinary build.** Section 1.2, principles 1, 2
  and 4: a lifetime fact is proved statically, else protected by an
  automatic runtime check, else the program is rejected. Checks are on by
  default, kernel and freestanding builds included, and a bounded check that
  fails does so loudly.
- **Unchecked execution is a choice, not a mode side effect.** Sections
  18.11, 18.12 and 21.2: protection is omitted only through the explicit
  unchecked build mode or `//pu:nochecks` on a function. Selecting
  `--mode kernel` does not select it. Section 21.2 forbids silent failure.
- **The opt-out stays coarse.** Section 21.5: the unchecked mode is per
  build and `//pu:nochecks` per function. A routine remedy for a diagnostic
  must not be a safety-disabling annotation.
- **No excuse from the encoding.** Section 21.5: an implementation may not
  accept unprotected access because its hosted encoding cannot represent
  kernel addresses.
- **Context constraints apply to the checks themselves.** Sections 20.5,
  20.6, 20.11 and 21.5: there is no default allocator before
  `alloc.SetDefault`; a package initializer that allocates is an error in
  freestanding builds; nothing reachable from an interrupt handler may wait.
  Checks in early boot and interrupt code obey the same allocation, waiting
  and failure rules.
- **Pointer width.** Section 20.5: a pointer is 64 bits on both targets.
- **Allocator contract.** Section 18.9: custom allocators and the Fixed and
  Chunked arenas take part in checking through the checked allocator
  contract, in every mode that checks.

### Left open

- **Representation.** Section 21.5 leaves the kernel checking representation
  to the implementation. D8 says no new directive or pointer encoding is
  chosen.
- **Hardware.** No hardware tagging API or paging-mode requirement is
  defined. The specification neither requires nor forbids LAM, LA57 or a
  particular kernel address layout.
- **Kernel runtime hooks.** No hook is defined for where checking metadata
  comes from in a kernel.
- **Failure route.** Which panic path a failed check takes before a panic
  handler is installed is left open (section 17.1 covers hosted builds).

## 2. Today's state

### What hosted builds check

With `gencheck` on (the hosted default, `genCheckDefault`):

- Slab objects carry a heap generation (`alloc/gen.p`, `GenTagHeap` and
  `GenFreeHeap`); large blocks register a range of their own.
- Arena lifetime records carry an epoch (`GenTagArena`, `GenTrimArena`,
  `GenArenaExhausted`); Chunked arenas tag each chunk (`arena/epoch.p`);
  Fixed arenas check frees against their records (`GenCheckArenaFree`).
- Counted values check their incarnation through `rt.checkLive`.
- Every dereference of a possibly heap pointer is guarded by
  `tag := p>>48; if tag != 0 && !registeredLive(p&mask48, tag) { panicStale(p) }`
  (abi.md 1.3). Lowering (`lower/gencheck.p`) untags by zero extension
  (`shl 16; shr 16`); converting to `uintptr` or passing to extern or
  assembly code strips the tag; `memmove` and `memclr` operands are checked
  at the call.
- `opt.GenCheck`, `opt.GenFrees` and `genrange.p` remove redundant checks,
  hoist loop checks and place range checks before counted loops. These are
  optimizer results only; acceptance never depends on them (section 21.3).

### Which of those depend on hosted facilities

- **Metadata pages.** `genMetadata` gets directory pages from `rt.Mmap`,
  pools them, and never frees them. There is no kernel source.
- **Encoding.** Tags live in bits 63..48 and untagging zero-extends. A kernel
  higher-half address (`0xFFFFFFFF80000000` and up, where the
  `testdata/kernel/sched` heap sits) would untag to a noncanonical address,
  and its top 16 bits already read as a nonzero "tag".
- **Large blocks.** Hosted large blocks come from `mmapPages`; freestanding
  slabs use the kernel's `alloc.PageProvider` instead.
- **Not hosted-specific.** The writer lock `genWriter` is an `rt.Lock`, and
  `lock_kernel.p` already disables interrupts while spinning; lookups only
  read. The directory indexes 48-bit addresses with no user-half assumption.

### What kernel and freestanding builds drop

`build.p` `genCheck()` returns false whenever `Freestanding` or `Kernel` is
set. `Check()` rejects `--gencheck` there with "--gencheck is for hosted
builds only: freestanding and kernel builds have no generation tags"
(covered by `tests/build_gencheck_test.go`), and `--gencheck=false` is
accepted without effect. Without the `gencheck` build tag the runtime links
the `_off` stubs: `GenRegisteredLive` returns true, `NewEpoch` returns 0 and
`checkLive` does nothing. So a kernel build has no heap generation checks,
no arena epoch checks and no incarnation checks. Fixed-arena bookkeeping
records are still kept. Every place listed in section 4 as "left to runtime"
is accepted unprotected, which is the gap section 21.5 names.

### Probe

`probe101/stale` (not committed) is section 18.11's example in a kernel
program: `first := &ix.words.Slice()[0]` through an `*Index` parameter,
then 100 pushes, then `*first`. `probe101/hosted` is its hosted twin. Built
with the release compiler on 2026-10-28:

| Build | Result |
| --- | --- |
| hosted, default | panics: "use of freed memory: pointer ... has an old generation" at `main.stale` |
| hosted, `--gencheck=false` | prints `stale read 140204349219776` (freed memory read) |
| `--mode kernel --gencheck` | rejected: "--gencheck is for hosted builds only" |
| `--mode freestanding --gencheck` | rejected, same message |
| `--mode kernel --gencheck=false` | builds |
| `--mode kernel`, default | builds; under QEMU prints `stale read -2146328544` and exits normally |

The default kernel build reads freed memory with no diagnostic, which is
the same result as the explicit unchecked hosted build. Its listing has one
line that matches `GenRegistered` or `panicStale`, against 2,253 in the
hosted listing.

## 3. Candidate representations

### A. Side-table generations without pointer bits

Keep a liveness table keyed by address and check that the target is live.

- **Cost.** One table lookup per check, the same order as today.
- **Risk.** It cannot tell an old pointer from a new one after the address
  is reused, so it is unsound unless memory is never reused. Recording the
  expected generation per pointer slot instead (a shadow of every pointer
  location) is a hidden fat pointer with a second memory system that every
  store, copy, `memmove` and C call must maintain.
- **Needs.** Metadata pages, plus either no reuse or full shadow memory.
- **Verdict.** Rejected as the main scheme. Liveness alone is still useful
  for frees and resets (it is what `GenCheckArenaFree` does).

### B. Fat pointers, or a generation word next to the pointer

Pointers become 128 bits, or every pointer-holding slot gets a neighbour.

- **Cost.** Doubles pointer storage and register pressure; atomics on
  pointers become 128-bit compare-and-swap.
- **Risk.** Breaks the 64-bit pointer promise of section 20.5, changes
  `unsafe.Sizeof` and layout between modes, and breaks the C ABI, packed
  structures, device-visible layouts and page tables.
- **Needs.** A second layout and ABI for kernel builds.
- **Verdict.** Rejected.

### C. High-bit tags under a kernel layout of our choosing

Keep 16-bit tags in bits 63..48, but change the encoding so it works in both
halves. Two variants:

- **C1, canonical XOR (recommended).** For an address `a` canonical in
  either half, a tagged pointer is `a XOR (tag << 48)`. The tag is
  `(p XOR sext48(p)) >> 48`, and untagging is sign extension from bit 47
  (`shl 16; sar 16`). For lower-half addresses this is bit for bit today's
  encoding. Tag 0 still means "canonical, unchecked" in both halves, so
  stack, static, device and `uintptr`-derived pointers keep working. A
  tagged pointer is always noncanonical, so a dereference that skips the
  untag raises #GP instead of reaching memory.
- **C2, kernel heap in a fixed window.** Place the kernel heap in a chosen
  region and tag only there. Simpler arithmetic, but it ties the language
  to one layout and still needs C1's sign extension for high-half heaps.
  C1 subsumes it.

For C1:

- **Cost.** Same check sequence as hosted, with `sar` in place of `shr`
  and one extra XOR to extract the tag. Metadata is about 25% of tagged
  memory at the current granule (an 8 KiB history and an 8 KiB table per
  64 KiB interval), allocated once and never freed.
- **Risk.**
  - It needs 4-level paging. Under LA57 only 7 tag bits are free, which is
    not enough for today's generation and epoch ranges.
  - Generations never wrap; a range is retired before 0x8000. Retirement
    consumes virtual addresses, so a heap that is a direct map of physical
    memory would retire physical memory. The kernel heap needs a virtual
    window that can be remapped.
  - Runtime code that builds tags with `bits>>48<<48` or ORs tags in must
    change to the XOR form.
  - A missed untag is a #GP, not a silent read, so failures stay loud.
- **Needs from the target.**
  - 4-level paging, or a check at boot that refuses LA57.
  - A metadata page source before the first tagged allocation.
  - Interrupt-safe locking, which `lock_kernel.p` already gives.
- **Context fit.** A check only reads metadata: it never allocates or
  waits, and a failure panics, so it is legal in interrupt code.
  Registration happens inside allocation, which is already illegal before
  `alloc.SetDefault` and already forbidden from interrupt handlers when it
  could wait.
- **Later.** Intel LAM_SUP (or AMD UAI) could make hardware ignore the tag
  bits and drop the untag. This is an optimization, not a requirement.

### D. Static proof only, rejecting the rest

Accept a kernel program only where escape2 and moves prove every access.

- **Cost.** None at run time.
- **Risk.** Section 4 shows that almost all heap access through a pointer
  is left to runtime checks today. `ix.words.Slice()[0]` through an
  `*Index` parameter is enough. Static-only rejection would reject nearly
  every kernel that uses the heap, and section 21.5 forbids making
  `//pu:nochecks` the routine remedy.
- **Needs.** Nothing from the target.
- **Verdict.** Rejected as the scheme. It is the right floor for the
  contexts the runtime cannot protect (phase 4).

## 4. What escape2 and moves prove today

Both analyses are mode independent and do not consult `opt.GenCheck`.

- **Proved.**
  - escape2 (section 18.3): frame and static views cannot outlive their
    storage.
  - moves (`moves/views.p`): E3313 rejects views of local owners after a
    move, drop or reassignment, and views into a local arena after `Reset`
    or `Release` (the Mark analysis).
  - Boxes held in locals are box views and are treated as frame pointers.
- **Left to runtime checks.** These sites defer to generation checks by
  design and are accepted unprotected in kernel builds today:
  - moves: a `view.inHeap` stored where the walk cannot follow it;
  - escape2 `boxview.p`: a box reached through a pointer, a package
    variable, heap memory or a caller's region;
  - escape2 `errorHolder` and `boxHolder` returning 0;
  - `solve`: `cHeap` sinks, that is arena views stored in heap memory;
  - `asCopies`: heap memory reached through a pointer;
  - `contract.p`: the custom allocator generation hooks;
  - and generally every dereference of a pointer that may point to the
    heap.

## 5. Recommendation and phases

Adopt C1 (canonical XOR tags) as the one encoding for every target, with the
kernel supplying metadata pages, and use D only for contexts where a check
cannot run. Keep `--gencheck=false` and `//pu:nochecks` as the only ways to
run unchecked, and make kernel builds checked by default once C1 is in. Until
then a kernel build must fail rather than run unprotected.

### Phase 0: stop the silent drop

- Change `genCheck()` so kernel and freestanding builds are not unchecked
  by default. Until phase 2 lands, a kernel or freestanding build without
  `--gencheck=false` is rejected with a diagnostic that names the missing
  support and the explicit opt-out.
- Update `tests/build_gencheck_test.go`; keep the testdata/kernel programs
  building by passing `--gencheck=false` explicitly, each with a comment
  pointing at this plan.
- **Tests.** Rejected: `--mode kernel` and `--mode freestanding` with no
  flag. Unchecked: the same builds with `--gencheck=false` link the `_off`
  runtime, as a listing check (no `GenRegisteredLive` calls).

### Phase 1: canonical XOR encoding

- Lowering: untag by sign extension; tag extraction by XOR with the
  sign-extended form. opt's rules are unchanged; the recognised untag
  pattern changes.
- Runtime: replace shift-and-OR tag construction in `gen_*_on.p`,
  `epoch_on.p`, `alloc/gen.p` and the arenas with the XOR form.
- abi.md 1.3: describe the encoding for both halves (lower-half bits are
  unchanged).
- **Tests.** Hosted results identical (full corpus and runtime tests with
  gencheck on). New unit tests for tag and untag of high-half addresses.
  A hosted stale-view program still panics with "use of freed memory".

### Phase 2: kernel metadata and checked kernel builds

- Add a kernel metadata page source used by `genMetadata` in place of
  `rt.Mmap` (open question 2 picks its shape). Directory pages come from it
  before the first registration.
- A boot-time check refuses to run checked code under LA57 (open question 1).
- Enable `gencheck` for kernel builds; drop the phase 0 rejection for
  builds that provide the source.
- **Tests.** QEMU programs in `testdata/kernel`:
  - the section 18.11 example (a view through `*Index` after the vector
    grows) panics through the `//pu:panic` handler with a stale-use
    report;
  - the sched test heap in the top 2 GiB registers and checks high-half
    addresses;
  - a check in an interrupt handler on a live object succeeds and does not
    take `genWriter`;
  - a missing metadata source fails as question 3 decides.

### Phase 3: arenas and custom allocators in kernel builds

- Epoch tags for Fixed and Chunked arenas and the custom allocator
  contract hooks run in kernel builds; `NewEpoch` exhaustion panics as in
  hosted builds.
- **Tests.** QEMU programs for a view into a reset Chunked arena, a stale
  Fixed-arena free, and a custom allocator that reuses an address.

### Phase 4: rejection where no check can run, elision where proved

- Source-level rejection, decided before optimization, for the contexts in
  which a check cannot run: tagged access reachable before a metadata
  source exists (package initializers in freestanding builds already
  cannot allocate), and access reachable from code that must not panic if
  the owner decides such contexts exist.
- Static elision where escape2 or moves already prove the access, so the
  check is never emitted. This is a cost change only.
- **Tests.** Corpus diagnostic cases for each rejection; listing checks that
  proved accesses carry no check; the same programs under `--gencheck=false`
  and `//pu:nochecks` compile and run with no checks, which is how
  deliberate unchecked execution stays visible: a build flag or a function
  directive, never kernel mode.

## 6. Decisions (owner, 2026-10-28)

The owner answered the open questions on 2026-10-28. They are recorded as
decision D41 in `docs/decisions.md`.

1. **Paging mode.** Checked kernels require 4-level paging. Under LA57 a
   checked kernel refuses to start, with a message that says why.
2. **Kernel hook.** The kernel supplies the metadata pages through a hook
   bound at link time, as `//pu:scheduler` binds the scheduler, not through
   a run-time setter such as `rt.SetMetadataPages`.
3. **Missing hook.** A checked kernel build with no hook is a link or build
   error, not a panic at the first tagged allocation.
4. **Opt-outs.** `--gencheck=false` and `//pu:nochecks` stay the only
   opt-outs. Kernels get no named unchecked region.
5. **Heap layout.** A checked kernel heap must live in a remappable virtual
   window. About 25% metadata overhead is accepted for now; phase 2
   measures it.
6. **Hosted ABI.** The untag rule of abi.md section 1.3 becomes sign
   extension from bit 47 on every target. Lower-half bits are unchanged.
7. **Phase 0 breakage.** Accepted. Kernel and freestanding builds must pass
   `--gencheck=false` until phase 2. The error says so and points to
   section 18.11 and roadmap item 101.

## 7. Progress

- **Phase 0: done.** A kernel or freestanding build that leaves `--gencheck`
  at its default is rejected; `--gencheck=false` builds it unchecked, as
  before; `--gencheck` (true) stays rejected until phase 2. Every kernel and
  freestanding build in the repository passes `--gencheck=false`.
- **Phase 1: done.** Hosted builds use the canonical XOR encoding (C1):
  lowering and opt untag by sign extension from bit 47 and test for a tag
  by comparing a pointer with its untagged form; the runtime builds and
  reads tags in the XOR form. Hosted results are unchanged. Measured on
  kraken against phase 0, building the checked compiler
  (`-O2 --gencheck`): code size (text, rodata, data) 53,258,566 to
  53,285,974 bytes (+0.05%; .text +0.17%, .rodata -0.3%); the checked
  compiler then builds a small checked program in 24.43 s instead of
  23.32 s (mean of three, +4.8%), and that program's text shrinks from
  228,210 to 225,786 bytes. The slowdown is the sign-extending untag
  (shift left, arithmetic shift right) where a mask was one AND. Phase 2
  measures kernels.
- **Phase 2: done** (58fcd043). Freestanding and kernel builds are checked by default,
  and `PLUTO_GENCHECK=0` and `--gencheck=false` build them unchecked.
  - *Hook (question 2).* The program exports a function as
    `pluto_gen_metadata`, `func(n uintptr) uintptr`, with the existing
    `//pu:export`. The runtime of a checked freestanding build declares that
    symbol with `//pu:extern` (`lib/pluto/rt/gen_pages_none.p`) and takes
    every directory leaf, interval node and tag table from it, zeroing what
    it gets; hosted builds keep `rt.Mmap` (`gen_pages_linux.p`). The linker
    binds the two, so no directive or run-time setter is added. A
    `//pu:scheduler` method was not used because a kernel need not have a
    scheduler.
  - *Missing hook (question 3).* Lowering rejects a checked freestanding
    build that exports no `pluto_gen_metadata`, or one with another
    signature, with E2588 (NoGenMetadata).
  - *Paging (question 1).* An `rt` initializer reads CR4.LA57 when it runs
    in ring 0 and panics with a message that says to build with
    `--gencheck=false` under 5-level paging. A kernel-mode program running
    in ring 3 skips the check.
  - *Upper half.* The directory indexes its top level with address bits
    47..32 masked to 16 bits, so a canonical upper-half address uses entries
    0x8000 and up; range checks test that a range does not cross bit 47
    instead of comparing with 2^48.
  - *Tests.* `TestKernelGenCheck` (`testdata/kernel/gencheck`) boots a
    checked kernel whose heap is in the top 2 GiB: live accesses pass,
    including in an interrupt handler raised inside the metadata source
    while the runtime holds `genWriter`, and section 18.11's example panics
    with "use of freed memory" through the `//pu:panic` handler.
    `TestKernelScheduler` runs `testdata/kernel/sched` checked and
    unchecked. `TestGenCheckNonHosted` checks E2588 and the unchecked
    builds. The LA57 refusal has no QEMU test: the boot code sets up 4-level
    paging only.
  - *Measured* on `testdata/kernel/sched` at `-O2`: text grows from 274,610
    to 429,301 bytes (+56%) and bss from 12,613,800 to 13,401,056 bytes
    (+787 KiB, mostly the 512 KiB directory top level). At the end of the
    run the kernel has taken 2,740,224 bytes of heap pages and its source
    has handed out 983,040 bytes of metadata, of which one 512 KiB leaf is
    fixed and the rest (448 KiB) is about 17% of the heap pages.
