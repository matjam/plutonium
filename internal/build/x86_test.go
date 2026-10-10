package build_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestX86Barriers checks that pluto/arch/x86 intrinsics are expanded inline
// and are compiler barriers: a load of a global is repeated after each one
// rather than reused, stores on both sides stay, and an intrinsic whose
// result is unused is still emitted.
func TestX86Barriers(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

import "pluto/arch/x86"

var g int64
var port uint16 = 0x3f8

//pu:noinline
func loads() int64 {
	a := g
	x86.Cli()
	b := g
	x86.Sti()
	c := g
	x86.Outb(0x80, 0)
	d := g
	x86.Outb(port, byte(d))
	e := g
	return a + b + c + d + e
}

//pu:noinline
func stores() {
	g = 1
	x86.Cli()
	g = 2
	x86.Sti()
	g = 3
}

//pu:noinline
func unused() {
	x86.Inb(0x60)
	x86.Rdtsc()
	x86.ReadCR2()
}

//pu:noinline
func spin() {
	for g == 0 {
		x86.Pause()
	}
}

func main() {
	stores()
	unused()
	spin()
	g = loads()
}
`
	listing, err := kernelCompile(t, src, 2)
	if err != nil {
		t.Fatal(err)
	}
	asm := funcListing(t, listing, "main.loads")
	if strings.Contains(asm, "call") {
		t.Errorf("intrinsics not inlined:\n%s", asm)
	}
	// Each load of g comes after the previous barrier.
	pos := 0
	for _, want := range []string{"main.g(%rip)", "\tcli", "main.g(%rip)", "\tsti", "main.g(%rip)",
		"outb %al, $128", "main.g(%rip)", "outb %al, %dx", "main.g(%rip)"} {
		i := strings.Index(asm[pos:], want)
		if i < 0 {
			t.Fatalf("loads: %q missing after offset %d:\n%s", want, pos, asm)
		}
		pos += i + len(want)
	}

	asm = funcListing(t, listing, "main.stores")
	if n := strings.Count(asm, ", main.g(%rip)"); n != 3 {
		t.Errorf("stores: %d stores of g, want 3:\n%s", n, asm)
	}
	asm = funcListing(t, listing, "main.unused")
	for _, want := range []string{"inb $96, %al", "rdtsc", "mov %cr2"} {
		if !strings.Contains(asm, want) {
			t.Errorf("unused: %q removed:\n%s", want, asm)
		}
	}
	asm = funcListing(t, listing, "main.spin")
	if i := strings.Index(asm, "pause"); i < 0 || !strings.Contains(asm[i:], "main.g(%rip)") {
		t.Errorf("spin: g must be reloaded after pause:\n%s", asm)
	}
}

// TestX86All compiles a call to every intrinsic and checks the listing
// holds its instruction.
func TestX86All(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

import "pluto/arch/x86"

var sink uint64
var gdt [4]uint64
var dtp x86.DescriptorTablePointer
var fpu x86.FPUState

//pu:noinline
func all(port uint16, msr uint32, addr uintptr) {
	x86.Cli()
	x86.Sti()
	x86.StiHlt()
	x86.Hlt()
	x86.Pause()
	f := x86.SaveFlagsCli()
	x86.RestoreFlags(f)
	sink += uint64(x86.Inb(port)) + uint64(x86.Inw(port)) + uint64(x86.Inl(port))
	x86.Outb(port, 1)
	x86.Outw(port, 2)
	x86.Outl(port, 3)
	x86.Outl(0x21, 3)
	sink += x86.Rdmsr(msr)
	x86.Wrmsr(msr, sink)
	sink += x86.ReadCR0() + x86.ReadCR2() + x86.ReadCR3() + x86.ReadCR4()
	x86.WriteCR0(sink)
	x86.WriteCR3(sink)
	x86.WriteCR4(sink)
	x86.Invlpg(addr)
	dtp = x86.NewDescriptorTablePointer(uintptr(addr), len(gdt)*8)
	x86.Lgdt(&dtp)
	x86.Lidt(&dtp)
	x86.Ltr(0x28)
	a, b, c, d := x86.Cpuid(msr, 0)
	sink += uint64(a + b + c + d)
	sink += x86.Rdtsc()
	tsc, aux := x86.Rdtscp()
	sink += tsc + uint64(aux)
	x86.Swapgs()
	sink += uint64(x86.ReadGSBase())
	x86.WriteGSBase(addr)
	sink += x86.GSLoad64(8) + uint64(x86.GSLoad32(uintptr(msr)))
	x86.GSStore64(16, sink)
	x86.GSStore32(addr, 7)
	x86.FPUBegin(&fpu)
	x86.FPUEnd(&fpu)
}

func main() { all(0x3f8, 0x1b, 0x1000) }
`
	listing, err := kernelCompile(t, src, 2)
	if err != nil {
		t.Fatal(err)
	}
	asm := funcListing(t, listing, "main.all")
	for _, want := range []string{
		"\tcli", "\tsti", "sti; hlt", "\thlt", "pause", "pushfq; pop %r", "; popfq",
		"inb %dx, %al", "inw %dx, %ax", "inl %dx, %eax", "outb %al, %dx", "outw %ax, %dx", "outl %eax, %dx",
		"outl %eax, $33", "rdmsr", "wrmsr", "mov %cr0,", "mov %cr2,", "mov %cr3,", "mov %cr4,",
		", %cr0", ", %cr3", ", %cr4", "invlpg", "lgdt", "lidt", "ltr", "cpuid", "rdtsc", "rdtscp",
		"swapgs", "%gs:8", "%gs:16", "%gs:(", "clts", "fxsave64", "fxrstor64", "$3221225729, %ecx",
	} {
		if !strings.Contains(asm, want) {
			t.Errorf("listing lacks %q", want)
		}
	}
	if strings.Contains(asm, "call") {
		t.Errorf("an intrinsic was called rather than expanded:\n%s", asm)
	}
	if t.Failed() {
		t.Log(asm)
	}
}

// TestX86Hosted runs the intrinsics that work in user space, inline and
// through func values (the assembly bodies), and checks they agree.
func TestX86Hosted(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

import "pluto/arch/x86"

func main() {
	x86.Pause()
	a, b, c, d := x86.Cpuid(0, 0)
	cpuid := x86.Cpuid
	a2, b2, c2, d2 := cpuid(0, 0)
	println("cpuid", a == a2, b == b2, c == c2, d == d2, a > 0)
	vendor := [12]byte{
		byte(b), byte(b >> 8), byte(b >> 16), byte(b >> 24),
		byte(d), byte(d >> 8), byte(d >> 16), byte(d >> 24),
		byte(c), byte(c >> 8), byte(c >> 16), byte(c >> 24),
	}
	ok := true
	for _, ch := range vendor {
		ok = ok && ch >= ' ' && ch < 127
	}
	println("vendor printable", ok)
	t1 := x86.Rdtsc()
	rdtsc := x86.Rdtsc
	t2 := rdtsc()
	t3 := x86.Rdtsc()
	println("rdtsc", t2 >= t1, t3 >= t2)
	pause := x86.Pause
	pause()
}
`
	got, status := compileAndRun(t, src, 2)
	const want = "cpuid true true true true true\nvendor printable true\nrdtsc true true\n"
	if status != 0 || got != want {
		t.Errorf("status %d, output:\n%s\nwant:\n%s", status, got, want)
	}
}
