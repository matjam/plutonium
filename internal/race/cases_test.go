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
	{"a vec whose allocator has no lock of its own does not cross", `
import (
	"pluto/vec"
	"unsafe"
)

type Bump struct {
	mem [4096]byte
	off uintptr
}

func (b *Bump) Alloc(size, align uintptr) unsafe.Pointer {
	p := unsafe.Pointer(&b.mem[b.off])
	b.off += size
	return p
}

func (b *Bump) Free(p unsafe.Pointer, size, align uintptr) {}

var bump Bump

func produce(out chan<- vec.Vec[int]) {
	v := vec.New[int](&bump)
	v.Push(1)
	out <- v
}

func main() {
	ch := make(chan vec.Vec[int], 1)
	task produce(ch)
	v := <-ch
	println(v.Len())
}`, []string{"main.p:25:6: the field a of v (holding a *Bump) is a view of Bump that may change while another task uses it, so v cannot be sent on a channel",
		"A Bump is changed through a pointer at main.p:14 (its field off, in (*Bump).Alloc) without a lock of the same Bump held", "E3501"}},

	{"package variable written by several tasks", `
var hits int

func handle(done chan bool) {
	hits++
	done <- true
}

func main() {
	done := make(chan bool)
	for i := 0; i < 4; i++ {
		task handle(done)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
}`, []string{"main.p:5:2: hits is written here with no lock that guards it, while another task may use it",
		"hits is written at main.p:5 in handle by the task started by task handle at main.p:12 with no lock held, and another task started there writes it at main.p:5 in handle.",
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
	task add()
	task add()
	wg.Wait()
	println(count)
}`, []string{"main.p:22:10: count is read here without holding mu, which guards it",
		"Every write to count that another task may run at the same time holds mu: main.p:12 in add",
		"Here the main task reads it at main.p:22 in main with no lock held.", "mu.Lock()\n", "E3503"}},

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
	task reader(done)
	task writer(done)
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
	task one(done)
	task two(done)
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
	task bump(done)
	task bump(done)
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
	task good(done)
	task bad(done)
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
	task handle(done)
	task handle(done)
	<-done
	<-done
	println(hits)
}`, []string{"hits is read here without an atomic operation, but other tasks change it with atomic operations"}},

	{"view of changing memory to go", `
type Counter struct{ n int }

var counter Counter

func bump(c *Counter, done chan bool) {
	c.n++
	done <- true
}

func main() {
	done := make(chan bool)
	task bump(&counter, done)
	task bump(&counter, done)
	<-done
	<-done
}`, []string{"main.p:13:2: &counter is a view of Counter that may change while another task uses it, so it cannot be passed to a new task",
		"It points to the package variable counter, which is written at main.p:7 while tasks run.",
		"A Counter is changed through a pointer at main.p:7 (its field n, in bump) without a lock of the same Counter held", "E3501"}},

	{"owner's view field of memory it did not allocate", `
import (
	"pluto/alloc"
	"unsafe"
)

type Counter struct{ n int }

func bump(c *Counter) { c.n++ }

type View owner struct {
	c *Counter
}

func newView(c *Counter) View { return View{c: c} }

func read(v View) { println(v.c.n, unsafe.Sizeof(v)) }

func main() {
	c := alloc.New[Counter]()
	task read(newView(c))
	bump(c)
}`, []string{"main.p:21:2: the field c of newView(c) is a view of Counter that may change while another task uses it", "E3501"}},

	{"owner's view field whose address is taken", `
import "pluto/alloc"

type Counter struct{ n int }

func bump(c *Counter) { c.n++ }

type View owner struct {
	c *Counter
}

func (v *View) Init() {
	v.c = alloc.New[Counter]()
	set(&v.c)
}

var last *Counter

func set(p **Counter) { last = *p }

func read(v View) { println(v.c.n) }

func main() {
	var v View
	v.Init()
	bump(last)
	task read(v)
}`, []string{"is a view of Counter that may change while another task uses it", "E3501"}},

	{"owner's memory also kept in a package variable", `
import "pluto/alloc"

type Counter struct{ n int }

func bump(c *Counter) { c.n++ }

type View owner struct {
	c *Counter
}

var keep []*Counter

func (v *View) Init() {
	c := alloc.New[Counter]()
	keep = []*Counter{c}
	v.c = c
}

func read(v View) { println(v.c.n) }

func main() {
	var v View
	v.Init()
	bump(keep[0])
	task read(v)
}`, []string{"is a view of Counter that may change while another task uses it", "E3501"}},

	{"owner filled with memory its function keeps", `
import "pluto/alloc"

type Counter struct{ n int }

func bump(c *Counter) { c.n++ }

type View owner struct {
	c *Counter
}

func read(v View) { println(v.c.n) }

func main() {
	c := alloc.New[Counter]()
	task read(View{c: c})
	bump(c)
}`, []string{"is a view of Counter that may change while another task uses it", "E3501"}},

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
	task work(&c, done)
	<-done
	println(c.Peek())
}`, []string{"main.p:26:2: &c is a view of Counter that may change while another task uses it",
		"Its field n is changed at main.p:11 (in (*Counter).Add) and read at main.p:15 (in (*Counter).Peek) without a lock that every change holds."}},

	{"view sent on a channel", `
type Job struct{ id int }

var job Job

func main() {
	ch := make(chan *Job, 1)
	done := make(chan bool)
	task show(ch, done)
	job.id = 1
	ch <- &job
	job.id = 2
	<-done
}

func show(ch chan *Job, done chan bool) {
	println((<-ch).id)
	done <- true
}`, []string{"main.p:11:5: &job is a view of Job that may change while another task uses it, so it cannot be sent on a channel",
		"It points to the package variable job, which is written at main.p:10 while tasks run."}},

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
	task sum(xs, out)
	xs[0] = 5
	println(<-out)
}`, []string{"xs is a view of int that may change while another task uses it"}},

	{"view of a package variable written later", `
type Config struct{ limit int }

var cfg Config

func show(c *Config, done chan bool) {
	println(c.limit)
	done <- true
}

func main() {
	done := make(chan bool)
	task show(&cfg, done)
	cfg.limit = 3
	<-done
}`, []string{"&cfg is a view of Config", "It points to the package variable cfg, which is written at main.p:14 while tasks run."}},

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
	task show(s, done)
	<-done
}`, []string{"s (holding a *T) is a view of T"}},

	{"interface made not sendable by a conversion elsewhere", `
import "pluto/alloc"

type Node struct{ count shared[int] }

func keep(a any) {}

func lend(n *Node) {
	keep(n)
}

func take(c chan any) { <-c }

func main() {
	n := &Node{}
	n.count = alloc.Shared[int]()
	lend(n)
	c := make(chan any, 1)
	task take(c)
	c <- n
}`, []string{"is an interface that may hold a *Node (converted at main.p:", ", package main)"}},

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
	task record(s, done)
	task read(s, done)
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
	task record(s, done)
	task peek(s, done)
	<-done
	<-done
}`, []string{"the field hits of a synced[Stats] is read here without holding mu, which guards it"}},

	{"task started by WaitGroup.Task", `
import "pluto/sync"

var (
	wg sync.WaitGroup
	n  int
)

func bump() { n++ }

func main() {
	for i := 0; i < 3; i++ {
		wg.Task(bump)
	}
	wg.Wait()
}`, []string{"main.p:9:15: n is written here with no lock that guards it", "n is written at main.p:9 in bump by the task started by a call of sync.(*WaitGroup).Task at main.p:13 with no lock held"}},
}

// Programs that are free of races: each must pass.
var accepted = []struct {
	name string
	src  string
}{
	{"shared box lent to an interface from the frame", `
import "pluto/alloc"

func show(a any) {}

func take(c chan any, done chan bool) {
	<-c
	done <- true
}

func main() {
	sh := alloc.Shared[int]()
	show(sh)
	c := make(chan any, 1)
	done := make(chan bool)
	task take(c, done)
	c <- 1
	<-done
}`},

	{"library owners cross with the memory they own", `
import (
	"pluto/bytes"
	"pluto/hash"
	"pluto/os"
	"pluto/strings"
	"pluto/vec"
)

func useVec(v vec.Vec[int])           { println(v.Len()) }
func useMap(m hash.Map[string, int])  { println(m.Len()) }
func useBuilder(b strings.Builder)    { println(b.Len()) }
func useBuffer(b bytes.Buffer)        { println(b.Len()) }
func useFile(f *os.File, done chan bool) { println(f.NameLossy()); done <- true }

func main() {
	v := vec.New[int]()
	v.Push(1)
	task useVec(v)
	m := hash.NewMap[string, int](4)
	m.Set("a", 1)
	task useMap(m)
	var b strings.Builder
	b.WriteString("x")
	task useBuilder(b)
	var buf bytes.Buffer
	buf.WriteString("y")
	task useBuffer(buf)
	done := make(chan bool)
	task useFile(os.Stdout, done)
	<-done
}`},

	{"a vec crosses in a program that formats text (spec 8.8, 12.5)", `
import (
	"pluto/fmt"
	"pluto/hash"
	"pluto/vec"
)

func produce(out chan<- vec.Vec[int]) {
	v := vec.New[int]()
	v.Push(1)
	out <- v
	close(out)
}

func count(m hash.Map[string, int], done chan<- int) { done <- m.Len() }

func main() {
	ch := make(chan vec.Vec[int], 2)
	task produce(ch)
	t := 0
	for v := range ch {
		t += v[0]
	}
	m := hash.NewMap[string, int](4)
	m.Set("a", 1)
	done := make(chan int, 1)
	task count(m, done)
	fmt.Println(t, <-done, fmt.Sprint(t))
}`},

	{"an owner's view field filled by an allocating helper", `
import (
	"pluto/alloc"
	"unsafe"
)

type Counter struct{ n int }

func bump(c *Counter) { c.n++ }

type Pool owner struct {
	items []Counter
	a     alloc.Allocator
}

func grow(a alloc.Allocator, n int) []Counter {
	var c Counter
	p := a.Alloc(uintptr(n)*unsafe.Sizeof(c), unsafe.Alignof(c))
	return unsafe.Slice((*Counter)(p), n)
}

func (p *Pool) Init(n int) {
	p.a = alloc.Default()
	items := grow(p.a, n)
	p.items = items[:n]
	for i := range p.items {
		bump(&p.items[i])
	}
}

func (p *Pool) Drop() {
	p.items = nil
}

func use(p Pool) { println(len(p.items)) }

func main() {
	var p Pool
	p.Init(4)
	task use(p)
}`},

	{"an owner's view field of memory it allocated moves with it", `
import "pluto/alloc"

type node struct {
	next *node
	v    int
}

type List owner struct {
	head *node
}

func (l *List) Push(v int) {
	n := alloc.New[node]()
	n.v, n.next = v, l.head
	l.head = n
}

func (l *List) Drop() {
	for l.head != nil {
		n := l.head
		l.head = n.next
		alloc.Free(n)
	}
}

func sum(l List) {
	t := 0
	for n := l.head; n != nil; n = n.next {
		t += n.v
	}
	println(t)
}

func main() {
	var l List
	l.Push(1)
	l.Push(2)
	task sum(l)
}`},
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
	task work(&c, done)
	task work(&c, done)
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
	task c.bump(done)
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
		task func() { done <- true }()
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
	task work(0, done)
	task work(1, done)
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
	task show(&table, done)
	task show(&table, done)
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
	task add()
	task add()
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
	task put(1, 2, done)
	task put(2, 3, done)
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
	task reader(done)
	task writer(done)
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
	task work(done)
	task work(done)
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
		task worker(i, done)
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
	task try(done)
	task try(done)
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
	task handle(done)
	task handle(done)
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
	task use(done)
	task use(done)
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
	task walk(list, Point{1, 2}, done)
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
	task work(&wg, &mu)
	task work(&wg, &mu)
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
	task sum(table, out)
	task sum(table[1:], out)
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
	task logger(done)
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
	task record(s, done)
	task record(s, done)
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
