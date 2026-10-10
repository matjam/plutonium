package main

// Views of a frame that outlive it (language spec sections 4.5, 6.3, 6.9).

type Config struct {
	timeout int
	retries int
}

type ParseError struct {
	line, col int
}

func (e ParseError) Error() string { return "parse error" }

var last *Config

var cache []*Config

func remember(c *Config) { cache[0] = c }

func load() *Config {
	var c Config
	c.timeout = 30
	return &c
}

func header() []byte {
	var buf [16]byte
	return buf[:]
}

func counter() func() int {
	total := 0
	return func() int {
		total++
		return total
	}
}

func parse() error {
	return ParseError{line: 1, col: 2}
}

func keep() {
	var c Config
	last = &c
	remember(&c)
}

func loops() int {
	var first func() int
	for i := 0; i < 3; i++ {
		f := func() int { return i * 10 }
		if i == 0 {
			first = f
		}
	}
	return first()
}

func main() {
	_ = load()
	_ = header()
	_ = counter()
	_ = parse()
	keep()
	_ = loops()
}
