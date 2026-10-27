package main

import (
	"pluto/alloc"
	"pluto/sync"
)

type Account struct {
	mu      sync.Mutex
	balance int
}

type Bank struct {
	mu   sync.Mutex
	accs map[string]synced[Account]
}

// The bank's lock guards the map; each account's own lock guards its
// balance (section 19.3, rule 3).
func (b *Bank) Deposit(id string, n int) {
	b.mu.Lock()
	a := b.accs[id]
	b.mu.Unlock()
	a.mu.Lock()
	a.balance += n
	a.mu.Unlock()
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
