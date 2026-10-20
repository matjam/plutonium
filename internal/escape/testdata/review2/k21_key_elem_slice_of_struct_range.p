package main

// ordinary code: range over a slice of structs and call a pointer method through an interface
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

type Item struct {
	S
	w int
}

func each(items []Item) string {
	out := ""
	for i := range items {
		var s Stringer = &items[i]
		out += s.String()
	}
	return out
}

//pu:noinline
func f() {
	items := [3]Item{}
	println(each(items[:]))
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
