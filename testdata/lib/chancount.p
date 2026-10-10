// Counted channels (language spec section 4.8): copies of a channel retain
// it, dropping a copy releases it, and the last release drops the values
// still buffered and frees the channel, with the timer of a time.After,
// time.Tick or time.Timer channel. TestChannelsLeakFree runs this with the
// debug allocator, which fails the program on a leak, with one, two and
// many processors.

package main

import (
	"pluto/alloc"
	"pluto/hash"
	"pluto/strings"
	"pluto/time"
	"pluto/vec"
)

// heap returns a counted copy of s.
func heap(s string) string { return strings.Clone(s) }

// A Res reports its drop.
type Res owner struct{ name string }

func (r *Res) Drop() { println("drop", r.name) }

func res(name string) owned[Res] {
	r := new(Res)
	r.name = heap(name)
	return r
}

// Values still in the buffer when the last reference goes are dropped
// with the channel, after the variables declared later (r).
func buffered() {
	c := make(chan string, 4)
	c <- heap("one")
	c <- heap("two")
	c <- "three"
	println("buffered", <-c, len(c))
	o := make(chan owned[Res], 3)
	o <- res("a")
	o <- res("b")
	o <- res("c")
	r := <-o
	println("received", r.name)
	o <- res("d") // wraps around the ring
	println("left", len(o))
}

// Synced boxes in a buffer are released with the channel; the object is
// dropped by the last reference.
func syncedInBuffer() {
	s := alloc.Synced[Res]()
	s.name = heap("synced")
	c := make(chan synced[Res], 2)
	c <- s
	c <- s
	c = nil
	println("channel gone")
	s = nil
	println("box gone")
}

// An unbuffered channel hands values over between puroutines; each holds
// its own reference to the channels it was given.
func handoff() {
	c := make(chan string)
	done := make(chan bool)
	go func(c <-chan string, done chan<- bool) {
		for s := range c {
			println("got", s)
		}
		done <- true
	}(c, done)
	c <- heap("x")
	c <- "y"
	close(c)
	c = nil // the puroutine's reference keeps the channel
	<-done
}

// Closed channels keep their buffered values until received or freed.
func closed() {
	c := make(chan string, 3)
	c <- heap("kept")
	c <- heap("dropped")
	close(c)
	v, ok := <-c
	println("closed", v, ok, len(c))
	d := make(chan int)
	close(d)
	n, ok := <-d
	println("closed empty", n, ok)
}

// Channels in structs and arrays are counted per copy.
type pipe struct {
	in, out chan string
	name    string
}

func structs() {
	p := pipe{in: make(chan string, 1), out: make(chan string, 1), name: heap("p")}
	q := p
	q.in <- heap("hello")
	println("struct", <-p.in)
	p.out = make(chan string, 1) // q still holds the old one
	p.out <- "new"
	q.out <- heap("old")
	println("struct", <-p.out, p.in == q.in, p.out == q.out, len(q.out))
	arr := [2]pipe{p, q}
	arr[1].out = nil // the old out is still buffering "old": freed with it
	println("array", len(arr[0].in), arr[1].out == nil)
}

// Channels sent on channels: the receiver takes over the sender's
// reference, and a channel left in a buffer is freed with the buffer.
func nested() {
	cc := make(chan chan string, 2)
	inner := make(chan string, 1)
	inner <- heap("deep")
	cc <- inner
	cc <- make(chan string, 1)
	inner = nil
	got := <-cc
	println("nested", <-got, len(cc))
}

// A reply channel per request, sent to a server puroutine.
func server(reqs <-chan chan int) {
	n := 0
	for r := range reqs {
		n++
		r <- n * 10
	}
}

func replies() {
	reqs := make(chan chan int)
	go server(reqs)
	for i := 0; i < 3; i++ {
		r := make(chan int)
		reqs <- r
		println("reply", <-r)
	}
	close(reqs)
}

// A synced box holding a channel, started with go: the puroutine holds a
// reference to the box, and the box to the channel.
type Mailbox owner struct {
	c chan string
}

func (m *Mailbox) Serve(done chan<- int) {
	n := 0
	for s := range m.c {
		n += len(s)
	}
	done <- n
}

func mailbox() {
	m := alloc.Synced[Mailbox]()
	m.c = make(chan string, 2)
	done := make(chan int)
	go m.Serve(done)
	m.c <- heap("abc")
	m.c <- "de"
	close(m.c)
	println("mailbox", <-done)
}

// Channels in containers and interfaces.
func containers() {
	v := vec.New[chan int]()
	for i := 0; i < 3; i++ {
		c := make(chan int, 1)
		c <- i
		v.Push(c)
	}
	sum := 0
	for i := 0; i < v.Len(); i++ {
		sum += <-v.At(i)
	}
	m := hash.NewMap[string, chan int](4)
	m.Set(heap("k"), make(chan int, 1))
	if c, ok := m.Get("k"); ok {
		c <- 7
		sum += <-c
	}
	var x any = m["k"]
	_, isChan := x.(chan int)
	println("containers", sum, isChan)
}

// An owning interface holds a reference of its own, released when it is
// dropped.
type Any owner interface{}

func owning() {
	c := make(chan string, 1)
	var a Any = c
	c = nil
	if d, ok := a.(chan string); ok {
		d <- heap("in the interface")
		println("owning", len(d))
	}
}

// The channel a range statement ranges over stays alive whatever the body
// does to its variable.
func rangeReassign() {
	c := make(chan int, 3)
	c <- 1
	c <- 2
	c <- 3
	close(c)
	sum := 0
	for v := range c {
		c = nil
		sum += v
	}
	println("range", sum, c == nil)
}

// time.After in a select loop: each channel, and its timer, is freed when
// the select ends, so the loop holds no more than one of each.
func timeouts() {
	msgs := make(chan int, 1)
	n := 0
	for i := 0; i < 1000; i++ {
		msgs <- i
		select {
		case m := <-msgs:
			n += m
		case <-time.After(time.Hour):
			println("timed out")
		}
	}
	println("timeouts", n)
	select {
	case <-time.After(time.Millisecond):
		println("after fired")
	}
	tick := time.Tick(time.Millisecond)
	<-tick
	<-tick
	println("ticked")
	t := time.NewTimer(time.Millisecond)
	c := t.C // the channel, and its timer, outlive the Timer
	alloc.Free(t)
	<-c
	println("timer fired after free")
	tk := time.NewTicker(time.Hour)
	tk.Stop()
	alloc.Free(tk)
	println("ticker freed")
}

func main() {
	buffered()
	syncedInBuffer()
	handoff()
	closed()
	structs()
	nested()
	replies()
	mailbox()
	containers()
	owning()
	rangeReassign()
	timeouts()
	// A puroutine releases its arguments after its function returns, which
	// may be after main has received its last value: give them time to
	// finish before the leak check at exit.
	time.Sleep(50 * time.Millisecond)
	println("end")
}
