package race_test

// A pointer from &v is bounded by the instance of the region it was taken
// in, not only by the site that opened the region (language spec section
// 20.9): the same site runs again after a goto, in a recursive call and in a
// second call of a function literal. A pointer through which an interrupt
// handler's variable is used needs interrupts disabled like the variable
// itself (section 20.9, with section 19.3).

func init() {
	type rej = struct {
		name, src string
		want      []string
	}
	type acc = struct{ name, src string }
	perCPURejected = append(perCPURejected,
		rej{"a pointer carried to the next pass of a goto loop", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	var p *uint64
	i := 0
again:
	rt.DisablePreemption()
	if i == 0 {
		p = &ticks
	}
	*p = 4
	rt.EnablePreemption()
	i++
	if i < 2 {
		goto again
	}
}`, []string{"main.p:13:3: a pointer to the per-CPU variable ticks is kept in a variable that outlives the pass of a loop made with goto, outside the region it was taken in", "E2023"}},
		rej{"a parameter assigned in a goto loop", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func loop(p *uint64) {
	i := 0
again:
	rt.DisablePreemption()
	if i == 1 {
		*p = 1
	}
	p = &ticks
	rt.EnablePreemption()
	i++
	if i < 2 {
		goto again
	}
}

func main() { loop(nil) }`, []string{"main.p:14:2: a pointer to the per-CPU variable ticks is kept in a variable that outlives the pass of a loop made with goto", "E2023"}},
		rej{"a pointer passed down a recursive call", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func rec(p *uint64, n int) {
	rt.DisablePreemption()
	if p == nil {
		p = &ticks
	}
	*p = 5
	rt.EnablePreemption()
	if n > 0 {
		rec(p, n-1)
	}
}

func main() {
	rec(nil, 3)
}`, []string{"main.p:12:2: a pointer to the per-CPU variable ticks is used outside the region it was taken in", "E2023"}},
		rej{"a pointer passed down mutually recursive calls", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func ping(p *uint64, n int) {
	rt.DisablePreemption()
	if p == nil {
		p = &ticks
	}
	*p = 5
	rt.EnablePreemption()
	if n > 0 {
		pong(p, n-1)
	}
}

func pong(p *uint64, n int) { ping(p, n) }

func main() {
	ping(nil, 3)
}`, []string{"main.p:12:2: a pointer to the per-CPU variable ticks is used outside the region it was taken in", "E2023"}},
		rej{"a pointer kept in a variable captured by a function literal", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	var p *uint64
	f := func() {
		rt.DisablePreemption()
		if p == nil {
			p = &ticks
		}
		*p = 15
		rt.EnablePreemption()
	}
	f()
	f()
}`, []string{"main.p:12:4: a pointer to the per-CPU variable ticks is kept in a variable of the enclosing function, which outlives the call, outside the region it was taken in", "E2023"}},
		rej{"a pointer used with preemption only while a handler uses the variable", `
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
	_ = x86.InterruptGate(timer, 0x08, 0)
	x86.Cli()
	rt.DisablePreemption()
	p := &ticks
	x86.Sti()
	*p++
	rt.EnablePreemption()
}`, []string{"main.p:21:2: the access through a pointer to ticks uses the per-CPU variable ticks outside a region with interrupts disabled, which it needs because the interrupt handler timer also uses it", "E2022"}},
		rej{"a compound store through a pointer while a handler uses the variable", `
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
	_ = x86.InterruptGate(timer, 0x08, 0)
	x86.Cli()
	rt.DisablePreemption()
	p := &ticks
	x86.Sti()
	*p = *p + 1
	rt.EnablePreemption()
}`, []string{"main.p:21:7: the access through a pointer to ticks uses the per-CPU variable ticks outside a region with interrupts disabled", "E2022"}},
		rej{"a store through a pointer to a variable larger than a word while a handler uses it", `
import (
	"pluto/arch/x86"
	"pluto/rt"
)

type pair struct{ a, b uint64 }

//pu:percpu
var stats pair

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	x86.Cli()
	stats.a++
	x86.Sti()
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	x86.Cli()
	rt.DisablePreemption()
	p := &stats
	x86.Sti()
	*p = pair{1, 2}
	rt.EnablePreemption()
}`, []string{"main.p:25:2: the access through a pointer to stats uses the per-CPU variable stats outside a region with interrupts disabled", "E2022"}},
		rej{"a callee that opens its own region and uses a pointer from its caller", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func use(p *uint64) {
	rt.DisablePreemption()
	*p = 1
	rt.EnablePreemption()
}

func main() {
	rt.DisablePreemption()
	p := &ticks
	rt.EnablePreemption()
	use(p)
}`, []string{"main.p:9:2: a pointer to the per-CPU variable ticks is used outside the region it was taken in", "E2023"}},
	)
	perCPUAccepted = append(perCPUAccepted,
		acc{"a pointer taken and used in each pass of a goto loop", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	i := 0
again:
	rt.DisablePreemption()
	p := &ticks
	*p += 1
	rt.EnablePreemption()
	i++
	if i < 2 {
		goto again
	}
}`},
		acc{"a goto loop that carries no pointer", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	var total uint64
	i := 0
again:
	rt.DisablePreemption()
	total += ticks
	ticks++
	rt.EnablePreemption()
	i++
	if i < 2 {
		goto again
	}
	println(total)
}`},
		acc{"a recursion that takes its own pointer in each call", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func rec(n int) {
	rt.DisablePreemption()
	p := &ticks
	*p += 1
	rt.EnablePreemption()
	if n > 0 {
		rec(n - 1)
	}
}

func main() { rec(3) }`},
		acc{"a pointer passed down a recursion inside its region", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func walk(p *uint64, n int) {
	*p += 1
	if n > 0 {
		walk(p, n-1)
	}
}

func main() {
	rt.DisablePreemption()
	p := &ticks
	walk(p, 3)
	rt.EnablePreemption()
}`},
		acc{"a callee that opens its own region and uses a pointer from its caller's region", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func use(p *uint64) {
	rt.DisablePreemption()
	*p = 1
	rt.EnablePreemption()
}

func main() {
	rt.DisablePreemption()
	p := &ticks
	use(p)
	rt.EnablePreemption()
}`},
		acc{"a literal that reads a captured pointer inside the region", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	rt.DisablePreemption()
	p := &ticks
	f := func() { *p += 1 }
	f()
	f()
	rt.EnablePreemption()
}`},
		acc{"a literal that takes and uses its own pointer, called twice", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	f := func() {
		rt.DisablePreemption()
		p := &ticks
		*p += 1
		rt.EnablePreemption()
	}
	f()
	f()
}`},
		acc{"a pointer used with interrupts disabled while a handler uses the variable", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks++
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	x86.Cli()
	p := &ticks
	*p++
	x86.Sti()
}`},
		acc{"a pointer used under a later Cli with its preemption region held while a handler uses the variable", `
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
	_ = x86.InterruptGate(timer, 0x08, 0)
	x86.Cli()
	rt.DisablePreemption()
	p := &ticks
	x86.Sti()
	x86.Cli()
	*p++
	x86.Sti()
	rt.EnablePreemption()
}`},
		acc{"a lone store through a pointer with preemption disabled while a handler uses the variable", `
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
	_ = x86.InterruptGate(timer, 0x08, 0)
	x86.Cli()
	rt.DisablePreemption()
	p := &ticks
	x86.Sti()
	*p = 7
	rt.EnablePreemption()
}`},
		acc{"a pointer with preemption disabled when no handler uses the variable", `
import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:percpu
var ticks uint64

//pu:percpu
var other uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	other++
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	rt.DisablePreemption()
	p := &ticks
	*p++
	rt.EnablePreemption()
}`},
	)
}
