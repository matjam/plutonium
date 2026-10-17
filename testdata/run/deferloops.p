package main

// A defer statement in a loop is allowed when every path from it leaves
// every loop that contains it before it can reach the defer again (language
// spec section 12.19). The program uses each way of leaving.

type item struct {
	id   int
	name string
}

func note(s string) { println("  run", s) }

// lockAndReturn: the return leaves the loop.
func lockAndReturn(items []item, id int) string {
	for _, it := range items {
		if it.id == id {
			defer note("unlock " + it.name)
			return it.name
		}
	}
	return "none"
}

// breakOut: an unlabeled break of the only loop leaves it.
func breakOut(n int) int {
	total := 0
	for i := 0; i < n; i++ {
		if i == 2 {
			defer note("break out")
			total = i
			break
		}
	}
	return total
}

// labeledBreak: a labeled break names the outer loop, so it leaves both.
func labeledBreak() {
outer:
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if i == 1 && j == 1 {
				defer note("labeled break")
				break outer
			}
		}
	}
	println("  after loops")
}

// nested: the defer is in the inner loop of two, and the return leaves both.
func nested() {
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			if i == 1 {
				defer note("nested")
				return
			}
			continue
		}
	}
}

// gotoOut: a goto to a label outside the loop leaves it.
func gotoOut(n int) {
	for i := 0; i < n; i++ {
		if i == 1 {
			defer note("goto out")
			goto done
		}
	}
done:
	println("  done")
}

// backwardGoto: the loop a backward goto makes, left by return.
func backwardGoto(n int) {
	i := 0
again:
	i++
	if i == n {
		defer note("backward goto")
		println("  i", i)
		return
	}
	goto again
}

func fail() { panic("failed") }

// panicLeaves: a call of panic, or of a function that never returns, leaves
// the loop; the other path returns.
func panicLeaves(n int) {
	for i := 0; i < n; i++ {
		defer note("panic leaves")
		if n > 100 {
			panic("too big")
		}
		if n > 50 {
			return
		}
		fail()
	}
}

// rangeOver: a range statement over a map.
func rangeOver(m map[string]int) {
	for k, v := range m {
		if v == 1 {
			defer note("range " + k)
			return
		}
	}
}

func main() {
	items := []item{{1, "a"}, {2, "b"}}
	println("lock and return")
	println(" ", lockAndReturn(items, 2))
	println("break out")
	println(" ", breakOut(5))
	println("labeled break")
	labeledBreak()
	println("nested")
	nested()
	println("goto out")
	gotoOut(4)
	println("backward goto")
	backwardGoto(3)
	println("panic leaves")
	panicLeaves(60)
	println("range")
	rangeOver(map[string]int{"x": 1})
}
