package main

// A value whose methods have pointer receivers does not satisfy an
// interface (language spec section 8.6). The fix depends on where the value
// goes: in a statement that only uses it, a pointer to it; in a return
// statement, a pointer to a local value would be E3001, so the fix builds the
// value on the heap with new (section 13.9).

type Shape interface{ Area() int }

type Square struct{ side int }

func (s *Square) Area() int { return s.side * s.side }

// Used in this function only: a pointer is the fix.
func use() int {
	var s Shape = Square{2}
	return s.Area()
}

// Returned: the fix is new, not &Square{}.
func build() Shape {
	return Square{3}
}

// Returned from a variable.
func buildVar() Shape {
	q := Square{4}
	return q
}

// An owning interface takes the box that new makes.
type Owned owner interface{ Area() int }

func buildOwned() Owned {
	return Square{5}
}

func main() {
	_ = use()
	_ = build()
	_ = buildVar()
	_ = buildOwned()
}
