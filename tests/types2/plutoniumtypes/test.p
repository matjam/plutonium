package main

import "unsafe"

type ControlReg packed uint32 {
	mode     bits[uint, 4]
	enable   bits[bool, 1]
	priority bits[uint, 3]
}

type StatusReg packed be uint32 {
	flags bits {
		ENABLE
		READY
	}
	mode bits[uint, 4]
}

type Header packed {
	version uint8
	flags   uint8
	length  uint32
}

type Padded packed {
	hdr Header
	seq aligned[uint32, 4]
}

type UART struct {
	data   volatile[uint8]
	status volatile[uint8]
}

var pageTable aligned[[4096]byte, 4096]
var q ring[int64, 8]

func use(u *UART) uint8 {
	for u.status&1 == 0 {
	}
	u.data = 'x'
	var r ControlReg
	r.mode = 5
	r.enable = true
	var s StatusReg
	s.flags.ENABLE = true
	pageTable[0] = 1
	ok := q.Push(42)
	v, ok2 := q.Pop()
	_ = v
	raw := uint32(r)
	r = ControlReg(raw)
	r2 := ControlReg{mode: 3, priority: 1}
	_ = r2 == r
	_, _ = ok, ok2
	p := (*volatile[uint32])(unsafe.Pointer(uintptr(0xFEE000B0)))
	*p = 0
	return u.data + uint8(r.mode) + uint8(q.Len())
}
