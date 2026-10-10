// Strings built at run time: concatenation, the allocating conversions,
// and counted strings copied through variables, fields, arrays, slices,
// closures, deferred calls, interfaces, results and every way out of a
// scope. Plutonium counts these strings (language spec section 6.10); the
// same program under --alloc=debug must free every one
// (internal/build/strcount_test.go).
package main

type person struct {
	name  string
	age   int
	alias [2]string
}

type pair struct {
	k, v string
}

func (p person) describe() string {
	return p.name + " (" + itoa(p.age) + ")"
}

func (p *person) rename(n string) {
	p.name = n + "!"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}

func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

func repeat(s string, n int) (r string) {
	for i := 0; i < n; i++ {
		r += s
	}
	return
}

func firstLong(words []string, min int) string {
	for _, w := range words {
		if len(w) >= min {
			return w + "?"
		}
	}
	return "none"
}

func swap(a, b string) (string, string) { return b, a }

func mkPerson(i int) person {
	n := "p" + itoa(i)
	return person{name: n, age: 20 + i, alias: [2]string{n + "a", n + "b"}}
}

func labels(n int) string {
	s := ""
	i := 0
loop:
	if i < n {
		t := "L" + itoa(i)
		s += t
		i++
		goto loop
	}
	return s
}

func classify(s string) string {
	switch s + "" {
	case "a" + "b":
		return "ab"
	case "x":
		return "x" + s
	}
	return "other:" + s
}

func nested(rows [][]string) string {
	out := ""
outer:
	for i, row := range rows {
		for _, c := range row {
			if c == "stop" {
				break outer
			}
			if c == "skip" {
				continue outer
			}
			out += itoa(i) + c
		}
	}
	return out
}

func describeAny(v any) string {
	switch x := v.(type) {
	case string:
		return "string " + x
	case person:
		return "person " + x.name
	case int:
		return "int " + itoa(x)
	}
	return "?"
}

func deferred() (out string) {
	s := "first"
	defer func(v string) { out = out + "/" + v + "/" + s }(s + "-arg")
	s = "second"
	out = "body"
	return out + "+"
}

func apply(f func(string) string, s string) string { return f(s) }

func main() {
	a := "abc"
	b := a + "def" + a
	println(b, len(b))
	c := b
	c += "!"
	println(b, c)

	// Conversions. (Bytes to a string and back are tested in
	// testdata/lib/strutf8.p: they are not in the subset Go shares.)
	ra := [...]rune{'X', 'b', 'c', 'd', 'e', 'f', 'a', 'b', 'c'}
	println(string(ra[:]), string(ra[1:4]))
	rs := [...]rune{'h', 'é', 'l', 'l', 'o', ',', ' ', 'w', 'ö', 'r', 'l', 'd'}
	println(len(rs), string(rs[7:]), string(rs[1]))
	for _, r := range "añb" {
		println(string(r) + string(r))
	}
	println(itoa(-1234), itoa(0), itoa(56))

	// Swaps and multiple assignment.
	x, y := "left"+a, "right"+a
	x, y = y, x
	println(x, y)
	x, y = swap(x, y)
	println(x, y)

	// Structs and arrays.
	var ps [3]person
	for i := range ps {
		ps[i] = mkPerson(i)
	}
	ps[1] = ps[2]
	for _, p := range ps {
		println(p.describe(), p.alias[0], p.alias[1])
	}
	q := ps[0]
	q.rename("renamed")
	println(q.name, ps[0].name)
	pp := &ps[2]
	pp.alias[1] = pp.name + pp.alias[0]
	println(ps[2].alias[1], ps[1].alias[1])

	// Slices of strings: literals, append, copy.
	parts := []string{"one", a + "two", "three"}
	buf := [8]string{}
	s := buf[:0]
	for _, p := range parts {
		s = s[:len(s)+1]
		s[len(s)-1] = p + "." + p
	}
	n := len(s)
	s = s[:n+len(parts)]
	copy(s[n:], parts)
	println(join(s, ","))
	copy(s[1:], s[:3])
	println(join(s, ","))
	s = s[:3]
	s[2] = "again"
	println(join(buf[:6], "|"))

	// Results, named results and early returns.
	println(repeat("ab", 3), firstLong(parts, 5), firstLong(parts, 50))
	println(labels(3), classify("ab"), classify("x"), classify("q"+a))
	println(nested([][]string{{"a", "b"}, {"c", "skip", "d"}, {"e", "stop", "f"}, {"g"}}))

	// Interfaces.
	var v any = "boxed " + a
	println(describeAny(v), describeAny(ps[0]), describeAny(42))
	vs := []any{a + "1", mkPerson(7), 3}
	for _, e := range vs {
		println(describeAny(e))
	}

	// Closures and method values.
	suffix := "-sfx"
	f := func(t string) string { return t + suffix }
	println(apply(f, a))
	suffix = "-changed" + a
	println(apply(f, "z"))
	m := ps[0].describe
	ps[0].name = "gone"
	println(m(), ps[0].describe())

	// Deferred calls and pointers.
	println(deferred())
	var pv pair
	pa := &pv
	pa.k = a + "k"
	pa.v = pa.k + "v"
	*pa = pair{pa.v, pa.k}
	println(pa.k, pa.v)

	// Comparisons and ordering of built strings.
	println(a+"x" == "abcx", a+"x" < "abd", "b" > a+"z", min(a+"z", "abd"), max("b", a))
}
