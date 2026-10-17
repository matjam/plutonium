package main

// Drop of an owner that declares none is not a method: it is not in the
// method set (spec section 7.18).

type Plain owner struct{ n int }

type Dropper interface{ Drop() }

func main() {
	var p Plain
	var d Dropper = &p
	_ = d
	f := Plain.Drop
	_ = f
}
