package main

type File owner struct{ fd int32 }

func (f *File) Drop() {}

// "owner" is the keyword only after the identifier list, when a constraint
// follows it (language spec section 4.6).
func Id[owner any](x owner) owner { return x }

type Box[owner any] struct{ v owner }

func (b Box[owner]) Get() owner { return b.v }

type Pair[owner, T any] struct {
	a owner
	b T
}

type Last[T, owner any] struct {
	a T
	b owner
}

// A type parameter named owner that admits owners.
type Both[owner owner any] owner struct{ v owner }

// An owner type parameter whose constraint begins with "~".
func Tilde[T owner ~int | ~uint](x T) T { return x }

type Slots[T owner interface{ ~int32 }] owner struct{ v T }

func use() {
	_ = Id(1)
	_ = Box[int]{v: 1}.Get()
	_ = Pair[int, string]{}
	_ = Last[int, string]{}
	var b Both[File]
	_ = &b
	_ = Tilde(3)
}
