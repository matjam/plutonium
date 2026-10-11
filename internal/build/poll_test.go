package build_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/build"
)

// TestPoller checks the epoll poller: separate read and write waiters on
// one descriptor, and Close waking the tasks parked on it.
func TestPoller(t *testing.T) {
	runConcCases(t, []concCase{
		// One end of a socket pair has a task waiting to read and
		// another waiting to write (the socket buffer is full) at the same
		// time. A third drains the other end and then answers.
		{name: "readwrite", want: "drained 4000000\nreply 4\n", src: `package main

import (
	"pluto/os"
	"pluto/syscall"
)

func writer(a *os.File, done chan int) {
	var chunk [10000]byte
	for i := 0; i < 400; i++ {
		if _, err := a.Write(chunk[:]); err != nil {
			println("write:", err.Error())
		}
	}
	done <- 1
}

func reader(a *os.File, done chan int) {
	var buf [16]byte
	n, err := a.Read(buf[:])
	if err != nil {
		println("read:", err.Error())
	}
	done <- n
}

func drain(b *os.File, want int, done chan int) {
	var buf [4096]byte
	total := 0
	for total < want {
		n, err := b.Read(buf[:])
		if err != nil {
			println("drain:", err.Error())
			break
		}
		total += n
	}
	b.WriteString("done")
	done <- total
}

func main() {
	var fds [2]int32
	if err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, 0, &fds); err != nil {
		println("socketpair:", err.Error())
		return
	}
	a := os.NewFile(int(fds[0]), "a")
	b := os.NewFile(int(fds[1]), "b")
	wdone := make(chan int)
	rdone := make(chan int)
	ddone := make(chan int)
	task reader(a, rdone)
	task writer(a, wdone)
	task drain(b, 4000000, ddone)
	<-wdone
	println("drained", <-ddone)
	println("reply", <-rdone)
	a.Close()
	b.Close()
}
`},
		// Close wakes everything parked on the descriptor: several readers
		// of a pipe, then a writer of a full pipe. Each reports ErrClosed,
		// and so does any later use of the file.
		{name: "close", want: "readers 3 closed\nwriter closed\nlater closed true true\n", src: `package main

import (
	"pluto/os"
	"pluto/rt"
	"pluto/time"
)

func read(r *os.File, res chan bool) {
	var buf [8]byte
	_, err := r.Read(buf[:])
	res <- err == os.ErrClosed
}

func write(w *os.File, res chan bool) {
	var chunk [4096]byte
	for {
		if _, err := w.Write(chunk[:]); err != nil {
			res <- err == os.ErrClosed
			return
		}
	}
}

// settle lets the tasks run until they are parked in the poller.
func settle() {
	for i := 0; i < 10; i++ {
		rt.Yield()
	}
	time.Sleep(50 * time.Millisecond)
}

func main() {
	r, w, err := os.Pipe()
	if err != nil {
		println("pipe:", err.Error())
		return
	}
	res := make(chan bool)
	for i := 0; i < 3; i++ {
		task read(r, res)
	}
	settle()
	r.Close()
	n := 0
	for i := 0; i < 3; i++ {
		if <-res {
			n++
		}
	}
	println("readers", n, "closed")
	w.Close()

	r, w, err = os.Pipe()
	if err != nil {
		println("pipe:", err.Error())
		return
	}
	task write(w, res)
	settle()
	w.Close()
	if <-res {
		println("writer closed")
	}
	var buf [1]byte
	_, e1 := r.Read(buf[:])
	r.Close()
	_, e2 := r.Read(buf[:])
	println("later closed", e1 != os.ErrClosed, e2 == os.ErrClosed && r.Close() == os.ErrClosed)
}
`},
		// Many rounds of a task reading while the program closes the
		// file, with and without giving the reader time to park: the
		// poller's records and the descriptors are reused every round, and
		// the read must report ErrClosed each time.
		{name: "closeloop", want: "closed 400\n", src: `package main

import (
	"pluto/os"
	"pluto/rt"
)

func read(r *os.File, res chan bool) {
	var buf [8]byte
	_, err := r.Read(buf[:])
	res <- err == os.ErrClosed
}

func main() {
	res := make(chan bool)
	closed := 0
	for i := 0; i < 400; i++ {
		r, w, err := os.Pipe()
		if err != nil {
			println("pipe:", err.Error())
			return
		}
		task read(r, res)
		if i%2 == 0 {
			rt.Yield()
		}
		r.Close()
		if <-res {
			closed++
		}
		w.Close()
	}
	println("closed", closed)
}
`},
	}, build.Config{})
}
