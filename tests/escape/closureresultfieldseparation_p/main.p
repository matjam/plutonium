package main
type Result struct { s string; p *int }
func inner(name string) func() Result {
 n := 7
 return func() Result { return Result{"hello " + name, &n} }
}
func outer(name string) func() *int {
 return func() *int { f := inner(name); return f().p }
}
func main() { _ = outer("world")() }
