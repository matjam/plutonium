package main

// hello is a simple program that demonstrates Plutonium's
// kernel-safe subset. It uses no garbage collector, no goroutines,
// and no hidden heap allocation.
//
// Eventually this will be compiled with:
//   pluto build -o hello ./testdata/programs/hello

func main() {
	msg := "hello, plutonium\n"
	writeString(1, msg)
	exit(0)
}

// writeString writes s to file descriptor fd using a raw syscall.
// This is the low-level form before pluto/os is available.
func writeString(fd int64, s string) {
	// In the final compiler, this calls pluto/os.Write
	// For now it demonstrates the intended usage pattern
	_ = fd
	_ = s
}

// exit terminates the process.
func exit(code int64) {
	_ = code
}
