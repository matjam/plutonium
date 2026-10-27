// Ported from internal/build/kernelmode_test.go at the go-cutover tag.

package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestKernelFloatErrors checks that each kind of floating-point use in a
// kernel-mode function without //pu:fpu is a compile error naming it.
func TestKernelFloatErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, decls, want string
	}{
		{"arithmetic", "func f(p *V) { p.X = p.X + p.Y }", "main.p:5:22: float64 arithmetic is not allowed in kernel mode"},
		{"constant", "func f(p *V) { p.X = 1.5 }", "float64 constant is not allowed"},
		{"intconstant", "func f(p *W) { p.X = 2 }", "float32 constant is not allowed"},
		{"comparison", "func f(p *V) bool { return p.X < p.Y }", "float64 comparison is not allowed"},
		{"equality", "func f(a, b *V) bool { return *a == *b }", "float64 comparison is not allowed"},
		{"tointeger", "func f(p *V) int { return int(p.X) }", "float64 conversion is not allowed"},
		{"fromint", "func f(p *V, n int) { p.X = float64(n) }", "float64 conversion is not allowed"},
		{"narrow", "func f(p *V, q *W) { q.X = float32(p.X) }", "float32 conversion is not allowed"},
		{"negate", "func f(p *V) { p.X = -p.Y }", "float64 arithmetic is not allowed"},
		{"parameter", "func f(x float64) {}", "main.p:5:1: float64 parameter is not allowed"},
		{"result", "func f(p *W) float32 { return p.X }", "float32 result is not allowed"},
		{"struct parameter", "func f(v V) {}", "float64 parameter is not allowed"},
		{"interface", "func f(p *V) any { return p.X }", "float64 conversion to an interface is not allowed"},
		{"println", "func f(p *V) { println(p.X) }", "float64 argument is not allowed"},
		{"sqrt", "func f(p *V) { p.X = math.Sqrt(p.Y) }", "float64 arithmetic is not allowed"},
		{"closure", "func f(p *V) { g := func() { p.X *= p.Y }; g() }", "float64 arithmetic is not allowed"},
		{"fpu call", "//pu:fpu\nfunc h(x float64) float64 { return x * 2 }\n\nfunc f(p *V) { p.X = h(p.Y) }", "float64 argument is not allowed"},
		{"generic", "func add[T int | float64](a, b T) T { return a + b }\n\nfunc f(p *V) { p.X = add(p.X, p.Y) }", "float64 parameter is not allowed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "package main\n\nimport \"pluto/math\"\n\n" + tc.decls +
				"\n\ntype V struct{ X, Y float64 }\n\ntype W struct{ X float32 }\n\nvar _ = math.Pi\n\nfunc main() { f(nil) }\n"
			src = strings.Replace(src, "func main() { f(nil) }", mainFor(tc.decls), 1)
			_, errs := kernelCompile(t, src, 2)
			if errs == "" {
				t.Fatalf("compiled; want error %q", tc.want)
			}
			if !strings.Contains(errs, tc.want) {
				t.Errorf("error:\n%v\nwant %q", errs, tc.want)
			}
			if strings.Count(errs, "kernel mode") > 2 {
				t.Errorf("too many errors:\n%v", errs)
			}
		})
	}
}

// mainFor returns a main function that calls f with zero arguments of the
// right shape.
func mainFor(decls string) string {
	switch {
	case strings.Contains(decls, "func f(a, b *V)"), strings.Contains(decls, "func f(p *V, q *W)"):
		return "func main() { f(nil, nil) }"
	case strings.Contains(decls, "func f(p *V, n int)"):
		return "func main() { f(nil, 0) }"
	case strings.Contains(decls, "func f(x float64)"), strings.Contains(decls, "func f(v V)"):
		return "func main() { var v V; _ = v }\n\nvar _ = f"
	}
	return "func main() { f(nil) }"
}

// TestKernelArenaNeedsNoScheduler checks that a kernel that allocates from
// the arenas of pluto/arena binds no scheduler (language spec section
// 19.5): the arenas use no waiting, and the end of main after rt.ExitTask
// (which their drop paths may request) waits for no other task in a program
// that has none. A program that does start tasks still needs one (E2593).
func TestKernelArenaNeedsNoScheduler(t *testing.T) {
	t.Parallel()
	const arenas = `package main

import (
	"pluto/arch/x86"
	"pluto/arena"
	"pluto/vec"
)

var heap [1 << 16]byte
var fixed [1 << 12]byte

func main() {
	a := arena.NewUnsafe(heap[:])
	v := vec.New[int](&a)
	v.Push(1)
	f := arena.NewFixed(1 << 12)
	w := vec.New[int](&f)
	w.Push(2)
	if v.Len() == w.Len() {
		x86.Hlt()
	}
}
`
	for _, level := range []int{0, 2} {
		if _, errs := kernelCompile(t, arenas, level); errs != "" {
			t.Errorf("-O%d: %v", level, errs)
		}
	}
	const tasks = `package main

func work() {}

func main() { task work() }
`
	if _, errs := kernelCompile(t, tasks, 0); !strings.Contains(errs, "needs a scheduler") {
		t.Errorf("a kernel that starts a task builds without a scheduler: %v", errs)
	}
}

// TestKernelFloatsAllowed checks what kernel mode still allows: float
// types, copying float values as bits, zero values, and floating point in
// //pu:fpu functions and the closures inside them.
func TestKernelFloatsAllowed(t *testing.T) {
	t.Parallel()
	const src = `package main

type V struct{ X, Y float64 }

var vs [8]V
var w V

func copyAll() {
	for i := 1; i < len(vs); i++ {
		vs[i] = vs[i-1]
	}
	var z V
	vs[0] = z
	f := vs[1].X
	vs[2].Y = f
	w = vs[3]
}

//pu:fpu
//pu:noinline
func scale(p *V, k int) {
	g := func() { p.Y = p.X * 0.5 }
	p.X *= float64(k)
	g()
}

func main() {
	copyAll()
	scale(&w, 3)
}
`
	listing, errs := kernelCompile(t, src, 2)
	if errs != "" {
		t.Fatal(errs)
	}
	if asm := funcListing(t, listing, "main.copyAll"); strings.Contains(asm, "xmm") {
		t.Errorf("copyAll uses XMM registers:\n%s", asm)
	}
	if asm := funcListing(t, listing, "main.scale"); !strings.Contains(asm, "xmm") {
		t.Errorf("scale (//pu:fpu) should use SSE:\n%s", asm)
	}
	if asm := funcListing(t, listing, "main.main"); strings.Contains(asm, "xmm") || !strings.Contains(asm, "call main.scale") {
		t.Errorf("main must call scale out of line without SSE:\n%s", asm)
	}
}

// TestKernelNoInlineFPU checks that the inliner keeps a //pu:fpu function
// out of a caller without FPU state, but inlines it into another
// //pu:fpu function.
func TestKernelNoInlineFPU(t *testing.T) {
	t.Parallel()
	const src = `package main

type V struct{ X, Y float64 }

var w V

//pu:fpu
func half(p *V) { p.X = p.Y * 0.5 }

//pu:fpu
//pu:noinline
func both(p *V) {
	half(p)
	half(p)
}

//pu:noinline
func plain(p *V) { half(p) }

func main() {
	plain(&w)
	both(&w)
}
`
	listing, errs := kernelCompile(t, src, 2)
	if errs != "" {
		t.Fatal(errs)
	}
	if asm := funcListing(t, listing, "main.plain"); !strings.Contains(asm, "call main.half") || strings.Contains(asm, "xmm") {
		t.Errorf("plain must call half, not inline it:\n%s", asm)
	}
	if asm := funcListing(t, listing, "main.both"); strings.Contains(asm, "call main.half") {
		t.Errorf("both (//pu:fpu) should inline half:\n%s", asm)
	}
}

// kernelProgram is a representative kernel-style program: structs with
// float fields copied around, large copies and clears, interfaces,
// generics, closures, defer, strings, slices, packed and volatile types.
const kernelProgram = `package main

import "unsafe"

// Five leaves: passed and returned through memory, so it may cross calls.
type Vec struct{ X, Y, Z, W, T float64 }

type Task struct {
	id    int
	name  string
	pos   Vec
	vel   Vec
	stack [256]byte
	next  *Task
}

type Ctrl packed uint32 {
	enable bits[bool, 1]
	mode   bits[uint, 3]
}

type Device interface {
	Read(p []byte) int
}

type Serial struct {
	port uint16
	reg  *volatile[uint32]
}

func (s *Serial) Read(p []byte) int {
	n := 0
	for i := range p {
		p[i] = byte(*s.reg)
		n++
	}
	return n
}

type Queue[T any] struct {
	items [16]T
	n     int
}

func (q *Queue[T]) Push(x T) bool {
	if q.n == len(q.items) {
		return false
	}
	q.items[q.n] = x
	q.n++
	return true
}

func (q *Queue[T]) Pop() (T, bool) {
	var zero T
	if q.n == 0 {
		return zero, false
	}
	q.n--
	return q.items[q.n], true
}

var tasks [8]Task
var runq Queue[*Task]
var vecs Queue[Vec]
var ctrl Ctrl
var reg volatile[uint32]
var log [64]byte

func spawn(i int, name string) *Task {
	t := &tasks[i]
	*t = Task{id: i, name: name}
	t.pos = tasks[0].pos
	runq.Push(t)
	vecs.Push(t.vel)
	return t
}

func schedule() *Task {
	t, ok := runq.Pop()
	if !ok {
		return nil
	}
	v, _ := vecs.Pop()
	t.vel = v
	return t
}

func readAll(d Device) int {
	var buf [32]byte
	return d.Read(buf[:])
}

func logf(msg string) {
	n := copy(log[:], msg)
	defer func() { log[n] = 0 }()
	for i := 0; i < n; i++ {
		if log[i] == ' ' {
			log[i] = '_'
		}
	}
}

func main() {
	spawn(0, "idle")
	spawn(1, "init")
	t := schedule()
	if t != nil {
		logf(t.name)
	}
	ctrl.enable = true
	ctrl.mode = 5
	s := &Serial{port: 0x3f8, reg: &reg}
	_ = readAll(s)
	big := tasks[1]
	tasks[2] = big
	var clear Task
	tasks[3] = clear
	_ = unsafe.Sizeof(big)
}
`

// TestKernelNoXMM compiles a representative kernel-style program and checks
// that no function in it, including the runtime it pulls in, uses an XMM
// register or the float constant pool, in the listing and in the object
// code.
func TestKernelNoXMM(t *testing.T) {
	t.Parallel()
	for _, level := range []int{0, 2} {
		dir := writeTree(t, map[string]string{"main.p": kernelProgram, "panic_handler.p": panicHandlerSource})
		asm := filepath.Join(t.TempDir(), "listing.s")
		r := tryBuildDir(t, dir, optFlag(level), "--mode", "kernel", "-c", "-S", asm)
		if r.status != 0 {
			t.Fatalf("-O%d: exit status %d\n%s", level, r.status, r.out)
		}
		obj := r.exe
		data, err := os.ReadFile(asm)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, bad := range []string{"xmm", "pluto.const", "movs4", "movs8", "cvt", "ucomis"} {
			if strings.Contains(text, bad) {
				t.Errorf("-O%d: listing contains %q:\n%s", level, bad, text)
			}
		}
		if _, err := exec.LookPath("objdump"); err == nil {
			out, err := exec.Command("objdump", "-d", obj).CombinedOutput()
			if err != nil {
				t.Fatalf("objdump: %v\n%s", err, out)
			}
			if strings.Contains(string(out), "xmm") {
				t.Errorf("-O%d: object code uses XMM registers", level)
			}
		}
	}
}

// TestKernelSmoke boots testdata/kernel/smoke, built with --mode kernel
// and the default linker script, under QEMU. The kernel runs in the top 2
// GiB with SSE disabled, prints over the serial port with x86.Outb, sets
// up per-CPU blocks from the .percpu image and switches GS between them,
// then enables SSE for one //pu:fpu call between FPUBegin and FPUEnd.
func TestKernelSmoke(t *testing.T) {
	t.Parallel()
	qemu := needQEMU(t)
	dir := filepath.Join("..", "testdata", "kernel", "smoke")
	const want = "hello from kernel mode\n" +
		"fmt -42 works 0xff true\n" +
		"fmt {1 -2} {3 4} [{5 6}]\n" +
		"cpu0 initial -1 7 9\n" +
		"cpu1 initial -1 0 true\n" +
		"cpu0 again 0 11 1 true\n" +
		"cpuid true sse off true\n" +
		"fmt float %!v(float64=0x4020000000000000)\n" +
		"fpu result bits 4620693217682128896\n" + // 1.5*4 + 2 = 8.0
		"println float float64=0x4020000000000000 float32=0x3f000000\n"
	for _, level := range []int{0, 2} {
		t.Run("O"+string(rune('0'+level)), func(t *testing.T) {
			t.Parallel()
			// The ELF image links with the per-CPU check; the flat binary
			// is what QEMU's multiboot loader boots.
			buildProgram(t, dir, optFlag(level), "--mode", "kernel")
			image := buildProgram(t, dir, optFlag(level), "--mode", "kernel", "--format", "binary")
			if got := bootKernel(t, qemu, image); got != want {
				t.Errorf("serial output differs\n--- got ---\n%s--- want ---\n%s", got, want)
			}
		})
	}
}

// TestKernelInterruptXMM checks which XMM registers interrupt handlers
// save: none for a kernel-mode handler without //pu:fpu, even when it
// makes calls; the ones it uses for a //pu:fpu handler; and, outside
// kernel mode, all of them when it makes a call.
func TestKernelInterruptXMM(t *testing.T) {
	t.Parallel()
	const src = `package main

import (
	"pluto/arch/x86"
	"pluto/sync/atomic"
)

// A handler is a party in the data race rules, so what it shares is atomic.
var count atomic.Int64
var idt [2]x86.Gate

//pu:noinline
func work() { count.Add(2) }

//pu:interrupt
func tick(f *x86.InterruptFrame) {
	count.Add(1)
	work()
}

//pu:fpu
//pu:interrupt
func ftick(f *x86.InterruptFrame) {
	f.RIP = uint64(float64(f.RIP) * 1.5)
}

func main() {
	idt[0] = x86.InterruptGate(tick, 0x08, 0)
	idt[1] = x86.InterruptGate(ftick, 0x08, 0)
}
`
	listing, errs := kernelCompile(t, src, 2)
	if errs != "" {
		t.Fatal(errs)
	}
	if asm := funcListing(t, listing, "main.tick"); !strings.Contains(asm, "call main.work") ||
		strings.Contains(asm, "movups") || strings.Contains(asm, "xmm") {
		t.Errorf("kernel-mode handler saves vector registers:\n%s", asm)
	}
	if asm := funcListing(t, listing, "main.ftick"); !strings.Contains(asm, "movups") ||
		strings.Count(asm, "movups") > 6 {
		t.Errorf("//pu:fpu handler should save just the XMM registers it uses:\n%s", asm)
	}

	// Outside kernel mode a handler that calls saves all sixteen.
	text, errs := modeCompile(t, src, 2, "freestanding", true)
	if errs != "" {
		t.Fatal(errs)
	}
	if asm := funcListing(t, text, "main.tick"); strings.Count(asm, "movups") != 32 {
		t.Errorf("freestanding handler that calls should save XMM0-15:\n%s", asm)
	}
}
