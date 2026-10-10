package main

import (
	"pluto/bytes"
	"pluto/fmt"
	"pluto/io"
	"pluto/os"
	"unsafe"
)

func isX(r rune) bool { return r == 'x' }

func main() {
	var mem [64]byte
	ins := []string{"", "  hello  ", "xxabcxx", "日本語", "a,b,c", "\t \n"}
	for _, s := range ins {
		n := copy(mem[:], s)
		b := mem[:n]
		before, after, found := bytes.Cut(b, bs(","))
		fmt.Printf("[%s] [%s] [%s] [%s] [%s] [%s|%s|%t] %d %d %d %d %d %t %t %t\n", bytes.TrimSpace(b), bytes.Trim(b, "x"), bytes.TrimLeft(b, "x "),
			bytes.TrimRight(b, "x日語"), bytes.TrimFunc(b, isX), before, after, found,
			bytes.Index(b, bs("bc")), bytes.LastIndex(b, bs("x")), bytes.IndexByte(b, 'l'), bytes.IndexRune(b, '本'), bytes.Count(b, bs("x")),
			bytes.Equal(b, bs("xxabcxx")), bytes.HasPrefix(b, bs("xx")), bytes.EqualFold(b, bs("XXABCxx")))
		fmt.Printf("%d %d %t %t [%s] [%s] %d\n", bytes.Compare(b, bs("a,b,c")), bytes.IndexAny(b, "cb"), bytes.ContainsAny(b, "本"),
			bytes.Contains(b, bs("ll")), bytes.TrimPrefix(b, bs("  ")), bytes.TrimSuffix(b, bs("xx")), len(bytes.TrimSpace(b)))
	}
	var buf bytes.Buffer
	defer buf.Free()
	fmt.Fprintf(&buf, "x=%d ", 42)
	buf.WriteString("hello ")
	buf.WriteByte('!')
	buf.WriteRune('é')
	fmt.Printf("%s %d %t\n", buf.String(), buf.Len(), buf.Cap() >= buf.Len())
	var p [4]byte
	n, err := buf.Read(p[:])
	fmt.Printf("%d %t %s\n", n, err == nil, p[:n])
	c, _ := buf.ReadByte()
	fmt.Printf("%c %s %s\n", c, buf.Next(3), buf.Bytes())
	buf.Truncate(2)
	fmt.Printf("[%s]\n", buf.String())
	var w io.Writer = &buf
	w.Write(bs("more"))
	buf.WriteTo(os.Stdout)
	fmt.Printf("\n%d\n", buf.Len())
	n, err = buf.Read(p[:])
	fmt.Printf("%d %t\n", n, err == io.EOF)
	rb := bytes.NewBufferString("abc")
	k, _ := rb.Read(p[:])
	fmt.Printf("%s\n", p[:k])
	rb.Free()

	// Growing well past the first allocation; reading leaves room at the
	// front that later writes reuse.
	var g bytes.Buffer
	defer g.Free()
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&g, "%d,", i)
	}
	fmt.Printf("%d %s\n", g.Len(), g.Next(10))
	for g.Len() > 100 {
		g.Next(50)
	}
	g.WriteString("end")
	fmt.Printf("%d %s\n", g.Len(), g.Bytes()[g.Len()-12:])
	g.Grow(5000)
	fmt.Println(g.Available() >= 5000)

	// NewBuffer copies its argument to read, and ReadFrom reads all of a
	// reader.
	data := []byte("from a reader")
	rf := bytes.NewBuffer(data)
	data[0] = 'F'
	var h bytes.Buffer
	n64, err := h.ReadFrom(&rf)
	fmt.Printf("%d %t %s %d\n", n64, err == nil, h.Bytes(), rf.Len())
	h.Free()
	rf.Free()
}

func bs(s string) []byte { return unsafe.Slice(unsafe.StringData(s), len(s)) }
