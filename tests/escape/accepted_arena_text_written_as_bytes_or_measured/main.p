package main

import (
	"pluto/arena"
	"pluto/fmt"
	"pluto/os"
	"pluto/strings"
)
func f(a, b string) int {
	ar := arena.NewFixed(256)
	var out strings.Builder
	out.Init(&ar)
	out.WriteString("x")
	os.Stdout.Write(out.Bytes())
	n := len(out.String())
	println(out.String())
	var heap strings.Builder
	defer heap.Free()
	heap.WriteString("y")
	fmt.Println(heap.String(), n)
	return n
}
func main() { _ = f("x", "y") }
