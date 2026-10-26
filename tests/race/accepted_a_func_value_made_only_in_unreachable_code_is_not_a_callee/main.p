package main
var n int

var hook func()

func racy() {
	task func() { n++ }()
	n++
}

func neverCalled() {
	hook = racy
}

func main() {
	if hook != nil {
		hook()
	}
}