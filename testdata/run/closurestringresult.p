package main

func inner(name string) func() string {
	return func() string {
		unused := func() int { return len(name) }
		_ = unused
		return "hello " + name
	}
}

func outer(name string) func() string {
	return func() string {
		f := inner(name)
		return f()
	}
}

func text(name string) string {
	f := outer(name)
	return f()
}

func main() {
	println(text("world"))
	for i := 0; i < 3; i++ {
		println(text("again"))
	}
}
