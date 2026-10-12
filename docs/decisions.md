# Implementation decisions

Record of decisions the implementation made under the authority granted on 2026-10-11:
the pending items in HANDOVER.md are resolved against the project goals (AGENTS.md),
recorded here, and reviewed by the user afterwards. Each decision names the sections of
the specification it changes and the work that lands them.

The goals, restated for reference: Go-familiar, not Go-compatible; no false friends
(differences from Go are visible in the source); memory and race safety without a borrow
checker; ordinary code needs no annotations or implementation knowledge; no GC and no
hidden allocation; suitable for a kernel; performance near Rust.

## D1. `errors.As` matches a counted error against a `*T` target (16.6, 16.7)

Section 16.7 says `fmt.Errorf`, `errors.Join` and the standard library error types
(`*os.PathError`, `*strconv.NumError`) are allocated as `synced[T]` objects that are
returned in an `error` and released exactly once by the count ("no code releases it by
hand", "there is no `errors.Free`"). Section 7.19 says a box is not a `*T`. Section 16.6's
matching rule ("the first error in the tree whose dynamic type is assignable to `E`")
would then make the 16.5 example, with `var perr *os.PathError` and
`errors.As(err, &perr)`, report `false`, so the chapter contradicts itself.

Decision: `errors.As` matches a counted reference in the tree against a pointer target.
When the dynamic type of an error in the tree is a counted reference `synced[T]`, `As`
reports a match for `E = *T` and stores a copy of the pointer in `*target`. Section 16.6
gets one sentence saying this, cross-referenced from 16.7. The 16.5 example works as
written. `errors.Is` needs no rule: it matches sentinels by identity and `Unwrap`,
neither of which depends on the box.

Why this and not the alternative (standard library errors as plain `*T` with a manual
`Free`, which is what the implementation does today): the counted object is the design of
16.7, and a manual free is what that section explicitly forbids, because a kernel error
path that forgets one leaks and one that frees twice corrupts. A Go programmer writing
`errors.As(err, &perr)` against a `*os.PathError` target is the familiar shape this
language promises.

Lands with: counted errors (roadmap items 11, 69; gap 22).

## D2. No task may be started during package initialization (15.2, 15.3, 19.1, 19.3; resolves G44)

Decision: a statement that starts a task (a `task` statement, `Scope.Task`,
`WaitGroup.Task`, or a library function that takes a func argument and starts a task),
reached from a package-level variable initializer or an `init` function directly or
through the functions it calls, over the whole program, is a compile error that names
the start and the initializer. The `Open (G44)` block in the specification is replaced by
this rule.

Why: initialization runs single-threaded, before any task exists. The rule that a package
variable written only during initialization needs no lock (19.3) is sound only if no task
can observe it while initialization is writing it. The alternative (allow tasks in
initialization and treat such variables as shared in the race analysis) weakens the
guarantee for every program, and a freestanding or kernel initializer has no scheduler to
run a task on anyway.

Lands with: roadmap item 100 (gap 32).

## D3. Shared metadata checks large contiguous blocks; fixed arenas stay unchecked (18.9, 18.11, abi.md 1.3)

Decision: slab allocations over 2 KiB and `arena.Chunked` chunks over 56 KiB are
allocated with their own 64 KiB-aligned metadata base and granule table before contiguous
payload. On 2026-10-12 the user explicitly chose a shared metadata directory for every
checked pointer lookup. The directory reaches the correct registered table for any
interior address without placing tables inside large payloads. The existing 16-bit tag
values and address mask are preserved. The buffer of a fixed
`arena.Arena` is exempt from the generation check, and the specification says so
(sentences in 18.11 and abi.md 1.3). Metadata readers must remain safe during concurrent
registration and reclamation; address generation history survives allocator and mapping
reuse, retires before heap generations wrap, and preserves nested parent/child validity.
Ordinary allocations through custom allocators meet the same checked contract (18.9).

Why: 18.11 requires no untagged heap or arena memory in a checked build. A fixed arena is
one user-owned buffer, and per-allocation tags for its allocations would mean the arena
suballocates through the allocator (defeating the point) or pays a tag lookup and a tag
write on every allocation. As written, abi.md 1.3 cannot be implemented for a fixed
buffer, so the honest move is a documented exception. Kernels use fixed arenas for
exactly this reason, and the exception names the cost instead of hiding it. The shared
directory avoids a second hole: an object spanning 64 KiB boundaries cannot contain
tag tables at each boundary without corrupting its contiguous payload. The user accepts
additional lookup cost, to be measured. The bit-47 flag with direct small-object checks
and permanently segregated address reservations was considered and not chosen.

Lands with: roadmap item 85 (gap 10).

## D4. Two implementation readings confirmed (17.5, 8.8)

(a) Slicing through a nil array pointer is always checked. `p[lo:hi]` computes an
address, and 17.5 requires every nil dereference to panic; the implementation checks even
where the bounds would not use the value. A sentence in 17.5 states that.

(b) An owner's sendability is decided by what it holds (8.8) under the assumption that
the filling function does not alias the allocation: no pointer to the owner's memory
outlives the fill except through the owner itself. The race analysis already assumes
this; a sentence in 8.8 states it.

Why: the code is already correct as implemented. Where the specification is silent, it
should say what the code does, so a later reader does not have to re-derive it. No code
change is expected.

Lands with: the spec fix pass.

## D5. `defer` accepts every built-in that may stand as an expression statement (12.19)

Decision: `defer` accepts `clear`, `close`, `copy`, `delete`, `panic`, `print` and
`println`, and reports E2532 for the rest. Section 12.19 already says that calls of
built-in functions are "restricted as for expression statements", and 12.4 defines
exactly that set. The implementation was narrower (only `print`, `println`, `close` and
`panic`); the implementation is fixed to match the specification. No specification change.

Why: the specification's sentence already states the rule, and 12.4's set is the line a
Go programmer expects. Forcing a function literal for `clear`, `copy` or `delete` would
be a difference from Go that the specification does not state.

Lands with: roadmap item 82 (gap 6).

## Deferred

- Linux file names that are not valid UTF-8 (open part of roadmap item 57): byte paths,
  a dedicated path type, or refusing non-UTF-8 names. This needs a design of its own, not
  a one-line decision. The `os` package keeps treating paths as strings until it is made.

## Superseded decisions

### D3: lookup at each payload address's 64 KiB base

The 2026-10-11 D3 wording assumed the existing check could reach every large allocation's
table by masking each data address to its 64 KiB base. The user replaced that assumption
on 2026-10-12 with a shared metadata directory. Contiguous objects larger than one chunk
need the later boundaries for data, so a table at every boundary cannot represent them.
Large allocation tables remain at their own metadata bases; directory lookup replaces
the address-mask lookup. The fixed-arena exemption and all generation/epoch values remain.
