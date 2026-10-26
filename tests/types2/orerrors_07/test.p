package p
func f(s string) (int, error) {
	n, err := atoi(s) or return
	x := fail() or return
	a, b, c := two() or return
	use(n, err, x, a, b, c)
	return 0, nil
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
