package race_test

import (
	"strings"
	"testing"
)

// Interrupt gates and handler regions (language spec sections 19.3, 20.8 and
// 20.10, decision D38): the body of a //pu:interrupt handler, entered through
// an interrupt gate, is an interrupt-disabled region until a call of x86.Sti
// or of a function that may switch tasks. The body of a
// //pu:interrupt trap handler is not.

var trapAccepted = []struct{ name, src string }{
	{"an interrupt handler's body is a region", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks++
	x := ticks
	ticks = x + 1
}

func main() { _ = x86.InterruptGate(timer, 0x08, 0) }`},
	{"a trap handler opens its own region", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt trap
func timer(f *x86.InterruptFrame) {
	fl := x86.SaveFlagsCli()
	ticks++
	x86.RestoreFlags(fl)
}

func main() { _ = x86.TrapGate(timer, 0x08, 0) }`},
	{"a trap handler with Cli and Sti", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt trap
func timer(f *x86.InterruptFrame) {
	x86.Cli()
	x := ticks
	ticks = x + 1
	x86.Sti()
}

func main() { _ = x86.TrapGate(timer, 0x08, 0) }`},
	{"a trap handler's lone accesses", `
import "pluto/arch/x86"

type Thread struct{ id int }

//pu:percpu
var current *Thread

var boot Thread

//pu:interrupt trap
func fault(f *x86.InterruptFrame) {
	t := current
	_ = t
	current = &boot
}

func main() { _ = x86.TrapGate(fault, 0x08, 0) }`},
	{"a per-CPU variable only a plain handler uses, in a trap handler's region", `
import "pluto/arch/x86"

//pu:percpu
var a uint64

//pu:percpu
var b uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) { a++ }

//pu:interrupt trap
func fault(f *x86.InterruptFrame) {
	b = 1
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	_ = x86.TrapGate(fault, 0x08, 0)
}`},
	{"a trap handler and atomic data", `
import (
	"pluto/arch/x86"
	"pluto/sync/atomic"
)

var n atomic.Int64

//pu:interrupt trap
func fault(f *x86.InterruptFrame) { n.Add(1) }

func main() {
	_ = x86.TrapGate(fault, 0x08, 0)
	println(n.Load())
}`},
}

func TestTrapAccepted(t *testing.T) {
	for _, c := range trapAccepted {
		t.Run(c.name, func(t *testing.T) {
			if err := checkKernel(t, c.src); err != nil {
				t.Fatalf("rejected:\n%s", fullText(err))
			}
		})
	}
}

var trapRejected = []struct {
	name, src string
	want      []string
	n         int
}{
	{"a trap handler's body is not a region", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt trap
func timer(f *x86.InterruptFrame) {
	ticks++
}

func main() { _ = x86.TrapGate(timer, 0x08, 0) }`,
		[]string{"main.p:9:2: ticks++ uses the per-CPU variable ticks outside a region with interrupts disabled, which it needs because the interrupt handler timer also uses it", "E2022"}, 1},
	{"a trap handler's dependent store", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt trap
func timer(f *x86.InterruptFrame) {
	x := ticks
	ticks = x + 1
}

func main() { _ = x86.TrapGate(timer, 0x08, 0) }`,
		[]string{"main.p:10:2: ticks = ... uses the per-CPU variable ticks outside a region with interrupts disabled"}, 1},
	{"preemption disabled is not enough in a trap handler", `
import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:percpu
var ticks uint64

//pu:interrupt trap
func timer(f *x86.InterruptFrame) {
	rt.DisablePreemption()
	ticks++
	rt.EnablePreemption()
}

func main() { _ = x86.TrapGate(timer, 0x08, 0) }`,
		[]string{"main.p:13:2: ticks++ uses the per-CPU variable ticks outside a region with interrupts disabled"}, 1},
	{"a trap handler's use after its own region ends", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt trap
func timer(f *x86.InterruptFrame) {
	x86.Cli()
	ticks++
	x86.Sti()
	ticks++
}

func main() { _ = x86.TrapGate(timer, 0x08, 0) }`,
		[]string{"main.p:12:2: ticks++ uses the per-CPU variable ticks outside a region with interrupts disabled"}, 1},
	{"a trap handler and a task share a variable", `
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var n int

//pu:interrupt trap
func fault(f *x86.InterruptFrame) { n++ }

func work() { n++ }

func main() {
	_ = x86.TrapGate(fault, 0x08, 0)
	task work()
	rt.Yield()
}`,
		[]string{"the interrupt handler fault", "E3502"}, -1},
}

func TestTrapRejected(t *testing.T) {
	for _, c := range trapRejected {
		t.Run(c.name, func(t *testing.T) {
			err := checkKernel(t, c.src)
			if err == nil {
				t.Fatal("accepted")
			}
			out := fullText(err)
			got := strings.Join(strings.Fields(out), " ")
			for _, w := range c.want {
				if !strings.Contains(got, strings.Join(strings.Fields(w), " ")) {
					t.Errorf("output lacks %q:\n%s", w, got)
				}
			}
			if n := strings.Count(out, `pluto explain E2022`); c.n >= 0 && n != c.n {
				t.Errorf("%d E2022 errors, want %d:\n%s", n, c.n, out)
			}
		})
	}
}
