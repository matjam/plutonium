package main

// append is not permitted: a slice cannot grow (language spec section 3).
// Each shape of call gets the fix for its idiom.

type list struct{ items []int }

func main() {
	var s []int
	s = append(s, 1)    // growing: Push
	s = append(s, 2, 3) // growing: Append
	var b []byte
	b = append(b, "yz"...)        // a string's bytes: vec.AppendString
	c := append([]int(nil), s...) // copying: slices.Clone
	d := append(s, c...)          // joining: slices.Concat
	s = append(s[:1], s[2:]...)   // deleting: slices.Delete
	var buf [16]byte
	out := append(buf[:0], 'a', 'b') // filling an array: vec.Fixed
	var l list
	l.items = append(l.items, 4)              // a field
	var append = func(x int) int { return x } // a program's own append is fine
	println(len(s), len(b), len(c), len(d), len(out), len(l.items), append(1))
}
