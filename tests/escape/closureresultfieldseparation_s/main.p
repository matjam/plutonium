package main
type Result struct { s string; p *int }
func inner(name string) func() Result {
 n := 7
 return func() Result { return Result{"hello " + name, &n} }
}
func outer(name string) func() string {
 return func() string { f := inner(name); return f().s }
}
func main() { _ = outer("world")() }
