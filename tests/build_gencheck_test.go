// Ported from internal/build/gencheck_test.go at the go-cutover tag.

package tests

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// runGenCheck builds src with generation checks at the given optimisation
// level and returns its output and exit status.
func runGenCheck(t *testing.T, src string, optLevel int) (string, int) {
	t.Helper()
	return runGenMode(t, src, optLevel, "--gencheck")
}

// runUnchecked is runGenCheck for the unchecked mode.
func runUnchecked(t *testing.T, src string, optLevel int) (string, int) {
	t.Helper()
	return runGenMode(t, src, optLevel, "--gencheck=false")
}

// runGenMode builds src with the generation check flag and runs it. The
// output is the standard output and standard error as they were interleaved.
func runGenMode(t *testing.T, src string, optLevel int, flag string) (string, int) {
	t.Helper()
	exe := mustBuild(t, single(src), optFlag(optLevel), flag)
	return runExe(t, exe, false)
}

// TestGenCheck checks that, built with -gencheck, a use of freed memory and
// a double free trap, while correct code runs as before.
func TestGenCheck(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, body, want string
		status           int
	}{
		{"use after free", `
	a := alloc.New[Node]()
	a.val = 7
	b := a
	alloc.Free(a)
	c := alloc.New[Node]() // reuses the slot
	c.val = 9
	println("stale", b.val)`, "use of freed memory", 2},
		{"interior pointer after free", `
	a := alloc.New[Node]()
	p := &a.val
	alloc.Free(a)
	_ = alloc.New[Node]()
	*p = 1`, "use of freed memory", 2},
		{"double free", `
	a := alloc.New[Node]()
	alloc.Free(a)
	alloc.Free(a)`, "already freed", 2},
		{"correct code", `
	var head *Node
	for i := 0; i < 1000; i++ {
		n := alloc.New[Node]()
		n.val = i
		n.next = head
		head = n
	}
	sum := 0
	for n := head; n != nil; {
		sum += n.val
		next := n.next
		alloc.Free(n)
		n = next
	}
	println("sum", sum)`, "sum 499500", 0},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				src := "package main\n\nimport \"pluto/alloc\"\n\ntype Node struct {\n\tnext *Node\n\tval  int\n}\n\nfunc main() {" + c.body + "\n}\n"
				out, status := runGenCheck(t, src, opt)
				if status != c.status || !strings.Contains(out, c.want) {
					t.Fatalf("-O%d: status %d, output %q; want status %d and %q", opt, status, out, c.status, c.want)
				}
			})
		}
	}
}

// TestGenCheckNonHosted checks that, until generation checks support
// freestanding and kernel builds (roadmap item 101, phase 2), such a build
// must pass --gencheck=false: leaving the default or asking for the checks
// is an error that says how to build unchecked on purpose (spec section
// 18.11).
func TestGenCheckNonHosted(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		flags []string
		want  string
	}{
		{"kernel default", []string{"--mode", "kernel"}, "kernel builds must pass --gencheck=false"},
		{"freestanding default", []string{"--mode", "freestanding"}, "freestanding builds must pass --gencheck=false"},
		{"none target default", []string{"--target", "x86_64-none"}, "freestanding builds must pass --gencheck=false"},
		{"kernel checked", []string{"--mode", "kernel", "--gencheck"}, "generation checks are not supported in kernel builds yet; pass --gencheck=false"},
		{"freestanding checked", []string{"--mode", "freestanding", "--gencheck=true"}, "generation checks are not supported in freestanding builds yet; pass --gencheck=false"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := writeProgram(t, "package main\n\nfunc main() {}\n")
			args := append([]string{"build"}, c.flags...)
			args = append(args, "-c", "-o", filepath.Join(t.TempDir(), "prog.o"), dir)
			out, status := runPluto(t, ".", nil, args...)
			if status != 2 || !strings.Contains(out, c.want) || !strings.Contains(out, "spec section 18.11, roadmap item 101") {
				t.Fatalf("status %d, output %q; want status 2 and %q naming section 18.11 and item 101", status, out, c.want)
			}
		})
	}
	// The environment's default does not exempt them either.
	dir := writeProgram(t, "package main\n\nfunc main() {}\n")
	out, status := runPluto(t, ".", []string{"PLUTO_GENCHECK=0"}, "build", "--mode", "kernel", "-c", "-o", filepath.Join(t.TempDir(), "prog.o"), dir)
	if status != 2 || !strings.Contains(out, "kernel builds must pass --gencheck=false") {
		t.Fatalf("PLUTO_GENCHECK=0: status %d, output %q; want the kernel build rejected", status, out)
	}
}

// TestGenCheckKernelUnchecked checks that a kernel build that passes
// --gencheck=false builds as before: unchecked, with no generation check
// in its code.
func TestGenCheckKernelUnchecked(t *testing.T) {
	t.Parallel()
	for _, level := range []int{0, 2} {
		dir := writeTree(t, map[string]string{"main.p": kernelProgram, "panic_handler.p": panicHandlerSource})
		asm := filepath.Join(t.TempDir(), "listing.s")
		r := tryBuildDir(t, dir, optFlag(level), "--mode", "kernel", "--gencheck=false", "-c", "-S", asm)
		if r.status != 0 {
			t.Fatalf("-O%d: exit status %d\n%s", level, r.status, r.out)
		}
		data, err := os.ReadFile(asm)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"GenRegisteredLive", "GenRegisteredRangeLive", "panicStale"} {
			if strings.Contains(string(data), bad) {
				t.Errorf("-O%d: unchecked kernel listing calls %s", level, bad)
			}
		}
	}
}

// TestGenCheckMissedUntag checks the canonical XOR encoding's safety net
// (abi.md section 1.3): a tagged heap pointer used as an address without
// its tag removed is not canonical, so the access faults instead of
// reading memory. Code outside Plutonium sees the raw bits here because
// they travel as a uintptr made from the pointer's representation, not by
// a conversion, which would remove the tag.
func TestGenCheckMissedUntag(t *testing.T) {
	t.Parallel()
	const src = `package main

import (
	"pluto/alloc"
	"unsafe"
)

//pu:extern i101_load
func load(p uintptr) int

func main() {
	p := alloc.New[int]()
	*p = 42
	bits := *(*uintptr)(unsafe.Pointer(&p))
	a := uintptr(unsafe.Pointer(p))
	if bits == a {
		println("untagged")
		return
	}
	println("address", load(a))
	println("tagged", load(bits))
	println("not reached")
}
`
	const stub = `# Loads the word at the address in %rdi, as code outside Plutonium would.
	.text
	.globl	i101_load
i101_load:
	movq	(%rdi), %rax
	ret
	.section .note.GNU-stack,"",@progbits
`
	for _, opt := range []int{0, 2} {
		exe := mustBuild(t, map[string]string{"main.p": src, "load_amd64.s": stub}, optFlag(opt), "--gencheck")
		out, status := runExe(t, exe, false)
		if status != 2 || !strings.Contains(out, "address 42") || !strings.Contains(out, "runtime error:") ||
			strings.Contains(out, "tagged") || strings.Contains(out, "not reached") {
			t.Fatalf("-O%d: status %d, output %q; want the untagged load to read 42 and the tagged one to fault", opt, status, out)
		}
	}
}

// TestGenCheckHighHalfUntag checks that removing a tag is sign extension
// from bit 47 (abi.md section 1.3): converting a higher-half pointer to
// uintptr keeps its address, untagged or with a tag XORed into bits
// 63..48, where zero extension would have cleared its high bits.
func TestGenCheckHighHalfUntag(t *testing.T) {
	t.Parallel()
	const src = `package main

import "unsafe"

//pu:noinline
func addr(p unsafe.Pointer) uintptr { return uintptr(p) }

func main() {
	x := uintptr(0xffff800000001230)
	println("plain", addr(unsafe.Pointer(x)) == x)
	for _, tag := range []uintptr{1, 0x7fff, 0x8000, 0xffff} {
		bits := x ^ tag<<48
		p := *(*unsafe.Pointer)(unsafe.Pointer(&bits))
		println("tag", tag, addr(p) == x)
	}
	y := uintptr(0x00007f0000001230)
	bits := y ^ 0x1234<<48
	println("low", addr(*(*unsafe.Pointer)(unsafe.Pointer(&bits))) == y)
}
`
	const want = "plain true\ntag 1 true\ntag 32767 true\ntag 32768 true\ntag 65535 true\nlow true\n"
	for _, opt := range []int{0, 2} {
		out, status := runGenCheck(t, src, opt)
		if status != 0 || out != want {
			t.Fatalf("-O%d: status %d, output %q; want %q", opt, status, out, want)
		}
	}
}

// TestArenaEpochs checks the arena epochs of a -gencheck build (memory-safety
// design D4): a view, box, string or error in an arena.Chunked that was
// reset, released past it or freed panics with where that happened, also
// when a longer-lived heap container holds it, and correct code runs as
// before.
func TestArenaEpochs(t *testing.T) {
	t.Parallel()
	const prelude = `package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
	"pluto/strconv"
	"pluto/vec"
	"unsafe"
)

type node struct {
	next *node
	val  int
}

func label(a *arena.Chunked, n int) string {
	defer alloc.Use(a).Restore()
	return "item " + strconv.Itoa(n)
}

func box(a *arena.Chunked, n int) shared[node] {
	defer alloc.Use(a).Restore()
	b := alloc.Shared[node]()
	b.val = n
	return b
}

func fail(a *arena.Chunked, n int) error {
	defer alloc.Use(a).Restore()
	return fmt.Errorf("parse failed at %d", n)
}

// heapNames uses the imports a case may leave unused.
func heapNames() vec.Vec[string] {
	_ = unsafe.Pointer(nil)
	return vec.New[string]()
}

func main() {
	a := arena.NewChunked()
`
	const reset = "use of memory from an arena that was reset at main.main (./main.p:"
	cases := []struct {
		name, body, want string
		status           int
	}{
		// Through pa the move checker does not see which arena is reset,
		// so the misuse reaches the run time check. TestArenaStaleViews
		// has the same programs on a itself, rejected at compile time.
		{"view after reset", `
	pa := &a
	p := a.New[node]()
	p.val = 7
	pa.Reset() // here
	println(p.val)`, reset, 2},
		{"view after release", `
	pa := &a
	m := a.Mark()
	p := a.New[node]()
	p.val = 7
	pa.Release(m) // here
	q := a.New[node]() // reuses p's memory
	q.val = 8
	println(p.val)`, reset, 2},
		{"slice in a chunk of its own", `
	pa := &a
	m := a.Mark()
	s := a.Slice[int](3000)
	pa.Release(m) // here
	_ = a.Slice[int](3000)
	s[2999] = 1`, reset, 2},
		{"string in a heap container", `
	names := vec.New[string](alloc.Default())
	names.Push(label(&a, 4))
	a.Reset() // here
	println(names.At(0))`, reset, 2},
		{"string dropped after reset", `
	names := vec.New[string](alloc.Default())
	names.Push(label(&a, 4))
	a.Reset() // here
	names.Drop()`, reset, 2},
		{"box in a heap container", `
	boxes := vec.New[shared[node]](alloc.Default())
	boxes.Push(box(&a, 5))
	a.Reset() // here
	println(boxes.At(0).val)`, reset, 2},
		{"box dropped after reset", `
	boxes := vec.New[shared[node]](alloc.Default())
	boxes.Push(box(&a, 5))
	a.Reset() // here
	boxes.Drop()`, reset, 2},
		{"error in a heap container", `
	errs := vec.New[error](alloc.Default())
	errs.Push(fail(&a, 3))
	a.Reset() // here
	println(errs.At(0).Error())`, reset, 2},
		{"free of the latest allocation", `
	p := (*node)(a.Alloc(16, 8))
	a.Free(unsafe.Pointer(p), 16, 8) // here
	p.val = 1`, "use of freed memory: an arena allocation freed at main.main (./main.p:", 2},
		{"string dropped after release", `
	for i := 0; i < 2; i++ {
		m := a.Mark()
		s := label(&a, i)
		println(s)
		a.Release(m) // here
	} // s is dropped here, after its memory went back to the arena`, reset, 2},
		{"correct code", `
	keep := a.New[node]()
	keep.val = 1
	for i := 0; i < 1000; i++ {
		m := a.Mark()
		{
			var head *node
			for j := 0; j < 100; j++ {
				n := a.New[node]()
				n.val = j
				n.next = head
				head = n
			}
			s := label(&a, i)
			if len(s) == 0 || head.val != 99 {
				println("wrong")
			}
		} // s is dropped before its memory goes back to the arena
		a.Release(m)
	}
	{
		b := box(&a, 2)
		println("ok", keep.val, b.val, label(&a, 3))
	}
	a.Reset()
	again := a.New[node]()
	again.val = 5
	println("again", again.val)
	a.Drop()`, "ok 1 2 item 3\nagain 5\n", 0},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				src := prelude + c.body + "\n}\n"
				want := c.want
				if i := strings.Index(src, "// here"); i >= 0 {
					want += strconv.Itoa(strings.Count(src[:i], "\n")+1) + ")"
				}
				out, status := runGenCheck(t, src, opt)
				if status != c.status || !strings.Contains(out, want) {
					t.Fatalf("-O%d: status %d, output %q; want status %d and %q", opt, status, out, c.status, want)
				}
			})
		}
	}
}

// TestArenaStaleViews checks that a view of a local arena.Chunked used after
// a Reset or Release of that arena by name is rejected at compile time
// (E3313, section 18.9), with the use and the reclaiming call named.
func TestArenaStaleViews(t *testing.T) {
	t.Parallel()
	const prelude = `package main

import "pluto/arena"

type node struct {
	next *node
	val  int
}

func main() {
	a := arena.NewChunked()
`
	cases := []struct{ name, body, use, reclaim string }{
		{"view after reset", `
	p := a.New[node]()
	p.val = 7
	a.Reset() // here
	println(p.val) // use`, "p", "a.Reset()"},
		{"view after release", `
	m := a.Mark()
	p := a.New[node]()
	p.val = 7
	a.Release(m) // here
	q := a.New[node]()
	q.val = 8
	println(p.val) // use`, "p", "a.Release(m)"},
		{"slice in a chunk of its own", `
	m := a.Mark()
	s := a.Slice[int](3000)
	a.Release(m) // here
	_ = a.Slice[int](3000)
	s[2999] = 1 // use`, "s", "a.Release(m)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := prelude + c.body + "\n\ta.Drop()\n}\n"
			line := func(mark string) string {
				return strconv.Itoa(strings.Count(src[:strings.Index(src, mark)], "\n") + 1)
			}
			out := buildFails(t, src)
			if !strings.Contains(out, "E3313") || !strings.Contains(out, "main.p:"+line("// use")+":") ||
				!strings.Contains(out, c.use) || !strings.Contains(out, c.reclaim) ||
				!strings.Contains(out, "line "+line("// here")) {
				t.Fatalf("output %q; want E3313 at line %s for %s after %s at line %s", out, line("// use"), c.use, c.reclaim, line("// here"))
			}
		})
	}
}

// TestSlabLargeAlign checks that the slab allocators honor an alignment
// above a page (arena epoch chunks ask for 64 KiB) and take such memory
// back, with and without generation tags.
func TestSlabLargeAlign(t *testing.T) {
	t.Parallel()
	const src = `package main

import "pluto/alloc"

func main() {
	slab := alloc.NewSlab(alloc.Pages)
	for _, a := range []alloc.Allocator{alloc.Default(), slab} {
		for i := 0; i < 20; i++ {
			p := a.Alloc(9000, 1<<16)
			q := a.Alloc(3000, 8192)
			*(*int)(p) = 1
			*(*int)(q) = 2
			if uintptr(p)%(1<<16) != 0 || uintptr(q)%8192 != 0 {
				println("misaligned")
			}
			a.Free(p, 9000, 1<<16)
			a.Free(q, 3000, 8192)
		}
	}
	println("aligned")
}
`
	for _, opt := range []int{0, 2} {
		if out, status := runGenCheck(t, src, opt); status != 0 || out != "aligned\n" {
			t.Errorf("-gencheck -O%d: status %d, output %q", opt, status, out)
		}
		if out, status := runUnchecked(t, src, opt); status != 0 || out != "aligned\n" {
			t.Errorf("--gencheck=false -O%d: status %d, output %q", opt, status, out)
		}
	}
}
