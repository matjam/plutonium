package main

import (
	"pluto/fmt"
	"unsafe"
)

// Packed unions: the x86-64 page-table entry of the sum types proposal, a
// descriptor whose tag is an enum, a byte-array backed record, and a packed
// union inside a packed struct.

type PTE packed union uint64 {
	present bits[bool, 1] // the tag: shared by every variant

	Swapped case false {
		slot bits[uint64, 63]
	}
	Mapped case true {
		writable bits[bool, 1]
		user     bits[bool, 1]
		_        bits[uint, 9]
		frame    bits[uint64, 40]
		_        bits[uint, 11]
		nx       bits[bool, 1]
	}
}

// translate returns the physical address an entry maps, or the swap slot
// it was paged out to.
func translate(pte PTE) (addr uint64, swapped bool) {
	switch e := pte.(type) {
	case PTE.Mapped:
		return e.frame << 12, false
	case PTE.Swapped:
		return e.slot, true
	}
}

// protect clears the writable bit of a mapped entry: the payload is the
// entry's bits, so changing it and converting back updates the entry.
func protect(pte PTE) PTE {
	if e, ok := pte.(PTE.Mapped); ok {
		e.writable = false
		return PTE.Mapped(e)
	}
	return pte
}

type Kind enum uint8 {
	Code
	Data
	Gate
	Trap
}

// A descriptor whose tag is an enum field; Gate and Trap share a layout,
// and the tag can hold more values than the enum names, so switches need a
// default case.
type Desc packed union uint16 {
	kind bits[Kind, 3]

	Seg case Kind.Code, Kind.Data {
		limit bits[uint16, 12]
	}
	Int case Kind.Gate, Kind.Trap {
		vector bits[uint8, 8]
		dpl    bits[uint8, 2]
	}
}

func describe(d Desc) {
	switch v := d.(type) {
	case Seg:
		fmt.Println("segment", v.kind, "limit", v.limit)
	case Int:
		fmt.Println("interrupt", v.kind, "vector", v.vector, "dpl", v.dpl)
	default:
		fmt.Printf("unknown descriptor %#x\n", uint16(d))
	}
}

// A little-endian record backed by bytes, with a signed tag.
type Msg packed union le [4]byte {
	op bits[int8, 3]

	Push case 1 {
		value int16
	}
	Pop case -1 {}
	Nop case 0, 2, 3, -4, -3, -2 {}
}

// A packed union is a field of a packed struct.
type Frame packed uint64 {
	seq   uint16
	entry Msg
	_     uint16
}

func main() {
	var table [4]uint64
	table[0] = uint64(PTE.Mapped{writable: true, frame: 0x1234})
	table[1] = uint64(PTE.Swapped{slot: 77})
	table[2] = uint64(PTE.Mapped{user: true, nx: true, frame: 0xFFFFFFFFFF})
	// table[3] is zero: a swapped-out entry with slot 0.

	for i, raw := range table {
		pte := PTE(raw) // converting from the raw word never fails
		addr, swapped := translate(pte)
		fmt.Printf("%d: %#018x present=%v addr=%#x swapped=%v\n", i, raw, pte.present, addr, swapped)
	}

	p := protect(PTE(table[0]))
	if e, ok := p.(PTE.Mapped); ok {
		fmt.Printf("protected: %#x writable=%v frame=%#x\n", uint64(p), e.writable, e.frame)
	}
	fmt.Println(protect(PTE(table[1])) == PTE(table[1]), PTE.Swapped{slot: 77} == PTE(table[1]))

	// The constructor keeps the payload's bits and sets the tag.
	if e, ok := PTE(table[2]).(PTE.Mapped); ok {
		e.frame = 1
		fmt.Printf("rebuilt: %#x\n", uint64(PTE.Mapped(e)))
	}

	for _, raw := range []uint16{0x0FF0 | 1, 0x2A00 | 0x1A, 4, 3, 7} {
		describe(Desc(raw))
	}
	describe(Desc.Int{vector: 0x80, dpl: 3})
	describe(Desc.Seg{limit: 0xABC})

	for _, m := range []Msg{Msg.Push{value: -300}, Msg.Pop{}, Msg.Nop{}, Msg([4]byte{2, 0, 0, 0})} {
		switch v := m.(type) {
		case Push:
			fmt.Println("push", v.value, v.op)
		case Pop:
			fmt.Println("pop", v.op)
		case Nop:
			fmt.Println("nop", v.op)
		}
	}

	raw := [4]byte(Msg.Push{value: 300})
	fmt.Println("raw", raw[0], raw[1], raw[2])

	f := Frame{seq: 9, entry: Msg.Push{value: 1000}}
	if v, ok := f.entry.(Msg.Push); ok {
		fmt.Println("frame", f.seq, v.value)
	}
	fmt.Println(unsafe.Sizeof(PTE(0)), unsafe.Sizeof(Desc(0)), unsafe.Sizeof(Msg.Pop{}),unsafe.Sizeof(f))
}
