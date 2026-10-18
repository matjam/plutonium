package race_test

import (
	"strings"
	"testing"
)

// Per-CPU programs that must be accepted (language spec section 20.9).
var perCPUAccepted = []struct{ name, src string }{
	{"lone loads and stores of a word", `
type Thread struct{ id int }

//pu:percpu
var current *Thread

//pu:percpu
var depth int

func set(t *Thread) { current = t }

func get() *Thread { return current }

func main() {
	t := get()
	println(t == nil)
	set(&Thread{id: 1})
	d := depth
	println(d)
	depth = 7
	println(current == nil)
}`},
	{"increment with preemption disabled", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func count() {
	rt.DisablePreemption()
	defer rt.EnablePreemption()
	ticks++
	ticks += 2
	ticks = ticks + 1
}

func main() { count() }`},
	{"interrupts disabled with Cli and Sti", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

func main() {
	x86.Cli()
	ticks++
	x86.Sti()
}`},
	{"interrupts disabled with SaveFlagsCli", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

func main() {
	f := x86.SaveFlagsCli()
	ticks++
	x86.RestoreFlags(f)
}`},
	{"an rt.Lock held", `
import "pluto/rt"

//pu:percpu
var ticks uint64

var l rt.Lock

func main() {
	l.Lock()
	defer l.Unlock()
	ticks++
}`},
	{"a TryLock that succeeded", `
import "pluto/rt"

//pu:percpu
var ticks uint64

var l rt.Lock

func main() {
	if l.TryLock() {
		ticks++
		l.Unlock()
	}
}`},
	{"a helper called inside the region", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func bump() { ticks++ }

func main() {
	rt.DisablePreemption()
	bump()
	rt.EnablePreemption()
}`},
	{"a function that returns with the region held", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func enter() { rt.DisablePreemption() }

func main() {
	enter()
	ticks++
	rt.EnablePreemption()
}`},
	{"a region held on every path", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	rt.DisablePreemption()
	if ticks > 3 {
		ticks++
	} else {
		ticks += 2
	}
	rt.EnablePreemption()
}`},
	{"a variable larger than a word and a part of one, in a region", `
import "pluto/rt"

type Stats struct{ a, b, c uint64 }

//pu:percpu
var stats Stats

func main() {
	rt.DisablePreemption()
	stats.a++
	s := stats
	stats = Stats{s.b, s.c, s.a}
	rt.EnablePreemption()
}`},
	{"a pointer taken and used inside the region", `
import "pluto/rt"

type Stats struct{ a, b uint64 }

//pu:percpu
var stats Stats

func add(s *Stats) { s.a++ }

func (s *Stats) bump() { s.b++ }

func main() {
	rt.DisablePreemption()
	p := &stats
	p.a++
	add(p)
	stats.bump()
	add(&stats)
	rt.EnablePreemption()
}`},
	{"a handler and a task, the task with interrupts disabled", `
import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks++
}

func work() {
	f := x86.SaveFlagsCli()
	ticks++
	x86.RestoreFlags(f)
}

func main() {
	_ = x86.HandlerAddr(timer)
	task work()
	rt.Yield()
}`},
	{"a handler's body is a region until a task switch", `
import "pluto/arch/x86"

//pu:percpu
var seen [4]uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	seen[0]++
	seen[1] = seen[0]
}

func main() { _ = x86.HandlerAddr(timer) }`},
	{"a func literal called in the region", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func within(f func()) {
	rt.DisablePreemption()
	f()
	rt.EnablePreemption()
}

func main() {
	within(func() { ticks++ })
}`},
}

func TestPerCPUAccepted(t *testing.T) {
	for _, c := range perCPUAccepted {
		t.Run(c.name, func(t *testing.T) {
			if err := checkKernel(t, c.src); err != nil {
				t.Fatalf("rejected:\n%s", fullText(err))
			}
		})
	}
}

// Per-CPU programs that must be rejected, with substrings of the output.
var perCPURejected = []struct {
	name, src string
	want      []string
}{
	{"an increment outside a region", `
//pu:percpu
var ticks uint64

func main() {
	ticks++
}`, []string{"main.p:6:2: ticks++ uses the per-CPU variable ticks outside a region where the task cannot move to another processor", "E2022"}},
	{"a compound assignment", `
//pu:percpu
var ticks uint64

func main() {
	ticks += 2
}`, []string{"main.p:6:2: ticks += ... uses the per-CPU variable ticks outside a region", "E2022"}},
	{"a read and a write in one statement", `
//pu:percpu
var ticks uint64

func main() {
	ticks = ticks + 1
}`, []string{"main.p:6:2: ticks = ... uses the per-CPU variable ticks outside a region", "a read and a write in one statement", "E2022"}},
	{"after the region ends", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	rt.DisablePreemption()
	ticks++
	rt.EnablePreemption()
	ticks++
}`, []string{"main.p:11:2: ticks++ uses the per-CPU variable ticks outside a region"}},
	{"a region on only one path", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	if ticks == 0 {
		rt.DisablePreemption()
	}
	ticks++
}`, []string{"main.p:11:2: ticks++ uses the per-CPU variable ticks outside a region"}},
	{"a helper called outside the region", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func bump() { ticks++ }

func main() {
	rt.DisablePreemption()
	bump()
	rt.EnablePreemption()
	bump()
}`, []string{"main.p:7:15: ticks++ uses the per-CPU variable ticks outside a region"}},
	{"a variable larger than a word", `
type Stats struct{ a, b, c uint64 }

//pu:percpu
var stats Stats

func main() {
	s := stats
	println(s.a)
}`, []string{"main.p:8:7: stats uses the per-CPU variable stats outside a region", "larger than a machine word", "E2022"}},
	{"a part of a variable", `
type Pair struct{ a, b uint32 }

//pu:percpu
var pair Pair

func main() {
	println(pair.a)
}`, []string{"main.p:8:10: pair.a uses the per-CPU variable pair outside a region", "part of the variable", "E2022"}},
	{"an element of an array", `
//pu:percpu
var counts [4]uint32

func main() {
	counts[1] = 3
}`, []string{"main.p:6:2: counts[1] = ... uses the per-CPU variable counts outside a region", "E2022"}},
	{"the address taken outside a region", `
//pu:percpu
var ticks uint64

func main() {
	p := &ticks
	*p = 1
}`, []string{"main.p:6:8: &ticks uses the per-CPU variable ticks outside a region", "E2022"}},
	{"a pointer-receiver call outside a region", `
type Stats struct{ a uint64 }

func (s *Stats) bump() { s.a++ }

//pu:percpu
var stats Stats

func main() {
	stats.bump()
}`, []string{"main.p:10:2: stats.bump uses the per-CPU variable stats outside a region", "a call of a method with a pointer receiver", "E2022"}},
	{"preemption disabled is not enough when a handler uses the variable", `
import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks++
}

func main() {
	_ = x86.HandlerAddr(timer)
	rt.DisablePreemption()
	ticks++
	rt.EnablePreemption()
}`, []string{"main.p:18:2: ticks++ uses the per-CPU variable ticks outside a region with interrupts disabled, which it needs because the interrupt handler timer also uses it",
		"E2022", "x86.SaveFlagsCli"}},
	{"a handler's use after a call that switches tasks", `
import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	rt.Yield()
	ticks++
}

func main() {
	_ = x86.HandlerAddr(timer)
}`, []string{"main.p:13:2: ticks++ uses the per-CPU variable ticks outside a region with interrupts disabled"}},
	{"a task with no region beside a handler", `
import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) { ticks = 0 }

func work() {
	ticks++
}

func main() {
	_ = x86.HandlerAddr(timer)
	task work()
	rt.Yield()
}`, []string{"main.p:14:2: ticks++ uses the per-CPU variable ticks outside a region with interrupts disabled"}},
	{"a pointer used after the region", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	rt.DisablePreemption()
	p := &ticks
	rt.EnablePreemption()
	*p = 1
}`, []string{"main.p:11:2: a pointer to the per-CPU variable ticks is used outside the region it was taken in", "E2023"}},
	{"a pointer used in a later region", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	rt.DisablePreemption()
	p := &ticks
	rt.EnablePreemption()
	rt.DisablePreemption()
	*p = 1
	rt.EnablePreemption()
}`, []string{"main.p:12:2: a pointer to the per-CPU variable ticks is used outside the region it was taken in", "E2023"}},
	{"a pointer stored in a package variable", `
import "pluto/rt"

//pu:percpu
var ticks uint64

var saved *uint64

func main() {
	rt.DisablePreemption()
	saved = &ticks
	rt.EnablePreemption()
}`, []string{"main.p:11:2: a pointer to the per-CPU variable ticks is stored where it outlives the region, outside the region it was taken in", "E2023"}},
	{"a pointer returned", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func get() *uint64 {
	rt.DisablePreemption()
	defer rt.EnablePreemption()
	return &ticks
}

func main() { _ = get() }`, []string{"main.p:10:2: a pointer to the per-CPU variable ticks is returned, outside the region it was taken in", "E2023"}},
	{"a pointer sent on a channel", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	ch := make(chan *uint64, 1)
	rt.DisablePreemption()
	ch <- &ticks
	rt.EnablePreemption()
}`, []string{"a pointer to the per-CPU variable ticks is sent on a channel, outside the region it was taken in", "E2023"}},
	{"a pointer passed to a task", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func use(p *uint64) { *p = 1 }

func main() {
	rt.DisablePreemption()
	task use(&ticks)
	rt.EnablePreemption()
}`, []string{"a pointer to the per-CPU variable ticks is passed to a new task, outside the region it was taken in", "E2023"}},
	{"a pointer kept by a callee", `
import "pluto/rt"

//pu:percpu
var ticks uint64

var keep *uint64

func remember(p *uint64) { keep = p }

func main() {
	rt.DisablePreemption()
	remember(&ticks)
	rt.EnablePreemption()
}`, []string{"is passed to remember, which may keep it, outside the region it was taken in", "E2023"}},
	{"a pointer used by a callee after the region ends", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func use(p *uint64) { *p = 1 }

func main() {
	rt.DisablePreemption()
	p := &ticks
	rt.EnablePreemption()
	use(p)
}`, []string{"a pointer to the per-CPU variable ticks is used outside the region it was taken in", "E2023"}},
}

func TestPerCPURejected(t *testing.T) {
	for _, c := range perCPURejected {
		t.Run(c.name, func(t *testing.T) {
			err := checkKernel(t, c.src)
			if err == nil {
				t.Fatal("accepted")
			}
			got := strings.Join(strings.Fields(fullText(err)), " ")
			for _, w := range c.want {
				if !strings.Contains(got, strings.Join(strings.Fields(w), " ")) {
					t.Errorf("output lacks %q:\n%s", w, got)
				}
			}
		})
	}
}
