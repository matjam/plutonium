package constant

import (
	"fmt"
	gc "go/constant"
	gotoken "go/token"
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/token"
)

// These tests check this package against go/constant: every result is
// compared with go/constant's for the same input, through describe, which
// prints everything the API can tell about a value.

// pair is the same constant in both packages.
type pair struct {
	x Value
	g gc.Value
}

func describe(x Value) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s", x.Kind(), x.String(), x.ExactString())
	switch x.Kind() {
	case Bool:
		fmt.Fprintf(&b, " %v", BoolVal(x))
	case String:
		fmt.Fprintf(&b, " %q %d", StringVal(x), StringLen(x))
	case Int:
		i, iok := Int64Val(x)
		u, uok := Uint64Val(x)
		fmt.Fprintf(&b, " i=%d,%v u=%d,%v bits=%d bytes=%x", i, iok, u, uok, BitLen(x), Bytes(x))
	}
	if k := x.Kind(); k == Int || k == Float {
		f32, e32 := Float32Val(x)
		f64, e64 := Float64Val(x)
		fmt.Fprintf(&b, " sign=%d f32=%v,%v f64=%v,%v", Sign(x), math.Float32bits(f32), e32, math.Float64bits(f64), e64)
		fmt.Fprintf(&b, " num=%s den=%s toint=%s tofloat=%s",
			Num(x).ExactString(), Denom(x).ExactString(), ToInt(x).ExactString(), ToFloat(x).ExactString())
		fmt.Fprintf(&b, " real=%s imag=%s", Real(x).ExactString(), Imag(x).ExactString())
	}
	return b.String()
}

func gdescribe(x gc.Value) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s", x.Kind(), x.String(), x.ExactString())
	switch x.Kind() {
	case gc.Bool:
		fmt.Fprintf(&b, " %v", gc.BoolVal(x))
	case gc.String:
		fmt.Fprintf(&b, " %q %d", gc.StringVal(x), gc.StringLen(x))
	case gc.Int:
		i, iok := gc.Int64Val(x)
		u, uok := gc.Uint64Val(x)
		fmt.Fprintf(&b, " i=%d,%v u=%d,%v bits=%d bytes=%x", i, iok, u, uok, gc.BitLen(x), gc.Bytes(x))
	}
	if k := x.Kind(); k == gc.Int || k == gc.Float {
		f32, e32 := gc.Float32Val(x)
		f64, e64 := gc.Float64Val(x)
		fmt.Fprintf(&b, " sign=%d f32=%v,%v f64=%v,%v", gc.Sign(x), math.Float32bits(f32), e32, math.Float64bits(f64), e64)
		fmt.Fprintf(&b, " num=%s den=%s toint=%s tofloat=%s",
			gc.Num(x).ExactString(), gc.Denom(x).ExactString(), gc.ToInt(x).ExactString(), gc.ToFloat(x).ExactString())
		fmt.Fprintf(&b, " real=%s imag=%s", gc.Real(x).ExactString(), gc.Imag(x).ExactString())
	}
	return b.String()
}

func check(t *testing.T, what string, p pair) {
	t.Helper()
	if got, want := describe(p.x), gdescribe(p.g); got != want {
		t.Errorf("%s:\n got %s\nwant %s", what, got, want)
	}
}

func lit(t *testing.T, s string, tok token.Token) pair {
	t.Helper()
	return pair{MakeFromLiteral(s, tok, 0), gc.MakeFromLiteral(s, gotoken.Token(tok), 0)}
}

// Literals from go/constant's own tests, and more.
var (
	intLits = []string{
		"0_123", "0123_456", "1_234", "1_234_567", "0X_0", "0X_1234", "0X_CAFE_f00d", "0o0", "0o1234",
		"0o01234567", "0O0", "0O1234", "0O01234567", "0o_0", "0o_1234", "0o0123_4567", "0O_0", "0O_1234",
		"0O0123_4567", "0b0", "0b1011", "0b00101101", "0B0", "0B1011", "0B00101101", "0b_0", "0b10_11",
		"0b_0010_1101", "0", "7", "9223372036854775807", "9223372036854775808", "18446744073709551615",
		"18446744073709551616", "0x8000000000000000", "0xffffffffffffffffffffffffffffffffffffffff",
		"123456789012345678901234567890123456789012345678901234567890", "08", "0x", "0b2", "1__2", "_1",
		"1_", "0x_", "0_", "00", "0o", "0o_", "0b1_", "0xg", "",
	}
	floatLits = []string{
		"1_2_3.", "0_123.", "0_0e0", "1_2_3e0", "0_123e0", "0e-0_0", "1_2_3E+0", "0123E1_2_3", "0.e+1",
		"123.E-1_0", "01_23.e123", ".0e-1", ".123E+10", ".0123E123", "1_2_3.123", "0123.01_23",
		"1e-1000000000", "1e+1000000000", "6e5518446744", "-6e5518446744", "0x0.p+0", "0Xdeadcafe.p-10",
		"0x1234.P84", "0x.1p-0", "0X.deadcafep4", "0x.1234P+12", "0x0p0", "0Xdeadcafep+1", "0x1234P-10",
		"0x0.0p0", "0Xdead.cafep+1", "0x12.34P-10", "0Xdead_cafep+1", "0x_1234P-10", "0X_dead_cafe.p-10",
		"0x12_34.P1_2_3", "1.5", "0.1", "1e308", "1.7976931348623157e308", "1.7976931348623159e308",
		"4.9e-324", "2.5e-324", "2.4703282292062327e-324", "1e-400", "1e-5000", "1e5000",
		"3.14159265358979323846264338327950288419716939937510582097494459", "1e1233", "1e1234", "1e-1233",
		"1e-1234", "123456789e-1240", "0x1p4095", "0x1p4096", "0x1p-4095", "0x1p-4096", "0x1.8p-1074",
		"1e+", "1e", "1.2.3", "e5", ".", "1p5", "0x1e5", "1_.5", "1._5", "0x1.p", "1e1_", "1e_1",
		"3.4028235e38", "3.4028236e38", "1.401298464324817e-45", "7e-46", "1e-46", "+5", "-0", "-0.0",
	}
	charLits   = []string{`'a'`, `'\n'`, `'\x41'`, `'\u1234'`, `'\U0010FFFF'`, `'ä'`, `'\''`, `'ab'`, `''`, `'`}
	stringLits = []string{`"abc"`, "`raw\\n`", `"\u00e4\t\x00"`, `"bad`, `""`,
		`"` + strings.Repeat("x", 100) + `"`, `"` + strings.Repeat("ä", 80) + `"`}
)

func TestLiterals(t *testing.T) {
	for _, s := range intLits {
		check(t, "INT "+s, lit(t, s, token.INT))
	}
	for _, s := range floatLits {
		check(t, "FLOAT "+s, lit(t, s, token.FLOAT))
	}
	for _, s := range charLits {
		check(t, "CHAR "+s, lit(t, s, token.CHAR))
	}
	for _, s := range stringLits {
		check(t, "STRING "+s, lit(t, s, token.STRING))
	}
	// Imaginary literals are not constants in Plutonium.
	if v := MakeFromLiteral("1i", token.IMAG, 0); v.Kind() != Unknown {
		t.Errorf("1i: %s", describe(v))
	}
}

func TestMakers(t *testing.T) {
	check(t, "unknown", pair{MakeUnknown(), gc.MakeUnknown()})
	check(t, "true", pair{MakeBool(true), gc.MakeBool(true)})
	check(t, "false", pair{MakeBool(false), gc.MakeBool(false)})
	check(t, "string", pair{MakeString("héllo"), gc.MakeString("héllo")})
	for _, x := range []int64{0, 1, -1, math.MaxInt64, math.MinInt64, 1 << 40} {
		check(t, fmt.Sprint("int64 ", x), pair{MakeInt64(x), gc.MakeInt64(x)})
	}
	for _, x := range []uint64{0, 1, 1<<63 - 1, 1 << 63, math.MaxUint64} {
		check(t, fmt.Sprint("uint64 ", x), pair{MakeUint64(x), gc.MakeUint64(x)})
	}
	for _, x := range []float64{0, math.Copysign(0, -1), 1, -1.5, 0.1, math.MaxFloat64, math.SmallestNonzeroFloat64,
		1e-310, math.Inf(1), math.NaN(), 0x1p-1022, 0x1p1023, 0x1p-1074 * 3, math.Ldexp(1, 4095), math.Ldexp(1, -4096)} {
		check(t, fmt.Sprint("float64 ", x), pair{MakeFloat64(x), gc.MakeFloat64(x)})
	}
	for _, b := range [][]byte{nil, {0}, {1}, {0, 0, 1}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}} {
		check(t, fmt.Sprintf("bytes %x", b), pair{MakeFromBytes(b), gc.MakeFromBytes(b)})
	}
	if Kind(42).String() != "Kind(42)" || Float.String() != gc.Float.String() {
		t.Error("Kind.String")
	}
}

// gen produces random constants in both packages.
type gen struct {
	r *rand.Rand
}

func (g *gen) digits(alphabet string, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[g.r.Intn(len(alphabet))]
	}
	return string(b)
}

func (g *gen) intLit() string {
	switch g.r.Intn(6) {
	case 0:
		return fmt.Sprint(g.r.Intn(1000))
	case 1:
		return fmt.Sprint(g.r.Uint64())
	case 2:
		return "0x" + g.digits("0123456789abcdef", 1+g.r.Intn(40))
	case 3:
		return "1" + g.digits("0123456789", g.r.Intn(160))
	case 4:
		return "0b1" + g.digits("01", g.r.Intn(130))
	}
	return fmt.Sprint(int64(1) << uint(g.r.Intn(63)))
}

func (g *gen) floatLit() string {
	mant := "1" + g.digits("0123456789", g.r.Intn(25))
	if g.r.Intn(2) == 0 {
		i := 1 + g.r.Intn(len(mant))
		mant = mant[:i] + "." + mant[i:]
	}
	var exp int
	switch g.r.Intn(5) {
	case 0:
		exp = g.r.Intn(41) - 20
	case 1:
		exp = g.r.Intn(700) - 350
	case 2:
		exp = g.r.Intn(2600) - 1300
	case 3:
		exp = g.r.Intn(4000) - 2000
	default:
		return "0x" + g.digits("0123456789abcdef", 1+g.r.Intn(20)) + "p" + fmt.Sprint(g.r.Intn(9000)-4500)
	}
	return mant + "e" + fmt.Sprint(exp)
}

func (g *gen) leaf(t *testing.T) pair {
	switch g.r.Intn(5) {
	case 0, 1:
		return lit(t, g.intLit(), token.INT)
	case 2, 3:
		return lit(t, g.floatLit(), token.FLOAT)
	}
	f := math.Float64frombits(g.r.Uint64())
	for math.IsNaN(f) || math.IsInf(f, 0) {
		f = math.Float64frombits(g.r.Uint64())
	}
	return pair{MakeFloat64(f), gc.MakeFloat64(f)}
}

var binOps = []token.Token{token.ADD, token.SUB, token.MUL, token.QUO}
var intOps = []token.Token{token.QUO_ASSIGN, token.REM, token.AND, token.OR, token.XOR, token.AND_NOT}
var cmpOps = []token.Token{token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ}

// expr builds a random expression of the given depth, checking every
// intermediate result.
func (g *gen) expr(t *testing.T, depth int, trace string) (pair, string) {
	if depth == 0 {
		p := g.leaf(t)
		return p, p.g.ExactString()
	}
	x, xs := g.expr(t, depth-1, trace)
	var p pair
	var s string
	switch g.r.Intn(8) {
	case 0: // unary
		ops := []token.Token{token.ADD, token.SUB}
		if x.g.Kind() == gc.Int {
			ops = append(ops, token.XOR)
		}
		op := ops[g.r.Intn(len(ops))]
		prec := uint([]int{0, 0, 8, 16, 32, 64, 100}[g.r.Intn(7)])
		p = pair{UnaryOp(op, x.x, prec), gc.UnaryOp(gotoken.Token(op), x.g, prec)}
		s = fmt.Sprintf("(%s %s [%d])", op, xs, prec)
	case 1: // shift
		if x.g.Kind() != gc.Int {
			x = pair{ToInt(x.x), gc.ToInt(x.g)}
			if x.g.Kind() != gc.Int {
				return x, "toint(" + xs + ")"
			}
		}
		op := []token.Token{token.SHL, token.SHR}[g.r.Intn(2)]
		n := uint(g.r.Intn(200))
		p = pair{Shift(x.x, op, n), gc.Shift(x.g, gotoken.Token(op), n)}
		s = fmt.Sprintf("(%s %s %d)", xs, op, n)
	case 2: // conversion
		if g.r.Intn(2) == 0 {
			p = pair{ToInt(x.x), gc.ToInt(x.g)}
			s = "toint(" + xs + ")"
		} else {
			p = pair{ToFloat(x.x), gc.ToFloat(x.g)}
			s = "tofloat(" + xs + ")"
		}
	default:
		y, ys := g.expr(t, depth-1, trace)
		for _, op := range cmpOps {
			if got, want := Compare(x.x, op, y.x), gc.Compare(x.g, gotoken.Token(op), y.g); got != want {
				t.Errorf("%s %s %s = %v, want %v", xs, op, ys, got, want)
			}
		}
		ops := binOps
		if x.g.Kind() == gc.Int && y.g.Kind() == gc.Int {
			ops = append(append([]token.Token(nil), binOps...), intOps...)
		}
		op := ops[g.r.Intn(len(ops))]
		if (op == token.QUO || op == token.QUO_ASSIGN || op == token.REM) && gc.Sign(y.g) == 0 {
			return x, xs
		}
		if x.g.Kind() == gc.Unknown || y.g.Kind() == gc.Unknown {
			return x, xs
		}
		p = pair{BinaryOp(x.x, op, y.x), gc.BinaryOp(x.g, gotoken.Token(op), y.g)}
		s = fmt.Sprintf("(%s %s %s)", xs, op, ys)
	}
	if got, want := describe(p.x), gdescribe(p.g); got != want {
		t.Fatalf("%s:\n got %s\nwant %s", s, got, want)
	}
	return p, s
}

// TestDifferential evaluates random constant expressions with this
// package and with go/constant and compares every result.
func TestDifferential(t *testing.T) {
	n := 10000
	if testing.Short() {
		n = 300
	}
	g := gen{rand.New(rand.NewSource(1))}
	for i := 0; i < n; i++ {
		g.expr(t, 1+g.r.Intn(4), "")
	}
}

// TestBigFloats combines Float values held as 512-bit floats (those too
// large or small for fractions) in every sign combination, with zeros,
// equal magnitudes and integers, so that each path of the float arithmetic
// runs.
func TestBigFloats(t *testing.T) {
	var vals []pair
	for _, s := range []string{"1e5000", "1.5e5000", "1e-5000", "3e-5000", "7e1300", "0x1p5000", "0x1.8p5000"} {
		p := lit(t, s, token.FLOAT)
		vals = append(vals, p, pair{UnaryOp(token.SUB, p.x, 0), gc.UnaryOp(gotoken.SUB, p.g, 0)})
	}
	// Zero, and an integer, as floats (by way of a big float).
	big := vals[0]
	vals = append(vals,
		pair{BinaryOp(big.x, token.SUB, big.x), gc.BinaryOp(big.g, gotoken.SUB, big.g)},
		pair{MakeInt64(3), gc.MakeInt64(3)},
		pair{MakeFloat64(-2.5), gc.MakeFloat64(-2.5)},
		lit(t, "123456789012345678901234567890123456789012345678901234567890", token.INT))
	for i, x := range vals {
		check(t, fmt.Sprint("value ", i), x)
		for j, y := range vals {
			for _, op := range cmpOps {
				if got, want := Compare(x.x, op, y.x), gc.Compare(x.g, gotoken.Token(op), y.g); got != want {
					t.Errorf("%d %s %d = %v, want %v", i, op, j, got, want)
				}
			}
			for _, op := range binOps {
				if op == token.QUO && gc.Sign(y.g) == 0 {
					continue
				}
				what := fmt.Sprintf("%d %s %d", i, op, j)
				check(t, what, pair{BinaryOp(x.x, op, y.x), gc.BinaryOp(x.g, gotoken.Token(op), y.g)})
			}
		}
	}
}

func TestStrings(t *testing.T) {
	x := pair{MakeString("ab"), gc.MakeString("ab")}
	y := pair{MakeString("c\"d"), gc.MakeString("c\"d")}
	sum := pair{BinaryOp(x.x, token.ADD, y.x), gc.BinaryOp(x.g, gotoken.ADD, y.g)}
	check(t, "concat", sum)
	for _, op := range cmpOps {
		if Compare(x.x, op, y.x) != gc.Compare(x.g, gotoken.Token(op), y.g) {
			t.Errorf("string compare %s", op)
		}
	}
	b := pair{MakeBool(true), gc.MakeBool(true)}
	f := pair{MakeBool(false), gc.MakeBool(false)}
	for _, op := range []token.Token{token.LAND, token.LOR} {
		check(t, "bool "+op.String(), pair{BinaryOp(b.x, op, f.x), gc.BinaryOp(b.g, gotoken.Token(op), f.g)})
	}
	check(t, "not", pair{UnaryOp(token.NOT, b.x, 0), gc.UnaryOp(gotoken.NOT, b.g, 0)})
	if Compare(b.x, token.EQL, f.x) || !Compare(b.x, token.NEQ, f.x) {
		t.Error("bool compare")
	}
	u := pair{MakeUnknown(), gc.MakeUnknown()}
	check(t, "unknown op", pair{BinaryOp(u.x, token.ADD, MakeInt64(1)), gc.BinaryOp(u.g, gotoken.ADD, gc.MakeInt64(1))})
	check(t, "unknown shift", pair{Shift(u.x, token.SHL, 3), gc.Shift(u.g, gotoken.SHL, 3)})
	check(t, "unknown unary", pair{UnaryOp(token.SUB, u.x, 0), gc.UnaryOp(gotoken.SUB, u.g, 0)})
	if Compare(u.x, token.EQL, u.x) {
		t.Error("unknown compare")
	}
}

func TestPanics(t *testing.T) {
	for _, f := range []func(){
		func() { BinaryOp(MakeInt64(1), token.QUO, MakeInt64(0)) },
		func() { BinaryOp(MakeString("a"), token.SUB, MakeString("b")) },
		func() { UnaryOp(token.NOT, MakeInt64(1), 0) },
		func() { Shift(MakeFloat64(1.5), token.SHL, 1) },
		func() { Compare(MakeBool(true), token.LSS, MakeBool(false)) },
		func() { BoolVal(MakeInt64(1)) },
		func() { StringVal(MakeInt64(1)) },
		func() { Int64Val(MakeString("x")) },
		func() { MakeFromLiteral("1", token.ADD, 0) },
		func() { MakeFromLiteral("1", token.INT, 1) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("no panic")
				}
			}()
			f()
		}()
	}
}
