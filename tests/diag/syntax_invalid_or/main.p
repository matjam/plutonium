package main

func f() (int, error) { return 0, nil }

func g() int { return 0 }

func uses() error {
	a := g() + g() or return // the operand of or is not a single call (E1013)
	b := (f()) or return     // the call is in parentheses (E1013)
	c := f().x or return     // a selector in front of or (E1013)
	d := f()
	or return // a newline ends the statement before or
	var e = f() or return // or follows no var declaration (E1001)
	_, _, _, _, _ = a, b, c, d, e
	return nil
}

func main() {
	uses() or panic(err)
}
