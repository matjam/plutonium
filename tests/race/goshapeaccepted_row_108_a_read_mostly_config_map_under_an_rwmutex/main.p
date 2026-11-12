package main

import "pluto/sync"

var (
	mu     sync.RWMutex
	config = map[string]string{"host": "a"}
)

func get(k string) string {
	mu.RLock()
	defer mu.RUnlock()
	return config[k]
}

func set(k, v string) {
	mu.Lock()
	config[k] = v
	mu.Unlock()
}

func reader(done chan string) {
	done <- get("host")
}

func writer(done chan string) {
	set("host", "b")
	done <- "set"
}

func main() {
	done := make(chan string)
	task reader(done)
	task reader(done)
	task writer(done)
	<-done
	<-done
	<-done
}
