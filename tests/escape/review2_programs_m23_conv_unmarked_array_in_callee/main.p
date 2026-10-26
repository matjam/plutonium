package main

// callee converts its *[2]int to *[2]S and passes &q[1]
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
func g(p *[2]int) string {
	q := (*[2]S)(unsafe.Pointer(p))
	return call(&q[1])
}

//pu:noinline
func f() {
	var x [2]int
	println(g(&x))
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
