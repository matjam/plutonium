// Command constdemo exercises the constant package (internal/constant):
// literals of every kind, the makers, the accessors, and a few thousand
// random expressions evaluated with BinaryOp, UnaryOp, Shift, Compare and the
// conversions. For each value it prints everything the API can tell about it.
// go2pu translates it with the package, and TestDifferential requires the
// Go and the Plutonium build to print the same text at -O0 and -O2.
//
// The random numbers come from the program's own generator, so that both
// builds see the same sequence.
package main

import (
	"fmt"
	"math"

	"github.com/matjam/plutonium/internal/constant"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// rng is a 64-bit linear congruential generator.
type rng struct{ s uint64 }

func (r *rng) next() uint64 {
	r.s = r.s*6364136223846793005 + 1442695040888963407
	return r.s >> 11
}

func (r *rng) intn(n int) int { return int(r.next() % uint64(n)) }

func (r *rng) digits(alphabet string, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[r.intn(len(alphabet))]
	}
	return string(b)
}

func (r *rng) intLit() string {
	switch r.intn(6) {
	case 0:
		return fmt.Sprint(r.intn(1000))
	case 1:
		return fmt.Sprint(r.next())
	case 2:
		return "0x" + r.digits("0123456789abcdef", 1+r.intn(40))
	case 3:
		return "1" + r.digits("0123456789", r.intn(160))
	case 4:
		return "0b1" + r.digits("01", r.intn(130))
	}
	return fmt.Sprint(int64(1) << uint(r.intn(63)))
}

func (r *rng) floatLit() string {
	mant := "1" + r.digits("0123456789", r.intn(25))
	if r.intn(2) == 0 {
		i := 1 + r.intn(len(mant))
		mant = mant[:i] + "." + mant[i:]
	}
	var exp int
	switch r.intn(5) {
	case 0:
		exp = r.intn(41) - 20
	case 1:
		exp = r.intn(700) - 350
	case 2:
		exp = r.intn(2600) - 1300
	case 3:
		exp = r.intn(4000) - 2000
	default:
		return "0x" + r.digits("0123456789abcdef", 1+r.intn(20)) + "p" + fmt.Sprint(r.intn(9000)-4500)
	}
	return mant + "e" + fmt.Sprint(exp)
}

func (r *rng) leaf() constant.Value {
	switch r.intn(5) {
	case 0, 1:
		return constant.MakeFromLiteral(r.intLit(), token.INT, 0)
	case 2, 3:
		return constant.MakeFromLiteral(r.floatLit(), token.FLOAT, 0)
	}
	f := math.Float64frombits(r.next() << 11)
	for math.IsNaN(f) || math.IsInf(f, 0) {
		f = math.Float64frombits(r.next() << 11)
	}
	return constant.MakeFloat64(f)
}

var binOps = []token.Token{token.ADD, token.SUB, token.MUL, token.QUO}
var intOps = []token.Token{token.QUO_ASSIGN, token.REM, token.AND, token.OR, token.XOR, token.AND_NOT}
var cmpOps = []token.Token{token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ}

// describe prints everything the API tells about x.
func describe(label string, x constant.Value) {
	s := fmt.Sprintf("%s: %s %s %s", label, x.Kind(), x.String(), x.ExactString())
	switch x.Kind() {
	case constant.Bool:
		s += fmt.Sprintf(" %v", constant.BoolVal(x))
	case constant.String:
		s += fmt.Sprintf(" %q %d", constant.StringVal(x), constant.StringLen(x))
	case constant.Int:
		i, iok := constant.Int64Val(x)
		u, uok := constant.Uint64Val(x)
		s += fmt.Sprintf(" i=%d,%v u=%d,%v bits=%d bytes=%x", i, iok, u, uok, constant.BitLen(x), constant.Bytes(x))
	}
	if k := x.Kind(); k == constant.Int || k == constant.Float {
		f32, e32 := constant.Float32Val(x)
		f64, e64 := constant.Float64Val(x)
		s += fmt.Sprintf(" sign=%d f32=%v,%v f64=%v,%v", constant.Sign(x), math.Float32bits(f32), e32, math.Float64bits(f64), e64)
		s += fmt.Sprintf(" num=%s den=%s toint=%s tofloat=%s",
			constant.Num(x).ExactString(), constant.Denom(x).ExactString(), constant.ToInt(x).ExactString(), constant.ToFloat(x).ExactString())
		s += fmt.Sprintf(" real=%s imag=%s", constant.Real(x).ExactString(), constant.Imag(x).ExactString())
	}
	fmt.Println(s)
}

func compare(label string, x, y constant.Value) {
	s := label + ":"
	for _, op := range cmpOps {
		s += fmt.Sprintf(" %v", constant.Compare(x, op, y))
	}
	fmt.Println(s)
}

// expr builds a random expression of the given depth and describes every
// intermediate result.
func (r *rng) expr(depth int, label string) constant.Value {
	if depth == 0 {
		p := r.leaf()
		describe(label, p)
		return p
	}
	x := r.expr(depth-1, label+"l")
	var p constant.Value
	switch r.intn(8) {
	case 0: // unary
		ops := []token.Token{token.ADD, token.SUB}
		if x.Kind() == constant.Int {
			ops = append(ops, token.XOR)
		}
		op := ops[r.intn(len(ops))]
		precs := []int{0, 0, 8, 16, 32, 64, 100}
		prec := uint(precs[r.intn(len(precs))])
		p = constant.UnaryOp(op, x, prec)
		label += fmt.Sprintf(" unary %s [%d]", op, prec)
	case 1: // shift
		if x.Kind() != constant.Int {
			x = constant.ToInt(x)
			if x.Kind() != constant.Int {
				return x
			}
		}
		ops := []token.Token{token.SHL, token.SHR}
		op := ops[r.intn(2)]
		n := uint(r.intn(200))
		p = constant.Shift(x, op, n)
		label += fmt.Sprintf(" shift %s %d", op, n)
	case 2: // conversion
		if r.intn(2) == 0 {
			p = constant.ToInt(x)
			label += " toint"
		} else {
			p = constant.ToFloat(x)
			label += " tofloat"
		}
	default:
		y := r.expr(depth-1, label+"r")
		compare(label+" compare", x, y)
		ops := binOps
		if x.Kind() == constant.Int && y.Kind() == constant.Int {
			ops = append(append([]token.Token(nil), binOps...), intOps...)
		}
		op := ops[r.intn(len(ops))]
		if (op == token.QUO || op == token.QUO_ASSIGN || op == token.REM) && constant.Sign(y) == 0 {
			return x
		}
		if x.Kind() == constant.Unknown || y.Kind() == constant.Unknown {
			return x
		}
		p = constant.BinaryOp(x, op, y)
		label += fmt.Sprintf(" binary %s", op)
	}
	describe(label, p)
	return p
}

var intLits = []string{
	"0_123", "0123_456", "1_234", "1_234_567", "0X_0", "0X_1234", "0X_CAFE_f00d", "0o0", "0o1234",
	"0o01234567", "0O0", "0O1234", "0O01234567", "0o_0", "0o_1234", "0o0123_4567", "0O_0", "0O_1234",
	"0O0123_4567", "0b0", "0b1011", "0b00101101", "0B0", "0B1011", "0B00101101", "0b_0", "0b10_11",
	"0b_0010_1101", "0", "7", "9223372036854775807", "9223372036854775808", "18446744073709551615",
	"18446744073709551616", "0x8000000000000000", "0xffffffffffffffffffffffffffffffffffffffff",
	"123456789012345678901234567890123456789012345678901234567890", "08", "0x", "0b2", "1__2", "_1",
	"1_", "0x_", "0_", "00", "0o", "0o_", "0b1_", "0xg", "",
}

var floatLits = []string{
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

var charLits = []string{`'a'`, `'\n'`, `'\x41'`, `'ሴ'`, `'\U0010FFFF'`, `'ä'`, `'\''`, `'ab'`, `''`, `'`}

var stringLits = []string{`"abc"`, "`raw\\n`", `"ä\t\x00"`, `"bad`, `""`,
	`"` + repeat("x", 100) + `"`, `"` + repeat("ä", 80) + `"`}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

func literals() {
	for _, s := range intLits {
		describe("INT "+s, constant.MakeFromLiteral(s, token.INT, 0))
	}
	for _, s := range floatLits {
		describe("FLOAT "+s, constant.MakeFromLiteral(s, token.FLOAT, 0))
	}
	for _, s := range charLits {
		describe("CHAR "+s, constant.MakeFromLiteral(s, token.CHAR, 0))
	}
	for _, s := range stringLits {
		describe("STRING "+s, constant.MakeFromLiteral(s, token.STRING, 0))
	}
	describe("IMAG 1i", constant.MakeFromLiteral("1i", token.IMAG, 0))
}

func makers() {
	describe("unknown", constant.MakeUnknown())
	describe("true", constant.MakeBool(true))
	describe("false", constant.MakeBool(false))
	describe("string", constant.MakeString("héllo"))
	for _, x := range []int64{0, 1, -1, math.MaxInt64, math.MinInt64, 1 << 40} {
		describe(fmt.Sprint("int64 ", x), constant.MakeInt64(x))
	}
	for _, x := range []uint64{0, 1, 1<<63 - 1, 1 << 63, math.MaxUint64} {
		describe(fmt.Sprint("uint64 ", x), constant.MakeUint64(x))
	}
	for _, x := range []float64{0, math.Copysign(0, -1), 1, -1.5, 0.1, math.MaxFloat64, math.SmallestNonzeroFloat64,
		1e-310, math.Inf(1), math.NaN(), 0x1p-1022, 0x1p1023, 0x1p-1074 * 3, math.Ldexp(1, 4095), math.Ldexp(1, -4096)} {
		describe(fmt.Sprint("float64 ", math.Float64bits(x)), constant.MakeFloat64(x))
	}
	for _, b := range [][]byte{nil, {0}, {1}, {0, 0, 1}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}} {
		describe(fmt.Sprintf("bytes %x", b), constant.MakeFromBytes(b))
	}
	fmt.Println(constant.Kind(42).String(), constant.Float.String())
}

// bigFloats combines values held as 512-bit floats in every sign
// combination, with zero, equal magnitudes and integers.
func bigFloats() {
	var vals []constant.Value
	for _, s := range []string{"1e5000", "1.5e5000", "1e-5000", "3e-5000", "7e1300", "0x1p5000", "0x1.8p5000"} {
		p := constant.MakeFromLiteral(s, token.FLOAT, 0)
		vals = append(vals, p, constant.UnaryOp(token.SUB, p, 0))
	}
	big := vals[0]
	vals = append(vals,
		constant.BinaryOp(big, token.SUB, big),
		constant.MakeInt64(3),
		constant.MakeFloat64(-2.5),
		constant.MakeFromLiteral("123456789012345678901234567890123456789012345678901234567890", token.INT, 0))
	for i, x := range vals {
		describe(fmt.Sprint("big value ", i), x)
		for j, y := range vals {
			compare(fmt.Sprint("big ", i, " ", j), x, y)
			for _, op := range binOps {
				if op == token.QUO && constant.Sign(y) == 0 {
					continue
				}
				describe(fmt.Sprint("big ", i, " ", op, " ", j), constant.BinaryOp(x, op, y))
			}
		}
	}
}

func others() {
	x, y := constant.MakeString("ab"), constant.MakeString("c\"d")
	describe("concat", constant.BinaryOp(x, token.ADD, y))
	compare("strings", x, y)
	b, f := constant.MakeBool(true), constant.MakeBool(false)
	for _, op := range []token.Token{token.LAND, token.LOR} {
		describe("bool "+op.String(), constant.BinaryOp(b, op, f))
	}
	describe("not", constant.UnaryOp(token.NOT, b, 0))
	fmt.Println("bools", constant.Compare(b, token.EQL, f), constant.Compare(b, token.NEQ, f))
	u := constant.MakeUnknown()
	describe("unknown op", constant.BinaryOp(u, token.ADD, constant.MakeInt64(1)))
	describe("unknown shift", constant.Shift(u, token.SHL, 3))
	describe("unknown unary", constant.UnaryOp(token.SUB, u, 0))
	compare("unknown", u, u)
}

func main() {
	literals()
	makers()
	others()
	bigFloats()
	r := &rng{s: 1}
	for i := 0; i < 3000; i++ {
		r.expr(1+r.intn(4), fmt.Sprint("expr ", i))
	}
}
