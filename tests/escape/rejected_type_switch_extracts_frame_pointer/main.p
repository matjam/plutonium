package main

var G *int
func main() {
	x := 1
	var a any = &x
	switch v := a.(type) {
	case *int:
		G = v
	}
}
