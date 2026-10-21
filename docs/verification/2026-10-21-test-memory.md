# Memory of the heavy tests, 2026-10-21

Seven agents share one 30 GB machine, and every heavy command runs in one of
two slots of `bigjob.sh`: a systemd scope with `MemoryMax=11G`,
`MemorySwapMax=0` and `GOGC=50`. This record measures what the heavy tests of
`tools/go2pu` and `internal/build` use there, and what changed to fit them.

Measured with a wrapper that runs a command and prints its wall time, its
`ru_maxrss` (the largest single process, itself or a waited-for descendant)
and the scope's `memory.peak`, and a sampler that reads the scope's
`memory.current` and the resident size of each of its processes every two
seconds. `memory.current` includes page cache, which the kernel reclaims
before it kills anything, so the process sums are the figures that matter.

## Building the translated demos

`pluto build` of each `selfhost/demo` program alone, at the commit before
these changes (`503c0cbd`):

| demo | -O0 peak | -O0 wall | -O2 peak | -O2 wall |
|---|---|---|---|---|
| objdemo | 164 MB | 1 s | 180 MB | 2 s |
| ssademo | 204 MB | 2 s | 280 MB | 4 s |
| amd64demo | 304 MB | 3 s | 503 MB | 15 s |
| optdemo | 461 MB | 5 s | 823 MB | 25 s |
| rewrites | 161 MB | 1 s | 210 MB | 3 s |
| scopes | 133 MB | 1 s | 149 MB | 2 s |
| pudemo | 165 MB | 1 s | 192 MB | 3 s |
| ssaback | 536 MB | 6 s | 964 MB | 28 s |
| constdemo | 212 MB | 2 s | 261 MB | 4 s |
| parsecheck | 269 MB | 3 s | 371 MB | 6 s |
| closures | 159 MB | 1 s | 157 MB | 2 s |
| diagdemo | 3755 MB | 2 min 51 s | 3827 MB | 5 min 14 s |
| typecheck | 4002 MB | 3 min 1 s | 4021 MB | 4 min 33 s |
| lowerdemo | about 15 GB (AGENTS.md; does not fit in 11 GB) | | | |

Running the small demos takes 5 MB each, except constdemo: 1.2 GB at both
levels.

## Where `go test ./tools/go2pu` spent its memory

Before (`503c0cbd`), under `bigjob.sh` without `-parallel`: the out-of-memory
killer ended the scope after 2 min 24 s at 11 GB. testguard had set
`-parallel 24` and a watchdog cap of 12 GB from the machine's 18 GB of
`MemAvailable`, above the scope's limit, so the watchdog never fired.

The sampler showed where the memory was:

- The test binary itself: 1.9 to 3.5 GB (24 Go-side compiles in flight in
  TestMixedPipeline, and the demo builds of TestDifferential).
- The translated back end (`selfhost/demo/ssaback`), which TestMixedPipeline
  runs once per program, level and variant (about 640 runs), up to 24 at a
  time: 100 MB to 8.5 GB each. These child processes are what filled the
  scope; testguard's watchdog only counted the test binary.
- constdemo at 1.2 GB per level while TestDifferential runs it.

The translated back end holds what it allocates, so its peak grows faster
than its input. On a sample of 70 of the corpus's SSA texts:

| text | level | peak | bytes per byte of text |
|---|---|---|---|
| 1.2 MB (most run programs) | -O2 | 168 MB | 150 |
| 2.9 MB (examples/20-scoped-tasks) | -O2 | 552 MB | 190 |
| 4.1 MB (lib/genlib) | -O2 | 1991 MB | 515 |
| 5.5 MB (lib/goexit) | -O2 | 2608 MB | 499 |
| 6.2 MB (lib/newmake) | -O2 | 3824 MB | 650 |
| 6.9 MB (lib/vec) | -O2 | 4167 MB | 631 |
| 13.7 MB (lib/strconvfloat) | -O2 | 8488 MB | 652 |
| 4.1 MB (lib/genlib) | -O0 | 248 MB | 64 |
| 13.7 MB (lib/strconvfloat) | -O0 | 1002 MB | 77 |

The same program was also built more than once per run:

- parsecheck at -O0 and -O2 by TestDifferential and TestParseCheck;
- ssaback at -O2 by TestDifferential and TestMixedPipelineChecked;
- with `GO2PU_SLOW=1`, typecheck (4 GB) and lowerdemo (15 GB) at both levels
  by TestDifferential and again by TestTypeCheck and TestLower, all four of
  each free to run at once;
- the Go side of parsecheck, typecheck and lowerdemo by two tests each, and
  fourteen `go build` processes started together.

## What changed

All checks are the same: every test builds and compares what it did before.

1. `internal/testguard` (pushed as `29308633`) reads the cgroup v2 limits of
   the process and sizes `-parallel`, the watchdog and a reservation budget
   to the lesser of `MemAvailable` and the cgroup's headroom.
2. The watchdog counts the test binary and every process it started, and its
   report names the largest of them. When the run has its cgroup to itself
   (as under `bigjob.sh`) it counts the cgroup's usage less reclaimable file
   cache if that is more: files the tests write to `/tmp`, a tmpfs here,
   cannot be reclaimed without swap, and they and the kernel's memory had
   made the scope reach its limit with the processes at 8 GB. In a cgroup
   the cap is 95% of the headroom (80% of `MemAvailable` otherwise): nothing
   else competes there and nothing swaps, and the largest program grows by
   about 110 MB a second.
3. `testguard.Reserve` hands out the budget: free memory is the budget less
   the larger of the reservations held and the memory the run uses. A
   reservation of more than a third of the budget waits until nothing else
   is held (no smaller one starts while it waits) and for the memory in use
   to leave room (at most a minute), and then runs alone.
4. `tools/go2pu` builds each demo (by demo, level and generation checks) and
   each Go program once per test binary, one `go build` at a time; the
   builds that exceed a slot reserve their measured peak (the table above),
   constdemo's runs reserve 1.3 GB, and TestMixedPipeline reserves a slot for
   each Go-side compile and an estimate from the text's size for each run of
   the translated back end (`ssabackPeakMB`, checked against the
   measurements by TestSSAbackPeak). The Go back end's object is computed
   before the translated back end runs, so a test waiting for memory holds
   only that object.

## Results

`go test ./tools/go2pu` (without `GO2PU_SLOW`), default `-parallel`, on a
machine shared with other agents (load average about 30 on 24 CPUs):

| tree | scope | outcome | wall |
|---|---|---|---|
| `503c0cbd` (before) | 11 GB | killed by the OOM killer at 11 GB | 2 min 24 s |
| `29308633` (cgroup-aware guard), `-parallel 2` | 11 GB | passes | 23 min 22 s |
| this change | 10 GB | stopped by the watchdog: lib/strconvfloat at -O2 alone needs 8.1 to 8.9 GB in the translated back end, the test binary 1 to 1.8 GB beside it, over the 9.7 GB cap | 12 min 16 s |
| this change | 20 GB (`BIGJOB_SLOTS=2`) | passes; largest process 8.5 GB, scope peak 12.1 GB with page cache | 5 min 54 s |

The scope limit dropped from 11 GB to 10 GB during this work. In 10 GB,
the largest translated back-end run (lib/strconvfloat at -O2) cannot fit
next to the test binary with any margin. Before this change the same run
was killed by the kernel; now the watchdog stops it and names it. Until the
translated back end frees what it allocates, run `go test ./tools/go2pu`
(and so `go test ./...`) with `BIGJOB_SLOTS=2`.

`go test ./internal/build` in a 10 GB scope passes in 9 min 42 s with a
largest process of 2.5 GB and a scope peak of 3.8 GB: its memory was never
the problem (its TestMain bounds the heap with a soft limit). Its time is:
under the same load, inside `go test ./...` beside the other packages, it
passed the default 10-minute timeout once (TestBuildTest/pluto/path/filepath
at -O2 took 4 min 20 s there, 36 s alone).

## What remains

- The translated back end (`selfhost/demo/ssaback`) holds everything it
  allocates: 150 to 650 bytes for each byte of SSA text at -O2. It is what
  fills the scope, and the only reason `tools/go2pu` needs more than 10 GB.
  Freeing each function's SSA and the optimiser's per-pass state once
  emitted (in the Go source that go2pu translates, internal/opt and
  internal/backend, or in how go2pu translates their allocation) would
  bring the strconvfloat run near the -O0 figure, about 1 GB.
- Building the translated front end (lowerdemo) takes about 15 GB and the
  type checker and diagnostics demos about 4 GB each, in the Go-hosted
  compiler; the escape analysis dominates those builds (internal/escape,
  being reworked as internal/escape2).
- The test binary itself reaches 1 to 2 GB while TestMixedPipeline runs 24
  Go-side compiles at once; that is the parallelism, which the reservations
  now bound.
