package main

import "m/sys"

// Mine is defined from sys.Fd, so main may convert between the two.
type Mine owner sys.Fd

type MySlot owner sys.Slot

func f() int32 {
	g := sys.Open()
	return int32(g) // rule 2: owner to plain value reads
}

func wrap(fd sys.Fd) Mine { return Mine(fd) } // fd is already a sys.Fd

func unwrap(m Mine) sys.Fd { return sys.Fd(m) } // Mine is defined from sys.Fd

func view(m *Mine) *sys.Fd { return (*sys.Fd)(m) } // converts both ways here

func slots(s sys.Slot) sys.Slot {
	m := MySlot(s)
	return sys.Slot(m)
}

func literals() (sys.Region, sys.Region, sys.File) {
	return sys.Region{Base: 0x1000, Len: 4096}, sys.Region{0x2000, 4096}, sys.File{}
}
