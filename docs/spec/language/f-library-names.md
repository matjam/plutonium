## F. Library names the language relies on

The standard library is not part of this specification. Each package documents itself (`pluto doc`), and
the verbs of `fmt` or the functions of `os` are found in the package documentation, not here. This appendix lists only the names that the language itself depends on: names
that the compiler calls or checks, names that a rule of the language mentions, and names that an error
sends the programmer to. A conforming implementation provides each of them, in the package shown, with the
meaning given here. Everything else about these packages is for their documentation.

The names fall in two kinds. Some are **called by the compiler**: it generates calls to them or reads
their declarations, and a program calls them directly only to implement a kernel (a scheduler, a panic
handler). The others are **ordinary library names** that the language gives meaning to, such as
`sync.Scope`.

### F.1 The runtime, `pluto/rt`

The runtime is the part of the library that the compiler knows by name. It provides mechanisms, and a
kernel supplies policy through hooks (section 1.2, Design rules). Application code never calls the hooks.

**The panic handler.** The program's panic handler is a function `func(p *rt.Panic)`. The types
`rt.Panic` and `rt.Fault`, with the fields `Message`, `Value`, `Fault`, `CPU`, `Task` and `During`, and the
methods `Print` and `PrintTrace`, are defined in section 17.4 (Panics in freestanding programs).

`rt.Writer` has the method of `io.Writer`. `rt.Console{}` writes where `print` does: in a freestanding
build, the function registered with `rt.SetConsole`, and in a hosted build, standard error with a system
call that never waits. A kernel's exception handler reports a fault with
`rt.PanicFault(kind rt.FaultKind, addr, pc, sp, bp uintptr)`, which never returns. A tool that describes an
error at length starts the panic with `rt.BeginPanic()`, prints with `print`, and raises it with
`rt.Abort()` or `rt.PanicFault`. `rt.SetCPU(n)` tells the runtime which processor the caller is, once
the processor's `GS` base points at its per-CPU block (section 20.9, Per-CPU variables).

**Tasks and the scheduler.** The scheduler contract (section 19.5, The scheduler contract) uses these:

```go
type Scheduler interface {
	NewTask(req StackRequest) *Task // a record with Task at offset 0, and a stack
	FreeTask(t *Task)               // free an exited task, after the switch away from it
	Spawn(t *Task)                  // make a new task runnable
	Park(t *Task)                   // switch away from t, the running task, until it is woken
	Wake(t *Task)                   // make a switched-out task runnable; interrupt-safe
	Yield(t *Task)                  // switch away from t, which stays runnable
}
```

A backend may also have `Init()`, called once before the first task operation; `InInterrupt() bool`, which
lets the runtime refuse an operation that would park in interrupt context; and `Now() int64` and
`ArmTimer(deadline int64)`, which give the runtime a clock and a timer. A program that uses timers needs
both of the last two. The compiler gives each runtime hook that stands for one of these methods (the
body-less functions `schedNewTask`, `schedFreeTask`, `schedSpawn`, `schedPark`, `schedWake`, `schedYield`,
`schedInit`, `schedInInterrupt`, `schedNow` and `schedArmTimer`) a body that calls the method of the
variable marked `//pu:scheduler` directly.

The runtime's other names that a scheduler uses are `rt.Task`, `rt.Context`, `rt.StackRequest`,
`rt.MakeContext`, `rt.Switch`, `rt.Park`, `rt.Ready`, `rt.Preempt`, `rt.Yield`, `rt.DisablePreemption`,
`rt.EnablePreemption`, `rt.BootTask`, `rt.Go(f, size, a)` with `rt.MinStackSize` (8 KiB), `rt.DefaultStackSize`
(64 KiB, the stack of a freestanding or kernel task whose size was neither computed nor asked for,
section 19.1), `rt.Timer`,
`rt.Now`, `rt.ParkUntil`, `rt.RunTimers` and `rt.NoDeadline`. `rt.Go` starts a task with a stack of an
explicit size, as kernels and scheduler binders need; its third argument is optional, and names the
allocator that supplies the stack (the program-wide default if omitted), for a scheduler that takes stack
memory from the allocator a program asks for (section 19.1, Tasks). `rt.StackLeft`, `rt.StackBounds` and
`rt.SetStackBounds` report and record the limits of a stack. `rt.ExitTask` ends the calling task after
running the deferred calls and drops of every frame; `rt.CatchExitTask` lets a runner stop that exit
(section 17.3, Ending a task with rt.ExitTask). `rt.Pinned` hides a pointer from the escape analysis, which
includes the check of the func values that start tasks (section 12.12), and is unsafe code (section 20.1, Package `unsafe`). The compiler gives `rt.Sleep`,
which is declared without a body, its body by name: a direct wait if the program never starts the scheduler,
a parked one otherwise.

**Generation metadata.** A checked freestanding or kernel build calls a function that the program exports
under the symbol `pluto_gen_metadata`, `func(n uintptr) uintptr`, for page-aligned memory that it never
returns; the function must not allocate, and a checked build without it, or with another signature, is
error E2588 (section 21.5). It is the program's name, not the library's, but the language relies on it.

**Hooks the compiler calls.** The compiler generates calls to these functions by name. Their names and
meanings are fixed by this appendix; their signatures are the runtime's business.

| Name | Called for |
|---|---|
| `callPanicHandler` | every panic and fault, to reach the program's panic handler |
| `makechan`, `makechanWith` | `make(chan T, n)` and `make(chan T, n, a)` |
| `chansend1`, `chanrecv1`, `chanrecv2` | send and receive; these may park |
| `closechan`, `chanlen`, `chancap` | `close`, `len` and `cap` of a channel; these never park |
| `selectgo`, `selectnb` | `select` without and with a `default` case |
| `spawnGo` | the `task` statement (the check of its function and func arguments is static, section 12.12) |
| `strRetain`, `strRelease` | copying and dropping a counted string |
| `boxNew`, `boxNewWith`, `boxRetain`, `boxReleaseStrong`, `weakRelease`, `boxFree` | creating, copying and dropping `owned`, `shared`, `synced` and `weak` values |
| `panicStale` | a failed generation or arena epoch check |
| `startTasks` | the first use of the scheduler, which decides how `rt.Sleep` waits |
| `SetThreaded` | switching reference counts to atomic arithmetic when the program becomes multi-threaded |
| `NewEpoch`, `EpochEnded` | the program-wide counter of arena epochs, and the record of the call that last ended one |
| `Type`, `UnionInfo` | reading type descriptors, for printing and equality in the library |
| `ReleaseValues`, `RetainValues`, `MoveValues`, `HoldsStrings` | generic functions that give library containers the compiler's drop and copy code for the values they hold |

### F.2 Allocation, `pluto/alloc` and `pluto/arena`

```go
package alloc

type Allocator interface {
	// Alloc returns size bytes of zeroed memory aligned to align, or nil.
	Alloc(size, align uintptr) unsafe.Pointer
	// Free releases memory returned by Alloc with the same size and align.
	Free(p unsafe.Pointer, size, align uintptr)
}

func Default() Allocator       // the current allocator; nil before SetDefault in a freestanding or kernel build
func SetDefault(a Allocator)   // replace the program-wide default
func Use(a Allocator) Restore  // install a for the calling task until the Restore ends

type Restore owner struct{ /* ... */ }
func (r Restore) Restore()     // consumes r: put back the allocator that was current before Use
```

The **current allocator** (the one `Default()` returns, and the one `new`, `make`, string-making
operations and the library's constructors use when none is given) is the calling task's override, if it
installed one with `Use`, and otherwise the program-wide default. A task starts with no override, so every
task, scoped or not, starts with the program-wide default; a task that wants an arena installs its own
(section 18.9, Allocators). Dropping a `Restore` puts the previous allocator back, so
`defer alloc.Use(&a).Restore()` installs `a` for the rest of the function. The compiler treats what is
allocated through `Default()` while a `Restore` that views a local arena is live as a view of that arena
(section 18.3, Views).

The functions `alloc.Owned[T]()`, `alloc.Shared[T]()` and `alloc.Synced[T]()` allocate a zeroed `T` in a
box. They take an optional allocator argument. `alloc.Raw`, `alloc.New` and `alloc.Free` are the
unchecked layer that owner types are written with (section 20.1).

`arena.NewFixed(capacity uintptr, parent alloc.Allocator = alloc.Default()) Fixed` explicitly allocates
private fixed-capacity backing. `Fixed` is a movable owner; it never grows its payload and returns its
backing on drop; its allocation bookkeeping comes out of that backing, sized when it is constructed, so an
allocation never asks the parent for more and fails when the table is full. `arena.Chunked` grows by
taking chunks from its parent allocator. An arena takes memory back in bulk only: freeing into it takes
nothing back, and `Reset`, `Release(m)` and `Drop` reclaim. Both count their live allocations, every
allocation made through their `Alloc` and not freed yet, and `Reset`, `Release` and `Drop` panic with
"arena has live allocations" while the count is not zero (section 18.9, Allocators, Live allocations).
An arena's `New[T]()` and `Slice[T](n)` methods return views that live until the arena takes their
memory back; they are not counted. For a type with drop glue, `New` and `Slice` of a `Fixed` or
`Chunked` record a drop entry, and `Reset`, `Release` (the entries made after the mark) and `Drop` run
the entries in reverse order of allocation before they check the count (section 18.9, Drop entries). A
`Fixed` keeps its entries in its bookkeeping, so a `New` or `Slice` also fails when that is full. Fixed and Chunked ordinary APIs enforce that lifetime statically or, in a checked build,
at runtime (sections 18.9 and 18.11). Direct raw allocation and unsafe pointer access retain their
manual lifetime obligations (section 18.12). `arena.NewUnsafe(buf []byte) Arena` is a borrowed-buffer
manual allocator. Its constructor and typed allocation/reclamation methods are explicit unsafe
operations; it preserves the caller's buffer and owns no private payload, and it keeps no count of live
allocations and records no drop entries. No arena is safe for unsynchronized simultaneous use by several tasks, except that the
free of an allocation made in a `Fixed` or `Chunked` arena (the release of a counted object, or the drop
of an owner moved to another task) may happen in any task.

### F.3 Iterators and containers

```go
package iter

type Seq[V any] func(yield func(V) bool)
type Seq2[K, V any] func(yield func(K, V) bool)
```

A range over a function (section 12.11, Range over functions) uses these types, and a range over a
container ranges over its `All` method (section 11.11, Indexing library containers). The constraints of
the indexing interface are in `pluto/container`:

```go
type Indexable[K, V any] interface { Index(k K) V; SetIndex(k K, v V) }
type Reader[K, V any] interface    { Index(k K) V }
type Lookup[K, V any] interface    { IndexOk(k K) (V, bool) }
type Sized interface               { Len() int }
type Iterable[K, V any] interface  { All() iter.Seq2[K, V] }
```

The language depends on the method names `Index`, `SetIndex`, `IndexOk`, `Len` and `All`, and on their
forms, not on the constraints; the constraints give generic code a name for a container.

`pluto/vec` has the growable owner `vec.Vec[T]`, created by `vec.New[T]`, by `vec.Make[T](n)` (an owner
of `n` zero elements) and by `vec.WithCapacity[T](c)` (an empty one with room for `c`), each with an
optional allocator. It has the methods `Push`, `Append`, `Insert`, `Replace`, `AppendSeq`, `Len`, `Slice`
(which lends the elements as a slice), `Ptr(i)` (a view of element `i`), `Pop` (the last element and whether
there was one) and `Remove(i)` (which take an element out, moving it to the caller), `Clone` (an explicit
copy with an optional allocator, when `T` is not an owner) and
`Truncate`, and the iterators `All`, `Values` and `Backward`,
which read the length once when the loop starts. `vec.Fixed[T]` is a bounded vector over a caller's array
(`vec.NewFixed(buf[:])`), which never allocates and records what did not fit. `vec.Appender[T]` is the
constraint that both satisfy, and `vec.AppendString` appends a string's bytes. `hash.Map[K, V]` is the
library table for owners: its `Ptr`, `Remove` and `Pop` methods look at or take out an owner, which the
built-in map cannot do (section 7.10, Map types).

### F.4 Errors

`errors.New(text)` is checked by the compiler: `text` must be a constant, and the call must be written as
a call, not taken as a value or deferred. Each call site has its own error type and a counter in static
storage, and each evaluation yields a new error, without allocating: its dynamic type is the site's type
and its data word holds the new counter value (section 16.3, Sentinels and errors.New). `%T` of such an
error prints `*errors.errorString`, the name Go prints. `errors.Text` is a struct with the field
`Msg string`, and `*errors.Text` implements `error`; it is the type of static sentinel literals, not of
`errors.New` results. A sentinel may be written as the literal
`&errors.Text{Msg: "..."}` in a package-level variable. `errors.Is`, `errors.As`, `errors.AsType`,
`errors.Unwrap` and `errors.Join` have the meaning of section 16.6 (Wrapping, Is and As).

### F.5 Synchronization

The compiler recognizes these types in `pluto/sync`:

- `sync.Scope`, an owner that starts tasks that may use the variables of the function that starts them, and
  waits for all of them when its scope ends. `s.Task(f)` starts `f` as a task that `s` waits for, `s.Wait()`
  waits for every task started so far, and `Drop` waits as `Wait` does (section 19.4, Scoped tasks). The
  zero `Scope` is ready to use.
- `sync.Mutex` and `sync.RWMutex`, the locks whose `Lock`, `Unlock`, `RLock`, `RUnlock` and `TryLock`
  the race analysis follows to infer which lock guards which data (section 19.3, Data race freedom). `Lock`
  and `RLock` may park the caller, so they may not be reached from interrupt context (section 20.10).
- `rt.Lock`, the runtime's own lock, which the race analysis follows like `sync.Mutex`. It may be taken in
  interrupt context (section 19.3, Data race freedom; section 20.10), which `sync.Mutex` may not.
- `sync.WaitGroup`, `sync.Once` and `sync.Cond`, which synchronize as in section 19.7 (Memory model). A
  local `sync.WaitGroup` shared with a task by address is an error, because the view of the frame may
  outlive the frame: `sync.Scope` is the type for that.

Every type declared in `pluto/sync` or `pluto/sync/atomic`, and the runtime's own lock `rt.Lock`, is
**synchronized by its own methods**: a view of one may cross between tasks (section 19.2, The sendable
rule). The atomic integer and pointer types of `pluto/sync/atomic` are the types that a package variable
written by several tasks may have instead of a lock.

### F.6 Strings

These functions are named by rules and by errors:

```go
package strings

func FromUTF8(b []byte) (string, error) // a string of b, or an error if b is not valid UTF-8
func FromUTF8Lossy(b []byte) string     // a string of b, each invalid byte replaced by U+FFFD
func Bytes(s string) vec.Vec[byte]      // a copy of the bytes of s, an owner
func Runes(s string) vec.Vec[rune]      // the runes of s, an owner
func Slice[S ~string](s S, i, j int) S  // the bytes from i to j; panics unless both are rune boundaries
func SliceFrom[S ~string](s S, i int) S // Slice(s, i, len(s))
func Truncate(s string, n int) string   // the longest prefix of at most n bytes that ends on a rune boundary

package utf8

func RuneStartBefore(s string, i int) int // rounds the byte offset i down to the start of a rune
```

`strings.Builder` is an owner that builds a string in memory it holds. Its zero value is ready to use
with the task's allocator, and `Init(a)` gives it an allocator. It is an `io.Writer`, `Bytes` lends what
it holds as a slice, and `String` returns a counted string of it, with each byte that does not begin a
valid UTF-8 sequence replaced by U+FFFD.

They are the replacements for the conversions and the string slice expression that Plutonium removes
(appendix A, section A.2). The compiler lowers `Slice` and `SliceFrom` inline, to the checks of section 7.3
(String types) and no call. `strconv.Itoa` is the replacement for
converting an integer to a string with `string(i)`.

### F.7 Time

`time.Sleep` parks the calling task until a deadline, so it may not be reached from interrupt context.
`time.Timer`, `time.Ticker`, `time.After` and `time.Tick` deliver the time on a channel, so a `select`
waits for a timer like any other case. `time.NewTimer` and `time.NewTicker` return an
`owned[Timer]` or `owned[Ticker]`, which stops and frees the timer when it is dropped; `time.AfterFunc`
returns a `synced[Timer]` handle, and dropping it never stops the timer (`Stop` does); `time.After` and
`time.Tick` return a counted channel that owns its timer, stopped and freed with the channel's last
reference (section 19.6, Timers). `time.AfterFunc(d, f)` starts `f` as a task when the timer expires, so
`f` is subject to the rules of the `task` statement for func arguments: a function that may capture
variables from a frame is a compile error (section 12.12). No library type needs a `Free` call. Underneath is the
runtime's mechanism (section F.1): a monotonic clock, `rt.Now()`, timers that never allocate, and
`rt.ParkUntil`. A kernel's scheduler provides the clock and the hardware with `Now` and `ArmTimer`.

### F.8 Machine access

`pluto/arch/x86` is specified in section 20.8. `unsafe` is specified in section 20.1.

### F.9 Operating-system names, `pluto/os`

A name or value that the operating system gives the program is bytes (section 7.3). The names that carry the
rule are these:

```go
package os

func (e *DirEntry) Name() (string, error) // the name, or a *NameError if it is not valid UTF-8
func (e *DirEntry) NameLossy() string     // each invalid byte replaced by U+FFFD; for display only
func (e *DirEntry) RawName() []byte       // the exact bytes
func Getenv(key string) (string, error)   // the value, or a *NameError if it is not valid UTF-8
func GetenvBytes(key []byte) []byte       // the exact bytes
func OpenBytes(name []byte, a alloc.Allocator = alloc.Default()) (*File, error) // opens a name of any bytes

type NameError struct {
	Op    string // the function that found the name, for example "DirEntry.Name"
	Bytes []byte // the name or value, exactly as the system gave it
}
func (e *NameError) Error() string
```

The other functions of `os` that return a name or value from the system as a string follow the same split:
the function with the Go name returns an error as well, a form with `Lossy` in its name returns the display
string, and the exact bytes are available as `RawName`, `GetenvBytes`, `ArgBytes` and the `Bytes` forms of the
file functions. A `*NameError` is a counted error like the other library error types (section 16.7).
The single-value use of a function that returns the error as well is a compile error whose explanation names
`RawName` and `NameLossy`. PlutoniumOS refuses to create a name that is not valid UTF-8, so there the error
cannot occur.
