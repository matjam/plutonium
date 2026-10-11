// A kernel, built with --mode kernel, that implements the runtime's
// scheduler contract (language spec section 4.7, abi.md section 12) with a
// round-robin scheduler on one processor: per-CPU state reached through
// GS, a FIFO run queue, an idle loop that halts, and preemption from the
// PIT timer interrupt. Tasks, channels, select and pluto/sync run on
// it; the timer handler sends on a channel with a non-blocking select.
// The PIT's tick count is the scheduler's clock (Now), and the handler
// runs the runtime's timers when the deadline given to ArmTimer has
// passed, so time.Sleep, time.After and tickers work too. Output goes to
// the first serial port.
package main

import (
	"pluto/alloc"
	"pluto/arch/x86"
	"pluto/rt"
	"pluto/sync"
	"pluto/sync/atomic"
	"pluto/time"
	"unsafe"
)

// Implemented in boot_amd64.s.
func percpuImage() uintptr
func percpuSize() uintptr

const com1 = 0x3F8

func serialWrite(p unsafe.Pointer, n int) {
	for i := 0; i < n; i++ {
		for x86.Inb(com1+5)&0x20 == 0 {
			x86.Pause()
		}
		x86.Outb(com1, *(*uint8)(unsafe.Add(p, i)))
	}
}

// onPanic prints the panic and leaves QEMU with status 3.
//
//pu:panic
func onPanic(p *rt.Panic) {
	p.Print(rt.Console{})
	for {
		x86.Outl(0xf4, 3)
		x86.Cli()
		x86.Hlt()
	}
}

// ----------------------------------------------------------------------------
// Memory: pages from a static arena, never returned (enough for a test).

var arena aligned[[8 << 20]byte, 4096]

type bumpPages struct{ next uintptr }

func (b *bumpPages) AllocPages(n uintptr) unsafe.Pointer {
	base := uintptr(unsafe.Pointer(&arena))
	if b.next+n*alloc.PageSize > uintptr(len(arena)) {
		return nil
	}
	p := unsafe.Pointer(base + b.next)
	b.next += n * alloc.PageSize
	return p
}

func (b *bumpPages) FreePages(p unsafe.Pointer, n uintptr) {}

var pages bumpPages

// ----------------------------------------------------------------------------
// The scheduler: the runtime's contract, implemented for one processor.

// task is the kernel's task record; the runtime's Task comes first.
type task struct {
	rt     rt.Task
	mem    unsafe.Pointer
	size   uintptr
	allocr alloc.Allocator
}

// cpuState is one processor's scheduling state, a per-CPU variable.
type cpuState struct {
	head, tail *rt.Task
	idle       rt.Context
	inIRQ      int
}

//pu:percpu
var cpu cpuState

type kernelSched struct{}

//pu:scheduler
var scheduler kernelSched

var idleStack aligned[[16 << 10]byte, 16]

// Init runs on the boot processor before the first task operation; GS
// already points at its per-CPU block.
func (kernelSched) Init() {
	rt.MakeContext(&cpu.idle, unsafe.Pointer(&idleStack), uintptr(len(idleStack)), idle)
}

func (kernelSched) NewTask(req rt.StackRequest) *rt.Task {
	head := (unsafe.Sizeof(task{}) + 15) &^ 15
	size := head + (req.Size+15)&^15
	mem := alloc.Raw(size, 16, req.Alloc)
	t := (*task)(mem)
	t.mem, t.size, t.allocr = mem, size, req.Alloc
	t.rt.SetStack(uintptr(mem)+head, uintptr(mem)+size)
	return &t.rt
}

func (kernelSched) FreeTask(rtt *rt.Task) {
	t := (*task)(unsafe.Pointer(rtt))
	t.allocr.Free(t.mem, t.size, 16)
}

func (kernelSched) Spawn(t *rt.Task) { push(t) }

// Wake may run in the timer interrupt; push masks interrupts itself.
func (kernelSched) Wake(t *rt.Task) { push(t) }

func (kernelSched) Park(t *rt.Task)  { switchFrom(t) }
func (kernelSched) Yield(t *rt.Task) { switchFrom(t) }

func (kernelSched) InInterrupt() bool { return cpu.inIRQ > 0 }

// Now is the scheduler's clock: the PIT ticks once a millisecond.
func (kernelSched) Now() int64 { return int64(ticks) * 1000000 }

// ArmTimer records when the runtime's earliest timer is due; the timer
// handler runs the timers once it has passed. It runs with interrupts
// disabled.
func (kernelSched) ArmTimer(deadline int64) { timerDue = deadline }

// switchFrom runs the next task, or the idle loop, in place of t.
func switchFrom(t *rt.Task) {
	if next := pop(); next != nil {
		rt.Switch(t.Context(), next.Context())
		return
	}
	rt.Switch(t.Context(), &cpu.idle)
}

func push(t *rt.Task) {
	f := x86.SaveFlagsCli()
	t.Link = nil
	if cpu.tail != nil {
		cpu.tail.Link = t
	} else {
		cpu.head = t
	}
	cpu.tail = t
	x86.RestoreFlags(f)
}

func pop() *rt.Task {
	f := x86.SaveFlagsCli()
	t := cpu.head
	if t != nil {
		cpu.head = t.Link
		if cpu.head == nil {
			cpu.tail = nil
		}
	}
	x86.RestoreFlags(f)
	return t
}

// idle runs runnable tasks, and halts until the next interrupt when there
// are none.
func idle() {
	for {
		x86.Cli()
		t := pop()
		if t == nil {
			x86.StiHlt()
			continue
		}
		rt.Switch(&cpu.idle, t.Context())
	}
}

// ----------------------------------------------------------------------------
// Interrupts: the PIT at 1 kHz on vector 32, through the remapped PIC.

var idt [256][2]uint64

func setGate(vec int, h uintptr) {
	addr := uint64(h)
	idt[vec][0] = addr&0xFFFF | 0x08<<16 | 0x8E<<40 | (addr>>16&0xFFFF)<<48
	idt[vec][1] = addr >> 32
}

var (
	ticks     volatile[uint64]
	tickCh    chan uint64
	tickSends volatile[uint64]
	timerDue  volatile[int64] = rt.NoDeadline
)

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	cpu.inIRQ++
	ticks = ticks + 1
	x86.Outb(0x20, 0x20) // end of interrupt
	if ticks%16 == 0 {
		select { // a non-blocking send is allowed in interrupt context
		case tickCh <- ticks:
			tickSends = tickSends + 1
		default:
		}
	}
	if int64(ticks)*1000000 >= timerDue {
		timerDue = rt.NoDeadline
		rt.RunTimers() // gives the next deadline to ArmTimer
	}
	cpu.inIRQ--
	rt.Preempt() // runs another task; returns when this one is resumed
}

// maskPICs masks every line of both PICs. The firmware leaves the PIT's
// IRQ 0 unmasked on vector 8, and a new task starts with interrupts
// enabled, so without this a tick that arrives between the first task
// statement and startTimer (which a busy host makes likely, as QEMU's
// clock is the host's) is delivered before there is an IDT, and the
// machine triple-faults.
func maskPICs() {
	x86.Outb(0x21, 0xFF)
	x86.Outb(0xA1, 0xFF)
}

func startTimer() {
	// Remap the PICs to vectors 32 to 47 and unmask only IRQ 0, with
	// interrupts off until the IDT is loaded.
	x86.Cli()
	x86.Outb(0x20, 0x11)
	x86.Outb(0xA0, 0x11)
	x86.Outb(0x21, 0x20)
	x86.Outb(0xA1, 0x28)
	x86.Outb(0x21, 4)
	x86.Outb(0xA1, 2)
	x86.Outb(0x21, 1)
	x86.Outb(0xA1, 1)
	x86.Outb(0x21, 0xFE)
	x86.Outb(0xA1, 0xFF)
	// PIT channel 0, rate generator, 1193182 / 1193 = 1 kHz.
	x86.Outb(0x43, 0x36)
	x86.Outb(0x40, 1193&0xFF)
	x86.Outb(0x40, 1193>>8)
	setGate(32, x86.HandlerAddr(timer))
	ptr := x86.NewDescriptorTablePointer(uintptr(unsafe.Pointer(&idt)), len(idt)*16)
	x86.Lidt(&ptr)
	x86.Sti()
}

// ----------------------------------------------------------------------------
// The test.

var block aligned[[512]byte, 64]

func setupPerCPU() {
	b := unsafe.Pointer(&block)
	image := percpuImage()
	for k := uintptr(0); k < percpuSize(); k++ {
		*(*byte)(unsafe.Add(b, k)) = *(*byte)(unsafe.Pointer(image + k))
	}
	*(*uintptr)(b) = uintptr(b)
	x86.WriteGSBase(uintptr(b))
}

var wg sync.WaitGroup

func square(in <-chan int, out chan<- int) {
	for v := range in {
		out <- v * v
	}
	wg.Done()
}

// flag is atomic: two tasks use it.
var flag atomic.Uint32

// spin never waits in its loop: only the timer's preemption lets set run.
func spin(done chan<- bool) {
	for flag.Load() == 0 {
	}
	done <- true
}

func set() { flag.Store(1) }

// sleepy sleeps i times 5ms: started in reverse, they wake in order.
func sleepy(i int, woke chan<- int) {
	time.Sleep(time.Duration(i) * 5 * time.Millisecond)
	woke <- i
}

func main() int64 {
	maskPICs()
	rt.SetConsole(serialWrite)
	setupPerCPU()
	alloc.SetDefault(alloc.NewSlab(&pages))
	tickCh = make(chan uint64, 1)

	in := make(chan int)
	out := make(chan int, 10)
	wg.Add(3)
	for i := 0; i < 3; i++ {
		task square(in, out)
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

	// Maps in kernel mode: no floating point in their hashing, and the
	// fixed seed of builds without a source of entropy.
	counts := map[int]int{}
	for i := 0; i < 100; i++ {
		counts[i%7] += i
	}
	for k := range counts {
		if k%2 == 1 {
			delete(counts, k)
		}
	}
	names := map[string]int{"a": 1}
	names["b"] = 2
	println("map", len(counts), counts[0], counts[6], names["b"], len(names))

	startTimer()
	done := make(chan bool)
	task spin(done)
	task set()
	<-done
	println("spinner preempted by the timer")

	// Ticks sent from the interrupt handler wake a parked task.
	first := <-tickCh
	second := <-tickCh
	println("ticks from the interrupt handler", first > 0, second > first, tickSends >= 2)

	// A select that waits on two channels, one fed by a task.
	a := make(chan int)
	b := make(chan int)
	task func(b chan<- int) { b <- 42 }(b)
	select {
	case v := <-a:
		println("a", v)
	case v := <-b:
		println("b", v)
	}

	// Timers on the PIT clock.
	start := rt.Now()
	time.Sleep(20 * time.Millisecond)
	println("slept", rt.Now()-start >= 20000000)
	woke := make(chan int, 3)
	for i := 3; i >= 1; i-- {
		task sleepy(i, woke)
	}
	println("woke in order", <-woke, <-woke, <-woke)
	select {
	case v := <-a:
		println("a", v)
	case <-time.After(10 * time.Millisecond):
		println("select timed out")
	}
	tk := time.NewTicker(2 * time.Millisecond)
	t1 := <-tk.C
	t2 := <-tk.C
	t3 := <-tk.C
	tk.Stop() // the owned[Ticker] is dropped at the end of its scope
	println("ticker", t2.Sub(t1) >= 2*time.Millisecond, t3.After(t2))

	// Scoped tasks (language spec section 4.11) on this scheduler: they use
	// main's frame, and the Scope waits for them where its block ends.
	// rt.WaitScopes, which a kernel calls before it reclaims the stack of a
	// task that did not return, waits for the tasks of the Scopes the task
	// started (abi.md section 12.10).
	var cubes [4]int
	{
		var s sync.Scope
		for i := range cubes {
			s.Task(func() { cubes[i] = i * i * i })
		}
	}
	println("cubes", cubes[0]+cubes[1]+cubes[2]+cubes[3])
	var marked atomic.Int64
	{
		var s sync.Scope
		for range 3 {
			s.Task(func() { marked.Add(1) })
		}
		rt.WaitScopes(rt.CurrentTask())
		println("waited", marked.Load())
	}
	x86.Cli()
	println("done")
	return 0
}
