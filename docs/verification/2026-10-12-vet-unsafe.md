# Unsafe audit verification, native Linux amd64

Status: complete. Roadmap item 97 implements sections 18.12, 21.1 and 21.5.
`pluto doc` is the next ordered smaller item.

## Behavior and exact oracles

`pluto vet -unsafe` loads packages and their dependencies without requiring `main`,
linking bodyless bindings or executing initializers. With no audit flag, `vet` performs
the same audit. Directory, standard-library and `/...` targets, source build modes,
build tags and normal brief diagnostics follow the existing command conventions.
Findings are a report and exit 0; source errors exit 1 and invalid arguments exit 2.

Every declared function is inspected, including unused generic functions. Function
literals have separate source identities; package initialization is labeled as an
initializer. Typed identities distinguish aliases and shadowing. Direct unsafe sites
include pointer use and conversion, unsafe primitives and layout queries, machine
access, unchecked allocation and Allocator methods, pinned values, bodyless functions
and trusted directives, including `//pu:nochecks`.

Safe wrappers retain safe labels. Their paths identify unsafe functions reached by
direct calls or conservative possible calls through function values, interfaces,
generics, promoted methods, tasks, deferred calls, iterators and user cleanup. Merely
storing an uninvoked callback does not produce a caller edge. Possible cleanup follows
actual destruction sites: borrowed parameters, transferred results, retained globals
and weak-reference release do not claim destruction of a strong payload. Generic
type expansion and call-path searches terminate on cycles.

Ten exact CLI output fixtures cover the default report, package patterns, tags,
hosted/freestanding/kernel selection, invalid flags and modes, a safe library without
main, and complete default/brief source diagnostics. Six complete audit output fixtures
cover categories, call resolution and cleanup. Focused regressions also cover function
literal signatures, writer errors, move/escape/race failures, aliased pointers,
allocator method values, receiver type parameters, promoted generic receivers,
counted discard and clear/copy/delete, weak references, retained tuples and recursive
generic forwarding. The sixteen `.out` files are explicitly tracked and their presence
is asserted in the staged archive inventory.

The one-bullet clarification in section 18.12 adds Sizeof, Alignof and Offsetof to its
summary of unsafe APIs. Root review recorded the existing authority before the edit:
section 20.1 already defines everything in package unsafe as unsafe. Independent spec
review confirmed this summary clarification; no language rule changed. Layout queries
are reported as layout queries, not memory reads.

## Acceptance record

Native Linux amd64, Go 1.27.1, golangci-lint 2.14.0. All commands use the default
ten-minute test timeout and unchanged testguard memory/parallelism caps.

| Gate | Result |
|---|---|
| Full covered suite | Exit 0; build package 537.673 seconds |
| Plain full suite from tracked-only archive | Exit 0; build package 132.819 seconds |
| Lint | Exit 0; 0 issues |
| Full SSA build checks | Exit 0; 138.648 seconds |
| Full generation-off build suite | Exit 0; 79.862 seconds |
| Focused audit, loader and exact CLI tests | Exit 0 |
| Complete coverage comparison and pkgcov | Exit 0; aggregate nondecrease and every floor |
| Catalog/specification, formatter and translation checks | Exit 0 through full suites |
| Archive compiler build and embedded hello | Exit 0; exact oracle with PLUTOROOT unset |

Complete cross-package coverage increases from the accepted streaming-hash baseline
`220bb94b`, 47602/54133 (87.93527%), to 48126/54681 (88.01229%). New audit code covers
496/525 statements (94.47619%); the loader adds 22 covered statements out of 23 new
statements, rising from 287/322 to 309/345. Unchanged-source syntax/token covers one
additional statement (398 to 399 out of 472), and types2 covers five additional
statements (10140 to 10145 out of 13240). These counter changes are recorded separately
from implementation coverage; all other package counts are identical. Every package
floor passes. Complete profiles, logs, archive inventories and numeric exit records
are retained under `/tmp/plutonium-vet-unsafe-verification`.

The plain suite uses a staged tracked-only archive. Its compiler build and embedded
hello pass after awaiting the run before comparing the complete output. The final
status documents are included in a refreshed archive and its compiler/hello checks
are repeated before shipping. No ignored runtime input or generated binary is required.

## Failed-run evidence

The test-first CLI regression failed because the command did not exist. The first
command parser rejected the required single-hyphen `-unsafe`; using the command's
standard flag parser corrected it. Early counted fixtures used an invalid conversion
to a strong reference and were corrected to the library allocation API. A blank-owner
cleanup probe found a nil lexical scope, which now receives the same explicit blank
initializer cleanup as the language requires. Initial expected source-line assertions
were corrected against complete reviewed output.

Read-only peer review found missing interface method values, receiver type-parameter
identity mapping, counted temporary cleanup, explicit builtin cleanup and lexical-scope
filtering, plus false payload cleanup for weak references and retained values. Each
received positive/negative regression coverage; recursive generic expansion also
received a finite visited-set traversal and termination regression. The final peer
review and root technical review are clear. No full acceptance gate failed, and no
oracle, specification rule, timeout or resource cap was weakened.
