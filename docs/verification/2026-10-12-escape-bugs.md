# Escape-bug verification, native Linux amd64

Status: the two known escape bugs in ordered step 4 are complete. Smaller items are next.

The implementation follows language sections 11.5, 18.3, 18.5, 18.6 and 18.7.
No specification rule, timeout, memory cap or allocation oracle is weakened.

## Closure result provenance

Difftest seed 362192, failing since `3265b8c`, reduced from 23767 to 517 bytes.
A closure returned a string independently of another captured closure, but a union
of every result's outer dependencies made that string borrow the enclosing region.
The original program now agrees with its Go oracle at O0 and O2. The preserved
reduction, original source and report are under
`/tmp/plutonium-escape-bugs-verification/seed/seed-362192`.

Outer dependencies now retain the result index and field path. Actual region
addresses are separate from copied outer parameter values. Returned closures,
conditional and bare named returns retain their actual dependencies; unrelated
captures no longer affect independent results. Borrowed strings from
`unsafe.String` over captured storage still cannot escape. A multi-result closure
may return its safe string while its borrowed pointer is discarded.

The common-subset `testdata/run/closurestringresult.p` has a Go-generated exact
oracle. Debug allocator runs at O0 and O2 prove that counted concatenation results
survive closure cleanup and balance their releases. Literal and parameter-string
cases also pass; string ownership and lowering are unchanged.

## Temporary box projections

`p := &mk().x` now reports E3009 when `mk` returns a fresh owner. The lifetime
belongs to the containing temporary, including fields and array elements of
counted aggregates. Loading an ordinary raw pointer from heap contents keeps the
existing runtime-generation rule. A named owner, a separately retained strong
field copy, an immediate call and a full `if` or `switch` header remain valid.

Evaluated strong-value tokens retain immutable holder metadata separately from
their heap contents. Borrowed summary effects activate that holder before walking
the projected path; counted transfers keep their independent reference. Generic
aggregate and tuple arguments therefore retain provenance without recovering an
individual argument expression. Global borrowed stores and indirect calls follow
the same effect. Re-evaluating a named counted value replaces its old holder.
Projected payload identities include the expression and field path, so sibling
fields of different dynamic types cannot change each other's interface filtering.
Generic identity forwarding does not introduce a borrowing effect.

Regression cases cover nested projections beyond the field precision bound,
arrays, dereferences, generic and indirect helpers, method values, differently
typed sibling fields, borrowed global stores and independent counted transfers.
Debug allocator checks at O0 and O2 cover owned temporaries and separately copied
shared and synced fields, with zero remaining allocations after helper cleanup.

Default and brief diagnostic oracles name the temporary and suggest keeping its
owner in a variable. Existing owner-temporary first lines are unchanged. The
previous boxed-temporary return error changes to the statement-lifetime E3009
because the old frame-lifetime explanation was incorrect. The E3009 catalog
reference is section 18.3, "Views".

## Acceptance record

Native Linux amd64 with Go 1.27.1 and golangci-lint 2.14.0:

| Gate | Result |
|---|---|
| Full covered suite | Exit 0; build package 504.838 seconds |
| Plain full suite from tracked-only archive | Exit 0; build package 130.375 seconds |
| Lint | Exit 0; 0 issues |
| Full SSA build checks | Exit 0; 135.208 seconds |
| Full generation-off build suite | Exit 0; 78.572 seconds |
| Exact default/brief diagnostics | Exit 0 |
| Focused O0/O2 runtime and leak checks | Exit 0 |
| Coverage comparison and package report | Exit 0; every floor passes |

Aggregate coverage increases from 47462/53996 (87.89910%) at `cc5104d6` to
47598/54133 (87.92788%). Escape coverage increases from 3291/3469 (94.86884%) to
3426/3606 (95.00832%). Other packages match the baseline except the unchanged token
package, which covers one additional statement. The complete package comparison
and profiles are retained in `/tmp/plutonium-escape-bugs-verification`.

The full tests include formatting, specification/catalog references and
translated-source checks. New code has no TODO or placeholder. Final acceptance
also includes plain full tests from a staged tracked-only archive of 1816 files, its compiler
build and an exact embedded-standard-library hello oracle with `PLUTOROOT` unset.
The archive logs record its tree identity, file count and numeric exits.

## Failed-run evidence

Test-first probes reproduced both original defects and the generic aggregate,
tuple and borrowed global-store bypasses before their fixes. Peer review also
exposed a projected dynamic-type cache collision and a chained generic forwarding
false positive; their negative and positive regressions pass.

The first full covered run failed only `TestInitializationUnrelatedDrop` at O0
and O2. Its initialization-reachability control used a borrowed `any` conversion
of a fresh synced temporary in `main`, which the corrected lifetime rule rightly
rejects. The fixture now keeps a named synced holder before conversion, preserving
its unrelated dynamic Drop/task control and exact output. The focused corrected
test passes. `coverage-first.log`, `coverage-first.exit` and `cover-first.out`
preserve the failed run and are excluded from acceptance. The accepted complete
rerun is `coverage-final.log`, `coverage-final.exit` and `cover-final.out`.

The previously recorded generic counted-error assertion precision limit under
roadmap item 5 remains for the later reverse compiler/specification audit.
