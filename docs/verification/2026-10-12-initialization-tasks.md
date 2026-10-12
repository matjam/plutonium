# Initialization-task verification, native Linux amd64

Status: gap 32 and roadmap item 100 are complete. The two known bugs are next.

The implementation follows language sections 15.2, 15.3, 19.1 and 19.3 and the
approved decision D2. E2587 names the task start and its package initializer and
suggests starting the task from `main`. The obsolete G44 block and specification
test allowlist entry are removed; the existing normative rule is unchanged.

## Reachability and cleanup

The check runs before optimization over every loaded package in every build mode.
Its recursive call graph follows direct calls, function values, interfaces, bound
methods, returned closures, generated adapters, range iterators and yield helpers,
deferred calls, region cleanup and dynamic owner/error drops. Known exported
Plutonium implementations reached through C aliases are included. Bodyless foreign
functions retain the explicit linkage contract. A task-only walk continues past
ordinary waits, including the lock taken before `time.AfterFunc` starts its task.
Storing a callback without invoking it during initialization remains legal.

Descriptor drop candidates are filtered by the source interface; counted error
drops additionally require a counted dynamic box. Separate call provenance retains
the source initializer position for generated cleanup without altering debug
locations. Runtime helper threads remain permitted.

The tests exposed missing destruction of blank package initializer results.
Initialization now evaluates all results and commits named stores before dropping
blank owner results and releasing counted temporaries, before the next initializer.
Fresh owners from receives and owning-interface assertions follow the same rule.
The move checker still rejects moves from borrowed elements or existing package
owners. Exact runtime counts prove immediate drops and retention of named globals;
discarded strings, errors, maps, channels and strong boxes balance allocation and
release. O0/O2 tests cover indirect starts, implicit drops, imported initializers,
recursion, waits, callbacks, source attribution and all build modes. Full default
and brief diagnostics have exact oracles.

## Acceptance record

Go 1.27.1 and golangci-lint 2.14.0 on native Linux amd64. The final covered full
suite passes all packages; the build package takes 519.782 seconds with the
default ten-minute timeout and unchanged memory caps. Final lint reports zero
issues. Full SSA checks pass in 125.684 seconds, and the full generation-off build
suite passes in 67.861 seconds. Catalog, specification and full diagnostic checks pass. New source
has no TODO or placeholder, and formatting and translated-source checks pass.

Aggregate coverage increases from 47337/53876 (87.86287%) to 47462/53996
(87.89910%). Every package floor passes. Lowering increases to 94.86831%.
The unchanged token package covers one fewer statement; other unchanged packages
match the accepted baseline. Complete profiles, comparison, logs and numeric exits
are retained in `/tmp/plutonium-gap32-verification`.

The final plain full suite also passes from an archive containing only the 1808
staged tracked files (build suite 116.526 seconds). That archive builds the compiler
and runs hello with the embedded standard library and `PLUTOROOT` unset; its output
matches the example's exact oracle. Archive contents are identified by the staged
tree recorded in `archive-tree.txt`. The acceptance paragraph was added afterward.

Earlier focused failures are preserved, including previously accepted task starts,
missing blank-result destruction, and diagnostic goldens before the explanation
update. Incomplete covered profiles stopped for required cleanup or catalog fixes
are excluded. One covered run saw the previous golden while it was being updated
and is excluded. A concurrent SSA run was stopped and then rerun sequentially.
No test, timeout, cap or specification rule was weakened.
