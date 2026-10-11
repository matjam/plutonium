package race_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// Tasks started by a sync.Scope that share the frame (scope.go): programs
// that race, each with substrings of the full error output.
var scopeRejected = []struct {
	name string
	src  string
	want []string
}{
	{"unguarded counter", `
import "pluto/sync"

func count(counts []int) int {
	var total int
	var s sync.Scope
	for _, n := range counts {
		s.Task(func() { total += n })
	}
	s.Wait()
	return total
}

func main() { println(count([]int{1, 2})) }`, []string{
		"main.p:8:19: total is written by the tasks started at main.p:8 at the same time as each other",
		"Each call of s.Task at main.p:8 starts a task", "E3402"}},

	{"read before the join", `
import "pluto/sync"

func first(items []int) int {
	var results [4]int
	var s sync.Scope
	for i := range results {
		s.Task(func() { results[i] = items[i] })
	}
	return results[0]
}

func main() { println(first([]int{1, 2, 3, 4})) }`, []string{
		"main.p:10:9: results[0] is read here while the task started at main.p:8 may write it",
		"s waits for it where its block ends", "s.Wait()", "E3402"}},

	{"element by a computed index", `
import "pluto/sync"

func half(i int) int { return i / 2 }

func fill(out []int) {
	var s sync.Scope
	for i := range out {
		s.Task(func() { out[half(i)] = i })
	}
}

func main() { fill(nil) }`, []string{
		"main.p:9:19: the tasks started at main.p:9 may write the same element of out",
		"x[i*k:(i+1)*k]", "E3403"}},

	{"two calls write one variable", `
import "pluto/sync"

func main() {
	var x int
	var s sync.Scope
	s.Task(func() { x = 1 })
	s.Task(func() { x = 2 })
	s.Wait()
	println(x)
}`, []string{"x is written by the task started at main.p:7 while the task started at main.p:8 uses it", "E3402"}},

	{"conditional wait", `
import "pluto/sync"

func run(fast bool) int {
	var res [2]int
	var s sync.Scope
	for i := range res {
		s.Task(func() { res[i] = i })
	}
	if fast {
		s.Wait()
	}
	res[0] = 5
	return res[1]
}

func main() { println(run(true)) }`, []string{"main.p:13:2: res[0] is written here while the task started at main.p:8 may use it"}},

	{"deferred call while tasks run", `
import "pluto/sync"

func run() {
	var total int
	defer func() { println(total) }()
	var mu sync.Mutex
	var s sync.Scope
	for i := 0; i < 3; i++ {
		s.Task(func() {
			mu.Lock()
			total += i
			mu.Unlock()
		})
	}
}

func main() { run() }`, []string{"total is read here while the task started at main.p:10 may write it"}},

	{"taken variable changed", `
import "pluto/sync"

func run(items []int) {
	var s sync.Scope
	for _, it := range items {
		n := 0
		s.Task(func() { n = it })
		println(n)
	}
}

func main() { run(nil) }`, []string{
		"main.p:8:19: the task started at main.p:8 changes n, but has its own copy of it, so the change is not seen at main.p:9", "E3407"}},

	{"moved while a task uses it", `
import "pluto/sync"

type Buf owner struct{ n int }

func (b *Buf) Drop() {}

func consume(b Buf) {}

func main() {
	var b Buf
	var s sync.Scope
	s.Task(func() { b.n = 1 })
	consume(b)
}`, []string{"main.p:14:10: cannot move b here: the task started at main.p:13 may still be using it", "E3404"}},

	{"dropped while a task uses it", `
import "pluto/sync"

type Buf owner struct{ n int }

func (b *Buf) Drop() {}

func main() {
	var b Buf
	var s sync.Scope
	s.Task(func() { b.n = 1 })
	b.Drop()
}`, []string{"main.p:12:2: cannot drop b here: the task started at main.p:11 may still be using it"}},

	{"views of the same type may alias", `
import "pluto/sync"

func double(in, out []int) {
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = in[i] * 2 })
	}
}

func main() { double(nil, nil) }`, []string{"out[i] is written by the tasks started at main.p:7 at the same time as each other",
		"in[i] and out[i] are reached through different views, which may point to the same memory"}},

	{"windows that overlap", `
import "pluto/sync"

func fill(out []int) {
	var s sync.Scope
	for i := 0; i+3 <= len(out); i += 2 {
		s.Task(func() {
			w := out[i : i+3]
			w[0] = i
		})
	}
}

func main() { fill(nil) }`, []string{"may write the same element of out"}},

	{"rows of a slice of slices", `
import "pluto/sync"

func fill(grid [][]int) {
	var s sync.Scope
	for y := range grid {
		s.Task(func() { grid[y][0] = y })
	}
}

func main() { fill(nil) }`, []string{"may write the same element of"}},

	{"split that overlaps", `
import "pluto/sync"

func zero(v []int) {
	for i := range v {
		v[i] = 0
	}
}

func run(v []int, p int) {
	var s sync.Scope
	s.Task(func() { zero(v[:p]) })
	zero(v[p-1:])
}

func main() { run(nil, 0) }`, []string{"v[i] is written here while the task started at main.p:12 may use it", "in zero called at main.p:13"}},

	{"package variable", `
import "pluto/sync"

var hits int

func main() {
	var s sync.Scope
	for i := 0; i < 2; i++ {
		s.Task(func() { hits++ })
	}
}`, []string{"hits is written by the tasks started at main.p:9 at the same time as each other"}},

	{"literal called from several tasks", `
import "pluto/sync"

func Map[T, U any](in []T, out []U, f func(T) U) {
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = f(in[i]) })
	}
}

func main() {
	var lens [2]int
	count := 0
	Map([]string{"a", "b"}, lens[:], func(s string) int { count++; return len(s) })
	println(count)
}`, []string{"main.p:14:35: Map calls f from several tasks at once, and the function literal passed here writes count", "E3406"}},

	{"lock held for reading only", `
import "pluto/sync"

func main() {
	var rw sync.RWMutex
	var n int
	var s sync.Scope
	for i := 0; i < 2; i++ {
		s.Task(func() {
			rw.RLock()
			n = i
			rw.RUnlock()
		})
	}
	s.Wait()
	println(n)
}`, []string{"n is written by the tasks started at main.p:9 at the same time as each other"}},
}

// Programs whose scoped tasks share the frame without races.
var scopeAccepted = []struct {
	name string
	src  string
}{
	{"parallel map", `
import "pluto/sync"

type Item struct{ n int }
type Result struct{ sq int }

func process(items []Item, results []Result) {
	var s sync.Scope
	for i, it := range items {
		s.Task(func() { results[i] = Result{it.n * it.n} })
	}
	s.Wait()
}

func main() { process(nil, nil) }`},

	{"worker pool", `
import "pluto/sync"

func crawl(urls []string) (int, error) {
	jobs := make(chan string, 16)
	var mu sync.Mutex
	var total int
	var first error
	var s sync.Scope
	for w := 0; w < 8; w++ {
		s.Task(func() {
			for u := range jobs {
				mu.Lock()
				total += len(u)
				if first == nil && u == "" {
					first = nil
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
	return total, first
}

func main() { crawl(nil) }`},

	{"windows", `
import "pluto/sync"

func scale(in []int, out []int64, k int) {
	var s sync.Scope
	for i := 0; i < len(in)/k; i++ {
		s.Task(func() {
			row := out[i*k : (i+1)*k]
			for j := range row {
				row[j] = int64(in[i*k+j])
			}
		})
	}
	s.Wait()
	for i := 0; i < len(in); i += 4 {
		s.Task(func() {
			dst := out[i:min(i+4, len(out))]
			for j := range dst {
				dst[j]++
			}
		})
		s.Wait()
	}
}

func main() { scale(nil, nil, 1) }`},

	{"split", `
import "pluto/sync"

func sortPar(v []int) {
	if len(v) < 2 {
		return
	}
	p := len(v) / 2
	var s sync.Scope
	s.Task(func() { sortPar(v[:p]) })
	sortPar(v[p+1:])
	v[p]++
}

func main() { sortPar(nil) }`},

	{"reads after the join", `
import "pluto/sync"

func main() {
	var res [3]int
	{
		var s sync.Scope
		for i := range res {
			s.Task(func() { res[i] = i })
		}
	}
	println(res[0])
	var s2 sync.Scope
	for i := range res {
		s2.Task(func() { res[i]++ })
	}
	s2.Wait()
	println(res[1])
}`},

	{"a scope per iteration", `
import "pluto/sync"

func main() {
	x := 0
	for i := 0; i < 3; i++ {
		var s sync.Scope
		s.Task(func() { x += i })
	}
	for i := 0; i < 3; i++ {
		var s sync.Scope
		s.Task(func() { x++ })
		s.Wait()
		x--
	}
	println(x)
}`},

	{"atomics and read locks", `
import (
	"pluto/sync"
	"pluto/sync/atomic"
)

func main() {
	var n atomic.Int64
	var rw sync.RWMutex
	table := [4]int{1, 2, 3, 4}
	sum := 0
	var s sync.Scope
	for i := 0; i < 4; i++ {
		s.Task(func() {
			n.Add(1)
			rw.RLock()
			v := table[i]
			rw.RUnlock()
			rw.Lock()
			sum += v
			rw.Unlock()
		})
	}
	rw.Lock()
	table[0] = 9
	rw.Unlock()
	s.Wait()
	println(n.Load(), sum)
}`},

	{"grid by both loop variables", `
import "pluto/sync"

func main() {
	var grid [3][4]int
	var s sync.Scope
	for y := range grid {
		for x := range 4 {
			s.Task(func() { grid[y][x] = y*4 + x })
		}
	}
	s.Wait()
	println(grid[2][3])
}`},

	{"nested scopes", `
import "pluto/sync"

func main() {
	var grid [3][3]int
	var s sync.Scope
	for y := range grid {
		s.Task(func() {
			var inner sync.Scope
			for x := range grid[y] {
				inner.Task(func() { grid[y][x] = y + x })
			}
		})
	}
	s.Wait()
	println(grid[1][1])
}`},

	{"tasks that start tasks", `
import "pluto/sync"

func main() {
	var out [4]int
	var s sync.Scope
	s.Task(func() {
		for i := range out {
			s.Task(func() { out[i] = i })
		}
	})
	s.Wait()
	println(out[3])
}`},

	{"literal with an atomic counter", `
import (
	"pluto/sync"
	"pluto/sync/atomic"
)

func Map[T, U any](in []T, out []U, f func(T) U) {
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = f(in[i]) })
	}
}

func main() {
	var lens [2]int
	var calls atomic.Int64
	Map([]string{"a", "b"}, lens[:], func(s string) int { calls.Add(1); return len(s) })
	println(calls.Load())
}`},

	{"constant elements of a package array", `
var table [2]int

func one(done chan bool) {
	table[0]++
	done <- true
}

func two(done chan bool) {
	table[1]++
	done <- true
}

func main() {
	done := make(chan bool)
	task one(done)
	task two(done)
	<-done
	<-done
}`},

	{"single task and the starter on different elements", `
import "pluto/sync"

func main() {
	var res [2]int
	var s sync.Scope
	s.Task(func() { res[0] = 1 })
	res[1] = 2
	s.Wait()
	println(res[0] + res[1])
}`},

	{"variable step", `
import "pluto/sync"

func fill(out []int, k int) {
	var s sync.Scope
	for i := 0; i < len(out); i += k {
		s.Task(func() {
			w := out[i:min(i+k, len(out))]
			for j := range w {
				w[j] = i
			}
		})
	}
}

func main() { fill(nil, 2) }`},
}

func TestScopeRejected(t *testing.T) {
	testguard.Parallel(t)
	for _, c := range scopeRejected {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			err := check(t, "package main"+c.src)
			if err == nil {
				t.Fatalf("no error")
			}
			got := fullText(err)
			for _, w := range c.want {
				if !contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
		})
	}
}

func TestScopeAccepted(t *testing.T) {
	testguard.Parallel(t)
	for _, c := range scopeAccepted {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			if err := check(t, "package main"+c.src); err != nil {
				t.Errorf("unexpected error:\n%s", fullText(err))
			}
		})
	}
}

// contains reports whether got has w, with explanations' wrapping ignored.
func contains(got, w string) bool {
	return strings.Contains(got, w) || strings.Contains(squash(got), squash(w))
}
