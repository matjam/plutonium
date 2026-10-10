package main

// Package-level interface values whose dynamic values do not fit in a word
// (strings, structs, slices, arrays) must stay valid after package
// initialisation returns, so their boxes cannot live in the init frame.

type pathError struct {
	op, path string
	code     int
}

func (e pathError) Error() string { return e.op }

type textError struct{ msg string }

func (e textError) Error() string { return e.msg }

type pair struct{ a, b int }

var (
	anyString any   = "a string in an interface"
	anyStruct any   = pair{3, 4}
	anySlice  any   = []int{10, 20, 30}
	anyArray  any   = [3]string{"x", "y", "z"}
	errStruct error = pathError{op: "open", path: "/etc/missing", code: 2}
	errText   error = textError{"sentinel"}
	list            = []any{"first", pair{5, 6}, 7, []byte{'b', 'y'}}
	wrapped         = holder{err: textError{"inside a struct"}, n: 9}
)

type holder struct {
	err error
	n   int
}

// clobber overwrites a large part of the stack below main's frame.
func clobber(depth int) int {
	var junk [64]int
	for i := range junk {
		junk[i] = depth*1000 + i
	}
	if depth > 0 {
		return junk[depth] + clobber(depth-1)
	}
	return junk[0]
}

func describe(v any) {
	switch x := v.(type) {
	case string:
		println("string", x)
	case pair:
		println("pair", x.a, x.b)
	case []int:
		println("[]int", len(x), x[0], x[1], x[2])
	case [3]string:
		println("[3]string", x[0], x[1], x[2])
	case int:
		println("int", x)
	case []byte:
		println("[]byte", len(x), x[0])
	default:
		println("other")
	}
}

// main keeps a small frame, so the calls it makes reuse the stack that the
// package initialisation used.
func main() {
	println("clobber", clobber(20) > 0)
	check()
}

func check() {
	describe(anyString)
	describe(anyStruct)
	describe(anySlice)
	describe(anyArray)
	for _, v := range list {
		describe(v)
	}
	println("error", errStruct.Error(), errText.Error(), wrapped.err.Error(), wrapped.n)
	if pe, ok := errStruct.(pathError); ok {
		println("pathError", pe.op, pe.path, pe.code)
	}
	println("equal", errText == error(textError{"sentinel"}), anyStruct == any(pair{3, 4}))
}
