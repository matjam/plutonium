package main

import "m/sys"

type Fake owner int32

type FakeSlot owner union {
	Empty
	Full sys.Fd
}

func rule1() sys.Fd { return sys.Fd(3) }

func rule3(f Fake) sys.Fd { return sys.Fd(f) }

func rule3b(fd sys.Fd) sys.Other { return sys.Other(fd) }

func pointerTo(n *int32) *sys.Fd { return (*sys.Fd)(n) }

func pointerFrom(fd *sys.Fd) *int32 { return (*int32)(fd) }

func pointerOwners(f *Fake) *sys.Fd { return (*sys.Fd)(f) }

func union(s FakeSlot) sys.Slot { return sys.Slot(s) }
