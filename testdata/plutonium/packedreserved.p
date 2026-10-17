package main

import "pluto/fmt"

// Reserved bits (spec section 7.14): conversions keep them, literals and the
// zero value have them zero, comparison and map hashing ignore them.

type Reg packed uint32 {
	mode bits[uint, 4]
	_    bits[uint, 4]
	on   bits[bool, 1]
}

// Bits 12 to 31 are covered by no field, so they are reserved too.
type Wide packed uint32 {
	a bits[uint, 4]
	_ bits[uint, 4]
	b bits[uint, 4]
}

// A big-endian register: the reserved bits are the logical bits 8 to 15.
type BE packed be uint32 {
	lo bits[uint, 8]
	_  bits[uint, 8]
	hi bits[uint, 16]
}

type Hdr packed [4]byte {
	a bits[uint8, 3]
	_ bits[uint8, 5]
	b uint8
}

// More than eight bytes: compared in chunks.
type Big packed [12]byte {
	_ uint8
	x uint16
	_ bits[uint, 4]
	y bits[uint, 4]
	_ [8]byte
}

type F packed uint64 {
	x float32
	_ bits[uint, 32]
}

type Inner packed uint16 {
	v bits[uint, 4]
	_ bits[uint, 4]
	w bits[uint, 8]
}

type Outer packed uint32 {
	i  Inner
	_  bits[uint, 8]
	hi bits[uint, 8]
}

type Rec struct {
	r Reg
	n int
}

func beValue(k BE) uint32  { return uint32(k) }
func beResult(x uint32) BE { return BE(x) }

func main() {
	// Conversions keep every bit both ways; a field write changes only its bits.
	r := Reg(0xFFFFFFFF)
	fmt.Printf("%#x\n", uint32(r))
	r.mode = 3
	fmt.Printf("%#x\n", uint32(r))
	r.on = false
	fmt.Printf("%#x\n", uint32(r))
	back := [4]byte(Hdr([4]byte{0xFF, 0x12, 0xAA, 0xBB}))
	fmt.Println(back)

	// Literals and the zero value have the reserved bits zero.
	lit := Reg{mode: 3, on: true}
	var zero Reg
	fmt.Printf("%#x %#x\n", uint32(lit), uint32(zero))

	// Comparison.
	a, b := Reg(0x1F3), Reg(0x103)
	fmt.Println(a == b, a != b, Reg(0x1F3) == Reg(0x003))
	fmt.Println(Wide(0xFFFFF012) == Wide(0x00000002), Wide(0xFFFFF012) == Wide(0x00000F02))
	fmt.Println(BE(0xFFFF00AA) == BE(0xFFFFFFAA), BE(0xFFFF00AA) == BE(0xFFFEFFAA))
	h1 := Hdr([4]byte{0xFF, 0x12, 0, 0})
	h2 := Hdr([4]byte{0x07, 0x12, 0, 0})
	h3 := Hdr([4]byte{0x07, 0x13, 0, 0})
	fmt.Println(h1 == h2, h1 == h3)
	var b1, b2 Big
	b1 = Big([12]byte{1, 2, 3, 4, 0xF5, 6, 7, 8, 9, 10, 11, 12})
	b2 = Big([12]byte{9, 2, 3, 4, 0x05, 0, 0, 0, 0, 0, 0, 0})
	fmt.Println(b1 == b2, b1 == Big([12]byte{0, 2, 4, 4, 0x05}))
	fmt.Println(F(0x1_0000_0000|0x3f800000) == F(0x3f800000), F(0x3f800001) == F(0x3f800000))
	o1, o2 := Outer(0xAB_CD_F1_E2), Outer(0xAB_00_F1_02)
	fmt.Println(o1 == o2, Outer(0xAB_CD_F1_E2) == Outer(0xAB_00_F1_E3))

	// Inside a struct, an array and an interface.
	fmt.Println(Rec{Reg(0x1F3), 1} == Rec{Reg(0x103), 1}, Rec{Reg(0x1F3), 1} == Rec{Reg(0x103), 2})
	ra := [2]Reg{Reg(0x1F3), Reg(5)}
	rb := [2]Reg{Reg(0x103), Reg(5)}
	fmt.Println(ra == rb)
	var ia, ib any = Reg(0x1F3), Reg(0x103)
	fmt.Println(ia == ib, ia == any(Reg(0x112)))
	var ba, bb any = b1, b2
	fmt.Println(ba == bb)

	// Map keys.
	m := map[Reg]int{}
	m[a] = 1
	m[b] = 2
	fmt.Println(len(m), m[a], m[Reg(0xFFF3)])
	hm := map[Hdr]int{}
	hm[h1] = 1
	hm[h2] = 2
	hm[h3] = 3
	fmt.Println(len(hm), hm[h1])
	bm := map[Big]string{}
	bm[b1] = "x"
	bm[b2] = "y"
	fmt.Println(len(bm), bm[b1])
	em := map[any]int{}
	em[ia] = 1
	em[ib] = 2
	fmt.Println(len(em))
	sm := map[Rec]int{}
	sm[Rec{Reg(0x1F3), 1}] = 1
	sm[Rec{Reg(0x103), 1}] = 2
	fmt.Println(len(sm))
	bem := map[BE]int{}
	bem[BE(0xFFFF00AA)] = 1
	bem[BE(0xFFFFFFAA)] = 2
	fmt.Println(len(bem))

	// A big-endian value passed to a function and returned keeps its bits.
	fmt.Printf("%#x %#x\n", beValue(BE(0xFFFF00AA)), uint32(beResult(0xFFFF00AA)))
}
