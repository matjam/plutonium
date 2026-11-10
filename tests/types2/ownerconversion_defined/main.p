package main

import "m/sys"

type Fake owner int32

type MyFd owner sys.Fd

type Deep owner MyFd

type MySlot owner sys.Slot

type FakeSlot owner union {
	Empty
	Full sys.Fd
}

func fromValue() MyFd { return MyFd(3) } // rule 1: a new MyFd holds a new sys.Fd

func fromOwner(f Fake) MyFd { return MyFd(f) } // rule 3: a Fake does not convert to sys.Fd here

func deep() Deep { return Deep(5) }

func pointer(n *int32) *MyFd { return (*MyFd)(n) }

func union(s FakeSlot) MySlot { return MySlot(s) }

func inner(fd sys.Fd) sys.Inner { return sys.Inner(fd) } // sys.Inner is declared in sys

func outer(i sys.Inner) sys.Fd { return sys.Fd(i) } // only sys converts its Inner to Fd
