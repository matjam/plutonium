package build_test

import (
	"bytes"
	"encoding/hex"
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
	"unicode/utf8"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
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
	"pluto/errors"
	"pluto/strconv"
	"pluto/rt"
	"pluto/strings"
	"pluto/vec"
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
	switch {
	case err == nil:
		return 'o'
	case errors.Is(err, strconv.ErrSyntax):
		return 's'
	case errors.Is(err, strconv.ErrRange):
		return 'r'
	}
	return '?'
}

// hexAll appends the hex bytes of b to out, two lowercase digits per byte.
func hexAll[D vec.Appender[byte]](b []byte, out D) {
	const d = "0123456789abcdef"
	for _, c := range b {
		out.Push(d[c>>4])
		out.Push(d[c&0xf])
	}
}

func hexVal(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a'+10), true
	}
	return 0, false
}

var hexBuf [4096]byte

// hexDecode returns the bytes of the hex-encoded string s.
func hexDecode(s string) string {
	n := len(s) / 2
	if n > len(hexBuf) {
		return ""
	}
	for i := 0; i < n; i++ {
		hi, ok1 := hexVal(s[2*i])
		lo, ok2 := hexVal(s[2*i+1])
		if !ok1 || !ok2 {
			return ""
		}
		hexBuf[i] = byte(hi<<4 | lo)
	}
	return unsafe.String(unsafe.SliceData(hexBuf[:]), n)
}

func quoteValue(api int, s string, r rune) string {
	switch api {
	case 0: return strconv.Quote(s)
	case 1: return strconv.QuoteToASCII(s)
	case 2: return strconv.QuoteToGraphic(s)
	case 3: return strconv.QuoteRune(r)
	case 4: return strconv.QuoteRuneToASCII(r)
	case 5: return strconv.QuoteRuneToGraphic(r)
	}
	panic("bad quoting API")
}

func quoteAppend[D vec.Appender[byte]](dst D, api int, s string, r rune) {
	switch api {
	case 0: strconv.AppendQuote(dst, s)
	case 1: strconv.AppendQuoteToASCII(dst, s)
	case 2: strconv.AppendQuoteToGraphic(dst, s)
	case 3: strconv.AppendQuoteRune(dst, r)
	case 4: strconv.AppendQuoteRuneToASCII(dst, r)
	case 5: strconv.AppendQuoteRuneToGraphic(dst, r)
	}
}

func bitsOf(f float64) uint64 { return *(*uint64)(unsafe.Pointer(&f)) }
func fromBits(b uint64) float64 { return *(*float64)(unsafe.Pointer(&b)) }

func main() {
	if os.NArgs() < 3 {
		os.Exit(2)
	}
	mode := os.ArgLossy(1)
	data, err := os.ReadFile(os.ArgLossy(2))
	if err != nil {
		os.Exit(3)
	}
	b := data.Slice()
	var scratch [16384]byte
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
			out := vec.NewFixed(scratch[:])
			strconv.AppendUint(&out, bitsOf(f64), 10)
			out.Append(' ', class(e64), ' ')
			strconv.AppendUint(&out, bitsOf(f32), 10)
			out.Append(' ', class(e32), '\n')
			put(out.Slice())
			continue
		}
		if mode == "quote" {
			i, j := 0, 0
			for i < len(line) && line[i] != ' ' { i++ }
			j = i+1
			for j < len(line) && line[j] != ' ' { j++ }
			api, _ := strconv.Atoi(strings.Slice(line, 0, i))
			capacity, _ := strconv.Atoi(strings.Slice(line, i+1, j))
			payload := strings.SliceFrom(line, j+1)
			var r rune
			if api < 3 { payload = hexDecode(payload) } else {
				n, _ := strconv.ParseInt(payload, 10, 32)
				r = rune(n)
			}
			out := vec.NewFixed(scratch[:])
			if capacity == -1 {
				q := quoteValue(api, payload, r)
				hexAll(unsafe.Slice(unsafe.StringData(q), len(q)), &out)
				out.Push(' ')
				strconv.AppendBool(&out, rt.KnownASCII(q))
			} else if capacity == -2 {
				v := vec.New[byte]()
				v.Append('p', ':', 0xff)
				quoteAppend(&v, api, payload, r)
				hexAll(v.Slice(), &out)
				vec.AppendString(&out, " false")
			} else {
				var mem [8192]byte
				f := vec.NewFixed(mem[:capacity])
				f.Append('p', ':', 0xff)
				quoteAppend(&f, api, payload, r)
				hexAll(f.Slice(), &out)
				out.Push(' ')
				strconv.AppendBool(&out, f.Truncated())
			}
			out.Push(10)
			put(out.Slice())
			continue
		}
		if mode == "unquote" {
			line = hexDecode(line)
			r, e := strconv.Unquote(line)
			out := vec.NewFixed(scratch[:])
			if e != nil {
				out.Append('e', class(e))
			} else {
				out.Push('o')
				hexAll(unsafe.Slice(unsafe.StringData(r), len(r)), &out)
			}
			out.Push(10)
			put(out.Slice())
			continue
		}
		if mode == "prefix" {
			line = hexDecode(line)
			p, e := strconv.QuotedPrefix(line)
			out := vec.NewFixed(scratch[:])
			if e != nil {
				out.Append('e', class(e))
			} else {
				out.Push('o')
				hexAll(unsafe.Slice(unsafe.StringData(p), len(p)), &out)
			}
			out.Push(10)
			put(out.Slice())
			continue
		}
		if mode == "char" {
			line = hexDecode(line)
			v, mb, tail, e := strconv.UnquoteChar(line, '"')
			out := vec.NewFixed(scratch[:])
			if e != nil {
				out.Append('e', class(e))
			} else {
				out.Append('o', ' ')
				strconv.AppendUint(&out, uint64(v), 10)
				out.Push(' ')
				if mb {
					out.Push('1')
				} else {
					out.Push('0')
				}
				out.Push(' ')
				strconv.AppendUint(&out, uint64(len(tail)), 10)
			}
			out.Push(10)
			put(out.Slice())
			continue
		}
		// format: bits fmt prec bitSize
		var fields [4]string
		nf := 0
		start := -1
		for i := 0; i <= len(line); i++ {
			if i == len(line) || line[i] == ' ' {
				if start >= 0 && nf < 4 {
					fields[nf] = strings.Slice(line, start, i)
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
		out := vec.NewFixed(scratch[:])
		strconv.AppendFloat(&out, fromBits(bits), fields[1][0], int(prec), int(size))
		out.Push('\n')
		put(out.Slice())
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
	if err := buildCfg(cfg); err != nil {
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
	testguard.Parallel(t)
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
			testguard.Parallel(t)
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
	testguard.Parallel(t)
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

func errClass(err error) string {
	switch err {
	case strconv.ErrSyntax:
		return "s"
	case strconv.ErrRange:
		return "r"
	}
	return "?"
}

func hexAllGo(b []byte) string {
	const d = "0123456789abcdef"
	var s strings.Builder
	for _, c := range b {
		s.WriteByte(d[c>>4])
		s.WriteByte(d[c&0xf])
	}
	return s.String()
}

// unquotedValid reports whether the double-quoted or backquoted literal lit,
// which Go's Unquote accepts, is valid UTF-8 and decodes to valid UTF-8.
// Plutonium rejects one that is not, as a string literal is rejected
// (section 4.11); Go replaces a raw invalid byte with U+FFFD and returns the
// byte an escape such as \xff names.
func unquotedValid(lit string) bool {
	r, err := strconv.Unquote(lit)
	return err == nil && utf8.ValidString(lit) && utf8.ValidString(r)
}

func goUnquoteLine(s string) string {
	if len(s) == 0 {
		return "es"
	}
	r, err := strconv.Unquote(s)
	if err != nil {
		return "e" + errClass(err)
	}
	if s[0] == '"' || s[0] == '`' {
		if !unquotedValid(s) {
			return "es"
		}
		return "o" + hexAllGo([]byte(r))
	}
	// Single-quoted: Go returns a raw byte for \xNN and octal escapes with
	// value >= 0x80; Plutonium returns the rune's UTF-8 encoding.
	if !utf8.ValidString(r) {
		r = string(rune(r[0]))
	}
	return "o" + hexAllGo([]byte(r))
}

func goPrefixLine(s string) string {
	if len(s) == 0 {
		return "es"
	}
	p, err := strconv.QuotedPrefix(s)
	if err != nil {
		return "e" + errClass(err)
	}
	if (s[0] == '"' || s[0] == '`') && !unquotedValid(p) {
		return "es"
	}
	return "o" + hexAllGo([]byte(p))
}

func goCharLine(s string) string {
	v, mb, tail, err := strconv.UnquoteChar(s, '"')
	if err != nil {
		return "e" + errClass(err)
	}
	m := "0"
	if mb {
		m = "1"
	}
	return fmt.Sprintf("o %d %s %d", v, m, len(tail))
}

func hexEncodeLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, s := range lines {
		out[i] = hex.EncodeToString([]byte(s))
	}
	return out
}

// unquoteCorpus returns full quoted strings, as pluto/strconv.Unquote and
// QuotedPrefix take them. It covers every branch: double, single and backtick
// quotes; known, hex, octal and unicode escapes; raw multibyte runes; invalid
// UTF-8; and unterminated or malformed inputs. Lines are hex-encoded before
// they reach the harness, so they may carry any byte.
func unquoteCorpus() []string {
	return []string{
		`"hello"`,
		`""`,
		`"a\"\xff"`,
		`"\"\xc3\xbf\""`,
		`"a\nb"`,
		`"a\tb"`,
		`"a\vb"`,
		`"a\fb"`,
		`"a\rb"`,
		`"a\\b"`,
		`"a\"b"`,
		`"a' b"`,
		`"a\x41b"`,
		`"a\x00b"`,
		`"a\x7fb"`,
		`"a\x80b"`,
		`"a\xffb"`,
		`"a\u00e9b"`,
		`"a\U0001F600b"`,
		`"\u0041"`,
		`"\377"`,
		`"\101"`,
		`"\0"`,
		`"é"`,
		`"`,
		`"abc"xyz`,
		`x"abc"`,
		`"a\Gb"`,
		`"a\x4"`,
		`"a\xG"`,
		`"a\u12"`,
		`"a\ud800"`,
		`"a\U00110000"`,
		`"a\777"`,
		`"a\`,
		`"a\"b`,
		`"a\3778`,
		`'a'`,
		`''`,
		`'\n'`,
		`'é'`,
		`'\xff'`,
		`'"'`,
		`'\''`,
		`'\\'`,
		`'\x41'`,
		`'\101'`,
		`'\0'`,
		`'ab'`,
		`'a b'`,
		`'a`,
		`'\ud800'`,
		`'\x4'`,
		`'\377'`,
		`"a\ab"`,
		`"a\bb"`,
		"`hello`",
		"`a\\rb`",
		"`a\rb`",
		"`a\\\\b`",
		"`a b`",
		"`a\"b`",
		"`",
		"\"a\nb\"",
		"\"a\xffb\"",
		"`a\xffb`",
	}
}

// charCorpus returns the sequences that follow the opening quote, as
// pluto/strconv.UnquoteChar takes them (the harness calls it with quote '"').
func charCorpus() []string {
	return []string{
		`A`,
		`a`,
		`é`,
		`A0`,
		`é0`,
		`\a`,
		`\b`,
		`\f`,
		`\n`,
		`\r`,
		`\t`,
		`\v`,
		`\\`,
		`\"`,
		`"`,
		`\'`,
		`\x41`,
		`\x00`,
		`\x7f`,
		`\x80`,
		`\xff`,
		`\x4`,
		`\xG`,
		`\u0041`,
		`\u00e9`,
		`\u0000`,
		`\ud800`,
		`\u12`,
		`\U0001F600`,
		`\U00000041`,
		`\U00110000`,
		`\377`,
		`\101`,
		`\0`,
		`\777`,
		`\`,
	}
}

// TestUnquoteDifferential compares pluto/strconv.Unquote with Go's over the
// corpus. A double-quoted or backquoted literal whose unescaped bytes are not
// valid UTF-8 is a syntax error (section 4.11).
func TestUnquoteDifferential(t *testing.T) {
	testguard.Parallel(t)
	corpus := unquoteCorpus()
	hex := hexEncodeLines(corpus)
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			bin := buildStrconvHarness(t, level)
			got := runHarness(t, bin, "unquote", hex)
			bad := 0
			for i, s := range corpus {
				if want := goUnquoteLine(s); got[i] != want {
					bad++
					if bad <= 20 {
						t.Errorf("Unquote(%.200q): got %q, want %q", s, got[i], want)
					}
				}
			}
			if bad > 0 {
				t.Errorf("%d of %d cases differ", bad, len(corpus))
			}
		})
	}
}

// TestQuotedPrefixDifferential compares pluto/strconv.QuotedPrefix with
// Go's over the same corpus. A double-quoted or backquoted prefix whose
// unescaped bytes are not valid UTF-8 is a syntax error (section 4.11).
func TestQuotedPrefixDifferential(t *testing.T) {
	testguard.Parallel(t)
	corpus := unquoteCorpus()
	hex := hexEncodeLines(corpus)
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			bin := buildStrconvHarness(t, level)
			got := runHarness(t, bin, "prefix", hex)
			bad := 0
			for i, s := range corpus {
				if want := goPrefixLine(s); got[i] != want {
					bad++
					if bad <= 20 {
						t.Errorf("QuotedPrefix(%.200q): got %q, want %q", s, got[i], want)
					}
				}
			}
			if bad > 0 {
				t.Errorf("%d of %d cases differ", bad, len(corpus))
			}
		})
	}
}

// TestUnquoteCharDifferential compares pluto/strconv.UnquoteChar with Go's
// over the corpus, always with quote '"'. UnquoteChar is unchanged by the
// UTF-8 rule of section 4.11; it reports the byte or rune as Go does.
func TestUnquoteCharDifferential(t *testing.T) {
	testguard.Parallel(t)
	corpus := charCorpus()
	hex := hexEncodeLines(corpus)
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			bin := buildStrconvHarness(t, level)
			got := runHarness(t, bin, "char", hex)
			bad := 0
			for i, s := range corpus {
				if want := goCharLine(s); got[i] != want {
					bad++
					if bad <= 20 {
						t.Errorf("UnquoteChar(%.200q): got %q, want %q", s, got[i], want)
					}
				}
			}
			if bad > 0 {
				t.Errorf("%d of %d cases differ", bad, len(corpus))
			}
		})
	}
}

// TestQuoteDifferential compares every string and Appender quoting API with
// Go. The Fixed oracle is the byte prefix of Go's full appended result, even
// when that prefix ends inside a UTF-8 rune or escape. Inputs and outputs are
// hex encoded, including malformed bytes supplied through explicit unsafe views.
func TestQuoteDifferential(t *testing.T) {
	testguard.Parallel(t)
	type quoteCase struct{ line, want string }
	var cases []quoteCase
	add := func(api int, input string, value string) {
		ascii := true
		for i := range len(value) {
			ascii = ascii && value[i] < utf8.RuneSelf
		}
		cases = append(cases, quoteCase{fmt.Sprintf("%d -1 %s", api, input), hex.EncodeToString([]byte(value)) + " " + strconv.FormatBool(ascii)})
		full := append([]byte{'p', ':', 0xff}, value...)
		cases = append(cases, quoteCase{fmt.Sprintf("%d -2 %s", api, input), hex.EncodeToString(full) + " false"})
		caps := make(map[int]bool)
		for c := 0; c <= min(len(full)+1, 20); c++ {
			caps[c] = true
		}
		for _, c := range []int{len(full) - 1, len(full), len(full) + 1, 244, 245, 246, 255, 256, 257, 258} {
			if c >= 0 && c <= len(full)+1 {
				caps[c] = true
			}
		}
		// Ordered capacities keep failure reports reproducible.
		for c := 0; c <= len(full)+1; c++ {
			if !caps[c] {
				continue
			}
			cases = append(cases, quoteCase{fmt.Sprintf("%d %d %s", api, c, input), hex.EncodeToString(full[:min(c, len(full))]) + " " + strconv.FormatBool(c < len(full))})
		}
	}
	corpus := []string{"", "plain \"'\\", "\a\b\f\n\r\t\v\x00\x7f", "\u0085\u00ad\u2028\u2029", "é\u00a0😀\ufffd", "\u1680\u2000\u200a\u202f\u205f\u3000", "\u0378\u0600\ufeff\U000e0001\U0010ffff"}
	for c := 0; c < 128; c++ {
		corpus = append(corpus, string(byte(c)))
	}
	for _, b := range [][]byte{{0xff}, {0xc0, 0x80}, {0xc2}, {0xe2, 0x82}, {0xf0, 0x9f, 0x98}, {0xed, 0xa0, 0x80}, {0xf4, 0x90, 0x80, 0x80}, {'a', 0xff, 0xef, 0xbf, 0xbd, 0x80, 'z'}} {
		corpus = append(corpus, string(b))
	}
	for _, n := range []int{62, 63, 64, 65, 244, 245, 246, 254, 255, 256, 257} {
		corpus = append(corpus, strings.Repeat("a", n)+"é😀\u0085\u00a0", strings.Repeat("\u0085😀", n))
	}
	for _, s := range corpus {
		for api, f := range []func(string) string{strconv.Quote, strconv.QuoteToASCII, strconv.QuoteToGraphic} {
			add(api, hex.EncodeToString([]byte(s)), f(s))
		}
	}
	runes := []rune{-2147483648, -1, 0xd7ff, 0xd800, 0xdfff, 0xe000, 0xfffd, 0xffff, 0x10000, 0x1f600, 0x10ffff, 0x110000, 2147483647, 0x85, 0xad, 0xa0, 0x1680, 0x2000, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0x378, 0xe0001}
	for r := rune(0); r < 128; r++ {
		runes = append(runes, r)
	}
	for _, r := range runes {
		for api, f := range []func(rune) string{strconv.QuoteRune, strconv.QuoteRuneToASCII, strconv.QuoteRuneToGraphic} {
			add(api+3, strconv.FormatInt(int64(r), 10), f(r))
		}
	}
	lines := make([]string, len(cases))
	for i, c := range cases {
		lines[i] = c.line
	}
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			got := runHarness(t, buildStrconvHarness(t, level), "quote", lines)
			bad := 0
			for i, c := range cases {
				if got[i] != c.want {
					bad++
					if bad <= 20 {
						t.Errorf("%s: got %q, want %q", c.line, got[i], c.want)
					}
				}
			}
			if bad > 0 {
				t.Errorf("%d of %d quoting cases differ", bad, len(cases))
			}
		})
	}
}
