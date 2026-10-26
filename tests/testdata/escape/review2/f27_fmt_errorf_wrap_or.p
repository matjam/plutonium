package main

// an error from or wrapped with Errorf %w and printed
import (
	"pluto/fmt"
	"unsafe"
)

type Inner struct{ n int }

var keep *Inner

type E struct {
	p *Inner
	n int
}

func (e *E) Error() string {
	keep = e.p
	return "e"
}

//pu:noinline
func get(e *E) (int, error) { return 0, e }

//pu:noinline
func f() {
	var in Inner
	in.n = 42
	ev := E{&in, 1}
	x := get(&ev) or {
		w := fmt.Errorf("wrapped: %w", err)
		fmt.Println(w)
		return
	}
	println(x)
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
