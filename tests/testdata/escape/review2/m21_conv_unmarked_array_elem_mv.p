package main

// as m20 with a method value q[1].visit
import "unsafe"

type S struct{ n int }

var keepS *S

type Stringer interface{ String() string }

func (s *S) String() string {
	keepS = s
	return "s"
}

func (s *S) visit(x int) bool {
	keepS = s
	return x > 0
}

//pu:noinline
func call(s Stringer) string { return s.String() }

//pu:noinline
func callf(f func(int) bool) bool { return f(1) }

//pu:noinline
func f() {
	var x [2]int
	q := (*[2]S)(unsafe.Pointer(&x))
	println(callf(q[1].visit))
}

func main() {
	var mark int
	f()
	if keepS == nil {
		println("method not called")
		return
	}
	// keepS points below main's frame: into f's (or g's) frame, which is gone.
	println("dangling:", uintptr(unsafe.Pointer(keepS)) < uintptr(unsafe.Pointer(&mark)))
}
