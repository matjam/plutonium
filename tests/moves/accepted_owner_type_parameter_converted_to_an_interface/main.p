package main

// The twins of rejected_owner_type_parameter_converted_to_an_interface:
// the view of the type parameter value is used while its variable still
// holds the value (language spec section 18.3, D75).

type Buf owner struct{ n int }

func use(i any) { println(i != nil) }

func consume[T owner any](t T) {}

func replaced[T owner any](t T, z T) {
	i := any(t)
	use(i)
	t = z
}

func moved[T owner any](t T) {
	i := any(t)
	use(i)
	consume(t)
}

func scoped[T owner any](t T) {
	{
		u := t
		i := any(u)
		use(i)
	}
}

func main() {
	replaced(Buf{n: 1}, Buf{n: 2})
	moved(Buf{n: 1})
	scoped(Buf{n: 1})
}
