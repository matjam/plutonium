package build_test

import (
	"fmt"
	"testing"
)

// TestCrossPackageDefaults calls functions and methods of another package
// whose default values use that package's unexported names and imports,
// which the calling package cannot name itself. The expected output was
// worked out by hand from the language spec, section 5.1.
func TestCrossPackageDefaults(t *testing.T) {
	files := map[string]string{
		"pluto.mod": "module example.com/defs\n",
		"shapes/shapes.p": `package shapes

import "pluto/strings"

var made int

// defaultName is unexported: callers in other packages use it only
// through New's default.
func defaultName() string {
	made++
	return "shape"
}

const unit = 10

var step = 2

type Shape struct {
	N    int
	Name string
}

func New(n int = unit, name string = defaultName()) Shape {
	return Shape{N: n, Name: name}
}

// Grow's default reads the unexported package variable at each call.
func (s *Shape) Grow(by int = step) int {
	s.N += by
	return s.N
}

func SetStep(n int) { step = n }

func Made() int { return made }

// Find's default uses an import of this file that the caller lacks.
func Find(s string, at int = strings.Index(s0, "c")) int { return at }

const s0 = "abc"

// A default may call a function that has defaults of its own.
func Twice(s Shape = New(unit * 2)) int { return 2 * s.N }

type zeroer[T any] struct{ z T }

func zero[T any]() T {
	var z zeroer[T]
	return z.z
}

// Fill's default depends on the instance's type argument.
func Fill[T any](xs []T, v T = zero[T]()) {
	for i := range xs {
		xs[i] = v
	}
}
`,
		"main.p": `package main

import (
	"example.com/defs/shapes"
	"pluto/fmt"
)

func main() {
	a := shapes.New()
	b := shapes.New(3)
	c := shapes.New(4, "four")
	fmt.Println(a.N, a.Name, b.N, b.Name, c.N, c.Name, shapes.Made())

	fmt.Println(a.Grow(), a.Grow())
	shapes.SetStep(5)
	fmt.Println(a.Grow(), a.Grow(1))

	fmt.Println(shapes.Find("x"), shapes.Find("x", 7))
	fmt.Println(shapes.Twice(), shapes.Twice(b), shapes.Made())

	xs := []int{1, 2, 3}
	shapes.Fill(xs)
	fmt.Println(xs[0], xs[1], xs[2])
	shapes.Fill(xs, 9)
	fmt.Println(xs[0], xs[1], xs[2])
	ss := []string{"p", "q"}
	shapes.Fill(ss)
	fmt.Println(len(ss[0]), len(ss[1]))
}
`,
	}
	const want = `10 shape 3 shape 4 four 2
12 14
19 20
2 7
40 6 3
0 0 0
9 9 9
0 0
`
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			got, status := compileAndRunFiles(t, files, level)
			if status != 0 || got != want {
				t.Errorf("exit status %d, output:\n%s\nwant:\n%s", status, got, want)
			}
		})
	}
}
