# Bound runtime hook Goexit verification, native Linux amd64

Status: native verified. This follow-up to roadmap item 58 implements section 17.3.
Package-variable debug allocator reporting (item 96) remains unfinished.

## Behavior and exact oracles

A scheduler's custom stack allocator can call `rt.Goexit` before creation returns a task.
Previously the caller could consume a zero result and panic "out of memory starting a task"
instead of running its remaining cleanup. Scheduler discovery now precedes Goexit analysis.
Bodyless runtime hooks contribute edges to their bound scheduler methods or the permitted late
implementations. Synthetic hook symbols retain that source identity when checking calls.
Declarations with source bodies keep their own analyzed effects. No runtime or language rule changes.

Four exact regressions run at O0 and O2 with ten-second program bounds: a custom scheduler exits
through direct and deferred task creation, a custom stack allocator exits through ordinary hosted
creation, a source body replacing Sleep preserves its exit, and bodyless Sleep propagates exits
through direct, saved and saved-deferred calls. The bound Sleep fixture replaces both permitted
implementations with different receipts. Its exact output confirms the parked implementation
selected by the program; every expected remaining defer runs and CatchGoexit reports each exit.

The original test-first custom allocator failure and earlier direct-only Sleep timeout were
recorded in the session note. The latter fixture modified a target the program did not select;
it was corrected to cover both permitted targets, without changing scheduler or sleep semantics.
Those temporary logs were lost during interruption, so they are historical evidence rather than
reviewable receipts. The production fix and allocator work remained in the primary checkout.
The independent candidate was reconstructed from baseline `a2d662f8` in a clean detached worktree.
The primary allocator work was preserved and excluded from every candidate gate.

## Acceptance record

Native Linux amd64, Go 1.27.1, golangci-lint 2.14.0. Default ten-minute test timeout and testguard
memory/parallelism limits remain unchanged. All gates ran sequentially with the production frozen.

| Gate | Result |
|---|---|
| Restored four focused regressions | Exit 0; 1.439 seconds |
| Full covered suite | Exit 0; internal/build 564.246 seconds |
| Lint | Exit 0; 0 issues |
| Full SSA build checks | Exit 0; 142.383 seconds |
| Full generation-off build suite | Exit 0; 81.072 seconds |
| Plain full suite from explicit candidate archive | Exit 0; internal/build 135.392 seconds |
| Coverage comparison and pkgcov | Exit 0; aggregate nondecrease and every package floor |
| Catalog/specification, formatter and translation checks | Exit 0 through full suites |
| Archive compiler build and awaited embedded hello | Exit 0; exact oracle with PLUTOROOT unset |

The complete candidate profile merges duplicate blocks by logical OR: 48300/54851 statements,
88.05674%. The durable baseline verification record reports 48283/54835, 88.05143%; its original
temporary raw profile is unavailable. This is an aggregate comparison against that recorded
baseline, not a newly repeated block-by-block baseline comparison. Lowering covers 10500/11065
versus recorded 10482/11049. Unchanged token source covers 398/472 versus recorded 399/472;
that one-statement variation is disclosed separately and is not attributed to this fix.
Every package passes its required floor.

The production archive includes 1895 baseline tracked paths plus the explicitly named new test
(1896 total files). Per-file SHA-256 receipts verify extraction against the candidate. Its tar
SHA-256 is `e2ae1178b92c8589b856f534ef8423c903da3db09e4b8e74eab15dd8469f6551`.
It contains neither the unfinished allocator changes nor ignored runtime inputs. Status documents
are added to the final archive after code gates; its compiler and embedded hello are checked again.
Logs, exit statuses, the complete profile, coverage counts and archive inventories are retained
under `.claude/worktrees/goexit-hooks-evidence/` in the development checkout.

## Failed-run evidence during reconstruction

Root review caught a reconstructed deferred fixture using a direct call instead of a saved call.
The in-progress covered run was stopped (exit 143); its incomplete profile is retained and never
used for acceptance. The original saved-deferred call was restored before the passing focused run.

The first complete covered attempt stopped at default memory guards: internal/build started with
661 MB available and a 529 MB cap, then reached 549 MB in TestConcurrency; tools/go2pu started with
259 MB available and a 207 MB cap, then reached 219 MB. The failed log and profile are preserved.
Read-only samples subsequently showed about 10 GB available and no concurrent heavy verification.
The unchanged full command then passed with the default guards. No cap, test or service was changed.

No specification chapter, runtime scheduling behavior or allocator retention policy changed.
