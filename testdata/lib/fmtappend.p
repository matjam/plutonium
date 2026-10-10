package main

import (
	"pluto/fmt"
	"pluto/os"
	"pluto/vec"
)

type point struct{ X, Y int }

// A sink is an Appender of the program's own, which fmt reaches through
// an io.Writer.
type sink struct {
	data  [64]byte
	n     int
	calls int
}

func (s *sink) Push(x byte)       { s.Append(x) }
func (s *sink) Append(xs ...byte) { s.calls++; s.n += copy(s.data[s.n:], xs) }
func (s *sink) Len() int          { return s.n }
func (s *sink) Slice() []byte     { return s.data[:s.n] }
func (s *sink) Truncate(n int)    { s.n = n }

func main() {
	// Appendf, Append and Appendln into a growing Vec: Go's fmt.Append.
	v := vec.New[byte]()
	defer v.Free()
	fmt.Appendf(&v, "%d-%s-%v|", 42, "x", point{1, 2})
	fmt.Append(&v, "a", 1, 2, "b", true)
	fmt.Appendln(&v, "|", 3.5, 'c')
	for i := 0; i < 40; i++ {
		fmt.Appendf(&v, "%03d", i)
	}
	os.Stdout.Write(v.Slice())
	fmt.Println()
	fmt.Println(v.Len())

	// Into a Fixed: what fits, and the record of what did not.
	var mem [16]byte
	f := vec.NewFixed(mem[:])
	fmt.Appendf(&f, "%s=%d", "answer", 42)
	fmt.Printf("%q %t\n", f.Slice(), f.Truncated())
	fmt.Appendf(&f, " and a long tail %v", point{3, 4})
	fmt.Printf("%q %t\n", f.Slice(), f.Truncated())

	// Output longer than the printer's own buffer goes through in pieces.
	var big [600]byte
	g := vec.NewFixed(big[:])
	fmt.Appendf(&g, "%300s|%-250d|", "right", 7)
	fmt.Println(g.Len(), g.Truncated(), string(g.Slice()[295:306]))

	// Any other Appender.
	var s sink
	fmt.Appendf(&s, "%x %v", 255, []int{1, 2})
	fmt.Printf("%s %d\n", s.Slice(), s.calls)
}
