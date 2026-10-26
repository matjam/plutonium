package main

var G error
type E int
func (e E) Error() string { return "e" }
func pick(n int) error {
	var err error
	switch n {
	case 0:
		err = E(1)
	case 1:
		err = E(2)
		fallthrough
	default:
		err = E(3)
	}
	return err
}
func main() { G = pick(1) }
