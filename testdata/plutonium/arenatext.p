package main

// Text built in a local arena (language spec section 18.8, Arenas): it is
// a view of the arena, so it is written as bytes, measured or copied before
// the arena goes, never converted to an interface in the function that owns
// the arena (tests/diag/arena_boxed). The program leaves the
// arena with a copy made from the default allocator.

import (
	"pluto/arena"
	"pluto/fmt"
	"pluto/os"
	"pluto/strings"
)

func handle(name string) string {
	a := arena.NewFixed(16 << 10)
	var out strings.Builder
	out.Init(&a)
	out.WriteString("hello, ")
	out.WriteString(name)
	nl := [1]byte{'\n'}
	os.Stdout.Write(out.Bytes())
	os.Stdout.Write(nl[:])
	fmt.Println(len(out.String()))
	return strings.FromUTF8Lossy(out.Bytes())
}

func main() {
	s := handle("arena")
	fmt.Println(s)
}
