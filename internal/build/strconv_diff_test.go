package build_test

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
)

// The differential tests of pluto/strconv's float conversions: ParseFloat
// and AppendFloat run over a large corpus, and every result must equal
// Go's. The corpus has exact decimal forms of floats and of the midpoints
// between neighbouring floats (the hard cases for rounding), those
// midpoints nudged by a digit, random digit strings of every length and
// exponent, hexadecimal floats, underscores in and out of place, special
// values in any case, and strings mutated by random edits. Set
// PLUTO_ATOF_N to change the number of strings (default 60000).

// strconvHarness reads a corpus file of one case per line and prints one
// result per case.
//
//	parse:  each line is a string; the output is "bits64 class bits32 class"
//	        for ParseFloat(s, 64) and ParseFloat(s, 32), with the bits of the
//	        float64 result and the class o(k), s(yntax) or r(ange).
//	format: each line is "bits fmt prec bitSize"; the output is the text
//	        AppendFloat gives.
const strconvHarness = `package main

import (
	"pluto/os"
	"pluto/strconv"
	"unsafe"
)

var obuf [1 << 16]byte
var on int

func put(b []byte) {
	if on+len(b) > len(obuf) {
		flush()
	}
	on += copy(obuf[on:], b)
}

func flush() {
	os.Stdout.Write(obuf[:on])
	on = 0
}

func class(err error) byte {
	switch err {
	case nil:
		return 'o'
	case strconv.ErrSyntax:
		return 's'
	case strconv.ErrRange:
		return 'r'
	}
	return '?'
}

func bitsOf(f float64) uint64 { return *(*uint64)(unsafe.Pointer(&f)) }
func fromBits(b uint64) float64 { return *(*float64)(unsafe.Pointer(&b)) }

func main() {
	if os.NArgs() < 3 {
		os.Exit(2)
	}
	mode := os.Arg(1)
	data, err := os.ReadFile(os.Arg(2))
	if err != nil {
		os.Exit(3)
	}
	b := data.Slice()
	var scratch [4096]byte
	for len(b) > 0 {
		n := 0
		for n < len(b) && b[n] != '\n' {
			n++
		}
		line := unsafe.String(unsafe.SliceData(b), n)
		if n < len(b) {
			n++
		}
		b = b[n:]
		if mode == "parse" {
			f64, e64 := strconv.ParseFloat(line, 64)
			f32, e32 := strconv.ParseFloat(line, 32)
			out := strconv.AppendUint(scratch[:0], bitsOf(f64), 10)
			out = append(out, ' ', class(e64), ' ')
			out = strconv.AppendUint(out, bitsOf(f32), 10)
			out = append(out, ' ', class(e32), '\n')
			put(out)
			continue
		}
		// format: bits fmt prec bitSize
		var fields [4]string
		nf := 0
		start := -1
		for i := 0; i <= len(line); i++ {
			if i == len(line) || line[i] == ' ' {
				if start >= 0 && nf < 4 {
					fields[nf] = line[start:i]
					nf++
				}
				start = -1
			} else if start < 0 {
				start = i
			}
		}
		bits, _ := strconv.ParseUint(fields[0], 10, 64)
		prec, _ := strconv.ParseInt(fields[2], 10, 64)
		size, _ := strconv.ParseInt(fields[3], 10, 64)
		out := strconv.AppendFloat(scratch[:0], fromBits(bits), fields[1][0], int(prec), int(size))
		out = append(out, '\n')
		put(out)
	}
	flush()
}
`

func atofN() int {
	if s := os.Getenv("PLUTO_ATOF_N"); s != "" {
		n, err := strconv.Atoi(s)
		if err == nil && n > 0 {
			return n
		}
	}
	return 60000
}

type atofGen struct {
	r *rand.Rand
}

func (g *atofGen) intn(n int) int { return g.r.IntN(n) }

// midpoint returns the average of a and b, exactly.
func midpoint(a, b float64, prec uint) *big.Float {
	x := new(big.Float).SetPrec(prec).SetFloat64(a)
	y := new(big.Float).SetPrec(prec).SetFloat64(b)
	x.Add(x, y)
	return x.Quo(x, new(big.Float).SetPrec(prec).SetInt64(2))
}

// digitsOf returns the exact decimal digits of m (positive) as a string
// of digits and the decimal exponent dp such that m = 0.digits * 10**dp.
func digitsOf(m *big.Float) (string, int) {
	text := m.Text('e', 1200) // d.ddd...e±xx with 1200 digits: exact for floats
	mant, exp, _ := strings.Cut(text, "e")
	e, _ := strconv.Atoi(exp)
	digits := strings.TrimRight(strings.Replace(mant, ".", "", 1), "0")
	if digits == "" {
		digits = "0"
	}
	return digits, e + 1
}

// spell writes digits (with the decimal point after dp digits) in a
// random notation.
func (g *atofGen) spell(neg bool, digits string, dp int) string {
	var sb strings.Builder
	if neg {
		sb.WriteByte('-')
	} else if g.intn(8) == 0 {
		sb.WriteByte('+')
	}
	switch g.intn(3) {
	case 0: // d.ddde±x
		sb.WriteString(digits[:1])
		if len(digits) > 1 {
			sb.WriteByte('.')
			sb.WriteString(digits[1:])
		}
		sb.WriteString([]string{"e", "E"}[g.intn(2)])
		fmt.Fprintf(&sb, "%+d", dp-1)
	case 1: // all digits then exponent
		sb.WriteString(digits)
		fmt.Fprintf(&sb, "e%d", dp-len(digits))
	default: // plain positional notation when short enough
		switch {
		case dp <= 0 && dp > -40:
			sb.WriteString("0.")
			sb.WriteString(strings.Repeat("0", -dp))
			sb.WriteString(digits)
		case dp >= len(digits) && dp < 330:
			sb.WriteString(digits)
			sb.WriteString(strings.Repeat("0", dp-len(digits)))
		case dp > 0 && dp < len(digits):
			sb.WriteString(digits[:dp])
			sb.WriteByte('.')
			sb.WriteString(digits[dp:])
		default:
			sb.WriteString(digits[:1])
			sb.WriteByte('.')
			sb.WriteString(digits[1:])
			fmt.Fprintf(&sb, "e%d", dp-1)
		}
	}
	return sb.String()
}

func (g *atofGen) randBits64() uint64 {
	switch g.intn(4) {
	case 0: // any pattern
		return g.r.Uint64()
	case 1: // subnormal and tiny
		return g.r.Uint64() & (1<<53 - 1)
	case 2: // near the top
		return 0x7fe0000000000000 + g.r.Uint64()&(1<<52-1)
	default: // exponent in a ordinary range
		return uint64(1023-60+g.intn(120))<<52 | g.r.Uint64()&(1<<52-1)
	}
}

func (g *atofGen) randFloat() float64 {
	for {
		f := math.Float64frombits(g.randBits64() &^ (1 << 63))
		if !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f
		}
	}
}

func (g *atofGen) randFloat32() float32 {
	for {
		bits := g.r.Uint32() &^ (1 << 31)
		switch g.intn(3) {
		case 0:
			bits &= 1<<23 - 1
		case 1:
			bits = uint32(127-40+g.intn(80))<<23 | bits&(1<<23-1)
		}
		f := math.Float32frombits(bits)
		if !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0) {
			return f
		}
	}
}

// hard returns strings around the midpoint of neighbouring floats.
func (g *atofGen) hard(dst []string) []string {
	var a, b float64
	if g.intn(3) == 0 {
		x := g.randFloat32()
		y := math.Nextafter32(x, float32(math.Inf(1)))
		a, b = float64(x), float64(y)
	} else {
		a = g.randFloat()
		b = math.Nextafter(a, math.Inf(1))
	}
	if math.IsInf(b, 0) {
		return dst
	}
	digits, dp := digitsOf(midpoint(a, b, 2400))
	neg := g.intn(2) == 0
	dst = append(dst, g.spell(neg, digits, dp))
	// A little above and below the midpoint.
	dst = append(dst, g.spell(neg, digits+"1", dp))
	dst = append(dst, g.spell(neg, digits+strings.Repeat("0", g.intn(40))+"1", dp))
	last := digits[len(digits)-1]
	if last > '0' {
		below := digits[:len(digits)-1] + string(last-1) + strings.Repeat("9", g.intn(40)+1)
		dst = append(dst, g.spell(neg, below, dp))
	}
	// Truncated to 19, 20 and 30 digits, where the fast paths give way.
	for _, n := range []int{17, 19, 20, 30} {
		if len(digits) > n {
			dst = append(dst, g.spell(neg, digits[:n], dp))
			dst = append(dst, g.spell(neg, digits[:n]+"1", dp))
		}
	}
	return dst
}

func (g *atofGen) randDigits() string {
	n := 1 + g.intn(40)
	if g.intn(10) == 0 {
		n = 1 + g.intn(800)
	}
	var sb strings.Builder
	if g.intn(2) == 0 {
		sb.WriteByte('-')
	}
	point := g.intn(n + 1)
	for i := 0; i < n; i++ {
		if i == point && g.intn(5) != 0 {
			sb.WriteByte('.')
		}
		d := byte('0' + g.intn(10))
		if g.intn(6) == 0 {
			d = '0'
		}
		sb.WriteByte(d)
	}
	if g.intn(3) != 0 {
		e := g.intn(700) - 350
		if g.intn(2) == 0 {
			e = g.intn(80) - 40
		}
		// "e+-5" is a syntax error, which the corpus wants too.
		fmt.Fprintf(&sb, "%s%d", []string{"e", "E", "e+"}[g.intn(3)], e)
	}
	return sb.String()
}

func (g *atofGen) hexFloat() string {
	var sb strings.Builder
	if g.intn(2) == 0 {
		sb.WriteByte('-')
	}
	sb.WriteString([]string{"0x", "0X"}[g.intn(2)])
	n := 1 + g.intn(24)
	point := g.intn(n + 1)
	const digits = "0123456789abcdefABCDEF"
	for i := 0; i < n; i++ {
		if i == point {
			sb.WriteByte('.')
		}
		if g.intn(8) == 0 {
			sb.WriteByte('0')
		} else {
			sb.WriteByte(digits[g.intn(len(digits))])
		}
	}
	if g.intn(12) != 0 {
		e := g.intn(2300) - 1150
		if g.intn(2) == 0 {
			e = g.intn(200) - 100
		}
		fmt.Fprintf(&sb, "%s%d", []string{"p", "P", "p+"}[g.intn(3)], e)
	}
	return sb.String()
}

func (g *atofGen) formatted() string {
	f := g.randFloat()
	if g.intn(2) == 0 {
		f = -f
	}
	bits := 64
	if g.intn(3) == 0 {
		bits = 32
		f = float64(g.randFloat32())
	}
	verb := "eEfgGxX"[g.intn(7)]
	prec := -1
	if g.intn(2) == 0 {
		prec = g.intn(25)
	}
	s := strconv.FormatFloat(f, verb, prec, bits)
	if verb == 'f' && len(s) > 400 {
		s = s[:400]
	}
	return s
}

func (g *atofGen) underscores(s string) string {
	// Insert underscores at random places: some give valid numbers.
	b := []byte(s)
	for k := 1 + g.intn(3); k > 0; k-- {
		i := g.intn(len(b) + 1)
		b = append(b[:i], append([]byte{'_'}, b[i:]...)...)
	}
	return string(b)
}

func (g *atofGen) mutate(s string) string {
	if s == "" {
		return "x"
	}
	b := []byte(s)
	for k := 1 + g.intn(2); k > 0 && len(b) > 0; k-- {
		i := g.intn(len(b))
		switch g.intn(4) {
		case 0:
			b = append(b[:i], b[i+1:]...)
		case 1:
			c := byte("0123456789.eE+-_xXpPaAfFiInN \t"[g.intn(30)])
			b[i] = c
		case 2:
			c := byte("0123456789.eE+-_xXpP"[g.intn(20)])
			b = append(b[:i], append([]byte{c}, b[i:]...)...)
		default:
			b = b[:i]
		}
	}
	return string(b)
}

var atofFixed = []string{
	"", "+", "-", ".", "+.", "-.", "e5", ".e5", "1e", "1e+", "1e-", "1e+-5", "1.e5", ".5", "5.", "-.5e-1",
	"0", "-0", "+0", "0.0", "0e0", "00", "000.000", "0x", "0X", "0x.", "0x.p1", "0x1", "0x1p", "0x1p+", "0x.1p0",
	"0x1.p0", "0x.8p1", "0x_1p0", "0x1_p0", "0x1p_0", "0x1p0_", "0x1p-1_0", "0x1_0p0", "0x_p0",
	"1_0", "1__0", "_1", "1_", "1_.5", "1._5", "1.5_", "1e_5", "1_e5", "1e5_", "1e1_0", "0_1", "0b1", "0o7", "07",
	"inf", "Inf", "INF", "+inf", "-Inf", "infinity", "Infinity", "-INFINITY", "infinit", "infin", "infi", "in", "i",
	"nan", "NaN", "NAN", "-nan", "+nan", "nanx", "nan ", "nann", "na", "n",
	"infx", "infinityx", " 1", "1 ", "1\n", "\t1", "1,5", "1.5.5", "1e5e5", "--1", "+-1", "0x1p1p1", "0x1.2.3p0",
	"1e400", "-1e400", "1e-400", "-1e-400", "1e309", "1e308", "1.7976931348623157e308", "1.7976931348623159e308",
	"1.797693134862315807e308", "1.797693134862315708145274237317043567981e+308", "1.7976931348623158e308",
	"4.9e-324", "2.4703282292062327e-324", "2.4703282292062328e-324", "2.2250738585072011e-308", "2.2250738585072014e-308",
	"3.4028234663852886e38", "3.4028235677973366e38", "3.4028235677973362e38", "1.401298464324817e-45", "7.006492321624085e-46",
	"7.0064923216240854e-46", "1e1000000", "1e-1000000", "1e99999999999999999999", "0e99999999999999999999",
	"0x1p1023", "0x1p1024", "0x1.fffffffffffffp1023", "0x1.fffffffffffff8p1023", "0x1.fffffffffffff7p1023", "0x1p-1074", "0x1p-1075",
	"0x1.8p-1075", "0x1.0000000000001p-1075", "0x1p-1076", "0x0p0", "0x0.0p-99999", "0x1p99999", "0xFFFFFFFFFFFFFFFFFFp0",
	"9007199254740993", "9007199254740992.5", "9007199254740993.0000000000000001", "18014398509481985", "16777217", "16777216.5",
	"1e22", "1e23", "9e22", "8.41e21", "5e-324", "2e-324", "4e-324", "123456789012345678901234567890", "0.000000000000000000000000000001",
	"1" + strings.Repeat("0", 400), "0." + strings.Repeat("0", 400) + "1", strings.Repeat("9", 800), "0." + strings.Repeat("9", 800),
}

func atofCorpus(n int) []string {
	g := &atofGen{r: rand.New(rand.NewPCG(1, 2))}
	corpus := append([]string(nil), atofFixed...)
	for len(corpus) < n {
		switch k := g.intn(100); {
		case k < 22:
			corpus = g.hard(corpus)
		case k < 40:
			corpus = append(corpus, g.randDigits())
		case k < 55:
			corpus = append(corpus, g.formatted())
		case k < 65:
			corpus = append(corpus, g.hexFloat())
		case k < 73:
			s := g.randDigits()
			if g.intn(2) == 0 {
				s = g.hexFloat()
			}
			corpus = append(corpus, g.underscores(s))
		case k < 78:
			corpus = append(corpus, g.mutate(atofFixed[g.intn(len(atofFixed))]))
		case k < 92:
			s := g.formatted()
			if g.intn(3) == 0 {
				s = g.randDigits()
			}
			corpus = append(corpus, g.mutate(s))
		default:
			w := []string{"inf", "infinity", "nan"}[g.intn(3)]
			w = []string{w, strings.ToUpper(w), strings.ToUpper(w[:1]) + w[1:]}[g.intn(3)]
			corpus = append(corpus, []string{"", "+", "-"}[g.intn(3)]+w+[]string{"", "", "x", "inity", "1"}[g.intn(5)])
		}
	}
	// A newline would end a line of the corpus file.
	for i, s := range corpus {
		corpus[i] = strings.ReplaceAll(s, "\n", " ")
	}
	return corpus
}

func goParseLine(s string) string {
	line := func(bitSize int) string {
		f, err := strconv.ParseFloat(s, bitSize)
		c := byte('o')
		if err != nil {
			ne := err.(*strconv.NumError)
			switch ne.Err {
			case strconv.ErrSyntax:
				c = 's'
			case strconv.ErrRange:
				c = 'r'
			default:
				c = '?'
			}
		}
		return fmt.Sprintf("%d %c", math.Float64bits(f), c)
	}
	return line(64) + " " + line(32)
}

// buildStrconvHarness compiles the harness and returns the binary's path.
func buildStrconvHarness(t *testing.T, optLevel int) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(strconvHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "harness")
	cfg := &build.Config{Dir: dir, Output: out, Stdlib: plutonium.Stdlib(), OptLevel: optLevel}
	if err := build.Build(cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	return out
}

func runHarness(t *testing.T, bin, mode string, lines []string) []string {
	t.Helper()
	in := filepath.Join(t.TempDir(), "corpus")
	if err := os.WriteFile(in, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, mode, in)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("harness: %v\n%s", err, stderr.String())
	}
	got := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(got) != len(lines) {
		t.Fatalf("harness printed %d lines for %d cases\n%s", len(got), len(lines), stderr.String())
	}
	return got
}

// TestParseFloatDifferential compares pluto/strconv.ParseFloat with Go's
// over the corpus, for float64 and float32 results.
func TestParseFloatDifferential(t *testing.T) {
	n := atofN()
	if testing.Short() && n > 5000 {
		n = 5000
	}
	corpus := atofCorpus(n)
	for _, level := range []int{0, 2} {
		count := len(corpus)
		if level == 0 {
			count = min(count, 5000) // -O0 is slow; the code is the same
		}
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			bin := buildStrconvHarness(t, level)
			cases := corpus[:count]
			got := runHarness(t, bin, "parse", cases)
			bad := 0
			for i, s := range cases {
				if want := goParseLine(s); got[i] != want {
					bad++
					if bad <= 20 {
						t.Errorf("ParseFloat(%.200q): got %q, want %q", s, got[i], want)
					}
				}
			}
			if bad > 0 {
				t.Errorf("%d of %d cases differ", bad, len(cases))
			}
		})
	}
}

func formatCorpus(n int) (lines, want []string) {
	g := &atofGen{r: rand.New(rand.NewPCG(3, 4))}
	for len(lines) < n {
		bitSize := 64
		var f float64
		if g.intn(3) == 0 {
			bitSize = 32
			f = float64(g.randFloat32())
		} else {
			f = g.randFloat()
		}
		if g.intn(2) == 0 {
			f = -f
		}
		switch g.intn(40) {
		case 0:
			f = 0
		case 1:
			f = math.Copysign(0, -1)
		case 2:
			f = math.Inf(1)
		case 3:
			f = math.Inf(-1)
		case 4:
			f = math.NaN()
		case 5:
			f = float64(g.intn(1000000))
		case 6:
			f = float64(g.intn(1000)) / 8
		}
		verb := "eEfgGbxX"[g.intn(8)]
		prec := -1
		if g.intn(3) != 0 {
			prec = g.intn(25)
		}
		if g.intn(30) == 0 {
			prec = g.intn(320)
		}
		lines = append(lines, fmt.Sprintf("%d %c %d %d", math.Float64bits(f), verb, prec, bitSize))
		want = append(want, string(strconv.AppendFloat(nil, f, verb, prec, bitSize)))
	}
	return lines, want
}

// TestAppendFloatDifferential compares pluto/strconv.AppendFloat with
// Go's over random floats, formats, precisions and bit sizes.
func TestAppendFloatDifferential(t *testing.T) {
	n := atofN()
	if testing.Short() && n > 5000 {
		n = 5000
	}
	lines, want := formatCorpus(n)
	bin := buildStrconvHarness(t, 2)
	got := runHarness(t, bin, "format", lines)
	bad := 0
	for i := range lines {
		if got[i] != want[i] {
			bad++
			if bad <= 20 {
				t.Errorf("AppendFloat(%s): got %q, want %q", lines[i], got[i], want[i])
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d of %d cases differ", bad, len(lines))
	}
}
