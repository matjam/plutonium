package race_test

import (
	"strings"
	"testing"
)

// A call of x86.Sti in an interrupt handler ends its interrupt-disabled
// region (language spec sections 19.3 and 20.10, decision D38, memory-model.md
// section 6.1), and x86.Cli begins one.

var stiAccepted = []struct{ name, src string }{
	{"an interrupt handler's access before Sti", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks++
	x86.Sti()
}

func main() { _ = x86.InterruptGate(timer, 0x08, 0) }`},
	{"an interrupt handler that turns interrupts off again", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	x86.Sti()
	x86.Cli()
	ticks++
}

func main() { _ = x86.InterruptGate(timer, 0x08, 0) }`},
	{"an interrupt handler with its own region after Sti", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	x86.Sti()
	fl := x86.SaveFlagsCli()
	ticks++
	x86.RestoreFlags(fl)
}

func main() { _ = x86.InterruptGate(timer, 0x08, 0) }`},
}

func TestStiAccepted(t *testing.T) {
	for _, c := range stiAccepted {
		t.Run(c.name, func(t *testing.T) {
			if err := checkKernel(t, c.src); err != nil {
				t.Fatalf("rejected:\n%s", fullText(err))
			}
		})
	}
}

var stiRejected = []struct {
	name, src string
	want      []string
	n         int
}{
	{"an access after Sti in an interrupt handler", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks++
	x86.Sti()
	ticks++
}

func main() { _ = x86.InterruptGate(timer, 0x08, 0) }`,
		[]string{"main.p:11:2: ticks++ uses the per-CPU variable ticks outside a region with interrupts disabled, which it needs because the interrupt handler timer also uses it"}, 1},
	{"a dependent store after Sti in an interrupt handler", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	x := ticks
	x86.Sti()
	ticks = x + 1
}

func main() { _ = x86.InterruptGate(timer, 0x08, 0) }`,
		[]string{"main.p:11:2: ticks = ... uses the per-CPU variable ticks outside a region with interrupts disabled"}, 1},
	{"StiHlt ends the region too", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	x86.StiHlt()
	ticks++
}

func main() { _ = x86.InterruptGate(timer, 0x08, 0) }`,
		[]string{"main.p:10:2: ticks++ uses the per-CPU variable ticks outside a region with interrupts disabled"}, 1},
	{"Sti in a function the handler calls", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

func allow() { x86.Sti() }

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	allow()
	ticks++
}

func main() { _ = x86.InterruptGate(timer, 0x08, 0) }`,
		[]string{"main.p:12:2: ticks++ uses the per-CPU variable ticks outside a region with interrupts disabled"}, 1},
	{"Sti on one path of a handler", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	if ticks > 5 {
		x86.Sti()
	}
	ticks++
}

func main() { _ = x86.InterruptGate(timer, 0x08, 0) }`,
		[]string{"main.p:12:2: ticks++ uses the per-CPU variable ticks outside a region with interrupts disabled"}, 1},
}

func TestStiRejected(t *testing.T) {
	for _, c := range stiRejected {
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
