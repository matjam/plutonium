# Plutonium implementation survey

Source: `/home/matjam/src/plutonium/.claude/worktrees/coord` (checkout of origin/main). Read only. Nothing was built, run or edited.
Date of survey: 2026-11-09. The roadmap header says "Updated 2026-10-27"; the newest document is the item 20 audit of 2026-10-31.

How to read the sizes. Small: one focused change. Medium: several files or a new analysis. Large: a new subsystem.

## 1. Roadmap

I read all 1075 lines of `docs/roadmap.md`. Items 102 to 145, the "Wrong code" items 1 to 5, items 73 to 100 and the kernel items 6 to 13, 53 to 55, 58, 59, 61, 62, 64 to 70, 71, 72, 74, 75, 77 to 83, 84 to 92, 94, 97, 99, 100, 128 to 136, 139 to 145 are done with no open text. Items 28, 95 and 98 are "decided" with nothing to build. Item 96 is a superseded direction (D6 replaced it) and its replacement is landed.

### 1.1 Items not fully done

| Item | Status in roadmap | What remains | Spec | Size |
|---|---|---|---|---|
| 101 Ordinary kernel lifetime protection | in progress | Phases 3 and 4 of `docs/plans/2026-10-28-kernel-lifetime-protection.md`. Phase 3: epoch tags for Fixed and Chunked arenas and the custom-allocator hooks in kernel builds, with QEMU tests (view into a reset Chunked arena, stale Fixed free, allocator that reuses an address). Phase 4: source-level rejection where no check can run (tagged access reachable before a metadata source exists), and static elision where escape2 or moves already prove the access. Phases 0, 1, 2 are done (`58fcd043`). | 1.2, 18.11, 21.5 | large (two medium phases) |
| 20 Ownership: owners, moves and views | in progress | (a) A consuming method used as a method value (`done := c.Close`) must be a compile error; `compiler/internal/moves/expr.p` line 188 moves the receiver into the method value and reports nothing. Needs an error code, catalog entry, tests. (b) Item 101 phases 3 and 4. (c) The four view-tracking limits at the end of item 138. (d) Missing test cases, listed in section 2.3 and in the audit (`docs/verification/2026-10-31-item20-audit.md`). | 7.18, 7.19, 11.9, 18.3, 18.5 | (a) small, (c) medium, (d) small, whole item medium |
| 75 (G26), part of item 20 | marked done, with "Open" inside | Same as 20(a). The item says "done" and "Open: a consuming method used as a method value is an error". | 11.9, 18.5 | small |
| 138 Views of local owners | marked done, with "Open" inside | Four cases accepted today that section 18.3 says are E3314 or need the run-time check: heap or caller memory returned by a method of a local owner and stored into unrelated memory; a pointer to caller memory loses its tie to the caller when the owner moves; a view kept in a local owner that is moved and then stored in a package variable; a view of a table that a map walk retired outliving the walk. | 18.3 | medium |
| 18 / gap 37 Bounded task-stack inference | header says done; gap table row 37 says planned; a paragraph after item 145 says "Next, finish item 18" | The bound is proven only for `--gencheck=false` builds. Checked builds stay "not computed" because `genLive` follows the parent chain recursively. Kernel, freestanding and custom-scheduler builds compute no bounds. The paragraph also names "small unverified witness pairs in the resolution record" (`docs/verification/2026-10-15-spec-consistency.md`) still to fill. | 19.1 | medium |
| 24 Compile-time type-switch folding | planned | Decide `switch any(x).(type)` on a type parameter per instantiation. An optimization; acceptance does not depend on it. | 12.9, 21.3 | medium |
| 32 Differential testing | in progress | `tools/difftest` (Go code) generates programs in the Go and Plutonium common subset and compares Plutonium at -O0 and -O2 with Go. The "full difftest campaign with every feature enabled" in the 2026-10-12 plan is not recorded as done. It needs a Go toolchain; nothing in CI runs it. | 1.2 rule 4 | small, ongoing |
| 34 Incremental builds and caching | planned | Per-package object cache keyed by source and flags. Every build compiles the whole program. A self-build is about 19 minutes (`docs/plans/2026-10-28-compiler-speed.md`). I found no cache code in `compiler/internal/build`. | none | large |
| 36 arm64 | planned | `aarch64-linux` and `aarch64-none` targets, a second backend under `compiler/internal/arch` (only `amd64` exists), memory-model.md 5.3 mapping. `pluto build --target arm-linux` is rejected (`tests/cli_test.go:119`). | 20.5 | large |
| 37 PE32+ for UEFI | planned | `--format pe32+`; needs PIC without `R_X86_64_32S`. The driver accepts only `elf` and `binary` (`compiler/cmd/pluto/main.p:494`). | 20.5, abi.md 7 | large |
| 38 Position-independent code | planned | For PE32+, PIE and KASLR outside the top 2 GiB. | abi.md 7 | medium |
| 39 C header bindings generator | planned | Turn C headers into `//pu:extern` declarations and struct layouts. No tool exists under `tools/`. | 20.4 | medium |
| 42 DWARF variable and type information (also listed under item 13) | done with "Open" | `DW_TAG_variable`, `DW_TAG_formal_parameter`, `DW_TAG_*_type`, locations, enum types. `compiler/internal/debuginfo/dwarf.p` emits only compile unit, subprogram and inlined subroutine tags. `docs/spec/debugging.md` section 9 also lists: column numbers in the line table, `.eh_frame`, task traces in panic reports and gdb, interrupt-handler CFI missing the interrupted RSP, allocation profiler under generation checks. | 21.7, debugging.md | large (variables and types); the rest of section 9 is five small to medium changes |
| 41 Symbolized traces | done with "Open" | A per-thread alternate signal stack for worker threads. | 17.1, 21.7 | small |
| 45 `time` | in progress | Time zones and layout-based formatting. The package has `AppendRFC3339` and `ParseDuration` only; no `Format`, `Parse`, `Location`. | 19.6, F.7 | medium |
| 46 `net` | planned | Sockets over the epoll poller. No package. | none | large |
| 51 Owner types in the library | in progress | Nothing named is missing: the audit found every listed type is an owner with `Drop`. The status is stale. One loose end: `sync.NewCond` returns `*Cond` from `alloc.New` and says "free it with alloc.Free" (`lib/pluto/sync/waitgroup.p:108`). | 7.18, 18.10 | small (a decision) |
| 57 Counted strings | done with "Open" | Interface boxes hold their string until the function returns; `(string, error)` returns go through memory; `compiler/compat` could drop helpers that are now native. All are efficiency items. | 7.3, 18.8 | small each |
| 60 Counted `map[K]V` | done with "Left" | The race rule groups maps by type, so two maps of one type are guarded together when either crosses. Precision, not soundness. | 7.10, 19.3 | medium |
| 63 `sync.Scope` | done with follow-ups (phase 2) | A Scope reached through a pointer; helpers that start tasks for their caller's Scope; an error-collecting scope; `Index` and `SetIndex` of library containers not partitioned between tasks; `vec.Vec` element partitions. Also blocks parallel compiler phases (compiler-speed plan). | 19.4 | large |
| 14 Tasks and the hosted scheduler | done with follow-ups | A program that binds its own hosted backend runs on one thread; task spawn costs about twice a mature scheduler's. | 19.1, 19.5 | medium |
| 73 Order of evaluation | done with "Open" | Survey the rest of `compiler/internal/lower` against section 11.26 and add run and difftest cases (assignments, `x[i] = f()`, operator operands). | 11.26 | medium |
| 85 Generation checks | done; tuning open | Speed only: coherent range and ancestor snapshots, same-granule scalar checks, proven extent reuse, interval lookup hints. Recorded cost: dense reads 16.498 ns against 0.809 ns unchecked; consumed 1024-byte copy and clear 1503.776 ns against 49.247 ns. The plan `docs/plans/2026-10-15-language-goal-evidence.md` says no performance result exists yet, so "performance near Rust" is unmeasured on the current implementation. | 18.11 | medium |
| 5 Escape errors | done with "Known precision limit" | A generic factory that returns either a counted assertion or its parameter keeps both branches, so `errors.As` over it is rejected although the child is static. Program in the roadmap text. | 16.6, 18.3 | small to medium |

Count: 21 numbered items. Large 7 (101, 34, 36, 37, 42, 46, 63). Medium 10 (18, 20, 24, 38, 39, 45, 60, 73, 14, 85). Small 4 (5, 32, 41, 57). Items 75, 138 and 13 are folded into 20 and 42. Item 51 is stale and not counted.

### 1.2 Open work that is not a roadmap item

| Where it is recorded | What | Size |
|---|---|---|
| `docs/plans/2026-10-28-compiler-speed.md` | Self-build takes about 19 minutes on one core; the escape phase is 86 percent. Done: six solver fixes. Next: token table locality, cheaper token decoding, sorted store pairs, SCC iteration count, register allocator `pick`/`selectSpill`. Then parallelism in order: per-task scratch stacks (`scratch.Stack`), an audited unsafe handoff for the race check, `sched.runTasks` on `jobs()` workers, parallel code generation. | large |
| `AGENTS.md`, "Known gaps" | Guards that left with the Go implementation: external `solve_test` package files, escape2 `oracle_test` and `sample_test`, refsolve, lsp `TestDefinitionExtractedStdlib`; no coverage measurement of `compiler/` (the per-package floors are gone); no test memory guard. | medium |
| `tests/escapereview_test.go:24` | One known gap: `review3/o14_drop_in_vec_of_owners_local`. The contract table gives `rt.ReleaseValues` and `ZeroValues` no effects although they run element drops, so the escape analysis accepts a program it should reject. Not in the roadmap. | small |
| `compiler/internal/escape2/report/resolve.p:130` | Stale `TODO(A)`: says `diag.EscapeLimit` is not in `internal/diag` yet; `codes.p:136` has it. The local constant `codeEscapeLimit` can go. | small |
| No test | The code blocks of the specification are not compiled by any test (`TestSpecReferences` checks numbering and catalog citations only; `tools/skillcheck` compiles the skill's examples, not the spec's). Spec chapter 18.13 "Examples" has no counterpart. | small |
| `.github/workflows/ci.yml` | CI installs `binutils` and `qemu-system-x86` only. The gdb tests in `tests/dwarf_test.go` skip without gdb. The heavy compiler unit tests (escape2, lsp, build) run only with `PLUTO_UNIT_HEAVY=1`, which CI does not set (`tests/compilerunits_test.go`). | small |

### 1.3 Stale or contradictory text in the roadmap

| Where | Problem |
|---|---|
| Header, line 3 | "Updated 2026-10-27", but item 20 cites the 2026-10-31 audit and item 101 cites 2026-10-28 plans. |
| Gap table row 36 (line 626) | "Nonconsuming owner indexing/length receivers (planned)". Item 103 says done and the audit confirms it. |
| Gap table row 37 (line 627) | "Bounded task-stack inference (planned)". Item 18 says done, with step A landed. The paragraph at line 585 ("Next, finish item 18") reads as if item 18 were still open. Pick one. |
| Gap table row 35 | "audited"; item 102 says done. Not a status the legend defines (done, in progress, planned, decided). |
| Gap table row 34 vs item 101 | Consistent (in progress), but the table's section list stops at 1.2, 18.11, 21.5. Fine. |
| Item 71 | "kernel and freestanding builds stay unchecked until a kernel tag encoding exists." Item 101 phase 2 made them checked by default. |
| Item 75 | Header "done", body "Open: a consuming method used as a method value is an error". Also item 20 repeats it. |
| Item 13 | "done" with "Open: DWARF variable and type information (item 42)". Item 42 is also "done" with the same "Open". |
| Item 51 | "in progress", but its text lists only completed types and the audit found nothing missing. |
| Item 56 | "done", but still says "Compiler hashing migration, explicit comparable-value encoding and key acquisition remain in the later port." The port is finished (go-cutover). Check whether that migration was done. |
| Item 52 | Describes the Go `pu` package, which was deleted with the Go implementation. History only. |
| Items 62, 18 | Item 62 says thunks are named `<function>.go<N>`; the AGENTS.md text and plan say `.task1`. Not checked which is current. |
| Item 5 and item 142 | Both describe the escape analysis; item 5 still says "`internal/escape`, replaced by `internal/escape2`". Correct but redundant. |
| `AGENTS.md` | Says `skill/plutonium/references/known-gaps.md` lists where code differs from the spec. The file now holds two notes and no gap items. |

## 2. Known failures and skips

### 2.1 Result

No test in the tree is marked as a known failure. The `knownfail` file mechanism exists in `tests/corpus_test.go` (lines 25 to 93), and `find` shows no file named `knownfail` under `tests/`, `testdata/`, `compiler/` or `tools/`. `grep` for `TODO`, `FIXME` and `XXX` finds only Go-derived comments in the forked `syntax` and `types2` packages (about 100, all from upstream Go) and the one stale `TODO(A)` above. The word "unimplemented" does not occur in `compiler/`, `lib/` or `tools/` source. Items from other searches are below.

### 2.2 Real gaps found by this search

| File | What | Waiting on |
|---|---|---|
| `tests/escapereview_test.go:24`, `knownEscapeGaps` | `review3/o14_drop_in_vec_of_owners_local` is accepted but should be rejected. | Effects for `rt.ReleaseValues` and `ZeroValues` in the escape contract table. |
| `compiler/internal/lower/builtin.p:149, 219`; `expr.p:117, 686, 1013, 1310`; `stmt.p:126, 721` | `unsupportedf` sites: valid constructs the lowering cannot compile report E2500 `NotYetSupported` ("builtin %s is not supported", "unsupported expression %T", "cannot compare values of type %s", "cannot range over %s"). Most are unreachable after the checker. `tests/build_unsupported_test.go` tests only "defer of builtin len is not supported". E2500 has no test that names its code. | Nothing scheduled; an audit of which sites are reachable. |
| `compiler/internal/lower/packed.p:124, 136, 192, 238` | Packed fields that "cannot be accessed" (big-endian field not on a byte boundary, wide fields in big-endian packed types). Item 117 and E2021 cover most cases at type-check time; these are the lowering backstops. | Not tracked. Check whether the checker rejects every case first. |
| `compiler/internal/lower/call.p:918` | `rt.*` intrinsics expanded inline "cannot be used as a func value" (E2500). | By design for intrinsics. Listed for completeness. |
| `compiler/internal/lower/expr.p:191` | Comment: storage reached through a pointer is "not yet checked for nil" in `lvRaw`. `lv` adds the guard. Looks like a comment about call order, not a gap. | Not verified. |
| `compiler/cmd/pluto/main.p:474, 494` | Unsupported target (anything but x86_64-linux, x86_64-none) and format (anything but elf, binary). | Items 36, 37. |

### 2.3 Rules the item 20 audit lists as untested or unchecked

These come from `docs/verification/2026-10-31-item20-audit.md`. I did not re-run them. I re-checked only that no `knownfail` exists and that `moves/expr.p` still has the method-value move.

- Codes never reported by any test: E3205, E3208, E3209, E3007 (section 3 here confirms).
- Sub-forms without a test: E3202 for embedded owner field, array of owners, named array of owners, interface embedding an owning interface, `type B A` of an owner, generic type with an `owner` type parameter; E3204 for string, map, channel, func, bool, float, packed, volatile, the four box kinds, and `type B owner A` over `owned[T]`; E3210 for `shared` and `synced` of an interface; E3212 for a `*T` converted to a box.
- Rules with no test where the check may be missing: a single expression that moves a variable and uses it (`send(buf, buf.Len())` and the reverse order); a `select` send case that moves an owner only if the case proceeds; `aligned[T, N]` of owners as an implied owner; `Drop` and `Clone` promotion from embedded owners and boxes; a box used as `*T`; a map in an ordinary interface that is returned or stored (item 88); `unsafe.Drop(p)` in a direct test.

### 2.4 Routine skips (not gaps)

| File | Condition |
|---|---|
| `tests/harness_test.go:30`, `tools/escapedigest/main_test.go:31`, `tools/skillcheck/skillcheck_test.go:38`, `editors/vscode/grammar_test.go:465` | No compiler given (`PLUTO_TEST_COMPILER`, `./pluto`, or `pluto` on PATH). A run with no compiler passes without testing the compiler. |
| `tests/dwarf_test.go:496` | gdb not installed. CI does not install it. |
| `tests/programs_test.go:510`, `:590` | No `qemu-system-x86_64`; no C compiler. |
| `tests/build_rangegen_test.go:28`, `tests/escapereview_test.go:105`, `tools/difftest/gen_test.go:98` | `-short`. |
| `tests/compilerunits_test.go:45, 52` | Light packages skip under `-short`; escape2, lsp, build skip without `PLUTO_UNIT_HEAVY=1`. |
| `tests/build_conc_test.go:360`, `tests/build_timer_test.go:401-412` | One CPU; no `taskset`. |
| `tests/lsp_memory_test.go:314, 337` | "A long session", gated. |
| `tools/bootstrap/bootstrap_test.go:51, 54`; `tools/skillcheck/skillcheck_test.go:598, 629, 633` | Needs `GO2PU_SLOW=1`, a linker, `sh`, `SKILLCHECK_FRESH=1`, the `go` command. |
| `tools/difftest/gen_test.go:101`; `editors/vscode/lsp_test.go:26, 29`; `editors/vscode/grammar_test.go:486, 516, 526` | No `go`, no `node`, no npm install, no editor grammar. |
| `compiler/internal/arch/amd64/encode_gas_test.p:137` | GNU `as` missing. |
| `compiler/internal/build/ssatext_test.p:72` | `PLUTOROOT` unset. |
| `compiler/internal/escape2/{scale,cache_measure,report_msgdiff,cache_adversarial}_test.p`, `eir/corpus_test.p`, `eir/build_test.p`, `sched/parallel_test.p` | Env switches (`PLUTO_SCALE_TEST`, `PLUTO_LONG_TESTS`, `PLUTO_SLOW_TESTS`, `EIR_DUMP`, and so on). |
| `compiler/internal/escape2/report_test.p:383, 403, 430`; `cache_adversarial_test.p:309`; `eir/corpus_test.p:130` | Skip a corpus program the analysis accepts or that does not load. Data-dependent, routine. |
| `compiler/internal/scratch/scratch_test.p:70` | No `/proc/self/status`. |
| `tests/testdata/testpkg/calc/calc_test.p:33` | A fixture that tests `t.Skip` itself ("not today"). |

## 3. Diagnostics with no expecting test

Method. The catalog is the 358 codes in `tests/testdata/explain/list.out`; `TestCatalogComplete` ties it to `compiler/internal/diag/codes.p` and `compiler/internal/types2/errors/plutonium.p`. For each code I searched every file under `tests/` (except the explain listings), `testdata/`, the compiler's `_test.p` files, `tools/` and `editors/` for the literal code. The expected-output files (`default.out`, `brief.out`) print the code, so a hit there is a test that expects it. The script is `/tmp/claude-1000/-home-matjam-src/4f4d2070-31e2-4138-9749-6cc0cb506909/scratchpad/codes.py`.

Result: 225 of 358 codes appear in at least one expected-output file or test. 133 appear in no test. Of those 133, 103 are the Go-derived E0001 to E0151 codes and 30 are Plutonium codes. E3010 appears only in compiler unit tests (counted as tested). E2588 appears only in `tests/build_gencheck_test.go`. E0024 appears only in an editor test that asks for an explanation, so I count it untested.

Caveat. A test may check the message and not print the code (for example `tests/parse_test.go`, `tests/build_errors_test.go`, `tests/plutotest_test.go` match message text). So "no code string" does not prove "no test". I spot-checked: E2500 is matched by message in `tests/build_errors_test.go`; E4018 by `tests/plutotest_test.go` ("wrong signature for"). The parser codes E1005 to E1009 may be matched by message in `tests/parse_test.go`; I did not check each.

### 3.1 Plutonium codes (30)

| Code | Name | Rule | Note |
|---|---|---|---|
| E1005 | MissingOperand | parser | maybe tested by message |
| E1006 | BraceOnNextLine | parser | maybe tested by message |
| E1007 | BadImportDecl | parser | |
| E1008 | BadLiteral | parser | |
| E1009 | MissingPackage | parser | |
| E1011 | IfaceMethodTParams | 10.15 | |
| E2009 | BadMainSignature | 15.4 | reported in `types2/resolver.p:430` |
| E2101 | BadPackedBacking | 7.14 | reported in `plutonium_types.p:324` |
| E2104 | BadPackedField | 7.14 | |
| E2106 | WrongWrapperArgs | 7.15 to 7.17 | |
| E2109 | BadWrapperSize | 7.16, 7.17 | |
| E2405 | TooManyVariants | 7.13 | |
| E2500 | NotYetSupported | 21.2 | message-tested only |
| E3007 | BadResultHolds | 18.3 | audit also lists it |
| E3205 | OwnerAlias | 7.18 | audit |
| E3208 | BadTakeArg | 13.10 | audit |
| E3209 | BadUnsafeDropArg | 18.6 | audit |
| E4002 | NotPackageDir | pluto test | |
| E4003 | TestDirName | pluto test | |
| E4004 | TestMainPackage | pluto test | |
| E4017 | ReadPackage | loader | |
| E4018 | BadTestSignature | pluto test | message-tested in `tests/plutotest_test.go` |
| E5002 | RelocOverflow | linker | |
| E5003 | MultipleDefs | linker | |
| E5004 | AssemblerError | `as` | |
| E5005 | CCompilerError | `cc` | |
| E5006 | LinkFailed | `ld` | |
| E5007 | PerCPUNotAtZero | 20.9 | |
| E5008 | ToolNotFound | build | |
| E9001 | InternalError | compiler bug | by nature |

### 3.2 Go-derived codes (103)

E0002 to E0007, E0012, E0013, E0015, E0018 to E0020, E0022, E0024, E0026 to E0028, E0030 to E0040, E0042, E0043, E0047 to E0050, E0052 to E0070, E0072, E0074, E0077, E0078, E0081 to E0083, E0085 to E0089, E0091 to E0094, E0096 to E0100, E0105, E0106, E0112, E0114, E0115, E0117 to E0120, E0124, E0128 to E0136, E0139, E0140, E0143 to E0146, E0148 to E0151. The exact list is the first block of `codes.py` output, saved in `untested.txt` beside the script.

Those with a Plutonium meaning worth testing first:

| Code | Name | Why it matters |
|---|---|---|
| E0036 | InvalidIota | Item 75 says `iota` in an enum is E0036. |
| E0083 | InvalidAppend | `append` is removed (E2013 is the tested path). |
| E0148 | InvalidClear | `clear` of owners (13.3). |
| E0085, E0088, E0086 | InvalidClose, InvalidDelete, InvalidCopy | Chapter 13 builtins' error forms. |
| E0091, E0092 | SwappedMakeArgs, InvalidMake | `make` of channel and map (13.7). |
| E0112, E0114, E0117 | MisplacedFallthrough, DuplicateDefault, InvalidExprSwitch | 12.9, 12.18. |
| E0119, E0120, E0124 | undeclared and duplicate label, jump into block | 12.3, 12.17. |
| E0100, E0128 | UnusedExpr, UnusedResults | 12.4. |
| E0129, E0130 | InvalidDefer, InvalidGo | 12.19 (`go` is E1012, tested). |
| E0150 | InvalidMinMaxOperand | 13.8. |
| E0133 to E0146 | unsafe.Add, Slice, SliceData, String | 20.1. |

Some tested codes appear only in a `tests/types2`, `tests/race`, `tests/escape` or `tests/lower` case, not in `tests/diag` (the script printed about 60 such codes, for example the enum and union codes E2301 to E2318 and E2401 to E2413). They are tested.

## 4. Spec sections without visible test coverage (chapters 7, 11, 12, 13, 18, 19, 20)

This is a search by name and content, not proof. I searched test directory names, test file names, test source and expected output under `tests/`, `testdata/`, `examples/`, `lib/**/*_test.p` and `compiler/**/*_test.p` for the feature each section defines. A hit means a test mentions the feature. It does not mean the test checks every rule of the section. A miss means I found no mention. Most sections are large and hold many rules; I did not check rules inside a section except where stated.

Corpus size for orientation: `tests/diag` 169 cases, `tests/types2` 370, `tests/escape` 634, `tests/race` 380, `tests/moves` 106, `tests/lower` 176, `testdata/run` 339 programs, `testdata/lib` 106, `testdata/plutonium` 92, `testdata/trace` 76, `testdata/kernel` 6 QEMU programs, 110 Go test files in `tests/`.

### 4.1 Sections with no visible coverage, or with a rule known to lack it

| Section | Finding |
|---|---|
| 11.9 Method values | Method values are exercised (`testdata/run/methodexpr.p`, `tests/diag/method_value_receivers`). The rule that a method value of a consuming method is a compile error has no test and no check (section 1.1, item 75). |
| 12.2 Empty statements | No test or program names an empty statement. Probably covered by the parser, not found. |
| 18.13 Examples | No test compiles the spec's examples (see 1.2). |
| 13.11 Handling panics | `panic` is tested widely (`testdata/trace/*`); the rule that `recover` does not exist is covered by `tests/types2/removednames`. No gap found. |
| 20.1 `unsafe.Drop` | Used by the library and by the audit goldens (`tests/audit_test.go`); no program calls it directly. Thin: `unsafe.Offsetof` appears in one run program (`testdata/run/tp_issue47716.p`). |
| 20.3 `//pu:resultholds` | Only the rejection outside the standard library (`tests/escape/rejected_resultholds_in_a_user_package`) and a unit test. No accepted-form test in user code (not allowed there by design). E3007 has no test. |
| 20.3 `//pu:nosimd`, `//pu:section` | One Go test each (`tests/build_opt_test.go`; `tests/directive_test.go`, `tests/build_percpu_test.go`). Thin. |
| 20.5 targets and formats | Only x86_64-linux and x86_64-none are implemented. No arm64 or PE32+ test, by design (items 36, 37). |
| 19.1 Tasks, bounded stack proof | `--stackreport` is tested (`tests/build_stackreport_test.go`). The proof for checked builds does not exist (item 18). |

### 4.2 Table of all sections

Evidence names the most direct test found. "thin" means one program or one Go test.

**Chapter 7**

| Section | Evidence |
|---|---|
| 7.1 Boolean | `testdata/run/boolselect.p`, `arith.p` |
| 7.2 Numeric | `testdata/run/arith.p`, `floats.p`, `testdata/lib/math.p` |
| 7.3 String | `testdata/run/strings.p`, `stringcut.p`, `tests/types2/removedconversions` (74 cases), `tests/build_stringslice_test.go` |
| 7.4 Array | `testdata/run/arrayeq.p`, `arrayeqpad.p` |
| 7.5 Slice | `testdata/run/slices.p`, `nilslice.p` |
| 7.6 Struct | `testdata/run/structs.p`, `directstruct.p`, `embiface.p` |
| 7.7 Pointer | `examples/04-pointers`, `testdata/trace/nil*.p` |
| 7.8 Function | `testdata/run/funcs.p` |
| 7.9 Interface | `testdata/run/ifaces.p`, `itablookup.p`, `ifaceeqmixed.p`, `tests/diag/chan_lent` |
| 7.10 Map | `testdata/run/maps.p`, `mapkeys.p`, `maprange.p`, `tests/build_maps_test.go`, `testdata/lib/mapalloc.p` |
| 7.11 Channel | `testdata/run/chan_*.p`, `tests/build_chancount_test.go` |
| 7.12 Enums | `testdata/plutonium/enum.p`, `genenum.p`, `tests/types2/enumerrors_*` (47) |
| 7.13 Unions | `testdata/plutonium/union.p`, `unionpte.p`, `tests/build_union_test.go`, `tests/types2/unionerrors_*` (56) |
| 7.14 packed | `testdata/plutonium/packedbytes.p`, `packedreserved.p`, `examples/09-packed`, `25-packed-hardware`; E2103, E2105, E2102 corpus cases. E2101, E2104, E2106, E2109 have no test |
| 7.15 volatile | `tests/types2/*volatile*`, `testdata/kernel`, `examples/10-volatile-aligned-unsafe`, `tests/build_opt_test.go` |
| 7.16 aligned | `tests/types2` (34 hits), `testdata/kernel`, `examples/10` |
| 7.17 ring | `testdata/plutonium/ringlen.p`, `ringmethods.p`, `ringiter.p`, `ringowners.p`, `tests/build_ringiter_test.go`, `examples/12-ring` |
| 7.18 Owner types | `testdata/plutonium/dropsites.p`, `dropflow.p`, `ownany.p`, `owniface.p`, `ownunion.p`, `tests/diag/owners`; holes listed in 2.3 |
| 7.19 owned, shared, synced, weak | `testdata/plutonium/boxes.p`, `embedshared.p`, `tests/types2/boxes`; `weak` and `Upgrade` appear in one run program and one build test (thin) |

**Chapter 11**

| Section | Evidence |
|---|---|
| 11.1 Operands | everything |
| 11.2 Qualified identifiers | `tests/diag/import_*` |
| 11.3 Composite literals | `testdata/run/promotedkeys.p`, `tests/types2` literal cases |
| 11.4 Function literals | `testdata/run/funcs.p`, `closureregions.p` |
| 11.5 Closures and regions | `testdata/run/closureregions*.p`, `tests/lower/closureresult*`, `tests/diag/closure_*`, `tests/escape/closure*` |
| 11.6 Primary expressions | everything |
| 11.7 Selectors | `testdata/run/genpromoted.p`, `embiface.p` |
| 11.8 Method expressions | `testdata/run/methexprs.p`, `methodexpr.p` |
| 11.9 Method values | see 4.1 (consuming form untested) |
| 11.10 Index expressions | `testdata/run/farindex.p`, `elemarrayindex.p`, `testdata/trace/index.p` |
| 11.11 Indexing library containers | `testdata/plutonium/indexing.p`, `tests/types2/indexingerrors_*` (25), `tests/diag/container_owner_value_receiver` |
| 11.12 Slice expressions | `testdata/run/stringcut.p`, `tests/build_stringslice_test.go`, `testdata/trace/runeslice*.p` |
| 11.13 Type assertions | `testdata/run/ifaces.p`, `tests/types2` |
| 11.14 Calls | `testdata/plutonium/ownertemp.p`, `boxtemprecv.p`, `tests/diag/temp_receiver_field` |
| 11.15 `...` parameters | `testdata/run/spread.p`; E3310 has a case |
| 11.16 Instantiations | `testdata/run/generics.p`, `genfuncs.p` |
| 11.17 Type inference | `testdata/run/tp_*.p` |
| 11.18 Operators | `testdata/plutonium/ownerread.p`, `tests/diag/owner_operators` |
| 11.19 Arithmetic | `testdata/run/arith.p`, `divconst.p`, `shiftcombine.p` |
| 11.20 Comparison | `testdata/run/arrayeq.p`, `fminmax.p` |
| 11.21 Logical | `testdata/run/control.p`, `boolselect.p` |
| 11.22 Address operators | `examples/04-pointers`, `tests/escape/*` |
| 11.23 Receive | `testdata/run/chan_*.p` |
| 11.24 Conversions | `tests/types2/removedconversions` |
| 11.25 Constant expressions | `testdata/run/floatconst.p`, `rangeconst.p`, `tests/types2/complexconstants` |
| 11.26 Order of evaluation | `testdata/run/evalorder.p` (item 73 survey still open) |
| 11.27 Or clauses | `testdata/plutonium/or*.p`, `tests/types2/orerrors_*`, `tests/build_or_test.go` |

**Chapter 12**

| Section | Evidence |
|---|---|
| 12.1 Terminating statements | `testdata/trace/enumswitch.p`, `tests/types2/enums`, `orvalid` (thin) |
| 12.2 Empty statements | none found |
| 12.3 Labeled statements | `testdata/run/chan_select.p`, `control.p` |
| 12.4 Expression statements | `tests/types2` (E0100, E0128 have no test) |
| 12.5 Send | `testdata/run/chan_*.p`, `tests/race/*` |
| 12.6 IncDec | everywhere; `tests/types2/enumerrors_increment` |
| 12.7 Assignment | everywhere |
| 12.8 If | everywhere |
| 12.9 Switch | `testdata/run/tp_typeswitch*.p`, `tests/build_union_test.go`, `testdata/trace/enumswitch.p` |
| 12.10 For | `testdata/run/maprange.p`, `rangeconst.p`, `testdata/lib/rangecontainer.p`, `tests/build_maps_test.go` |
| 12.11 Range over functions | `testdata/run/rangefunc*.p`, `tests/build_rangefunc_test.go`, `testdata/trace/rangefunc*.p` |
| 12.12 Task statements | `testdata/run/puroutines.p`, `testdata/plutonium/taskcounted.p`, `taskdynamic.p`, `tests/lower/gofunccall*`, `tests/diag/go_*` |
| 12.13 Select | `testdata/run/chan_select*.p`, `selects.p`, `tests/build_sleep_test.go` |
| 12.14 Return | everywhere |
| 12.15 Break | `testdata/run/control.p` |
| 12.16 Continue | `testdata/run/control.p` |
| 12.17 Goto | `tests/escape/gotoloop_*`, `tests/build_rangefunc_test.go`, `testdata/run` |
| 12.18 Fallthrough | `testdata/run/control.p` only (thin); E0112 untested |
| 12.19 Defer | `testdata/run/deferbuiltins.p`, `deferloops.p`, `testdata/plutonium/deferowner.p`, `tests/diag/defer_*`, `testdata/trace/defer*.p` |

**Chapter 13**

| Section | Evidence |
|---|---|
| 13.1 Built-ins absent | `tests/diag/append`, `tests/types2/removednames`, `forbiddenconstructs` |
| 13.2 copy | `testdata/run/selfcopy.p`, `testdata/lib/*`, `tests/escape/rejected_copy_to_heap_slice` |
| 13.3 clear | `tests/build_clear_test.go`, `testdata/plutonium/dropsites.p` |
| 13.4 close | `testdata/run/chan_*.p` (E0085 untested) |
| 13.5 delete | `testdata/run/maps.p` (E0088 untested) |
| 13.6 len, cap | `testdata/plutonium/ringlen.p`, `testdata/run/slices.p` |
| 13.7 make chan, map | `testdata/lib/newmake.p`, `tests/build_newmake_test.go` |
| 13.8 min, max | `testdata/run/fminmax.p` |
| 13.9 new | `testdata/plutonium/newexpr.p`, `newiface.p` |
| 13.10 take | `testdata/lib/take.p`, `takecounted.p`, `tests/moves/*`; E3208 untested |
| 13.11 panic | `testdata/trace/*` |
| 13.12 print | `testdata/plutonium/printtypes.p`, `testdata/run/printfloat.p`, `printorder.p` |

**Chapter 18**

| Section | Evidence |
|---|---|
| 18.1 Values, views, owners | `tests/types2/ownershipkind` |
| 18.2 Where variables live | `testdata/run/initboxes.p` (thin) |
| 18.3 Views | `tests/escape/*` (634 cases), `tests/diag/stale_*`, `owner_view_escapes` |
| 18.4 Loop iterations | `tests/escape/gotoloop_*`, `testdata/run/loopvar.p`, E3006 cases |
| 18.5 Moves | `tests/moves/*` (106), `tests/diag/moves_*`; consuming method value untested; select send untested |
| 18.6 Drops | `testdata/plutonium/dropsites.p`, `dropflow.p`, `dropbase.p`, `tests/build_goexit_cleanup_test.go`; E3209 untested |
| 18.7 Shared ownership | `tests/build_chancount_test.go`, `build_normal_shutdown_atomic_abi_test.go`, `testdata/plutonium/boxes.p` |
| 18.8 Strings | `testdata/lib/strcount*`, `strconc.p`, `strregions.p` |
| 18.9 Allocators | `tests/build_arena_test.go`, `build_safe_arena_*_test.go`, `testdata/lib/chunked.p`, `tests/diag/stale_arena_*` |
| 18.10 No allocation without owner | `tests/diag/append`, `testdata/lib/vec*.p` |
| 18.11 Generation checks | `tests/build_gencheck_test.go`, `build_generation_directory_test.go`, `testdata/kernel/gencheck` |
| 18.12 Safe and unsafe | `tests/audit_test.go` (7 goldens), `tests/vetdoc_test.go` |
| 18.13 Examples | none (spec examples are not compiled) |

**Chapter 19**

| Section | Evidence |
|---|---|
| 19.1 Tasks | `tests/build_conc_test.go`, `build_stack_test.go`, `build_taskstack_test.go`, `build_stackreport_test.go`, `build_deadlock_test.go` |
| 19.2 Sendable rule | `tests/diag/send_*`, `tests/race/transfer*` (41) |
| 19.3 Data race freedom | `tests/race/*` (380) |
| 19.4 Scoped tasks | `tests/race/scope*`, `tests/moves/scopeowners*`, `testdata/plutonium/scope.p`, `tests/build_scope_test.go` |
| 19.5 Scheduler contract | `testdata/plutonium/customsched.p`, `testdata/kernel/sched`, `tests/build_goexit_task_handoff_test.go` |
| 19.6 Timers | `tests/build_timer_test.go`, `build_normal_shutdown_timer_*_test.go`, `testdata/lib/time.p` |
| 19.7 Memory model | `testdata/lib/atomic.p`, `tests/build_opt_test.go`, `tests/build_printatomic_test.go` |
| 19.8 Channels | `tests/build_chancount_test.go`, `testdata/run/chan_*.p` |

**Chapter 20**

| Section | Evidence |
|---|---|
| 20.1 unsafe | `tests/audit_test.go`, `testdata/lib/*`; `unsafe.Drop` and `Offsetof` thin |
| 20.2 Size and alignment | `tests/types2/checksizes`, `tests/build_kernelmode_test.go` |
| 20.3 Directives | `tests/directive_test.go` (`TestSpecDirectives`, `TestDirectiveCheck`); `nosimd`, `section`, `resultholds` thin |
| 20.4 Assembly and C | `testdata/cabi`, `tests/lower/csignatureerrors*` (18) |
| 20.5 Build modes and targets | `tests/cli_test.go`, `tests/build_kernelmode_test.go`; only two targets exist |
| 20.6 Kernel mode | `tests/build_kernelmode_test.go`, `testdata/kernel/*` under QEMU |
| 20.7 Floating point in kernel | `tests/build_kernelmode_test.go`, `build_opt_test.go` |
| 20.8 `pluto/arch/x86` | `tests/build_x86_test.go`; compiler unit `encode_gas_test.p` (needs GNU as) |
| 20.9 Per-CPU variables | `tests/race/percpu*` (about 190), `tests/build_percpu_test.go` |
| 20.10 Interrupts | `tests/lower/interrupt*` (about 70), `tests/race/handler*`, `trap*`, `sti*`, `testdata/kernel/irq` |
| 20.11 Allocation in kernel mode | `tests/lower/interruptcontext*`, `tests/build_initalloc_test.go` |

Sections with no visible test: 2 (12.2, 18.13). Sections with a named rule or form that lacks a test: 11.9, 18.5, 18.6, 20.1, 20.3, 7.14 (four codes), plus thin coverage for 7.19, 12.18 and 12.1.

## 5. Standard library

States judged from line counts, exported functions and the presence of `_test.p` files, and against appendix F. Tests for most packages are in `testdata/lib/*.p` (106 programs) rather than `lib/**/*_test.p`. Only 11 packages have `_test.p` files (cmp, container/heap, flag, hash, path/filepath, slices, sort, strings, unicode, plus none for the others).

| Package | Lines | State | Note |
|---|---|---|---|
| `rt` | 13188 | complete for the contract | Every F.1 name found (see 5.1). 48 files. |
| `alloc`, `alloc/debug` | 739, 1274 | complete | `Default`, `SetDefault`, `Use`, `Restore`, `Owned`, `Shared`, `Synced`, `Raw`, `New`, `Free`. Debug allocator per `docs/spec/debug-allocator.md`. |
| `arena` | 884 | complete | `NewFixed`, `Chunked`, `NewUnsafe`, `Arena`, `Fixed`. |
| `vec` | 593 | complete | All F.3 names found. |
| `hash` | 665 | complete for F.3 | `Map` with `Ptr`, `Remove`, `Pop`; `Set`; FNV and SipHash. |
| `container`, `container/heap` | 60, 287 | complete | |
| `iter` | 220 | partial | `Seq`, `Seq2`; `iter.Pull` is not provided (go-to-plutonium.md line 52). |
| `errors` | 235 | complete | `New`, `Text`, `Is`, `As`, `AsType`, `Unwrap`, `Join`. |
| `sync`, `sync/atomic` | 554, 434 | complete for F.5 | `Scope`, `Mutex`, `RWMutex`, `WaitGroup`, `Once`, `Cond`. `sync.NewCond` is not an owner (see item 51). No `sync.Map` or `Pool` checked. |
| `strings` | 1656 | complete for F.6 | `FromUTF8`, `FromUTF8Lossy`, `Bytes`, `Runes`, `Slice`, `SliceFrom`, `Truncate`, `Builder`. |
| `unicode`, `unicode/utf8` | 11362, 392 | complete | Tables generated by `tools/mkunicode`. No `utf16`. |
| `strconv` | 3548 | complete | Quoting, ParseFloat, ints. |
| `bytes` | 474 | partial | 51 exported functions, `Buffer`. No `Fields`, `Split`, `Replace`, `Map`, `Title` family found (it has `SplitSeq`, `FieldsSeq`). |
| `fmt` | 2046 | partial | 16 exported functions: Print, Printf, Println, Sprint*, Fprint*, Errorf, Append*. No `Scan`, `Sscan`, `Sscanf`. |
| `io` | 128 | stub-like | `WriteString`, `ReadAtLeast`, `ReadFull`, `ReadAll`, plus interfaces. No `Copy`, `Pipe`, `MultiWriter`, `TeeReader`, `LimitReader`. |
| `os` | 2172 | mostly complete | 107 exported functions, `File` owner, `NameError` and name split (F.9). |
| `os/exec` | 784 | partial | `Cmd`, `LookPath`. 14 exported functions. |
| `path/filepath` | 767 | partial | 27 exported functions, `WalkDir`. No `path` package. |
| `flag` | 730 | complete for compiler use | 41 exported functions, `FlagSet` owner. |
| `time` | 1470 | partial | `Duration`, `Time` (UTC), `Now`, `Since`, `Sleep`, `Timer`, `Ticker`, `After`, `Tick`, `AfterFunc`, `ParseDuration`, `AppendRFC3339`. No zones, no `Format`, no `Parse` (item 45). |
| `math`, `bits` | 2639, 239 | complete for common use | 47 + 30 exported functions. No `math/rand`, `math/big`, `math/cmplx`. |
| `slices`, `sort`, `cmp` | 1595, 1229, 77 | complete | |
| `syscall` | 658 | partial | Linux x86-64 only; 44 exported functions. |
| `arch/x86` | 225 | complete for F.8 | Intrinsics, `Gate`, `InterruptGate`, `TrapGate`. |
| `testing` | 499 | partial | `T`, `B`, `FailNow`, `Skip*`, 25 exported functions. |
| `prof` | 367 | partial | 6 exported functions. |
| `internal/oserror` | 14 | stub | by design |

Packages that Go has and Plutonium does not: `context`, `bufio`, `net`, `net/http`, `encoding/*`, `regexp` (the compiler has its own `compiler/internal/regexp`), `log`, `reflect` (forbidden, E2005), `text/template`, `os/signal`, `math/rand`, `unicode/utf16`, `path`, `io/ioutil`, `container/list`, `container/ring`, `compress/*`, `crypto/*`, `hash/crc32` and friends beyond `hash.FNV*` and `SipHash24`. Source: `skill/plutonium/references/go-to-plutonium.md` line 30, and a listing of `lib/pluto`.

### 5.1 Appendix F names

I checked each name F.1 to F.9 against a declaration (`func`, `type`, `var`, `const` or struct field) in the named package. I validated the check with a name that does not exist (it reported missing). Every name appendix F lists was found. Names checked: all of F.1 (types `Panic`, `Fault`, `Writer`, `Console`, `Scheduler`, `Task`, `Context`, `StackRequest`, the hook functions `makechan` to `schedArmTimer`, `NewEpoch`, `EpochEnded`, `ReleaseValues`, `RetainValues`, `MoveValues`, `HoldsStrings`, `Type`, `UnionInfo`), F.2, F.3, F.4, F.5, F.6, F.7 and F.9.

Missing appendix F names: none found. Limits of this check: it proves a declaration of that name exists in that package, not that its signature or behavior matches the text (for example the optional allocator argument of `rt.Go`, the `Restore` consume rule, `vec.Make` and `WithCapacity` signatures, `os.DirEntry.Name` returning `(string, error)`). The tests listed in section 4 cover the main ones.

## 6. Tooling and platforms

| Area | State | Evidence |
|---|---|---|
| `pluto fmt` | done | Forked printer in `compiler/internal/syntax/printer`, `format`; `tests/fmt_test.go` (`TestFmtRepoFormatted`, `TestSameAsGoFormat`). |
| `pluto vet` | one check | `-unsafe` only (item 97). No general lint (`skill/.../known-gaps.md`). Goldens in `tests/testdata/vet`, `audit`. |
| `pluto doc` | done | Item 93; `tests/testdata/doc` (about 30 goldens), `tests/vetdoc_test.go`. |
| `pluto test` | done | `compiler/internal/build/test.p`, `compiler/cmd/pluto/test.p`; `tests/plutotest_test.go`. No external test packages (`x_test` is E4011). |
| `pluto lsp` and VS Code | done | Diagnostics, hover, definition, references, completion, rename (item 33). `editors/vscode` tests need node. `tests/lsp_memory_test.go` has gated long sessions. |
| `pluto explain` | done | `tests/testdata/explain`. |
| Debugger support (DWARF) | partial | Line tables and CFI only (items 13, 42). No variable, parameter or type DIEs, no column numbers, no `.eh_frame`. gdb breakpoints and backtraces tested only when gdb is installed (`tests/dwarf_test.go`). |
| Incremental builds | not started | Item 34. |
| arm64 | not started | Item 36. Only `compiler/internal/arch/amd64`. |
| PE32+ | not started | Item 37. The flag is rejected. |
| PIC | not started | Item 38. |
| C header generator | not started | Item 39. |
| C interop (`//pu:extern`, `//pu:export`, `.c`/`.o`/`.s` in packages) | done | `testdata/cabi`, `tests/lower/csignatureerrors*`, tested against GCC and Clang (item 12). |
| Kernel mode | done for x86-64 | QEMU programs in `testdata/kernel` (6). Item 101 phases 3 and 4 open. |
| Differential testing | in progress | `tools/difftest` (Go code; needs a Go toolchain). Not run in CI. Item 32. |
| Self-hosting | done | Go implementation removed in `15e6515d`. Fixpoint via `tools/bootstrap -stages 3`; CI runs it weekly or on demand. |
| CI | working, with gaps | `.github/workflows/ci.yml`: quick job (gofmt, vet, tests that need no compiler), compiler job (builds with the release compiler, `pluto fmt` check, all tests; about 10 minutes to build, 50 to test), weekly fixpoint job. Runs on the self-hosted runner on kraken. Gaps: no gdb, no `PLUTO_UNIT_HEAVY`, no difftest, no coverage measurement, no memory guard. |
| Reproducible builds | done | `tests/build_determinism_test.go`. |
| Agent skill | done | `skill/plutonium`; `tools/skillcheck` compiles its examples. |

## 7. Not checked

- I did not build or run anything. Every "done" in the roadmap is taken from its text; I did not verify any commit hash, and I did not run a test. Test presence means the file exists, not that it passes.
- I did not read `docs/decisions.md`, `docs/plans/2026-10-10-complete-spec.md` (section 6.5), the 2026-10-13 implementation-state survey, `docs/verification/2026-10-15-spec-consistency.md`, or the other plans except the kernel-lifetime and compiler-speed plans (read in part) and the go-removal plan (phase 2 status only).
- I did not check whether the code matches the spec text rule by rule. Section 4 is a search by name and content. A large section such as 19.3 or 18.3 can have an untested rule that my search cannot see.
- Section 3 relies on `tests/testdata/explain/list.out` as the catalog. I did not confirm it is complete against `codes.p` and `plutonium.p`, though `TestCatalogComplete` is meant to.
- For the 133 untested codes I did not search tests for the message text, except the spot checks named. Some may be tested by message.
- I matched code strings only; I did not open the corpus cases to confirm each expects the code for the reason its name suggests.
- I did not verify whether the `unsupportedf` sites in `compiler/internal/lower` are reachable.
- I did not check `lib/` APIs against Go beyond counting exported functions and listing a few names. The "partial" and "stub-like" labels are judgments from size and a function list, not from reading each package. `sync.Map`, `sync.Pool`, `strings` functions beyond F.6, `os` functions and `testing` coverage were not compared to a reference.
- I did not check `tools/difftest` results or when it last ran, the state of the bench suite (`bench/`), or the performance claims.
- I did not check the editor integration (`editors/vscode`) beyond its test file names.
- I did not verify the claim in item 62 versus AGENTS.md about thunk names (`.go<N>` versus `.task1`).
- I did not check whether item 56's "compiler hashing migration" was done.
- I did not check `docs/spec/abi.md`, `memory-model.md` or `debug-allocator.md` for unimplemented sections, only `debugging.md` section 9.
