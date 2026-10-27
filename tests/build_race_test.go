// Ported from internal/build/race_test.go at the go-cutover tag.

package tests

import (
	"strings"
	"testing"
)

// A raceCase is a program with a data race, which must not compile, and
// the same program fixed, which must compile and print want with one, two
// and 24 processors (language spec section 19.3).
type raceCase struct {
	name    string
	racy    string
	wantErr string // the first line of the error, after the position
	fixed   string
	want    string
}

var raceCases = []raceCase{
	{
		name: "counter",
		racy: `package main

import "pluto/sync"

var (
	hits int
	wg   sync.WaitGroup
)

func handle() {
	for i := 0; i < 1000; i++ {
		hits++
	}
	wg.Done()
}

func main() {
	wg.Add(8)
	for i := 0; i < 8; i++ {
		task handle()
	}
	wg.Wait()
	println(hits)
}
`,
		wantErr: "hits is written here with no lock that guards it, while another task may use it",
		fixed: `package main

import "pluto/sync"

var (
	mu   sync.Mutex
	hits int
	wg   sync.WaitGroup
)

func handle() {
	for i := 0; i < 1000; i++ {
		mu.Lock()
		hits++
		mu.Unlock()
	}
	wg.Done()
}

func main() {
	wg.Add(8)
	for i := 0; i < 8; i++ {
		task handle()
	}
	wg.Wait()
	mu.Lock()
	println(hits)
	mu.Unlock()
}
`,
		want: "8000\n",
	},
	{
		name: "read after wait",
		racy: `package main

import "pluto/sync"

var (
	mu  sync.Mutex
	sum int
	wg  sync.WaitGroup
)

func add(n int) {
	mu.Lock()
	sum += n
	mu.Unlock()
	wg.Done()
}

func main() {
	wg.Add(10)
	for i := 1; i <= 10; i++ {
		task add(i)
	}
	wg.Wait()
	println(sum)
}
`,
		wantErr: "sum is read here without holding mu, which guards it",
		fixed: `package main

import "pluto/sync"

var (
	mu  sync.Mutex
	sum int
	wg  sync.WaitGroup
)

func add(n int) {
	mu.Lock()
	defer mu.Unlock()
	sum += n
	wg.Done()
}

func total() int {
	mu.Lock()
	defer mu.Unlock()
	return sum
}

func main() {
	wg.Add(10)
	for i := 1; i <= 10; i++ {
		task add(i)
	}
	wg.Wait()
	println(total())
}
`,
		want: "55\n",
	},
	{
		name: "write under a read lock",
		racy: `package main

import "pluto/sync"

var (
	rw    sync.RWMutex
	table [8]int
	wg    sync.WaitGroup
)

func bump(i int) {
	rw.RLock()
	table[i%8]++
	rw.RUnlock()
	wg.Done()
}

func main() {
	wg.Add(16)
	for i := 0; i < 16; i++ {
		task bump(i)
	}
	wg.Wait()
	rw.RLock()
	println(table[0])
	rw.RUnlock()
}
`,
		wantErr: "table[i] is written here with no lock that guards it, while another task may use it",
		fixed: `package main

import "pluto/sync"

var (
	rw    sync.RWMutex
	table [8]int
	wg    sync.WaitGroup
)

func bump(i int) {
	rw.Lock()
	table[i%8]++
	rw.Unlock()
	wg.Done()
}

func read(i int) int {
	rw.RLock()
	defer rw.RUnlock()
	return table[i]
}

func main() {
	wg.Add(16)
	for i := 0; i < 16; i++ {
		task bump(i)
	}
	wg.Wait()
	println(read(0), read(7))
}
`,
		want: "2 2\n",
	},
	{
		name: "lock in a helper method",
		racy: `package main

import "pluto/sync"

type Stats struct {
	mu   sync.Mutex
	hits int
}

var stats Stats
var wg sync.WaitGroup

func (s *Stats) lock()   { s.mu.Lock() }
func (s *Stats) unlock() { s.mu.Unlock() }

func record() {
	stats.lock()
	stats.hits++
	stats.unlock()
	stats.hits++
	wg.Done()
}

func main() {
	wg.Add(4)
	for i := 0; i < 4; i++ {
		task record()
	}
	wg.Wait()
	stats.lock()
	println(stats.hits)
	stats.unlock()
}
`,
		wantErr: "stats.hits is written here with no lock that guards it, while another task may use it",
		fixed: `package main

import "pluto/sync"

type Stats struct {
	mu   sync.Mutex
	hits int
}

var stats Stats
var wg sync.WaitGroup

func (s *Stats) lock()   { s.mu.Lock() }
func (s *Stats) unlock() { s.mu.Unlock() }

func (s *Stats) addLocked(n int) { s.hits += n }

func record() {
	stats.lock()
	stats.addLocked(1)
	stats.addLocked(1)
	stats.unlock()
	wg.Done()
}

func main() {
	wg.Add(4)
	for i := 0; i < 4; i++ {
		task record()
	}
	wg.Wait()
	stats.lock()
	println(stats.hits)
	stats.unlock()
}
`,
		want: "8\n",
	},
	{
		name: "view of changing memory",
		racy: `package main

type Counter struct{ n int }

var counter Counter

func (c *Counter) bump(done chan bool) {
	c.n++
	done <- true
}

func main() {
	done := make(chan bool)
	c := &counter
	for i := 0; i < 4; i++ {
		task c.bump(done)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	println(c.n)
}
`,
		wantErr: "counter.n is written here with no lock that guards it, while another task may use it",
		fixed: `package main

import "pluto/sync"

// Every use of n holds the Counter's own mutex, so a *Counter may cross.
type Counter struct {
	mu sync.Mutex
	n  int
}

func (c *Counter) bump(done chan bool) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	done <- true
}

func (c *Counter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

var counter Counter

func main() {
	done := make(chan bool)
	c := &counter
	for i := 0; i < 4; i++ {
		task c.bump(done)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	println(c.get())
}
`,
		want: "4\n",
	},
	{
		name: "channel of views",
		racy: `package main

type Job struct{ id, cost int }

func worker(jobs chan *Job, out chan int) {
	for j := range jobs {
		out <- j.id * j.cost
	}
}

var job Job

func main() {
	jobs := make(chan *Job, 4)
	out := make(chan int, 4)
	task worker(jobs, out)
	for i := 1; i <= 3; i++ {
		job.id, job.cost = i, 10
		jobs <- &job
	}
	close(jobs)
	println(<-out + <-out + <-out)
}
`,
		wantErr: "&job is a view of Job that may change while another task uses it, so it cannot be sent on a channel",
		fixed: `package main

type Job struct{ id, cost int }

func worker(jobs chan Job, out chan int) {
	for j := range jobs {
		out <- j.id * j.cost
	}
}

func main() {
	jobs := make(chan Job, 4)
	out := make(chan int, 4)
	task worker(jobs, out)
	for i := 1; i <= 3; i++ {
		jobs <- Job{id: i, cost: 10}
	}
	close(jobs)
	println(<-out + <-out + <-out)
}
`,
		want: "60\n",
	},
	{
		name: "atomic writes, plain read",
		racy: `package main

import (
	"pluto/sync"
	"pluto/sync/atomic"
)

var (
	ops int64
	wg  sync.WaitGroup
)

func work() {
	for i := 0; i < 100; i++ {
		atomic.AddInt64(&ops, 1)
	}
	wg.Done()
}

func main() {
	wg.Add(4)
	for i := 0; i < 4; i++ {
		task work()
	}
	wg.Wait()
	println(ops)
}
`,
		wantErr: "ops is read here without an atomic operation, but other tasks change it with atomic operations",
		fixed: `package main

import (
	"pluto/sync"
	"pluto/sync/atomic"
)

var (
	ops atomic.Int64
	wg  sync.WaitGroup
)

func work() {
	for i := 0; i < 100; i++ {
		ops.Add(1)
	}
	wg.Done()
}

func main() {
	wg.Add(4)
	for i := 0; i < 4; i++ {
		task work()
	}
	wg.Wait()
	println(ops.Load())
}
`,
		want: "400\n",
	},
	{
		name: "synced object",
		racy: `package main

import (
	"pluto/alloc"
	"pluto/sync"
)

type Stats owner struct {
	mu   sync.Mutex
	hits int
}

func record(s synced[Stats], done chan bool) {
	for i := 0; i < 100; i++ {
		s.hits++
	}
	done <- true
}

func main() {
	s := alloc.Synced[Stats]()
	done := make(chan bool)
	for i := 0; i < 4; i++ {
		task record(s, done)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	s.mu.Lock()
	println(s.hits)
	s.mu.Unlock()
}
`,
		wantErr: "the field hits of a synced[Stats] is written here with no lock that guards it, while another task may use it",
		fixed: `package main

import (
	"pluto/alloc"
	"pluto/sync"
)

type Stats owner struct {
	mu   sync.Mutex
	hits int
}

func (s *Stats) add() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hits++
}

func record(s synced[Stats], done chan bool) {
	for i := 0; i < 100; i++ {
		s.add()
	}
	done <- true
}

func main() {
	s := alloc.Synced[Stats]()
	done := make(chan bool)
	for i := 0; i < 4; i++ {
		task record(s, done)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	s.mu.Lock()
	println(s.hits)
	s.mu.Unlock()
}
`,
		want: "400\n",
	},
}

// TestRaces checks that each racy program is rejected with its error and
// that its fixed version runs correctly on one, two and 24 processors.
func TestRaces(t *testing.T) {
	t.Parallel()
	for _, c := range raceCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			errs := buildFails(t, c.racy, "--brief")
			if errs == "" {
				t.Fatalf("racy version built")
			}
			if first, _, _ := strings.Cut(errs, "\n"); !strings.HasSuffix(first, ": "+c.wantErr) {
				t.Errorf("error:\n%v\nwant a first line ending %q", errs, c.wantErr)
			}
			prog := mustBuild(t, single(c.fixed), "-O2")
			for _, procs := range []string{"1", "2", "24"} {
				out, status := runExeEnv(t, prog, "PLUTO_MAXPROCS="+procs)
				if status != 0 || out != c.want {
					t.Errorf("PLUTO_MAXPROCS=%s: status %d, output %q; want %q", procs, status, out, c.want)
				}
			}
		})
	}
}
