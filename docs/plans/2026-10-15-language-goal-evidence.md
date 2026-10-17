# Language goal evidence plan

**Status:** methodology approved through checklist item 8 on 2026-10-15; execution
pending corrected-specification review and implementation conformance work. This is
an evidence plan, not a second language specification or a performance result.
Sections 1.1–1.4 and chapter 21 define the goals and implementation obligations;
chapters 7–8 and 18–20 define the specific lifetime, concurrency and kernel rules.
The specification supersedes any incidental behavior of a candidate fixture.

## Fixed scope

Use the following four fixture groups. Reuse existing examples and tests where they
exercise the requirement; add only the missing combinations. A fixture that fails
becomes a concrete conformance gap, not grounds to weaken a rule or expand the suite
without a demonstrated missing case. None alone proves whole-language soundness.

| Group | Scenario and result | Existing starting points | Evidence required |
|---|---|---|---|
| A: ordinary application | Read a fixed input, parse into owned containers, process with scoped tasks, return contextual errors and exit normally. Output agrees with a separately computed oracle; cleanup releases tracked application allocations. | `examples/19-owners`, `20-scoped-tasks`, `24-errors`; counted-error and normal-shutdown build tests. | Source has no routine safety directives; exact successful/error results, explicit allocation behavior, normal cleanup evidence. Existing pieces do not imply the combined scenario is verified. |
| K: kernel mechanisms | Caller-selected memory, packed/volatile hardware-facing layout, interrupt work handoff without parking, safe API over a documented unsafe implementation. | `examples/25-packed-hardware`, `10-volatile-aligned-unsafe`; `internal/build/kernel_test.go`, `kernelmode_test.go`, `kernelpanic_test.go`. | Layout and compile-time forbidden-call checks; no unrequested scheduler; memory/check metadata and failure-path requirements documented. Distinguish native code-generation checks, simulator execution and real hardware evidence. A full OS is not required. |
| L: lifetimes and boundaries | Explicit owner return versus local-address escape; ordinary arena stale access; owner-transfer alias conflict; composite and saved-receiver counted cleanup; strong cycle versus weak back-reference. | `internal/build/safe_arena*_test.go`, `counted_errors_test.go`, `normal_shutdown_region_test.go`; focused escape/move/race tests. | Accepted/rejected/runtime-failing outcomes by defining section. Observe balanced resources for acyclic cases; a deliberate tracked cycle remains a reported leak. Include checked kernel policy and explicit unchecked-mode distinction. |
| R: runtime capabilities and termination | Sequential work and direct hosted sleep; task/timer capability activation; normal task/callback drain followed by package cleanup. | `internal/build/conc_test.go`, `normal_shutdown*_test.go`; scheduler/timer tests. | Exact deterministic events or allowed-order invariants; synchronization evidence including backend retirement. Inspect linked/runtime dependencies separately from elapsed time. |

Record every result with the source revision, compiler binary revision, target/build
mode, flags, environment and exact command. Keep positive and negative cases together.
Distinguish a source inspection from an executed result. Tests run before the corrected
specification are historical evidence until checked against the updated rule.

## Performance protocol

Performance is a separate engineering acceptance record, not permission to violate
semantic conformance. No universal numerical threshold is set by this plan.

1. Start with existing `bench/strings`, `bench/maps`, `bench/binarytrees`,
   `bench/structs`, `bench/matmul` and `bench/conc` workloads. Together they cover text,
   containers, allocation, generated code and concurrency. Existing Rust counterparts
   are used where present. Map/concurrency comparisons currently described against Go
   cannot be labeled Rust comparisons without an equivalent Rust workload.
2. Inspect each selected source and harness before execution. The benchmark README
   contains historical status/commands and is not a current conformance receipt.
   Confirm identical input/problem size, algorithm and verified output. Document
   allocation-strategy, data-placement and concurrency differences instead of hiding them.
3. Record hardware/OS, compiler revisions, complete flags, target ISA, thread counts,
   allocator, lifetime/bounds-check settings and relevant runtime configuration.
   The principal Plutonium result uses the ordinary safe build. Explicit unchecked
   results, if measured, are separately labeled and never replace it.
4. Use a quiet machine, a verified warm-up and repeated measured runs. Keep every raw
   sample and failure. Use the existing five-run default initially; report median and
   spread. If noise prevents a conclusion, report that uncertainty rather than choosing
   the fastest run. Do not run compiler coverage gates alongside timing measurements.
5. Measure allocations and memory in separate instrumented runs so instrumentation
   does not contaminate timing. Separate virtual reservation, committed/resident memory,
   live allocation bytes and total allocation traffic. Record measurement limitations.
6. Preserve observable results so dead-code elimination cannot erase the workload.
   Concurrency cases require correctness/invariant checks in addition to timing;
   nanoseconds-per-operation output alone is insufficient.
7. Report per-workload results and limitations. Do not exclude failures from a broad
   success claim, turn a geometric mean into a universal claim, or weaken safety for a
   favorable ratio. Investigate measured regressions as scoped implementation work.

## Required result record

For each fixture or workload, report:

- defining specification section and goal exercised;
- source and executable revisions, reproducible command and environment;
- expected result and its independent derivation;
- observed outcome, full log location and measurement samples where applicable;
- status: verified for this case, missing behavior, incorrect behavior or unverified;
- remaining limits, including untested targets and unsafe implementation assumptions.

The release summary must keep three conclusions separate: specification consistency,
tested compiler conformance, and observed performance/usability. None implies the others.
