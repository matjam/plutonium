package main

import (
	"pluto/alloc"
	"pluto/sync"
)

type Account struct{ balance int }

type Bank struct {
	mu   sync.Mutex
	accs map[string]synced[Account]
}

// The bank's lock guards the map, not the accounts it holds (section 19.3,
// rule 3): a field of a synced[Account] needs a lock of that account.
func (b *Bank) Deposit(id string, n int) {
	b.mu.Lock()
	a := b.accs[id]
	a.balance += n
	b.mu.Unlock()
}

func main() {
	b := Bank{accs: map[string]synced[Account]{}}
	b.accs["x"] = alloc.Synced[Account]()
	var s sync.Scope
	for i := 0; i < 4; i++ {
		s.Task(func() { b.Deposit("x", 1) })
	}
	s.Wait()
}
