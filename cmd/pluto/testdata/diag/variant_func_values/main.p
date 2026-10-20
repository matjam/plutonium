package main

// A union variant with a payload is a function value of type func(P) U
// (spec section 7.13). The union a call through it makes holds what its
// argument does, like the union U.V(x) makes (spec section 18.3).

type Inner struct{ n int }

var keep *Inner

type U union {
	None
	P *Inner
}

type Opt[T any] union {
	None
	Some T
}

//pu:noinline
func store(u U) {
	if v, ok := u.(U.P); ok {
		keep = v
	}
}

//pu:noinline
func storeOpt(o Opt[*Inner]) {
	if p, ok := o.(Opt[*Inner].Some); ok {
		keep = p
	}
}

//pu:noinline
func apply(mk func(*Inner) U, p *Inner) U { return mk(p) }

type H struct{ mk func(*Inner) U }

var global Inner

// A variable holds the variant.
func Variable() {
	var in Inner
	mk := U.P
	store(mk(&in))
}

// The variant is passed to a function that calls it.
func Passed() {
	var in Inner
	store(apply(U.P, &in))
}

// A variant of a generic union.
func Generic() {
	var in Inner
	mk := Opt[*Inner].Some
	storeOpt(mk(&in))
}

// The union made is returned.
func Returned() U {
	var in Inner
	mk := U.P
	return mk(&in)
}

// The variant is in a struct field.
func Field() U {
	var in Inner
	h := H{U.P}
	return h.mk(&in)
}

// Fine: the payload is a package variable's address.
func Fine() {
	mk := U.P
	store(mk(&global))
}

func main() {
	Variable()
	Passed()
	Generic()
	_ = Returned()
	_ = Field()
	Fine()
	println(keep != nil)
}
