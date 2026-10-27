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

func (b *Bank) Deposit(id string, n int) {
	a := b.accs[id]
	a.balance += n
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
