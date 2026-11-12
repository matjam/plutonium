package main

// An or statement is not the post statement of a for statement (spec
// section 11.27, E2465), whether its clause is a return or a break: the
// block that follows belongs to the for statement.

func f() error { return nil }

func g() (int, error) { return 0, nil }

func run() error {
	for i := 0; i < 3; f() or return {
		_ = i
	}
	for i := 0; i < 3; i = g() or return {
	}
	for i := 0; i < 3; f() or break {
		_ = i
	}
	return nil
}

func main() {
	_ = run()
}
