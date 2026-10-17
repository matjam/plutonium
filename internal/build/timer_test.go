package build_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/build"
)

// runTimerCases runs each program at -O0 and -O2 with one, two and 24
// processors: the per-processor timer heaps, the stealing of expired
// timers and the poller's timeouts all take part with more than one.
func runTimerCases(t *testing.T, cases []concCase, cfg build.Config) {
	progs := buildConcCases(t, cases, cfg)
	for _, c := range cases {
		for _, level := range []int{0, 2} {
			prog := progs[0]
			progs = progs[1:]
			for _, procs := range []string{"1", "2", "24"} {
				t.Run(c.name+"/O"+string(rune('0'+level))+"/procs"+procs, func(t *testing.T) {
					got, status := runConc(t, prog, "PLUTO_MAXPROCS="+procs)
					if status != c.status || got != c.want {
						t.Errorf("status %d, output:\n%s\nwant status %d, output:\n%s", status, got, c.status, c.want)
					}
				})
			}
		}
	}
}

// TestTimers checks time.Sleep, timers, tickers and AfterFunc on the hosted
// scheduler's timers, and that deadlock detection waits for armed timers.
func TestTimers(t *testing.T) {
	cases := []concCase{
		{name: "sleeporder", want: "in order true\nconcurrent true\nnever early true\n", src: `package main

import (
	"pluto/sync"
	"pluto/sync/atomic"
	"pluto/time"
)

var (
	mu    sync.Mutex
	order [20]int
	n     int
	wg    sync.WaitGroup
	early atomic.Int64
)

// sleeper i sleeps (i+1)*10ms: started in reverse, they wake in order.
func sleeper(i int) {
	time.Sleep(time.Duration(i+1) * 10 * time.Millisecond)
	mu.Lock()
	order[n] = i
	n++
	mu.Unlock()
	wg.Done()
}

// many sleeps for d and checks it did not wake before.
func many(d time.Duration) {
	start := time.Now()
	time.Sleep(d)
	if time.Since(start) < d {
		early.Add(1)
	}
	wg.Done()
}

func main() {
	start := time.Now()
	wg.Add(20)
	for i := 19; i >= 0; i-- {
		task sleeper(i)
	}
	wg.Wait()
	el := time.Since(start)
	sorted := true
	mu.Lock() // mu guards order
	for i := range order {
		sorted = sorted && order[i] == i
	}
	mu.Unlock()
	println("in order", sorted)
	println("concurrent", el >= 200*time.Millisecond && el < 2*time.Second)

	wg.Add(1000)
	for i := 0; i < 1000; i++ {
		task many(time.Duration(i%50+1) * time.Millisecond)
	}
	wg.Wait()
	println("never early", early.Load() == 0)
}
`},
		{name: "select", want: "timeout true\nvalue 1\nstopped true\nticks true\n", src: `package main

import "pluto/time"

func later(c chan int) {
	time.Sleep(5 * time.Millisecond)
	c <- 1
}

func main() {
	c := make(chan int)
	start := time.Now()
	select {
	case <-c:
		println("received")
	case <-time.After(20 * time.Millisecond):
		println("timeout", time.Since(start) >= 20*time.Millisecond)
	}

	// The channel wins against a long timer, which is then stopped.
	task later(c)
	t := time.NewTimer(time.Hour)
	select {
	case v := <-c:
		println("value", v)
	case <-t.C:
		println("timer")
	}
	println("stopped", t.Stop())

	// A ticker and a timeout in one select loop.
	tk := time.NewTicker(2 * time.Millisecond)
	timeout := time.NewTimer(30 * time.Millisecond)
	ticks := 0
loop:
	for {
		select {
		case <-tk.C:
			ticks++
		case <-timeout.C:
			break loop
		}
	}
	println("ticks", ticks > 0)
}
`},
		{name: "stopreset", want: "bad 0\n", src: `package main

import (
	"pluto/alloc"
	"pluto/sync"
	"pluto/sync/atomic"
	"pluto/time"
)

var (
	bad atomic.Int64
	wg  sync.WaitGroup
)

// A sharedTimer is a timer that several tasks use: they hold synced
// references to it, and the last one to go drops the timer.
type sharedTimer owner struct {
	t owned[time.Timer]
}

// churn re-arms its timer for a few microseconds, lets it race with the
// expiry, and stops it: Stop always reports true (the timer was armed, or
// its time was never received) and leaves nothing in C.
func churn(id int) {
	t := time.NewTimer(time.Hour)
	seed := uint64(id)*0x9E3779B97F4A7C15 + 1
	for i := 0; i < 2000; i++ {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		t.Reset(time.Duration(seed%50) * time.Microsecond)
		x := 0
		for k := 0; k < int(seed>>40%3000); k++ {
			x += k
		}
		if !t.Stop() {
			bad.Add(1)
		}
		select {
		case <-t.C:
			bad.Add(1)
		default:
		}
	}
	// One Reset, one value.
	t.Reset(time.Millisecond)
	<-t.C
	time.Sleep(3 * time.Millisecond)
	select {
	case <-t.C:
		bad.Add(1)
	default:
	}
	wg.Done()
}

// hammer resets and stops a timer that other tasks reset and stop.
func hammer(id int, shared synced[sharedTimer]) {
	for i := 0; i < 2000; i++ {
		if (i+id)%2 == 0 {
			shared.t.Reset(time.Duration(i%7) * time.Microsecond)
		} else {
			shared.t.Stop()
		}
	}
	wg.Done()
}

func main() {
	shared := alloc.Synced[sharedTimer]()
	shared.t = time.NewTimer(time.Hour)
	wg.Add(8)
	for i := 0; i < 8; i++ {
		task churn(i)
	}
	wg.Wait()

	wg.Add(4)
	for i := 0; i < 4; i++ {
		task hammer(i, shared)
	}
	wg.Wait()
	shared.t.Stop()
	select {
	case <-shared.t.C:
		bad.Add(1)
	default:
	}
	shared.t.Reset(time.Millisecond)
	<-shared.t.C
	println("bad", bad.Load())
}
`},
		{name: "ticker", want: "five ticks true\nstopped\nbuffered 1\ntick true\nnil tick true\n", src: `package main

import "pluto/time"

func main() {
	start := time.Now()
	tk := time.NewTicker(5 * time.Millisecond)
	for i := 0; i < 5; i++ {
		<-tk.C
	}
	println("five ticks", time.Since(start) >= 25*time.Millisecond)

	tk.Reset(time.Millisecond)
	<-tk.C
	tk.Stop()
	time.Sleep(5 * time.Millisecond)
	select {
	case <-tk.C:
		println("tick after Stop")
	default:
		println("stopped")
	}

	// A slow receiver: ticks are dropped, not queued.
	tk.Reset(time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	println("buffered", len(tk.C))

	c := time.Tick(time.Millisecond)
	a := <-c
	b := <-c
	println("tick", b.After(a))
	println("nil tick", time.Tick(0) == nil)
}
`},
		{name: "afterfunc", want: "ran true\nagain true\n", src: `package main

import (
	"pluto/sync/atomic"
	"pluto/time"
)

var (
	count atomic.Int64
	done  = make(chan bool, 100)
)

func f() {
	count.Add(1)
	done <- true
}

func main() {
	var ts [50]synced[time.Timer]
	for i := range ts {
		ts[i] = time.AfterFunc(time.Duration(i%10)*time.Millisecond, f)
	}
	stopped := 0
	for i := 4; i < 50; i += 5 {
		if ts[i].Stop() {
			stopped++
		}
	}
	for i := 0; i < 50-stopped; i++ {
		<-done
	}
	time.Sleep(20 * time.Millisecond)
	println("ran", count.Load() == int64(50-stopped))

	// Reset runs a fired function again.
	ts[0].Reset(time.Millisecond)
	<-done
	println("again", count.Load() == int64(51-stopped))
}
`},
		{name: "afterfuncclosure", status: 2, want: "panic: a task started with a function value that captures variables or binds a method value: its closure record lives in the starting frame; pass the values as arguments\n", src: `package main

import (
	"pluto/rt"
	"pluto/time"
)

// The escape analysis rejects passing the literal to AfterFunc (spec
// section 6.3); rt.Pinned hides it, so the check at run time is what
// stops it.
func main() {
	n := 1
	f := func() { n++ }
	time.AfterFunc(time.Millisecond, *rt.Pinned(&f))
}
`},
		{name: "idle", want: "idle while sleeping true\n", src: `package main

import (
	"pluto/sync"
	"pluto/syscall"
	"pluto/time"
	"unsafe"
)

// struct rusage: user and system time (timevals), then 14 counters.
type rusage struct {
	utime, stime [2]int64
	rest         [14]int64
}

// cpuMicros returns the processor time the process has used.
func cpuMicros() int64 {
	var ru rusage
	syscall.Syscall(98, 0, uintptr(unsafe.Pointer(&ru)), 0) // getrusage(RUSAGE_SELF)
	return (ru.utime[0]+ru.stime[0])*1000000 + ru.utime[1] + ru.stime[1]
}

var wg sync.WaitGroup

func sleeper() {
	time.Sleep(300 * time.Millisecond)
	wg.Done()
}

// While every task sleeps, no thread spins: the workers, the
// monitor and the poller all wait in the kernel.
func main() {
	time.Sleep(time.Millisecond) // start the scheduler first
	c0 := cpuMicros()
	wg.Add(100)
	for i := 0; i < 100; i++ {
		task sleeper()
	}
	wg.Wait()
	println("idle while sleeping", cpuMicros()-c0 < 100000)
}
`},
		{name: "sleepsend", want: "received 1\n", src: `package main

import "pluto/time"

// A task that sleeps before it sends keeps the program from being
// reported as deadlocked while main waits.
func later(c chan int) {
	time.Sleep(30 * time.Millisecond)
	c <- 1
}

func main() {
	c := make(chan int)
	task later(c)
	println("received", <-c)
}
`},
		{name: "deadlockafter", status: 2, want: "fatal error: all tasks are asleep - deadlock!\n\ntask 1 [chan receive]:\n", src: `package main

import "pluto/time"

// A timer that nobody keeps is dropped, and so stopped, at the end of its
// statement: it wakes nothing.
func main() {
	time.NewTimer(30 * time.Millisecond)
	c := make(chan int)
	<-c
}
`},
		{name: "deadlockstopped", status: 2, want: "fatal error: all tasks are asleep - deadlock!\n\ntask 1 [chan receive]:\n", src: `package main

import "pluto/time"

// A stopped timer keeps nothing alive.
func main() {
	t := time.NewTimer(time.Hour)
	t.Stop()
	c := make(chan int)
	<-c
}
`},
	}
	runTimerCases(t, cases, build.Config{NoTrace: true})
}

// TestTimersDebugAlloc checks under the debug allocator, which fails the
// program on a leak, that the owned[Timer] and owned[Ticker] that NewTimer
// and NewTicker return stop and free their timers when they are dropped, and
// that the synced[Timer] that AfterFunc returns keeps its timer armed when
// every handle is dropped, stops it for all handles when one stops it, and
// frees it after the function has been started (language spec section 19.6),
// with nothing freed by hand.
func TestTimersDebugAlloc(t *testing.T) {
	prog := buildConc(t, `package main

import (
	"pluto/sync"
	"pluto/sync/atomic"
	"pluto/time"
)

var (
	fired sync.WaitGroup
	ran   atomic.Int64
	late  atomic.Int64
)

func f()     { fired.Done() }
func run()   { ran.Add(1) }
func never() { late.Add(1) }

func use(d time.Duration) {
	t := time.NewTimer(d)
	<-t.C
} // t is dropped: stopped and freed

// copied returns a copy of the channel of a timer that it drops before
// the timer fires.
func copied() <-chan time.Time {
	t := time.NewTimer(time.Millisecond)
	return t.C
}

func main() {
	use(time.Millisecond)
	use(0)
	c := copied()
	tk := time.NewTicker(time.Millisecond)
	<-tk.C
	fired.Add(1)
	af := time.AfterFunc(time.Millisecond, f)
	fired.Wait()
	println("stop after it ran", af.Stop())
	fired.Add(1)
	println("reset after it ran", af.Reset(time.Millisecond))
	fired.Wait()
	{
		time.AfterFunc(time.Millisecond, run) // the statement leaves the timer armed
		dropped := time.AfterFunc(time.Millisecond, run)
		_ = dropped
	} // dropped is dropped here, and its timer still runs
	stopped := time.AfterFunc(time.Hour, never)
	copied := stopped
	println("stop through a copy", copied.Stop(), stopped.Stop())
	time.NewTimer(time.Millisecond) // dropped at the end of the statement
	time.Sleep(20 * time.Millisecond)
	select {
	case <-c:
		println("the dropped timer fired")
	default:
		println("the copied channel got nothing")
	}
	println("dropped AfterFunc ran", ran.Load(), "stopped AfterFunc ran", late.Load())
	pending := time.NewTimer(time.Hour) // dropped when main returns
	println("pending", pending.Stop())
}
`, build.Config{OptLevel: 2, Alloc: "debug"})
	out, status := runConc(t, prog, "PLUTO_DEBUGALLOC=leaks=fail")
	want := "stop after it ran false\nreset after it ran false\nstop through a copy true false\nthe copied channel got nothing\ndropped AfterFunc ran 2 stopped AfterFunc ran 0\npending true\n"
	if status != 0 || out != want {
		t.Errorf("status %d, output:\n%s", status, out)
	}
}
