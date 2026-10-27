package main

import (
	"pluto/alloc"
	"pluto/sync"
)

type Account struct{ balance int }

// A lock passed in is named by where it came from (section 19.3): mu is
// not a lock of the account.
func Deposit(mu *sync.Mutex, accs map[string]synced[Account], id string, n int) {
	mu.Lock()
	a := accs[id]
	a.balance += n
	mu.Unlock()
}

func main() {
	var mu sync.Mutex
	accs := map[string]synced[Account]{}
	accs["x"] = alloc.Synced[Account]()
	var s sync.Scope
	for i := 0; i < 4; i++ {
		s.Task(func() { Deposit(&mu, accs, "x", 1) })
	}
	s.Wait()
}
