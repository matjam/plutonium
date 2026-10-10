package main

import (
	"strconv"
	"strings"
)

// Floating-point values are first-class scalars: variables, fields,
// elements, parameters and results. Neither Go (at GOAMD64=v1) nor
// Plutonium fuses multiply-add, so every operation rounds the same way
// and inexact results are still deterministic. Floats reach the output
// only through f2i, which converts values in range and maps the rest
// (NaN, infinities, huge values) to fixed markers, since converting an
// out-of-range float to an integer is implementation-defined.

const f2iFunc = `// f2i converts x to an integer where that is defined.
func f2i(x float64) int64 {
	if x > -9e18 && x < 9e18 {
		return int64(x)
	}
	if x != x {
		return -1
	}
	if x > 0 {
		return 1
	}
	return -2
}

`

// leafOrBool spells a printable form of scalar p of type t.
func leafOrBool(p string, t *typ) string {
	if t.kind == kFloat {
		return toInt(p, t)
	}
	return p
}

// randFloat picks a float type.
func (g *gen) randFloat() *typ {
	if g.chance(3) {
		return tFloat32
	}
	return tFloat64
}

// toInt spells an int64 conversion of float expression e of type t.
func toInt(e string, t *typ) string {
	if t == tFloat32 {
		return "f2i(float64(" + e + "))"
	}
	return "f2i(" + e + ")"
}

// floatConst returns a constant that fits t, never zero when nonzero is
// set (a constant zero divisor is a compile error).
func (g *gen) floatConst(t *typ, nonzero bool) string {
	vals := []float64{0, 1, -1, 0.5, -0.5, 1.5, -2.25, 3, 10, 0.1, -0.3, 1e10, -1e-5, 123.456, 1e-30, 6.02e23, 65536, -4096}
	if t == tFloat64 {
		vals = append(vals, 1e300, -1e-300, 4.9e-324, 1.7976931348623157e308, 9007199254740993)
	}
	for {
		v := vals[g.r.Intn(len(vals))]
		if g.chance(4) {
			v = float64(g.r.Intn(2001)-1000) / 8
		}
		if nonzero && v == 0 {
			continue
		}
		s := strconv.FormatFloat(v, 'g', -1, 64)
		if !strings.ContainsAny(s, ".e") {
			s += ".0"
		}
		return s
	}
}

// floatExpr returns an expression of float type t.
func (g *gen) floatExpr(t *typ, d int, nc bool) expr {
	if d <= 0 || g.chance(4) {
		return g.floatLeaf(t, nc)
	}
	switch k := g.r.Intn(10); {
	case k < 5:
		op := []string{"+", "-", "*", "/"}[g.r.Intn(4)]
		a := g.floatExpr(t, d-1, false)
		var b expr
		if a.c || op == "/" && g.chance(2) {
			b = g.floatExpr(t, d-1, true)
		} else {
			b = g.floatExpr(t, d-1, false)
			if b.c && op == "/" {
				b.s = g.floatConst(t, true)
			}
		}
		return expr{s: "(" + a.s + " " + op + " " + b.s + ")"}
	case k < 6:
		return expr{s: "(-" + g.floatExpr(t, d-1, true).s + ")"}
	case k < 7:
		fn := "min"
		if g.chance(2) {
			fn = "max"
		}
		a := g.floatExpr(t, d-1, false)
		b := g.floatExpr(t, d-1, a.c)
		return expr{s: fn + "(" + a.s + ", " + b.s + ")"}
	case k < 8:
		// From an integer, which may round for large values.
		return expr{s: t.String() + "(" + g.intExpr(g.randInt(), d-1, true).s + ")"}
	case k < 9:
		// From the other float type.
		u := tFloat64
		if t == tFloat64 {
			u = tFloat32
		}
		return expr{s: t.String() + "(" + g.floatExpr(u, d-1, true).s + ")"}
	default:
		if c := g.pureCall(t); c != "" {
			return expr{s: c}
		}
		return g.floatLeaf(t, nc)
	}
}

func (g *gen) floatLeaf(t *typ, nc bool) expr {
	for {
		switch g.r.Intn(6) {
		case 0, 1:
			if !nc {
				return expr{s: g.floatConst(t, false), c: true}
			}
		case 2, 3, 4:
			if p := g.place(t, false); p != "" {
				return expr{s: p}
			}
		default:
			return expr{s: t.String() + "(" + g.intExpr(g.randInt(), 0, true).s + ")"}
		}
	}
}

// floatCompare compares two floats, which may be NaN.
func (g *gen) floatCompare(d int) expr {
	t := g.randFloat()
	a := g.floatExpr(t, d-1, false)
	b := g.floatExpr(t, d-1, a.c)
	ops := []string{"==", "!=", "<", "<=", ">", ">="}
	return expr{s: "(" + a.s + " " + ops[g.r.Intn(len(ops))] + " " + b.s + ")"}
}
