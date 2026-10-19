package race_test

import (
	"strings"
	"testing"
)

// Dependent per-CPU stores the check must not miss, and cases it must keep
// accepting (language spec section 20.9, decision D37): where it cannot see,
// it rejects.

const reviewHead = `
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ = x86.Pause
var _ = rt.DisablePreemption

//pu:percpu
var ticks uint64
`

const reviewSched = `
type ks struct{}

//pu:scheduler
var sched ks

func (ks) Init()                                {}
func (ks) NewTask(req rt.StackRequest) *rt.Task { return nil }
func (ks) FreeTask(t *rt.Task)                  {}
func (ks) Spawn(t *rt.Task)                     {}
func (ks) Wake(t *rt.Task)                      {}
func (ks) Park(t *rt.Task)                      {}
func (ks) Yield(t *rt.Task)                     {}
func (ks) InInterrupt() bool                    { return false }
func (ks) Now() int64                           { return 0 }
func (ks) ArmTimer(deadline int64)              {}
`

var perCPUReviewRejected = []struct{ name, src string }{
	{"callee writes through a pointer", `
func put(p *uint64, x uint64) { *p = x }

func main() {
	var l uint64
	put(&l, ticks)
	ticks = l + 1
}`},
	{"bodyless callee writes through a pointer", `
//pu:extern ext_put
func ext_put(p *uint64, x uint64)

func main() {
	var l uint64
	ext_put(&l, ticks)
	ticks = l + 1
}`},
	{"store through a pointer from the address", `
func main() {
	x := ticks
	rt.DisablePreemption()
	p := &ticks
	*p = x + 1
	rt.EnablePreemption()
}`},
	{"store through a copied pointer from the address", `
func main() {
	x := ticks
	rt.DisablePreemption()
	p := &ticks
	q := p
	*q = x + 1
	rt.EnablePreemption()
}`},
	{"struct field across two regions", `
type pair struct{ a, b uint32 }

//pu:percpu
var pp pair

func main() {
	rt.DisablePreemption()
	x := pp.a
	rt.EnablePreemption()
	rt.DisablePreemption()
	pp.a = x + 1
	rt.EnablePreemption()
}`},
	{"array element across two regions", `
//pu:percpu
var arr [2]uint32

func main() {
	rt.DisablePreemption()
	x := arr[0]
	rt.EnablePreemption()
	rt.DisablePreemption()
	arr[0] = x + 1
	rt.EnablePreemption()
}`},
	{"struct field in a function that only writes it", `
type pair struct{ a, b uint32 }

//pu:percpu
var pp pair

func set(x uint32) { pp.a = x }

func main() {
	rt.DisablePreemption()
	x := pp.a
	rt.EnablePreemption()
	set(x + 1)
}`},
	{"func value", `
func set(x uint64) { ticks = x }

func main() {
	g := set
	x := ticks
	g(x + 1)
}`},
	{"method value", `
type T struct{ n int }

func (T) set(x uint64) { ticks = x }

func main() {
	var t T
	m := t.set
	x := ticks
	m(x + 1)
}`},
	{"func-typed parameter with a literal", `
func apply(f func(uint64), v uint64) { f(v) }

func main() {
	x := ticks
	apply(func(v uint64) { ticks = v }, x+1)
}`},
	{"func-typed parameter with a declared function", `
func set(x uint64) { ticks = x }

func apply(f func(uint64), v uint64) { f(v) }

func main() {
	x := ticks
	apply(set, x+1)
}`},
	{"method expression", `
type T struct{ n int }

func (T) set(x uint64) { ticks = x }

func main() {
	f := T.set
	x := ticks
	f(T{}, x+1)
}`},
	{"method value and another per-CPU variable", `
//pu:percpu
var other uint64

type T struct{ n int }

func (T) set(x uint64) { ticks = x }

func main() {
	var t T
	m := t.set
	other = 1
	x := ticks
	m(x + 1)
}`},
	{"loop condition", `
func main() {
	for ticks != 0 {
		x86.Pause()
	}
	ticks = 1
}`},
	{"second per-CPU variable", `
//pu:percpu
var a uint64

func main() {
	a = ticks
	ticks = a + 1
}`},
	{"task started in a region", reviewSched + `
func set(x uint64) {
	rt.DisablePreemption()
	ticks = x
	rt.EnablePreemption()
}

func main() {
	rt.DisablePreemption()
	x := ticks
	task set(x + 1)
	rt.EnablePreemption()
}`},
	{"task with a lone store", reviewSched + `
func set(x uint64) { ticks = x }

func main() {
	rt.DisablePreemption()
	x := ticks
	task set(x + 1)
	rt.EnablePreemption()
}`},
	{"yield in a region", `
func main() {
	rt.DisablePreemption()
	x := ticks
	rt.Yield()
	ticks = x + 1
	rt.EnablePreemption()
}`},
	{"yield in a helper in a region", `
func y() { rt.Yield() }

func main() {
	rt.DisablePreemption()
	x := ticks
	y()
	ticks = x + 1
	rt.EnablePreemption()
}`},
	{"increment around a yield", `
func main() {
	rt.DisablePreemption()
	rt.Yield()
	ticks++
	rt.EnablePreemption()
}`},
	{"channel receive in a region", reviewSched + `
func main() {
	ch := make(chan int)
	rt.DisablePreemption()
	x := ticks
	<-ch
	ticks = x + 1
	rt.EnablePreemption()
}`},
	{"field through a pointer from the address", `
type pair struct{ a, b uint32 }

//pu:percpu
var pp pair

func main() {
	x := pp.a
	rt.DisablePreemption()
	p := &pp
	p.a = x + 1
	rt.EnablePreemption()
}`},
	{"task literal started in a region", reviewSched + `
func main() {
	rt.DisablePreemption()
	x := ticks
	task func() {
		rt.DisablePreemption()
		ticks = x + 1
		rt.EnablePreemption()
	}()
	rt.EnablePreemption()
}`},
	{"deferred literal", `
func main() {
	x := ticks
	defer func() { ticks = x + 1 }()
}`},
	{"callee stores through a slice", `
func put(s []uint64, x uint64) { s[0] = x }

func main() {
	var l [1]uint64
	put(l[:], ticks)
	ticks = l[0] + 1
}`},
	{"callee stores through a channel", reviewSched + `
func main() {
	ch := make(chan uint64, 1)
	ch <- ticks
	ticks = <-ch + 1
}`},
}

var perCPUReviewAccepted = []struct{ name, src string }{
	{"literal defined and called in one region", `
func main() {
	rt.DisablePreemption()
	x := ticks
	f := func() { ticks = x + 1 }
	f()
	rt.EnablePreemption()
}`},
	{"independent lone store", `
func main() {
	x := ticks
	println(x)
	ticks = 5
}`},
	{"increment in a region", `
func main() {
	rt.DisablePreemption()
	ticks++
	rt.EnablePreemption()
}`},
	{"increment in a region with a yield after it", `
func main() {
	rt.DisablePreemption()
	ticks++
	rt.EnablePreemption()
	rt.Yield()
}`},
	{"literal called twice in one region", `
func main() {
	rt.DisablePreemption()
	f := func() { ticks++ }
	f()
	f()
	rt.EnablePreemption()
}`},
	{"load and store of a different variable through a call", `
//pu:percpu
var other uint64

func put(p *uint64, x uint64) { *p = x }

func main() {
	var l uint64
	put(&l, other)
	ticks = 3
}`},
	{"a later independent store after a call that runs a local literal", `
func a() {
	rt.DisablePreemption()
	x := ticks
	f := func() { ticks = x + 1 }
	f()
	rt.EnablePreemption()
}

func reset() { ticks = 0 }

func main() {
	a()
	reset()
}`},
}

func TestPerCPUReviewRejected(t *testing.T) {
	for _, c := range perCPUReviewRejected {
		t.Run(c.name, func(t *testing.T) {
			err := checkKernel(t, reviewHead+c.src)
			if err == nil {
				t.Fatal("accepted")
			}
			if out := fullText(err); !strings.Contains(out, "E2022") {
				t.Errorf("no E2022:\n%s", out)
			}
		})
	}
}

func TestPerCPUReviewAccepted(t *testing.T) {
	for _, c := range perCPUReviewAccepted {
		t.Run(c.name, func(t *testing.T) {
			if err := checkKernel(t, reviewHead+c.src); err != nil {
				t.Fatalf("rejected:\n%s", fullText(err))
			}
		})
	}
}
