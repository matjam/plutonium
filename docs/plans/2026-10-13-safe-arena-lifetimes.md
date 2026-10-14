# Safe arena lifetimes

Status: D7's lifetime policy is approved on 2026-10-13. Sections 17.6, 18.9,
18.11, 18.12 and 21.5, ABI1.3 and memory-model3.8 now define the requirement.
The user subsequently approved the concrete safe/manual API split below.
This plan records the reviewed mechanism and remaining verification work. The
isolated implementation passes the all-package covered suite, every package
coverage floor, zero lint, full SSA and generation-off build suites. Tracked-only
archive verification and final review also pass. The implementation landed
as `218008141373138b78f3d2224850eaf4afbd2a19` and its installed compiler passed
the embedded-runtime hello and safe/manual API smoke checks.
It supersedes the fixed-arena and hosted epoch-wrap exemptions in D3 and the
earlier generation-directory verification scope. See the
[verification record](../verification/2026-10-13-safe-arena-lifetimes.md) for the
implementation's measured scope, landing receipt and preserved failures.

## Required behavior and preserved pre-D7 failure

Ordinary fixed and Chunked arena interfaces must prevent access to reclaimed
memory, by static proof or checks in a checked build. Reuse must never restore
an old pointer's validity. This includes interior pointers, views kept in
heap objects, count updates and dependent backing allocations. Explicit
unchecked builds and unsafe access retain their existing distinct contract.

The checked O0 compiler at `deeadf80738a` accepted this plain application
before the amendment:

```go
package main

import "pluto/arena"

var memory [4096]byte
var a = arena.New(memory[:])
type Holder struct { p *int }
var held = new(Holder)

func retain() {
    p := a.New[int]()
    *p = 7
    held.p = p
}

func main() {
    retain()
    a.Reset()
    p := a.New[int]()
    *p = 99
    println(*held.p)
}
```

That compiler printed `99\n`, exit0. The matched Chunked program panicked at the final
read and exits2. Exact commands/hash/output are preserved in the
[implementation survey](../verification/2026-10-13-implementation-state-survey.md).
The new ordinary `NewFixed` route rejects the stale access at runtime and
passes the live-prefix and sequential-reuse controls. The old `New` constructor
has been replaced by the explicitly unsafe `NewUnsafe` route. This example is
historical evidence, not a current accepted ordinary program. Rejecting all
unproved uses alone would not complete support for valid ordinary arena programs.

## Approved public API

The manual `Arena` remains copyable `{base,size,off}`. Copying it creates two
cursors over the same backing. The former `New(buf []byte)` also asked callers to stop
using a slice while retaining their existing aliases. Tagging returned arena
views cannot retroactively revoke those untagged buffer aliases. Neither
constraint is supplied by a general exclusive-loan check today.

Approved ordinary route: a movable fixed-capacity owner with private
backing obtained explicitly from its parent:

```go
type Fixed owner struct { /* private backing and lifetime state */ }
func NewFixed(capacity uintptr, parent alloc.Allocator = alloc.Default()) Fixed
```

`*Fixed` implements Allocator and has typed New/Slice, Mark/Release, Reset,
Used/Available/Cap and Drop. The constructor takes one fixed payload capacity,
does not grow or relocate payload, and releases its owned backing on Drop.
Its parent dependency stays visible to the existing lifetime analysis. Its
metadata storage and allocation costs must be documented under the explicit
constructor/parent contract. Ordinary typed allocations and reclamation do
not secretly switch allocators or allocate a new growing payload.

The implemented candidate uses one separate parent-funded bookkeeping record
per successful allocation, including unchecked mode, for exact reclamation
and mark validity. Reset/Release/latest Free return reclaimed records; Drop
also returns payload. Reads allocate nothing. Capacity excludes these records.
The native parent-count oracle must verify this documented cost and cleanup;
the constructor is not a promise of one total allocation for its lifetime.

Example ordinary use, with the allocating operation visible in source:

```go
a := arena.NewFixed(4096, alloc.Default())
p := a.New[int]()
*p = 7
a.Reset()
println(*p) // compile-time rejection or checked stale-access panic
```

The existing caller-buffer constructor must preserve its borrowed-buffer
behavior; it cannot simply acquire private backing behind the same call.
Approved manual route: keep that representation separate and rename its
constructor `NewUnsafe(buf []byte) Arena`, with actual constructor identity
included in the existing unsafe-operation classifier and audit. This spelling
is the approved API. A name prefix does not confer unsafe semantics. It must
state the backing lifetime, no concurrent/overlapping cursors, surviving alias
and manual reclamation obligations. Direct Allocator and unsafe pointer uses
already belong to the unsafe layer. No new directive is needed.

This choice provides an enforceable ordinary route and a visible low-level
buffer route without claiming that a borrowed slice has exclusive ownership.
It preserves bounded kernel/manual use. It changes the caller-buffer API's
name/classification and adds an explicitly allocating safe constructor. The
user approved this tradeoff after reviewing hardware/GPU buffer use.

Alternative: a constructor consuming genuinely exclusive owned backing plus
an explicit permanent metadata workspace. Moving a backing owner alone does
not revoke its previous heap-retained views; transfer must rekey/invalidate
them or prove them unusable. A metadata workspace must outlive every stale
reader and its address-history record, so a reclaimed frame or arena workspace
is insufficient. This alternative preserves payload allocation control but
requires a larger transfer/workspace contract; it is not selected here.

## Finite implementation mechanism and risks

Reuse the shared directory's exact-range registrations, persistent address
history and saved backing identity. Register each allocation incarnation,
invalidate exactly the reclaimed extent before cursor rewind or backing free,
and never issue an identity that could match an old view. Existing registration
machinery handles unaligned extents and byte-aligned siblings, so partial
Release must not invalidate a live prefix sharing a granule. The verification
record above supplies the implemented mechanism's native evidence.

Chunked's modulo-32,768 epoch issuance cannot meet D7. Use noncolliding
incarnations with persistent history and explicit retirement/exhaustion, or
a reviewed widened encoding. The exact encoding and panic metadata require
ABI implementation review; the amendment grants no permission for an old
tag to become live again. Fixed storage may exhaust reusable incarnations;
it cannot silently relocate payload or resume unchecked.

Marks need allocator identity and validity after reclamation, rather than
only an offset that can become numerically plausible after reset/reuse.
Count cleanup must detect a stale header before touching it. Moving an owner
must transfer one unregister/free authority and empty the source. Parent
invalidation must make every affected child stale while preserving siblings.

The current directory uses stable Mmap-backed metadata. Reusing it is not a
zero-metadata-allocation scheme. Its process infrastructure cost must be
explicit in the checked contract; a strict no-allocation target needs a
reviewed permanent workspace/budget. It must fail loudly when unavailable,
not publish an unchecked ordinary view. Kernel checked encoding is separate;
the currently documented unchecked defaults are preserved.

## Bounded acceptance matrix

Pin the selected rejection or panic outcome, at O0/O2 in checked mode, for
fixed and Chunked implementations. Separate manual/unchecked cases from
ordinary safety controls. Positive cases are required to complete the API;
blanket rejection of ordinary reuse is not completion.

| Case | Required result |
|---|---|
| Access before Reset, fresh allocation after Reset, repeated safe sequential reuse | Accepted; fresh contents and normal allocation behavior. |
| Retained pointer/slice after Reset and after same-address reuse | Rejected or panics before load/store of reclaimed bytes. |
| Prefix allocated before Mark, suffix after Mark, then Release | Prefix remains valid; suffix becomes stale; fresh suffix allocation works. |
| Legal nested marks, foreign mark and invalidated mark | Valid earlier marks work; invalid marks cannot reclaim unrelated live allocations. |
| Latest-allocation Free; Trim of spare Chunked blocks | Only reclaimed extents become stale; live siblings/chunks survive. |
| Arena/backing Drop and backing reused by another arena | Old child views fail; new allocation is valid and has one release authority. |
| Alias/helper/generic/interior/aggregate/interface transport | Valid before reclamation; stale after reclamation without provenance loss. |
| Counted strings/boxes and owner containers using arena as ordinary allocator | Normal releases before reclaim balance; stale cleanup fails before touching reused headers. |
| Last finite incarnation and next allocation/reclamation | Last supported identity works; next follows explicit exhaustion/retirement/widening behavior, never a matching stale tag. |
| Zero size, zero-length views, alignment and byte-adjacent siblings | No false access for an empty view; bounds/alignment and unaffected live sibling semantics preserved. |
| Ordinary new/make/default override/container allocation | Supported lifetime dependencies and normal ownership/drop semantics remain. |
| Manual constructor/direct or indirect raw calls | Exact selected API identities appear in unsafe audit; ordinary safe callers need no new annotation. |
| Explicit unchecked build or nochecks function | No claim of runtime stale detection; static checks remain enabled and correct-program behavior agrees. |

Complete all AGENTS verification gates against the amended sections and this
finite matrix on a stable snapshot. Old item96 reporting work was discarded
from active source after verified durable archival; see the
[cleanup record](../verification/2026-10-13-item96-cleanup.md). D6 shutdown is
not part of this amendment.
