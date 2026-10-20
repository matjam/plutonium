package main

// callee slices the array and passes &sl[1]
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
func g(p *[2]S) string {
	sl := p[:]
	return call(&sl[1])
}

//pu:noinline
func f() {
	var arr [2]S
	println(g(&arr))
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
