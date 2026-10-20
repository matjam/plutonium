package main

// unexported pointer field at depth>0 (ro): method should not be called; if it is, dangling
import (
	"pluto/fmt"
	"unsafe"
)

type Inner struct{ n int }

var keep *Inner

func (i *Inner) String() string {
	keep = i
	return "inner"
}

type Mid struct {
	p *Inner
	k int
}

type Outer struct {
	M Mid
	k int
}

//pu:noinline
func f() {
	var in Inner
	in.n = 42
	fmt.Println(Outer{Mid{&in, 1}, 2})
}

func main() {
	var mark int
	f()
	if keep == nil {
		println("method not called")
		return
	}
	// keep points below main's frame: into f's frame, which is gone.
	println("dangling:", uintptr(unsafe.Pointer(keep)) < uintptr(unsafe.Pointer(&mark)))
}
