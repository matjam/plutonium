package main

// A value of a type parameter [T owner any] converted to an ordinary
// interface is a view of its variable, checked once for the generic body
// (language spec section 18.3, D75): the events that end a view of an
// owner end it.

type Buf owner struct{ n int }

func use(i any) { println(i != nil) }

func consume[T owner any](t T) {}

// The variable gets a new value.
func replaced[T owner any](t T, z T) {
	i := any(t)
	t = z
	use(i)
}

// The variable is moved away.
func moved[T owner any](t T) {
	i := any(t)
	consume(t)
	use(i)
}

// The variable is dropped at the end of its block.
func scoped[T owner any](t T) {
	var i any
	{
		u := t
		i = any(u)
	}
	use(i)
}

func main() {
	replaced(Buf{n: 1}, Buf{n: 2})
	moved(Buf{n: 1})
	scoped(Buf{n: 1})
}
