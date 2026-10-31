// Ported from internal/build/kernelpanic_test.go at the go-cutover tag.

package tests

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestKernelPanic boots testdata/kernel/panic, a kernel-mode kernel with a
// //pu:panic handler, under QEMU, once for each way it panics, and checks
// what the handler prints over the serial port: the CPU number from the
// per-CPU block, the message, the fault and the stack trace. When the
// handler itself panics, the runtime's last resort halts the processor
// without a word, so QEMU keeps running, silent, until the test stops it.
func TestKernelPanic(t *testing.T) {
	t.Parallel()
	qemu := needQEMU(t)
	src := filepath.Join("..", "testdata", "kernel", "panic")
	files := map[string]string{
		"main.p":       filepath.Join(src, "main.p"),
		"int_amd64.s":  filepath.Join(src, "int_amd64.s"),
		"boot_amd64.s": filepath.Join("..", "testdata", "kernel", "smoke", "boot_amd64.s"),
	}
	cases := []struct {
		scenario string
		want     string // a regular expression; FILE is main.p's trimmed path
		halts    bool   // the last resort halts instead of leaving QEMU
	}{
		{"explicit", `scenario explicit
kernel panic on cpu 2
panic: kernel explicit
    main\.main \(FILE:113\)
`, false},
		{"bounds", `scenario bounds
kernel panic on cpu 2
panic: runtime error: index out of range \[5\] with length 3
    main\.get \(FILE:92\)
    main\.main \(FILE:115\)
`, false},
		{"fault", `scenario fault
kernel panic on cpu 2
panic: runtime error: unexpected fault address 0x40000000
\[fault addr=0x40000000 pc=0xffffffff80[0-9a-f]+\]
    main\.soft \(FILE:72\)
fault at the bad address: true
`, false},
		{"nested", `scenario nested
kernel panic on cpu 2
panic: kernel nested
    main\.main \(FILE:113\)
`, true},
	}
	for _, c := range cases {
		for _, level := range []int{0, 2} {
			t.Run(c.scenario+"/O"+string(rune('0'+level)), func(t *testing.T) {
				t.Parallel()
				tree := map[string]string{
					"scenario.p": "package main\n\nconst scenario = \"" + c.scenario + "\"\n",
				}
				for name, from := range files {
					data, err := os.ReadFile(from)
					if err != nil {
						t.Fatal(err)
					}
					tree[name] = string(data)
				}
				image := mustBuild(t, tree, optFlag(level), "--mode", "kernel", "--gencheck=false", "--format", "binary")
				want := regexp.MustCompile(`\A` + strings.ReplaceAll(c.want, "FILE", `\./main\.p`) + `\z`)
				out, status, running := runQEMU(t, qemu, image, want)
				if !want.MatchString(out) {
					t.Errorf("serial output\n%s\nwant a match for\n%s", out, want)
				}
				switch {
				case c.halts && !running:
					t.Errorf("QEMU exited with status %d; want it halted by the last resort", status)
				case !c.halts && status != 7:
					// The handler leaves through isa-debug-exit with 3,
					// which QEMU reports as (3 << 1) | 1.
					t.Errorf("QEMU exit status %d (running %v), want 7", status, running)
				}
			})
		}
	}
}

// TestKernelGenCheck boots testdata/kernel/gencheck, a kernel-mode kernel
// built with generation checks (roadmap item 101, phase 2), under QEMU. Its
// heap and metadata are static arrays in the upper half. In the live
// scenario checks of live memory pass, also in an interrupt handler entered
// while the runtime holds the directory's writer lock, and the kernel
// leaves QEMU with status 0. In the stale scenario, section 18.11's
// example, the check catches the use of freed memory and the panic handler
// leaves QEMU with status 3.
func TestKernelGenCheck(t *testing.T) {
	t.Parallel()
	qemu := needQEMU(t)
	src := filepath.Join("..", "testdata", "kernel", "gencheck")
	files := map[string]string{
		"main.p":       filepath.Join(src, "main.p"),
		"int_amd64.s":  filepath.Join(src, "int_amd64.s"),
		"boot_amd64.s": filepath.Join("..", "testdata", "kernel", "smoke", "boot_amd64.s"),
	}
	cases := []struct {
		scenario string
		want     string // a regular expression
		status   int    // QEMU's exit status: (code << 1) | 1
	}{
		{"live", `scenario live
upper half true tagged true
sum 49
source called true handler saw 42
metadata used true
`, 1},
		{"stale", `scenario stale
panic: runtime error: use of freed memory[^\n]*
(.*\n)*`, 7},
	}
	for _, c := range cases {
		for _, level := range []int{0, 2} {
			for _, check := range []string{"default", "--gencheck=true"} {
				t.Run(c.scenario+"/O"+string(rune('0'+level))+"/"+strings.TrimPrefix(check, "--"), func(t *testing.T) {
					t.Parallel()
					tree := map[string]string{
						"scenario.p": "package main\n\nconst scenario = \"" + c.scenario + "\"\n",
					}
					for name, from := range files {
						data, err := os.ReadFile(from)
						if err != nil {
							t.Fatal(err)
						}
						tree[name] = string(data)
					}
					flags := []string{optFlag(level), "--mode", "kernel", "--format", "binary"}
					if check != "default" {
						flags = append(flags, check)
					}
					image := mustBuild(t, tree, flags...)
					want := regexp.MustCompile(`\A` + c.want + `\z`)
					out, status, running := runQEMU(t, qemu, image, want)
					if !want.MatchString(out) {
						t.Errorf("serial output\n%s\nwant a match for\n%s", out, want)
					}
					if running || status != c.status {
						t.Errorf("QEMU exit status %d (running %v), want %d", status, running, c.status)
					}
				})
			}
		}
	}
}

// syncBuffer is a bytes.Buffer safe for one writer and one reader.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runQEMU boots image and returns its serial output and exit status. If
// QEMU is still running once the output matches want and a second has
// passed without it exiting, it is stopped and running is true.
func runQEMU(t *testing.T, qemu, image string, want *regexp.Regexp) (out string, status int, running bool) {
	t.Helper()
	cmd := exec.Command(qemu, "-kernel", image, "-serial", "stdio", "-display", "none",
		"-monitor", "none", "-no-reboot", "-m", "64M",
		"-device", "isa-debug-exit,iobase=0xf4,iosize=0x04")
	var stdout syncBuffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	deadline := time.After(60 * time.Second)
	for !want.MatchString(stdout.String()) {
		select {
		case <-done:
			return stdout.String(), cmd.ProcessState.ExitCode(), false
		case <-deadline:
			_ = cmd.Process.Kill()
			<-done
			t.Fatalf("QEMU timed out\nserial: %s\nstderr: %s", stdout.String(), stderr.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
	select {
	case <-done:
		return stdout.String(), cmd.ProcessState.ExitCode(), false
	case <-time.After(time.Second):
		_ = cmd.Process.Kill()
		<-done
		return stdout.String(), -1, true
	}
}
