package main
type Thread struct{ id int }

//pu:percpu
var current *Thread

var next = &Thread{id: 1}

func switchTo() { current = next }

func main() {
	if current == nil {
		switchTo()
	}
}