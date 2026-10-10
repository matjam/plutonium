// Calls between Plutonium and C under the System V AMD64 convention. The C
// half is cabi.c, compiled with the system C compiler. Every check prints
// "ok" or the values that differ; the expected weights are computed again
// here, so a value passed in the wrong place shows up as a failure.
package main

import "unsafe"

func check(name string, got, want int64) {
	if got == want {
		println(name, "ok")
	} else {
		println(name, "FAIL got", got, "want", want)
	}
}

func checkF(name string, got, want float64) {
	if got == want {
		println(name, "ok")
	} else {
		println(name, "FAIL got", int64(got*1000), "want", int64(want*1000), "(x1000)")
	}
}

// ---------------------------------------------------------------------------
// Scalars

//pu:extern c_scalars
func cScalars(a int8, b uint8, c int16, d uint16, e int32, f uint32, g int64, h uint64, z bool) int64

//pu:extern c_ret_u8
func cRetU8(x uint8) uint8

//pu:extern c_ret_i8
func cRetI8(x int8) int8

//pu:extern c_ret_u16
func cRetU16(x uint16) uint16

//pu:extern c_ret_i16
func cRetI16(x int16) int16

//pu:extern c_ret_bool
func cRetBool(x int32) bool

//pu:extern c_floats
func cFloats(a float32, b float64, c float32, d float64) float64

//pu:extern c_ret_f32
func cRetF32(a, b float32) float32

//pu:extern c_mixed
func cMixed(a int32, b float64, c int64, d float32) float64

// ---------------------------------------------------------------------------
// Structs

type I1 struct{ A int32 }
type I2 struct{ A, B int32 }
type I3 struct {
	A int64
	B int8
	C int16
}
type I4 struct{ A, B, C, D int16 }

type F1 struct{ A float32 }
type F2 struct{ A, B float32 }
type F3 struct{ A, B, C float32 }
type F4 struct{ A, B, C, D float32 }
type D2 struct{ A, B float64 }

type M2 struct {
	A int64
	B float64
}
type M2b struct {
	A float64
	B int32
}
type M3 struct {
	A float32
	B int32
	C float64
}
type M4 struct {
	A int8
	B uint8
	C float32
	D float64
}

type Big struct {
	A int64
	B float64
	C int32
}
type Huge struct{ V [5]int64 }

//pu:extern c_i1
func cI1(s I1) int64

//pu:extern c_i2
func cI2(s I2) int64

//pu:extern c_i3
func cI3(s I3) int64

//pu:extern c_i4
func cI4(s I4) int64

//pu:extern c_make_i1
func cMakeI1(a int32) I1

//pu:extern c_make_i2
func cMakeI2(a, b int32) I2

//pu:extern c_make_i3
func cMakeI3(a int64, b int8, c int16) I3

//pu:extern c_make_i4
func cMakeI4(a int16) I4

//pu:extern c_f1
func cF1(s F1) float64

//pu:extern c_f2
func cF2(s F2) float64

//pu:extern c_f3
func cF3(s F3) float64

//pu:extern c_f4
func cF4(s F4) float64

//pu:extern c_d2
func cD2(s D2) float64

//pu:extern c_make_f1
func cMakeF1(a float32) F1

//pu:extern c_make_f2
func cMakeF2(a, b float32) F2

//pu:extern c_make_f3
func cMakeF3(a float32) F3

//pu:extern c_make_f4
func cMakeF4(a float32) F4

//pu:extern c_make_d2
func cMakeD2(a, b float64) D2

//pu:extern c_m2
func cM2(s M2) float64

//pu:extern c_m2b
func cM2b(s M2b) float64

//pu:extern c_m3
func cM3(s M3) float64

//pu:extern c_m4
func cM4(s M4) float64

//pu:extern c_make_m2
func cMakeM2(a int64, b float64) M2

//pu:extern c_make_m2b
func cMakeM2b(a float64, b int32) M2b

//pu:extern c_make_m3
func cMakeM3(a float32, b int32, c float64) M3

//pu:extern c_make_m4
func cMakeM4(a int8) M4

//pu:extern c_big
func cBig(s Big, k int64) float64

//pu:extern c_make_big
func cMakeBig(a int64, b float64, c int32) Big

//pu:extern c_big_round_trip
func cBigRoundTrip(s Big) Big

//pu:extern c_huge
func cHuge(h, g Huge) int64

//pu:extern c_many
func cMany(a, b, c, d, e, f, g, h int64, x0, x1, x2, x3, x4, x5, x6, x7, x8, x9 float64, n int8, y float32) int64

//pu:extern c_spill_int
func cSpillInt(a, b, c, d, e int64, s I3, f int64) int64

//pu:extern c_spill_sse
func cSpillSSE(a, b, c, d, e, f, g float64, s D2, h float64) float64

//pu:extern c_sret_many
func cSretMany(a, b, c, d, e, f int64) Big

// Several results are a struct of the results.

//pu:extern c_divmod
func cDivmod(a, b int64) (q, r int64)

//pu:extern c_pair32
func cPair32(x int32) (int32, int32)

//pu:extern c_three
func cThree(x int64) (int64, int64, int64)

//pu:extern c_int_double
func cIntDouble(x int64) (int64, float64)

//pu:extern c_callbacks
func cCallbacks() int64

//pu:extern c_apply
func cApply(fn unsafe.Pointer, a, b int32) int64

//pu:extern c_address_of_sum
func cAddressOfSum() unsafe.Pointer

// Packed and volatile types cross as their storage.

type Ctl packed uint32 {
	mode   bits[uint, 4]
	enable bits[bool, 1]
	level  bits[uint, 7]
}

type Header packed {
	version uint8
	kind    uint8
	length  uint32
}

type WithHeader struct {
	H Header
	X int16
}

type VolMix struct {
	V volatile[int32]
	F float32
}

//pu:extern c_ctl
func cCtl(c Ctl) int64

//pu:extern c_with_header
func cWithHeader(w WithHeader) int64

//pu:extern c_vol_mix
func cVolMix(v VolMix) VolMix

// A body-less method bound to a C function takes its receiver as the first
// argument.
//
//pu:extern c_m2
func (m M2) viaC() float64

// ---------------------------------------------------------------------------
// Exported to C

//pu:export pu_m2_scale
func (m M2) Scale(k float64) M2 { return M2{m.A * int64(k), m.B * k} }

//pu:export pu_scale_big
func scaleBig(b Big, k int64) Big {
	return Big{b.A * k, b.B * float64(k), b.C * int32(k)}
}

//pu:export pu_mixed
func mixed(m M2, f float32, i int8) M2b {
	return M2b{float64(m.A) + m.B + float64(f), int32(i) * int32(m.A)}
}

//pu:export pu_divmod
func divmod(a, b int64) (int64, int64) { return a / b, a % b }

//pu:export pu_three
func three(x int64) (int64, int64, int64) { return x + 1, x + 2, x + 3 }

//pu:export pu_narrow
func narrow(a int8, b uint16) uint8 { return uint8(int(a) + int(b)) }

//pu:export pu_is_big
func isBig(x int64) bool { return x > 100 }

//pu:export pu_spill
func spill(a, b, c, d, e int64, s I3, f int64, x0, x1, x2, x3, x4, x5, x6 float64, t D2, x7 float64) float64 {
	ints := a + b + c + d + e + s.A + int64(s.B) + int64(s.C) + f
	return float64(ints) + (x0+x1+x2+x3+x4+x5+x6+t.A+t.B+x7)*100
}

//pu:export pu_f3
func f3(v F3) F3 { return F3{v.C, v.B, v.A} }

//pu:export pu_point
func point(a int64, b int8, c int16) I3 { return I3{a, b, c} }

//pu:export pu_huge
func huge(h Huge) int64 {
	return h.V[0] + h.V[1]*10 + h.V[2]*100 + h.V[3]*1000 + h.V[4]*10000
}

//pu:export pu_i2_sum
func i2Sum(s I2) int64 { return int64(s.A)*100 + int64(s.B) }

func main() {
	check("scalars", cScalars(-3, 250, -300, 60000, -70000, 4000000000, -5000000000, 9000000000, true),
		-3+250*3+(-300)*5+60000*7+(-70000)*11+4000000000*13+(-5000000000)*17+9000000000*19+23)
	check("ret u8", int64(cRetU8(150)), 250)
	// 200+100 overflows: GCC computes it with lea and leaves bit 8 set in
	// EAX, which only the low byte of the result may ignore.
	check("ret u8 wrapped", int64(cRetU8(200)), 44)
	check("ret i8", int64(cRetI8(-20)), -120)
	check("ret u16", int64(cRetU16(20000)), 60000)
	check("ret i16", int64(cRetI16(-12345)), 12345)
	b1, b2 := cRetBool(4), cRetBool(2)
	check("ret bool", int64(boolInt(b1)*10+boolInt(b2)), 10)
	checkF("floats", cFloats(1.5, 2.25, -0.5, 4), 1.5+2.25*2-0.5*4+4*8)
	checkF("ret f32", float64(cRetF32(1.5, -2.5)), -3.75)
	checkF("mixed scalars", cMixed(-7, 0.5, 9, 0.25), -7+0.5*3+9*5+0.25*7)

	check("I1 arg", cI1(I1{-9}), -27)
	check("I2 arg", cI2(I2{4, -6}), 4*3-6*5)
	check("I3 arg", cI3(I3{1 << 40, -7, 300}), (1<<40)*3-7*5+300*7)
	check("I4 arg", cI4(I4{1, -2, 3, -4}), 3-10+21-44)
	if r := cMakeI1(41); r.A == 42 {
		println("I1 result ok")
	} else {
		println("I1 result FAIL", r.A)
	}
	if r := cMakeI2(10, -20); r.A == 11 && r.B == -18 {
		println("I2 result ok")
	} else {
		println("I2 result FAIL", r.A, r.B)
	}
	if r := cMakeI3(-1<<50, 100, -200); r.A == -1<<50+1 && r.B == 102 && r.C == -197 {
		println("I3 result ok")
	} else {
		println("I3 result FAIL", r.A, r.B, r.C)
	}
	if r := cMakeI4(-2); r.A == -2 && r.B == -1 && r.C == 0 && r.D == 1 {
		println("I4 result ok")
	} else {
		println("I4 result FAIL", r.A, r.B, r.C, r.D)
	}

	checkF("F1 arg", cF1(F1{1.5}), 4.5)
	checkF("F2 arg", cF2(F2{1.5, -2}), 4.5-10)
	checkF("F3 arg", cF3(F3{1, 2, 4}), 3+10+28)
	checkF("F4 arg", cF4(F4{1, 2, 4, 8}), 3+10+28+88)
	checkF("D2 arg", cD2(D2{0.25, 1e10}), 0.75+5e10)
	if r := cMakeF1(2.5); r.A == 5 {
		println("F1 result ok")
	} else {
		println("F1 result FAIL")
	}
	if r := cMakeF2(2.5, -1); r.A == 5 && r.B == -2 {
		println("F2 result ok")
	} else {
		println("F2 result FAIL")
	}
	if r := cMakeF3(0.5); r.A == 0.5 && r.B == 1.5 && r.C == 2.5 {
		println("F3 result ok")
	} else {
		println("F3 result FAIL")
	}
	if r := cMakeF4(-1); r.A == -1 && r.B == 0 && r.C == 1 && r.D == 2 {
		println("F4 result ok")
	} else {
		println("F4 result FAIL")
	}
	if r := cMakeD2(3, 0.5); r.A == 3.5 && r.B == 2.5 {
		println("D2 result ok")
	} else {
		println("D2 result FAIL")
	}

	checkF("M2 arg", cM2(M2{-4, 0.5}), -12+2.5)
	checkF("M2b arg", cM2b(M2b{0.5, -4}), 1.5-20)
	checkF("M3 arg", cM3(M3{0.5, 3, 0.25}), 1.5+15+1.75)
	checkF("M4 arg", cM4(M4{-1, 200, 0.5, 2}), -3+1000+3.5+22)
	if r := cMakeM2(-21, 1.25); r.A == -42 && r.B == 2.5 {
		println("M2 result ok")
	} else {
		println("M2 result FAIL")
	}
	if r := cMakeM2b(1.25, -21); r.A == 2.5 && r.B == -42 {
		println("M2b result ok")
	} else {
		println("M2b result FAIL")
	}
	if r := cMakeM3(0.5, 7, -2); r.A == 1.5 && r.B == 8 && r.C == -1 {
		println("M3 result ok")
	} else {
		println("M3 result FAIL")
	}
	if r := cMakeM4(-3); r.A == -3 && r.B == 254 && r.C == -0.5 && r.D == 0.5 {
		println("M4 result ok")
	} else {
		println("M4 result FAIL")
	}

	checkF("Big arg", cBig(Big{2, 0.5, -3}, 4), 6+2.5-21+44)
	if r := cMakeBig(1<<33, 0.5, -1); r.A == 1<<33+1 && r.B == 1.5 && r.C == 0 {
		println("Big result ok")
	} else {
		println("Big result FAIL")
	}
	if r := cBigRoundTrip(Big{-5, 0.75, 1 << 20}); r.A == -10 && r.B == 1.5 && r.C == 1<<21 {
		println("Big round trip ok")
	} else {
		println("Big round trip FAIL")
	}
	check("Huge args", cHuge(Huge{[5]int64{1, 2, 3, 4, 5}}, Huge{[5]int64{5, 4, 3, 2, 1}}),
		(1*1+2*2+3*3+4*4+5*5)-(5*7+4*8+3*9+2*10+1*11))

	check("many args", cMany(1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, -2, 0.5),
		1+4+9+16+25+36+49+64+(9+20+33+48+65+84+105+128+153+180)-38+10)
	check("struct spilled after ints", cSpillInt(1, 2, 3, 4, 5, I3{6, 7, 8}, 9),
		1+4+9+16+25+36+49+64+81)
	checkF("struct spilled after floats", cSpillSSE(1, 2, 3, 4, 5, 6, 7, D2{8, 9}, 10),
		1+4+9+16+25+36+49+64+81+100)
	if r := cSretMany(1, 2, 3, 4, 5, 6); r.A == 21 && r.B == 6 && r.C == -3 {
		println("sret with six ints ok")
	} else {
		println("sret with six ints FAIL", r.A, r.C)
	}

	q, r := cDivmod(47, 5)
	check("two results", q*100+r, 902)
	p1, p2 := cPair32(-10)
	check("two int32 results", int64(p1)*100+int64(p2), -9*100-8)
	t1, t2, t3 := cThree(7)
	check("three results", t1*10000+t2*100+t3, 71421)
	n, d := cIntDouble(4)
	checkF("int and float results", float64(n)+d, 12+2)

	// A C function used as a func value goes through a thunk.
	fv := cMakeM2b
	if r := fv(0.5, 3); r.A == 1 && r.B == 6 {
		println("func value ok")
	} else {
		println("func value FAIL")
	}

	check("packed uint32", cCtl(Ctl{mode: 5, enable: true, level: 9}), 5|1<<4|9<<5)
	check("packed bytes in a struct", cWithHeader(WithHeader{Header{version: 1, kind: 2, length: 0x01020304}, -3}),
		1+2*10+0x01020304*100-3*7)
	var vm VolMix
	vm.V = 3
	vm.F = 1.5
	if r := cVolMix(vm); r.V == 6 && r.F == 3 {
		println("volatile field ok")
	} else {
		println("volatile field FAIL")
	}

	m := M2{2, 0.5}
	checkF("extern method", m.viaC(), 6+2.5)
	mv := m.viaC
	checkF("extern method value", mv(), 6+2.5)
	me := M2.viaC
	checkF("extern method expression", me(M2{-1, 1}), -3+5)

	check("callbacks from C", cCallbacks(), 0)
	check("C function pointer to export", cApply(cAddressOfSum(), 7, -3), 697)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
