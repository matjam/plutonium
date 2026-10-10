package main

import "fmt"

// Strings are values: variables, fields, elements, parameters, results
// and closure state, built by concatenation, slicing and conversion from
// runes. Plutonium counts them, so every copy, overwrite and scope end
// holds or releases a buffer. Every string is valid UTF-8 and is sliced
// only at rune starts (see subset.go): constant bounds come from the
// literal's contents, and other bounds go through rs. Concatenations go
// through cut, which keeps strings short however often a loop repeats.

const strFuncs = `// rs moves i into [0, len(s)] and down to the start of a rune, so that
// it is a valid bound for slicing s.
func rs(s string, i int) int {
	if i <= 0 {
		return 0
	}
	if i >= len(s) {
		return len(s)
	}
	for s[i]&0xc0 == 0x80 {
		i--
	}
	return i
}

// cut keeps a string short: at most 40 bytes, cut at the start of a rune.
func cut(s string) string {
	return s[:rs(s, 40)]
}

`

// strExpr returns an expression of type string.
func (g *gen) strExpr(d int) string {
	if d <= 0 || g.chance(3) {
		return g.strLeaf()
	}
	switch g.r.Intn(8) {
	case 0, 1:
		return "cut(" + g.strExpr(d-1) + " + " + g.strExpr(d-1) + ")"
	case 2:
		return "cut(" + g.strExpr(d-1) + " + " + g.strLeaf() + " + " + g.strExpr(d-1) + ")"
	case 3:
		x := g.strPlace()
		k := g.intExpr(tInt, 1, true).s
		if g.chance(2) {
			return fmt.Sprintf("%s[rs(%s, %s):]", x, x, k)
		}
		return fmt.Sprintf("%s[:rs(%s, %s)]", x, x, k)
	case 4:
		// Code points that are negative, surrogates or too large become
		// U+FFFD.
		return "string(rune(" + g.intExpr(intTypes[2], 1, false).s + "))"
	case 5:
		if c := g.pureCall(tString); c != "" {
			return c
		}
	case 6:
		lit := g.randString()
		b := sliceBounds(lit.val)
		lo := g.r.Intn(len(b))
		hi := lo + g.r.Intn(len(b)-lo)
		return fmt.Sprintf("%s[%d:%d]", lit.src, b[lo], b[hi])
	}
	return g.strLeaf()
}

// strLeaf returns a string literal or variable.
func (g *gen) strLeaf() string {
	switch g.r.Intn(4) {
	case 0:
		return g.randString().src
	case 1:
		return g.stringVar().name
	}
	if p := g.place(tString, false); p != "" {
		return p
	}
	return g.randString().src
}

// strPlace returns a string variable or a path to one, which may be
// evaluated more than once.
func (g *gen) strPlace() string {
	if g.chance(3) {
		if p := g.place(tString, false); p != "" {
			return p
		}
	}
	return g.stringVar().name
}

// strCompare compares two strings.
func (g *gen) strCompare() expr {
	ops := []string{"==", "!=", "<", "<=", ">", ">="}
	return expr{s: "(" + g.strPlace() + " " + ops[g.r.Intn(len(ops))] + " " + g.strExpr(1) + ")"}
}

// strStmt works on a string whose contents are known only when the
// program runs: a local copy, bytes read under a length check, a range
// loop that slices at each rune, a string built in a loop, and a switch.
func (g *gen) strStmt() {
	s, acc := g.name("s"), g.name("acc")
	g.line("{")
	g.indent++
	g.line("%s := %s", s, g.strExpr(2))
	g.line("%s := len(%s)", acc, s)
	switch g.r.Intn(4) {
	case 0:
		k := g.intExpr(g.randInt(), 1, true).s
		g.line("if len(%s) > 0 {", s)
		g.line("\t%s += int(%s[uint(%s)%%uint(len(%s))])", acc, s, k, s)
		g.line("}")
	case 1:
		g.line("for i, r := range %s {", s)
		g.line("\t%s = %s*31 + i + int(r) + len(%s[i:])", acc, acc, s)
		g.line("}")
	case 2:
		n := 1 + g.r.Intn(int(min(g.tripLimit(), 8)))
		g.line("for i := range %d {", n)
		g.line("\t%s = cut(%s + %s)", s, s, g.strExpr(1))
		g.line("\t%s += len(%s) + i", acc, s)
		g.line("}")
		g.addCost(int64(4 * n))
	default:
		g.line("%s = %s[rs(%s, %s):]", s, s, s, g.intExpr(tInt, 1, true).s)
		g.line("%s += len(%s)", acc, s)
	}
	g.line("switch %s {", s)
	seen := map[string]bool{}
	for i, n := 0, 1+g.r.Intn(2); i < n; i++ {
		lit := g.randString()
		if seen[lit.val] {
			continue
		}
		seen[lit.val] = true
		g.line("case %s:", lit.src)
		g.line("\tprintln(%q)", g.name("case"))
	}
	g.line("default:")
	g.line("\tprintln(%q, %s, len(%s))", s, s, s)
	g.line("}")
	g.line("println(%q, %s)", acc, acc)
	g.indent--
	g.line("}")
	g.addCost(16)
}
