package build_test

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// TestKernelScheduler boots testdata/kernel/sched, a kernel-mode image
// that binds its own scheduler (a per-CPU round-robin run queue with
// preemption from the PIT timer interrupt), under QEMU, and checks that
// tasks, channels, select and pluto/sync run on it: a spinning
// task with no calls is preempted, and the timer handler wakes a
// parked task with a non-blocking channel send.
func TestKernelScheduler(t *testing.T) {
	testguard.Parallel(t)
	qemu, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Skip("qemu-system-x86_64 is not installed")
	}
	dir := filepath.Join("..", "..", "testdata", "kernel", "sched")
	const want = "sum of squares 385\n" +
		"map 4 735 721 2 2\n" +
		"spinner preempted by the timer\n" +
		"ticks from the interrupt handler true true true\n" +
		"b 42\n" +
		"slept true\n" +
		"woke in order 1 2 3\n" +
		"select timed out\n" +
		"ticker true true\n" +
		"cubes 36\n" +
		"waited 3\n" +
		"done\n"
	for _, level := range []int{0, 2} {
		t.Run("O"+string(rune('0'+level)), func(t *testing.T) {
			testguard.Parallel(t)
			elfOut := filepath.Join(t.TempDir(), "kernel.elf")
			cfg := &build.Config{Dir: dir, Output: elfOut, Stdlib: plutonium.Stdlib(), OptLevel: level, Kernel: true}
			if err := buildCfg(cfg); err != nil {
				t.Fatalf("build ELF: %v", err)
			}
			image := filepath.Join(t.TempDir(), "kernel.bin")
			cfg.Output, cfg.Format = image, "binary"
			if err := buildCfg(cfg); err != nil {
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

// TestKernelInterrupts boots the freestanding kernel in testdata/kernel/irq
// under QEMU. It installs two //pu:interrupt handlers and enters them with
// int $0x80 and with a general protection fault (which pushes an error
// code), then reports over the serial port whether every register survived,
// the stack alignment inside the handlers, and the error code.
func TestKernelInterrupts(t *testing.T) {
	testguard.Parallel(t)
	qemu, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Skip("qemu-system-x86_64 is not installed")
	}
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "kernel", "irq"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "in handler: vector 0x80, cs 8\n" +
		"int 0x80: count 1 registers changed 0 stack mod 16 0 cs 8\n" +
		"float in handler x2: 3\n" +
		"#GP: count 1 error code 24 stack mod 16 0\n"
	for _, level := range []int{0, 2} {
		t.Run("O"+string(rune('0'+level)), func(t *testing.T) {
			testguard.Parallel(t)
			image := filepath.Join(t.TempDir(), "kernel.bin")
			cfg := &build.Config{
				Dir: dir, Output: image, Stdlib: plutonium.Stdlib(), OptLevel: level,
				Freestanding: true, LinkerScript: filepath.Join(dir, "kernel.ld"), Format: "binary",
			}
			if err := buildCfg(cfg); err != nil {
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
			// The kernel leaves through isa-debug-exit with 0, which QEMU
			// reports as status (0 << 1) | 1.
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
