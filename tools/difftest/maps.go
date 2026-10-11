package main

import "fmt"

// Maps are counted references to hash tables in Plutonium (language spec
// section 7.10), with Go's semantics: copies alias one table, a function
// changes its caller's map, a nil map reads as empty. The generator
// exercises every operation and prints only what does not depend on the
// order a range visits keys in: lengths, lookups of given keys, and sums
// of a term per entry. A range body changes the map only by deleting the
// entry it is visiting, whose effect is the same in any order.

const mapFuncs = `// mfill stores into m, which the caller sees: a map is a reference.
func mfill(m map[int]int, n, base int) {
	for i := 0; i < n; i++ {
		m[i*7%13] += base + i
	}
}

// msum folds a map's entries in a way that does not depend on their order.
func msum(m map[int]int) int {
	s := 0
	for k, v := range m {
		s += k*31 ^ v
	}
	return s
}

// ssum is msum for string keys.
func ssum(m map[string]int) int {
	s := 0
	for k, v := range m {
		s += len(k)*17 ^ v
		for _, r := range k {
			s += int(r)
		}
	}
	return s
}

`

// mapKeys are the string keys of generated maps: few, so that stores,
// lookups and deletions meet.
var mapKeys = []string{`""`, `"a"`, `"b"`, `"ab"`, `"é"`, `"key"`}

// mapStmt writes a block that makes a map and works on it.
func (g *gen) mapStmt() {
	if !subset.Maps {
		return
	}
	g.line("{")
	g.indent++
	if g.chance(3) {
		g.stringMapOps()
	} else {
		g.intMapOps()
	}
	g.indent--
	g.line("}")
}

// intKey returns an int expression in a small range, as a map key.
func (g *gen) intKey() string {
	if g.chance(3) {
		return fmt.Sprint(g.r.Intn(16))
	}
	return "(" + g.intExpr(tInt, 1, true).s + ")&15"
}

// intMapOps works on a map[int]int: stores, compound assignments,
// deletions, lookups, aliases, calls that change it, ranges and clear.
func (g *gen) intMapOps() {
	m := g.name("m")
	switch g.r.Intn(3) {
	case 0:
		g.line("%s := map[int]int{}", m)
	case 1:
		g.line("%s := make(map[int]int, %d)", m, g.r.Intn(20))
	default:
		g.line("%s := map[int]int{%d: %s, %d: %s}", m, g.r.Intn(8), g.intExpr(tInt, 1, true).s, 8+g.r.Intn(8), g.intExpr(tInt, 1, true).s)
	}
	alias := ""
	for i, n := 0, 4+g.r.Intn(8); i < n; i++ {
		target := m
		if alias != "" && g.chance(2) {
			target = alias
		}
		switch g.r.Intn(14) {
		case 0, 1:
			g.line("%s[%s] = %s", target, g.intKey(), g.intExpr(tInt, 1, true).s)
		case 2:
			g.line("%s[%s] += %s", target, g.intKey(), g.intExpr(tInt, 1, true).s)
		case 3:
			g.line("%s[%s]++", target, g.intKey())
		case 4:
			g.line("%s[%s] ^= %s", target, g.intKey(), g.intExpr(tInt, 1, true).s)
		case 5:
			g.line("delete(%s, %s)", target, g.intKey())
		case 6:
			v, ok := g.name("v"), g.name("ok")
			g.line("%s, %s := %s[%s]", v, ok, target, g.intKey())
			g.line("println(%q, %s, %s, len(%s))", v, v, ok, target)
		case 7:
			g.line("println(%q, %s[%s], %s[%s])", m, target, g.intKey(), m, g.intKey())
		case 8:
			if alias == "" {
				alias = g.name("alias")
				g.line("%s := %s", alias, m)
				g.line("_ = %s", alias)
			}
		case 9:
			g.line("mfill(%s, %d, %s)", target, 1+g.r.Intn(20), g.intExpr(tInt, 1, true).s)
		case 10:
			// Deleting the entry being visited: the result does not depend
			// on the order of the visits.
			k, v := g.name("k"), g.name("v")
			g.line("for %s, %s := range %s {", k, v, target)
			g.line("\tif (%s^%s)&%d == 0 {", k, v, 1+g.r.Intn(3))
			g.line("\t\tdelete(%s, %s)", target, k)
			g.line("\t}")
			g.line("}")
		case 11:
			acc := g.name("acc")
			g.line("%s := 0", acc)
			g.line("for k := range %s {", target)
			g.line("\t%s += k * k", acc)
			g.line("}")
			g.line("println(%q, %s, msum(%s))", acc, acc, target)
		case 12:
			if g.chance(3) {
				g.line("clear(%s)", target)
			} else {
				nested := g.name("nest")
				k := g.intKey()
				g.line("%s := map[int]map[int]int{%s: %s}", nested, k, target)
				g.line("%s[%s][%s] = %d", nested, k, g.intKey(), g.r.Intn(100))
				g.line("println(%q, len(%s), len(%s[%s]), len(%s[99]))", nested, nested, nested, k, nested)
			}
		default:
			var nm = g.name("nilm")
			g.line("var %s map[int]int", nm)
			g.line("delete(%s, %s)", nm, g.intKey())
			g.line("println(%q, %s[%s], len(%s), %s == nil)", nm, nm, g.intKey(), nm, nm)
		}
	}
	g.line("println(%q, len(%s), msum(%s))", m, m, m)
	g.addCost(60)
}

// stringMapOps works on a map[string]int with keys from mapKeys and
// counted strings made at run time.
func (g *gen) stringMapOps() {
	m := g.name("sm")
	g.line("%s := map[string]int{%s: 1}", m, mapKeys[g.r.Intn(len(mapKeys))])
	key := func() string {
		if g.chance(4) {
			return g.strExpr(1)
		}
		return mapKeys[g.r.Intn(len(mapKeys))]
	}
	for i, n := 0, 3+g.r.Intn(6); i < n; i++ {
		switch g.r.Intn(7) {
		case 0, 1:
			g.line("%s[%s] = %s", m, key(), g.intExpr(tInt, 1, true).s)
		case 2:
			g.line("%s[%s] += len(%s)", m, key(), key())
		case 3:
			g.line("delete(%s, %s)", m, key())
		case 4:
			v, ok := g.name("v"), g.name("ok")
			g.line("%s, %s := %s[%s]", v, ok, m, key())
			g.line("println(%q, %s, %s)", v, v, ok)
		case 5:
			k := g.name("k")
			g.line("for %s := range %s {", k, m)
			g.line("\tif len(%s) > 1 {", k)
			g.line("\t\tdelete(%s, %s)", m, k)
			g.line("\t}")
			g.line("}")
		default:
			c := g.name("copy")
			g.line("%s := map[string]int{}", c)
			g.line("for k, v := range %s {", m)
			g.line("\t%s[k+\"!\"] = v", c)
			g.line("}")
			g.line("println(%q, len(%s), ssum(%s))", c, c, c)
		}
	}
	g.line("println(%q, len(%s), ssum(%s))", m, m, m)
	g.addCost(50)
}
