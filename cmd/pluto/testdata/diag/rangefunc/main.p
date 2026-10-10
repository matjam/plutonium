package main

func Count(n int) func(func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func log(int) {}

func eachDefer() {
	for x := range Count(3) {
		defer log(x)
	}
}

func deferCapture() {
	for x := range Count(3) {
		if x == 1 {
			defer func() { log(x) }()
			return
		}
	}
}

func main() {
	eachDefer()
	deferCapture()
}
