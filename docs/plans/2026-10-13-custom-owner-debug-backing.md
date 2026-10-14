# Custom-owner backing in debug allocation reports

**Status:** historical, unapproved proposal for the abandoned item 96 reporting
direction. D6 supersedes its exit-time package-held premise. This proposal adds
no API or language decision; source and fixtures referred to below are preserved
in `/home/matjam/.codex/recovery/plutonium-item96-2026-10-13/dirty-files.tar.gz`,
under their original repository paths, and removed from the active checkout.
Sections 15.4 (Program execution) and 18.6 (Drops) remain authoritative until
D6's shutdown specification is settled. See the
[cleanup ledger](../verification/2026-10-13-item96-cleanup.md). Statements below
describe the archived proposal and implementation, not present support.

## The concrete boundary

Section 15.4 requires memory held by package variables to be reported separately
from leaks. Section 18.6 permits a user `Drop` method before field cleanup.
An ordinary pointer or view does not establish ownership of its allocation.

This raw owner is supported by the current source receipt:

```go
type Parcel owner struct {
	data unsafe.Pointer
	size uintptr
	heap alloc.Allocator
}
func (r *Parcel) build(a alloc.Allocator, n uintptr) {
	if r.data != nil { panic("already initialized") }
	r.heap = a
	r.data = a.Alloc(n, 8)
	r.size = n
}
func (r *Parcel) Drop() {
	if r.data != nil {
		r.heap.Free(r.data, r.size, 8)
		r.data = nil
		r.size = 0
	}
}
var retained Parcel
```

The archived raw-backing fixture `internal/build/debugrootraw_test.go`
allocates 32 bytes in this owner and a separate borrowed 8-byte allocation.
Its expected oracle puts the former in the package-held category and the latter
in the leak category. This exact runtime oracle needs a refreshed native run;
the selected source recipe is supported, but this proposal claims no new native
acceptance evidence or support for arbitrary `Drop` implementations.

The archived E3601 fixture `cmd/pluto/testdata/diag/debug_package_owner/main.p`
has the same allocation fields but uses `func (r *Parcel) Drop() { release(r) }`.
`release` is bodyless and its assembly adapter jumps to the visible `finish`,
which frees the saved pointer with the saved allocator, size, and alignment.
The implementation is valid; the compiler does not infer the adapter's complete
ownership contract. `--alloc=debug` currently rejects its live package root:

```
debug allocator reporting does not support package variable retained: ownership of main.Parcel.data is unproved
```

The same boundary applies to explicit debug allocator reporting. Normal builds,
unused owners, local-only owners, borrowed views, and nonmemory resources have
separate accepted controls. E3601 prevents incorrect classification, but does
not fulfill the reporting promise for this valid package owner.

Reasonable source patterns can be inferred, including the transparent pair
above. Arbitrary opaque release code can instead select allocations through
foreign state, callbacks, or token tables whose ownership relation is absent
from the typed object. Neither field names nor an address inside a live block
proves that relation. The issue is missing information, not a reason to abandon
inference for transparent custom types.

## An existing representation to try first

For the 32-byte, 8-aligned example, existing owned storage can make the relation
explicit without a new directive or API:

```go
type Parcel owner struct { storage vec.Vec[uint64] }
func (r *Parcel) build(a alloc.Allocator) {
	r.storage = vec.Make[uint64](4, a)
}
var retained Parcel
```

`vec.Make` explicitly allocates four words. Compiler-generated field cleanup
drops the Vec; `Slice` and `Ptr` provide borrowed views. The schema already
describes Vec backing. This proposed migration should receive its own native
contrast before being called verified.

It fits code that can leave buffer release to the owned field. It cannot retain
an opaque destructor that also frees that buffer: doing both would double-free.
Foreign release must separate nonmemory cleanup from buffer ownership, or use
the second option below. Variable alignment, foreign allocation, and opaque
collections may also need another representation.

## Two feasible options

| Option | Behavior and compatibility | Specification consequence |
|---|---|---|
| Keep an explicit support limit | Preserve bounded source receipts and existing owned-container representations. E3601 remains for unrepresented opaque backing. Existing normal programs continue to compile. | No chapter changes now; item 96 remains incomplete. Treating this limit as complete conformance would require an explicit user decision changing the reporting promise. |
| Add typed backing ownership | Offer an explicit owner handle for raw backing, with declarative compiler/runtime metadata. Compatible owners can migrate to existing containers first; foreign owners must expose the ownership transfer. | A new public API and any unsafe adoption contract require a user decision and documentation. Unchanged arbitrary opaque owners remain unsupported unless their ownership is represented. |

Recommendation: validate existing container migrations first, then evaluate a
minimal typed backing handle for the remaining opaque boundary. This preserves
ownership in types, explicit allocation, and kernel suitability without growing
an unbounded `Drop` recognizer. It does not promise support for hidden allocation
graphs that the foreign implementation never exposes.

## Minimal declarative contract to evaluate

The following is a proposed API sketch; these names are not implemented or
decided:

```go
// Backing is an owner with private identity/base/extent/alignment/allocator fields.
func NewBacking(bytes, align uintptr, heap Allocator = Default()) Backing
func (b *Backing) Pointer() unsafe.Pointer // borrowed until release
func (b *Backing) Size() uintptr
// Drop invokes the saved allocator's Free exactly once, then empties the handle.
```

For an opaque release implementation, migrate its deallocation authority to an
allocator adapter rather than leaving a second raw-freeing `Parcel.Drop`:

```go
type Parcel owner struct { storage alloc.Backing }
func (r *Parcel) build(a alloc.Allocator) {
	r.storage = alloc.NewBacking(32, 8, a)
}
// No Parcel.Drop that frees data: field cleanup is the sole release authority.

type ReleaseHeap struct { heap alloc.Allocator }
func (a *ReleaseHeap) Alloc(n, align uintptr) unsafe.Pointer {
	return a.heap.Alloc(n, align)
}
func (a *ReleaseHeap) Free(p unsafe.Pointer, n, align uintptr) {
	releaseAllocation(a.heap, p, n, align)
}
// Opaque bridge obeys the existing Allocator.Free contract and actually frees
// p through heap with n and align. Its ABI implementation is supplied separately.
//pu:noescape
func releaseAllocation(heap alloc.Allocator, p unsafe.Pointer, n, align uintptr)

var releases ReleaseHeap // stable allocator receiver outlives retained storage
var retained Parcel
func main() {
	releases.heap = alloc.Default()
	retained.build(&releases)
}
```

The handle owns the buffer; the adapter supplies its release operation. The old
opaque `release(*Parcel)` must be replaced or split: resource-only cleanup may
remain in `Parcel.Drop`, but cannot also free `storage`. Acquiring `NewBacking`
explicitly calls `Alloc` once. Moving the handle does not allocate or call an
allocator. Destruction calls the saved `Free` once outside snapshot traversal.
No additional handle allocation is permitted. Raw address, original count,
alignment, allocator dependency, and incarnation remain in private handle
metadata, independent of what a later callback reads from the Parcel.

The handle would be an owner
containing the original allocation identity, base, extent, alignment, generation,
and release allocator or capability. Its compiler/runtime schema states that it
owns this allocation; ordinary pointers and views contain no such capability.
Release runs on normal destruction, not during classification. A foreign object
with several owned allocations supplies several handles in existing owned
containers, rather than a callback that discovers them during reporting.

A safe constructor performs an explicit allocation and obtains the corresponding
ownership capability. Adopting existing raw storage requires a real ownership
transfer. An address or range check alone cannot prove that transfer. A bridge
from foreign raw pointers would therefore require an explicit unsafe contract,
with misuse outside its guarantees; it must not look like a safe conversion
from a borrowed pointer. Static field annotations asserting that any pointer is
owned would leave this same trust problem unresolved.

The collector reads fixed typed slots and checks the existing allocation record
and incarnation. It does not call user visitors, `Drop`, allocation methods, or
release callbacks under the snapshot barrier. A visitor is a different, explicitly
unsafe/trusted design: it reintroduces mutation, waiting, reentrancy, and opaque
effects while traversing roots. It is not the recommended default.

Handle storage should be inline, with static descriptors and existing allocation
bookkeeping. No separate hidden allocation is acceptable. If an implementation
needs additional storage, construction or reservation must expose that operation.
Mutation, moves, retirement, and release must use the existing store/drop and
generation protocols; the proposal does not waive callbacks' effects or promise
that the current runtime integration already implements this API.

Compiler/runtime internal contracts for exact selected library bindings are
already authorized and separate from this public ownership contract. Recognizing
an internal primitive does not authorize a user's named `Drop` or visitor.
Implementation would add the public library type/constructors and its static
compiler/runtime descriptor contract, with any required ABI extension documented
separately. None is a new directive. Unsafe adoption and any restriction on
debug-reportable user owners are separate user-facing specification decisions.
Adding this handle alone does not make unchanged valid opaque owners fulfill
the current universal reporting promise: they must migrate or expose their
ownership, or the user must explicitly narrow that promise. This is an
architectural conformance boundary, not merely a remaining library checklist.

## Validation before any completion claim

Compare transparent raw Parcel, opaque Parcel, and the Vec migration at O0/O2
with exact held/leak counts and bytes. Keep normal/local/unused/resource controls.
For a new handle, test borrowed views, interior pointers, wrong allocator/extent,
duplicate adoption, moves, reuse with a new incarnation, aggregate/container
payloads, and complete release including callback `Goexit`. Count allocations
to verify that handle storage introduces none. Reporting must invoke no user
callback; independent mutation and allocator callbacks still participate in
snapshot synchronization. Keep exact CLI diagnostics for unsupported graphs.

The user decision is whether to keep this explicit reporting limitation or pursue
the typed ownership API. Neither option silently changes sections 15.4 or 18.6.
