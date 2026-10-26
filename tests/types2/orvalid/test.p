package p

func a(path string) (int, error) {
	load(path) or return
	cfg := load(path) or return wrap(err)
	cfg = load(path) or return 0, err
	n, s := two() or return
	n, s = two() or panic(err)
	fail() or return
	m := gen(3) or return
	m = gen[int](4) or {
		use(err)
		return 0, err
	}
	use(cfg, n, s, m)
	for _, line := range []string{"1"} {
		k := atoi(line) or continue
		k = atoi(line) or break
		use(k)
		switch k := atoi(line) or continue; {
		case k > 0:
			atoi(line) or break // breaks the switch
		}
	}
	if v := atoi(path) or return; v > 0 {
		use(v)
	} else {
		use(v)
	}
	atoi(path) or {
		if n > 0 {
			return 0, err
		} else {
			panic(err)
		}
	}
	atoi(path) or {
		err := wrap(err) // a block clause may declare its own err
		return 0, err
	}
	atoi(path) or {
		err = wrap(err) // err is an ordinary variable
		return 0, err
	}
	atoi(path) or goto done
done:
	pair() or return pair() // an ordinary return of both results
	return 0, nil
}

func named(fields []string) (parsed int, err error) {
	for _, f := range fields {
		atoi(f) or return // err hides the named result: set by position
		parsed++
	}
	return
}

func errOnly() error {
	fail() or return
	fail() or return wrap(err)
	return nil
}

func oneResult() int {
	atoi("") or return 1 // an ordinary return
	return 0
}

func loop() {
Outer:
	for {
		for {
			atoi("") or break Outer
			atoi("") or continue Outer
		}
	}
}

func lit() {
	f := func() (int, error) {
		atoi("") or return
		return 0, nil
	}
	use(f)
}

func or(x int) int { return x } // or as a name

func names() {
	or := 3
	use(or)
}

func notTerminating() int {
	for {
		atoi("") or break // a break referring to the for
	}
	return 0
}

type T struct{ name string }

type myErr error

type E struct{}

func (E) Error() string { return "" }

func load(string) (T, error)    { return T{}, nil }
func two() (int, string, error) { return 0, "", nil }
func fail() error               { return nil }
func one() int                  { return 0 }
func mine() (int, myErr)        { return 0, nil }
func impl() (int, E)            { return 0, E{} }
func none()                     {}
func atoi(string) (int, error)  { return 0, nil }
func use(...any)                {}
func exit()                     {}
func gen[V any](v V) (V, error) { return v, nil }
func wrap(error) error          { return nil }
func pair() (int, error)        { return 0, nil }
