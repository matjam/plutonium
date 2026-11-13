# Compiler speed

Status: in progress. Measurements as of 2026-10-28 (`90ae3e43`), the
single-threaded solver fixes listed under "Done", the rest proposed.

The self-build (`pluto build -O2 ./compiler/cmd/pluto`) takes about 19
minutes on kraken, and it runs on one core. This plan says where the time
goes, what was fixed without changing any output, and how the compiler
could use more than one core.

## Measuring

`PLUTO_PHASES=1` names each phase on standard error as it starts and gives
its time and the peak resident memory during it (the kernel's VmHWM, reset
at each phase through `/proc/self/clear_refs`); the load phase also gives
parse and type-check time, the opt phase the time of each pass summed over
the functions, and the escape phase the number of solves, their steps and
each solve of a million steps or more. `PLUTO_CPUPROFILE=file` writes a
CPU profile that `go tool pprof` reads with the compiler binary.

Comparisons are made within one kraken job, which runs at nice 19 with no
CPU quota, so wall time moves with the machine's load; CPU time and the
profile are the steadier measures.

## Where the time goes

Self-build of `90ae3e43`, unchecked (`--gencheck=false`), wall 1136s,
user 1035s, sys 100s: 1.0 core.

| phase  | wall    | peak RSS |
|--------|---------|----------|
| load   | 2.0s (parse 0.4s, types2 1.6s) | 480MB |
| escape | 840.8s  | 6154MB   |
| moves  | 1.8s    | 736MB    |
| race   | 0.7s    | 897MB    |
| lower  | 32.7s   | 6638MB   |
| opt    | 40.9s   | 1886MB   |
| emit   | 57.3s   | 2114MB   |
| write  | 0.4s    | 2509MB   |
| link   | 0.8s    | 2507MB   |

The escape analysis is 86% of the wall time (the lower phase's peak
includes the escape phase's memory still mapped). The opt passes that
matter: inline 16.7s, sroa 4.8s, mem2reg 2.8s, cse2 2.6s, cse 1.5s,
unroll 1.1s; the rest are under a second each.

Checked (`--gencheck=true`, the default for programs) the same build takes
1311s wall, 1184s user: escape 853s and lower 32s as before, opt 50s (the
gencheck pass 5.1s), and emit 173s, three times the unchecked 57s; the
rest is unchanged. The phases add up to about 980s of the 1136s; the
remainder is the CPU profiler writing its profile at exit (about three
minutes), not the compiler.

The runtime's static cleanup check (`rt.staticCleanupCheck`, inlined in
every store that may reach a static object, and in `memclr` and
`memmove`) is 28s of self time, 3% of the build, for a flag that is clear
until shutdown.

### The escape analysis

Of 976s of profile samples, `escape2.Run` is 840s, nearly all in
`solve.SolveKeep` (783s). The functions with the most self time:

| function | self | cum |
|----------|------|-----|
| solve.(*table).get | 107s | 124s |
| solve.(*solver).fire | 47s | 476s |
| domain.(*paths).Steps | 47s | |
| solve.(*solver).coveredBy | 45s | 248s |
| solve.tokOf | 40s | |
| solve.tok.words | 32s | |
| solve.(*solver).storeDst | 28s | 81s |
| rt.staticCleanupCheck | 28s | |
| solve.(*solver).holdsTok | 25s | 139s |
| domain.(*paths).throughLoad | 19s | |
| rt.memmove, rt.memclr | 19s each | |
| solve.mix | 18s | |

Three paths take most of it:

- **GE subsumption** (`addID` → `coveredIn` → `coveredBy` → `holdsTok`,
  248s). Every token added to a node is first checked against every GE or
  Deep In token that could cover it: for each prefix of its path and of its
  tag, a candidate token is built, encoded and looked up in the token
  table, and the node's bit tested. Almost every lookup fails: the
  candidate was never interned, or the node does not hold it.
- **Store pairs** (`fire`, `cStoreIn` → `addStorePair`, 284s). Each
  firing builds the destination key (`storeDst` → `off` → `extendAll`,
  81s) and, once an input's raw pairs pass `MaxRawStorePerInput`, cuts
  both keys (`summary.CutKey`, 112s) before the pair is de-duplicated. The
  same keys are built and cut over and over.
- **Hash lookups**. `table.get` is 11% of all samples on its own: the
  tables are open-addressed with three words a slot and grow to millions
  of slots, so most probes miss the cache.

The loops that run most: the self-build makes 77,917 solves of 3.8
billion constraint firings (`fire`, each a step); the 716 solves of a
million steps or more are 2.1 billion of them, and a few functions,
members of big SCCs solved again in each round until their summaries
stop changing, account for most of those:

| function | big solves | steps |
|----------|-----------:|------:|
| types2.(*unifier).nify | 78 | 569M |
| lower.(*Lowerer).mayReach | 73 | 299M |
| lower.(*Lowerer).mayHoldFunc | 63 | 219M |
| lower.(*fnState).compositeLitOf | 77 | 169M |
| types2.unsendable | 47 | 164M |
| types2.(*operand).convertibleTo | 30 | 129M |

So the cost is a few thousand steps a solve, times solves the driver's
rounds repeat. Each firing of a constraint on a token is cheap; what makes
it dear is the work around it, a hash lookup or two each (the constraint
de-duplication in `addCons`, the subsumption check in `addID`, the store
pair tables).

### The largest SCC (2026-11-13)

`PLUTO_ESCAPE2_SCC_STATS=1` prints where the steps go (sched/sccstats.p;
the output is the same with and without it). On the self-build at
86fe8280, one SCC of 1994 functions (lower 787, types2 677, eir 112,
race 111, moves 87) takes 8.09 of 8.14 billion steps and 48,883 of
62,009 solves.

The deferral edges do not glue it, although a first reading of the
edges said they did: with no deferral edges it is still 1994 functions. Interface edges and the fallback joins of deferred calls each
glue it without the other: without interface edges 1197, without both
390, static edges only 95. The fallback edges are 59,146 of its 68,535
edges, nearly all from about 16 deferred calls of func values whose
fallback is the same 327 functions, but the solves read only about 230
of them (9,316 edges read in all), and an edge not read makes no solve
run again.

Of its 44,895 re-solves (7.96 billion steps), 18,323 left the summary
unchanged; they took 5.50 billion steps, two thirds of the analysis.
Most re-solves follow a static callee's summary growing (35,327 with
static triggers only). `types2.(*unifier).nify` alone is 4.65 billion
steps: 58 re-solves of median 55 million steps, 10 of which changed its
summary.

So the lever is fewer re-solves that change nothing, while the summaries
stay byte-identical: skip a reader when what its callee gained cannot
reach anything its call sites bind (to be measured first). Resuming kept
states (incr.go) would cut more but changes summaries (drift). Scheduling
the deferral deps outside the graph gains nothing; resolving interface
calls exactly would split the SCC but changes summaries.

## Done

All in the solver's own structures (`escape2/solve`, `escape2/domain`):

- **Coverer lookups a node cannot hold.** `addID` records, per node and
  base, the paths of the GE and Deep In tokens it holds (the `wide`
  table). `coveredIn` skips every lookup at a path where the node has
  none, and all of them when it has none for the base; the paths a
  candidate would be built from are still made, in the same order, so the
  path interner's IDs do not change.
- **Memoised store destinations and cut keys.** `storeDst` and
  `summary.CutKey` are pure in their inputs and make only paths already
  made the second time; their results are kept per solve (`dsts`,
  `cuts`), and a cStoreIn firing decodes its destination token only on a
  miss.
- **Repeated cut store pairs.** Past `MaxRawStorePerInput` a firing of a
  store cut both keys before finding the cut pair already there; a raw
  pair seen before (`rawCuts`) now returns at once (`addCutPair`, 200s of
  CPU on the loaded run below, now under 10s).
- **Token decoding once per process.** `process` decodes a node's new
  tokens once, not once for each of the node's constraints.
- **One probe to add to a set.** `table.add` finds the pair or its free
  slot in one probe, where it looked the pair up and then probed again to
  put it.
- **Arena clears.** A big allocation takes a power-of-two block from the
  pool; only the words it hands out are cleared.

Measured on kraken with both compilers self-building the same tree side
by side (unchecked, `PLUTO_PHASES`, CPU profile):

| run | escape phase | user CPU | escape2.Run samples |
|-----|-------------:|---------:|--------------------:|
| `1f9d2af8` (before) | 850.7s | 1056s | |
| the first two (`d53c979c`) | 591.0s (-31%) | 786s (-26%) | |
| `1f9d2af8` (before), loaded machine | 1590s | 1473s | 1238s |
| all six, loaded machine | 1285s (-19%) | 1142s (-22%) | 916s (-26%) |

The second pair ran while kraken had a load average of 5 to 9, which
stretches both; the profiles are the steadier comparison there. The
bootstrap's stages 2 and 3 (a self-build each, without the profiler)
took 694s and 681s of CPU with all six, where a stage took about 1003s
before.

Every fix keeps the output byte for byte: the self-built compiler is the
same file, the escape digests of the corpus (`tools/escapedigest`) match,
and the goldens and the bootstrap fixpoint hold.

## Next, single-threaded

In order of expected gain for the work:

1. **Token table locality.** `table.get` misses the cache on most probes.
   Keep the interned token table's slots as two words with the value in a
   side array, or index by a 32-bit hash tag array probed first (a
   Swiss-table layout), so a failed lookup touches one cache line.
2. **Cheaper token decoding.** `tokAt` decodes two words into a 40-byte
   struct (`tokOf`, 40s) and `holdsTok` encodes it back (`words`, 32s).
   The hot paths only need the kind, base and path: read those from the
   words directly.
3. **Sorted store pairs.** Once an input's pairs are cut, the cut pairs
   could be kept per (input, destination) so that a firing whose source is
   already covered by a cut pair is dropped before the key is built.
4. **The SCC iteration count.** The parallel driver solves an SCC's dirty
   members against the previous round's summaries (Jacobi), then once
   more for the reasons. The sequential driver
   (`PLUTO_ESCAPE2_SEQUENTIAL=1`, Gauss-Seidel with each member's solver
   state kept and resumed) would need fewer solves, but on the self-build
   it passed the 24GB a kraken build slot allows during the escape phase
   and was killed, where the parallel driver peaks at 6.2GB. Kept states
   would have to be dropped past a memory budget (resumed from scratch
   when dropped) before it is worth measuring, and its result is not
   bound to be the parallel driver's: the cuts make solving depend on
   order. The parallel driver already solves only the members whose
   reads grew; what it adds is the last round, which solves every member
   of an SCC that iterated once more to record reasons. Recording the
   derivations in the round that turns out to be the last (when no
   summary grows, that round's solves are the final ones) would save a
   solve of each member of every iterated SCC, if the reasons it records
   can be shown to be the same.
5. Outside the escape analysis: the register allocator's `pick` is linear
   in the live set per call and `selectSpill` sorts every time (a heap for
   both); the scoped analysis runs four times over the same program; the
   move and race checks copy the `Info` maps. Each is seconds, not minutes.

## Parallelism

Today the compiler starts no tasks. The phases and what stands in the way
of running each on more than one core:

### Shared state

- **Scratch memory.** `compiler/internal/scratch` keeps its arena stack in
  package variables (`levels`, `depth`, `heap`). Every phase that frees
  (lowering per function, each escape solve, each emitted function) runs
  in `scratch.Do`, which pushes a level on that one stack. Two tasks in
  `scratch.Do` at once would interleave their levels. The allocator under
  it is not the problem: `alloc_linux.p` is a slab allocator with
  per-processor caches and a lock, safe from any task, and the runtime
  already has a per-task allocator (`rt.UseAllocator`, `g.alloc`) that
  `scratch.Do` installs. What has to change is that the level stack becomes
  per task: a `scratch.Stack` value the task owns, with the package
  functions working on the current task's stack.
- **The race check.** Section 19.4 lets scoped tasks partition the
  elements of a slice, but "a partition covers the storage of the
  elements, not memory that pointers in them point to": two tasks writing
  through `*ssa.Func` views of different functions conflict (E3402),
  because the check does not tell the functions apart. A per-function
  phase therefore has to hand each task its function through memory the
  check does not follow (`unsafe.Pointer` or `rt.Pinned`, section 19.4's
  "What is not checked"), in one audited place, or the language needs a
  way to say that the pointers in a slice are disjoint.
- **The panic handler.** `lower/context.p` rejects any path from the panic
  handler or an interrupt handler to a wait or a task start. The compiler's
  panic handler reaches much of the compiler through func values, so a
  task statement or a channel in code the handler can reach is an error.
  This is why the escape driver's `runTasks` is hand-written to run its
  tasks in order. Starting tasks only from a function no func value
  reaches (the build driver, through a `sync.Scope`) and keeping workers
  free of channels avoids it.
- **Read-mostly package state.** The opt and ssa packages' package
  variables are tables built once (`opTable`, the predeclared types, name
  maps); they are safe to read from many tasks once built. types2 has 34
  package variables, some of them caches written during checking, which
  have to be audited before packages are checked in parallel.

### By phase

- **escape (86%).** The parallel driver exists and is the default
  (`sched/parallel.p`): rounds of solves against frozen summaries, each
  solve's new paths in its own overlay, committed in a fixed order, so the
  result does not depend on the number of workers (the Go tests checked
  this with several worker counts). `runTasks` runs the round's tasks in
  order on one worker, for the two reasons above: the Go version's channel
  and task statement, and the global scratch stack (each solve runs in
  `scratch.Do`). With a per-task scratch stack and a `sync.Scope` started
  from the build driver, `runTasks` can run the round's tasks on
  `jobs()` workers. A solve writes only its worker's workspace, its own
  overlay and its outcome; it reads the summaries, facts and interner,
  which do not change during a round (`parFacts`, made read only, with a
  lock for what is computed lazily). The race check sees the workers'
  state as one type, so the handoff of each task's worker goes through
  the audited unsafe place. Expected gain: the largest SCC rounds have
  many dirty members; the self-build's escape phase on 8 workers should
  take a fraction of 840s, bounded by the longest chain of SCC rounds.
  Memory grows by `workerMB` (256MB) a worker.
- **lower (3%).** Per function in principle, but lowering writes the
  shared program (symbols, types, the function-value table) as it goes.
  Leave sequential.
- **opt (4%).** The passes run per function (`opt.runFuncs`); except the
  inliner, which reads callees, a pass reads and writes only its function.
  Per-function tasks need the scratch stack per task and the function
  handoff of the race check. The inliner reads other functions' bodies,
  so it needs those frozen during its pass (inline in rounds, by SCC, as
  the escape driver does).
- **emit (5%).** `backend.emitAll` compiles each function in
  `scratch.Do` and appends to one object file. The code generation of a
  function is independent; the appends are not. Run code generation per
  function in tasks into per-function buffers and append them in order:
  the object file stays byte-identical.
- **load (0.2%).** types2 per package, in dependency order, once its
  package variables are audited. Not worth it before the rest.

### Order

1. Per-task scratch stacks (`scratch.Stack`), with the package functions
   using the current task's. No behaviour change; one task.
2. The audited handoff: a small package that gives a task a `*T` through
   an `unsafe.Pointer` partition, so that the race check accepts tasks
   that each own one element's pointee. Its doc comment says why it is
   sound (each pointer is distinct and owned by one task until the scope
   ends).
3. `sched.runTasks` on `jobs()` workers through a `sync.Scope` started
   from `escape2.Run`'s caller, outside any func value the panic handler
   reaches. Check with the escape digest and the fixpoint at 1, 2 and 8
   workers.
4. Parallel code generation with an in-order append, then the opt passes
   other than inline.

Turning on parallel SSA or code generation is not a quick change: it
needs steps 1 and 2 first, and it is under a tenth of the build.
