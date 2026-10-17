package race_test

import (
	"strings"
	"testing"
)

// Programs with an interrupt handler that must be accepted: a handler is a
// party in the race rules (language spec section 19.3), so what it shares
// is atomic or guarded by an rt.Lock.
var handlerAccepted = []struct{ name, src string }{
	{"an atomic counter", `
import (
	"pluto/arch/x86"
	"pluto/sync/atomic"
)

var ticks atomic.Int64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks.Add(1)
}

func main() {
	_ = x86.HandlerAddr(timer)
	println(ticks.Load())
}`},
	{"atomic functions", `
import (
	"pluto/arch/x86"
	"pluto/sync/atomic"
)

var ticks int64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	atomic.AddInt64(&ticks, 1)
}

func main() {
	_ = x86.HandlerAddr(timer)
	println(atomic.LoadInt64(&ticks))
}`},
	{"an rt.Lock held at every access", `
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var (
	mu    rt.Lock
	queue [8]int
	head  int
)

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	mu.Lock()
	queue[head%8]++
	head++
	mu.Unlock()
}

func main() {
	_ = x86.HandlerAddr(timer)
	mu.Lock()
	n := head
	mu.Unlock()
	println(n)
}`},
	{"data written before the handler is installed", `
import "pluto/arch/x86"

var table [4]int

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	println(table[1])
}

func main() {
	table[1] = 7
	_ = x86.HandlerAddr(timer)
}`},
	{"a non-blocking send to a channel", `
import "pluto/arch/x86"

var events = make(chan int, 4)

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	select {
	case events <- 1:
	default:
	}
}

func main() {
	_ = x86.HandlerAddr(timer)
	<-events
}`},
	{"a handler that touches only its frame and per-CPU data", `
import "pluto/arch/x86"

//pu:percpu
var depth int

//pu:interrupt
func fault(f *x86.InterruptFrame, code uint64) {
	depth = int(code)
	f.RIP += 2
}

func main() { _ = x86.HandlerAddr(fault) }`},
	{"a handler that reads what only it and the main task read", `
import "pluto/arch/x86"

var limit = 10

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	println(limit)
}

func main() {
	_ = x86.HandlerAddr(timer)
	println(limit)
}`},
}

func init() {
	handlerAccepted = append(handlerAccepted, struct{ name, src string }{"an allocator with atomic state that a handler reaches", `
import (
	"pluto/alloc"
	"pluto/arch/x86"
	"pluto/sync/atomic"
	"unsafe"
)

var next atomic.Uintptr

type bump struct{}

func (bump) Alloc(size, align uintptr) unsafe.Pointer {
	next.Add(size)
	return nil
}

func (bump) Free(p unsafe.Pointer, size, align uintptr) {}

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	p := new(int)
	_ = p
}

func main() {
	alloc.SetDefault(bump{})
	_ = x86.HandlerAddr(timer)
	q := new(int)
	_ = q
}`})
	handlerRejected = append(handlerRejected, struct {
		name, src string
		want      []string
	}{"an allocator with plain state that a handler reaches", `
import (
	"pluto/alloc"
	"pluto/arch/x86"
	"unsafe"
)

var next uintptr

type bump struct{}

func (bump) Alloc(size, align uintptr) unsafe.Pointer {
	next += size
	return nil
}

func (bump) Free(p unsafe.Pointer, size, align uintptr) {}

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	p := new(int)
	_ = p
}

func main() {
	alloc.SetDefault(bump{})
	_ = x86.HandlerAddr(timer)
	q := new(int)
	_ = q
}`, []string{"main.p:13:2: next is written here with no lock that guards it", "the interrupt handler timer", "E3502"}})
}

func TestHandlerAccepted(t *testing.T) {
	for _, c := range handlerAccepted {
		t.Run(c.name, func(t *testing.T) {
			if err := checkKernel(t, c.src); err != nil {
				t.Fatalf("rejected:\n%s", fullText(err))
			}
		})
	}
}

// Programs with an interrupt handler that must be rejected.
var handlerRejected = []struct {
	name, src string
	want      []string
}{
	{"a plain variable shared with the main task", `
import "pluto/arch/x86"

var ticks int

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks++
}

func main() {
	_ = x86.HandlerAddr(timer)
	println(ticks)
}`, []string{"main.p:8:2: ticks is written here with no lock that guards it, while another task or an interrupt handler may use it",
		"the interrupt handler timer installed at main.p:12", "E3502"}},
	{"a handler that writes alone may run on several processors", `
import "pluto/arch/x86"

var count int

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	count++
}

func main() {
	_ = x86.HandlerAddr(timer)
}`, []string{"main.p:8:2: count is written here with no lock that guards it", "the interrupt handler timer", "E3502"}},
	{"a read without the guard", `
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var (
	mu    rt.Lock
	state int
)

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	mu.Lock()
	state = 1
	mu.Unlock()
}

func main() {
	_ = x86.HandlerAddr(timer)
	println(state)
}`, []string{"main.p:21:10: state is read here without holding mu, which guards it", "the interrupt handler timer", "E3503"}},
	{"volatile is not enough", `
import "pluto/arch/x86"

var ticks volatile[uint64]

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks = ticks + 1
}

func main() {
	_ = x86.HandlerAddr(timer)
	println(ticks)
}`, []string{"ticks is written here with no lock that guards it", "the interrupt handler timer", "E3502"}},
	{"an interrupt-disabled region is not enough", `
import "pluto/arch/x86"

var ticks int

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks++
}

func main() {
	_ = x86.HandlerAddr(timer)
	f := x86.SaveFlagsCli()
	ticks = 0
	x86.RestoreFlags(f)
}`, []string{"ticks is written here with no lock that guards it", "the interrupt handler timer", "E3502"}},
	{"a handler reached through a helper and a task", `
import "pluto/arch/x86"

var log [4]int

func record(i int) { log[i&3] = i }

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	record(1)
}

func work() { record(2) }

func main() {
	_ = x86.HandlerAddr(timer)
	task work()
}`, []string{"main.p:6:22: log[i] is written here with no lock that guards it", "E3502"}},
	{"a lock of a mutex a handler cannot take", `
import (
	"pluto/arch/x86"
	"pluto/sync"
)

var (
	mu sync.Mutex
	n  int
)

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	n = 1
}

func main() {
	_ = x86.HandlerAddr(timer)
	mu.Lock()
	n = 2
	mu.Unlock()
}`, []string{"n is written here with no lock that guards it", "E3502"}},
}

func TestHandlerRejected(t *testing.T) {
	for _, c := range handlerRejected {
		t.Run(c.name, func(t *testing.T) {
			err := checkKernel(t, c.src)
			if err == nil {
				t.Fatal("accepted")
			}
			got := strings.Join(strings.Fields(fullText(err)), " ")
			for _, w := range c.want {
				if !strings.Contains(got, strings.Join(strings.Fields(w), " ")) {
					t.Errorf("output lacks %q:\n%s", w, fullText(err))
				}
			}
		})
	}
}
