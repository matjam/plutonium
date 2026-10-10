package main

// Results returned through a hidden buffer, inlined into the caller. The
// buffer is split into scalars only when every result has scalar parts;
// an array result keeps it in memory.

type set [8]uint32

func mk(chars string) (as set, ok bool) {
	for i := 0; i < len(chars); i++ {
		c := chars[i]
		if c >= 0x80 {
			return as, false
		}
		as[c/32] |= 1 << (c % 32)
	}
	return as, true
}

func (as *set) contains(c byte) bool {
	return (as[c/32] & (1 << (c % 32))) != 0
}

//pu:noinline
func indexAny(s, chars string) int {
	if as, ok := mk(chars); ok {
		for i := 0; i < len(s); i++ {
			if as.contains(s[i]) {
				return i
			}
		}
		return -1
	}
	return -2
}

type P struct{ a, b, c, d, e float64 }

var ps [4]P

func step(p P, k float64) P {
	p.a += p.b * k
	p.c -= p.d * k
	return p
}

func pair(n int) (P, int) {
	return P{float64(n), 1, 2, 3, 4}, n * 2
}

func main() {
	println(indexAny("  hello  ", "cb"), indexAny("a,b,c", "cb"), indexAny("xxabc", "cb"), indexAny("x", "é"))
	for i := range ps {
		ps[i].b = float64(i)
		ps[i].d = 1
	}
	for n := 0; n < 3; n++ {
		for i := range ps {
			ps[i] = step(ps[i], 0.5)
		}
	}
	for i := range ps {
		println(int64(ps[i].a*100), int64(ps[i].c*100))
	}
	p, m := pair(7)
	println(int64(p.a), int64(p.e), m)
}
