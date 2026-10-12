# Generation-directory verification, native Linux amd64

Status: gap 10 is complete and passes native verification. Gap 32 is next.

The implementation follows language sections 18.9 and 18.11, ABI sections 1.2 and 1.3,
and memory-model section 3.8. The user approved the shared metadata directory on
2026-10-12. Fixed `arena.Arena` buffers remain explicitly unchecked.

## Safety and integration

All checked pointers use the shared directory, including small and contiguous large
Slab allocations, Chunked arenas and inferred custom allocator wrappers. Persistent
address histories prevent an old view from becoming live through address reuse;
heap generations retire before 0x8000. Reader-visible metadata stays mapped, uses
coherent version snapshots before table reads, and preserves backing incarnations.
Partial arena release invalidates only returned memory. Raw slab caches validate
their saved allocator and backing identity before reading physical links.

Ordinary runtime helpers receive checks. Narrow compiler-owned metadata/bootstrap
helpers avoid recursion. Count operations validate actual header addresses, so
zero-sized boxes retain the exact ABI allocation length. Bulk copy keeps tagged
pointers through counted drops and checks its final memory access after those calls.
Access checks cover the actual SSA address and byte extent; only equal extents at
the same address can reuse an established check. Free, reset and unknown-call
barriers still invalidate cached checks. Fresh and owned pointers establish
liveness information, never an unchecked proof of arbitrary extent bounds.

Regressions cover O0 and O2, large interior bytes across 64 KiB boundaries, stale
Free/Reset/Release/Drop, byte-aligned custom siblings, counted strings and boxes,
zero-sized boxes, bodyless/direct/interface/method-value allocators, promoted generic
methods, nested partial releases, parent reuse, heap retirement, concurrent unrelated
metadata reuse, exact range tails, prebody Free validation, and counted-copy drops
that reclaim destination storage. The concurrent test has no payload use/free race.
Intentional debug allocator invalid-memory probes keep their original diagnostic
oracles in unchecked mode; checked counterparts test the language panic contract.

## Measurements

Native AMD Ryzen 9 9900X3D, Linux x86-64, O2, CPU 0 affinity. Seven serial samples
per mode follow warmup, with alternating modes and exact checksum validation.
Setup is outside the timed loops. No heavy tests or builds run during measurements.
The prior checked compiler is isolated from a tracked `a834206a` archive.

| Workload | Prior checked ns/op | New checked ns/op | New unchecked ns/op |
|---|---:|---:|---:|
| Dense, 32 million uint64 reads | 0.809 | 16.498 | 0.794 |
| Sparse, 32 million uint64 reads | 1.548 | 19.302 | 1.284 |
| 100,000 allocation/read/free cycles | 10.398 | 86.189 | 7.699 |
| 100,000 consumed 1024-byte copy/clear cycles | 49.247 | 1503.776 | 55.344 |

The new checks have a substantial cost. These probes do not support a near-Rust
performance claim. The sparse layouts differ in stride and mapping geometry, so
that comparison also includes cache and TLB effects. The prior implementation
leaves 4096-byte growth allocations unchecked; growth timings measure added safety
as well as implementation cost.

Simultaneous live 4096-byte allocations at N=128/256/512/1024 take
0.657/1.275/2.250/4.356 ms with checks. Per-allocation cost decreases from 5135 to
4254 ns over those sizes. No quadratic trend appears in this finite probe; it is
not an asymptotic proof. A bounded eight-candidate sweep replaces the full record
scan when a metadata pool is empty. At N=1024 median VmHWM is 10,204 KiB versus
4260 KiB for the prior compiler. VmHWM includes setup and is not a metadata-only
allocation measurement.

Follow-up tuning should use one coherent registration/ancestry snapshot for a
whole range, validate same-granule scalar endpoints in one snapshot, improve
proven extent reuse, and consider interval-node lookup hints that never cache
liveness. These optimizations must preserve full bounds, ancestor and version
validation and all invalidation barriers.

Benchmark compiler SHA256:
`dc4d996b00081000e61c474f13647ab589fe3d73437e776d56dee59110cbfd90`.
Runtime directory SHA256:
`d21b3b7e680537629444164a91311f5a0635578e0282cb9031211565484f954b`.
The final compiler replaces register-set bit-index shift loops with a trailing-zero
intrinsic and intersects live registers with each allocator's register class before
edge enumeration. Ascending interference-graph iteration, adjacency order, costs
and move state are preserved. Its SHA256 is
`2c01543ef6ed35f6092d041d312ed82ad8591b1460ad7fe773b09431df64cb7c`.
All 22 checked/unchecked benchmark executables rebuilt by that compiler are
byte-for-byte identical to the measured artifacts, so the measurements above
apply to the final compiler output. The equivalence report and all build exits
are preserved in `regset-byte-equivalence.json`,
`class-filter-byte-equivalence.json` and their companion build logs.
All 210 measured runs and 30 warmups returned zero with exact checksums.
The complete report, sources, binaries, samples and provenance are retained under
`/tmp/plutonium-gap10-verification`, including earlier results superseded by the
final exact-extent reuse patch.

## Acceptance record

Final full covered `go test -count=1 -coverpkg=./internal/...` passes with the
default timeout and memory caps; the build suite takes 389.735 seconds.
`golangci-lint run ./...` reports zero issues. Full `PLUTO_SSACHECK=1` build tests
pass (121.120 seconds), as do full `PLUTO_GENCHECK=0` build tests (66.696 seconds).
The final plain `go test -count=1 ./...` passes from an archive containing only
the 1802 staged tracked files (build suite 114.465 seconds). That archive builds
the compiler and runs the embedded-runtime hello with `PLUTOROOT` unset; output
matches the golden file exactly. Installation through `GOBIN=$HOME/go/bin`
succeeds and the installed embedded-runtime hello also matches exactly.
All command exit statuses are zero and retained with their complete logs.

Earlier failures are preserved. They exposed allocator prefix overlap, raw tagged
timer pointer comparisons, stale translated sources, intentionally unchecked debug
tool oracles, and the need for exact access checks. The initial full covered run
timed out at the default ten-minute limit with interference-graph construction in
its stacks. Exact SSA address/extent reuse removes avoidable repeated checks; the
default timeout and memory caps remain unchanged. A second covered run still
timed out; its stacks again identify register-set edge enumeration. Replacing
the per-bit shift loop with a trailing-zero intrinsic removes repeated covered
loop counters while preserving exact register order and interference semantics.
All 192 single-bit positions, empty sets, high bits, word boundaries, ordering
and nonmutation are tested. A third covered timeout led to the class-intersection
optimization. An independent historical graph-construction oracle compares edges,
adjacency insertion order, degrees, costs and move state across mixed classes,
calls, loop weights and register-set growth after liveness. Benchmark rebuilding
caught an initial shorter-liveOut indexing error; restoring the original length-safe
copy followed by mask intersection fixes it and preserves the previous tail behavior.
The grown-register oracle and all benchmark rebuilds pass. No incomplete profile is
used for acceptance. An optional CPU-profile probe was killed before producing a
profile; cgroup counters show no OOM and the kernel journal has no matching OOM
event. That probe supplies no performance or acceptance evidence.

Coverage compares the complete final profile against the complete native
`a834206a` production profile at
`/tmp/plutonium-native-verification/cover-strongfield-final.out`, including every
package and every required floor. Aggregate baseline: 47,245/53,822 statements,
87.78009%. Final aggregate: 47,337/53,876 statements, 87.86287%; every floor
passes. The optimizer decreases from 94.95718% to 94.78074%, remaining above
its 90% floor. The [complete package comparison](2026-10-12-generation-directory-coverage.md)
reports every package, including changed executed counters in unchanged-source
packages, without selecting favorable reruns.
