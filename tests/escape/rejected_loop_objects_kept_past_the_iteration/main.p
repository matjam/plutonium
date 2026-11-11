package main

// The object of &T{...}, the array of a slice literal, the array of
// variadic arguments and the receiver a method value saves are frame
// storage, refilled each time an iteration evaluates the same expression.
// Keeping a view of one where the next iteration can see it is E3006
// (language spec sections 11.3 and 18.4).

type T struct{ n int }

func (t T) Get() int { return t.n }

func pack(xs ...int) []int { return xs }

func literal() {
	var keep *T
	for i := 0; i < 3; i++ {
		p := &T{n: i}
		if keep != nil {
			println(keep.n)
		}
		keep = p
	}
}

func sliceLiteral() {
	var keep []int
	for i := 0; i < 3; i++ {
		s := []int{i, i}
		if keep != nil {
			println(keep[0])
		}
		keep = s
	}
}

func variadic() {
	var keep []int
	for i := 0; i < 3; i++ {
		s := pack(i, i)
		if keep != nil {
			println(keep[0])
		}
		keep = s
	}
}

func methodValue() {
	var keep func() int
	for i := 0; i < 3; i++ {
		t := T{n: i}
		f := t.Get
		if keep != nil {
			println(keep())
		}
		keep = f
	}
}

func main() {
	literal()
	sliceLiteral()
	variadic()
	methodValue()
}
