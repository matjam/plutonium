package build_test

import (
	"errors"
	"io"
	"os/exec"
	"syscall"
	"testing"

	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// A write to a pipe with no reader fails with EPIPE instead of killing the
// process: the runtime catches SIGPIPE, as Go does.
func TestSigpipeReturnsEPIPE(t *testing.T) {
	testguard.Parallel(t)
	runConcCases(t, []concCase{
		{name: "epipe", want: "pipe write: EPIPE true\nstill alive\n", src: `package main

import (
	"pluto/os"
	"pluto/syscall"
)

func main() {
	r, w, err := os.Pipe()
	if err != nil {
		println("pipe:", err.Error())
		return
	}
	r.Close()
	_, err = w.WriteString("hello")
	println("pipe write: EPIPE", err == syscall.EPIPE)
	w.Close()
	println("still alive")
}
`},
		// The signal is raised on the thread that writes, which may be any
		// worker: write from several tasks.
		{name: "epipe-workers", want: "epipe 8\n", src: `package main

import (
	"pluto/os"
	"pluto/syscall"
)

func write(w *os.File, done chan bool) {
	for j := 0; j < 100; j++ {
		if _, err := w.WriteString("x"); err != syscall.EPIPE {
			done <- false
			return
		}
	}
	done <- true
}

func main() {
	r, w, err := os.Pipe()
	if err != nil {
		println("pipe:", err.Error())
		return
	}
	r.Close()
	done := make(chan bool)
	for i := 0; i < 8; i++ {
		task write(w, done)
	}
	n := 0
	for i := 0; i < 8; i++ {
		if <-done {
			n++
		}
	}
	println("epipe", n)
}
`},
	}, build.Config{})
}

// A write to standard output or standard error that fails with EPIPE ends
// the program with SIGPIPE, as in Go.
func TestSigpipeStdio(t *testing.T) {
	testguard.Parallel(t)
	for _, c := range []struct{ name, stream string }{
		{"stdout", "Stdout"},
		{"stderr", "Stderr"},
	} {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			prog := buildConc(t, `package main

import "pluto/os"

func main() {
	for {
		os.`+c.stream+`.WriteString("hello\n")
	}
}
`, build.Config{})
			cmd := exec.Command(prog)
			var rd io.ReadCloser
			var err error
			if c.stream == "Stdout" {
				rd, err = cmd.StdoutPipe()
			} else {
				rd, err = cmd.StderrPipe()
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			var one [1]byte
			if _, err := io.ReadFull(rd, one[:]); err != nil {
				t.Fatal(err)
			}
			_ = rd.Close()
			err = cmd.Wait()
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				t.Fatalf("wait: %v, want death by SIGPIPE", err)
			}
			ws := ee.Sys().(syscall.WaitStatus)
			if !ws.Signaled() || ws.Signal() != syscall.SIGPIPE {
				t.Errorf("status %v, want death by SIGPIPE", ws)
			}
		})
	}
}
