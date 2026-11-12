package main

func checkChan(c <-chan int) bool {
	select {
	case c:
		return true
	default:
		return false
	}
}

func main() {}
