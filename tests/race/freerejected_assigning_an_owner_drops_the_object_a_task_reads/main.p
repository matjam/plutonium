package main

type Config struct{ n int }
type Server owner struct{ cfg owned[Config] }

var srv Server

func watch(c *Config) {
	for i := 0; i < 3; i++ {
		println(c.n)
	}
}

func start() {
	srv.cfg = new(Config)
	task watch(&*srv.cfg)
	srv.cfg = new(Config)
}

func main() {
	start()
}
