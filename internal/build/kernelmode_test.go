package build_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// panicHandlerSource is the panic handler that freestanding and kernel
// test programs, which must have one, are built with.
const panicHandlerSource = `package main

import "pluto/rt"

//pu:panic
func testPanicHandler(p *rt.Panic) {
	for {
	}
}
`

// writePanicHandler adds panicHandlerSource to the main package in dir.
func writePanicHandler(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "panic_handler.p"), []byte(panicHandlerSource), 0o644); err != nil {
		t.Fatal(err)
	}
}

// kernelCompile compiles src as a kernel-mode main package, with a panic
// handler, to an object file and returns the machine code listing.
func kernelCompile(t *testing.T, src string, optLevel int) (string, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	writePanicHandler(t, dir)
	var listing bytes.Buffer
	cfg := &build.Config{Dir: dir, Output: filepath.Join(dir, "k.o"), Stdlib: plutonium.Stdlib(),
		OptLevel: optLevel, Kernel: true, Listing: &listing, CompileOnly: true}
	err := build.Build(cfg)
	return listing.String(), err
}

// funcListing returns the listing of function name from a whole listing.
func funcListing(t *testing.T, text, name string) string {
	t.Helper()
	i := strings.Index(text, "\n"+name+":\n")
	if i < 0 && strings.HasPrefix(text, name+":\n") {
		i = -1
	} else if i < 0 {
		t.Fatalf("%s not in listing:\n%s", name, text)
	}
	text = text[i+1:]
	if j := strings.Index(text, "\n\n"); j >= 0 {
		text = text[:j]
	}
	return text
}

// TestKernelFloatErrors checks that each kind of floating-point use in a
// kernel-mode function without //pu:fpu is a compile error naming it.
func TestKernelFloatErrors(t *testing.T) {
	testguard.Parallel(t)
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
			testguard.Parallel(t)
			src := "package main\n\nimport \"pluto/math\"\n\n" + tc.decls +
				"\n\ntype V struct{ X, Y float64 }\n\ntype W struct{ X float32 }\n\nvar _ = math.Pi\n\nfunc main() { f(nil) }\n"
			src = strings.Replace(src, "func main() { f(nil) }", mainFor(tc.decls), 1)
			_, err := kernelCompile(t, src, 2)
			if err == nil {
				t.Fatalf("compiled; want error %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error:\n%v\nwant %q", err, tc.want)
			}
			if strings.Count(err.Error(), "kernel mode") > 2 {
				t.Errorf("too many errors:\n%v", err)
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

// TestKernelFloatsAllowed checks what kernel mode still allows: float
// types, copying float values as bits, zero values, and floating point in
// //pu:fpu functions and the closures inside them.
func TestKernelFloatsAllowed(t *testing.T) {
	testguard.Parallel(t)
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
	listing, err := kernelCompile(t, src, 2)
	if err != nil {
		t.Fatal(err)
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
	testguard.Parallel(t)
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
	listing, err := kernelCompile(t, src, 2)
	if err != nil {
		t.Fatal(err)
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
	testguard.Parallel(t)
	for _, level := range []int{0, 2} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(kernelProgram), 0o644); err != nil {
			t.Fatal(err)
		}
		writePanicHandler(t, dir)
		var listing bytes.Buffer
		obj := filepath.Join(dir, "k.o")
		cfg := &build.Config{Dir: dir, Output: obj, Stdlib: plutonium.Stdlib(),
			OptLevel: level, Kernel: true, Listing: &listing, CompileOnly: true}
		if err := build.Build(cfg); err != nil {
			t.Fatalf("-O%d: %v", level, err)
		}
		text := listing.String()
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
	testguard.Parallel(t)
	qemu, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Skip("qemu-system-x86_64 is not installed")
	}
	dir := filepath.Join("..", "..", "testdata", "kernel", "smoke")
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
			testguard.Parallel(t)
			// The ELF image links with the per-CPU check; the flat binary
			// is what QEMU's multiboot loader boots.
			elfOut := filepath.Join(t.TempDir(), "kernel.elf")
			cfg := &build.Config{Dir: dir, Output: elfOut, Stdlib: plutonium.Stdlib(), OptLevel: level, Kernel: true}
			if err := build.Build(cfg); err != nil {
				t.Fatalf("build ELF: %v", err)
			}
			image := filepath.Join(t.TempDir(), "kernel.bin")
			cfg.Output, cfg.Format = image, "binary"
			if err := build.Build(cfg); err != nil {
				t.Fatalf("build: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, qemu, "-kernel", image, "-serial", "stdio", "-display", "none",
				"-monitor", "none", "-no-reboot", "-m", "64M",
				"-device", "isa-debug-exit,iobase=0xf4,iosize=0x04")
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("qemu: %v (want exit status 1)\nserial: %s\nstderr: %s", err, stdout.String(), stderr.String())
			}
			if got := stdout.String(); got != want {
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
	testguard.Parallel(t)
	const src = `package main

import "pluto/arch/x86"

type V struct{ X, Y float64 }

var count int
var v V
var idt [2]uintptr

//pu:noinline
func work() { count += 2 }

//pu:interrupt
func tick(f *x86.InterruptFrame) {
	count++
	work()
}

//pu:fpu
//pu:interrupt
func ftick(f *x86.InterruptFrame) {
	v.X = v.X * v.Y
}

func main() {
	idt[0] = x86.HandlerAddr(tick)
	idt[1] = x86.HandlerAddr(ftick)
}
`
	listing, err := kernelCompile(t, src, 2)
	if err != nil {
		t.Fatal(err)
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
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	writePanicHandler(t, dir)
	var buf bytes.Buffer
	cfg := &build.Config{Dir: dir, Output: filepath.Join(dir, "k.o"), Stdlib: plutonium.Stdlib(),
		OptLevel: 2, Freestanding: true, Listing: &buf, CompileOnly: true}
	if err := build.Build(cfg); err != nil {
		t.Fatal(err)
	}
	if asm := funcListing(t, buf.String(), "main.tick"); strings.Count(asm, "movups") != 32 {
		t.Errorf("freestanding handler that calls should save XMM0-15:\n%s", asm)
	}
}
