package main

// A panic runs the panicking puroutine's deferred calls, innermost frame
// first and each frame's last first, before the panic is reported. Their
// arguments were evaluated at the defer statements; deferred calls that
// had not been reached do not run, and nor do those of frames that have
// returned. The trace shows where the panic happened.

import "pluto/fmt"

type file struct {
	name   string
	closed bool
}

func (f *file) Close() {
	f.closed = true
	fmt.Println("closed", f.name)
}

type buffer struct {
	data [16]byte
	n    int
}

func (b *buffer) Write(s string) {
	for i := 0; i < len(s); i++ {
		b.data[b.n] = s[i]
		b.n++
	}
}

func (b *buffer) Flush() {
	fmt.Println("flushed:", string(b.data[:b.n]))
}

func returned() {
	defer fmt.Println("returned normally")
}

func fill(b *buffer, words []string) {
	f := &file{name: "log"}
	defer f.Close()
	defer println("deferred builtin, words =", len(words))
	for _, w := range words {
		b.Write(w)
	}
	println("not reached")
}

func main() {
	var b buffer
	defer b.Flush()
	n := 1
	defer fmt.Println("deferred with n =", n)
	n = 2
	defer func() {
		fmt.Println("closure sees n =", n)
	}()
	returned()
	fill(&b, []string{"hello ", "world ", "and more"})
	defer fmt.Println("not armed")
}
