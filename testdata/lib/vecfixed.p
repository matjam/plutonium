package main

import (
	"pluto/fmt"
	"pluto/iter"
	"pluto/strings"
	"pluto/vec"
)

// putDigits writes n in decimal into dst, whichever kind of Appender it is.
func putDigits[D vec.Appender[byte]](dst D, n int) {
	var buf [20]byte
	i := len(buf)
	for {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			break
		}
	}
	dst.Append(buf[i:]...)
}

// line formats a short report into dst and returns its length.
func line[D vec.Appender[byte]](dst D, name string, n int) int {
	start := dst.Len()
	vec.AppendString(dst, name)
	dst.Push('=')
	putDigits(dst, n)
	if dst.Len()-start > 12 {
		// Too long for the report: take it back.
		dst.Truncate(start)
	}
	return dst.Len() - start
}

func count(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func main() {
	// A Fixed stores what fits and records the rest.
	var mem [8]byte
	f := vec.NewFixed(mem[:])
	fmt.Println(f.Len(), f.Cap(), f.Truncated())
	vec.AppendString(&f, "hello")
	f.Push(' ')
	fmt.Printf("%q %d %t\n", f.Slice(), f.Len(), f.Truncated())
	f.Append('w', 'o', 'r', 'l', 'd')
	fmt.Printf("%q %d %t\n", f.Slice(), f.Len(), f.Truncated())
	f.Push('!')
	fmt.Printf("%q %t\n", f.Slice(), f.Truncated())
	fmt.Println(len(f.Slice()), cap(f.Slice()))

	// Truncate keeps the record; Reset clears it.
	f.Truncate(2)
	fmt.Printf("%q %t\n", f.Slice(), f.Truncated())
	f.Reset()
	fmt.Printf("%q %t\n", f.Slice(), f.Truncated())

	// TryPush refuses instead of recording.
	var three [3]int
	g := vec.NewFixed(three[:])
	for i := 1; i <= 4; i++ {
		fmt.Print(g.TryPush(i*10), " ")
	}
	fmt.Println(g.Slice(), g.Truncated())

	// The storage's length is the room, whatever its capacity.
	var big [16]byte
	h := vec.NewFixed(big[:4])
	vec.AppendString(&h, "0123456789")
	fmt.Printf("%q %d %t\n", h.Slice(), h.Cap(), h.Truncated())

	// The same generic code writes into a Vec, which grows, and a Fixed.
	v := vec.New[byte]()
	defer v.Free()
	for i := 0; i < 5; i++ {
		n := line(&v, "item", i*12345)
		v.Push(';')
		fmt.Print(n, " ")
	}
	fmt.Println()
	fmt.Println(strings.FromUTF8Lossy(v.Slice()))
	fmt.Println(line(&v, "much-too-long", 1), v.Len())
	var small [24]byte
	w := vec.NewFixed(small[:])
	for i := 0; i < 5; i++ {
		n := line(&w, "item", i*12345)
		w.Push(';')
		fmt.Print(n, " ")
	}
	fmt.Println()
	fmt.Println(strings.FromUTF8Lossy(w.Slice()), w.Truncated())
	fmt.Println(line(&w, "much-too-long", 1), w.Len())

	// A Fixed of strings releases those it drops from its storage.
	var names [4]string
	s := vec.NewFixed(names[:])
	for i := 0; i < 6; i++ {
		s.Push(fmt.Sprint("name", i))
	}
	fmt.Println(s.Slice(), s.Truncated())
	s.Truncate(1)
	fmt.Printf("%q %q\n", names[0], names[1])

	// vec.Vec's Insert and Replace grow as needed.
	iv := vec.New[int]()
	defer iv.Free()
	iv.Append(1, 2, 3, 4, 5)
	iv.Insert(2, 10, 11)
	fmt.Println(iv.Slice(), iv.Len())
	iv.Insert(iv.Len(), 99)
	iv.Insert(0)
	fmt.Println(iv.Slice())
	iv.Replace(1, 3, 7, 7, 7, 7)
	fmt.Println(iv.Slice())
	iv.Replace(0, 5, 42)
	fmt.Println(iv.Slice())
	iv.Replace(1, 1)
	iv.Replace(0, iv.Len())
	fmt.Println(iv.Slice(), iv.Len())
	iv.AppendSeq(count(4))
	iv.AppendSeq(count(2))
	fmt.Println("AppendSeq", iv.Slice())

	sv := vec.New[string]()
	sv.Append("a", "b", "c", "d")
	sv.Insert(1, "x"+fmt.Sprint(1), "y")
	sv.Replace(0, 3, "q")
	sv.Replace(1, 2, "r", "s", "t", "u", "v")
	fmt.Println(sv.Slice(), sv.Len())
	sv.Free()

	// Make is Go's make([]T, n).
	m := vec.Make[int](3)
	m.Set(1, 5)
	m.Push(7)
	fmt.Println(m.Slice(), m.Len())
	m.Free()
	ms := vec.Make[string](2)
	ms[0] = "x" + fmt.Sprint(9)
	fmt.Printf("%q %d\n", ms.Slice(), ms.Len())
	ms.Free()
	e := vec.Make[int](0)
	fmt.Println(e.Len(), e.Cap())
}
