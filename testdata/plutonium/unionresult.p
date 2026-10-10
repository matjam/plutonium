package main

import (
	"pluto/fmt"
	"pluto/syscall"
	"unsafe"
)

// Generic unions: the system call result of the sum types proposal, an
// Option, generic functions and methods over them, and unions as type
// arguments.

type Result[T any] union {
	Ok  T
	Err syscall.Errno
}

// Unwrap returns the value, or def for an error.
func (r Result[T]) Unwrap(def T) T {
	switch v := r.(type) {
	case Ok:
		return v
	case Err:
		return def
	}
}

func (r Result[T]) Failed() bool {
	_, ok := r.(Result[T].Err)
	return ok
}

// sysResult turns a raw system call result into a Result.
func sysResult(r uintptr) Result[int] {
	if r > ^uintptr(4095) {
		return Result[int].Err(syscall.Errno(-r))
	}
	return Result[int].Ok(int(r))
}

func Read(fd int, p []byte) Result[int] {
	return sysResult(syscall.Syscall(syscall.SYS_READ, uintptr(fd), uintptr(unsafe.Pointer(unsafe.SliceData(p))), uintptr(len(p))))
}

func Write(fd int, p []byte) Result[int] {
	return sysResult(syscall.Syscall(syscall.SYS_WRITE, uintptr(fd), uintptr(unsafe.Pointer(unsafe.SliceData(p))), uintptr(len(p))))
}

// flaky fails with EINTR twice before it succeeds, like a read interrupted
// by signals.
var calls int

func flaky() Result[int] {
	calls++
	if calls < 3 {
		return Result[int].Err(syscall.EINTR)
	}
	return Result[int].Ok(42)
}

// readRetry is the proposal's retry loop.
func readRetry() (int, error) {
	for {
		switch r := flaky().(type) {
		case Result[int].Ok:
			return r, nil
		case Result[int].Err:
			if r != syscall.EINTR {
				return 0, r
			}
			fmt.Println("interrupted, retrying")
		}
	}
}

type Option[T any] union {
	None
	Some T
}

func Map[T, U any](o Option[T], f func(T) U) Option[U] {
	switch v := o.(type) {
	case Some:
		return Option[U].Some(f(v))
	case None:
		return Option[U].None
	}
}

func Find[T comparable](xs []T, x T) Option[int] {
	for i, y := range xs {
		if y == x {
			return Option[int].Some(i)
		}
	}
	return Option[int].None
}

func (o Option[T]) Or(def T) T {
	if v, ok := o.(Option[T].Some); ok {
		return v
	}
	return def
}

// Collect gathers the values of the Ok results, or returns the first error.
func Collect[T any](rs []Result[T], out []T) Result[[]T] {
	n := 0
	for _, r := range rs {
		switch v := r.(type) {
		case Ok:
			out[n] = v
			n++
		case Err:
			return Result[[]T].Err(v)
		}
	}
	return Result[[]T].Ok(out[:n])
}

type Pair[K comparable, V any] struct {
	k K
	v V
}

func main() {
	text := "written through a Result\n"
	msg := unsafe.Slice(unsafe.StringData(text), len(text))
	if n, ok := Write(1, msg).(Result[int].Ok); ok {
		fmt.Println("wrote", n, "bytes")
	}

	var buf [8]byte
	switch r := Read(99, buf[:]).(type) {
	case Ok:
		fmt.Println("read", r)
	case Err:
		fmt.Println("read from fd 99:", r.Error())
	}
	fmt.Println(Read(99, buf[:]).Unwrap(-1), Write(1, msg[:0]).Unwrap(-1), Read(99, buf[:]).Failed())

	n, err := readRetry()
	fmt.Println("readRetry:", n, err == nil)

	names := []string{"ax", "bx", "cx"}
	fmt.Println(Find(names, "bx").Or(-1), Find(names, "dx").Or(-1))
	lens := Map(Option[string].Some("hello"), func(s string) int { return len(s) })
	fmt.Println(lens.Or(0), Map(Option[string].None, func(s string) int { return len(s) }).Or(-7))

	// Unions as type arguments, and nested generic unions.
	nested := Option[Result[int]].Some(Result[int].Ok(3))
	switch v := nested.(type) {
	case Some:
		fmt.Println("nested", v.Unwrap(0))
	case None:
	}
	pairs := []Pair[string, Option[int]]{{"a", Option[int].Some(1)}, {"b", Option[int].None}}
	for _, p := range pairs {
		fmt.Println(p.k, p.v.Or(0), p.v == Option[int].None)
	}

	var out [3]int
	good := []Result[int]{Result[int].Ok(1), Result[int].Ok(2)}
	if s, ok := Collect(good, out[:]).(Result[[]int].Ok); ok {
		fmt.Println("collected", len(s), s[0], s[1])
	}
	bad := []Result[int]{Result[int].Ok(1), Result[int].Err(syscall.ENOENT), Result[int].Ok(3)}
	switch v := Collect(bad, out[:]).(type) {
	case Err:
		fmt.Println("collect failed:", v.Error())
	case Ok:
		fmt.Println("unexpected", len(v))
	}

	// Comparison of generic unions.
	fmt.Println(Result[int].Ok(1) == Result[int].Ok(1), Result[int].Ok(1) == Result[int].Err(1), Option[string].Some("x") != Option[string].Some("y"))
	fmt.Println(unsafe.Sizeof(Result[int].Ok(0)), unsafe.Sizeof(Option[string].None), unsafe.Sizeof(Option[uint8].None))
}
