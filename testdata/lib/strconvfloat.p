package main

// ParseFloat, AppendFloat and NewNumError: the tables of Go 1.27's
// internal/strconv atof_test.go and ftoa_test.go (every case checked by Go's
// own tests), run against pluto/strconv at -O0 and -O2. A failed parse is
// ErrSyntax or ErrRange themselves, where Go returns them in a *NumError.
// internal/build/strconv_diff_test.go compares ParseFloat and AppendFloat
// with Go's over a million random inputs.
//
// The program prints "ok" lines, and each failure with its details.

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/errors"
	"pluto/fmt"
	"pluto/os"
	"pluto/strconv"
	"unsafe"
)

var (
	ErrSyntax = strconv.ErrSyntax
	ErrRange  = strconv.ErrRange
)

var failures int

func errorf(format string, args ...any) {
	failures++
	fmt.Printf(format, args...)
	fmt.Printf("\n")
}

type atofTest struct {
	in  string
	out string
	err error
}

var atoftests = [...]atofTest{
	{"", "0", ErrSyntax},
	{"1", "1", nil},
	{"+1", "1", nil},
	{"1x", "0", ErrSyntax},
	{"1.1.", "0", ErrSyntax},
	{"1e23", "1e+23", nil},
	{"1E23", "1e+23", nil},
	{"100000000000000000000000", "1e+23", nil},
	{"1e-100", "1e-100", nil},
	{"123456700", "1.234567e+08", nil},
	{"99999999999999974834176", "9.999999999999997e+22", nil},
	{"100000000000000000000001", "1.0000000000000001e+23", nil},
	{"100000000000000008388608", "1.0000000000000001e+23", nil},
	{"100000000000000016777215", "1.0000000000000001e+23", nil},
	{"100000000000000016777216", "1.0000000000000003e+23", nil},
	{"-1", "-1", nil},
	{"-0.1", "-0.1", nil},
	{"-0", "-0", nil},
	{"1e-20", "1e-20", nil},
	{"625e-3", "0.625", nil},

	// Hexadecimal floating-point.
	{"0x1p0", "1", nil},
	{"0x1p1", "2", nil},
	{"0x1p-1", "0.5", nil},
	{"0x1ep-1", "15", nil},
	{"-0x1ep-1", "-15", nil},
	{"-0x1_ep-1", "-15", nil},
	{"0x1p-200", "6.223015277861142e-61", nil},
	{"0x1p200", "1.6069380442589903e+60", nil},
	{"0x1fFe2.p0", "131042", nil},
	{"0x1fFe2.P0", "131042", nil},
	{"-0x2p3", "-16", nil},
	{"0x0.fp4", "15", nil},
	{"0x0.fp0", "0.9375", nil},
	{"0x1e2", "0", ErrSyntax},
	{"1p2", "0", ErrSyntax},

	// zeros
	{"0", "0", nil},
	{"0e0", "0", nil},
	{"-0e0", "-0", nil},
	{"+0e0", "0", nil},
	{"0e-0", "0", nil},
	{"-0e-0", "-0", nil},
	{"+0e-0", "0", nil},
	{"0e+0", "0", nil},
	{"-0e+0", "-0", nil},
	{"+0e+0", "0", nil},
	{"0e+01234567890123456789", "0", nil},
	{"0.00e-01234567890123456789", "0", nil},
	{"-0e+01234567890123456789", "-0", nil},
	{"-0.00e-01234567890123456789", "-0", nil},
	{"0x0p+01234567890123456789", "0", nil},
	{"0x0.00p-01234567890123456789", "0", nil},
	{"-0x0p+01234567890123456789", "-0", nil},
	{"-0x0.00p-01234567890123456789", "-0", nil},

	{"0e291", "0", nil}, // issue 15364
	{"0e292", "0", nil}, // issue 15364
	{"0e347", "0", nil}, // issue 15364
	{"0e348", "0", nil}, // issue 15364
	{"-0e291", "-0", nil},
	{"-0e292", "-0", nil},
	{"-0e347", "-0", nil},
	{"-0e348", "-0", nil},
	{"0x0p126", "0", nil},
	{"0x0p127", "0", nil},
	{"0x0p128", "0", nil},
	{"0x0p129", "0", nil},
	{"0x0p130", "0", nil},
	{"0x0p1022", "0", nil},
	{"0x0p1023", "0", nil},
	{"0x0p1024", "0", nil},
	{"0x0p1025", "0", nil},
	{"0x0p1026", "0", nil},
	{"-0x0p126", "-0", nil},
	{"-0x0p127", "-0", nil},
	{"-0x0p128", "-0", nil},
	{"-0x0p129", "-0", nil},
	{"-0x0p130", "-0", nil},
	{"-0x0p1022", "-0", nil},
	{"-0x0p1023", "-0", nil},
	{"-0x0p1024", "-0", nil},
	{"-0x0p1025", "-0", nil},
	{"-0x0p1026", "-0", nil},

	// NaNs
	{"nan", "NaN", nil},
	{"NaN", "NaN", nil},
	{"NAN", "NaN", nil},

	// Infs
	{"inf", "+Inf", nil},
	{"-Inf", "-Inf", nil},
	{"+INF", "+Inf", nil},
	{"-Infinity", "-Inf", nil},
	{"+INFINITY", "+Inf", nil},
	{"Infinity", "+Inf", nil},

	// largest float64
	{"1.7976931348623157e308", "1.7976931348623157e+308", nil},
	{"-1.7976931348623157e308", "-1.7976931348623157e+308", nil},
	{"0x1.fffffffffffffp1023", "1.7976931348623157e+308", nil},
	{"-0x1.fffffffffffffp1023", "-1.7976931348623157e+308", nil},
	{"0x1fffffffffffffp+971", "1.7976931348623157e+308", nil},
	{"-0x1fffffffffffffp+971", "-1.7976931348623157e+308", nil},
	{"0x.1fffffffffffffp1027", "1.7976931348623157e+308", nil},
	{"-0x.1fffffffffffffp1027", "-1.7976931348623157e+308", nil},

	// next float64 - too large
	{"1.7976931348623159e308", "+Inf", ErrRange},
	{"-1.7976931348623159e308", "-Inf", ErrRange},
	{"0x1p1024", "+Inf", ErrRange},
	{"-0x1p1024", "-Inf", ErrRange},
	{"0x2p1023", "+Inf", ErrRange},
	{"-0x2p1023", "-Inf", ErrRange},
	{"0x.1p1028", "+Inf", ErrRange},
	{"-0x.1p1028", "-Inf", ErrRange},
	{"0x.2p1027", "+Inf", ErrRange},
	{"-0x.2p1027", "-Inf", ErrRange},

	// the border is ...158079
	// borderline - okay
	{"1.7976931348623158e308", "1.7976931348623157e+308", nil},
	{"-1.7976931348623158e308", "-1.7976931348623157e+308", nil},
	{"0x1.fffffffffffff7fffp1023", "1.7976931348623157e+308", nil},
	{"-0x1.fffffffffffff7fffp1023", "-1.7976931348623157e+308", nil},
	// borderline - too large
	{"1.797693134862315808e308", "+Inf", ErrRange},
	{"-1.797693134862315808e308", "-Inf", ErrRange},
	{"0x1.fffffffffffff8p1023", "+Inf", ErrRange},
	{"-0x1.fffffffffffff8p1023", "-Inf", ErrRange},
	{"0x1fffffffffffff.8p+971", "+Inf", ErrRange},
	{"-0x1fffffffffffff8p+967", "-Inf", ErrRange},
	{"0x.1fffffffffffff8p1027", "+Inf", ErrRange},
	{"-0x.1fffffffffffff9p1027", "-Inf", ErrRange},

	// a little too large
	{"1e308", "1e+308", nil},
	{"2e308", "+Inf", ErrRange},
	{"1e309", "+Inf", ErrRange},
	{"0x1p1025", "+Inf", ErrRange},

	// way too large
	{"1e310", "+Inf", ErrRange},
	{"-1e310", "-Inf", ErrRange},
	{"1e400", "+Inf", ErrRange},
	{"-1e400", "-Inf", ErrRange},
	{"1e400000", "+Inf", ErrRange},
	{"-1e400000", "-Inf", ErrRange},
	{"0x1p1030", "+Inf", ErrRange},
	{"0x1p2000", "+Inf", ErrRange},
	{"0x1p2000000000", "+Inf", ErrRange},
	{"-0x1p1030", "-Inf", ErrRange},
	{"-0x1p2000", "-Inf", ErrRange},
	{"-0x1p2000000000", "-Inf", ErrRange},

	// denormalized
	{"1e-305", "1e-305", nil},
	{"1e-306", "1e-306", nil},
	{"1e-307", "1e-307", nil},
	{"1e-308", "1e-308", nil},
	{"1e-309", "1e-309", nil},
	{"1e-310", "1e-310", nil},
	{"1e-322", "1e-322", nil},
	// smallest denormal
	{"5e-324", "5e-324", nil},
	{"4e-324", "5e-324", nil},
	{"3e-324", "5e-324", nil},
	// too small
	{"2e-324", "0", nil},
	// way too small
	{"1e-350", "0", nil},
	{"1e-400000", "0", nil},
	{"1e-345", "0", nil}, // picked off in atof64
	{"1e-343", "0", nil}, // large c.s in parseFloat64
	{"9.999999999999999999e-343", "0", nil},

	// Near denormals and denormals.
	{"0x2.00000000000000p-1010", "1.8227805048890994e-304", nil}, // 0x00e0000000000000
	{"0x1.fffffffffffff0p-1010", "1.8227805048890992e-304", nil}, // 0x00dfffffffffffff
	{"0x1.fffffffffffff7p-1010", "1.8227805048890992e-304", nil}, // rounded down
	{"0x1.fffffffffffff8p-1010", "1.8227805048890994e-304", nil}, // rounded up
	{"0x1.fffffffffffff9p-1010", "1.8227805048890994e-304", nil}, // rounded up

	{"0x2.00000000000000p-1022", "4.450147717014403e-308", nil},  // 0x0020000000000000
	{"0x1.fffffffffffff0p-1022", "4.4501477170144023e-308", nil}, // 0x001fffffffffffff
	{"0x1.fffffffffffff7p-1022", "4.4501477170144023e-308", nil}, // rounded down
	{"0x1.fffffffffffff8p-1022", "4.450147717014403e-308", nil},  // rounded up
	{"0x1.fffffffffffff9p-1022", "4.450147717014403e-308", nil},  // rounded up

	{"0x1.00000000000000p-1022", "2.2250738585072014e-308", nil}, // 0x0010000000000000
	{"0x0.fffffffffffff0p-1022", "2.225073858507201e-308", nil},  // 0x000fffffffffffff
	{"0x0.ffffffffffffe0p-1022", "2.2250738585072004e-308", nil}, // 0x000ffffffffffffe
	{"0x0.ffffffffffffe7p-1022", "2.2250738585072004e-308", nil}, // rounded down
	{"0x1.ffffffffffffe8p-1023", "2.225073858507201e-308", nil},  // rounded up
	{"0x1.ffffffffffffe9p-1023", "2.225073858507201e-308", nil},  // rounded up

	{"0x0.00000003fffff0p-1022", "2.072261e-317", nil},  // 0x00000000003fffff
	{"0x0.00000003456780p-1022", "1.694649e-317", nil},  // 0x0000000000345678
	{"0x0.00000003456787p-1022", "1.694649e-317", nil},  // rounded down
	{"0x0.00000003456788p-1022", "1.694649e-317", nil},  // rounded down (half to even)
	{"0x0.00000003456790p-1022", "1.6946496e-317", nil}, // 0x0000000000345679
	{"0x0.00000003456789p-1022", "1.6946496e-317", nil}, // rounded up

	{"0x0.0000000345678800000000000000000000000001p-1022", "1.6946496e-317", nil}, // rounded up

	{"0x0.000000000000f0p-1022", "7.4e-323", nil}, // 0x000000000000000f
	{"0x0.00000000000060p-1022", "3e-323", nil},   // 0x0000000000000006
	{"0x0.00000000000058p-1022", "3e-323", nil},   // rounded up
	{"0x0.00000000000057p-1022", "2.5e-323", nil}, // rounded down
	{"0x0.00000000000050p-1022", "2.5e-323", nil}, // 0x0000000000000005

	{"0x0.00000000000010p-1022", "5e-324", nil},  // 0x0000000000000001
	{"0x0.000000000000081p-1022", "5e-324", nil}, // rounded up
	{"0x0.00000000000008p-1022", "0", nil},       // rounded down
	{"0x0.00000000000007fp-1022", "0", nil},      // rounded down

	// try to overflow exponent
	{"1e-4294967296", "0", nil},
	{"1e+4294967296", "+Inf", ErrRange},
	{"1e-18446744073709551616", "0", nil},
	{"1e+18446744073709551616", "+Inf", ErrRange},
	{"0x1p-4294967296", "0", nil},
	{"0x1p+4294967296", "+Inf", ErrRange},
	{"0x1p-18446744073709551616", "0", nil},
	{"0x1p+18446744073709551616", "+Inf", ErrRange},

	// Parse errors
	{"1e", "0", ErrSyntax},
	{"1e-", "0", ErrSyntax},
	{".e-1", "0", ErrSyntax},
	{"1\x00.2", "0", ErrSyntax},
	{"0x", "0", ErrSyntax},
	{"0x.", "0", ErrSyntax},
	{"0x1", "0", ErrSyntax},
	{"0x.1", "0", ErrSyntax},
	{"0x1p", "0", ErrSyntax},
	{"0x.1p", "0", ErrSyntax},
	{"0x1p+", "0", ErrSyntax},
	{"0x.1p+", "0", ErrSyntax},
	{"0x1p-", "0", ErrSyntax},
	{"0x.1p-", "0", ErrSyntax},
	{"0x1p+2", "4", nil},
	{"0x.1p+2", "0.25", nil},
	{"0x1p-2", "0.25", nil},
	{"0x.1p-2", "0.015625", nil},

	// https://www.exploringbinary.com/java-hangs-when-converting-2-2250738585072012e-308/
	{"2.2250738585072012e-308", "2.2250738585072014e-308", nil},
	// https://www.exploringbinary.com/php-hangs-on-numeric-value-2-2250738585072011e-308/
	{"2.2250738585072011e-308", "2.225073858507201e-308", nil},

	// A very large number (initially wrongly parsed by the fast algorithm).
	{"4.630813248087435e+307", "4.630813248087435e+307", nil},

	// A different kind of very large number.
	{"22.222222222222222", "22.22222222222222", nil},
	{"0x1.1111111111111p222", "7.18931911124017e+66", nil},
	{"0x2.2222222222222p221", "7.18931911124017e+66", nil},

	// Exactly halfway between 1 and math.Nextafter(1, 2).
	// Round to even (down).
	{"1.00000000000000011102230246251565404236316680908203125", "1", nil},
	{"0x1.00000000000008p0", "1", nil},
	// Slightly lower; still round down.
	{"1.00000000000000011102230246251565404236316680908203124", "1", nil},
	{"0x1.00000000000007Fp0", "1", nil},
	// Slightly higher; round up.
	{"1.00000000000000011102230246251565404236316680908203126", "1.0000000000000002", nil},
	{"0x1.000000000000081p0", "1.0000000000000002", nil},
	{"0x1.00000000000009p0", "1.0000000000000002", nil},

	// Halfway between x := math.Nextafter(1, 2) and math.Nextafter(x, 2)
	// Round to even (up).
	{"1.00000000000000033306690738754696212708950042724609375", "1.0000000000000004", nil},
	{"0x1.00000000000018p0", "1.0000000000000004", nil},

	// Halfway between 1090544144181609278303144771584 and 1090544144181609419040633126912
	// (15497564393479157p+46, should round to even 15497564393479156p+46, issue 36657)
	{"1090544144181609348671888949248", "1.0905441441816093e+30", nil},
	// slightly above, rounds up
	{"1090544144181609348835077142190", "1.0905441441816094e+30", nil},

	// Underscores.
	{"1_23.50_0_0e+1_2", "1.235e+14", nil},
	{"-_123.5e+12", "0", ErrSyntax},
	{"+_123.5e+12", "0", ErrSyntax},
	{"_123.5e+12", "0", ErrSyntax},
	{"1__23.5e+12", "0", ErrSyntax},
	{"123_.5e+12", "0", ErrSyntax},
	{"123._5e+12", "0", ErrSyntax},
	{"123.5_e+12", "0", ErrSyntax},
	{"123.5__0e+12", "0", ErrSyntax},
	{"123.5e_+12", "0", ErrSyntax},
	{"123.5e+_12", "0", ErrSyntax},
	{"123.5e_-12", "0", ErrSyntax},
	{"123.5e-_12", "0", ErrSyntax},
	{"123.5e+1__2", "0", ErrSyntax},
	{"123.5e+12_", "0", ErrSyntax},

	{"0x_1_2.3_4_5p+1_2", "74565", nil},
	{"-_0x12.345p+12", "0", ErrSyntax},
	{"+_0x12.345p+12", "0", ErrSyntax},
	{"_0x12.345p+12", "0", ErrSyntax},
	{"0x__12.345p+12", "0", ErrSyntax},
	{"0x1__2.345p+12", "0", ErrSyntax},
	{"0x12_.345p+12", "0", ErrSyntax},
	{"0x12._345p+12", "0", ErrSyntax},
	{"0x12.3__45p+12", "0", ErrSyntax},
	{"0x12.345_p+12", "0", ErrSyntax},
	{"0x12.345p_+12", "0", ErrSyntax},
	{"0x12.345p+_12", "0", ErrSyntax},
	{"0x12.345p_-12", "0", ErrSyntax},
	{"0x12.345p-_12", "0", ErrSyntax},
	{"0x12.345p+1__2", "0", ErrSyntax},
	{"0x12.345p+12_", "0", ErrSyntax},

	{"1e100x", "0", ErrSyntax},
	{"1e1000x", "0", ErrSyntax},
}

var atof32tests = [...]atofTest{
	// Hex
	{"0x1p-100", "7.888609e-31", nil},
	{"0x1p100", "1.2676506e+30", nil},

	// Exactly halfway between 1 and the next float32.
	// Round to even (down).
	{"1.000000059604644775390625", "1", nil},
	{"0x1.000001p0", "1", nil},
	// Slightly lower.
	{"1.000000059604644775390624", "1", nil},
	{"0x1.0000008p0", "1", nil},
	{"0x1.000000fp0", "1", nil},
	// Slightly higher.
	{"1.000000059604644775390626", "1.0000001", nil},
	{"0x1.000002p0", "1.0000001", nil},
	{"0x1.0000018p0", "1.0000001", nil},
	{"0x1.0000011p0", "1.0000001", nil},

	// largest float32: (1<<128) * (1 - 2^-24)
	{"340282346638528859811704183484516925440", "3.4028235e+38", nil},
	{"-340282346638528859811704183484516925440", "-3.4028235e+38", nil},
	{"0x.ffffffp128", "3.4028235e+38", nil},
	{"-0x.ffffffp128", "-3.4028235e+38", nil},
	// next float32 - too large
	{"3.4028236e38", "+Inf", ErrRange},
	{"-3.4028236e38", "-Inf", ErrRange},
	{"0x1.0p128", "+Inf", ErrRange},
	{"-0x1.0p128", "-Inf", ErrRange},
	// the border is 3.40282356779...e+38
	// borderline - okay
	{"3.402823567e38", "3.4028235e+38", nil},
	{"-3.402823567e38", "-3.4028235e+38", nil},
	{"0x.ffffff7fp128", "3.4028235e+38", nil},
	{"-0x.ffffff7fp128", "-3.4028235e+38", nil},
	// borderline - too large
	{"3.4028235678e38", "+Inf", ErrRange},
	{"-3.4028235678e38", "-Inf", ErrRange},
	{"0x.ffffff8p128", "+Inf", ErrRange},
	{"-0x.ffffff8p128", "-Inf", ErrRange},

	// Denormals: less than 2^-126
	{"1e-38", "1e-38", nil},
	{"1e-39", "1e-39", nil},
	{"1e-40", "1e-40", nil},
	{"1e-41", "1e-41", nil},
	{"1e-42", "1e-42", nil},
	{"1e-43", "1e-43", nil},
	{"1e-44", "1e-44", nil},
	{"6e-45", "6e-45", nil}, // 4p-149 = 5.6e-45
	{"5e-45", "6e-45", nil},

	// Smallest denormal
	{"1e-45", "1e-45", nil}, // 1p-149 = 1.4e-45
	{"2e-45", "1e-45", nil},
	{"3e-45", "3e-45", nil},

	// Near denormals and denormals.
	{"0x0.89aBcDp-125", "1.2643093e-38", nil},  // 0x0089abcd
	{"0x0.8000000p-125", "1.1754944e-38", nil}, // 0x00800000
	{"0x0.1234560p-125", "1.671814e-39", nil},  // 0x00123456
	{"0x0.1234567p-125", "1.671814e-39", nil},  // rounded down
	{"0x0.1234568p-125", "1.671814e-39", nil},  // rounded down
	{"0x0.1234569p-125", "1.671815e-39", nil},  // rounded up
	{"0x0.1234570p-125", "1.671815e-39", nil},  // 0x00123457
	{"0x0.0000010p-125", "1e-45", nil},         // 0x00000001
	{"0x0.00000081p-125", "1e-45", nil},        // rounded up
	{"0x0.0000008p-125", "0", nil},             // rounded down
	{"0x0.0000007p-125", "0", nil},             // rounded down

	{"1e-70", "0", nil}, // picked off in atof32
	{"1e-65", "0", nil}, // picked off in atof32
	{"1e-64", "0", nil}, // large c.s in parseFloat32
	{"9.999999999999999999e-64", "0", nil},

	// 2^92 = 8388608p+69 = 4951760157141521099596496896 (4.9517602e27)
	// is an exact power of two that needs 8 decimal digits to be correctly
	// parsed back.
	// The float32 before is 16777215p+68 = 4.95175986e+27
	// The halfway is 4.951760009. A bad algorithm that thinks the previous
	// float32 is 8388607p+69 will shorten incorrectly to 4.95176e+27.
	{"4951760157141521099596496896", "4.9517602e+27", nil},
}

// fmtBuf holds the result of format, so that comparing it with an expected
// string allocates nothing.
var fmtBuf [1024]byte

// format returns FormatFloat(f, fmt, prec, bitSize), a view of fmtBuf that
// is valid until the next call.
func format(f float64, fmt byte, prec, bitSize int) string {
	b := strconv.AppendFloat(fmtBuf[:0], f, fmt, prec, bitSize)
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func bitsOf(f float64) uint64     { return *(*uint64)(unsafe.Pointer(&f)) }
func fromBits(b uint64) float64   { return *(*float64)(unsafe.Pointer(&b)) }
func fromBits32(b uint32) float32 { return *(*float32)(unsafe.Pointer(&b)) }

func testAtof() {
	for i := 0; i < len(atoftests); i++ {
		test := &atoftests[i]
		out, err := strconv.ParseFloat(test.in, 64)
		outs := format(out, 'g', -1, 64)
		if outs != test.out || err != test.err {
			errorf("ParseFloat(%v, 64) = %v, %v want %v, %v", test.in, out, err, test.out, test.err)
		}

		if float64(float32(out)) == out {
			out, err := strconv.ParseFloat(test.in, 32)
			out32 := float32(out)
			if float64(out32) != out {
				errorf("ParseFloat(%v, 32) = %v, not a float32 (closest is %v)", test.in, out, float64(out32))
				continue
			}
			outs := format(float64(out32), 'g', -1, 32)
			if outs != test.out || err != test.err {
				errorf("ParseFloat(%v, 32) = %v, %v want %v, %v  # %v", test.in, out32, err, test.out, test.err, out)
			}
		}
	}
	for _, test := range atof32tests {
		out, err := strconv.ParseFloat(test.in, 32)
		out32 := float32(out)
		if float64(out32) != out {
			errorf("ParseFloat(%v, 32) = %v, not a float32 (closest is %v)", test.in, out, float64(out32))
			continue
		}
		outs := format(float64(out32), 'g', -1, 32)
		if outs != test.out || err != test.err {
			errorf("ParseFloat(%v, 32) = %v, %v want %v, %v  # %v", test.in, out32, err, test.out, test.err, out)
		}
	}
	fmt.Println("ok Atof", len(atoftests)+len(atof32tests))
}

// longInput builds head, n copies of fill, then tail, in buf.
func longInput(buf []byte, head string, fill byte, n int, tail string) string {
	b := append(buf[:0], head...)
	for i := 0; i < n; i++ {
		b = append(b, fill)
	}
	b = append(b, tail...)
	return unsafe.String(unsafe.SliceData(b), len(b))
}

var longBuf [10100]byte

func testAtofLong() {
	cases := [...]struct {
		head, tail string
		fill       byte
		n          int
		out        string
		bitSize    int
	}{
		// A different kind of very large number.
		{"2.", "e+1", '2', 4000, "22.22222222222222", 64},
		{"0x2.", "p221", '2', 4000, "7.18931911124017e+66", 64},
		// Slightly higher, but you have to read all the way to the end.
		{"1.00000000000000011102230246251565404236316680908203125", "1", '0', 10000, "1.0000000000000002", 64},
		{"0x1.00000000000008", "1p0", '0', 10000, "1.0000000000000002", 64},
		{"1.000000059604644775390625", "1", '0', 10000, "1.0000001", 32},
		{"0x1.000001", "1p0", '0', 10000, "1.0000001", 32},
	}
	for _, c := range cases {
		in := longInput(longBuf[:], c.head, c.fill, c.n, c.tail)
		out, err := strconv.ParseFloat(in, c.bitSize)
		if err != nil {
			errorf("ParseFloat(%.40q..., %d): %v", in, c.bitSize, err)
			continue
		}
		if outs := format(out, 'g', -1, c.bitSize); outs != c.out {
			errorf("ParseFloat(%.40q..., %d) = %s, want %s", in, c.bitSize, outs, c.out)
		}
	}
	fmt.Println("ok AtofLong", len(cases))
}

// lcg is a small deterministic generator for test data.
type lcg struct{ state uint64 }

func (g *lcg) next() uint64 {
	g.state = g.state*6364136223846793005 + 1442695040888963407
	return g.state
}

func testAtofRandom() {
	g := lcg{state: 1}
	var buf [64]byte
	for i := 0; i < 20000; i++ {
		n := g.next()>>32<<32 | g.next()>>32
		x := fromBits(n)
		s := strconv.AppendFloat(buf[:0], x, 'g', -1, 64)
		str := unsafe.String(unsafe.SliceData(s), len(s))
		y, _ := strconv.ParseFloat(str, 64)
		if y != x && !(x != x && y != y) {
			errorf("number %s badly parsed as %b (expected %b)", str, bitsOf(y), bitsOf(x))
		}
	}
	fmt.Println("ok AtofRandom", 20000)
}

var roundTripCases = [...]struct {
	f float64
	s string
}{
	// Issue 2917.
	{8865794286000691 << 39, "4.87402195346389e+27"},
	{8865794286000692 << 39, "4.8740219534638903e+27"},
}

func testRoundTrip() {
	for _, tt := range roundTripCases {
		s := format(tt.f, 'g', -1, 64)
		if s != tt.s {
			errorf("FormatFloat(%b) = %s, want %s", bitsOf(tt.f), s, tt.s)
		}
		f, err := strconv.ParseFloat(tt.s, 64)
		if f != tt.f || err != nil {
			errorf("ParseFloat(%s) = %b, %v want %b, nil", tt.s, bitsOf(f), err, bitsOf(tt.f))
		}
	}
	fmt.Println("ok RoundTrip", len(roundTripCases))
}

// testRoundTrip32 tries a fraction of all finite positive float32 values.
func testRoundTrip32() {
	step := uint32(997)
	count := 0
	for i := uint32(0); i < 0xff<<23; i += step {
		f := fromBits32(i)
		if i&1 == 1 {
			f = -f // negative
		}
		s := format(float64(f), 'g', -1, 32)

		parsed, err := strconv.ParseFloat(s, 32)
		parsed32 := float32(parsed)
		switch {
		case err != nil:
			errorf("ParseFloat(%q, 32) gave error %s", s, err)
		case float64(parsed32) != parsed:
			errorf("ParseFloat(%q, 32) = %v, not a float32 (nearest is %v)", s, parsed, parsed32)
		case parsed32 != f:
			errorf("ParseFloat(%q, 32) = %v (expected %v)", s, parsed32, f)
		}
		count++
	}
	fmt.Println("ok RoundTrip32", count)
}

// Issue 42297: a lot of code in the wild accidentally calls ParseFloat(s, 10)
// or ParseFloat(s, 0), so allow bitSize values other than 32 and 64.
func testIncorrectBitSize() {
	const s = "1.5e308"
	const want = 1.5e308

	for _, bitSize := range [...]int{0, 10, 100, 128} {
		f, err := strconv.ParseFloat(s, bitSize)
		if err != nil {
			errorf("ParseFloat(%q, %d) gave error %s", s, bitSize, err)
		} else if f != want {
			errorf("ParseFloat(%q, %d) = %g (expected %g)", s, bitSize, f, want)
		}
	}
	fmt.Println("ok IncorrectBitSize")
}

// Prefix handling: an input that continues past a number is a syntax error.
func testTrailing() {
	var buf [128]byte
	n := 0
	for i := range atoftests {
		test := &atoftests[i]
		if test.err != nil {
			continue
		}
		// Adding characters that do not extend a number invalidates the input.
		for _, suffix := range [...]string{" ", "q", "+", "-", "<", "=", ">", "(", ")", "i", "init"} {
			b := append(buf[:0], test.in...)
			b = append(b, suffix...)
			in := unsafe.String(unsafe.SliceData(b), len(b))
			if _, err := strconv.ParseFloat(in, 64); err != ErrSyntax {
				errorf("ParseFloat(%q, 64): err = %v; want ErrSyntax", in, err)
			}
			n++
		}
	}
	fmt.Println("ok Trailing", n)
}

type ftoaTest struct {
	f    float64
	fmt  byte
	prec int
	s    string
}

func fdiv(a, b float64) float64 { return a / b }

const (
	below1e23 = 99999999999999974834176
	above1e23 = 100000000000000008388608
)

var nan = fromBits(0x7FF8000000000001)
var inf = fromBits(0x7FF0000000000000)

var ftoatests = [...]ftoaTest{
	{1, 'e', 5, "1.00000e+00"},
	{1, 'f', 5, "1.00000"},
	{1, 'g', 5, "1"},
	{1, 'g', -1, "1"},
	{1, 'x', -1, "0x1p+00"},
	{1, 'x', 5, "0x1.00000p+00"},
	{20, 'g', -1, "20"},
	{20, 'x', -1, "0x1.4p+04"},
	{1234567.8, 'g', -1, "1.2345678e+06"},
	{1234567.8, 'x', -1, "0x1.2d687cccccccdp+20"},
	{200000, 'g', -1, "200000"},
	{200000, 'x', -1, "0x1.86ap+17"},
	{200000, 'X', -1, "0X1.86AP+17"},
	{2000000, 'g', -1, "2e+06"},
	{1e10, 'g', -1, "1e+10"},

	// f conversion basic cases
	{12345, 'f', 2, "12345.00"},
	{1234.5, 'f', 2, "1234.50"},
	{123.45, 'f', 2, "123.45"},
	{12.345, 'f', 2, "12.35"},
	{1.2345, 'f', 2, "1.23"},
	{0.12345, 'f', 2, "0.12"},
	{0.12945, 'f', 2, "0.13"},
	{0.012345, 'f', 2, "0.01"},
	{0.015, 'f', 2, "0.01"},
	{0.016, 'f', 2, "0.02"},
	{0.0052345, 'f', 2, "0.01"},
	{0.0012345, 'f', 2, "0.00"},
	{0.00012345, 'f', 2, "0.00"},
	{0.000012345, 'f', 2, "0.00"},

	{0.996644984, 'f', 6, "0.996645"},
	{0.996644984, 'f', 5, "0.99664"},
	{0.996644984, 'f', 4, "0.9966"},
	{0.996644984, 'f', 3, "0.997"},
	{0.996644984, 'f', 2, "1.00"},
	{0.996644984, 'f', 1, "1.0"},

	// g conversion and zero suppression
	{400, 'g', 2, "4e+02"},
	{40, 'g', 2, "40"},
	{4, 'g', 2, "4"},
	{.4, 'g', 2, "0.4"},
	{.04, 'g', 2, "0.04"},
	{.004, 'g', 2, "0.004"},
	{.0004, 'g', 2, "0.0004"},
	{.00004, 'g', 2, "4e-05"},
	{.000004, 'g', 2, "4e-06"},

	{0, 'e', 5, "0.00000e+00"},
	{0, 'f', 5, "0.00000"},
	{0, 'g', 5, "0"},
	{0, 'g', -1, "0"},
	{0, 'x', 5, "0x0.00000p+00"},

	{-1, 'e', 5, "-1.00000e+00"},
	{-1, 'f', 5, "-1.00000"},
	{-1, 'g', 5, "-1"},
	{-1, 'g', -1, "-1"},

	{12, 'e', 5, "1.20000e+01"},
	{12, 'f', 5, "12.00000"},
	{12, 'g', 5, "12"},
	{12, 'g', -1, "12"},

	{123456700, 'e', 5, "1.23457e+08"},
	{123456700, 'f', 5, "123456700.00000"},
	{123456700, 'g', 5, "1.2346e+08"},
	{123456700, 'g', -1, "1.234567e+08"},

	{1.2345e6, 'e', 5, "1.23450e+06"},
	{1.2345e6, 'f', 5, "1234500.00000"},
	{1.2345e6, 'g', 5, "1.2345e+06"},

	// Round to even
	{1.2345e6, 'e', 3, "1.234e+06"},
	{1.2355e6, 'e', 3, "1.236e+06"},
	{1.2345, 'f', 3, "1.234"},
	{1.2355, 'f', 3, "1.236"},
	{1234567890123456.5, 'e', 15, "1.234567890123456e+15"},
	{1234567890123457.5, 'e', 15, "1.234567890123458e+15"},
	{108678236358137.625, 'g', -1, "1.0867823635813762e+14"},

	{1e23, 'e', 17, "9.99999999999999916e+22"},
	{1e23, 'f', 17, "99999999999999991611392.00000000000000000"},
	{1e23, 'g', 17, "9.9999999999999992e+22"},

	{1e23, 'e', -1, "1e+23"},
	{1e23, 'f', -1, "100000000000000000000000"},
	{1e23, 'g', -1, "1e+23"},

	{below1e23, 'e', 17, "9.99999999999999748e+22"},
	{below1e23, 'f', 17, "99999999999999974834176.00000000000000000"},
	{below1e23, 'g', 17, "9.9999999999999975e+22"},

	{below1e23, 'e', -1, "9.999999999999997e+22"},
	{below1e23, 'f', -1, "99999999999999970000000"},
	{below1e23, 'g', -1, "9.999999999999997e+22"},

	{above1e23, 'e', 17, "1.00000000000000008e+23"},
	{above1e23, 'f', 17, "100000000000000008388608.00000000000000000"},
	{above1e23, 'g', 17, "1.0000000000000001e+23"},

	{above1e23, 'e', -1, "1.0000000000000001e+23"},
	{above1e23, 'f', -1, "100000000000000010000000"},
	{above1e23, 'g', -1, "1.0000000000000001e+23"},

	{fdiv(5e-304, 1e20), 'g', -1, "5e-324"},   // avoid constant arithmetic
	{fdiv(-5e-304, 1e20), 'g', -1, "-5e-324"}, // avoid constant arithmetic
	{fdiv(5e-304, 1e20), 'e', -1, "5e-324"},

	{32, 'g', -1, "32"},
	{32, 'g', 0, "3e+01"},

	{100, 'x', -1, "0x1.9p+06"},
	{100, 'y', -1, "%y"},

	{nan, 'g', -1, "NaN"},
	{-nan, 'g', -1, "NaN"},
	{inf, 'g', -1, "+Inf"},
	{-inf, 'g', -1, "-Inf"},

	{-1, 'b', -1, "-4503599627370496p-52"},

	// fixed bugs
	{0.9, 'f', 1, "0.9"},
	{0.09, 'f', 1, "0.1"},
	{0.0999, 'f', 1, "0.1"},
	{0.05, 'f', 1, "0.1"},
	{0.05, 'f', 0, "0"},
	{0.5, 'f', 1, "0.5"},
	{0.5, 'f', 0, "0"},
	{1.5, 'f', 0, "2"},

	// https://www.exploringbinary.com/java-hangs-when-converting-2-2250738585072012e-308/
	{2.2250738585072012e-308, 'g', -1, "2.2250738585072014e-308"},
	// https://www.exploringbinary.com/php-hangs-on-numeric-value-2-2250738585072011e-308/
	{2.2250738585072011e-308, 'g', -1, "2.225073858507201e-308"},

	// Issue 2625.
	{383260575764816448, 'f', 0, "383260575764816448"},
	{383260575764816448, 'g', -1, "3.8326057576481645e+17"},

	// Issue 29491.
	{498484681984085570, 'f', -1, "498484681984085570"},
	{-5.8339553793802237e+23, 'g', -1, "-5.8339553793802237e+23"},

	// Issue 52187
	{123.45, '?', 0, "%?"},
	{123.45, '?', 1, "%?"},
	{123.45, '?', -1, "%?"},

	// rounding
	{2.275555555555555, 'x', -1, "0x1.23456789abcdep+01"},
	{2.275555555555555, 'x', 0, "0x1p+01"},
	{2.275555555555555, 'x', 2, "0x1.23p+01"},
	{2.275555555555555, 'x', 16, "0x1.23456789abcde000p+01"},
	{2.275555555555555, 'x', 21, "0x1.23456789abcde00000000p+01"},
	{2.2755555510520935, 'x', -1, "0x1.2345678p+01"},
	{2.2755555510520935, 'x', 6, "0x1.234568p+01"},
	{2.275555431842804, 'x', -1, "0x1.2345668p+01"},
	{2.275555431842804, 'x', 6, "0x1.234566p+01"},
	{3.999969482421875, 'x', -1, "0x1.ffffp+01"},
	{3.999969482421875, 'x', 4, "0x1.ffffp+01"},
	{3.999969482421875, 'x', 3, "0x1.000p+02"},
	{3.999969482421875, 'x', 2, "0x1.00p+02"},
	{3.999969482421875, 'x', 1, "0x1.0p+02"},
	{3.999969482421875, 'x', 0, "0x1p+02"},

	// Cases that Java once mishandled, from David Chase.
	{1.801439850948199e+16, 'g', -1, "1.801439850948199e+16"},
	{5.960464477539063e-08, 'g', -1, "5.960464477539063e-08"},
	{1.012e-320, 'g', -1, "1.012e-320"},

	// Cases from TestFtoaRandom that caught bugs in fixedFtoa.
	{8177880169308380. * (1 << 1), 'e', 14, "1.63557603386168e+16"},
	{8393378656576888. * (1 << 1), 'e', 15, "1.678675731315378e+16"},
	{8738676561280626. * (1 << 4), 'e', 16, "1.3981882498049002e+17"},
	{8291032395191335. / (1 << 30), 'e', 5, "7.72163e+06"},
	{8880392441509914. / (1 << 80), 'e', 16, "7.3456884594794477e-09"},

	// Exercise divisiblePow5 case in fixedFtoa
	{2384185791015625. * (1 << 12), 'e', 5, "9.76562e+18"},
	{2384185791015625. * (1 << 13), 'e', 5, "1.95312e+19"},

	// Exercise potential mistakes in fixedFtoa.
	{0x1.000000000005p+71, 'e', 16, "2.3611832414348645e+21"},
	{0x1.0000p-27, 'e', 17, "7.45058059692382812e-09"},
	{0x1.0000p-41, 'e', 17, "4.54747350886464119e-13"},

	// go.dev/issue/79591; used NULs instead of trailing zeros
	{0.00000000000000000093564868367555, 'f', 150, "0.000000000000000000935648683675550014820074270847655141588693848715273682775661612254225474316626787185668945312500000000000000000000000000000000000000"},
}

func testFtoa() {
	var abc [1100]byte
	for i := 0; i < len(ftoatests); i++ {
		test := &ftoatests[i]
		s := format(test.f, test.fmt, test.prec, 64)
		if s != test.s {
			errorf("testN=64 %v %c %d want %s got %s", test.f, test.fmt, test.prec, test.s, s)
		}
		x := strconv.AppendFloat(append(abc[:0], "abc"...), test.f, test.fmt, test.prec, 64)
		if len(x) != 3+len(test.s) || unsafe.String(unsafe.SliceData(x), 3) != "abc" || unsafe.String(unsafe.SliceData(x[3:]), len(x)-3) != test.s {
			errorf("AppendFloat testN=64 %v %c %d want abc%s got %s", test.f, test.fmt, test.prec, test.s, unsafe.String(unsafe.SliceData(x), len(x)))
		}
		if float64(float32(test.f)) == test.f && test.fmt != 'b' {
			if test.f == 5.960464477539063e-08 {
				// This test is an exact float32 but asking for float64 precision in the string.
				continue
			}
			s := format(test.f, test.fmt, test.prec, 32)
			if s != test.s {
				errorf("testN=32 %v %c %d want %s got %s", test.f, test.fmt, test.prec, test.s, s)
			}
		}
	}
	fmt.Println("ok Ftoa", len(ftoatests))
}

func testFtoaPowersOfTwo() {
	// Every power of two that is a finite float, 'e' shortest, must read back.
	n := 0
	f := 1.0
	for exp := 0; exp < 1024; exp++ {
		s := format(f, 'e', -1, 64)
		if x, _ := strconv.ParseFloat(s, 64); x != f {
			errorf("failed roundtrip %v => %s => %v", f, s, x)
		}
		f *= 2
		n++
	}
	f = 1.0
	for exp := 0; exp < 1074; exp++ {
		s := format(f, 'e', -1, 64)
		if x, _ := strconv.ParseFloat(s, 64); x != f {
			errorf("failed roundtrip %v => %s => %v", f, s, x)
		}
		f /= 2
		n++
	}
	fmt.Println("ok FtoaPowersOfTwo", n)
}

func testNumError() {
	var mem [1024]byte
	a := arena.New(mem[:])
	e := strconv.NewNumError("ParseFloat", "1x\n\"", ErrSyntax, &a)
	want := "strconv.ParseFloat: parsing \"1x\\n\\\"\": invalid syntax"
	if e.Error() != want {
		errorf("Error() = %q, want %q", e.Error(), want)
	}
	if e.Func != "ParseFloat" || e.Num != "1x\n\"" || e.Err != ErrSyntax {
		errorf("fields %q %q %v", e.Func, e.Num, e.Err)
	}
	if !errors.Is(e, ErrSyntax) || errors.Is(e, ErrRange) {
		errorf("errors.Is does not see the reason")
	}
	var ne *strconv.NumError
	if !errors.As(e, &ne) || ne != e {
		errorf("errors.As does not find the NumError")
	}
	// Strings are valid UTF-8 (language spec section 6.10), so the input
	// has no invalid bytes; U+0080 is not printable and is escaped.
	e2 := strconv.NewNumError("ParseInt", "\u0080é\u0001", ErrRange, &a)
	if want := "strconv.ParseInt: parsing \"\\u0080é\\x01\": value out of range"; e2.Error() != want {
		errorf("Error() = %q, want %q", e2.Error(), want)
	}
	a.Reset()
	// With the default allocator, Free returns the memory.
	e3 := strconv.NewNumError("ParseUint", "", ErrSyntax, alloc.Default())
	if want := "strconv.ParseUint: parsing \"\": invalid syntax"; e3.Error() != want {
		errorf("Error() = %q, want %q", e3.Error(), want)
	}
	e3.Free()
	var plain strconv.NumError
	plain.Err = ErrRange
	if plain.Error() != ErrRange.Error() {
		errorf("plain NumError.Error() = %q", plain.Error())
	}
	fmt.Println("ok NumError")
}

func main() {
	testAtof()
	testAtofLong()
	testAtofRandom()
	testRoundTrip()
	testRoundTrip32()
	testIncorrectBitSize()
	testTrailing()
	testFtoa()
	testFtoaPowersOfTwo()
	testNumError()
	if failures > 0 {
		fmt.Println(failures, "failures")
		os.Exit(1)
	}
	fmt.Println("all ok")
}
