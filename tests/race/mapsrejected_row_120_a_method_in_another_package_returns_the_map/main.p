package main

import "example.com/proj/cache"

func work(done chan bool) {
	cache.Users.Put("ann", 1)
	cache.Hosts.Put("db", 2)
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	task work(done)
	<-done
	<-done
	println(len(cache.Users.Dump()), len(cache.Hosts.Dump()))
}
