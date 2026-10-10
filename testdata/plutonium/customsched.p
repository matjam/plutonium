// A program that binds its own scheduler backend with //pu:scheduler,
// replacing the hosted one: a single processor with a FIFO run queue, as a
// small kernel's would be. Puroutines, channels, select and pluto/sync run
// on it unchanged. Its clock is CLOCK_MONOTONIC, and its idle loop sleeps
// until the deadline the runtime gave ArmTimer, then runs the timers, so
// time.Sleep and time.After work on the runtime's shared timer heap.

package main

import (
	"pluto/alloc"
	"pluto/os"
	"pluto/rt"
	"pluto/sync"
	"pluto/syscall"
	"pluto/time"
	"unsafe"
)

// rrTask is the backend's task record, with the runtime's Task at offset 0.
type rrTask struct {
	task  rt.Task
	mem   unsafe.Pointer
	size  uintptr
	alloc alloc.Allocator
}

type roundRobin struct {
	head, tail *rt.Task
	idle       rt.Context
	spawned    int
	freed      int
	switches   int
	due        int64 // the earliest timer's deadline
	armed      int
}

//pu:scheduler
var sched = roundRobin{due: rt.NoDeadline}

var idleStack [16 << 10]byte

// Init runs once, before the first go statement or blocking operation.
func (s *roundRobin) Init() {
	rt.MakeContext(&s.idle, unsafe.Pointer(&idleStack[0]), uintptr(len(idleStack)), idleLoop)
}

// idleLoop runs whatever is runnable. With nothing to run, it sleeps until
// the earliest timer is due and runs the timers; with no timer either,
// every puroutine waits for another.
func idleLoop() {
	for {
		t := sched.pop()
		if t == nil {
			if sched.due == rt.NoDeadline {
				println("deadlock")
				os.Exit(3)
			}
			if wait := sched.due - sched.Now(); wait > 0 {
				ts := timespec{wait / 1000000000, wait % 1000000000}
				syscall.Syscall(syscall.SYS_NANOSLEEP, uintptr(unsafe.Pointer(&ts)), 0, 0)
			}
			sched.due = rt.NoDeadline
			rt.RunTimers() // calls ArmTimer with the next deadline
			continue
		}
		sched.switches++
		rt.Switch(&sched.idle, t.Context())
	}
}

func (s *roundRobin) NewTask(req rt.StackRequest) *rt.Task {
	head := (unsafe.Sizeof(rrTask{}) + 15) &^ 15
	size := head + (req.Size+15)&^15
	mem := alloc.Raw(size, 16, req.Alloc)
	r := (*rrTask)(mem)
	r.mem, r.size, r.alloc = mem, size, req.Alloc
	r.task.SetStack(uintptr(mem)+head, uintptr(mem)+size)
	s.spawned++
	return &r.task
}

func (s *roundRobin) FreeTask(t *rt.Task) {
	r := (*rrTask)(unsafe.Pointer(t))
	r.alloc.Free(r.mem, r.size, 16)
	s.freed++
}

type timespec struct{ sec, nsec int64 }

// Now reads CLOCK_MONOTONIC.
func (s *roundRobin) Now() int64 {
	var ts timespec
	syscall.Syscall(syscall.SYS_CLOCK_GETTIME, 1, uintptr(unsafe.Pointer(&ts)), 0)
	return ts.sec*1000000000 + ts.nsec
}

// ArmTimer records when the earliest timer is due, for the idle loop.
func (s *roundRobin) ArmTimer(deadline int64) {
	s.due = deadline
	s.armed++
}

func (s *roundRobin) Spawn(t *rt.Task) { s.push(t) }
func (s *roundRobin) Wake(t *rt.Task)  { s.push(t) }
func (s *roundRobin) Park(t *rt.Task)  { s.switchFrom(t) }
func (s *roundRobin) Yield(t *rt.Task) { s.switchFrom(t) }

// switchFrom gives the processor to the next runnable task, or to the idle
// loop. The runtime finishes with t (requeues it, or frees it) once the
// switch is complete.
func (s *roundRobin) switchFrom(t *rt.Task) {
	s.switches++
	if next := s.pop(); next != nil {
		rt.Switch(t.Context(), next.Context())
		return
	}
	rt.Switch(t.Context(), &s.idle)
}

func (s *roundRobin) push(t *rt.Task) {
	t.Link = nil
	if s.tail != nil {
		s.tail.Link = t
	} else {
		s.head = t
	}
	s.tail = t
}

func (s *roundRobin) pop() *rt.Task {
	t := s.head
	if t != nil {
		s.head = t.Link
		if s.head == nil {
			s.tail = nil
		}
	}
	return t
}

// The program.

var wg sync.WaitGroup

func square(in <-chan int, out chan<- int) {
	for v := range in {
		out <- v * v
	}
	wg.Done()
}

func ticker(id int, n int, out chan<- string) {
	for i := 0; i < n; i++ {
		rt.Yield()
	}
	if id == 0 {
		out <- "zero"
	} else {
		out <- "one"
	}
}

func main() {
	in := make(chan int)
	out := make(chan int, 10)
	wg.Add(3)
	for i := 0; i < 3; i++ {
		go square(in, out)
	}
	for i := 1; i <= 10; i++ {
		in <- i
	}
	close(in)
	wg.Wait()
	sum := 0
	for i := 0; i < 10; i++ {
		sum += <-out
	}
	println("sum of squares", sum)

	// Yield interleaves: the puroutine that yields fewer times finishes
	// first, whichever started first.
	names := make(chan string, 2)
	go ticker(0, 50, names)
	go ticker(1, 5, names)
	println(<-names, <-names)

	// A select that waits on two channels.
	a := make(chan int)
	b := make(chan int)
	go func(b chan int) { b <- 7 }(b)
	select {
	case v := <-a:
		println("a", v)
	case v := <-b:
		println("b", v)
	}
	// Timers: the idle loop sleeps until each is due.
	start := rt.Now()
	woke := make(chan int, 3)
	for i := 3; i >= 1; i-- {
		go sleepy(i, woke)
	}
	println("woke in order", <-woke, <-woke, <-woke)
	select {
	case v := <-a:
		println("a", v)
	case <-time.After(5 * time.Millisecond):
		println("select timed out")
	}
	println("timers armed", sched.armed > 0, "elapsed", rt.Now()-start >= 20000000)

	println("spawned", sched.spawned, "freed", sched.freed, "switched", sched.switches > 10)
}

// sleepy sleeps i times 5ms: started in reverse, they wake in order.
func sleepy(i int, woke chan<- int) {
	time.Sleep(time.Duration(i) * 5 * time.Millisecond)
	woke <- i
}
