package main

var saved string
func greeter(name string) func() string {
	greeting := "hi " + name
	return func() string { return greeting }
}
func h() { saved = greeter("x")() }
func main() { h(); println(saved) }
