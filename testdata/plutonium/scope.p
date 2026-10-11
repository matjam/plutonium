package main

// sync.Scope (language spec section 4.11): tasks that use the variables of
// the function that starts them, which waits for them before the Scope's
// block ends. The examples of the design
// (docs/plans/2026-10-10-scoped-puroutines.md, section 7). The program
// also runs under the debug allocator, which fails on a leak, with one, two
// and many processors (internal/build/scope_test.go).

import (
	"pluto/alloc"
	"pluto/strings"
	"pluto/sync"
	"pluto/sync/atomic"
	"unsafe"
)

type Item struct{ n int }

type Result struct{ sq int }

// process is the idiom: each task writes its own element of results, by
// the loop variable, which it takes.
func process(items []Item, results []Result) {
	var s sync.Scope
	for i, it := range items {
		s.Task(func() { results[i] = Result{it.n * it.n} })
	}
	s.Wait() // optional: s waits when process returns in any case
}

// squares returns a local array, filled by tasks, by value after the join.
func squares() [8]int {
	var out [8]int
	var s sync.Scope
	for i := range out {
		s.Task(func() { out[i] = i * i })
	}
	s.Wait()
	return out
}

// Map calls f from several tasks at once: the literals passed to it use an
// atomic counter.
func Map[T, U any](in []T, out []U, f func(T) U) {
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = f(in[i]) })
	}
}

// MapChunked gives each task a window of out.
func MapChunked[T, U any](in []T, out []U, f func(T) U, k int) {
	var s sync.Scope
	for i := 0; i < len(in); i += k {
		s.Task(func() {
			dst := out[i:min(i+k, len(out))]
			for j := range dst {
				dst[j] = f(in[i+j])
			}
		})
	}
}

// crawl is a worker pool with a local channel and a local mutex, which
// the compiler infers guards total and longest.
func crawl(urls []string) (int, string) {
	jobs := make(chan string, 4)
	var mu sync.Mutex
	var total int
	var longest string
	var s sync.Scope
	for w := 0; w < 3; w++ {
		s.Task(func() {
			for u := range jobs {
				mu.Lock()
				total += len(u)
				if len(u) > len(longest) {
					longest = u
				}
				mu.Unlock()
			}
		})
	}
	for _, u := range urls {
		jobs <- u
	}
	close(jobs)
	s.Wait()
	return total, longest
}

// sortPar splits v: the task sorts v[:p], this function v[p+1:].
func sortPar(v []int) {
	if len(v) < 2 {
		return
	}
	p := partition(v)
	var s sync.Scope
	s.Task(func() { sortPar(v[:p]) })
	sortPar(v[p+1:])
}

func partition(v []int) int {
	p := len(v) - 1
	pivot := v[p]
	i := 0
	for j := 0; j < p; j++ {
		if v[j] < pivot {
			v[i], v[j] = v[j], v[i]
			i++
		}
	}
	v[i], v[p] = v[p], v[i]
	return i
}

// early returns before its explicit Wait: s waits for the tasks there.
func early(n int) int {
	var res [4]int
	var s sync.Scope
	for i := range res {
		s.Task(func() { res[i] = i + n })
	}
	if n > 0 {
		return -1
	}
	s.Wait()
	return res[0] + res[1] + res[2] + res[3]
}

// grid uses nested scopes: each row's task starts a task per cell, on a
// Scope of its own.
func grid() int {
	var g [3][4]int
	var s sync.Scope
	for y := range g {
		s.Task(func() {
			var row sync.Scope
			for x := range 4 {
				row.Task(func() { g[y][x] = y*4 + x })
			}
		})
	}
	s.Wait()
	sum := 0
	for _, r := range g {
		for _, v := range r {
			sum += v
		}
	}
	return sum
}

// fanOut starts tasks from a task, on the outer Scope: they are counted
// while the task that starts them is.
func fanOut() [6]int {
	var out [6]int
	var s sync.Scope
	s.Task(func() {
		for i := range out {
			s.Task(func() { out[i] = 10 + i })
		}
	})
	s.Wait()
	return out
}

// Buf is an owner: a task takes one declared in the loop body, and drops it
// when it returns.
type Buf owner struct {
	name string
	hits *atomic.Int64
}

func (b *Buf) Drop() {
	if b.hits != nil {
		b.hits.Add(1)
	}
}

func owners() int64 {
	var drops atomic.Int64
	var lens [3]int
	{
		var s sync.Scope
		for i, w := range []string{"a", "bb", "ccc"} {
			b := Buf{name: strings.Repeat(w, 2), hits: &drops}
			s.Task(func() { lens[i] = len(b.name) })
		}
	}
	return drops.Load()*100 + int64(lens[0]+lens[1]+lens[2])
}

// counter is an allocator over another one that counts its allocations.
type counter struct {
	base alloc.Allocator
	n    atomic.Int64
}

func (c *counter) Alloc(size, align uintptr) unsafe.Pointer {
	c.n.Add(1)
	return c.base.Alloc(size, align)
}

func (c *counter) Free(p unsafe.Pointer, size, align uintptr) { c.base.Free(p, size, align) }

// defaults starts tasks while it has installed an allocator of its own with
// alloc.Use: each task starts with the program-wide default allocator.
func defaults() bool {
	base := alloc.Default()
	c := counter{base: base}
	r := alloc.Use(&c)
	var same [2]bool
	var s sync.Scope
	for i := range same {
		s.Task(func() { same[i] = alloc.Default() == base })
	}
	s.Wait()
	starter := alloc.Default() == alloc.Allocator(&c)
	r.Restore()
	return same[0] && same[1] && starter
}

func main() {
	items := []Item{{1}, {2}, {3}, {4}}
	var results [4]Result
	process(items, results[:])
	println(results[0].sq, results[1].sq, results[2].sq, results[3].sq)

	sq := squares()
	println(sq[0], sq[3], sq[7])

	names := []string{"a", "bb", "ccc", "dddd", "eeeee"}
	var lens [5]int
	var calls atomic.Int64
	Map(names, lens[:], func(s string) int { calls.Add(1); return len(s) })
	println(lens[0], lens[4], calls.Load())

	var halves [7]float64
	MapChunked([]int{2, 4, 6, 8, 10, 12, 14}, halves[:], func(n int) float64 { return float64(n) / 2 }, 3)
	println(halves[0] == 1, halves[6] == 7)

	total, longest := crawl([]string{"x", "yyy", "zz", "wwww", "v"})
	println(total, longest)

	v := [10]int{5, 3, 8, 1, 9, 2, 7, 4, 0, 6}
	sortPar(v[:])
	println(v[0], v[1], v[5], v[9])

	println(early(1), early(0))
	println(grid())
	f := fanOut()
	println(f[0], f[5])
	println(owners())
	println(defaults())
}
