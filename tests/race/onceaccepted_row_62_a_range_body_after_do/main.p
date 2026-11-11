package main

import (
	"pluto/sync"
	"pluto/vec"
)

type server struct {
	name string
}

type config owner struct {
	port    int
	servers vec.Vec[server]
}

var (
	cfgOnce sync.Once
	cfg     config
)

func load() {
	cfg.port = 8080
	cfg.servers.Push(server{name: "a"})
	cfg.servers.Push(server{name: "b"})
}

func show(done chan bool) {
	cfgOnce.Do(load)
	for _, s := range cfg.servers.All() {
		println(s.name, cfg.port)
	}
	done <- true
}

func main() {
	done := make(chan bool)
	task show(done)
	task show(done)
	<-done
	<-done
}
