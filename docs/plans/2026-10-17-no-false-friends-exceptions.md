# Removing the exceptions to the no-false-friends rule

**Date:** 2026-10-17
**Baseline:** `6a58edb3` (spec: the coordinated consistency correction of 2026-10-15)

> **Status (2026-10-17): decided by the owner and in the specification.** Recorded as decisions D9 (string
> slicing), D10 (normal termination) and D11 (`errors.New` identity) of docs/decisions.md. The owner's
> brief called them D8 to D10; D8 was taken. Implementation is roadmap items 104 (with 74), 105 and 106,
> and none is done.
>
> **Defined now by:** string slicing: sections 7.3, 11.12, 17.1, 18.8, 20.3, A.2 and F.6; normal
> termination: sections 12.12, 14.5, 15.4, 19.1 and 19.7, abi.md 12.10, memory-model.md 3.9 and
> debug-allocator.md (Leaks); `errors.New`: sections 15.2, 16.2, 16.3, 17.1 and F.4; the rule itself:
> sections 1.1, 1.2 and A.1 to A.5. The error code is E2019. Where the specification says otherwise, this
> plan's text is historical; in particular its question 4 (resource limits) is settled by a sentence in
> A.1, and its question 1 (names from the operating system) is still open.
>
> **Supersedes:** decisions F12 and F19 of `2026-10-10-go-false-friends.md`; the "Approved task policy" of
> `2026-10-14-normal-shutdown.md` (D6, wait for every task).

## Summary

| Exception | Decision | Effect on the rule |
|---|---|---|
| 1. `s[i:j]` panics off a rune boundary | The owner's direction: a slice expression on a string is a compile error (E2019) that names `strings.Slice(s, i, j)` and `strings.SliceFrom(s, i)`, which the compiler lowers inline and which panic off a rune boundary. Strings stay valid UTF-8; arrays and slices are unchanged. | removed |
| 2. normal termination waits for tasks | When `main` returns, the program ends as in Go. Package cleanup and leak accounting run only when no other application task is still running at that moment; otherwise the remaining tasks are abandoned as `os.Exit` abandons them. | removed |
| 3. `errors.New` has one identity per call site | Each evaluation of `errors.New` yields a new identity without allocating: the site has its own error type, and the interface's data word holds a fresh value of a per-site counter. | removed |

With all three adopted, section 1.2 states the rule with no exceptions, appendix A loses section A.5
and the "Documented exceptions" class of section A.1, and the oracle subset needs no exclusions for
these behaviours.

Goals used below, by number:

1. **Easy to use.** The compiler works out when memory is freed and proves each use safe; allocation
   sites stay visible; ordinary code needs no annotations.
2. **Go-familiar, not Go-compatible; no false friends.** A divergence from Go caused by the memory
   model is visible in the source. Code that looks like Go and compiles does what Go would do.
3. **Kernel code works like hosted code**, except for explicit kernel features.

Standing goals: memory and race safety at Rust's level without a borrow checker (named "safety"
below), no garbage collector, performance near Rust, suitability for the PlutoniumOS kernel
(freestanding builds, no scheduler unless requested).

---

## 1. String slicing

### The question

In Go a string is a sequence of bytes. `s[i:j]` cuts it at byte offsets and may cut a multi-byte
character (a rune) in half; the result holds invalid UTF-8, which Go permits. In Plutonium every string
holds valid UTF-8 (section 7.3, String types), so a cut inside a rune cannot produce a string. Today the
specification makes such a cut panic (section 11.12, Slice expressions):

```go
if len(title) > 40 {
	title = title[:40] + "..." // Go: may end in half a rune. Plutonium today: may panic.
}
```

The line is valid Go, compiles in Plutonium, works on ASCII input and panics on the first title with
an accented letter at byte 39. Nothing in the source says it differs from Go. That is a false friend,
and on a server reading untrusted input it is a denial of service: `if len(line) >= 4 && line[:4] ==
"GET "` panics on `"abé..."` where Go compares unequal.

The difficulty is a three-way conflict. A design can have at most two of:

1. every string is valid UTF-8;
2. `s[i:j]` on a string means what it means in Go (cut at byte offsets);
3. `s[i:j]` on a string compiles with indices computed at run time.

Keeping 1 and 3 gives the panic (status quo). Keeping 2 and 3 means strings may hold any bytes.
Keeping 1 and 2 means `s[i:j]` on a string compiles only where it cannot differ from Go. The owner's
direction takes the third pair in its simplest form: `s[i:j]` on a string never compiles, and a named
function does the cut.

### The owner's direction: `strings.Slice` and a compile error (recommended)

**What a programmer writes.**

```go
head := line[:4]
// error E2019: a string cannot be sliced with s[i:j]. A string holds valid UTF-8, so a cut must
// fall between runes; strings.Slice(line, 0, 4) cuts there and panics if it would split a rune.

head := strings.Slice(line, 0, 4)        // checked cut; panics off a rune boundary
rest := strings.SliceFrom(s, i+len(sep)) // the s[i:] form
key, val, ok := strings.Cut(kv, "=")     // no index at all: cannot fail
title = strings.Truncate(title, 40)      // longest prefix of at most 40 bytes on a boundary
c := line[0]                             // indexing a byte: unchanged, as in Go
for i, r := range s { ... }              // unchanged, as in Go
```

**The rule.**

- A slice expression whose operand is a string (of any string type, typed or untyped, constant or
  not) is a compile error, E2019, whose message names `strings.Slice` and `strings.SliceFrom`. Arrays,
  pointers to arrays and slices keep `a[i:j]` with their element as the unit, as in Go.
- `strings.Slice(s, i, j)` returns the bytes of `s` from offset `i` to offset `j`, sharing `s`'s buffer
  as a substring does today. `strings.SliceFrom(s, i)` is `strings.Slice(s, i, len(s))`. Both are generic
  over the string type, `func Slice[S ~string](s S, i, j int) S`, so a named string type keeps its type
  as it does under Go's `s[i:j]`.
- An index out of range panics as an out-of-range slice expression does. An index that is in range but
  not a rune boundary panics with a message that names the rule and the helpers:
  `strings.Slice: offset 2 is inside a rune; a string holds valid UTF-8 (use strings.Truncate or
  utf8.RuneStartBefore to cut at a byte count)`.
- `s[i]` (a byte), `len(s)` (bytes), `range s` (runes) and comparison are unchanged and mean what they
  mean in Go.

**Settled points.**

- *Bad boundary: panic.* A panic matches the out-of-range case and keeps every call one expression.
  Returning an error would put an error path on every slice in every parser, for a condition that in
  correct code arises only from untrusted input. Rounding down, as `strings.Truncate` does, is silent
  and has no single right answer for a start offset (round the start down and the result gains bytes;
  round it up and it loses them). Rounding stays available by name: `strings.Truncate(s, n)` and
  `utf8.RuneStartBefore(s, i)`.
- *Common cases need no index.* `strings.Cut`, `CutPrefix`, `CutSuffix`, `TrimPrefix`, `TrimSuffix`,
  `HasPrefix`, `Split` and `Fields` keep Go's names and meaning and cannot split a rune. Offsets from
  `strings.Index`, `IndexByte`, `IndexRune`, `LastIndex` and from `range` are always boundaries, and so
  is `i+len(sep)` after a match of a valid UTF-8 `sep`: a valid needle starts and ends on rune
  boundaries of a valid haystack. So `strings.Slice` after `Index` never panics on valid UTF-8 input.
  The cases that can panic are fixed offsets on data whose content the program does not control
  (`line[:4]`). For untrusted input the idiomatic forms are `strings.HasPrefix(line, "GET ")`, or a check
  with Go's `utf8.RuneStart(line[4])` before the cut; the documentation of `strings.Slice` says so.
- *Cost.* A rune-boundary test is O(1): offset `k` of `s` is a boundary if `k` is 0 or `len(s)`, or
  `s[k]` is not a continuation byte (`0b10xxxxxx`). Two byte loads and two compares, and none when the
  control word's `nonASCII` flag is clear (abi.md section 1.1), which is one test for both bounds. The
  compiler knows `strings.Slice` and `strings.SliceFrom` (appendix F lists them) and lowers them inline
  to the same code as today's string slice expression, so there is no call. The optimizer may remove
  the boundary test where it proves a boundary (`internal/opt/runebounds.go` does this today); that
  changes only speed, never acceptance, so section 21.3 (Acceptance does not depend on optimization) is
  respected.
- *Constants: no special case.* `"hello"[1:3]` is E2019 too. A constant exception would make the rule
  depend on whether an operand is constant, it is rare in real code, and a single rule is easier to
  learn and to translate mechanically. `s[:]` is also E2019; write `s`.
- *Library code.* `pluto/strings` and the runtime implement `Slice` on the compiler's lowering, not on
  `s[i:j]`; no `unsafe` is needed in programs.

**Mechanical translation (the self-hosting port).** The Go-to-Plutonium translator (`tools/go2pu`) has type information
(it must, to place `strings` versus `vec` and owner rewrites), so the rewrite is local:

| Go, with `x` of core type string | Plutonium |
|---|---|
| `x[lo:hi]` | `strings.Slice(x, lo, hi)` |
| `x[lo:]` | `strings.SliceFrom(x, lo)` |
| `x[:hi]` | `strings.Slice(x, 0, hi)` |
| `x[:]` | `x` |

Arguments are evaluated left to right as Go evaluates the operand and indices, so the order of side
effects is kept. The translator adds the `pluto/strings` import, under another name if the file already
declares `strings`. A named string type keeps its type through the generic signature. One semantic
change remains, and the translator cannot remove it: Go code that cuts inside a rune on purpose (byte
truncation of text, splitting a string into byte chunks) now panics at that call. The port finds these
by running its tests; each becomes `strings.Truncate`, a byte loop, or `[]byte` data.

**Against the goals.**

- **Goal 2: served.** No Go-looking string slice compiles with a different meaning. The run-time check
  now sits behind a name Go does not have, which section A.1 already treats as visible ("a function that
  Go does not have ... is not a false friend, because its documentation is the first thing a reader
  looks up"). The exception is removed.
- **Goal 2, familiarity: cost.** Slicing a string is one of Go's most frequent operations; every one
  needs an edit. The edit is mechanical, the error names it, and the language has already accepted
  this kind of cost where the memory model required it (`append` and `make([]T, n)` are compile errors
  with named replacements). Many uses disappear into `Cut`, `TrimPrefix` and `HasPrefix`, which Go
  style already prefers.
- **Goal 1: served.** No annotation, no analysis to learn: one rule with one fix.
- **Safety and the text invariant: kept.** Every string stays valid UTF-8, which the owner wants, and
  all string functions can rely on it.
- **Performance: neutral.** The same machine code as today's checked slice.
- **Kernel (goal 3): neutral.** Kernel code slices strings the same way hosted code does; binary data
  stays in `[]byte` and arrays, as today.

### Other options, and why they are worse

**(a) Keep the panic on `s[i:j]` (status quo).** Same run-time check, but behind Go's syntax, so the
difference is invisible. The direction above is (a) with the check moved behind a name.

**(b) A compile error only where the compiler cannot prove a boundary.** No specified rule accepts
`line[:4]` or `s[i+2:]`, so most ASCII parsing would fail anyway, and programmers would have to learn
which index expressions the analysis tracks (cost to goal 1). It rejects about as much code as the
owner's direction while being harder to explain.

**(c) A distinct type for byte offsets.** `strings.Index` would return a `strings.Offset` and `s[i:j]`
would accept only that type. Every offset arithmetic (`i+1`, `i+len(sep)`) leaves the type, so Go's
parsing code fails to compile with errors that do not name a single fix. Worse than one rule with one
replacement.

**(d) Strings hold any bytes, as in Go.** `s[i:j]` slices bytes and never panics for a boundary;
`string(b)` returns as a copying conversion; names from the operating system return their exact bytes.
Go code needs no edits at all, and it also removes the lossy-name difference of A.3 (decision F20 of the
audit). It gives up the guarantee that a `string` is valid text. The owner wants that guarantee, and
nothing in safety depends on it either way (section 12.10 already defines `range` over invalid bytes),
so the choice between (d) and the owner's direction is the value of the invariant against the cost of
editing every string slice. With the invariant wanted, the owner's direction is the sound way to keep
it without an exception.

**(e) Unit-aware slicing: `s[i:j]` counts runes.**

```go
s := "héllo"
t := s[:2] // "hé" (2 runes); Go gives "h\xc3" (2 bytes)
```

1. If `len(s)` stays a byte count, units mix (`s[:len(s)]` is out of range for non-ASCII text). If
   `len(s)` counts runes and `s[i]` returns a rune, everything agrees with Go on ASCII text and silently
   disagrees on other text: `s[:4]` is four runes where Go means four bytes, and `len(s)` sizes a buffer
   too small for `copy(buf[:], s)`. That is the false friend goal 2 forbids, in its worst form: a
   different answer instead of a panic or an error.
2. Finding rune `i` is O(n) on UTF-8 unless a rune index is stored with the string (hidden allocation and
   a larger representation). `strings.Index`, `bufio`, `utf8` and every parser use byte offsets, so each
   would change unit silently or need a conversion. Parsers, hashing, the compiler's scanner and kernel
   code would pay the walk on every slice.
3. A rune-unit operation under its own name is sound: a library function such as `utf8.Substr(s, i, j)`
   documented as O(n). A rune-indexed type already exists: `[]rune`, or `strings.Runes(s)`.
4. Arrays and slices already use their element as the unit, and so does a Go string: its element is
   the byte (`s[i]` is a `byte`). Only strings would change, against what `s[i]` returns.

Rejected as the meaning of `s[i:j]`. A named rune-unit function can be added to `utf8` as an ordinary
library function without a specification change.

### Specification changes

- **Section 1.2 (Design rules), rule 2:** string slicing leaves the exception list (combined text in
  part 4).
- **Section 7.3 (String types):** replace the paragraph **Slicing on rune boundaries** with: "A slice
  expression on a string is a compile error (E2019, section 11.12). `strings.Slice(s, i, j)` and
  `strings.SliceFrom(s, i)` take a substring; each bound must be a rune boundary, that is `0`, `len(s)`,
  or the offset of a byte that is not a UTF-8 continuation byte, and a bound that is not one panics.
  `strings.Truncate(s, n)` and `utf8.RuneStartBefore(s, i)` find the boundary at or before a byte count."
  In **Counted values**, "A substring `s[i:j]` shares the buffer of `s`" becomes "A substring made by
  `strings.Slice` shares the buffer of `s`."
- **Section 11.12 (Slice expressions):**
  - first sentence of the section: the operand of a simple slice expression is an array, a pointer to
    an array or a slice. Add: "If the operand is of string type (typed or untyped), the expression is a
    compile error (E2019) that names `strings.Slice` and `strings.SliceFrom` (section 7.3)."
  - remove "or strings" and "or constant strings" from the range rule; delete "Except for untyped strings"
    and the sentence on untyped string operands; "If the sliced operand is a string or slice" becomes
    "If the sliced operand is a slice"; delete "A slice of a string shares the string's buffer and keeps
    it alive, as a copy of the string does (section 18.8, Strings)" from **Slices are views**.
  - delete the paragraphs from "**Strings slice on rune boundaries.**" to the end of the section (the
    boundary rule, the panic message, the constant-operand check, the "one documented exception"
    paragraph with its example, and the `unsafe.String` paragraph). The definition of a rune boundary
    moves to section 7.3 (above).
- **Section 17.1 (Panics), What panics:** the bullet "A slice expression on a string with a bound that
  is not at the start of a rune..." becomes "`strings.Slice` or `strings.SliceFrom` with a bound that is
  not a rune boundary (section 7.3). The message names `strings.Truncate` and `utf8.RuneStartBefore`."
- **Section 18.8 (Strings), Buffers and counts:** "A substring `s[i:j]` shares `s`'s buffer" becomes "A
  substring made by `strings.Slice` or `strings.SliceFrom` shares `s`'s buffer".
- **Section 20.3 (Directives), The rule:** delete "which string slices fall on rune boundaries" from the
  facts the compiler infers (no acceptance decision depends on it any longer).
- **Appendix A.2 (Removed):** add the row "`s[i:j]`, `s[i:]`, `s[:j]`, `s[:]` on a string | compile error
  (E2019): a string holds valid UTF-8 and a byte cut could split a rune | `strings.Slice(s, i, j)`,
  `strings.SliceFrom(s, i)` (panic off a rune boundary); `strings.Cut`, `TrimPrefix`, `HasPrefix`;
  `strings.Truncate(s, n)` | 11.12, 7.3".
- **Appendix A.3, Strings:** delete the row "`s[i:j]` slices bytes". The rows on valid UTF-8 and on names
  from the operating system stay (see question 1 in part 5).
- **Appendix A.5:** deleted (part 4).
- **Appendix F.6 (Strings):** add
  `func Slice[S ~string](s S, i, j int) S // the bytes from i to j; panics unless both are rune boundaries`
  and `func SliceFrom[S ~string](s S, i int) S // Slice(s, i, len(s))`, and state that the compiler
  lowers both inline. Keep `Truncate` and `RuneStartBefore`. The closing paragraph says these are the
  replacement for slicing a string (appendix A, section A.2), not "helpers for slicing a string on a rune
  boundary (appendix A, section A.5)".
- **Examples in every chapter:** any fenced `go` example that slices a string is rewritten to
  `strings.Slice` (a search of the examples is part of the amendment).
- **Error codes:** new E2019 (`StringSliceNotPermitted`; free at the baseline, confirm when allocating).
  The compile-time check for constant strings in section 11.12 disappears into E2019. E2012 and E2015
  are unchanged.

### Implementation consequences

- `internal/types2`: report E2019 for any slice expression on a string-typed or untyped-string operand,
  with a suggested fix that spells the `strings.Slice` or `strings.SliceFrom` call; add the catalog entry
  in `internal/diag` with `pspec("11.12", "Slice expressions")`.
- `internal/lower`: lower calls of `strings.Slice` and `strings.SliceFrom` as intrinsics, reusing the
  string-slice code of `strops.go` (bounds check, boundary check behind the `nonASCII` flag, shared
  buffer and count). Keep `internal/opt/runebounds.go`, now applied to the intrinsic.
- `lib/pluto/strings` and `unicode/utf8`: add `strings.Truncate` and `utf8.RuneStartBefore`, which
  appendix F.6 already names but the library at the baseline does not have (an existing gap).
- `lib/pluto/strings`: declare `Slice` and `SliceFrom` (bodies are the intrinsic); rewrite every
  `s[i:j]` in `lib/` and in Plutonium test programs; update the panic message text.
- `tools/go2pu`: add the rewrite table above, with the import handling.
- Tests: E2019 for typed, untyped, constant and named string operands; panics for both bounds; no
  panic for `Index`-derived cuts; inline lowering (no call in the SSA of a simple use).


---

## 2. Normal termination

### The question

In Go, when `main` returns the process exits at once; other goroutines are stopped wherever they are.
In Plutonium today (sections 15.4 and 19.1), when `main` returns the program waits for every other task
and every application timer callback to finish, then cleans up package variables in reverse order of
initialization, then runs the debug allocator's leak accounting.

```go
func main() {
	task heartbeat() // loops forever, as background goroutines often do
	fmt.Println("done")
}
// Go: prints "done" and exits.
// Plutonium today: prints "done" and never exits.
```

Nothing in the source shows the difference, so it is a false friend. The waiting was chosen so that
every owner is dropped, every counted object released and the leak report exact (decision D6).

### What the memory model requires

- **Scoped tasks.** A task that uses its starter's frame is started by a `sync.Scope`, and every path out
  of the Scope's block drops it, which joins its tasks (section 19.4). A Scope in `main` (or in anything
  `main` calls) has joined before `main`'s locals are dropped. So when `main` has returned, no task holds
  a view of any frame of the main task. Scoped tasks need nothing from termination.
- **Unscoped tasks** hold only sendable values (section 19.2): owners moved into them, counted
  references, and access to package variables. Their memory stays valid as long as nothing frees it.
- **`main`'s own cleanup** already runs while other tasks run, in the status quo too: its defers and
  drops complete before the wait begins. Returning from `main` while tasks run is no different, for
  safety, from returning from any function while tasks it started run.
- **Package cleanup** frees what package variables hold. If it ran while other tasks still use those
  variables, they would read freed memory (a generation-check panic in checked builds). So package
  cleanup is only sound after every other application task has finished.
- **Leak accounting** is exact only after all owners have been released. With tasks still holding
  owners, the debug allocator cannot tell their allocations from leaks.
- **Abrupt exit** is already specified: `os.Exit` ends the program at once, drops nothing, joins
  nothing, and memory stays valid until the process ends (sections 15.4 and 19.4).

### Options

**(a) Wait for all tasks (status quo).** Exact cleanup and leak reports. A program with a never-ending
worker never exits. Costs goal 2.

**(b) Go's behaviour: exit at once, always.** `main` returns, `main`'s cleanup runs, the process exits
with `main`'s status. Abandoned tasks keep their owners undropped (the operating system reclaims the
memory and descriptors); package cleanup never runs; the leak report would list everything package
variables hold, so it must exempt them or be skipped. Scoped tasks are unaffected (already joined). In
freestanding builds control returns to the program's entry point, which decides what follows; no drain
contract is asked of a scheduler backend. Serves goal 2. Costs the exact leak report and package
`Drop`s for every program, including programs that have joined all their tasks.

**(c) Cancel, then wait.** `main` returning closes a cancellation signal that tasks observe (a closed
channel, a context), then waits.

```go
func worker(ctx rt.Context) { for !ctx.Done() { ... } }
```

The language has no cancellation, a task blocked in a system call cannot observe one, and a task that
ignores it still keeps the program alive. Go programs written for Go never look at such a signal, so
they would still hang. It adds a mechanism and still diverges. Rejected.

**(d) Go's behaviour, with cleanup when nothing else runs.** As (b), except that when no other
application task is running at the moment `main` returns, package cleanup and leak accounting run before
the exit, as today.

```go
func main() {
func main() {
	s := sync.Scope{}
	s.Task(func() { work() })
	s.Wait()
	// every task has finished: package cleanup and the leak report run, then exit
}

func main() {
	task heartbeat()
	fmt.Println("done")
	// heartbeat is still running: exit at once, as Go does; no package cleanup
}
```

Recommended.

### Recommendation: (d)

**The rule.** When `main` returns normally:

1. `main`'s ordinary return completes: its deferred calls run and its locals are dropped, which joins
   any `sync.Scope` it declared (unchanged).
2. One scheduler transition closes task and application-callback admission and observes whether any
   other application task is still running (started and not yet finished with its function's deferred
   calls and drops) or any application callback is in flight.
3. **If none is:** pending application timer callbacks are canceled as `Stop` cancels them, releasing
   their saved arguments and captures; tasks that have ended complete their retirement (runtime work
   only, bounded); package cleanup runs as section 15.4 defines it today; the debug allocator accounts;
   the program exits with `main`'s status.
4. **Otherwise:** the program ends at once with `main`'s status, as Go does. The remaining tasks stop
   where they are; their owners are not dropped; package cleanup does not run. This is the abrupt path
   `os.Exit` already takes, after `main`'s own cleanup. The debug allocator prints one line naming the
   number of tasks still running and skips the leak report (their allocations are live, not leaked); it
   does not change the exit status.

`rt.ExitTask` in `main` (section 17.3) and `os.Exit` are unchanged. They already match Go.

**Justification.**

- **Goal 2.** A Go programmer writing `task worker()` and returning from `main` sees Go's behaviour:
  the program exits. Package cleanup is a Plutonium addition with no Go counterpart: it runs `Drop`
  methods, which Go programs do not have, and returns memory the operating system would reclaim anyway.
  For code in the common subset of Go and Plutonium, steps 3 and 4 are indistinguishable from Go.
- **Goal 1 and the leak report.** A program that joins its tasks before returning (the usual structure,
  with `sync.Scope`, `sync.WaitGroup` or a closed channel) gets the deterministic cleanup and exact leak
  report of decision D6. A program that leaves workers running gets Go's exit and a report that says why
  it is incomplete. The debug allocator stays useful where it can be exact and does not claim exactness
  where it cannot.
- **Safety.** Package cleanup runs only when no other application task can touch a package variable,
  so it cannot free memory a running task uses. Abandoned tasks free nothing and are stopped by the end
  of the process, as under `os.Exit`, which section 19.4 already shows to be sound.
- **Kernel (goal 3).** A kernel's `main` does not return, so it is unaffected. A freestanding program
  whose `main` returns gets the same rule; when tasks remain, control returns to its entry point.
  Scheduler backends no longer have to implement the full drain-until-retired contract; they answer
  "is any application task unfinished" and close admission in the same transition, which the existing
  contract already includes.
- **Cost.** Whether package `Drop`s run depends on whether every task finished first; with a task
  finishing at the same moment it depends on the schedule. Only Plutonium-specific behaviour (package
  `Drop`s, the debug report) varies, and section 21.3 already lets output vary between permitted
  schedules. A program that needs its package cleanup joins its tasks, which is visible in its source.

A library call such as `rt.WaitTasks()` that waits for all other tasks before `main` returns would let a
program ask for the old behaviour explicitly. It is not needed for the decision and is left to the owner.

### Specification changes

- **Section 1.2 (Design rules), rule 2:** the exception list loses normal-termination task waiting
  (combined text in part 4).
- **Section 1.1 (Scope and goals):** the goal "Read like Go, and never mislead a Go reader" ends
  "Differences are visible in the source (section 1.2, Design rules; appendix A)."
- **Section 12.12 (Task statements):** replace "Normal termination after `main` returns waits for all
  remaining application tasks and callbacks, including their cleanup and retirement (section 15.4,
  Program execution)." with "When `main` returns, tasks that are still running are not waited for
  (section 15.4, Program execution)."
- **Section 14.5 (An example package):** replace the last two sentences after the sieve with "When
  `main` returns, the program ends; workers still blocked are not waited for (section 15.4)."
- **Section 15.4 (Program execution):**
  - **When `main` returns:** rewrite as steps 1 to 4 of the rule above. Delete "It then waits for all
    remaining tasks to finish naturally... A running task that never finishes keeps the program alive",
    "Deadlock remains a fatal error under section 19.1; waiting for termination does not suppress it",
    and the paragraph on scheduled application callbacks, replaced by step 3's cancellation of pending
    callbacks and step 2's in-flight check.
  - **Package cleanup:** opens "When package cleanup runs (step 3)". The rest of the paragraph, the
    static-object, in-place and revival paragraphs are unchanged.
  - The paragraph on starting tasks during package cleanup is unchanged.
  - The example's comment "global is dropped after all tasks finish" becomes "global is dropped at
    package cleanup, which runs because no other task is running".
  - **`os.Exit`:** unchanged; add that step 4 ends the program the same way after `main`'s cleanup.
- **Section 19.1 (Tasks):**
  - **How a task ends:** delete "Normal termination is an additional synchronization boundary: every
    application task/callback, its mandatory cleanup and retirement callbacks happen before package
    cleanup begins (section 15.4), including visibility across processors." Replace with "When package
    cleanup runs, every application task has finished before it begins (section 15.4)."
  - **Program exit:** rewrite as: "When `main` returns, the program ends; tasks still running are not
    waited for and their owners are not dropped. Package cleanup runs only when no other application task
    is running (section 15.4)." The sentences on `os.Exit` and on `rt.ExitTask` in `main` stay.
- **Section 19.7 (Memory model):** the bullet sentence "At normal termination all application
  task/callback operations, required cleanup and retirement callbacks happen before package cleanup
  begins..." becomes "When package cleanup runs (section 15.4), every operation of every finished
  application task and callback happens before it begins."
- **memory-model.md section 3.9 (Ending a scoped task), its normal-termination boundary:** the same
  change as section 19.7.
- **abi.md section 12.10 (Scoped tasks), Normal termination:** replace the drain contract with the
  check-and-close transition of step 2; the backend reports retirement of tasks that have ended; the
  rest (cleanup order, static bounds, exits during cleanup) applies when cleanup runs.
- **debug-allocator.md, Leaks:** "At normal exit, hosted builds first complete the task/callback drain
  and package cleanup" becomes "When `main` returns with no other task running, hosted builds first
  complete package cleanup ... When tasks are still running, the allocator prints how many and skips
  the report." Delete "live application work keeps normal shutdown waiting, and deadlock remains fatal".
- **Appendix A.3, Tasks and concurrency:** delete the row "the program exits when `main` returns".
  **A.4 (Added):** add "package cleanup at exit | when `main` returns and no other task is running,
  package variables are dropped in reverse order of initialization | 15.4".
- **Error codes:** none.

### Implementation consequences

- Runtime (`lib/pluto/rt`, `sched.p`, `join.p`, `rt_linux.p` `mainOnStack`): after `main` returns,
  perform the close-admission-and-count transition; branch to the existing cleanup sequence when the
  count of running application tasks and in-flight callbacks is zero, otherwise exit with the status.
  Cancel pending application timer callbacks on the cleanup path, releasing captures.
- The drain loop that waits for running tasks goes; the retirement wait for already-ended tasks stays.
- Debug allocator (`lib/pluto/alloc/debug`): on the abandon path print the running-task count and skip
  the report; keep the exit status.
- Freestanding entry: return from the termination routine to the program's entry point on the abandon
  path. Custom scheduler backends implement the count-and-close transition (already part of the drain
  contract).
- Tests: the "forever task keeps program alive" acceptance case becomes "forever task: program exits
  with main's status, no package cleanup"; the joined-task cases keep the cleanup and leak assertions.
  Add Go-oracle programs that start a background task and return from `main`.

---

## 3. `errors.New` identity

### The question

In Go, `errors.New("x")` returns a new pointer at every call, so two calls compare unequal even at the
same line. In Plutonium today each call site evaluates to one static error (section 16.3), so repeated
calls at one site compare equal. The static design avoids an allocation per call.

```go
func check(b []byte) error {
	if len(b) == 0 {
		return errors.New("empty")
	}
	return nil
}

e1, e2 := check(nil), check(nil)
fmt.Println(e1 == e2) // Go: false. Plutonium today: true.
```

`var ErrX = errors.New("x")` at package level behaves the same in both languages: its initializer runs
once. The difference shows only when one call site runs more than once and two of its results are
compared. Go idioms that depend on it are few but real: deduplicating errors with `==` or a
`map[error]int` (Go counts each failure separately, Plutonium today merges them), a test asserting that
two failures are distinct, and `errors.Is(err, f())` where `f` returns a fresh `errors.New` (false in
Go, true here). It is a false friend: valid Go, compiles, different answer, nothing in the source says so.

### Options

**(a) Keep static identity (status quo).** No allocation, works before an allocator exists. Costs
goal 2 for the cases above.

**(b) `errors.New` only at package level.** A call in a function body is a compile error that points to
a package-level sentinel or to `fmt.Errorf`.

```go
func check(b []byte) error {
	return errors.New("empty") // error: declare var errEmpty = errors.New("empty") at package level
}
```

Serves goal 2 by the letter, but `return errors.New("...")` in a function body is one of Go's most
common lines; every such line needs an edit, and the suggested sentinel changes the identity semantics
of the Go original anyway. Costs goal 1 and familiarity. Rejected.

**(c) Allocate a fresh counted error per call.** Matches Go, but `errors.New` would allocate behind a
name that Go programmers do not read as allocating, fail when no allocator exists (early kernel boot),
and need an out-of-memory path. Against the no-hidden-allocation rule and goal 3. Rejected.

**(d) Fresh identity carried in the value, no allocation.** Each call site of `errors.New` gets its own
compiler-generated error type and a 64-bit counter in static storage. Each evaluation of the call
increments the site's counter atomically and yields an error whose dynamic type is the site's type and
whose data word holds the new counter value. The site type's `Error` method returns the constant text.

```go
e1, e2 := check(nil), check(nil)
fmt.Println(e1 == e2) // false, as in Go: same type, different counter values
e3 := e1
fmt.Println(e1 == e3) // true, as in Go: a copy has the same identity
```

Recommended.

### Recommendation: (d)

Section 7.9 already stores an integer value directly in an interface's data word, and interface
equality compares dynamic type and value. So:

- two results of one call site have the same type and different counter values: unequal, as in Go;
- copies of one result are equal, as copies of a Go pointer are;
- results of different call sites have different dynamic types: unequal, as in Go;
- a package-level `var ErrX = errors.New("x")` is evaluated once; the compiler may give it counter
  value 0 statically, so sentinels stay static and need no code at initialization.

**Justification.**

- **Goal 2.** Equality, `errors.Is`, map keys and `%v` behave as in Go for every use of `errors.New`.
  The exception disappears.
- **No hidden allocation, goal 3.** Nothing is allocated. The call costs one atomic increment (relaxed
  ordering suffices, since only uniqueness is required), which is available in every build mode,
  including interrupt context and before any allocator exists. Section 15.2's statement that
  `errors.New` allocates nothing stays true.
- **Bounded checks fail loudly (standing principle 4).** A site's counter is exhausted after 2^64
  evaluations; reaching that is a run-time panic, never a wrap that would make two errors equal. At
  one call per nanosecond this takes centuries, so no program meets it, but the rule is stated.
- **Cost.** One small type descriptor and method table per call site (the method returns a constant).
  `errors.New` results are no longer `*errors.Text`; a Plutonium program that asserted
  `err.(*errors.Text)` on an `errors.New` result must use `Error()` or `errors.Is`. Go programs cannot
  name Go's `*errors.errorString`, so none is affected. `fmt`'s `%T` prints `*errors.errorString` for
  these types, the name Go prints, so oracle output matches.

The existing restrictions stay, because the per-site type is created at the call: the text must be a
constant (E2008), and `errors.New` must be called directly, not taken as a value or deferred (E2542).
These are compile errors that explain themselves, so they are visible differences, not false friends;
they move from the A.3 row to A.2.

### Specification changes

- **Section 1.2 (Design rules), rule 2:** the exception list loses static `errors.New` identity (part 4).
- **Section 15.2 (Package initialization):** "`errors.New` and the other forms of section 16.3 ... are
  static and allocate nothing" becomes "allocate nothing and need no allocator".
- **Section 16.2 (Where an error lives), table:** the static-storage row's examples become
  `var ErrClosed = &errors.Text{Msg: "..."}` and fixed messages; `return errors.New("bad header")` moves
  to the data-word row ("`errors.New` results: a per-site type holding a counter").
- **Section 16.3 (Sentinels and `errors.New`):** replace "Each call evaluates to the address of a static,
  read-only `errors.Text{Msg: text}` that belongs to the call site" and the bullets with the rule of (d):
  each call site has its own error type and counter; each evaluation yields a new error, not equal to any
  other result of any call; copies are equal; exhausting a site's counter panics. Keep "never allocates
  and never runs out of memory, and it works before an allocator exists". The bullet on E2542 becomes
  "Since each call site has its own type, `errors.New` must be called directly...". The `&errors.Text`
  sentinel paragraph stays.
- **Section 17.1 (Panics), What panics:** add "Exhausting the counter of an `errors.New` call site
  (section 16.3)."
- **Appendix F.4 (Errors):** rewrite the sentence on static `errors.Text` per call site as in 16.3;
  `errors.Text` stays as the type of static sentinel literals.
- **Appendix A.2 (Removed):** add a row: "`errors.New(s)` with a non-constant `s`; `errors.New` as a value
  or in `defer` | compile error (E2008, E2542) | `fmt.Errorf("%s", s)`; call `errors.New` directly with a
  constant | 16.3".
- **Appendix A.3, Types, maps and channels:** delete the row "`errors.New` returns a new error at every
  call".
- **Error codes:** E2008 and E2542 keep their numbers and rules. Their catalog `Why` texts change from
  "Plutonium makes a static error per call site so that it never allocates" to "Each place that calls
  errors.New has its own error type made when compiling, so it never allocates; each call still returns
  a distinct error, as in Go." No new code; the counter-exhaustion panic has no code (it is a run-time
  panic).

### Implementation consequences

- `internal/lower/errorsnew.go`: emit per call site a type descriptor with an `Error` method returning
  the constant, and a static `uint64` counter; lower each call to an atomic fetch-add on the counter,
  a check for exhaustion, and an interface value of (site type, counter value). Package-level
  initializers may use a constant 0.
- `fmt`: `%T` of a site type prints `*errors.errorString`.
- `internal/diag`: update the E2008 and E2542 texts; add a test that two results of one site compare
  unequal and that copies compare equal, and a Go-oracle program for it.
- Library and tests: find code that asserts `*errors.Text` on `errors.New` results and change it.

---

## 4. Combined text after all three decisions

**Section 1.2 (Design rules), rule 2** reads:

> **No false friends.** Where Plutonium behaves differently from Go in a way a Go programmer would not
> expect, the difference is visible in the source: a different name, a different syntax, a different
> type, or a compile error that explains it. Code that looks like Go and compiles does what Go would do.
> Appendix A lists every difference.

**Appendix A.1 (The no-false-friends rule):** the quoted rule ends at "...a compile error that
explains it." The sentence on approved exceptions is deleted. "The language makes such differences
explicit except for the documented exceptions below" becomes "The language makes every such difference
explicit". The class **Documented exceptions** is deleted from the list, with its sentence about the
oracle subset. The paragraph on the common subset stays.

**Appendix A.5 (The one documented exception):** deleted. No catalog entry cites it (checked:
`internal/diag` has no `pspec("A.5", ...)`); the citations in sections 1.2, 7.3, 11.12, 17.1 and F.6
are removed by parts 1 to 3. `TestSpecReferences` must still pass.

**Error codes, in total:** new E2019 (slice expression on a string); E2008 and E2542 unchanged in
rule, changed in catalog text. No code is retired.

## 5. Questions for the owner

1. **Names from the operating system (A.3).** With strings kept valid UTF-8, `os.Args`, `os.Getenv` and
   directory entries still replace invalid bytes with U+FFFD, and a program that opens `e.Name()` fails
   on a file whose name is not UTF-8 where Go succeeds. The specification lists this only in a table
   (A.5 calls it a deliberate difference), so it is a fourth hidden difference, outside the three of
   this plan. Making it visible needs a type or name change (for example `os.Args` as a different type,
   or the lossy functions renamed) and is a separate decision.
2. **Names of the slicing functions.** `strings.Slice` and `strings.SliceFrom` are proposed; any pair
   works if the error message and the translator agree.
3. **An explicit call to wait for tasks** (`rt.WaitTasks()` or another name) for programs that want
   the old termination behaviour: wanted or not. Not needed for the decision.
4. **Resource limits that show only at run time.** A.3 lists "a program that recurses deeply in a task
   fails at the task's limit" as how a fixed task stack shows. It is not visible in the source either. It
   is of the same kind as Go's own maximum stack size, a resource limit rather than a different meaning,
   but section A.1 does not say how resource limits are classified. A sentence in A.1 ("exhausting a
   documented resource limit is not a false friend") would settle it.
