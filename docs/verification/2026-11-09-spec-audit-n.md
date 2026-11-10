# Audit N: closing check of the third pass (21d49ec6..dcc53d93)

Scope: the data-race rules of spec 0.4 after audit M's rulings were applied. Checkout at dcc53d93.
Files: docs/spec/language/19-concurrency.md (19.3), 20-system-considerations.md (20.9, 20.10),
12-statements.md (12.11), 17-run-time-panics.md, docs/spec/abi.md (12.2, 12.4). Recorded attacks in
docs/verification/2026-11-09-spec-rulings.md "Audit M" were not repeated.

## 1. Verdict per ruling

| Ruling | Verdict | Notes |
|---|---|---|
| M1 handler regions | Applied, consistent | 19.3 Regions (451-455, 470-479), Switching (1253-1257), 20.9 (638-640, 645-654), 20.10 (674-678) agree. Rows 110-115 match. One wording issue in Switching (N1). |
| M2 capture-name rule | Applied, consistent | 19.3 Locks and names (377-386), Part of the enclosing function (221-228), 12.11 (792) agree. Rows 109, 116, 117 match. |
| M3 entries with no returned `Do` | Applied, consistent | Lazy initialization (550-556) lists every entry kind the spec has; rows 118, 119 match. A gap in the surrounding paragraph on deferred reads (N2) predates the pass. |
| M4 map copy-out error | Applied, consistent | Maps kept in place (296-303) and rule 1 (650-651) agree; row 120 matches. |
| `owned[T]` box sentence | Applied, consistent | Locks and names (365-368). Nothing contradicts it. |
| G3 | Applied, consistent | Part of the enclosing function (214-219) and row 121 agree. One wording point (N3). |

Six of six applied and consistent. Two wording findings touch them (N1, N3).

## 2. Table rows 109-121

All thirteen rows give the right result and cite the rule that decides them. Exactly one rule decides each.

- Row 114 is right only under the inclusive reading of the Switching sentence. Under the restrictive
  reading (see N1) the region is opened before any `x86.Sti` in a plain handler and is not checked. The
  row's own note, "a region the handler opens itself", shows the intended reading. Fixing N1 removes the
  doubt; the row needs no change.
- Row 113 depends on the M1 sentence that any switching candidate ends the region. It is stated in
  Regions and in 20.10, so it is decided.
- Row 114 says "a compile error" with no code, as row 73 does for the same rule. Consistent.

No row problems.

## 3. Findings

### N1. "after `x86.Sti` or in a trap handler" reads as a restriction

- Kind: wording, with an unsound reading. Severity: low.
- Section: 19.3, Switching tasks in a non-migrating region (1253-1257): "It does start from a region
  that handler code opens itself with `x86.Cli`, `x86.SaveFlagsCli`, `rt.DisablePreemption` or an
  `rt.Lock`, after `x86.Sti` or in a trap handler".
- Problem: read literally, the clause limits the check to regions opened after `Sti` or in a trap
  handler. A region opened inside a plain handler's own body region would then not be a start.
- Example, in a `//pu:interrupt` handler, with per-CPU `ticks`:

  ```go
  fl := x86.SaveFlagsCli()
  p := &ticks
  rt.Preempt()   // no count raised, so this switches; the task may resume on CPU B
  *p = 0         // writes CPU A's copy from CPU B
  x86.RestoreFlags(fl)
  ```

  Under the restrictive reading the switch check does not start, and E2023 does not fire because the
  inner region has not ended. That is a race.
- Why only low: row 114 decides the same shape the inclusive way, and 20.9's last sentence promises "A
  switch of tasks inside the region, such as a call of `rt.Yield`, is a compile error or panics". The
  `rt.Lock` and `rt.DisablePreemption` cases are safe at run time anyway, since they raise the count.
- Resolution: "It does start from every region that handler code opens itself with `x86.Cli`,
  `x86.SaveFlagsCli`, `rt.DisablePreemption` or an `rt.Lock`, whether inside the handler body's own
  region, after `x86.Sti`, or in a trap handler."
- Rejected: dropping the clause (loses the reason trap handlers are named); a new row only (the text
  would still read two ways).
- Changes a promise: no.

### N2. A deferred read is not tied to the `Do` on exits through `rt.ExitTask` or a panic

- Kind: gap; two careful implementers build it differently, and one way accepts a race.
  Severity: medium.
- Sections:
  - 19.3 Lazy initialization (528-530): every read outside `f` "is made where a call of `o.Do` has
    returned on every path to the read".
  - 19.3 Lazy initialization (566-568): "If `f` ends its task with `rt.ExitTask` ... The Once releases
    its lock and is not done, so the next call of `o.Do` calls its own function".
  - 17.3 (196-197): every function on the stack "runs its deferred calls ... as if it returned at the
    call it is in".
  - 17.2 (149-153): a panic runs the armed deferred calls of every frame while other tasks "keep
    running".
- Problem: the Lazy initialization paths say nothing about deferred calls. A compiler that runs its path
  test at the function's normal end sees `Do` returned on every path. But on an `ExitTask` exit from
  inside `f`, the caller's frame "returns at the call it is in", which is the `Do` call. Its deferred
  calls run although no `Do` returned. The lock rule closes the same hole on purpose, in Releasing a
  lock (1051-1052): "the lock must be held at the `defer` statement and at every point after it". The
  Scope rule does too (1670-1676): a deferred call counts on every path that arms it, "the same in every
  build mode". Lazy initialization has no such sentence.
- Example:

  ```go
  var once sync.Once
  var cfg int

  func load() {
  	cfg = 1
  	if bad() { rt.ExitTask() }
  	cfg = 2
  }
  func show() { println(cfg) }

  func worker() {
  	defer show()
  	once.Do(load)
  }

  func main() { task worker(); task worker() }
  ```

  Task A runs `load`, which exits. The Once releases its lock, and the deferred `show` runs in A. At the
  same time task B's `Do` runs `load` again and writes `cfg`. A's read and B's write race. Every write is
  inside `Do`, and under the normal-end reading the read follows a returned `Do`, so E3503 does not fire.
- Panic variant: the same section says (565) "A panic in `f` stops the program (section 17.1)". In a
  hosted build 17.2 runs the deferred calls first while other tasks keep running. Whether the Once
  releases its lock during that time is not stated. If it does, the same race follows. The sentence at
  565 also reads against 17.2 as untouched text.
- Resolution: add to "After a returned `Do`": "A read in a deferred call, or in a function it calls,
  counts as made at its `defer` statement and at every point after it, as a deferred release does
  (Releasing a lock): so a `Do` counts for it only when the `Do` has returned before the `defer`
  statement." Then `defer show()` after `once.Do(load)` is accepted, and before it is E3503. Change
  565 to "A panic in `f` ends the program after the panicking task's deferred calls (section 17.2)", and
  state that the Once stays locked while they run, or rely on the new sentence, which already rejects
  the read.
- Rejected:
  - Treat an `ExitTask` or panic inside `f` as reaching no deferred call. False: 17.2 and 17.3 run them.
  - Leave it to the general path analysis. Two implementers would model the exceptional edge
    differently, and the lock rule shows the spec states such edges when it needs them.
  - Forbid `defer` in a function that calls `Do`. Too broad; ordinary code defers `Unlock` there.
- Changes a promise: no. It tightens acceptance; the race-freedom promise is kept only with it.
- Note: this text predates the third pass. M3 edited the same paragraph, and no earlier audit (K, L, M)
  combined a deferred read with `Once` and `ExitTask` or a panic.

### N3. G3 "every access of the iterator counts as made without that lock" could strip the iterator's own locks

- Kind: wording. Severity: low.
- Section: 19.3 Part of the enclosing function (214-219), row 121.
- Problem: the sentence means the caller's hold on the lock the body may release. Read alone, it also
  covers an access the iterator makes after it takes the same mutex again itself. That only rejects
  more code (an odd shape where the iterator relocks the caller's mutex), so it is not unsound.
- Resolution: "... counts as made without the caller's hold of that lock; locks the iterator takes
  itself are counted as usual."
- Rejected: no change (the reading is conservative but two implementers could differ on accepted code).
- Changes a promise: no.

## 4. Checked and dropped

M1:
- A handler callee that opens and closes its own region: `RestoreFlags` and `EnablePreemption` end only
  their own kind, so the handler body's region is unaffected. Decided.
- Nested interrupts: `Sti` ends the region; trap handlers must `Cli`; NMIs and exceptions are the
  kernel's contract (20.10). Decided.
- A per-CPU read-modify-write split across a switch: E2022 on the store. Decided.
- A view of a per-CPU variable's owned memory kept across a switch: E2023, "bounded by the region".
  Decided.
- A callee that switches only through an interface: the region flows like held locks; M1 ends it at any
  switching candidate. Decided.
- `rt.Lock` data also used by tasks: ordinary lock rule. Decided.
- Waiting rule opening says "while maskable interrupts are disabled" and later excludes handler regions
  (1221-1223): the exclusion gives its reason (E2584 is stricter, one report). Not a contradiction.

M2:
- Interface receiver (names nothing); method value `f := sc.All` (not `x.M(args)`);
  `reg.caches[i].All()` (no name); `c := sc` (a copy carries the name); generic methods (per
  instantiation); `Pair(a, b)` with aliasing (sound, both names held); a literal returned through an
  intermediate call (not written in `M`, so not mapped); a receiver reassigned in the body (recorded);
  the iterator stored in a struct field (a func value, no lock added). All decided or conservative.

M3:
- Package-level var initializers: run in the main task before the start of sharing unless `init`
  installs a handler, and `init` is an entry. Decided.
- Inserted drops and `Drop` methods: visible calls, followed as calls. Decided.
- Drop of package variables at package cleanup: runs after every task has retired (15.4), and starting a
  task then panics. No sharing. Decided.
- `sync.Scope` tasks (`s.Task`) and C callbacks (`//pu:export`) are listed entries. Decided.

M4:
- No new attack beyond row 120 and the recorded ones.

Low items:
- The body releasing a lock the iterator took: "it may not release them" and Releasing a lock decide it.

## 5. Judgment

Not closed. M1 to M4, the box sentence and G3 are applied and stated the same everywhere, and rows
109-121 are right. One medium stands: N2, a deferred read after `once.Do` that runs on an `rt.ExitTask`
exit from `f` while another task reruns `f`. It needs one sentence in Lazy initialization, modeled on
the deferred-release sentence in Releasing a lock, and a fix to the panic sentence at 565. N1 and N3 are
wording. With N2 resolved, I would call the data-race rules closed.
