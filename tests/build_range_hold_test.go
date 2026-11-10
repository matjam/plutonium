package tests

import (
	"fmt"
	"testing"
)

// Section 12.10 (roadmap item 173): a range loop over a counted value (a
// string, a map, a channel, or a value holding one) holds its own count of
// the ranged value until the loop ends, so a body that overwrites the
// variable it ranges over frees nothing the loop still reads. Run with the
// debug allocator, whose poisoned frees catch a read of freed memory.
func TestRangeHoldsCount(t *testing.T) {
	t.Parallel()
	const prelude = `package main
import ("pluto/alloc"; "pluto/strings")
type Res owner struct { id int }
func (r *Res) Drop() { println("drop", r.id) }
func mk(i int) shared[Res] { p := alloc.Shared[Res](); p.id = i; return p }
`
	cases := []struct{ name, source, output string }{
		{"array", prelude + `func main() {
	a := [1]shared[Res]{mk(1)}
	for _, r := range a {
		a = [1]shared[Res]{}
		println("cleared")
		println("got", r.id)
	}
	println("after array")
}
`, "cleared\ngot 1\ndrop 1\nafter array\n"},
		{"map", prelude + `func main() {
	m := map[int]shared[Res]{1: mk(3)}
	for k := range m {
		m = nil
		println("key", k)
	}
	println("after map")
}
`, "key 1\ndrop 3\nafter map\n"},
		// A shared reference is not sendable, so the channel carries
		// strings; the loop still receives from it after c is cleared.
		{"channel", prelude + `func main() {
	c := make(chan string, 2)
	c <- strings.Repeat("d", 20)
	c <- strings.Repeat("e", 20)
	close(c)
	for r := range c {
		c = nil
		println("recv", r)
	}
	println("after chan")
}
`, "recv dddddddddddddddddddd\nrecv eeeeeeeeeeeeeeeeeeee\nafter chan\n"},
		{"string", prelude + `func main() {
	s := strings.Repeat("ab", 975)
	n := 0
	for i, r := range s {
		s = ""
		if r == 'a' && i%2 == 0 || r == 'b' && i%2 == 1 {
			n++
		}
	}
	println(n, len(s))
}
`, "1950 0\n"},
		// The twin: loops that leave the ranged variable alone.
		{"ordinaryLoops", prelude + `func main() {
	s := strings.Repeat("ab", 5)
	m := map[string]int{s: 1}
	xs := []int{1, 2, 3}
	t := 0
	for _, x := range xs {
		t += x
	}
	for k, v := range m {
		t += len(k) + v
	}
	for range s {
		t++
	}
	println(t, len(s), len(m))
}
`, "27 10 1\n"},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/O%d", c.name, opt), func(t *testing.T) {
				t.Parallel()
				exe := buildDebugAlloc(t, dropUnusedStrings(c.source), opt)
				out, status := runDebugAlloc(t, exe, "")
				if status != 0 || out != c.output {
					t.Fatalf("status %d, output %q; want status 0 and %q", status, out, c.output)
				}
			})
		}
	}
}
