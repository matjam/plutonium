# Original item 96 keep/discard ledger

Status: root-approved cleanup executed on 2026-10-13. Applies to the original `/home/matjam/src/plutonium` checkout relative to landed HEAD `deeadf80738a31e3c579b14d63aa7f492ed2b346`, which remains unchanged. The recommendations below record the reviewed triage. Every optional production candidate is archive-only; none was carried into active code. Approved specification/decision/audit documents remain.

## Executed cleanup and verification

Durable recovery directory: `/home/matjam/.codex/recovery/plutonium-item96-2026-10-13/`.
It preserves dirty tracked/untracked bytes, the index, staged/working diffs, status and original HEAD.

- `dirty-files.tar.gz` SHA-256: `186ca033e0f6e1af787e08f624b48a482f1356ad77326fdf3c734d3e370fb367`.
- Durable `manifest.json` SHA-256: `083247b466212c31fd5ed81adf0cf5704cd5edb67afa354e5758df5502e32993`.
- Restored **21 exact tracked production/diagnostic files** to HEAD, preserving landed core cleanup.
- Removed **229 exact archived untracked source/test/fixture files**. No wildcard deletion was used. Every selected file's current hash matched the durable manifest before any mutation; an unarchived candidate or changed byte would have stopped the script.
- Exact executed path lists and hashes are in [cleanup manifest](2026-10-13-item96-cleanup-manifest.json). No reset, stash, index mutation or HEAD change occurred. No isolated arena worktree files were touched.
- Source check finds no remaining reporting intrinsic/store-token/poll/E3601 references in the restored compiler/runtime. `git diff --name-only -- internal lib` is empty; remaining work is documents only.

Minimal acceptance, with complete logs and exact exit status preserved:

| Command | Result | Evidence |
|---|---|---|
| `go build -o /tmp/plutonium-restored-main-pluto ./cmd/pluto` | Exit 0 | `/tmp/item96-cleanup-build.log`, `.exit` |
| `go test ./internal/diag -run '^(TestCatalogComplete\|TestCatalogStyle\|TestSpecReferences)$' -count=1 -v` | PASS, 0.005s, exit 0 | `/tmp/item96-cleanup-diag.log`, `.exit` |
| `PLUTO_SSACHECK=1 go test ./internal/build -run '^(TestGoexitCleanupBalance\|TestGoexitNamedResults\|TestSchedulerHookGoexit\|TestAllocatorCreationHookGoexit\|TestSourceLateHookGoexit\|TestBoundLateHookGoexit)$' -count=1 -v` | All six focused tests PASS, O0/O2 cleanup/result cases and four hooks, 2.012s, exit 0 | `/tmp/item96-cleanup-core.log`, `.exit` |
| `git diff --check` | Exit 0 | Performed after cleanup and documentation updates |

Restored-main compiler SHA-256: `533624aabb1234a3e3d3d032ef6e3c3d0372a77856cf7bfee4679fc7823a2145`.
These focused checks confirm restoration and retained core behavior. They are not a new full test/lint/coverage/SSA/generation-off/archive gate or a claim of D6/D7 implementation completion. The installed compiler was not replaced.

Exact status inventories: `/tmp/item96-original-status.txt` and `/tmp/item96-original-untracked.txt`. Recommendations are by feature family and inspected mixed hunks, not a new exhaustive proof audit.

## Decision summary

1. Keep landed `deeadf8` core task-exit/ownership fixes unchanged. They are already in HEAD, have independent native/full-gate evidence, and are not the remaining dirty reporting machinery.
2. Keep approved D6/D7 decisions, focused arena amendment/API plan, specification and implementation audit reports, and accurate historical verification records. Update stale status claims during documentation integration rather than deleting the record of failed work.
3. Remove the old package-held classification implementation, E3601 diagnostic and its closure/snapshot machinery from the active tree after archiving. D6 changes its premise; retaining these pieces would import a large dependency graph into the replacement design.
4. Do not automatically land the remaining mixed allocator and scheduler hunks. Preserve a small recovery bundle for independent alignment/provenance/fairness candidates and optional zeroing robustness, with their tests and exact evidence. Extract only a demonstrated independent fix into a separately reviewed change. No whole dirty subsystem should be retained merely because a component might later be useful.

## Keep in active source/documentation

| Paths | Recommendation and evidence |
|---|---|
| All files/hunks already committed in `deeadf8`, including core lower cleanup, staged ownership, task handoff, runtime string/channel defers, optimizer/backend repairs and standalone core tests | Keep HEAD. `docs/verification/2026-10-13-goexit-cleanup.md` records native acceptance/full gates. Dirty lower changes inspected below add reporting transactions, not independent missing core cleanup. |
| `docs/decisions.md` | Keep D6/D7 and supersession markers for D3. D6 normal shutdown is approved but implementation/design remains pending; D7 arena policy/API work is separately authorized. |
| Dirty `docs/spec/{abi.md,debugging.md,memory-model.md,language/01-introduction.md,language/17-run-time-panics.md,language/18-ownership-and-memory.md,language/21-implementation-requirements.md,language/f-library-names.md}` | Keep approved arena amendment. Later API finalization is owned by coordinator/context. Never reset these with obsolete item96 source. |
| `docs/plans/2026-10-13-safe-arena-lifetimes.md` | Keep approved policy, implementation gap and finite matrix; make API proposal/approval status match latest user decision during coordinator integration. |
| `docs/verification/2026-10-13-specification-audit.md`, `docs/verification/2026-10-13-implementation-state-survey.md` | Keep read-only audit findings and measured evidence. They do not claim dirty-tree full verification. |
| Dirty `docs/roadmap.md`, `docs/plans/2026-10-10-complete-spec.md` | Mixed documentation: keep D7 reopened arena gap and valid historical evidence; replace old item96 implementation direction with D6 pending shutdown direction. Preserve the historical classification/E3601 limit as abandoned work rather than a current ordered task. |
| `docs/verification/2026-10-13-package-memory.md` | Keep as historical/incomplete evidence, explicitly mark abandoned design after D6. Correct its assertion that PageProvider has no zeroing contract: current `alloc.p:156` says providers supply zeroed pages. Do not erase failed logs or turn scoped passes into completion claims. |
| `docs/plans/2026-10-13-custom-owner-debug-backing.md` | Preserve as historical unimplemented proposal under old reporting direction. It is not approval to build a new public Backing API. Archive/status-mark if it no longer guides D6. |

## Discard from active source after complete archive

### Compiler integration: revert remaining dirty hunks together

These tracked diffs contain no standalone core cleanup fix relative to `deeadf8`:

| File | Exact discarded purpose; restoration dependency |
|---|---|
| `internal/lower/lower.go` | `debugRootEnabled`, retention/foreign/owner-recipe caches, `lateDebugDirect`, `debugRoots()` call before entry, `emitDebugRoots()` after global pruning and its late error check. Remove reporting fields/calls together with untracked source families; ordinary preexisting error checks remain in HEAD. |
| `internal/lower/intrinsic.go` | `rt.DebugRootsEnabled` compiler intrinsic. Remove only this addition. |
| `internal/lower/call.go` | `callee.debugSyscall`, trusted raw syscall wrapping, debug mutable-call transactions, direct Drop transaction. Preserve HEAD call cleanup/actual ownership handoff. |
| `internal/lower/func.go` | `debugRootBody`, body token, transfer token stack; conditional postponement of `passGoexit` and body-End token. Restore unconditional HEAD `passGoexit`; keep HEAD pending-result cleanup. |
| `internal/lower/goexit.go` | `lateHookParked` debug selection and early-exit debug-token balancing. Restore HEAD behavior; do not remove the landed cleanup accumulator. |
| `internal/lower/sched.go` | Snapshot/creation/poll late hook entries, custom/unbound debug selection, `lateDebugDirect` path. Preserve original Sleep late hook and checks. |
| `internal/lower/stmt.go` | Statement/backedge snapshot polls, assign transaction, `debugRootCondition` folding; restore HEAD temporary cleanup comment/flow. |
| `internal/lower/{strcount.go,strscope.go,value.go}` | Store transactions, body Begin token, storeLV transaction respectively. All are reporting additions; core counted cleanup/statement ownership remains in HEAD. |

### Entire new compiler source families

All paths matching `internal/lower/debug*.go` in the saved untracked inventory are new reporting work and should leave active source as a unit. The pattern currently includes:

- `debugroots.go`, `debugrootunsupported.go`, `debugrootflow*.go`: root descriptors and flow/E3601 reachable payload classification.
- `debugrecipe*.go`: raw/backing graph, producer/release recipes, node algorithm/template matching, runtime source/ASM receipts, publication inventory, callback/context closure and unsupported detection.
- `debugsource*.go`: physical/origin/extent/global/callpoint proofs and scoped internal contracts supporting the old root mutation guard.
- `debugconstant*.go`, `debugcounter*.go`, `debugmetadatacounter*.go`, `debugpartial*.go`, `debugextent*.go`, `debugforeign*.go`, `debugformal.go`, `debugglobal*.go`, `debugslot*.go`, `debugeffects.go`, `debugsnapshot*.go`, `debugsyscall*.go`, `debugtask*.go`, `debugwriter*.go`, `debugwriters.go`, and related debug-prefixed tests.

This includes the carefully tested selected-source/ASM binding repairs: they repair authority within this discarded guard, not a separate baseline feature. Do not retain the receipts/caches alone without their consumer. Archive the complete sources/tests for reference; do not import them into arena lifetime work as a new general inference prerequisite.

### Diagnostics

- Remove dirty `DebugOwnerUnsupported` catalog entry from `internal/diag/catalog_build.go` and E3601 constant from `internal/diag/codes.go` with its emitter.
- Remove active fixture directory `cmd/pluto/testdata/diag/debug_package_owner/` (all five saved paths plus source/ASM). Its explanation starts from package owners remaining undropped at normal exit, which D6 supersedes.
- Preserve the code assignment and exact goldens in the archive/history. Do not later reuse a published diagnostic meaning casually; the cleanup ledger does not authorize a replacement E3601 meaning.

### Runtime/reporting

- Remove whole untracked `lib/pluto/alloc/debug/{rootindex.p,roots.p}`.
- Remove whole untracked runtime files: `lib/pluto/rt/debugmetadata_{linux,none}.p`, `debugrootcontainers.p`, `debugrootinterface.p`, `debugrootnodes.p`, `debugroots.p`, `debugruntime_linux.p`, `debugsnapshot.p`, `debugsnapshot_linux.p`, `debugtaskmetadata_{linux,none}.p`, `debugtaskstate.p`.
- In `lib/pluto/alloc/debug/debug.p`, discard root epochs/visits/index fields, per-record root tags, rewritten ReportLeaks capture/traversal/scratch reserve, `rootReportRow` and `printRootRecords`. Restore baseline ReportLeaks behavior. This does not choose how D6 reports after shutdown; that requires its separate specification/design.
- In `lib/pluto/rt/sched.p`, discard DebugRootsEnabled work in Switch, spawnFor and taskMain. Preserve HEAD task ownership handoff ABI and core cleanup.
- In `lib/pluto/rt/sched_linux.p`, discard debug admission/dispatch/deferred-task-free/settling hooks, all debugWorld checks and wrapper machinery. Periodic ordinary global-queue selection is a separate candidate below.
- In `lib/pluto/rt/timer_linux.p`, discard the whole added debugTimerBegin/debugRuntimeEnd gate.

### Feature tests

Discard active `internal/build/debugroot*.go`, `debugroots_test.go`, `debugsnapshot_test.go`, `debugglobaleffects_test.go`, `debugcompletion_test.go`, `debugcreation_test.go`, and `debugstoregoexit_test.go`: they assert the old package-held output or report/snapshot token invariants. The independent cleanup matrix already landed as normal-allocator core tests; do not carry activated token duplicates forward.

`debugconstructor_test.go`, `debugqueue_test.go`, and `debugsyscall_test.go` are mixed recovery files; split the independent candidates below rather than keeping entire reporting tests. No test may remain expecting nonexistent report-root machinery.

## Small recovery candidates: preserve archive, extract only with independent evidence

| Candidate and exact paths/hunks | Assessment and dependency/evidence |
|---|---|
| Bootstrap strong alignment: `lib/pluto/alloc/alloc.p` `bootstrap.Alloc` calling `alignedPages` instead of direct page request | Independent contract concern: Allocator promises requested alignment; original bootstrap ignores align. `alloczero_test.go` includes TestBootstrapAlignment with 8192 alignment, but its hostile provider family also deliberately violates provider zeroing. Split a valid zeroed provider/alignment case before retaining this fix. Current scoped 16-case pass is evidence the new code works for that fixture, not proof the original valid-provider case failed. |
| Dirty-page clearing: bootstrap, large Slab/SyncSlab, refill/grow; debug constructor/guarded payload/table clearing | Optional robustness, not a spec bug for a conforming PageProvider. `PageProvider` documentation explicitly requires zeroed pages. Sixteen hostile-provider cases passed 4.612s, and constructor controls exist. Root should decide whether defensive clearing cost is desired; default recommendation is remove from active feature recovery until that independent choice. Do not change the provider contract to justify these tests. |
| Tagged nonempty provider geometry: `genPagesNonzero`, genChunk/genLarge caller snapshots, alignedPages typed view/trims, `arena/epoch.p` original-tag header projection | Plausible independent nested-allocator lifetime/provenance improvement. Tests `allocnonzero_test.go`, `alloctrim_test.go`, `arenaepochbacking_test.go` and geometry log `/tmp/item96-geometry-native-refresh-second.log` passed 2.801s. They check original count/raw cleanup, ancestor reset, extent and tags. Need actual pre-fix failing valid-program evidence before extracting. Newly named helper is not itself a required public API, and source-proof matcher convenience is insufficient reason to keep it. |
| `genPageBacking`, `genPageRelease`, owner `genLargeHeader` and `take` teardown in `lib/pluto/alloc/gen.p` | Primarily makes physical backing/release visible to old owner recipe recognition. `allocbacking_test.go` checks ordinary/self provider free once, but no independently demonstrated baseline failure is supplied here. Recommend discard this owner scaffolding from active source. If a separate real teardown lifetime failure is established, recover its minimal saved-receiver/raw/count fix without recipe requirements. |
| Global queue fairness: periodic `pp.schedtick%61` global pop in `lib/pluto/rt/sched_linux.p findRunnable` | Independent ordinary scheduler candidate: do not mix with debugDispatch/globalFirst retry. `TestGlobalQueueSyscallProgress` in debugsyscall_test.go uses normal build and pipe-observed syscall overlap; `TestGlobalQueueFullLocal` in debugqueue_test.go includes ordinary full-local-queue control. Preserve those source portions and obtain/reference their exact original-failure/post-fix evidence before shipping. If kept, retain original one-argument findRunnable unless optional globalFirst has an independent consumer. |

The recovery bundle should contain `lib/pluto/alloc/{alloc.p,alloc_linux.p,gen.p}`, `lib/pluto/arena/epoch.p`, relevant small `lib/pluto/alloc/debug/{debug.p,table.p}` hunks, the periodic queue hunk, and six allocator tests plus the ordinary queue/syscall portions. This is an archive boundary, not approval to keep these entire dirty files active.

## Test/helper dependencies to avoid accidental retention

- `allocbacking_test.go`, `allocnonzero_test.go`, `alloctrim_test.go`, `alloczero_test.go`, and `debugconstructor_test.go` depend on `debugQueueOverlay` declared in `debugqueue_test.go`. If an independent test survives, move the small generic overlay helper into an appropriately named independent test file; do not retain debug queue/report tests to satisfy this dependency.
- `arenaepochbacking_test.go` uses existing baseline `runGenCheck`; it does not require old root reporting.
- `debugstoregoexit_test.go` requires debug task/store tokens. Its core cleanup substance has a separately landed normal counterpart, so retaining it would reintroduce the discarded runtime solely for duplicate tests.
- The raw root/E3601 tests depend on lower emitted descriptors and runtime visitors together. Keeping one test family or descriptor opcode without the others will not produce a meaningful reduced feature.

## Material review choices

1. Keep optional dirty-provider defensive zeroing despite the zeroed-provider contract and added writes? Recommend a separate robustness decision, not this abandoned feature landing.
2. Is there preserved original failing evidence for the alignment/tagged geometry/global fairness candidates under valid input contracts? If yes, recover each minimal fix and test separately; if absent, leave candidates archived until reproduced. No new probe campaign is authorized by this ledger.
3. D6 replaces automatic exit-time package-held reporting. Explicit mid-program ReportLeaks behavior remains baseline until the normal-termination design selects any change. Do not preserve old classification machinery based on a guessed future requirement.

## Approved execution sequence (completed)

1. Archived complete dirty tracked/untracked bytes, staged index and status/hash manifest; preserved approved docs and independent HEAD commit.
2. Marked old proposal/verification material historical; corrected the PageProvider contract claim.
3. Restored only the listed tracked production files to HEAD and removed only the exact archived new source/test/diagnostic families. Optional recovery candidates remain in the durable archive; no repository-wide reset occurred.
4. Checked for dangling feature references and passed the minimal build/diag/core checks above. New arena work continues only in the isolated specification-ready worktree. No optional candidate or abandoned reporting feature has been declared verified or complete.
