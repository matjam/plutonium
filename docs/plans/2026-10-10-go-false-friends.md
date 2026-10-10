# Go false friends: an audit

**Date:** 2026-10-10
**Status:** audit and proposals. Nothing here is implemented. Section 5 lists the decisions that need
sign-off.

## 1. The rule

On 2026-10-10 the goal of the language changed from "most Go source compiles unchanged" to **Go-familiar,
not Go-compatible** (language spec, introduction). With it came a design rule:

> **No false friends.** Where Plutonium behaves differently from Go in a way a Go programmer would not
> expect, the difference must be visible in the source: a different name, a different syntax, a
> different type, or a compile error. Code that looks like Go and compiles does what Go would do.

A false friend is Go-looking code that compiles in Plutonium and then does something else. Code that
fails to compile with an explanation is not one: the reader learns about the difference at the line
that has it. Neither is a new type or function that Go does not have, since its documentation is the
first thing a reader looks up.

This audit went through the language spec, the memory model, the accepted designs (memory safety D1 to
D9, owners and moves, counted strings, closure regions, panic aborts, the removal of `append`, scoped
puroutines) and the parts of the standard library that keep Go's names. For each candidate it records
the Go code, what Go does, what Plutonium does, how surprising and how likely the difference is, and a
fix. Where the behavior could be checked, it was: the outputs below come from small programs built with
the compiler on `main` (commit `5001093`) and, where marked, with the compiler of the unlanded
escape-checks branch (`worktree-agent-a1cbe4e24983acabc`), and compared with Go 1.27.

**Risk** combines surprise (would a Go programmer expect this?) with likelihood (how often does the
pattern appear in ordinary code?). A silent wrong result ranks above a leak, a leak above a loud panic,
and a panic above a compile error, which ranks as no risk.

## 2. Summary

| # | Case | Today | Risk | Proposed fix |
|---|---|---|---|---|
| F1 | Loop variables, interface boxes and method values kept across iterations share one slot | silent wrong values on `main`; compile errors on the escape-checks branch | high until the branch lands | land the escape checks |
| F2 | `string(b)` replaces invalid UTF-8, so different bytes give equal strings | silent data change | high | remove the `string([]byte)` conversion; `strings.FromUTF8` or `strings.FromUTF8Lossy` |
| F3 | Deleting from a map while ranging over it skips other entries | silent | high | Go's iteration guarantees for `hash.Map` and the coming `map[K]V` |
| F4 | Deferred calls do not run when the program panics | lost output, cleanup skipped | medium-high | run the panicking puroutine's deferred calls in hosted builds |
| F5 | `t.Fatal` and `t.FailNow` skip the test's deferred calls and drops | cleanup skipped, leaks | medium-high | `FailNow` exits the test's puroutine, running deferred calls and drops |
| F6 | `time.After` and `time.Tick` never free their timer; a `Timer` needs `alloc.Free` | leak in the common `select` idiom | high | the channel owns its timer and frees it with its last reference |
| F7 | Channels are never freed until counting lands | leak | high for long-running programs | land channel counting (roadmap item 48) |
| F8 | Puroutine stacks are a fixed 64 KiB | stack overflow panic where Go grows | medium-high | computed sizes, and large lazily mapped stacks in hosted builds |
| F9 | `bytes.Buffer` is bounded: the zero value holds nothing, and `NewBuffer(b)` treats `b` as empty storage | silent data loss | high where used | make `bytes.Buffer` Go's growing buffer; bounded writes use `vec.Fixed[byte]` |
| F10 | The accepted scoped-puroutines design makes a local `sync.WaitGroup` join at the end of its block | silent change of when a function returns | medium (design only) | `sync.Scope`, a new type; `WaitGroup` keeps Go's behavior |
| F11 | `range` over a `vec.Vec` visits elements pushed during the loop | silent, possible endless loop | medium | read the length once, as `range` over a slice does |
| F12 | `s[i:j]` on a string panics off a rune boundary | loud panic | medium | keep, as a documented exception; a helper that rounds to a boundary |
| F13 | `fmt.Errorf` and `errors.Join` leak unless freed with `errors.Free` | leak | medium | counted errors (spec section 14.6) |
| F14 | A view of an owner's storage reached through a pointer dangles silently after the owner grows | silent use after free | medium | generation checks on by default in hosted builds (memory safety D3(b)) |
| F15 | `os.File` I/O returns a bare `syscall.Errno`, `strconv` no `*NumError` | different `errors.As` result and message | medium-low | Go's error types once errors are counted |
| F16 | `new` and `make` memory is never freed today; under the owner proposal `make([]T, n)` is not a `[]T` | leak today; confusing types later | medium-low | owners as proposed; spell the slice form `vec.Make` |
| F17 | Map iteration order is fixed by a constant seed | no Go program breaks; predictable hashing | low | a random seed per map in hosted builds |
| F18 | An `os.File` closes when its variable goes out of scope | rare surprise | low | keep: the type differs from Go's `*os.File` |
| F19 | `errors.New` returns the same error each time one call site runs | almost never observable | low | keep, documented |
| F20 | Names from the operating system are lossy strings | rare | low | keep, with the `Bytes` forms |
| F21 | Test log lines are cut short; traces say `puroutine` | cosmetic | low | write long lines in pieces; keep the word |

Section 4 lists what was checked and found not to be a false friend, `vec.Fixed`'s truncation among
them.

## 3. The cases

### F1. Loop storage kept across iterations

```go
var fs [3]func() int
for i := 0; i < 3; i++ {
	fs[i] = func() int { return i }
}
println(fs[0](), fs[1](), fs[2]())
```

Go 1.22 gives each iteration its own `i` and prints `0 1 2`. Plutonium's loop variables live in one
frame slot that every iteration reuses (spec section 4.5), and so do the closure record, an interface
box (`xs[i] = Big{i, i, i}`), a method value's receiver copy (`fs[i] = t.Get`) and `&i`. On `main` the
four probes print `3 3 3`, `{2 2 2} {2 2 2} {2 2 2}`, `2 2 2` and `3 3 3`. The escape-checks branch
rejects all four:

```
main.p:6:3: cannot keep the function literal in fs[i] after this iteration: it uses i, which the loop at line 5 reuses in every iteration
main.p:10:3: cannot keep Big{...} in xs[i] after this iteration: the loop at line 9 makes it in the same memory in every iteration
```

**Risk:** high on `main`: the pattern (collecting callbacks, `&item` pointers or boxed values in a loop)
is common and the result is silently wrong. None once the branch lands. **Fix:** land the escape
checks. This is the most urgent item, because every other fix assumes that frame views cannot outlive
their storage.

### F2. `string(b)` changes the bytes

```go
sum := sha256.Sum256(data)
seen[string(sum[:])] = true       // a map keyed by binary digests
s := string([]byte{0x61, 0xff, 0x62})
```

Go keeps the bytes: a Go string is any byte sequence. Plutonium's strings hold valid UTF-8 (memory
safety D9, spec section 6.10), so `string(b)` replaces each invalid byte with U+FFFD. The probe gives
`len(s) == 5` with bytes `[97 239 191 189 98]`, and `string([]byte{0xfe}) == string([]byte{0xff})` is
true. Two different digests can become the same key; a file read into a string and written back is
changed; `[]byte(string(b))` is not `b`.

**Risk:** high. Using `string(b)` as a byte container (map keys of hashes and IDs, binary protocol
fields, file contents) is a common Go idiom, and the result is silently different data.

**Fix:** make the conversion from `[]byte` (and from `vec.Vec[byte]`) to `string` a compile error, with
the replacement chosen by what the code means:

- `strings.FromUTF8(b) (string, error)` (exists) when the bytes should be text;
- `strings.FromUTF8Lossy(b) string`, a new name for today's behavior, when replacement is acceptable;
- an array key (`map[[32]byte]bool`, valid Go) or a byte-slice keyed map when the bytes are binary.

`string(r)` and `string(runes)` stay: they already replace invalid code points in Go. The library's
other byte-to-string paths (`strings.Builder.String` after `Write`, `bytes.Buffer.String`) keep
replacing, since they are fed text far more often than binary data; they are listed as residual risk
and revisited with usage data.

**Alternatives.** Making `string(b)` panic on invalid bytes is loud, but it is a run-time failure where
Go has none. Giving up the UTF-8 invariant reverses D9 and the string-slicing and ASCII-flag work built
on it.

### F3. Deleting during `range` over a map

```go
for k := range m {
	if expired(k) {
		delete(m, k) // m.Delete(k) on a hash.Map
	}
}
```

Go guarantees that every entry present when the loop starts and not deleted before it is reached is
visited exactly once; entries added during the loop may or may not be. `hash.Map.All` documents that
"adding or deleting keys during the loop is safe but may move entries, which the loop then visits
twice or not at all". In the probe (1,000 keys, deleting each visited key divisible by 3), the loop
visited between 900 and 917 keys instead of 1,000.

**Risk:** high. Deleting while ranging is the standard Go way to filter a map, and the counted
`map[K]V` that is coming back (with Go's reference semantics) will inherit the iterator.

**Fix:** adopt Go's iteration guarantees as the specification of `hash.Map`, `hash.Set` and `map[K]V`:
a deletion never moves another entry (it leaves an empty or deleted mark in place, as Go's swiss tables
do), and when the table grows during an iteration the iterator keeps walking the old table and
looks each key up in the new one, as Go's iterator does. The cost is a probe sequence that skips deleted marks until the
table is next rebuilt, and the old table kept alive while an iteration that saw it is running.

### F4. Deferred calls on panic

```go
w := bufio.NewWriter(os.Stdout)
defer w.Flush()
defer os.RemoveAll(tmp)
defer term.Restore(fd, old)
process(items) // panics on bad input
```

When a goroutine panics and nothing recovers, Go runs that goroutine's deferred calls, innermost first,
and then prints the panic and exits with status 2. Plutonium's panic aborts at once (spec sections 4.1
and 8): buffered output is lost, temporary files stay, a terminal stays in raw mode, and a deferred log
line never appears. The probe's `defer fmt.Println("deferred ran")` prints nothing before the panic.

**Risk:** medium-high. The program fails either way, but what it leaves behind differs, and `defer` for
cleanup is everywhere in Go.

**Fix:** in hosted builds, the built-in panic handler runs the deferred calls of the panicking
puroutine, innermost frame first, before it prints the panic and exits; a program's own `//pu:panic`
handler may do the same with `p.RunDeferred()`. This keeps the accepted decision that panic aborts:
there is no `recover`, control never returns to a frame that panicked, owners are not dropped, and other
puroutines' deferred calls do not run, as in Go. A panic inside a deferred call during this, and a stack
overflow, abort at once (Go reports a stack overflow as a fatal error without deferred calls too).
Freestanding and kernel builds keep today's behavior unless their handler asks.

The frame walk already exists for stack traces. Each defer statement has one slot in its frame (spec
section 4.10), so a table per function of its slots and the instructions where each is armed lets the
panic path find and call the armed ones. Nothing changes on paths that do not panic.

### F5. `t.Fatal` skips deferred calls

```go
func TestServer(t *testing.T) {
	dir := t.TempDir()
	srv := start(dir)
	defer srv.Close()
	if err := srv.Ping(); err != nil {
		t.Fatal(err)
	}
}
```

Go's `FailNow` (and `Fatal`, `Skip` and their relatives) calls `runtime.Goexit`, which runs every
deferred call of the test's goroutine. Plutonium's `FailNow` jumps back to the runner with `longjmp`
(`pluto/testing`): deferred calls do not run, owners in the frames it leaves are not dropped, and a
server or child process started by the test keeps running for the rest of the test binary.

**Risk:** medium-high in tests, where `t.Fatal` after a `defer` is the normal shape.

**Fix:** a runtime primitive, `rt.Goexit`, that ends the calling puroutine after running the deferred
calls and drops of every frame on it, innermost first, using the tables of F4 and the drop sites that
lowering already knows. Each test then runs on its own puroutine, as in Go, and `FailNow` calls
`rt.Goexit`. This is an unwinder for a controlled exit only (it never runs from a fault, and it never
resumes a frame), so it does not reopen `recover`. It also gives the scoped-puroutines design its
non-local exit (that plan's D7): leaving a frame drops its `sync.Scope`, which waits.

### F6. Timers that are never freed

```go
for {
	select {
	case m := <-msgs:
		handle(m)
	case <-time.After(time.Second):
		heartbeat()
	}
}
```

Since Go 1.23 an unreferenced timer and its channel are collected even if the timer has not fired.
Plutonium's `time.After` and `time.Tick` "allocate a timer that is never freed" (spec section 4.7), and a
`time.Timer` or `Ticker` must be freed with `alloc.Free`. The loop above leaks a timer per message.

**Risk:** high: `time.After` in a `select` loop is one of Go's most common idioms.

**Fix:** once channels are counted (F7), the channel owns its timer: when the last reference to a
timer's channel goes, the timer is stopped and freed with it. `time.After` then frees itself when the
`select` drops its temporary, `Timer` and `Ticker` hold a reference to their channel like any other
value, and `alloc.Free(t)` goes away. The runtime's own pointer from the timer heap to the channel is
not counted.

### F7. Channels are never freed

`make(chan T)` allocates a channel that lives until `alloc.FreeChan` (spec section 4.8); Go code never
calls it, so a server that makes a channel per request grows without bound. The counted design is
accepted and specified; it is waiting on implementation (roadmap item 48).

**Risk:** high for long-running programs; none for short ones. **Fix:** land counting before declaring
the concurrency story Go-familiar. Until then the debug allocator's leak report lists the channels.

### F8. Fixed puroutine stacks

```go
go func(c chan int) { c <- depth(20000) }(done) // recursion about 3 MB deep
```

Go's goroutine stacks grow, up to 1 GB. A puroutine has a fixed 64 KiB stack with a guard page (spec
section 4.7); the probe panics with `runtime error: stack overflow` in `puroutine 2`. The same happens to
a puroutine with a large local array (`var buf [128 << 10]byte`), which Go would move to the heap.

**Risk:** medium-high: recursive parsers, tree walks and per-connection buffers in goroutines are
common, and the failure appears only on large inputs.

**Fix:**

1. The planned stack analysis (roadmap item 18): a spawned function whose call graph is bounded gets
   exactly the stack it needs, computed at compile time, including its locals.
2. In hosted builds, a puroutine whose need cannot be computed (recursion, calls through func values or
   interfaces) gets a large stack reserved and mapped lazily with a guard region below, as `//pu:stack`
   already does for `main`. Pages are committed only as they are touched, so the cost is address space,
   not memory. A 64 MiB reservation allows about two million such puroutines in a 47-bit address space.
3. The overflow message names the size the puroutine had and how to ask for more (`rt.Go(f, size)`).

Kernel builds keep fixed stacks, sized by the analysis or by `rt.Go`, and `-stackreport`.

### F9. `bytes.Buffer` is bounded

```go
var b bytes.Buffer
b.WriteString("hello") // Go: always succeeds, the error is always nil
r := bytes.NewBuffer(data) // Go: a reader over data
```

Plutonium's `bytes.Buffer` stores into a fixed slice and never grows. Its zero value has no storage,
so every write returns `ErrFull`, which Go code ignores because Go documents the error as always nil.
`NewBuffer(buf)` keeps the same signature as Go's but uses `buf` as empty storage, so a reader over
`data` reads nothing.

**Risk:** high where `bytes.Buffer` is used, and it is used widely in Go.

**Fix:** make `bytes.Buffer` Go's growing buffer, an owner that records its allocator like
`strings.Builder`, with Go's `NewBuffer` and `NewBufferString`. Bounded writes into a caller's array use
`vec.Fixed[byte]` (removal-of-`append` plan, section 3.2). This settles the question that plan left
open.

### F10. A WaitGroup that joins at the end of its block

The accepted scoped-puroutines design (decision D1(b')) made a local `sync.WaitGroup` wait for its
puroutines at the end of its block, so that Go's idiom would compile unchanged. A Go programmer's
`WaitGroup` never waits on its own: a function that returns early without `Wait` returns at once in Go,
and in that design it blocks. Under the new rule this is a false friend. The plan is revised in place
(`2026-10-10-scoped-puroutines.md`): scoped puroutines are started by a new type, `sync.Scope`, whose
name and documentation say that it joins, and `sync.WaitGroup` keeps Go's behavior and today's escape
errors.

### F11. `range` over a `vec.Vec` sees pushes

```go
for _, x := range work {     // work is a vec.Vec[int]
	if x < 100 {
		work.Push(x * 10)
	}
}
```

In Go, `range` over a slice evaluates the slice once, so its length is fixed when the loop starts, and
`s = append(s, y)` in the body never extends the loop. `vec.Vec.All` reads the vector as it goes and
visits elements pushed during the loop: the probe that pushes while ranging over two elements runs 7
iterations, where the slice version runs 2. With `append` removed, the Go programmer's rewrite of
`s = append(s, y)` is `s.Push(y)`, so the loop keeps its Go shape and changes its meaning.

**Risk:** medium; it can turn a finite loop into an endless one.

**Fix:** `All`, `Values` and `Backward` read the length once when the loop starts, as `range` over a
slice does, and read each element when they reach it; they stop early if the vector has shrunk below
the next index, instead of panicking. A worklist that should see its own pushes is written with an index
loop, `for i := 0; i < v.Len(); i++`, which is what Go programmers already write for that.
`ring[T, N]`'s range, which visits elements pushed during the loop, is not a Go type and keeps its
documented behavior.

### F12. Slicing a string off a rune boundary

```go
if len(title) > 40 {
	title = title[:40] + "..."
}
```

Go slices bytes and may leave half a rune at the end. Plutonium panics: `slice bound 2 is not at the
start of a rune: a string holds valid UTF-8, so it can only be sliced between runes` (spec section
6.10). ASCII text never panics; the first title with an accented letter in the wrong place does.

**Risk:** medium: the failure is loud and names its cause, but it depends on the input.

**Recommendation: keep the panic, as a documented exception to the rule.** The alternatives are worse:

- a compile error wherever the compiler cannot prove a bound is a boundary would flag most ASCII parsing
  code (`line[:4]`, `s[i+2:]`), and the proof lives in the optimizer, so whether a program compiled
  would depend on `-O`, which the memory safety design rules out;
- rounding the bound down to a boundary is silent, the very thing the rule forbids;
- slicing bytes as Go does requires strings that may hold invalid UTF-8, which reverses D9.

The panic is of the same kind as Go's own `slice bounds out of range`: an index that does not fit the
data, reported at the line. Add `strings.Truncate(s, n)` (or a `utf8` helper that rounds a byte offset
down to a rune start) and name it in the panic's message.

### F13. Allocated errors leak

`fmt.Errorf`, `errors.Join` and the `*os.PathError` of a failed `os.Open` are allocated and freed only by
`errors.Free` (spec section 14.3). Go code never calls it, so a long-running program that wraps errors
leaks a little on every failure. **Risk:** medium. **Fix:** adopt the counted errors of spec section
14.6, which release each allocated error exactly once with no change to the code that handles it.

### F14. Views of an owner's storage reached through a pointer

```go
first := ix.words.Slice() // ix is a *Index; words is a vec.Vec[string]
ix.Add("x")               // Push may grow words and free the old array
fmt.Println(first[0])
```

In Go, a slice taken before an `append` that grows keeps the old array alive. In Plutonium the old array
is freed when the vector grows. For a local owner the compiler rejects the later use (spec section 6.3);
through a pointer it cannot, and without `--gencheck` the read hits freed memory silently. **Risk:**
medium. **Fix:** the accepted path of memory safety D3: generation checks become the default in hosted
builds once loop hoisting and `SyncSlab` support land. Until then the debug allocator catches it.

### F15. Error types in `os` and `strconv`

`os.File.Read`, `Write` and `Close` return a bare `syscall.Errno`, not a `*os.PathError`, and `strconv`
returns `ErrSyntax` and `ErrRange` without a `*NumError` (spec section 14.5). `errors.As(err, &perr)` is
false where Go's is true, and `err.Error()` reads `is a directory` where Go prints `read data: is a
directory`. `errors.Is(err, strconv.ErrSyntax)` works in both. **Risk:** medium-low. **Fix:** once
errors are counted (F13), return Go's types, allocated only on failure as `os.Open` already does; `io.EOF`
and `os.ErrClosed` stay sentinels. The reason for the bare codes, no allocation on the I/O path, holds
only for the success path, which does not change.

### F16. `new` and `make`

Today `new(T)` returns a `*T` and `make([]T, n)` a `[]T` that nothing frees: Go code compiles and leaks
(spec section 4.2). The owner proposal makes `new` return `owned[T]` and `make([]T, n)` a `vec.Vec[T]`.
Every difference then shows as a compile error (a use after a move, a `vec.Vec[int]` passed where a
`[]int` is expected), so neither is a false friend. But `make([]int, n)` would produce a value that is
not a `[]int`, and the errors would talk about a type the source never names.

**Fix:** adopt the owner proposal for `new`, whose result dereferences like `*T` and keeps Go's meaning.
For slices, make `make([]T, ...)` a compile error whose fix is `vec.Make[T](n)` (or `vec.WithCapacity`),
as the removal of `append` does for growth: the source then names the type it gets. `make` keeps
channels (and maps, when they return).

### F17. Map iteration order and the hash seed

Go randomizes iteration order. `hash.Map` iterates in table order with a constant seed
(`hash.DefaultSeed`), so the same program visits keys in the same order on every run (the probe prints
`bfcdahge` every time). No correct Go program depends on random order, so this breaks nothing. But a
constant seed makes hashing predictable to an attacker who chooses keys (hash flooding), which Go's per
map random seed prevents. **Fix:** in hosted builds, seed each map from a random value the runtime reads
at startup (the kernel's `AT_RANDOM` bytes), mixed with a counter; keep `InitSeed` for reproducible
builds and kernels.

### F18. An `os.File` closes at the end of its scope

`os.File` is an owner, dropped (and closed) when its variable's scope ends. Go's `*os.File` stays open
until closed or finalized, so code that takes `f.Fd()` and returns the number keeps a valid descriptor in
Go and a closed one here. **Risk:** low: the type differs (`os.Open` returns an `os.File`, not a
pointer), so signatures that pass it on change, and passing the descriptor number out is rare. Keep.

### F19. `errors.New` per call site

`errors.New("x")` returns the same error every time its call site runs (spec section 14.1); Go returns a
new one. Only a program that compares two results of one call site can tell. **Risk:** low. Keep; it is
what lets `errors.New` work without allocation.

### F20. Names from the operating system

`DirEntry.Name`, `os.Getenv`, `os.Args` and their relatives replace invalid UTF-8 (spec section 13.1),
so a program that opens `e.Name()` fails on a file whose name is not UTF-8, where Go succeeds. **Risk:**
low: such names are rare, the failure is an error from `Open`, and the exact bytes are next to every
lossy string (`RawName`, `OpenBytes`). Keep.

### F21. Small differences in output

`testing` formats log lines into fixed buffers and cuts long ones short; Go prints them whole. Stack
traces and the deadlock message say `puroutine` where Go says `goroutine`, which matters only to tools
that parse Go's text. **Risk:** low. Write long log lines in pieces; keep the word, which says what the
thing is.

## 4. Checked and not false friends

| Area | Finding |
|---|---|
| Integers | Wrapping, shifts and division match Go: `MinInt64 / -1` gives `MinInt64` and `% -1` gives 0, `1 << 70` is 0, a negative shift count panics with `negative shift amount` (probes). |
| Interface equality | Two interfaces holding equal structs, or equal strings built differently, compare equal, as in Go (probe). Boxes in the frame do not change equality. |
| `fmt` | `%v`, `%+v`, `%#v`, `%T`, `%q`, `%x`, widths, precisions and flags printed the same as Go for a sample of pointers, structs, nil slices and errors, floats, `Stringer`s and padded strings (probe). Kernel builds print floats as bits and may strip field names; that is not Go code. |
| `select` | Picks among ready cases at random (`rt.selectOrder` shuffles the cases), as in Go. |
| Channels, `close`, nil channels | As in Go (spec section 4.8), apart from freeing (F7). |
| Memory model | For code in the common subset it is Go's: same synchronizing operations, sequentially consistent atomics, races that may tear multi-word values. The additions (packed values as one location, `volatile`, interrupts, `atomic.Fence`) concern types and code Go does not have. A racy read of a string may see a freed buffer where Go sees a torn string; both are races. |
| Preemption and scheduling | Asynchronous preemption every 10 ms, a worker per CPU, work stealing and the deadlock report, as in Go. |
| Strings: `len`, indexing, `range`, comparison, `string(r)`, `string(runes)` | As in Go (spec section 6.10). |
| Invalid string literals (`"\xff"`) | Compile error. |
| Closures and method values | Capture by reference and copy the receiver as Go does. Outliving the frame is a compile error (with the escape checks; see F1 for the loop case). |
| Capturing literals with `go` | Compile error (E2590) with a fix. |
| Interface boxes in the frame | Returning or storing one where it outlives the frame is a compile error (spec section 4.4); within one function, see F1. |
| Copying a struct that holds an owner | The struct must be declared `owner`; a use after the move is a compile error that names the move. |
| `append`, `recover`, `reflect`, complex numbers, today's `map` | Compile errors with replacements. |
| `defer` in a loop | Compile error. |
| nil slices and interfaces | As in Go. When `map[K]V` returns, a nil map should behave as Go's: reads give zero values and writes panic. |
| `vec.Fixed` truncation | Not Go code: the bound is chosen where a type Go does not have is declared, and `Truncated()` reports a loss. Functions that must not lose bytes check and return an error (removal-of-`append` plan, section 3.2). |
| Owners in ordinary interfaces | Converting lends; asserting an owner back out is a compile error (spec section 4.4). |

## 5. Decisions

1. **Order of work.** Land the escape checks first (F1); every other frame rule assumes them.
2. **F2.** Remove the conversion from `[]byte` to `string`, with a compile error naming
   `strings.FromUTF8` and a new `strings.FromUTF8Lossy`. Recommended.
   **Implemented** (E2015, language spec section 3 and 6.10, roadmap item 60). The conversion from a
   `vec.Vec[byte]` needs no rule of its own: a `Vec` does not convert to `string`.
3. **F3.** Go's map iteration guarantees become the specification of `hash.Map`, `hash.Set` and the
   coming `map[K]V`. Recommended.
4. **F4.** In hosted builds a panic runs the panicking puroutine's deferred calls before the handler
   prints it; no `recover`, no drops, other puroutines untouched; `p.RunDeferred()` for a program's own
   handler. Recommended. This amends "panic aborts" only in that deferred calls run.
   **Implemented** (language spec section 8.2). The runtime runs the deferred calls before calling any
   hosted handler, the program's own included, so `p.RunDeferred()` is not needed and does not exist.
5. **F5.** Add `rt.Goexit`, which runs deferred calls and drops of every frame of the puroutine and ends
   it; run each test on its own puroutine; `FailNow` calls `Goexit`. Recommended.
   **Implemented** (language spec section 8.3), with one change: tests still run one after another on
   the task that runs main, and the runner stops the exit with `rt.CatchGoexit`. A task per test would
   start the scheduler in every test binary and run tests on 64 KiB stacks (F8); what Go programmers
   rely on, deferred calls and drops running when a test stops, holds either way.
6. **F6, F7.** Channel counting before more concurrency work, then the channel owns its timer and
   `alloc.Free` of timers and `alloc.FreeChan` go away. Recommended.
7. **F8.** Computed stack sizes where the call graph allows (roadmap item 18), and a 64 MiB lazily
   mapped stack for the rest in hosted builds. Recommended; the size is open.
8. **F9.** `bytes.Buffer` becomes Go's growing buffer; bounded writes use `vec.Fixed[byte]`.
   Recommended.
9. **F10.** Scoped puroutines through `sync.Scope`; `sync.WaitGroup` keeps Go's behavior. See the
   revised scoped-puroutines plan.
10. **F11.** Range over `vec.Vec` reads the length once. Recommended.
11. **F12.** Keep the panic on slicing a string off a rune boundary as a documented exception to the rule,
    and add a truncation helper named in the panic message. Recommended; this is the one case where the
    rule is not met, so it needs explicit sign-off.
12. **F13, F15.** Adopt counted errors (spec section 14.6), then return Go's `*PathError` and `*NumError`.
    Recommended.
13. **F14.** No new decision: memory safety D3(b) covers it. Its priority rises.
14. **F16.** `new` returns `owned[T]` as proposed; `make([]T, ...)` becomes a compile error pointing at
    `vec.Make`. Recommended.
15. **F17.** A random per-map seed in hosted builds. Recommended.
16. **F18 to F21.** Keep, with the small fixes listed.
