package main

import (
	"pluto/fmt"
	"pluto/strings"
)

func isComma(r rune) bool { return r == ',' || r == ';' }

func show(name string, parts []string) {
	fmt.Printf("%s %d:", name, len(parts))
	for _, p := range parts {
		fmt.Printf(" [%s]", p)
	}
	fmt.Printf("\n")
}

func main() {
	hay := []string{"", "a", "abc", "abcabcabd", "chicken", "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxy", "日本語日本", "héllo wörld"}
	needles := []string{"", "a", "b", "abd", "ken", "xxxxy", "xy", "本", "日本", "ö", "z", "abcabcabd!"}
	for _, h := range hay {
		for _, n := range needles {
			fmt.Printf("%d %d %d %t ", strings.Index(h, n), strings.LastIndex(h, n), strings.Count(h, n), strings.Contains(h, n))
			fmt.Printf("%d %d %t %t %d ", strings.IndexAny(h, n), strings.LastIndexAny(h, n), strings.HasPrefix(h, n), strings.HasSuffix(h, n), strings.Compare(h, n))
			b, a, f := strings.Cut(h, n)
			fmt.Printf("[%s|%s|%t] [%s] [%s] [%s] [%s] [%s]\n", b, a, f, strings.Trim(h, n), strings.TrimLeft(h, n), strings.TrimRight(h, n), strings.TrimPrefix(h, n), strings.TrimSuffix(h, n))
		}
		fmt.Printf("%d %d %d %d %d\n", strings.IndexByte(h, 'c'), strings.LastIndexByte(h, 'c'), strings.IndexRune(h, '本'), strings.IndexRune(h, 'ö'), strings.IndexRune(h, 'b'))
	}
	fmt.Printf("%d %d\n", strings.IndexRune("a\xffb", 0xFFFD), strings.IndexRune("abc", -1))
	sp := []string{"", "  ", " a ", "\t\n hello world \r\n", " x ", "　日本　 ", "abc"}
	for _, s := range sp {
		fmt.Printf("[%s]\n", strings.TrimSpace(s))
	}
	folds := [][2]string{{"Go", "GO"}, {"go", "gO"}, {"abc", "abd"}, {"日本Go", "日本gO"}, {"a", "ab"}, {"", ""}, {"K", "k"}}
	for _, p := range folds {
		fmt.Printf("%t ", strings.EqualFold(p[0], p[1]))
	}
	fmt.Printf("\n")
	var buf [16]string
	splits := [][2]string{{"a,b,c", ","}, {"", ","}, {"abc", ""}, {"a,b,c,", ","}, {"a--b--c", "--"}, {"日本語", ""}, {"nosep", "x"}}
	for _, p := range splits {
		n := strings.SplitInto(p[0], p[1], buf[:])
		show("split", buf[:n])
		n = strings.SplitInto(p[0], p[1], buf[:2])
		show("split2", buf[:n])
		n = strings.SplitAfterInto(p[0], p[1], buf[:])
		show("after", buf[:n])
		it := strings.SplitIter(p[0], p[1])
		k := 0
		for s, ok := it.Next(); ok; s, ok = it.Next() {
			buf[k] = s
			k++
		}
		show("iter", buf[:k])
	}
	fields := []string{"", "   ", " a b  c ", "foo\tbar\nbaz", "日本 語 x", "a,b;;c,"}
	for _, f := range fields {
		n := strings.FieldsInto(f, buf[:])
		show("fields", buf[:n])
		n = strings.FieldsFuncInto(f, isComma, buf[:])
		show("ffunc", buf[:n])
		it := strings.FieldsIter(f)
		k := 0
		for s, ok := it.Next(); ok; s, ok = it.Next() {
			buf[k] = s
			k++
		}
		show("fiter", buf[:k])
	}
	out := make([]byte, 0, 256)
	out = strings.AppendUpper(out, "Hello, World! héllo ")
	out = strings.AppendLower(out, "Hello, World! HÉLLO ")
	out = strings.AppendRepeat(out, "ab", 3)
	out = append(out, ' ')
	out = strings.AppendJoin(out, []string{"x", "y", "z"}, ", ")
	out = append(out, ' ')
	out = strings.AppendReplace(out, "oink oink oink", "k", "ky", 2)
	out = append(out, ' ')
	out = strings.AppendReplace(out, "oink oink oink", "oink", "moo", -1)
	out = append(out, ' ')
	out = strings.AppendReplace(out, "abc", "", "-", -1)
	fmt.Printf("%s\n", out)

	var mem [8]byte
	b := strings.NewBuilder(mem[:])
	b.WriteString("hi")
	b.WriteByte(' ')
	b.WriteRune('é')
	fmt.Printf("%s %d %d\n", b.String(), b.Len(), b.Cap())

	var g strings.Builder
	g.Grow(4)
	for i := 0; i < 20; i++ {
		g.WriteString("abc")
		g.WriteRune('世')
		fmt.Fprintf(&g, "%d", i)
	}
	fmt.Printf("%s %d\n", g.String(), g.Len())
	g.Reset()
	g.WriteString("reset")
	fmt.Printf("%s\n", g.String())
	g.Free()
	fmt.Printf("%d\n", g.Len())
}
