package race_test

import (
	"strings"
	"testing"
)

// Programs that race, each with substrings of the full error output.
var rejected = []struct {
	name string
	src  string
	want []string
}{
	{"package variable written by several tasks", `
var hits int

func handle(done chan bool) {
	hits++
	done <- true
}

func main() {
	done := make(chan bool)
	for i := 0; i < 4; i++ {
		go handle(done)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
}`, []string{"main.p:5:2: hits is written here with no lock that guards it, while another puroutine may use it",
		"hits is written at main.p:5 in handle by the puroutine started by go handle at main.p:12 with no lock held, and another puroutine started there writes it at main.p:5 in handle.",
		"E3502"}},

	{"read without the inferred lock", `
import "pluto/sync"

var (
	mu    sync.Mutex
	count int
	wg    sync.WaitGroup
)

func add() {
	mu.Lock()
	count++
	mu.Unlock()
	wg.Done()
}

func main() {
	wg.Add(2)
	go add()
	go add()
	wg.Wait()
	println(count)
}`, []string{"main.p:22:10: count is read here without holding mu, which guards it",
		"Every write to count that another puroutine may run at the same time holds mu: main.p:12 in add",
		"Here the main puroutine reads it at main.p:22 in main with no lock held.", "mu.Lock()\n", "E3503"}},

	{"write under a read lock", `
import "pluto/sync"

var (
	rw    sync.RWMutex
	table [4]int
)

func reader(done chan bool) {
	rw.RLock()
	_ = table[0]
	rw.RUnlock()
	done <- true
}

func writer(done chan bool) {
	rw.RLock()
	table[0] = 2
	rw.RUnlock()
	done <- true
}

func main() {
	done := make(chan bool)
	go reader(done)
	go writer(done)
	<-done
	<-done
}`, []string{"table[0] is written here with no lock that guards it", "while holding rw only for reading"}},

	{"two different locks", `
import "pluto/sync"

var (
	a, b sync.Mutex
	n    int
)

func one(done chan bool) {
	a.Lock()
	n++
	a.Unlock()
	done <- true
}

func two(done chan bool) {
	b.Lock()
	n++
	b.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	go one(done)
	go two(done)
	<-done
	<-done
}`, []string{"n is written here with no lock that guards it", "holding a", "holding b", "Here no one lock is held at every write"}},

	{"lock released before the access", `
import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func bump(done chan bool) {
	mu.Lock()
	n++
	mu.Unlock()
	n++
	done <- true
}

func main() {
	done := make(chan bool)
	go bump(done)
	go bump(done)
	<-done
	<-done
}`, []string{"main.p:13:2: n is written here with no lock that guards it"}},

	{"helper called without the lock", `
import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func incLocked() { n++ }

func good(done chan bool) {
	mu.Lock()
	incLocked()
	mu.Unlock()
	done <- true
}

func bad(done chan bool) {
	incLocked()
	done <- true
}

func main() {
	done := make(chan bool)
	go good(done)
	go bad(done)
	<-done
	<-done
}`, []string{"main.p:9:20: n is written here with no lock that guards it", "in incLocked"}},

	{"atomic writes and a plain read", `
import "pluto/sync/atomic"

var hits int64

func handle(done chan bool) {
	atomic.AddInt64(&hits, 1)
	done <- true
}

func main() {
	done := make(chan bool)
	go handle(done)
	go handle(done)
	<-done
	<-done
	println(hits)
}`, []string{"hits is read here without an atomic operation, but other puroutines change it with atomic operations"}},

	{"view of changing memory to go", `
type Counter struct{ n int }

var counter Counter

func bump(c *Counter, done chan bool) {
	c.n++
	done <- true
}

func main() {
	done := make(chan bool)
	go bump(&counter, done)
	go bump(&counter, done)
	<-done
	<-done
}`, []string{"main.p:13:2: &counter is a view of Counter that may change while another puroutine uses it, so it cannot be passed to a new puroutine",
		"It points to the package variable counter, which is written at main.p:7 while puroutines run.",
		"A Counter is changed through a pointer at main.p:7 (its field n, in bump) without a lock of the same Counter held", "E3501"}},

	{"object read without its own mutex", `
import "pluto/sync"

type Counter struct {
	mu sync.Mutex
	n  int
}

func (c *Counter) Add() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *Counter) Peek() int { return c.n }

func work(c *Counter, done chan bool) {
	c.Add()
	done <- true
}

var c Counter

func main() {
	done := make(chan bool)
	go work(&c, done)
	<-done
	println(c.Peek())
}`, []string{"main.p:26:2: &c is a view of Counter that may change while another puroutine uses it",
		"Its field n is changed at main.p:11 (in (*Counter).Add) and read at main.p:15 (in (*Counter).Peek) without a lock that every change holds."}},

	{"view sent on a channel", `
type Job struct{ id int }

var job Job

func main() {
	ch := make(chan *Job, 1)
	done := make(chan bool)
	go show(ch, done)
	job.id = 1
	ch <- &job
	job.id = 2
	<-done
}

func show(ch chan *Job, done chan bool) {
	println((<-ch).id)
	done <- true
}`, []string{"main.p:11:5: &job is a view of Job that may change while another puroutine uses it, so it cannot be sent on a channel",
		"It points to the package variable job, which is written at main.p:10 while puroutines run."}},

	{"slice of a changing array", `
func sum(xs []int, out chan int) {
	t := 0
	for _, x := range xs {
		t += x
	}
	out <- t
}

func main() {
	xs := new([3]int)[:]
	out := make(chan int)
	go sum(xs, out)
	xs[0] = 5
	println(<-out)
}`, []string{"xs is a view of int that may change while another puroutine uses it"}},

	{"view of a package variable written later", `
type Config struct{ limit int }

var cfg Config

func show(c *Config, done chan bool) {
	println(c.limit)
	done <- true
}

func main() {
	done := make(chan bool)
	go show(&cfg, done)
	cfg.limit = 3
	<-done
}`, []string{"&cfg is a view of Config", "It points to the package variable cfg, which is written at main.p:14 while puroutines run."}},

	{"interface holding a changing pointer", `
type T struct{ n int }

func (t *T) String() string { t.n++; return "t" }

type stringer interface{ String() string }

func show(s stringer, done chan bool) {
	println(s.String())
	done <- true
}

var tv T

func main() {
	done := make(chan bool)
	var s stringer = &tv
	go show(s, done)
	<-done
}`, []string{"s (holding a *T) is a view of T"}},

	{"synced field written without a lock", `
import "pluto/sync"

type Stats struct {
	mu   sync.Mutex
	hits int
}

func record(s synced[Stats], done chan bool) {
	s.hits++
	done <- true
}

func read(s synced[Stats], done chan bool) {
	s.mu.Lock()
	println(s.hits)
	s.mu.Unlock()
	done <- true
}

func main() {
	var s synced[Stats]
	done := make(chan bool)
	go record(s, done)
	go read(s, done)
	<-done
	<-done
}`, []string{"the field hits of a synced[Stats] is written here with no lock that guards it"}},

	{"synced field read without its lock", `
import "pluto/sync"

type Stats struct {
	mu   sync.Mutex
	hits int
}

func record(s synced[Stats], done chan bool) {
	s.mu.Lock()
	s.hits++
	s.mu.Unlock()
	done <- true
}

func peek(s synced[Stats], done chan bool) {
	println(s.hits)
	done <- true
}

func main() {
	var s synced[Stats]
	done := make(chan bool)
	go record(s, done)
	go peek(s, done)
	<-done
	<-done
}`, []string{"the field hits of a synced[Stats] is read here without holding mu, which guards it"}},

	{"task started by WaitGroup.Go", `
import "pluto/sync"

var (
	wg sync.WaitGroup
	n  int
)

func bump() { n++ }

func main() {
	for i := 0; i < 3; i++ {
		wg.Go(bump)
	}
	wg.Wait()
}`, []string{"main.p:9:15: n is written here with no lock that guards it", "n is written at main.p:9 in bump by the puroutine started by a call of sync.(*WaitGroup).Go at main.p:13 with no lock held"}},
}

// Programs that are free of races: each must pass.
var accepted = []struct {
	name string
	src  string
}{
	{"object guarded by its own mutex crosses", `
import "pluto/sync"

type Counter struct {
	mu sync.Mutex
	n  int
}

func (c *Counter) Add() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *Counter) Get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func work(c *Counter, done chan bool) {
	c.Add()
	done <- true
}

var c Counter

func main() {
	done := make(chan bool)
	go work(&c, done)
	go work(&c, done)
	<-done
	<-done
	println(c.Get())
}`},

	{"an owned box moves to the task", `
type Counter struct{ n int }

func (c *Counter) bump(done chan int) {
	c.n++
	done <- c.n
}

func main() {
	done := make(chan int)
	c := new(Counter)
	go c.bump(done)
	println(<-done)
}`},

	{"statements and expressions under one lock", `
import (
	"pluto/sync"
	"pluto/vec"
)

type pair struct{ a, b int }

var (
	mu    sync.Mutex
	ring1 ring[int, 8]
	items vec.Vec[int]
	arr   [4]pair
	buf   [16]byte
	total int
	label string
)

func seq(n int) func(yield func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func two() (int, string) { return 2, "two" }

func (p *pair) sum() int { return p.a + p.b }

func work(id int, done chan bool) {
	mu.Lock()
	defer mu.Unlock()
	for v := range seq(3) {
		total += v
	}
	ring1.Push(id)
	items.Push(id)
	items[0] = id
	n, s := two()
	label = s
	arr[id%4] = pair{n, id}
	local := pair{1, 2}
	f := local.sum // a method value of a local: called later, with no lock
	total += f()
	copy(buf[:], "hello")
	switch {
	case id == 0:
		total++
		fallthrough
	case id == 1:
		total += 2
	default:
	}
	select {
	case done <- true:
	default:
		go func() { done <- true }()
	}
outer:
	for i := range arr {
		for j := range arr {
			if i+j > 3 {
				break outer
			}
			if j > i {
				continue outer
			}
			total += arr[i].a
		}
	}
	var x any = arr[1]
	if p, ok := x.(pair); ok {
		total += p.b
	}
	switch v := x.(type) {
	case pair:
		total += v.a
	}
	_ = take(&label)
}

func main() {
	done := make(chan bool)
	go work(0, done)
	go work(1, done)
	<-done
	<-done
	mu.Lock()
	println(total, label, len(items))
	mu.Unlock()
}`},

	{"package variable set up before the tasks, then shared read-only", `
type Table struct {
	name string
	rows [4]int
}

func (t *Table) init(name string) {
	t.name = name
	for i := range t.rows {
		t.rows[i] = i * i
	}
}

var table Table

func init() { table.init("squares") }

func show(t *Table, done chan int) { done <- t.rows[3] }

func main() {
	done := make(chan int)
	go show(&table, done)
	go show(&table, done)
	println(<-done + <-done)
}`},

	{"guarded counter, read under the lock", `
import "pluto/sync"

var (
	mu    sync.Mutex
	count int
	wg    sync.WaitGroup
)

func add() {
	mu.Lock()
	count++
	mu.Unlock()
	wg.Done()
}

func main() {
	wg.Add(2)
	go add()
	go add()
	wg.Wait()
	mu.Lock()
	println(count)
	mu.Unlock()
}`},

	{"defer unlock", `
import "pluto/sync"

var (
	mu sync.Mutex
	m  [8]int
)

func put(i, v int, done chan bool) {
	mu.Lock()
	defer mu.Unlock()
	m[i] = v
	done <- true
}

func get(i int) int {
	mu.Lock()
	defer mu.Unlock()
	return m[i]
}

func main() {
	done := make(chan bool)
	go put(1, 2, done)
	go put(2, 3, done)
	<-done
	<-done
	println(get(1) + get(2))
}`},

	{"RWMutex reads under RLock", `
import "pluto/sync"

var (
	rw    sync.RWMutex
	table [4]int
)

func reader(done chan int) {
	rw.RLock()
	v := table[0]
	rw.RUnlock()
	done <- v
}

func writer(done chan int) {
	rw.Lock()
	table[0]++
	rw.Unlock()
	done <- 0
}

func main() {
	done := make(chan int)
	go reader(done)
	go writer(done)
	<-done
	<-done
}`},

	{"lock held across calls", `
import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func incLocked() { n++ }

func work(done chan bool) {
	mu.Lock()
	incLocked()
	incLocked()
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	go work(done)
	go work(done)
	<-done
	<-done
}`},

	{"lock taken in a helper method", `
import "pluto/sync"

type State struct {
	mu    sync.Mutex
	items [16]int
	n     int
}

var state State

func (s *State) lock()   { s.mu.Lock() }
func (s *State) unlock() { s.mu.Unlock() }

func (s *State) add(v int) {
	s.lock()
	defer s.unlock()
	if s.n < len(s.items) {
		s.items[s.n] = v
		s.n++
	}
}

func worker(v int, done chan bool) {
	state.add(v)
	done <- true
}

func main() {
	done := make(chan bool)
	for i := 0; i < 4; i++ {
		go worker(i, done)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	state.lock()
	println(state.n)
	state.unlock()
}`},

	{"TryLock", `
import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func try(done chan bool) {
	if mu.TryLock() {
		n++
		mu.Unlock()
	}
	done <- true
}

func main() {
	done := make(chan bool)
	go try(done)
	go try(done)
	<-done
	<-done
}`},

	{"atomic everywhere", `
import "pluto/sync/atomic"

var hits atomic.Int64

func handle(done chan bool) {
	hits.Add(1)
	done <- true
}

func main() {
	done := make(chan bool)
	go handle(done)
	go handle(done)
	<-done
	<-done
	println(hits.Load())
}`},

	{"written before the tasks start", `
var limit int

func use(done chan int) { done <- limit }

func main() {
	limit = 10
	done := make(chan int)
	go use(done)
	go use(done)
	println(<-done + <-done)
}`},

	{"values and immutable views cross", `
type Point struct{ x, y int }

type Node struct {
	name string
	next *Node
}

var b = Node{name: "b"}
var a = Node{name: "a", next: &b}

func walk(n *Node, p Point, done chan string) {
	s := ""
	for ; n != nil; n = n.next {
		s += n.name
	}
	done <- s
}

func main() {
	done := make(chan string)
	list := &a
	go walk(list, Point{1, 2}, done)
	println(<-done)
	ch := make(chan *Node, 1)
	ch <- list
	println((<-ch).name)
}`},

	{"pointer to synchronised memory", `
import "pluto/sync"

func work(wg *sync.WaitGroup, mu *sync.Mutex) {
	mu.Lock()
	mu.Unlock()
	wg.Done()
}

var (
	wg sync.WaitGroup
	mu sync.Mutex
)

func main() {
	wg.Add(2)
	go work(&wg, &mu)
	go work(&wg, &mu)
	wg.Wait()
}`},

	{"package variable never written after initialization", `
var table = []int{1, 2, 3}

func sum(xs []int, out chan int) {
	t := 0
	for _, x := range xs {
		t += x
	}
	out <- t
}

func main() {
	out := make(chan int)
	go sum(table, out)
	go sum(table[1:], out)
	println(<-out + <-out)
}`},

	{"one task alone", `
var log [16]int
var n int

func logger(done chan bool) {
	for i := 0; i < 16; i++ {
		log[n] = i
		n++
	}
	done <- true
}

func main() {
	done := make(chan bool)
	go logger(done)
	<-done
}`},

	{"synced fields guarded by the object's mutex", `
import "pluto/sync"

type Stats struct {
	mu   sync.Mutex
	hits int
}

func record(s synced[Stats], done chan bool) {
	s.mu.Lock()
	s.hits++
	s.mu.Unlock()
	done <- true
}

func main() {
	var s synced[Stats]
	done := make(chan bool)
	go record(s, done)
	go record(s, done)
	<-done
	<-done
	s.mu.Lock()
	println(s.hits)
	s.mu.Unlock()
}`},
}

func TestRejected(t *testing.T) {
	t.Parallel()
	for _, c := range rejected {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := check(t, "package main"+c.src)
			if err == nil {
				t.Fatalf("no error")
			}
			got := fullText(err)
			for _, w := range c.want {
				// Explanations are wrapped: compare with the spacing collapsed.
				if !strings.Contains(got, w) && !strings.Contains(squash(got), squash(w)) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
		})
	}
}

func TestAccepted(t *testing.T) {
	t.Parallel()
	for _, c := range accepted {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if err := check(t, "package main"+c.src); err != nil {
				t.Errorf("unexpected error:\n%s", fullText(err))
			}
		})
	}
}

// squash collapses runs of white space to one space.
func squash(s string) string { return strings.Join(strings.Fields(s), " ") }
