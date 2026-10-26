package main
func inner(name string) func() (string, *int) {
 n := 7
 return func() (s string, p *int) {
  unused := func() int { return n }
  _ = unused
  s = "hello " + name
  p = &n
  return
 }
}
func outer(name string) func() string {
 return func() string { f := inner(name); s, _ := f(); return s }
}
func main() { println(outer("world")()) }
