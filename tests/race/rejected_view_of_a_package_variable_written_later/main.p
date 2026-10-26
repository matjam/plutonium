package main
type Config struct{ limit int }

var cfg Config

func show(c *Config, done chan bool) {
	println(c.limit)
	done <- true
}

func main() {
	done := make(chan bool)
	task show(&cfg, done)
	cfg.limit = 3
	<-done
}