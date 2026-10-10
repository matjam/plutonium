package build_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
)

// TestFreestandingNoAllocator boots a freestanding program under QEMU that
// calls new before alloc.SetDefault: it must panic with "no allocator"
// (language spec section 7.2). The boot code and linker script are those of
// testdata/kernel/irq; the program's panic handler leaves QEMU through the
// isa-debug-exit device with status 3.
func TestFreestandingNoAllocator(t *testing.T) {
	t.Parallel()
	qemu, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Skip("qemu-system-x86_64 is not installed")
	}
	const src = `package main

import (
	"pluto/rt"
	"unsafe"
)

// Implemented in boot_amd64.s.
func outb(port uint16, v uint8)

func serialWrite(p unsafe.Pointer, n int) {
	for i := 0; i < n; i++ {
		outb(0x3F8, *(*uint8)(unsafe.Add(p, i)))
	}
}

//pu:panic
func onPanic(p *rt.Panic) {
	p.Print(rt.Console{})
	for {
		outb(0xF4, 1)
	}
}

type node struct{ next *node }

func main() int64 {
	rt.SetConsole(serialWrite)
	println("before")
	n := new(node)
	println("unreachable", n)
	return 0
}
`
	irq := filepath.Join("..", "..", "testdata", "kernel", "irq")
	dir := t.TempDir()
	for _, name := range []string{"boot_amd64.s", "kernel.ld"} {
		data, err := os.ReadFile(filepath.Join(irq, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(dir, "kernel.bin")
	cfg := &build.Config{
		Dir: dir, Output: image, Stdlib: plutonium.Stdlib(),
		Freestanding: true, LinkerScript: filepath.Join(dir, "kernel.ld"), Format: "binary",
	}
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
	err = cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("qemu: %v (want exit status 3)\nserial: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	// The message, then the stack trace: the new on line 30.
	want := regexp.MustCompile(`\Abefore\npanic: no allocator: call alloc\.SetDefault before allocating\n    main\.main \(\S*/main\.p:30\)\n\z`)
	if got := stdout.String(); !want.MatchString(got) {
		t.Errorf("serial output %q, want a match for %q", got, want)
	}
}

// TestNoAllocator checks that new and make panic when there is nothing to
// allocate from (language spec sections 7.2 and 8): with "no allocator"
// when the default allocator has not been set, as in a freestanding program
// before alloc.SetDefault, and with a message of its own when the explicit
// allocator is nil.
func TestNoAllocator(t *testing.T) {
	t.Parallel()
	const noDefault = "panic: no allocator: call alloc.SetDefault before allocating\n"
	const nilArg = "panic: nil allocator passed to new or make\n"
	cases := []struct {
		name, body, want string
		line             int // of the new or make, which the stack trace names
	}{
		{"new without a default", "alloc.SetDefault(nil)\n\t_ = new(int)", noDefault, 9},
		{"new with a nil allocator", "var a alloc.Allocator\n\t_ = new(int, a)", nilArg, 9},
	}
	for _, c := range cases {
		want := regexp.MustCompile(`\A` + regexp.QuoteMeta(c.want) + `    main\.main \(\S*/main\.p:` + strconv.Itoa(c.line) + `\)\n\z`)
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := "package main\n\nimport \"pluto/alloc\"\n\nvar _ alloc.Allocator\n\nfunc main() {\n\t" + c.body + "\n\tprintln(\"unreachable\")\n}\n"
			out, status := compileAndRun(t, src, 0)
			if status != 2 || !want.MatchString(out) {
				t.Errorf("exit status %d, output %q; want 2, a match for %q", status, out, want)
			}
		})
	}
}
