package main

import "m/sys"

type MyFd owner sys.Fd

// Deep is defined from sys.Fd through MyFd.
type Deep owner MyFd

type MySlot owner sys.Slot

func wrap(fd sys.Fd) MyFd { return MyFd(fd) } // a sys.Fd needs no conversion to sys.Fd

func deep(fd sys.Fd) Deep { return Deep(MyFd(fd)) }

func down(d Deep) sys.Fd { return sys.Fd(d) } // Deep is defined from sys.Fd through MyFd

func slot(s sys.Slot) MySlot { return MySlot(s) }

func view(p *sys.Fd) *MyFd { return (*MyFd)(p) }
