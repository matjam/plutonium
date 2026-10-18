# Library cheat sheet

The calls programs use most, with the shapes that differ from Go. Every name here is checked against the library
by the skill's test. The complete lists are `stdlib.md`, `stdlib-os-time-math.md` and
`stdlib-memory-concurrency.md`; the full comment of a name is `pluto doc pluto/strings Cut` or
`pluto doc pluto/vec Vec.Push`. An optional last parameter `a alloc.Allocator = alloc.Default()` is written
`[a]` below: leave it out for the current allocator, pass `&arena` to allocate there.

## Growing sequences and tables

| Call | Result and notes |
|---|---|
| `vec.New[T]([a])`, `vec.Make[T](n [, a])`, `vec.WithCapacity[T](n [, a])` | an empty `vec.Vec[T]`, one of `n` zeros, one with room for `n` (an owner) |
| `vec.NewFixed(buf[:])` | a `vec.Fixed[T]` over your array: never allocates, `Push` drops what does not fit (`Truncated()`), `TryPush` refuses |
| `v.Push(x)`, `v.Append(xs...)`, `v.Insert(i, xs...)`, `v.Replace(i, j, xs...)` | grow; may invalidate `v.Slice()` and `v.Ptr(i)` |
| `v.Pop() (T, bool)`, `v.Remove(i) T`, `v.SwapRemove(i) T`, `v.Truncate(n)`, `v.Clear()` | shrink |
| `v.Len()`, `v.Cap()`, `v.Last() (T, bool)`, `v.At(i)`, `v.Set(i, x)`, `v[i]`, `len(v)` | read and write (bounds checked) |
| `v.Slice() []T`, `v.Ptr(i) *T` | views of the elements, valid until the next growth |
| `v.Clone([a])`, `v.Reserve(n)`, `v.Free()` | copy, make room, release now (it is dropped at scope end anyway) |
| `v.All()`, `v.Values()`, `v.Backward()` | iterators; `for i, x := range v` uses `All` |
| `hash.NewMap[K, V](n [, a])` | a `hash.Map`: `m.Get(k) (V, bool)`, `m.Set(k, v)`, `m.Delete(k) bool`, `m.Has(k)`, `m.Ptr(k) *V`, `m.Upsert(k) (*V, bool)`, `m.Len()`, `m.Keys()`, `m.Values()`, `m.All()`, `m.Clear()`, `m[k]`, `m[k] = v` |
| `hash.NewSet[K](n [, a])` | `s.Add(k) bool`, `s.Has(k)`, `s.Delete(k) bool`, `s.Len()`, `s.All()` |
| `slices.Sort(x)`, `slices.SortFunc(x, func(a, b T) int)`, `slices.BinarySearch(x, t)`, `slices.Contains`, `slices.Index`, `slices.Reverse`, `slices.Max`, `slices.Min`, `slices.Equal` | in place on a slice (`v.Slice()`) |
| `slices.Clone(s)`, `slices.Concat(a, b)`, `slices.Collect(seq)`, `slices.Delete(s, i, j)` | the first three return a `vec.Vec`; `Delete` works in place and returns the shorter slice |
| `sort.Ints`, `sort.Strings`, `sort.SortFunc(x, func(a, b T) bool)`, `cmp.Compare(a, b)` | sorting by a less function; three-way compare |

## Text

| Call | Notes |
|---|---|
| `strings.Contains`, `Index`, `LastIndex`, `HasPrefix`, `HasSuffix`, `Count`, `EqualFold`, `Compare` | as Go |
| `strings.TrimSpace`, `Trim`, `TrimPrefix`, `TrimSuffix`, `TrimLeft`, `TrimRight`, `ToUpper`, `ToLower` | as Go (case mapping is ASCII only today) |
| `strings.Replace(s, old, new, n)`, `ReplaceAll`, `Repeat`, `Join(elems, sep)` | return a new counted string |
| `strings.Cut(s, sep) (before, after, found)`, `CutPrefix`, `CutSuffix` | substrings share the buffer |
| `for p := range strings.SplitSeq(s, sep)`, `FieldsSeq(s)`, `FieldsFuncSeq(s, f)`, `Lines(s)` | iterate pieces; `SplitInto(s, sep, out []string) int` fills your slice |
| `strings.Slice(s, i, j)`, `SliceFrom(s, i)`, `Truncate(s, n)` | substrings on rune boundaries |
| `strings.Bytes(s) vec.Vec[byte]`, `Runes(s) vec.Vec[rune]`, `FromUTF8(b) (string, error)`, `FromUTF8Lossy(b)` | conversions to and from bytes and runes |
| `var b strings.Builder`; `b.WriteString(s)`, `WriteByte`, `WriteRune`, `b.String()`, `b.Len()`, `fmt.Fprintf(&b, ...)` | zero value ready |
| `strconv.Itoa(i)`, `Atoi(s) (int, error)`, `ParseInt(s, base, bits)`, `ParseUint`, `ParseFloat(s, bits)`, `FormatInt(i, base)`, `FormatFloat`, `Quote`, `Unquote` | `Append...` forms write into a `vec.Vec[byte]` or `vec.Fixed[byte]` |
| `unicode.IsLetter`, `IsDigit`, `IsSpace`, `IsUpper`, `IsLower`, `IsPunct`, `ToUpper`, `ToLower`; `utf8.RuneCountInString`, `RuneLen`, `DecodeRuneInString`, `ValidString` | rune classification |
| `fmt.Println`, `Printf`, `Sprintf`, `Sprint`, `Errorf`, `Fprintf(w, ...)`, `Fprintln`, `Appendf(&buf, ...)` | `Print*` write to stdout; `Sprint*` allocate a string; `Fprintf(os.Stderr, ...)` for stderr |

## Errors, files, processes, time

| Call | Notes |
|---|---|
| `errors.New("constant")`, `errors.Is(err, target)`, `errors.As(err, &target)`, `errors.AsType[E](err)`, `errors.Join(a, b)`, `errors.Unwrap(err)` | `fmt.Errorf("ctx: %w", err)` wraps |
| `os.Args() vec.Vec[string]`, `os.Arg(i)`, `os.NArgs()`, `os.Getenv(k)`, `os.LookupEnv(k)`, `os.Exit(n)` | `os.Exit` drops nothing and runs no deferred call |
| `os.ReadFile(path) (vec.Vec[byte], error)`, `os.WriteFile(path, []byte, 0o644)`, `os.Open`, `os.Create`, `os.OpenFile(path, flags, perm)` | files are `os.File` owners that close when dropped (`f.Close()` to close early and see the error); `f.Read(buf[:])`, `f.Write`, `f.WriteString`, `f.Seek`, `f.Stat()`; `&f` is an `io.Reader` and `io.Writer` |
| `os.Stat`, `os.ReadDir`, `os.Mkdir`, `os.MkdirAll`, `os.MkdirTemp`, `os.Remove`, `os.RemoveAll`, `os.Rename`, `os.TempDir()` | errors are `*os.PathError`; `errors.Is(err, os.ErrNotExist)` |
| `os.Stdin`, `os.Stdout`, `os.Stderr` | `*os.File` pointers to files that are never dropped; `io.ReadAll(os.Stdin)` returns a `vec.Vec[byte]` |
| `io.ReadAll(r)`, `io.ReadFull`, `io.WriteString(w, s)`, `io.EOF` | the `io.Reader` and `io.Writer` interfaces are Go's |
| `filepath.Join`, `Base`, `Dir`, `Ext`, `Clean`, `Abs`, `Rel`, `WalkDir` | `pluto/path/filepath` |
| `flag.NewFlagSet(name, flag.ContinueOnError)`; `fs.String`, `Int`, `Bool`, `Duration`; `fs.Parse(args)`, `fs.Args()` | pass `os.Args().Slice()[1:]` |
| `time.Now()`, `time.Since(t)`, `time.Sleep(d)`, `time.After(d)`, `time.NewTimer(d)`, `time.NewTicker(d)`, `time.Millisecond`, `time.ParseDuration(s)` | `t.Sub(u)`, `t.Unix()`, `t.RFC3339()` |
| `math.Sqrt`, `Pow`, `Abs`, `Floor`, `Ceil`, `Round`, `Inf(sign)`, `IsNaN`, `math.MaxInt64`, `math.Pi` | `float64` only; `min` and `max` are built in |

## Concurrency and testing

| Call | Notes |
|---|---|
| `var s sync.Scope`; `s.Task(func(){...})`, `s.Wait()` | tasks that may use the caller's variables |
| `sync.Mutex` (`Lock`, `Unlock`, `TryLock`), `sync.RWMutex` (`RLock`, `RUnlock`), `sync.Once.Do(f)`, `sync.WaitGroup` (`Add`, `Done`, `Wait`) | a local `WaitGroup` may be used by `Scope` tasks, but `task f(&wg)` with a local is E3004 |
| `atomic.Int64`, `atomic.Int32`, `atomic.Uint64`, `atomic.Bool` with `Load`, `Store`, `Add`, `CompareAndSwap` | `atomic.AddInt64(&x, 1)` functions exist too |
| `make(chan T, n)`, `ch <- v`, `<-ch`, `close(ch)`, `for v := range ch`, `select` | owners sent are moved |
| `alloc.Owned[T]()`, `alloc.Shared[T]()`, `alloc.Synced[T]()`, `new(T{...})` | boxes; `alloc.Use(&a).Restore()` installs an allocator |
| `arena.NewFixed(n)`, `arena.NewChunked()`; `a.New[T]()`, `a.Slice[T](n)`, `a.Mark()`, `a.Release(m)`, `a.Reset()` | arena views are valid until reset or drop |
| `func TestXxx(t *testing.T)`: `t.Errorf`, `t.Fatalf`, `t.Fatal`, `t.Log`, `t.Run(name, func(t *testing.T){...})`, `t.Helper()`, `t.Skip` | in `*_test.p` files of the package; `pluto test` |

```pluto-names
vec.New
vec.Make
vec.WithCapacity
vec.NewFixed
vec.Fixed.TryPush
vec.Fixed.Truncated
vec.Vec.Push
vec.Vec.Append
vec.Vec.Insert
vec.Vec.Replace
vec.Vec.Pop
vec.Vec.Remove
vec.Vec.SwapRemove
vec.Vec.Truncate
vec.Vec.Clear
vec.Vec.Len
vec.Vec.Cap
vec.Vec.Last
vec.Vec.At
vec.Vec.Set
vec.Vec.Slice
vec.Vec.Ptr
vec.Vec.Clone
vec.Vec.Reserve
vec.Vec.Free
vec.Vec.All
vec.Vec.Values
vec.Vec.Backward
hash.NewMap
hash.NewSet
hash.Map.Get
hash.Map.Set
hash.Map.Delete
hash.Map.Has
hash.Map.Ptr
hash.Map.Upsert
hash.Map.Len
hash.Map.Keys
hash.Map.Values
hash.Map.All
hash.Map.Clear
hash.Set.Add
hash.Set.Has
hash.Set.Delete
hash.Set.Len
hash.Set.All
slices.Sort
slices.SortFunc
slices.BinarySearch
slices.Contains
slices.Index
slices.Reverse
slices.Max
slices.Min
slices.Equal
slices.Clone
slices.Concat
slices.Collect
slices.Delete
sort.Ints
sort.Strings
sort.SortFunc
cmp.Compare
strings.Contains
strings.Index
strings.LastIndex
strings.HasPrefix
strings.HasSuffix
strings.Count
strings.EqualFold
strings.Compare
strings.TrimSpace
strings.Trim
strings.TrimPrefix
strings.TrimSuffix
strings.TrimLeft
strings.TrimRight
strings.ToUpper
strings.ToLower
strings.Replace
strings.ReplaceAll
strings.Repeat
strings.Join
strings.Cut
strings.CutPrefix
strings.CutSuffix
strings.SplitSeq
strings.FieldsSeq
strings.FieldsFuncSeq
strings.Lines
strings.SplitInto
strings.Slice
strings.SliceFrom
strings.Truncate
strings.Bytes
strings.Runes
strings.FromUTF8
strings.FromUTF8Lossy
strings.Builder.WriteString
strings.Builder.WriteByte
strings.Builder.WriteRune
strings.Builder.String
strings.Builder.Len
strconv.Itoa
strconv.Atoi
strconv.ParseInt
strconv.ParseUint
strconv.ParseFloat
strconv.FormatInt
strconv.FormatFloat
strconv.Quote
strconv.Unquote
unicode.IsLetter
unicode.IsDigit
unicode.IsSpace
unicode.IsUpper
unicode.IsLower
unicode.IsPunct
unicode.ToUpper
unicode.ToLower
unicode/utf8.RuneCountInString
unicode/utf8.RuneLen
unicode/utf8.DecodeRuneInString
unicode/utf8.ValidString
fmt.Println
fmt.Printf
fmt.Sprintf
fmt.Sprint
fmt.Errorf
fmt.Fprintf
fmt.Fprintln
fmt.Appendf
errors.New
errors.Is
errors.As
errors.AsType
errors.Join
errors.Unwrap
os.Args
os.Arg
os.NArgs
os.Getenv
os.LookupEnv
os.Exit
os.ReadFile
os.WriteFile
os.Open
os.Create
os.OpenFile
os.File.Close
os.File.Read
os.File.Write
os.File.WriteString
os.File.Seek
os.File.Stat
os.Stat
os.ReadDir
os.Mkdir
os.MkdirAll
os.MkdirTemp
os.Remove
os.RemoveAll
os.Rename
os.TempDir
os.Stdin
os.Stdout
os.Stderr
os.PathError
os.ErrNotExist
io.ReadAll
io.ReadFull
io.WriteString
io.EOF
path/filepath.Join
path/filepath.Base
path/filepath.Dir
path/filepath.Ext
path/filepath.Clean
path/filepath.Abs
path/filepath.Rel
path/filepath.WalkDir
flag.NewFlagSet
flag.ContinueOnError
flag.FlagSet.String
flag.FlagSet.Int
flag.FlagSet.Bool
flag.FlagSet.Duration
flag.FlagSet.Parse
flag.FlagSet.Args
time.Now
time.Since
time.Sleep
time.After
time.NewTimer
time.NewTicker
time.Millisecond
time.ParseDuration
time.Time.Sub
time.Time.Unix
time.Time.RFC3339
math.Sqrt
math.Pow
math.Abs
math.Floor
math.Ceil
math.Round
math.Inf
math.IsNaN
math.MaxInt64
math.Pi
sync.Scope.Task
sync.Scope.Wait
sync.Mutex.Lock
sync.Mutex.Unlock
sync.Mutex.TryLock
sync.RWMutex.RLock
sync.RWMutex.RUnlock
sync.Once.Do
sync.WaitGroup.Add
sync.WaitGroup.Done
sync.WaitGroup.Wait
sync/atomic.Int64
sync/atomic.Int32
sync/atomic.Uint64
sync/atomic.Bool
sync/atomic.Int64.Load
sync/atomic.Int64.Store
sync/atomic.Int64.Add
sync/atomic.Int64.CompareAndSwap
sync/atomic.AddInt64
alloc.Owned
alloc.Shared
alloc.Synced
alloc.Use
arena.NewFixed
arena.NewChunked
arena.Fixed.New
arena.Fixed.Slice
arena.Fixed.Mark
arena.Fixed.Release
arena.Fixed.Reset
testing.T.Errorf
testing.T.Fatalf
testing.T.Fatal
testing.T.Log
testing.T.Run
testing.T.Helper
testing.T.Skip
```
